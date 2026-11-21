package statuspage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/i18n"
	"github.com/mayagurutech/tallyguru/internal/onboard"
	"github.com/mayagurutech/tallyguru/internal/tally"
	"github.com/mayagurutech/tallyguru/internal/tools"
)

// Settings is what the Welcome and Features sections show and edit.
type Settings struct {
	Agreed      string
	NeedAgree   bool
	Disclaimer  string
	Notice      string
	Language    string
	Profile     string
	Profiles    map[string][]string
	Names       []string
	Modules     []SetModule
	ReadOnly    bool
	PrivacyMode bool
	Masking     bool
	Host        string
	Port        int
	Tokens      int // tool definitions with the current switches
}

// SetModule is one feature switch.
type SetModule struct {
	ID, Name string
	On       bool
	Built    bool
	Write    bool
	Tokens   int
}

// LoadSettings reads the saved config for the panel.
func LoadSettings(root string) (Settings, error) {
	c, err := config.Load(root)
	if err != nil {
		return Settings{}, err
	}
	L := i18n.Of(c.Language)
	s := Settings{Agreed: c.Agreed, Disclaimer: i18n.T(L, "tool_disclaimer"), Notice: i18n.T(L, "notice"),
		Language: c.Language, Profile: c.Profile, Profiles: config.Profiles, Names: config.ProfileNames,
		ReadOnly: c.ReadOnly, PrivacyMode: c.PrivacyMode, Masking: !c.NoMask, Host: c.Host, Port: c.Port}
	s.NeedAgree = onboard.NeedAgree(c)
	tok := tools.ModuleTokens()
	for _, m := range config.Modules {
		on := c.Enabled(m.ID)
		s.Modules = append(s.Modules, SetModule{ID: m.ID, Name: m.Name, On: c.Modules[m.ID], Built: m.Phase <= config.CurrentPhase,
			Write: m.Write, Tokens: tok[m.ID]})
		if on {
			s.Tokens += tok[m.ID]
		}
	}
	return s, nil
}

// Agree records acceptance of the disclaimer and DPDP notice.
var Agree = onboard.Agree

// Change is a settings update from the panel. Nil fields stay as they are.
type Change struct {
	Language    *string         `json:"language"`
	Profile     *string         `json:"profile"`
	Modules     map[string]bool `json:"modules"`
	ReadOnly    *bool           `json:"read_only"`
	PrivacyMode *bool           `json:"privacy_mode"`
	Masking     *bool           `json:"masking"`
	Host        *string         `json:"host"`
	Port        *int            `json:"port"`
}

// Apply saves a change. Choosing a profile resets modules to its preset first; module
// switches given in the same change are applied on top.
func Apply(root string, ch Change) (config.Config, error) {
	c, err := config.Load(root)
	if err != nil {
		return c, err
	}
	if ch.Profile != nil && *ch.Profile != strings.TrimSuffix(c.Profile, "*") {
		p, err := config.FromProfile(*ch.Profile)
		if err != nil {
			return c, err
		}
		c.Profile, c.Modules, c.ReadOnly = p.Profile, p.Modules, p.ReadOnly
	}
	for id, on := range ch.Modules {
		known := false
		for _, m := range config.Modules {
			known = known || m.ID == id
		}
		if !known {
			return c, fmt.Errorf("unknown feature %q", id)
		}
		if c.Modules[id] != on {
			c.Modules[id] = on
			c.Profile = strings.TrimSuffix(c.Profile, "*") + "*"
		}
	}
	if ch.Language != nil {
		if *ch.Language != "en" && *ch.Language != "hi" {
			return c, fmt.Errorf("language is en or hi")
		}
		c.Language = *ch.Language
	}
	if ch.ReadOnly != nil {
		c.ReadOnly = *ch.ReadOnly
	}
	if ch.PrivacyMode != nil {
		c.PrivacyMode = *ch.PrivacyMode
	}
	if ch.Masking != nil {
		c.NoMask = !*ch.Masking
	}
	if ch.Host != nil {
		h := strings.TrimSpace(*ch.Host)
		if h == "" || strings.ContainsAny(h, "/ \\") {
			return c, fmt.Errorf("host %q is not valid", h)
		}
		c.Host = h
	}
	if ch.Port != nil {
		if *ch.Port < 1 || *ch.Port > 65535 {
			return c, fmt.Errorf("port must be 1-65535")
		}
		c.Port = *ch.Port
	}
	c.Modules["core"] = c.Modules["core"] || c.Profile != "banker"
	return c, c.Save(root)
}

// Check is one item of the connection check (also printed by `tallyguru doctor`).
type Check struct {
	Name, Detail, Fix string
	OK, Warn          bool
}

