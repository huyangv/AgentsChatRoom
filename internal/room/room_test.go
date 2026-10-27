package room

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

var agents = []string{"claude", "codex"}

func post(t *testing.T, r *Room, sender, text string) []string {
	t.Helper()
	_, woke, err := r.Post(sender, text)
	if err != nil {
		t.Fatal(err)
	}
	return woke
}

func senders(msgs []Message) []string {
	out := []string{}
	for _, m := range msgs {
		out = append(out, m.Sender)
	}
	return out
}

func TestUserWithoutMentionWakesAllAgents(t *testing.T) {
	r := New(agents, 6, nil)
	if got := post(t, r, User, "大家好"); !reflect.DeepEqual(got, agents) {
		t.Fatalf("woke = %v", got)
	}
}

func TestUserMentionWakesOnlyTarget(t *testing.T) {
	r := New(agents, 6, nil)
	if got := post(t, r, User, "@codex 你看呢"); !reflect.DeepEqual(got, []string{"codex"}) {
		t.Fatalf("woke = %v", got)
	}
}

func TestAgentWithoutMentionWakesNobody(t *testing.T) {
	r := New(agents, 6, nil)
	if got := post(t, r, "claude", "我先记一下"); len(got) != 0 {
		t.Fatalf("woke = %v", got)
	}
}

func TestRejectsUnknownSenderAndEmptyText(t *testing.T) {
	r := New(agents, 6, nil)
	if _, _, err := r.Post("someone", "x"); err == nil {
		t.Fatal("unknown sender accepted")
	}
	if _, _, err := r.Post(User, "   "); err == nil {
		t.Fatal("empty text accepted")
	}
}

func TestWaitReturnsUnseenMessagesAndSkipsOwn(t *testing.T) {
	r := New(agents, 6, nil)
	ctx := context.Background()
	post(t, r, User, "开始")
	if msgs, ok := r.Wait(ctx, "claude", time.Second); !ok || msgs[0].Text != "开始" {
		t.Fatalf("claude got %v %v", msgs, ok)
	}
	post(t, r, "claude", "@codex 我的方案是 A")
	if msgs, _ := r.Wait(ctx, "codex", time.Second); !reflect.DeepEqual(senders(msgs), []string{User, "claude"}) {
		t.Fatalf("codex got %v", senders(msgs))
	}
	post(t, r, "codex", "@claude A 有风险")
	if msgs, _ := r.Wait(ctx, "claude", time.Second); !reflect.DeepEqual(senders(msgs), []string{"codex"}) {
		t.Fatalf("claude got %v", senders(msgs))
	}
}

func TestWaitTimesOut(t *testing.T) {
	r := New(agents, 6, nil)
	if _, ok := r.Wait(context.Background(), "claude", 50*time.Millisecond); ok {
		t.Fatal("expected timeout")
	}
}

func TestWaitStopsWhenContextCancelled(t *testing.T) {
	r := New(agents, 6, nil)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if _, ok := r.Wait(ctx, "claude", time.Minute); ok {
		t.Fatal("expected cancel")
	}
	if r.Status().Agents["claude"].Waiting {
		t.Fatal("waiting flag not cleared")
	}
}

func TestWaitBlocksUntilMessageArrives(t *testing.T) {
	r := New(agents, 6, nil)
	time.AfterFunc(100*time.Millisecond, func() { _, _, _ = r.Post(User, "@claude 在吗") })
	start := time.Now()
	msgs, ok := r.Wait(context.Background(), "claude", 5*time.Second)
	if !ok || msgs[0].Text != "@claude 在吗" || time.Since(start) < 80*time.Millisecond {
		t.Fatalf("got %v %v after %v", msgs, ok, time.Since(start))
	}
}

