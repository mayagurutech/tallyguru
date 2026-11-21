package statuspage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ClaudeCodeConfig is Claude Code's user settings file; its top-level mcpServers are available
// in every project (user scope), the same as `claude mcp add --scope user`.
func ClaudeCodeConfig() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude.json")
}

// ClaudeCodeInstalled reports whether Claude Code has been used on this PC.
func ClaudeCodeInstalled() bool {
	home, _ := os.UserHomeDir()
	_, e1 := os.Stat(ClaudeCodeConfig())
	_, e2 := os.Stat(filepath.Join(home, ".claude"))
	return e1 == nil || e2 == nil
}

// ClaudeDesktopInstalled reports whether Claude Desktop has run on this PC.
func ClaudeDesktopInstalled() bool {
	_, err := os.Stat(filepath.Dir(ClaudeConfig()))
	return err == nil
}

// ConnectClaudeCode adds this server to Claude Code at user scope. Everything else in the
// file is kept as it is (numbers too); the old file is kept as a copy; already there: no change.
func ConnectClaudeCode(root string) (changed bool, err error) {
	p := ClaudeCodeConfig()
	m := map[string]any{}
	old, rerr := os.ReadFile(p)
	if rerr == nil && len(bytes.TrimSpace(old)) > 0 {
		d := json.NewDecoder(bytes.NewReader(old))
		d.UseNumber()
		if err := d.Decode(&m); err != nil {
			return false, fmt.Errorf("Claude Code's settings file is not valid JSON; fix %s first", p)
		}
	}
	servers, _ := m["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	cmd, args := Entry(root)
	entry := map[string]any{"type": "stdio", "command": cmd, "args": args}
	if o, ok := servers[ServerName].(map[string]any); ok {
		ob, _ := json.Marshal([]any{o["command"], o["args"]})
		nb, _ := json.Marshal([]any{cmd, args})
		if string(ob) == string(nb) {
			return false, nil
		}
		if env, ok := o["env"]; ok {
			entry["env"] = env
		}
	}
	servers[ServerName] = entry
	m["mcpServers"] = servers
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return false, err
	}
	if rerr == nil {
		if err := os.WriteFile(p+".before-tallyguru", old, 0o600); err != nil {
			return false, err
		}
	}
	tmp := p + ".tallyguru-tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, p)
}
