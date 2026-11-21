package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Module is one optional feature group. Only enabled modules register MCP tools.
type Module struct {
	ID, Name string
	Phase    int  // phase in which it becomes available
	Write    bool // contains write tools (hidden when read-only)
}

// Modules lists every module in display order.
var Modules = []Module{
	{"core", "Core: companies, sync status, trial balance, ledgers, outstanding", 1, false},
	{"mapping", "Ledger mapping: TDS section, GST input, tax and audit tags per ledger", 2, false},
	{"reports", "Reports and owner MIS", 3, false},
	{"audit", "Audit checks (3CD, hygiene)", 4, false},
	{"gst", "GST reconciliation", 4, false},
	{"tds", "TDS reconciliation", 4, false},
	{"accounting", "Voucher entry", 5, true},
	{"changes", "Alter / cancel / delete vouchers", 5, true},
	{"masters", "Create / alter masters", 5, true},
	{"bulk", "Bulk import", 6, true},
	{"bank", "Bank statements and reconciliation", 6, true},
	{"banker", "Banker snapshot", 7, false},
}

// CurrentPhase is the latest phase built; later modules can be chosen but load nothing yet.
const CurrentPhase = 6

// Profiles are presets of modules; the user can adjust afterwards. "all" (every built module,
// writes allowed) is the default for everyone: author's decision, 21 Nov 2026.
var Profiles = map[string][]string{
	"all":        allBuilt(),
	"owner":      {"core", "reports"},
	"accountant": {"core", "mapping", "reports", "gst", "tds", "accounting", "masters"}, // changes, bulk, bank: off by default (token budget)
	"auditor":    {"core", "mapping", "reports", "audit", "gst", "tds"},
	"banker":     {"banker"},
}

// ProfileNames in display order.
var ProfileNames = []string{"all", "owner", "accountant", "auditor", "banker"}

// allBuilt lists every module built so far.
func allBuilt() []string {
	var out []string
	for _, m := range Modules {
		if m.Phase <= CurrentPhase && m.ID != "banker" {
			out = append(out, m.ID)
		}
	}
	return out
}

// ConfigVersion 2: everything on for everyone (21 Nov 2026). Older files that still follow a
// preset are moved to "all" when read; files the user adjusted by hand ("*") are kept.
const ConfigVersion = 2

// Config is saved as config.json in the data folder.
type Config struct {
	Language string          `json:"language"` // "en" or "hi"
	Profile  string          `json:"profile"`
	Modules  map[string]bool `json:"modules"`
	ReadOnly bool            `json:"read_only"` // hides every write tool
	Host     string          `json:"tally_host"`
	Port     int             `json:"tally_port"`
	TopN     int             `json:"top_n"` // rows shown by default
	// NoMask switches OFF masking of PAN/phone/bank/Aadhaar (masking is on by default).
	NoMask bool `json:"no_mask,omitempty"`
	// PrivacyMode replaces party names with codes (P001...) in AI answers.
	PrivacyMode bool `json:"privacy_mode"`
	// NoAutoRefresh switches OFF the quick check with Tally before answering (on by default).
	NoAutoRefresh bool `json:"no_auto_refresh,omitempty"`
	// ExportDir is where Excel files are saved (default: Documents\LedgerGuru).
	ExportDir string `json:"export_dir,omitempty"`
	// Agreed records when the disclaimer and DPDP notice were accepted in the control panel.
	Agreed string `json:"agreed,omitempty"`
	// LiveWrites are companies (other than TEST MCP) the user allowed writes to, in the panel.
	LiveWrites []string `json:"live_writes,omitempty"`
	// Version of the defaults this file follows (see ConfigVersion).
	Version int `json:"config_version,omitempty"`
}

// Default is used before setup has run: every built module, writes allowed (posting to a real
// company still needs the user's Yes for that company, and every entry a preview and confirm).
func Default() Config {
	c := Config{Language: "en", Profile: "all", Modules: map[string]bool{},
		Host: "localhost", Port: 9000, TopN: 10, Version: ConfigVersion}
	for _, m := range allBuilt() {
		c.Modules[m] = true
	}
	return c
}

// FromProfile builds a config with a profile's modules switched on.
func FromProfile(p string) (Config, error) {
	ms, ok := Profiles[p]
	if !ok {
		return Config{}, fmt.Errorf("unknown profile %q", p)
	}
	c := Default()
	c.Profile, c.Modules = p, map[string]bool{}
	for _, m := range ms {
		c.Modules[m] = true
	}
	c.ReadOnly = p != "accountant" && p != "all" // owner, auditor and banker are read-only by default
	return c, nil
}

// Enabled reports whether a module's tools should load now.
func (c Config) Enabled(id string) bool {
	if !c.Modules[id] {
		return false
	}
	for _, m := range Modules {
		if m.ID == id {
			if m.Phase > CurrentPhase {
				return false // chosen, but not built yet
			}
			if m.Write && (c.ReadOnly || CurrentPhase < 5) {
				return false // Phase 1 has no write tools at all
			}
			return true
		}
	}
	return false
}

// ExportBase is the folder for Excel files: ExportDir, or Documents\LedgerGuru (the old "LedgerGuru" folder if it exists).
func (c Config) ExportBase() string {
	if c.ExportDir != "" {
		return c.ExportDir
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "LedgerGuru"
	}
	if old := filepath.Join(h, "Documents", "LedgerGuru"); dirExists(old) {
		return old // files saved before the rename stay where users know them
	}
	return filepath.Join(h, "Documents", "LedgerGuru")
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// EnabledList returns enabled module IDs, sorted.
func (c Config) EnabledList() []string {
	var out []string
	for id := range c.Modules {
		if c.Enabled(id) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// Path of config.json under root.
func Path(root string) string { return filepath.Join(root, "config.json") }

// Load reads config.json, or returns Default if setup has not run.
func Load(root string) (Config, error) {
	b, err := os.ReadFile(Path(root))
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	c := Default()
	c.Modules, c.Version = nil, 0 // the file's own choices, not merged with the defaults
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("config.json: %w", err)
	}
	if c.Version < ConfigVersion && !strings.HasSuffix(c.Profile, "*") {
		d := Default()
		c.Profile, c.Modules, c.ReadOnly, c.Version = d.Profile, d.Modules, false, ConfigVersion
	}
	if c.Modules == nil {
		c.Modules = Default().Modules
	}
	if c.TopN <= 0 {
		c.TopN = 10
	}
	return c, nil
}

// Save writes config.json.
func (c Config) Save(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(Path(root), b, 0o600)
}
