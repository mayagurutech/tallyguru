package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/i18n"
)

func runSetup(root string) error { return setup(root, os.Stdin) }

// setup is the feature screen (command-line for now; GUI in Phase 8).
func setup(root string, r io.Reader) error {
	in := bufio.NewReader(r)
	ask := func(q, def string) string {
		fmt.Printf("%s [%s]: ", q, def)
		s, _ := in.ReadString('\n')
		if s = strings.TrimSpace(s); s == "" {
			return def
		}
		return s
	}
	yn := map[bool]string{true: "y", false: "n"}
	old, err := config.Load(root)
	if err != nil {
		return err
	}
	// Language first, so the rest of the screen is in that language.
	langCode := "en"
	if ask("Language / भाषा: en or hi", old.Language) == "hi" {
		langCode = "hi"
	}
	L := i18n.Of(langCode)

	fmt.Println(i18n.T(L, "setup_title"))
	fmt.Println(i18n.T(L, "setup_intro"))
	fmt.Println()
	for i, p := range config.ProfileNames {
		fmt.Printf("  %d. %-10s %s\n", i+1, p, strings.Join(config.Profiles[p], ", "))
	}
	def := "2"
	for i, p := range config.ProfileNames {
		if strings.TrimSuffix(old.Profile, "*") == p {
			def = strconv.Itoa(i + 1)
		}
	}
	n, _ := strconv.Atoi(ask(i18n.T(L, "setup_profile"), def))
	if n < 1 || n > len(config.ProfileNames) {
		return fmt.Errorf("choose 1-%d", len(config.ProfileNames))
	}
	c, _ := config.FromProfile(config.ProfileNames[n-1])
	c.Host, c.Port, c.TopN, c.NoMask, c.Language = old.Host, old.Port, old.TopN, old.NoMask, langCode

	for {
		fmt.Println("\n" + i18n.T(L, "setup_modules"))
		for i, m := range config.Modules {
			mark := "[ ]"
			if c.Modules[m.ID] {
				mark = "[x]"
			}
			note := ""
			if m.Phase > config.CurrentPhase {
				note = i18n.T(L, "setup_later", m.Phase)
			}
			fmt.Printf("  %2d. %s %s%s\n", i+1, mark, m.Name, note)
		}
		s := ask(i18n.T(L, "setup_toggle"), "")
		if s == "" {
			break
		}
		if k, err := strconv.Atoi(s); err == nil && k >= 1 && k <= len(config.Modules) {
			id := config.Modules[k-1].ID
			c.Modules[id] = !c.Modules[id]
			c.Profile = strings.TrimSuffix(c.Profile, "*") + "*" // "*" = adjusted from preset
		}
	}
	c.PrivacyMode = strings.ToLower(ask(i18n.T(L, "setup_privacy"), yn[old.PrivacyMode])) == "y"
	c.ReadOnly = strings.ToLower(ask(i18n.T(L, "setup_readonly"), yn[c.ReadOnly])) != "n"
	if err := c.Save(root); err != nil {
		return err
	}
	fmt.Println("\n" + i18n.T(L, "setup_saved", config.Path(root), strings.Join(c.EnabledList(), ", ")))
	return nil
}