// ClaudeConfig is where Claude Desktop keeps its MCP servers: the installer version uses
// %APPDATA%Claude; the Microsoft Store version keeps it inside its package folder.
func ClaudeConfig() string {
	cands := []string{filepath.Join(os.Getenv("APPDATA"), "Claude", "claude_desktop_config.json")}
	if m, _ := filepath.Glob(filepath.Join(os.Getenv("LOCALAPPDATA"), "Packages", "Claude_*", "LocalCache", "Roaming", "Claude", "claude_desktop_config.json")); len(m) > 0 {
		cands = append(cands, m...)
	}
	for _, p := range cands {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return cands[0]
}

// Checks tests everything the tool needs, without changing anything.
func Checks(ctx context.Context, root string, cfg config.Config, c *tally.Client) []Check {
	var out []Check
	// 1. Tally port open at all.
	addr := net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		out = append(out, Check{Name: "TallyPrime on " + addr, Detail: "nothing is listening there",
			Fix: "Open TallyPrime. Then F1 (Help) > Settings > Connectivity: set 'TallyPrime acts as' to Server (or Both) and Port to " +
				fmt.Sprint(cfg.Port) + ". Restart TallyPrime if asked."})
		return append(out, otherChecks(root)...)
	}
	conn.Close()
	// 2. Tally answers XML and has a company open.
	var open []string
	ctx2, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err = c.Do(ctx2, tally.CompanyListRequest().Build(), func(r io.Reader) error {
		return tally.ParseCompanies(r, func(co tally.Company) error { open = append(open, co.Name); return nil })
	})
	switch {
	case err != nil:
		out = append(out, Check{Name: "TallyPrime answers requests", Detail: err.Error(),
			Fix: "Check the port number in Tally (F1 > Settings > Connectivity) matches this tool's port."})
	case len(open) == 0:
		out = append(out, Check{Name: "TallyPrime on " + addr, OK: true, Detail: "connected"},
			Check{Name: "Company open in Tally", Warn: true, Detail: "no company is open", Fix: "Open the company in TallyPrime (Alt+F3) before syncing."})
	default:
		out = append(out, Check{Name: "TallyPrime on " + addr, OK: true, Detail: "connected"},
			Check{Name: "Company open in Tally", OK: true, Detail: strings.Join(open, ", ")})
	}
	return append(out, otherChecks(root)...)
}

func otherChecks(root string) []Check {
	var out []Check
	// Data folder writable.
	f, err := os.CreateTemp(root, ".check-*")
	if err != nil {
		out = append(out, Check{Name: "Data folder", Detail: err.Error(), Fix: "Check the folder " + root + " is not read-only."})
	} else {
		f.Close()
		os.Remove(f.Name())
		out = append(out, Check{Name: "Data folder", OK: true, Detail: root})
	}
	out = append(out, claudeCodeCheck())
	// Claude Desktop entry.
	exe, _ := os.Executable()
	b, err := os.ReadFile(ClaudeConfig())
	if err != nil {
		out = append(out, Check{Name: "Claude Desktop", Warn: true, Detail: "no Claude Desktop settings found (Claude Code or another MCP app may be used)",
			Fix: "Claude Desktop: connect from the AI app section. Claude Code or other apps: add an MCP server with command \"" + exe + "\" and argument serve."})
		return out
	}
	var cfg struct {
		MCP map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		out = append(out, Check{Name: "Claude Desktop", Detail: "its config file is not valid JSON", Fix: "Fix or remove " + ClaudeConfig()})
		return out
	}
	for name, s := range cfg.MCP {
		if strings.EqualFold(filepath.Base(s.Command), "tallyguru.exe") {
			_, err := os.Stat(s.Command)
			if err != nil {
				out = append(out, Check{Name: "Claude Desktop", Detail: fmt.Sprintf("entry %q points to a missing file: %s", name, s.Command),
					Fix: "Connect again from the AI app section."})
			} else {
				out = append(out, Check{Name: "Claude Desktop", OK: true, Detail: fmt.Sprintf("connected as %q (restart Claude after changing features)", name)})
			}
			return out
		}
	}
	return append(out, Check{Name: "Claude Desktop", Warn: true, Detail: "installed, but this tool is not added yet", Fix: "Connect it from the AI app section."})
}

// claudeCodeCheck tells whether Claude Code has this tool at user scope (~/.claude.json).
func claudeCodeCheck() Check {
	const fix = "Quit Claude fully, run: tallyguru connect, then open Claude again."
	b, err := os.ReadFile(ClaudeCodeConfig())
	if err != nil {
		return Check{Name: "Claude Code", Warn: true, Detail: "not connected (no Claude Code settings yet)", Fix: fix}
	}
	var cfg struct {
		MCP map[string]struct {
			Command string `json:"command"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return Check{Name: "Claude Code", Detail: "its settings file is not valid JSON", Fix: "Fix " + ClaudeCodeConfig()}
	}
	for name, s := range cfg.MCP {
		if strings.EqualFold(filepath.Base(s.Command), "tallyguru.exe") {
			if _, err := os.Stat(s.Command); err != nil {
				return Check{Name: "Claude Code", Detail: fmt.Sprintf("entry %q points to a missing file: %s", name, s.Command), Fix: fix}
			}
			return Check{Name: "Claude Code", OK: true, Detail: fmt.Sprintf("connected as %q (restart Claude after changes)", name)}
		}
	}
	return Check{Name: "Claude Code", Warn: true, Detail: "this tool is not added yet", Fix: fix}
}
