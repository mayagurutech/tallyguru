package config

import (
	"os"
	"reflect"
	"testing"
)

func TestProfiles(t *testing.T) {
	a, _ := FromProfile("accountant")
	if a.ReadOnly {
		t.Error("accountant should not be read-only by default")
	}
	// Phase 5: write modules load for the accountant (not read-only). Bulk and bank wait for Phase 6.
	if got := a.EnabledList(); !reflect.DeepEqual(got, []string{"accounting", "core", "gst", "mapping", "masters", "reports", "tds"}) {
		t.Errorf("accountant enabled = %v", got)
	}
	for _, p := range []string{"owner", "auditor", "banker"} {
		c, _ := FromProfile(p)
		if !c.ReadOnly {
			t.Errorf("%s must be read-only by default", p)
		}
	}
	b, _ := FromProfile("banker")
	if b.Enabled("core") {
		t.Error("banker profile has no Tally access, core must be off")
	}
	if _, err := FromProfile("hacker"); err == nil {
		t.Error("unknown profile accepted")
	}
}

func TestReadOnlyHidesWriteModules(t *testing.T) {
	c := Default()
	c.ReadOnly = true
	for _, m := range Modules {
		c.Modules[m.ID] = true
	}
	for _, m := range Modules {
		if m.Write && c.Enabled(m.ID) {
			t.Errorf("write module %s loaded while read-only", m.ID)
		}
	}
}

func TestSaveLoad(t *testing.T) {
	root := t.TempDir()
	if c, _ := Load(root); c.Profile != "all" || c.ReadOnly || !c.Enabled("bank") || !c.Enabled("accounting") || !c.Enabled("audit") {
		t.Fatalf("default must be everything on, writes allowed: %+v", c)
	}
	c, _ := FromProfile("auditor")
	c.Language = "hi"
	c.Save(root)
	got, err := Load(root)
	if err != nil || got.Profile != "auditor" || got.Language != "hi" || !got.Modules["audit"] {
		t.Fatalf("round trip: %+v %v", got, err)
	}
}

// Files written before 21 Nov 2026 that follow a preset move to everything-on; a file the user
// adjusted by hand keeps its choices; a choice saved after the change is kept.
func TestOldConfigMovesToAll(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(Path(root), []byte(`{"profile":"auditor","modules":{"core":true,"audit":true},"read_only":true,"top_n":10}`), 0o600)
	if c, _ := Load(root); c.Profile != "all" || c.ReadOnly || !c.Enabled("bank") {
		t.Fatalf("old preset must move to all: %+v", c)
	}
	os.WriteFile(Path(root), []byte(`{"profile":"auditor*","modules":{"core":true},"read_only":true,"top_n":10}`), 0o600)
	if c, _ := Load(root); c.Profile != "auditor*" || !c.ReadOnly || c.Modules["audit"] {
		t.Fatalf("hand-adjusted file must be kept: %+v", c)
	}
	c, _ := FromProfile("owner")
	c.Save(root)
	if got, _ := Load(root); got.Profile != "owner" || !got.ReadOnly {
		t.Fatalf("a choice saved after the change must be kept: %+v", got)
	}
}
