package tools

import (
	"strings"
	"testing"

	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/onboard"
)

// With the Windows Yes/No dialog: Yes records the yearly declaration (and is not asked again
// for a second company); No records nothing.
func TestConfirmDialogAuthorisation(t *testing.T) {
	e := connectEnv(t, fakeTally(t))
	shown := ""
	answer := false
	e.Confirm = func(title, text string) (bool, error) { shown = title + "\n" + text; return answer, nil }
	if s, _ := e.syncNow(map[string]any{"company": "TEST MCP"}); !strings.Contains(s, "No was clicked") || onboard.HasAnnual(e.Root, e.Box, e.Now()) {
		t.Fatalf("No must record nothing: %q", s)
	}
	if !strings.Contains(shown, "Digital Personal Data Protection Act") || !strings.Contains(shown, "either my own business") {
		t.Errorf("dialog must show the notice and the declaration: %q", shown)
	}
	answer = true
	if s, err := e.syncNow(map[string]any{"company": "TEST MCP"}); err != nil || !strings.Contains(s, "Sync started") || !onboard.HasAnnual(e.Root, e.Box, e.Now()) {
		t.Fatalf("Yes: %q %v", s, err)
	}
	for bg.busy() {
	}
	shown = ""
	if s, _ := e.syncNow(map[string]any{"company": "Other Co"}); !strings.Contains(s, "Sync started") || shown != "" {
		t.Fatalf("second company must not ask again: %q shown=%q", s, shown)
	}
}

func TestConfirmDialogAllowPost(t *testing.T) {
	t.Setenv("ALLOW_LIVE_WRITES", "")
	t.Cleanup(func() { config.SetLiveWrites(nil) })
	e := connectEnv(t, nil)
	answer := false
	e.Confirm = func(string, string) (bool, error) { return answer, nil }
	if e.allowPost("Client Co") == nil {
		t.Fatal("No must refuse")
	}
	answer = true
	if err := e.allowPost("Client Co"); err != nil {
		t.Fatal(err)
	}
	if c, _ := config.Load(e.Root); len(c.LiveWrites) != 1 {
		t.Fatalf("Yes must be saved: %v", c.LiveWrites)
	}
}
