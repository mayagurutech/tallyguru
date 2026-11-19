// Package mcp is a minimal Model Context Protocol server over stdio (newline-delimited
// JSON-RPC 2.0). It implements only what tool servers need: initialize, ping, tools/list
// and tools/call. Kept in-house so tool definitions stay small and no extra dependency
// is shipped.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
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
	mu           sync.Mutex
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
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			enc.Encode(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}
		if len(req.ID) == 0 { // notification: no reply
			continue
		}
		res, rerr := s.handle(req)
		out := response{JSONRPC: "2.0", ID: req.ID, Result: res, Error: rerr}
		s.mu.Lock()
		err := enc.Encode(out)
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) handle(req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
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
