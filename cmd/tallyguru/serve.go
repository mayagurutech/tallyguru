package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/crypt"
	"github.com/mayagurutech/tallyguru/internal/i18n"
	"github.com/mayagurutech/tallyguru/internal/mask"
	"github.com/mayagurutech/tallyguru/internal/mcp"
	"github.com/mayagurutech/tallyguru/internal/sections"
	"github.com/mayagurutech/tallyguru/internal/tally"
	"github.com/mayagurutech/tallyguru/internal/tools"
)

// startupEnv is the tools' environment of the server being built (for the startup note).
var startupEnv *tools.Env

// buildServer registers tools for enabled modules only.
func buildServer(root string) (*mcp.Server, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	box, err := crypt.LoadOrCreate(root)
	if err != nil {
		return nil, err
	}
	config.SetLiveWrites(cfg.LiveWrites)
	env := &tools.Env{Root: root, Cfg: cfg, Box: box, Now: time.Now, Tally: tally.NewClient(cfg.Host, cfg.Port)}
	s := &mcp.Server{Name: "ledgerguru", Version: version,
		Instructions: i18n.T(i18n.Of(cfg.Language), "ai_instructions")}
	startupEnv = env
	env.Ask = s.Elicit // the authorisation form goes to the user, not the AI
	if confirmDialog != nil {
		env.Confirm = confirmDialog // Windows: a Yes/No box at this PC (Claude Code's desktop app closed forms unseen)
	}
	env.OpenPanel = func() error { return startDetached("-data", root, "-here", "ui") }
	if !cfg.NoMask {
		s.Filter = mask.Text // PAN, Aadhaar, mobile, bank a/c masked in every answer
	}
	if cfg.Enabled("core") {
		s.Add(tools.Core(env)...)
	}
	if cfg.Enabled("mapping") {
		s.Add(tools.Mapping(env)...)
	}
	if cfg.Enabled("reports") {
		s.Add(tools.Reports(env)...)
	}
	if cfg.Enabled("audit") {
		// Section pairs (2025 Act with 1961 in brackets): the user's corrected copy if present.
		if err := sections.Load(root); err != nil {
			return nil, fmt.Errorf("%s: %w", sections.FileName, err)
		}
		s.Add(tools.Audit(env)...)
	}
	if cfg.Enabled("accounting") {
		s.Add(tools.Accounting(env)...)
	}
	if cfg.Enabled("changes") {
		s.Add(tools.Changes(env)...)
	}
	if cfg.Enabled("masters") {
		if !cfg.Enabled("accounting") {
			s.Add(tools.PostTools(env)...) // drafts tool
		}
		s.Add(tools.Masters(env)...)
	}
	if cfg.Enabled("bulk") || cfg.Enabled("bank") {
		if !cfg.Enabled("accounting") && !cfg.Enabled("masters") {
			s.Add(tools.PostTools(env)...)
		}
		if cfg.Enabled("bulk") {
			s.Add(tools.Bulk(env)...)
		}
		if cfg.Enabled("bank") {
			s.Add(tools.Bank(env)...)
		}
		s.Add(tools.Rules(env)...)
	}
	// Later phases register their modules here, each behind cfg.Enabled(id).
	return s, nil
}

// runServe speaks MCP on stdin/stdout. Answers come from the local database. Before
// answering about a live company it runs the same safe smart sync as the CLI (one small
// request when nothing changed); the AI cannot send arbitrary requests to Tally.
// Logs go to stderr only.
func runServe(root string) error {
	s, err := buildServer(root)
	if err != nil {
		return err
	}
	if note := startupEnv.StartupNote(); note != "" { // at most 3 s when Tally is slow
		s.Instructions = strings.TrimSpace(s.Instructions + "\n" + note)
	}
	var names []string
	for _, t := range s.Tools() {
		names = append(names, t.Name)
	}
	fmt.Fprintf(os.Stderr, "ledgerguru %s serving %d tools: %s\n", version, len(names), strings.Join(names, ", "))
	return s.Serve(os.Stdin, os.Stdout)
}
