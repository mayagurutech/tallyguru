// Package mcp is a minimal Model Context Protocol server over stdio (newline-delimited
// JSON-RPC 2.0). It implements only what tool servers need: initialize, ping, tools/list,
// tools/call, and elicitation (the server asks the user a question through the AI app; the AI
// does not see or fill the answer). Kept in-house so tool definitions stay small and no extra dependency
// is shipped.
package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// Tool is one callable tool.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	// Handler returns compact text for the AI. A returned error is shown to the AI as a
	// tool error (isError), not a protocol failure.
	Handler func(args map[string]any) (string, error) `json:"-"`
}

// Server holds registered tools.
type Server struct {
	Name, Version string
	// Filter, if set, is applied to every tool answer and error before it is sent
	// (used for masking personal identifiers).
	Filter func(string) string
	// Instructions, if set, are sent to the AI app at connection time.
	Instructions string
	tools        []Tool
	mu           sync.Mutex // guards writes to the client
	enc          *json.Encoder
	canElicit    bool
	nextID       int
	pending      map[string]chan reply
	pmu          sync.Mutex
}

type reply struct {
	Result json.RawMessage
	Error  *rpcError
}

// ErrNoElicit means the AI app cannot show the user a form (no elicitation support).
var ErrNoElicit = errors.New("the AI app cannot show a form to the user")

// Elicitation is the user's answer to Elicit: Action is "accept", "decline" or "cancel".
type Elicitation struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content"`
}

// ElicitWait is how long Elicit waits for the user.
var ElicitWait = 15 * time.Minute

// Elicit asks the user directly through the AI app (MCP elicitation/create). The AI app shows
// the message and a form built from schema to the user; the AI never sees or fills it. Call
// only from a tool handler while Serve runs.
func (s *Server) Elicit(message string, schema json.RawMessage) (Elicitation, error) {
	var out Elicitation
	if !s.canElicit || s.enc == nil {
		return out, ErrNoElicit
	}
	s.pmu.Lock()
	s.nextID++
	id := fmt.Sprintf(`"guru-%d"`, s.nextID)
	ch := make(chan reply, 1)
	s.pending[id] = ch
	s.pmu.Unlock()
	defer func() { s.pmu.Lock(); delete(s.pending, id); s.pmu.Unlock() }()
	req := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": "elicitation/create",
		"params": map[string]any{"message": message, "requestedSchema": schema}}
	s.mu.Lock()
	err := s.enc.Encode(req)
	s.mu.Unlock()
	if err != nil {
		return out, err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return out, fmt.Errorf("the AI app closed the connection")
		}
		if r.Error != nil {
			if r.Error.Code == -32601 {
				return out, ErrNoElicit
			}
			return out, fmt.Errorf("the AI app could not show the form: %s", r.Error.Message)
		}
		if err := json.Unmarshal(r.Result, &out); err != nil {
			return out, fmt.Errorf("unreadable answer from the AI app: %w", err)
		}
		return out, nil
	case <-time.After(ElicitWait):
		return out, fmt.Errorf("no answer from the user within %s", ElicitWait)
	}
}

// Add registers tools.
func (s *Server) Add(t ...Tool) { s.tools = append(s.tools, t...) }

// Tools returns registered tools (for size measurement).
func (s *Server) Tools() []Tool { return s.tools }

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Supported protocol versions, newest first.
var versions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// Serve reads requests from r and writes responses to w until r closes.
// Logs must never go to w (stdout); it carries protocol messages only.
// Requests are handled one at a time, in order, by a worker; the reader keeps reading so a
// tool waiting on Elicit still receives the AI app's answer.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	s.enc = json.NewEncoder(w)
	s.pmu.Lock()
	s.pending = map[string]chan reply{}
	s.pmu.Unlock()
	work := make(chan request, 64)
	werr := make(chan error, 1)
	go func() {
		for req := range work {
			res, rerr := s.handle(req)
			out := response{JSONRPC: "2.0", ID: req.ID, Result: res, Error: rerr}
			s.mu.Lock()
			err := s.enc.Encode(out)
			s.mu.Unlock()
			if err != nil {
				werr <- err
				for range work {
				}
				return
			}
		}
		werr <- nil
	}()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m struct {
			request
			Result json.RawMessage `json:"result,omitempty"`
			Error  *rpcError       `json:"error,omitempty"`
		}
		if err := json.Unmarshal(line, &m); err != nil {
			work <- request{ID: json.RawMessage("null"), Method: parseErr} // answered in order
			continue
		}
		if m.Method == "" && len(m.ID) > 0 { // the AI app answering our elicitation request
			s.pmu.Lock()
			ch := s.pending[string(m.ID)]
			s.pmu.Unlock()
			if ch != nil {
				ch <- reply{m.Result, m.Error}
			}
			continue
		}
		if len(m.ID) == 0 { // notification: no reply
			continue
		}
		work <- m.request
	}
	close(work)
	s.pmu.Lock()
	for id, ch := range s.pending {
		close(ch)
		delete(s.pending, id)
	}
	s.pmu.Unlock()
	if err := <-werr; err != nil {
		return err
	}
	return sc.Err()
}

// parseErr marks an unreadable line, so its error reply keeps its place in the order.
const parseErr = " parse error"

func (s *Server) handle(req request) (any, *rpcError) {
	switch req.Method {
	case parseErr:
		return nil, &rpcError{-32700, "parse error"}
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Elicitation json.RawMessage `json:"elicitation"`
			} `json:"capabilities"`
		}
		json.Unmarshal(req.Params, &p)
		s.canElicit = len(p.Capabilities.Elicitation) > 0 && string(p.Capabilities.Elicitation) != "null"
		v := versions[0]
		for _, sv := range versions {
			if sv == p.ProtocolVersion {
				v = sv
			}
		}
		res := map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}
		if s.Instructions != "" {
			res["instructions"] = s.Instructions
		}
		return res, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{-32602, "invalid params"}
		}
		for _, t := range s.tools {
			if t.Name == p.Name {
				text, err := safeCall(t, p.Arguments)
				f := s.Filter
				if f == nil {
					f = func(s string) string { return s }
				}
				if err != nil {
					return map[string]any{"content": []textContent{{"text", f(err.Error())}}, "isError": true}, nil
				}
				text = f(text)
				return map[string]any{"content": []textContent{{"text", text}}}, nil
			}
		}
		return nil, &rpcError{-32602, "unknown tool: " + p.Name}
	default:
		return nil, &rpcError{-32601, "method not found: " + req.Method}
	}
}

func safeCall(t Tool, args map[string]any) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("internal error in %s: %v", t.Name, r)
		}
	}()
	if args == nil {
		args = map[string]any{}
	}
	return t.Handler(args)
}
