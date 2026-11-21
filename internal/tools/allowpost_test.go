package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/mcp"
)

func TestAllowPost(t *testing.T) {
	t.Setenv("ALLOW_LIVE_WRITES", "")
	t.Cleanup(func() { config.SetLiveWrites(nil) })
	e := &Env{Root: t.TempDir(), Cfg: config.Default(), Now: time.Now}

	if err := e.allowPost("TEST MCP"); err != nil {
		t.Fatalf("TEST MCP is always open: %v", err)
	}
	// No form and no panel: refused with the reason.
	if err := e.allowPost("Client Co"); err == nil || !strings.Contains(err.Error(), config.LiveWord) {
		t.Fatalf("must refuse: %v", err)
	}
	// Declined, or a different word: refused, nothing saved.
	asks := 0
	answer := mcp.Elicitation{Action: "decline"}
	e.Ask = func(msg string, _ json.RawMessage) (mcp.Elicitation, error) {
		asks++
		if !strings.Contains(msg, " Co in TallyPrime") {
			t.Errorf("the form must name the company: %q", msg)
		}
		return answer, nil
	}
	if e.allowPost("Client Co") == nil {
		t.Fatal("decline must refuse")
	}
	answer = mcp.Elicitation{Action: "accept", Content: map[string]any{"typed": "yes"}}
	if e.allowPost("Client Co") == nil {
		t.Fatal("a different word must refuse")
	}
	if c, _ := config.Load(e.Root); len(c.LiveWrites) != 0 {
		t.Fatalf("nothing may be saved: %v", c.LiveWrites)
	}
	// Typed by the user: allowed and remembered (no second question).
	answer = mcp.Elicitation{Action: "accept", Content: map[string]any{"typed": "allow writes"}}
	if err := e.allowPost("Client Co"); err != nil {
		t.Fatal(err)
	}
	n := asks
	if err := e.allowPost("Client Co"); err != nil || asks != n {
		t.Fatalf("second post must not ask again: %v (asks %d -> %d)", err, n, asks)
	}
	if c, _ := config.Load(e.Root); len(c.LiveWrites) != 1 {
		t.Fatalf("not saved: %v", c.LiveWrites)
	}
	// Another company still needs its own answer.
	answer = mcp.Elicitation{Action: "cancel"}
	if e.allowPost("Other Co") == nil {
		t.Fatal("each company is allowed on its own")
	}
	// App without forms: the panel opens.
	opened := false
	e.Ask = func(string, json.RawMessage) (mcp.Elicitation, error) { return mcp.Elicitation{}, mcp.ErrNoElicit }
	e.OpenPanel = func() error { opened = true; return nil }
	if err := e.allowPost("Third Co"); err == nil || !opened {
		t.Fatalf("panel fallback: %v %v", err, opened)
	}
}
