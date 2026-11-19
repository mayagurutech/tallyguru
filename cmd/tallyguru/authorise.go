package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/auth"
	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/crypt"
	"github.com/mayagurutech/tallyguru/internal/i18n"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

func runAuthorise(ctx context.Context, c *tally.Client, args []string, src store.Source, root string) error {
	// Usage: authorise ["<company>"] [-letter f | -email f | -owner]
	// No company: pick from the list. No flag: self-certification.
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = strings.TrimSpace(args[0]), args[1:]
	}
	fs := flag.NewFlagSet("authorise", flag.ContinueOnError)
	letter := fs.String("letter", "", "optional: signed authorisation letter (PDF, JPG or PNG)")
	email := fs.String("email", "", "optional: saved authorisation email (.eml or .msg)")
	owner := fs.Bool("owner", false, "owner's declaration (owners using their own books)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if name == "" && fs.NArg() > 0 {
		name = strings.TrimSpace(fs.Arg(0))
	}
	n := 0
	for _, set := range []bool{*letter != "", *email != "", *owner} {
		if set {
			n++
		}
	}
	if n > 1 {
		return fmt.Errorf("choose at most one of -letter, -email or -owner")
	}
	// Recording an authorisation reads no books, so the TEST MCP guard is not applied
	// here; sync and every tool still apply it.
	cs, err := companies(ctx, c)
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		return fmt.Errorf("no company is open in TallyPrime")
	}
	L := lang(root)
	in := bufio.NewReader(os.Stdin)
	var co *tally.Company
	if name == "" {
		fmt.Println(i18n.T(L, "pick_company"))
		for i, x := range cs {
			fmt.Printf("  %d. %s\n", i+1, x.Name)
		}
		fmt.Print(i18n.T(L, "pick_number"))
		s, _ := in.ReadString('\n')
		var k int
		if _, err := fmt.Sscan(strings.TrimSpace(s), &k); err != nil || k < 1 || k > len(cs) {
			return fmt.Errorf("choose a number from 1 to %d", len(cs))
		}
		co = &cs[k-1]
	} else {
		for i := range cs {
			if strings.EqualFold(cs[i].Name, name) {
				co = &cs[i]
			}
		}
		if co == nil {
			return fmt.Errorf("company %q is not open in TallyPrime", name)
		}
	}

	r := auth.Record{Company: co.Name, GSTIN: co.GSTIN, Source: string(src), FY: auth.FY(time.Now())}
	var data []byte
	switch {
	case *letter != "":
		r.Kind, r.FileName = auth.Letter, filepath.Base(*letter)
		data, err = os.ReadFile(*letter)
	case *email != "":
		r.Kind, r.FileName = auth.Email, filepath.Base(*email)
		data, err = os.ReadFile(*email)
	case *owner:
		r.Kind = auth.Owner
	default:
		r.Kind = auth.Self
	}
	if err != nil {
		return err
	}
	if r.Kind.HasFile() {
		if err := auth.CheckEvidence(r.Kind, r.FileName, data); err != nil {
			return err
		}
	}

	notice := i18n.T(L, "notice")
	fmt.Printf("\n%s\n\n", i18n.T(L, "company_line", co.Name, dash(co.GSTIN), src, r.FY))
	fmt.Println(notice)
	fmt.Println()
	fmt.Println(i18n.T(L, "disclaimer"))
	switch r.Kind {
	case auth.Owner:
		fmt.Println("\n" + i18n.T(L, "owner_heading"))
		fmt.Println(i18n.T(L, "owner_declaration"))
	case auth.Self:
		fmt.Println("\n" + i18n.T(L, "self_heading"))
		fmt.Println(i18n.T(L, "self_declaration"))
	default:
		fmt.Println("\n" + i18n.T(L, "auth_file", r.FileName, len(data)))
	}
	words := i18n.ConfirmWords[L]
	fmt.Print("\n" + i18n.T(L, "type_agree", words[0]))
	line, _ := in.ReadString('\n')
	ok := false
	for _, w := range words {
		ok = ok || strings.EqualFold(strings.TrimSpace(line), w)
	}
	if !ok {
		return fmt.Errorf("%s", i18n.T(L, "not_accepted"))
	}
	r.NoticeLang, r.NoticeSHA256 = string(L), auth.Sum([]byte(notice))

	box, err := crypt.LoadOrCreate(root)
	if err != nil {
		return err
	}
	r.AcceptedAt = time.Now()
	dir := store.Label{Company: co.Name, GSTIN: co.GSTIN, Source: src}.Dir(root)
	if err := (auth.Store{Dir: dir, Box: box}).Add(r, data); err != nil {
		return err
	}
	fmt.Println("\n" + i18n.T(L, "recorded", dir, r.FY))
	return nil
}

// lang reads the chosen language from config (English if unset or unreadable).
func lang(root string) i18n.Lang {
	c, err := config.Load(root)
	if err != nil {
		return i18n.EN
	}
	return i18n.Of(c.Language)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
