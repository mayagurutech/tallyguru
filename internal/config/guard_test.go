package config

import "testing"

func TestLiveWritesPerCompany(t *testing.T) {
	t.Setenv("ALLOW_LIVE_WRITES", "")
	defer SetLiveWrites(nil)
	if CheckWrite("Pilot Client") == nil {
		t.Fatal("live company writable by default")
	}
	SetLiveWrites([]string{"pilot client"})
	if err := CheckWrite("Pilot Client"); err != nil {
		t.Error(err)
	}
	if CheckWrite("Other Co") == nil {
		t.Error("switch for one company opened another")
	}
}
