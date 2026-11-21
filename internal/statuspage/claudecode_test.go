package statuspage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Claude Code's file keeps its other settings and big numbers; a second run changes nothing.
func TestConnectClaudeCode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	p := filepath.Join(home, ".claude.json")
	os.WriteFile(p, []byte(`{"numStartups": 12345678901234567, "projects": {"x": {"a": 1}}, "mcpServers": {"other": {"command": "o"}}}`), 0o600)
	changed, err := ConnectClaudeCode(`C:\data`)
	if err != nil || !changed {
		t.Fatalf("first: %v %v", changed, err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	for _, want := range []string{"12345678901234567", `"other"`, `"tally-guru"`, `"serve"`, `"projects"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	if _, err := os.Stat(p + ".before-tallyguru"); err != nil {
		t.Error("no backup copy")
	}
	if changed, err := ConnectClaudeCode(`C:\data`); err != nil || changed {
		t.Fatalf("second run must not change: %v %v", changed, err)
	}
}

// Reading tools are allowed without prompts; other settings stay; posting tools are not added.
func TestAllowInClaudeCode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o700)
	p := filepath.Join(home, ".claude", "settings.json")
	os.WriteFile(p, []byte(`{"model":"opus","permissions":{"allow":["Bash(git status)"],"deny":["Read(.env)"]}}`), 0o600)
	n, err := AllowInClaudeCode([]string{"sync_now", "trial_balance"})
	if err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	for _, want := range []string{`"model": "opus"`, "Bash(git status)", "Read(.env)", "mcp__tally-guru__sync_now", "mcp__tally-guru__trial_balance"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	if n, _ := AllowInClaudeCode([]string{"sync_now"}); n != 0 {
		t.Error("second run must add nothing")
	}
}
