package statuspage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mayagurutech/tallyguru/internal/onboard"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/syncjob"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

// Shared with the AI app's company_connect tool (package onboard).
type (
	AuthText    = onboard.AuthText
	AuthRequest = onboard.AuthRequest
)

var (
	AuthTexts     = onboard.AuthTexts
	OpenCompanies = onboard.OpenCompanies
	Authorise     = onboard.Authorise
)

// Jobs runs one sync at a time from the panel and keeps its progress lines.
type Jobs struct {
	mu      sync.Mutex
	running bool
	Company string
	Lines   []string
	Done    bool
	Err     string
}

// JobView is what the page polls.
type JobView struct {
	Running bool
	Company string
	Lines   []string
	Done    bool
	Err     string
}

// View copies the state.
func (j *Jobs) View() JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	return JobView{Running: j.running, Company: j.Company, Lines: append([]string{}, j.Lines...), Done: j.Done, Err: j.Err}
}

// Start begins a sync in the background (same safe chunked sync as the CLI).
func (j *Jobs) Start(root string, c *tally.Client, company string, src store.Source, full bool) error {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return fmt.Errorf("a sync of %s is already running", j.Company)
	}
	j.running, j.Company, j.Lines, j.Done, j.Err = true, company, nil, false, ""
	j.mu.Unlock()
	add := func(s string) {
		j.mu.Lock()
		j.Lines = append(j.Lines, s)
		if len(j.Lines) > 200 {
			j.Lines = j.Lines[len(j.Lines)-200:]
		}
		j.mu.Unlock()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
		defer cancel()
		t0 := time.Now()
		res, err := syncjob.Run(ctx, c, syncjob.Job{Root: root, Company: company, Source: src, Full: full, Progress: add})
		if res.Mode != "" {
			add(fmt.Sprintf("%s (took %s)", res, time.Since(t0).Round(time.Second)))
		}
		j.mu.Lock()
		j.running, j.Done = false, true
		if err != nil {
			j.Err = err.Error()
		}
		j.mu.Unlock()
	}()
	return nil
}

// Forget deletes everything stored for one company and source after the user typed its
// exact name. The folder must sit directly inside the data root.
func Forget(root, company string, src store.Source, typed string) (string, error) {
	if strings.TrimSpace(typed) != company {
		return "", fmt.Errorf("type the company name exactly to delete its data")
	}
	ls, err := store.List(root)
	if err != nil {
		return "", err
	}
	dir := ""
	for _, l := range ls {
		if l.Company == company && l.Source == src {
			dir = l.Dir(root)
		}
	}
	if dir == "" {
		if d := (store.Label{Company: company, Source: src}).Dir(root); isDir(d) {
			dir = d
		}
	}
	if dir == "" {
		return "", fmt.Errorf("no stored data for %q (%s)", company, src)
	}
	if filepath.Dir(dir) != filepath.Clean(root) {
		return "", fmt.Errorf("refusing: %s is not a company folder inside %s", dir, root)
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("could not delete everything (close the AI app first): %w", err)
	}
	return dir, nil
}

func isDir(p string) bool { fi, err := os.Stat(p); return err == nil && fi.IsDir() }

// ServerName is the entry name used in AI apps.
const ServerName = "tally-guru"

// Entry is the MCP server entry for this tool. -data fixes the data folder, so the AI app and
// the control panel always use the same one (the Microsoft Store Claude app redirects AppData).
func Entry(root string) (command string, args []string) {
	exe, _ := os.Executable()
	return exe, []string{"-data", root, "serve"}
}

// ClaudePlan shows what connecting would change in Claude Desktop's config.
type ClaudePlan struct {
	Path, Before, After string
	Exists, Already     bool
	ClaudeCode          string // command for Claude Code users
}

// PlanClaude prepares the change without writing anything.
func PlanClaude(root string) (ClaudePlan, error) {
	p := ClaudePlan{Path: ClaudeConfig()}
	cmd, args := Entry(root)
	q := func(s string) string { return `"` + s + `"` }
	p.ClaudeCode = "claude mcp add " + ServerName + " -- " + q(cmd) + " " + strings.Join([]string{args[0], q(args[1]), args[2]}, " ")
	m := map[string]any{}
	if b, err := os.ReadFile(p.Path); err == nil {
		p.Exists, p.Before = true, string(b)
		if err := json.Unmarshal(b, &m); err != nil {
			return p, fmt.Errorf("Claude Desktop's config is not valid JSON; fix %s first", p.Path)
		}
	}
	servers, _ := m["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	entry := map[string]any{"command": cmd, "args": args}
	if old, ok := servers[ServerName].(map[string]any); ok {
		ob, _ := json.Marshal([]any{old["command"], old["args"]})
		nb, _ := json.Marshal([]any{cmd, args})
		p.Already = string(ob) == string(nb)
		if env, ok := old["env"]; ok {
			entry["env"] = env // keep the user's own settings
		}
	}
	servers[ServerName] = entry
	m["mcpServers"] = servers
	b, _ := json.MarshalIndent(m, "", "  ")
	p.After = string(b)
	return p, nil
}

// ConnectClaude writes the planned change, keeping a copy of the old file.
func ConnectClaude(root string) (ClaudePlan, error) {
	p, err := PlanClaude(root)
	if err != nil || p.Already {
		return p, err
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o700); err != nil {
		return p, err
	}
	if p.Exists {
		if err := os.WriteFile(p.Path+".before-tallyguru", []byte(p.Before), 0o600); err != nil {
			return p, err
		}
	}
	return p, os.WriteFile(p.Path, []byte(p.After), 0o600)
}
