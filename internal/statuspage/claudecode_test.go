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
