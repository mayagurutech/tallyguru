package statuspage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mayagurutech/tallyguru/internal/store"
)

func TestConnectClaudeKeepsOtherServers(t *testing.T) {
	app := t.TempDir()
	t.Setenv("APPDATA", app)
	p := filepath.Join(app, "Claude", "claude_desktop_config.json")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte(`{"mcpServers":{"other":{"command":"x.exe"},"tally-guru":{"command":"old.exe","env":{"ALLOW_LIVE":"1"}}},"theme":"dark"}`), 0o600)
	root := t.TempDir()
	plan, err := PlanClaude(root)
	if err != nil || plan.Path != p || plan.Already {
		t.Fatalf("%+v %v", plan, err)
	}
	if b, _ := os.ReadFile(p); strings.Contains(string(b), root) {
		t.Fatal("plan wrote the file")
	}
	if _, err := ConnectClaude(root); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	b, _ := os.ReadFile(p)
	json.Unmarshal(b, &m)
	srv := m["mcpServers"].(map[string]any)
	tg := srv["tally-guru"].(map[string]any)
	args, _ := json.Marshal(tg["args"])
	if srv["other"] == nil || m["theme"] != "dark" || tg["env"] == nil || !strings.Contains(string(args), "serve") || !strings.Contains(string(args), "-data") {
		t.Errorf("config after connect: %s", b)
	}
	if _, err := os.Stat(p + ".before-tallyguru"); err != nil {
		t.Error("no backup of the old config")
	}
	if plan, _ := PlanClaude(root); !plan.Already {
		t.Error("not seen as connected after connecting")
	}
}

func TestForgetNeedsExactName(t *testing.T) {
	root := t.TempDir()
	l := store.Label{Company: "TEST MCP", Source: store.Live}
	db, err := store.Open(root, l)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Forget(root, "TEST MCP", store.Live, "test mcp"); err == nil {
		t.Error("deleted with a wrong name")
	}
	if _, err := Forget(root, "TEST MCP", store.Live, "TEST MCP"); err != nil {
		t.Fatal(err)
	}
	if isDir(l.Dir(root)) {
		t.Error("folder still there")
	}
}
