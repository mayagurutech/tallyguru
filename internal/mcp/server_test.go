package mcp

import (
	"encoding/json"
	"errors"
	"io"
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

// A tool asks the user through the AI app (elicitation) and gets the answer while Serve keeps
// reading; without the client capability it gets ErrNoElicit.
func TestElicit(t *testing.T) {
	s := &Server{Name: "t", Version: "1"}
	var got Elicitation
	var gotErr error
	s.Add(Tool{Name: "ask", InputSchema: json.RawMessage(`{"type":"object"}`), Handler: func(map[string]any) (string, error) {
		got, gotErr = s.Elicit("Type I AGREE", json.RawMessage(`{"type":"object","properties":{"typed":{"type":"string"}}}`))
		return "done", gotErr
	}})
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go s.Serve(inR, outW)
	dec := json.NewDecoder(outR)
	send := func(l string) { inW.Write([]byte(l + "\n")) }
	read := func() map[string]any {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{"elicitation":{}}}}`)
	read()
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ask"}}`)
	q := read()
	if q["method"] != "elicitation/create" || !strings.Contains(q["params"].(map[string]any)["message"].(string), "I AGREE") {
		t.Fatalf("expected an elicitation request, got %v", q)
	}
	id, _ := json.Marshal(q["id"])
	send(`{"jsonrpc":"2.0","id":` + string(id) + `,"result":{"action":"accept","content":{"typed":"I AGREE"}}}`)
	if r := read(); r["id"] != float64(2) {
		t.Fatalf("tool reply: %v", r)
	}
	if gotErr != nil || got.Action != "accept" || got.Content["typed"] != "I AGREE" {
		t.Fatalf("answer: %+v %v", got, gotErr)
	}
	inW.Close()

	s2 := &Server{}
	s2.Add(Tool{Name: "ask", Handler: func(map[string]any) (string, error) {
		_, err := s2.Elicit("x", nil)
		return "", err
	}})
	r := run(t, s2, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ask"}}`)
	b, _ := json.Marshal(r[1])
	if !strings.Contains(string(b), ErrNoElicit.Error()) {
		t.Fatalf("without the capability: %s", b)
	}
}
