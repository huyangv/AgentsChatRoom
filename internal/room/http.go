package room

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"time"
)

// HeaderName 是本机调用方（hook、MCP）必须带的请求头。浏览器跨站请求带不了自定义头，
// 以此挡住外部网页向聊天室塞消息。
const HeaderName = "X-Agent-Room"

const maxWait = time.Hour

// NewHandler 返回给 hook 和 MCP 用的 HTTP 接口。addr 是实际监听地址（如 127.0.0.1:8765），
// 只接受 Host 为该地址或 localhost 同端口的请求，防止 DNS 重绑定。
func NewHandler(r *Room, addr string) http.Handler {
	_, port, _ := net.SplitHostPort(addr)
	allowed := map[string]bool{"127.0.0.1:" + port: true, "localhost:" + port: true}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/messages", func(w http.ResponseWriter, req *http.Request) {
		after, err := atoiDefault(req.URL.Query().Get("after"), 0)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody("after 必须是整数"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": r.History(after)})
	})
	mux.HandleFunc("POST /api/messages", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Sender string `json:"sender"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody("请求体必须是 JSON"))
			return
		}
		msg, woke, err := r.Post(body.Sender, body.Text)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
			return
		}
		if woke == nil {
			woke = []string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"message": msg, "woke": woke})
	})
	mux.HandleFunc("POST /api/register", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Agent     string `json:"agent"`
			SessionID string `json:"sessionId"`
			CWD       string `json:"cwd"`
			Socket    string `json:"socket"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody("请求体必须是 JSON"))
			return
		}
		if err := r.Register(body.Agent, Session{SessionID: body.SessionID, CWD: body.CWD, Socket: body.Socket}); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /api/wait", func(w http.ResponseWriter, req *http.Request) {
		agent := req.URL.Query().Get("agent")
		if !r.IsAgent(agent) {
			writeJSON(w, http.StatusBadRequest, errBody("未知 agent"))
			return
		}
		seconds, err := atoiDefault(req.URL.Query().Get("timeout"), 30)
		if err != nil || seconds < 0 {
			writeJSON(w, http.StatusBadRequest, errBody("timeout 必须是非负整数（秒）"))
			return
		}
		timeout := min(time.Duration(seconds)*time.Second, maxWait)
		msgs, ok := r.Wait(req.Context(), agent, timeout)
		if msgs == nil {
			msgs = []Message{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"wake": ok, "messages": msgs})
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, r.Status())
	})

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !allowed[req.Host] || req.Header.Get(HeaderName) != "1" {
			writeJSON(w, http.StatusForbidden, errBody("forbidden"))
			return
		}
		mux.ServeHTTP(w, req)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errBody(msg string) map[string]string {
	return map[string]string{"error": msg}
}

func atoiDefault(s string, def int) (int, error) {
	if s == "" {
		return def, nil
	}
	return strconv.Atoi(s)
}
