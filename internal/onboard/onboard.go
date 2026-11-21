// Package onboard holds the first-run steps shared by the control panel and the AI app's
// company_connect tool: the tool disclaimer acceptance, the list of open companies, and the
// typed authorisation record (DPDP Act, 2023). The typed words always come from the user
// (a form in the panel or a form the AI app shows the user), never from a tool argument.
package onboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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

// NoticeHash fingerprints the texts the user agrees to, so a changed text asks again.
func NoticeHash() string {
	h := sha256.Sum256([]byte(i18n.T(i18n.EN, "tool_disclaimer") + i18n.T(i18n.EN, "notice")))
	return hex.EncodeToString(h[:6])
}

// NeedAgree reports whether the disclaimer and notice still need accepting.
func NeedAgree(c config.Config) bool { return !strings.HasSuffix(c.Agreed, NoticeHash()) }

// Agree records acceptance of the disclaimer and DPDP notice (date + text fingerprint).
func Agree(root string, now time.Time) error {
	c, err := config.Load(root)
	if err != nil {
		return err
	}
	c.Agreed = now.Format("02-01-2006 15:04") + " " + NoticeHash()
	return c.Save(root)
}

// AuthText is what the user reads before recording an authorisation.
type AuthText struct {
	Notice, Disclaimer, Heading, Declaration, Word string
}

// AuthTexts returns the texts for a kind (self, owner, letter, email).
func AuthTexts(lang string, kind auth.Kind) AuthText {
	L := i18n.Of(lang)
	t := AuthText{Notice: i18n.T(L, "notice"), Disclaimer: i18n.T(L, "disclaimer"), Word: i18n.ConfirmWords[L][0]}
	switch kind {
	case auth.Owner:
		t.Heading, t.Declaration = i18n.T(L, "owner_heading"), i18n.T(L, "owner_declaration")
	case auth.Letter, auth.Email:
		t.Heading, t.Declaration = i18n.T(L, "self_heading"), i18n.T(L, "self_declaration")
	default:
		t.Heading, t.Declaration = i18n.T(L, "self_heading"), i18n.T(L, "self_declaration")
	}
	return t
}

// OpenCompanies lists the companies open in Tally now.
func OpenCompanies(ctx context.Context, c *tally.Client) ([]tally.Company, error) {
	var out []tally.Company
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err := c.Do(ctx, tally.CompanyListRequest().Build(), func(r io.Reader) error {
		return tally.ParseCompanies(r, func(co tally.Company) error { out = append(out, co); return nil })
	})
	return out, err
}

// AuthRequest is the panel's authorisation form.
type AuthRequest struct {
	Company  string `json:"company"`
	Source   string `json:"source"`
	Kind     string `json:"kind"`
	Typed    string `json:"typed"`
	FileName string `json:"file_name"`
	File     []byte `json:"file"` // optional letter/email, base64 in JSON
}

// Authorise records the typed self-certification (or owner's declaration) for this FY,
// exactly as `tallyguru authorise` does. A live company must be open in Tally.
func Authorise(ctx context.Context, root string, c *tally.Client, rq AuthRequest, now time.Time) error {
	cfg, _ := config.Load(root)
	L := i18n.Of(cfg.Language)
	src := store.Source(rq.Source)
	if src == "" {
		src = store.Live
	}
	cs, err := OpenCompanies(ctx, c)
	if err != nil {
		return err
	}
	var co *tally.Company
	for i := range cs {
		if strings.EqualFold(cs[i].Name, rq.Company) {
			co = &cs[i]
		}
	}
	if co == nil {
		return fmt.Errorf("%q is not open in TallyPrime: open it there first", rq.Company)
	}
	r := auth.Record{Company: co.Name, GSTIN: co.GSTIN, Source: string(src), FY: auth.FY(now), Kind: auth.Kind(rq.Kind)}
	switch r.Kind {
	case auth.Self, auth.Owner:
	case auth.Letter, auth.Email:
		r.FileName = filepath.Base(rq.FileName)
		if err := auth.CheckEvidence(r.Kind, r.FileName, rq.File); err != nil {
			return err
		}
	default:
		return fmt.Errorf("choose self-certification, owner's declaration, letter or email")
	}
	ok := false
	for _, w := range i18n.ConfirmWords[L] {
		ok = ok || strings.EqualFold(strings.TrimSpace(rq.Typed), w)
	}
	if !ok {
		return fmt.Errorf("%s", i18n.T(L, "not_accepted"))
	}
	notice := i18n.T(L, "notice")
	r.NoticeLang, r.NoticeSHA256, r.AcceptedAt = string(L), auth.Sum([]byte(notice)), now
	box, err := crypt.LoadOrCreate(root)
	if err != nil {
		return err
	}
	var data []byte
	if r.Kind.HasFile() {
		data = rq.File
	}
	dir := store.Label{Company: co.Name, GSTIN: co.GSTIN, Source: src}.Dir(root)
	return (auth.Store{Dir: dir, Box: box}).Add(r, data)
}

// AnnualStore is where the yearly declaration covering every company is kept (the data folder).
func AnnualStore(root string, box *crypt.Box) auth.Store { return auth.Store{Dir: root, Box: box} }

// HasAnnual reports whether this financial year's yearly declaration is recorded.
func HasAnnual(root string, box *crypt.Box, now time.Time) bool {
	_, err := AnnualStore(root, box).Valid(auth.AllCompanies, now)
	return err == nil
}

// AnnualTexts returns the yearly declaration for a kind (self or owner).
func AnnualTexts(lang string, kind auth.Kind) AuthText {
	t := AuthTexts(lang, kind)
	L := i18n.Of(lang)
	if kind == auth.Owner {
		t.Declaration = i18n.T(L, "owner_declaration_all")
	} else {
		t.Declaration = i18n.T(L, "self_declaration_all")
	}
	return t
}

// RecordAnnual records the yearly declaration (self-certification or owner's declaration)
// covering every company connected this financial year. typed must be the user's own words.
func RecordAnnual(root string, kind auth.Kind, typed string, now time.Time) error {
	cfg, _ := config.Load(root)
	L := i18n.Of(cfg.Language)
	if kind != auth.Owner {
		kind = auth.Self
	}
	ok := false
	for _, w := range i18n.ConfirmWords[L] {
		ok = ok || strings.EqualFold(strings.TrimSpace(typed), w)
	}
	if !ok {
		return fmt.Errorf("%s", i18n.T(L, "not_accepted"))
	}
	box, err := crypt.LoadOrCreate(root)
	if err != nil {
		return err
	}
	t := AnnualTexts(cfg.Language, kind)
	r := auth.Record{Company: auth.AllCompanies, Kind: kind, FY: auth.FY(now), AcceptedAt: now,
		NoticeLang: string(L), NoticeSHA256: auth.Sum([]byte(t.Notice + "\n" + t.Declaration))}
	return AnnualStore(root, box).Add(r, nil)
}
