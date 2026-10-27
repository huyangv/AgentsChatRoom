package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentchatroom/internal/room"
)

// httpClientNoProxy 直连本机，不走环境里的代理。
var httpClientNoProxy = http.Client{Transport: &http.Transport{}}

func newRoom(t *testing.T) (*room.Room, *Client) {
	t.Helper()
	r := room.New([]string{"claude", "codex"}, 6, nil)
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = room.NewHandler(r, srv.Listener.Addr().String())
	srv.Start()
	t.Cleanup(srv.Close)
	return r, &Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestStopHookBlocksWithNewMessages(t *testing.T) {
	r, c := newRoom(t)
	if _, _, err := r.Post(room.User, "@codex 这个方案有什么风险"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := RunHook(context.Background(), c, "stop", "codex", 2*time.Second, strings.NewReader("{}"), &out); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output %q: %v", out.String(), err)
	}
	if got["decision"] != "block" || !strings.Contains(got["reason"], "这个方案有什么风险") || !strings.Contains(got["reason"], "chat_send") {
		t.Fatalf("got %v", got)
	}
}

func TestStopHookLetsAgentStopWhenNothingArrives(t *testing.T) {
	_, c := newRoom(t)
	var codexOut, claudeOut bytes.Buffer
	_ = RunHook(context.Background(), c, "stop", "codex", time.Second, strings.NewReader(""), &codexOut)
	_ = RunHook(context.Background(), c, "stop", "claude", time.Second, strings.NewReader(""), &claudeOut)
	if strings.TrimSpace(codexOut.String()) != "{}" || claudeOut.Len() != 0 {
		t.Fatalf("codex=%q claude=%q", codexOut.String(), claudeOut.String())
	}
}

func TestStopHookReturnsFastWhenRoomIsDown(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1", HTTP: &httpClientNoProxy}
	var out bytes.Buffer
	start := time.Now()
	_ = RunHook(context.Background(), c, "stop", "codex", time.Minute, strings.NewReader("{}"), &out)
	if strings.TrimSpace(out.String()) != "{}" || time.Since(start) > 5*time.Second {
		t.Fatalf("out=%q after %v", out.String(), time.Since(start))
	}
}

func TestSessionStartRegistersQuietly(t *testing.T) {
	r, c := newRoom(t)
	t.Setenv("CLAUDE_CODE_MESSAGING_SOCKET", "/tmp/inbox.sock")
	var out bytes.Buffer
	if err := RunHook(context.Background(), c, "session-start", "claude", time.Second,
		strings.NewReader(`{"session_id":"s1","cwd":"/w"}`), &out); err != nil {
		t.Fatal(err)
	}
	s := r.Status().Agents["claude"].Session
	if out.Len() != 0 || s == nil || s.SessionID != "s1" || s.Socket != "/tmp/inbox.sock" {
		t.Fatalf("out=%q session=%+v", out.String(), s)
	}
}

func runMCP(t *testing.T, c *Client, requests ...string) map[string]map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := ServeMCP(context.Background(), c, "claude", strings.NewReader(strings.Join(requests, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("bad line %q: %v", line, err)
		}
		id, _ := json.Marshal(resp["id"])
		byID[string(id)] = resp
	}
	return byID
}

func toolText(resp map[string]any) string {
	content := resp["result"].(map[string]any)["content"].([]any)
	return content[0].(map[string]any)["text"].(string)
}

func TestMCPProtocolFlow(t *testing.T) {
	r, c := newRoom(t)
	got := runMCP(t, c,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"chat_send","arguments":{"text":"@codex 我来提方案"}}}`,
		`{"jsonrpc":"2.0","id":"r4","method":"tools/call","params":{"name":"chat_read","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":6,"method":"unknown/method"}`,
	)
	if len(got) != 6 {
		t.Fatalf("want 6 responses, got %d: %v", len(got), got)
	}
	if v := got["1"]["result"].(map[string]any)["protocolVersion"]; v != "2025-06-18" {
		t.Fatalf("version %v", v)
	}
	if n := len(got["2"]["result"].(map[string]any)["tools"].([]any)); n != 2 {
		t.Fatalf("tools %d", n)
	}
	if s := toolText(got["3"]); !strings.Contains(s, "已通知：codex") {
		t.Fatalf("chat_send %q", s)
	}
	if s := toolText(got[`"r4"`]); !strings.Contains(s, "[#1 claude] @codex 我来提方案") {
		t.Fatalf("chat_read %q", s)
	}
	if _, ok := got["5"]["result"]; !ok {
		t.Fatalf("ping has no result: %v", got["5"])
	}
	if code := got["6"]["error"].(map[string]any)["code"]; code != float64(-32601) {
		t.Fatalf("unknown method code %v", code)
	}
	if r.History(0)[0].Sender != "claude" {
		t.Fatal("message not posted as claude")
	}
}

func TestMCPUnknownVersionFallsBack(t *testing.T) {
	_, c := newRoom(t)
	got := runMCP(t, c, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2099-01-01"}}`)
	if v := got["1"]["result"].(map[string]any)["protocolVersion"]; v != fallbackVersion {
		t.Fatalf("version %v", v)
	}
}

func TestMCPToolErrorWhenRoomIsDown(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1", HTTP: &httpClientNoProxy}
	got := runMCP(t, c, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"chat_send","arguments":{"text":"hi"}}}`)
	if got["1"]["result"].(map[string]any)["isError"] != true {
		t.Fatalf("want isError, got %v", got["1"])
	}
}