func TestHopLimitPausesUntilUserSpeaks(t *testing.T) {
	r := New(agents, 2, nil)
	post(t, r, User, "开始")
	if got := post(t, r, "claude", "@codex 1"); !reflect.DeepEqual(got, []string{"codex"}) {
		t.Fatalf("hop1 woke %v", got)
	}
	if got := post(t, r, "codex", "@claude 2"); !reflect.DeepEqual(got, []string{"claude"}) {
		t.Fatalf("hop2 woke %v", got)
	}
	if got := post(t, r, "claude", "@codex 3"); len(got) != 0 {
		t.Fatalf("hop3 should pause, woke %v", got)
	}
	if last := r.History(0); last[len(last)-1].Sender != System {
		t.Fatal("missing pause notice")
	}
	if got := post(t, r, User, "继续"); !reflect.DeepEqual(got, agents) {
		t.Fatalf("user woke %v", got)
	}
	if got := post(t, r, "claude", "@codex 4"); !reflect.DeepEqual(got, []string{"codex"}) {
		t.Fatalf("after reset woke %v", got)
	}
}

func TestCallbacksFireOutsideLock(t *testing.T) {
	r := New(agents, 6, nil)
	var got []Message
	r.OnMessage = func(m Message) {
		got = append(got, m)
		_ = r.Status() // 回调里再取状态，持有锁时会死锁
	}
	post(t, r, User, "你好")
	if len(got) != 1 || got[0].Text != "你好" {
		t.Fatalf("callback got %v", got)
	}
}

func TestLogWritesJSONLines(t *testing.T) {
	var b strings.Builder
	r := New(agents, 6, &b)
	post(t, r, User, "记录我")
	var m Message
	if err := json.Unmarshal([]byte(strings.TrimSpace(b.String())), &m); err != nil || m.Text != "记录我" {
		t.Fatalf("log = %q err=%v", b.String(), err)
	}
}

func newTestServer(t *testing.T) (*Room, *httptest.Server) {
	t.Helper()
	r := New(agents, 6, nil)
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = NewHandler(r, srv.Listener.Addr().String())
	srv.Start()
	t.Cleanup(srv.Close)
	return r, srv
}

func doReq(t *testing.T, srv *httptest.Server, method, path, body string, header map[string]string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	req.Header.Set(HeaderName, "1")
	for k, v := range header {
		if k == "Host" {
			req.Host = v
		} else if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestHTTPPostAndRead(t *testing.T) {
	_, srv := newTestServer(t)
	code, out := doReq(t, srv, "POST", "/api/messages", `{"sender":"user","text":"你好"}`, nil)
	if code != 200 || len(out["woke"].([]any)) != 2 {
		t.Fatalf("post %d %v", code, out)
	}
	code, out = doReq(t, srv, "GET", "/api/messages?after=0", "", nil)
	if code != 200 || len(out["messages"].([]any)) != 1 {
		t.Fatalf("read %d %v", code, out)
	}
}

func TestHTTPWaitReturnsWake(t *testing.T) {
	_, srv := newTestServer(t)
	doReq(t, srv, "POST", "/api/messages", `{"sender":"user","text":"@codex 看下"}`, nil)
	code, out := doReq(t, srv, "GET", "/api/wait?agent=codex&timeout=2", "", nil)
	if code != 200 || out["wake"] != true {
		t.Fatalf("wait %d %v", code, out)
	}
}

func TestHTTPRejectsMissingHeader(t *testing.T) {
	r, srv := newTestServer(t)
	code, _ := doReq(t, srv, "POST", "/api/messages", `{"sender":"user","text":"注入"}`, map[string]string{HeaderName: ""})
	if code != 403 || len(r.History(0)) != 0 {
		t.Fatalf("code %d, history %v", code, r.History(0))
	}
}

func TestHTTPRejectsForeignHost(t *testing.T) {
	_, srv := newTestServer(t)
	code, _ := doReq(t, srv, "GET", "/api/status", "", map[string]string{"Host": "evil.example:80"})
	if code != 403 {
		t.Fatalf("code %d", code)
	}
}

func TestHTTPRegister(t *testing.T) {
	r, srv := newTestServer(t)
	code, _ := doReq(t, srv, "POST", "/api/register", `{"agent":"claude","sessionId":"s1","cwd":"/w","socket":"/tmp/x.sock"}`, nil)
	if code != 200 || r.Status().Agents["claude"].Session.Socket != "/tmp/x.sock" {
		t.Fatalf("register %d %+v", code, r.Status().Agents["claude"])
	}
}
