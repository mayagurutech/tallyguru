package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func run(t *testing.T, s *Server, lines ...string) []map[string]any {
	var out strings.Builder
	if err := s.Serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var res []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("bad json %q", l)
		}
		res = append(res, m)
	}
	return res
}

func TestFilterAppliesToAnswersAndErrors(t *testing.T) {
	s := &Server{Filter: func(x string) string { return strings.ReplaceAll(x, "SECRET", "XXX") }}
	s.Add(Tool{Name: "ok", Handler: func(map[string]any) (string, error) { return "a SECRET", nil }},
		Tool{Name: "bad", Handler: func(map[string]any) (string, error) { return "", errors.New("SECRET in error") }})
	r := run(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ok"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"bad"}}`)
	for i := range r {
		b, _ := json.Marshal(r[i])
		if strings.Contains(string(b), "SECRET") {
			t.Errorf("filter not applied: %s", b)
		}
	}
}

func TestHandshakeListCall(t *testing.T) {
	s := &Server{Name: "t", Version: "1"}
	s.Add(Tool{Name: "echo", Description: "e", InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(a map[string]any) (string, error) { return "hi " + a["n"].(string), nil }},
		Tool{Name: "fail", InputSchema: json.RawMessage(`{"type":"object"}`),
			Handler: func(map[string]any) (string, error) { return "", errors.New("blocked: TB mismatch") }},
		Tool{Name: "boom", InputSchema: json.RawMessage(`{"type":"object"}`),
			Handler: func(map[string]any) (string, error) { panic("x") }})
	r := run(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"n":"there"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"fail"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"boom"}}`,
		`{"jsonrpc":"2.0","id":6,"method":"nope"}`,
		`not json`,
	)
	if len(r) != 7 {
		t.Fatalf("want 7 replies (notification gets none), got %d", len(r))
	}
	if v := r[0]["result"].(map[string]any)["protocolVersion"]; v != "2025-03-26" {
		t.Errorf("version negotiation: %v", v)
	}
	if n := len(r[1]["result"].(map[string]any)["tools"].([]any)); n != 3 {
		t.Errorf("tools/list returned %d", n)
	}
	text := func(i int) string {
		return r[i]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	}
	if text(2) != "hi there" {
		t.Errorf("call: %q", text(2))
	}
	if r[3]["result"].(map[string]any)["isError"] != true || !strings.Contains(text(3), "TB mismatch") {
		t.Error("tool error must be isError with message")
	}
	if !strings.Contains(text(4), "internal error") {
		t.Error("panic must be caught")
	}
	if r[5]["error"].(map[string]any)["code"].(float64) != -32601 || r[6]["error"].(map[string]any)["code"].(float64) != -32700 {
		t.Error("bad error codes")
	}
}
