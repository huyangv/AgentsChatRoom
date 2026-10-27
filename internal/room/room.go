// Package room 是聊天室核心：保存消息，按 @ 决定唤醒哪个 agent，并限制 agent 之间连续互相唤醒的次数。
package room

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	User   = "user"
	System = "system"

	historyLimit = 200
)

var mentionRE = regexp.MustCompile(`@(\w+)`)

// Message 是聊天室里的一条发言。
type Message struct {
	ID     int    `json:"id"`
	Sender string `json:"sender"`
	Text   string `json:"text"`
	// TS 是发送时间，Unix 毫秒。
	TS int64 `json:"ts"`
}

// Session 是 agent 会话启动时登记的信息。
type Session struct {
	SessionID    string `json:"sessionId"`
	CWD          string `json:"cwd"`
	Socket       string `json:"socket"`
	RegisteredAt int64  `json:"registeredAt"`
}

// AgentStatus 是单个 agent 的在线状态。
type AgentStatus struct {
	// Waiting 表示它的 Stop hook 正在等聊天室消息，此时可以被唤醒。
	Waiting bool `json:"waiting"`
	// Pending 表示有发给它的新消息还没取走。
	Pending bool     `json:"pending"`
	Session *Session `json:"session"`
}

// Status 是整个聊天室的状态。
type Status struct {
	Agents  map[string]AgentStatus `json:"agents"`
	Hops    int                    `json:"hops"`
	MaxHops int                    `json:"maxHops"`
}

// Room 保存聊天室状态，并发安全。
type Room struct {
	// OnMessage 和 OnStatus 在状态变化后调用（不持有锁），用于推送给界面。
	OnMessage func(Message)
	OnStatus  func(Status)

	mu       sync.Mutex
	agents   []string
	maxHops  int
	log      io.Writer
	messages []Message
	pending  map[string]bool
	cursor   map[string]int
	waiting  map[string]int
	sessions map[string]*Session
	hops     int
	changed  chan struct{}
}

// New 创建聊天室。log 不为 nil 时，每条消息以 JSON 行追加写入。
func New(agents []string, maxHops int, log io.Writer) *Room {
	r := &Room{
		agents:   append([]string(nil), agents...),
		maxHops:  maxHops,
		log:      log,
		pending:  map[string]bool{},
		cursor:   map[string]int{},
		waiting:  map[string]int{},
		sessions: map[string]*Session{},
		changed:  make(chan struct{}),
	}
	return r
}

// IsAgent 判断名字是否是聊天室里的 agent。
func (r *Room) IsAgent(name string) bool {
	for _, a := range r.agents {
		if a == name {
			return true
		}
	}
	return false
}

// Post 记录一条发言，返回这条消息和被唤醒的 agent。
//
// 用户发言：@ 了谁就唤醒谁，没 @ 任何 agent 就唤醒全部，并重置轮次计数。
// agent 发言：只唤醒被 @ 的 agent；连续互相唤醒超过 maxHops 次后暂停，等用户发言。
func (r *Room) Post(sender, text string) (Message, []string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Message{}, nil, fmt.Errorf("消息不能为空")
	}
	if sender != User && !r.IsAgent(sender) {
		return Message{}, nil, fmt.Errorf("未知发送者：%s", sender)
	}

	r.mu.Lock()
	msg := r.appendLocked(sender, text)
	added := []Message{msg}
	targets := r.targetsLocked(sender, text)
	if sender == User {
		r.hops = 0
	} else if len(targets) > 0 {
		r.hops++
		if r.hops > r.maxHops {
			notice := fmt.Sprintf("agent 之间已连续对话 %d 次，先暂停，等你发言后继续。", r.maxHops)
			added = append(added, r.appendLocked(System, notice))
			targets = nil
		}
	}
	for _, a := range targets {
		r.pending[a] = true
	}
	r.broadcastLocked()
	status := r.statusLocked()
	r.mu.Unlock()

	for _, m := range added {
		r.emitMessage(m)
	}
	r.emitStatus(status)
	return msg, targets, nil
}

