package config

import (
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
	if c, _ := Load(root); !c.Modules["core"] || !c.ReadOnly {
		t.Fatal("default must be core-only, read-only")
	}
	c, _ := FromProfile("auditor")
	c.Language = "hi"
	c.Save(root)
	got, err := Load(root)
	if err != nil || got.Profile != "auditor" || got.Language != "hi" || !got.Modules["audit"] {
		t.Fatalf("round trip: %+v %v", got, err)
	}
}
