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
	for _, want := range []string{"12345678901234567", `"other"`, `"ledgerguru"`, `"serve"`, `"projects"`} {
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
	for _, want := range []string{`"model": "opus"`, "Bash(git status)", "Read(.env)", "mcp__ledgerguru__sync_now", "mcp__ledgerguru__trial_balance"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	if n, _ := AllowInClaudeCode([]string{"sync_now"}); n != 0 {
		t.Error("second run must add nothing")
	}
}

// Before 21 Nov 2026 the entry was called "tally-guru": it is replaced, not doubled, and its
// old tool approvals are dropped.
func TestRenameMovesOldEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o700)
	p := filepath.Join(home, ".claude.json")
	os.WriteFile(p, []byte(`{"mcpServers":{"tally-guru":{"command":"C:/x/tallyguru.exe","args":["serve"]},"other":{"command":"o.exe"}}}`), 0o600)
	s := filepath.Join(home, ".claude", "settings.json")
	os.WriteFile(s, []byte(`{"permissions":{"allow":["mcp__tally-guru__sync_now","Bash(ls)"]}}`), 0o600)
	if changed, err := ConnectClaudeCode(`C:\data`); err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), `"tally-guru"`) || !strings.Contains(string(b), `"ledgerguru"`) || !strings.Contains(string(b), `"other"`) {
		t.Fatalf("entry not moved:\n%s", b)
	}
	if _, err := AllowInClaudeCode([]string{"sync_now"}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(s)
	if strings.Contains(string(b), "mcp__tally-guru__") || !strings.Contains(string(b), "mcp__ledgerguru__sync_now") || !strings.Contains(string(b), "Bash(ls)") {
		t.Fatalf("approvals not moved:\n%s", b)
	}
}