// Wait 阻塞到 agent 被唤醒、超时或 ctx 结束。被唤醒时返回它还没收到的新消息（不含它自己的发言）；
// 超时或 ctx 结束返回 ok=false。
func (r *Room) Wait(ctx context.Context, agent string, timeout time.Duration) (msgs []Message, ok bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	r.mu.Lock()
	r.waiting[agent]++
	status := r.statusLocked()
	r.mu.Unlock()
	r.emitStatus(status)

	defer func() {
		r.mu.Lock()
		r.waiting[agent]--
		status := r.statusLocked()
		r.mu.Unlock()
		r.emitStatus(status)
	}()

	r.mu.Lock()
	for !r.pending[agent] {
		ch := r.changed
		r.mu.Unlock()
		select {
		case <-ch:
		case <-timer.C:
			return nil, false
		case <-ctx.Done():
			return nil, false
		}
		r.mu.Lock()
	}
	r.pending[agent] = false
	for _, m := range r.messages {
		if m.ID > r.cursor[agent] && m.Sender != agent {
			msgs = append(msgs, m)
		}
	}
	r.cursor[agent] = r.messages[len(r.messages)-1].ID
	r.mu.Unlock()
	return msgs, true
}

// Register 记录 agent 会话的登记信息。
func (r *Room) Register(agent string, s Session) error {
	if !r.IsAgent(agent) {
		return fmt.Errorf("未知 agent：%s", agent)
	}
	s.RegisteredAt = time.Now().UnixMilli()
	r.mu.Lock()
	r.sessions[agent] = &s
	status := r.statusLocked()
	r.mu.Unlock()
	r.emitStatus(status)
	return nil
}

// History 返回 ID 大于 after 的消息，最多最近 200 条。
func (r *Room) History(after int) []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Message{}
	for _, m := range r.messages {
		if m.ID > after {
			out = append(out, m)
		}
	}
	if len(out) > historyLimit {
		out = out[len(out)-historyLimit:]
	}
	return out
}

// Status 返回当前状态。
func (r *Room) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statusLocked()
}

func (r *Room) appendLocked(sender, text string) Message {
	msg := Message{ID: len(r.messages) + 1, Sender: sender, Text: text, TS: time.Now().UnixMilli()}
	r.messages = append(r.messages, msg)
	if r.log != nil {
		if line, err := json.Marshal(msg); err == nil {
			_, _ = r.log.Write(append(line, '\n'))
		}
	}
	return msg
}

func (r *Room) targetsLocked(sender, text string) []string {
	names := map[string]bool{}
	for _, m := range mentionRE.FindAllStringSubmatch(text, -1) {
		names[strings.ToLower(m[1])] = true
	}
	var found []string
	for _, a := range r.agents {
		if (names[a] || names["all"]) && a != sender {
			found = append(found, a)
		}
	}
	if sender == User && len(found) == 0 {
		return append([]string(nil), r.agents...)
	}
	return found
}

func (r *Room) broadcastLocked() {
	close(r.changed)
	r.changed = make(chan struct{})
}

func (r *Room) statusLocked() Status {
	st := Status{Agents: map[string]AgentStatus{}, Hops: r.hops, MaxHops: r.maxHops}
	for _, a := range r.agents {
		var s *Session
		if r.sessions[a] != nil {
			copied := *r.sessions[a]
			s = &copied
		}
		st.Agents[a] = AgentStatus{Waiting: r.waiting[a] > 0, Pending: r.pending[a], Session: s}
	}
	return st
}

func (r *Room) emitMessage(m Message) {
	if r.OnMessage != nil {
		r.OnMessage(m)
	}
}

func (r *Room) emitStatus(s Status) {
	if r.OnStatus != nil {
		r.OnStatus(s)
	}
}
