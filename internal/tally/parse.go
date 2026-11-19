package tally

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Paise is an amount in paise. Tally sign convention kept: negative = debit.
type Paise int64

// Foreign-currency lines read "-$100.00 @ ₹83.00/$ = -₹8,300.00": the rupee figure after "=" is used.
// ParseAmount converts a Tally amount string ("-1,234.50", "1234.5", "") to paise exactly.
func ParseAmount(s string) (Paise, error) {
	orig := s
	if _, after, ok := strings.Cut(s, "="); ok {
		s = after
	}
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Sc, r) || unicode.IsSpace(r) || r == ',' {
			return -1 // currency symbols (₹, $, €), spaces, commas
		}
		return r
	}, s)
	if strings.ContainsAny(orig, "@") && !strings.Contains(orig, "=") {
		return 0, fmt.Errorf("foreign-currency amount without a rupee value: %q", orig)
	}
	if s == "" {
		return 0, nil
	}
	neg := false
	if s[0] == '-' || s[0] == '+' {
		neg = s[0] == '-'
		s = s[1:]
	}
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 {
		// Tally amounts carry at most 2 decimals; round half up on the 3rd.
		up := frac[2] >= '5'
		frac = frac[:2]
		v, err := ParseAmount(sign(neg) + whole + "." + frac)
		if err == nil && up {
			if neg {
				v--
			} else {
				v++
			}
		}
		return v, err
	}
	for len(frac) < 2 {
		frac += "0"
	}
	w, err := strconv.ParseInt(nz(whole), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad amount %q", s)
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad amount %q", s)
	}
	v := Paise(w*100 + f)
	if neg {
		v = -v
	}
	return v, nil
}

func sign(neg bool) string {
	if neg {
		return "-"
	}
	return ""
}
func nz(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// ParseDate reads Tally's YYYYMMDD date.
func ParseDate(s string) (time.Time, error) { return time.Parse("20060102", strings.TrimSpace(s)) }

func yes(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "Yes") }

// Company is one company open in Tally.
type Company struct {
	Name, GUID, GSTIN string
	State             string // the company's state (IGST vs CGST+SGST)
	BooksFrom         time.Time
	AlterID           int64
	// Company-wide change counters: they rise whenever any voucher / master changes.
	AltVchID, AltMstID int64
}

// LedgerLine is one accounting line of a voucher.
type LedgerLine struct {
	Ledger string
	Amount Paise
}

// Voucher holds the header and ledger lines we store.
type Voucher struct {
	GUID, Type, Number, Party, Reference, Narration string
	Date                                            time.Time
	AlterID, MasterID                               int64
	Cancelled, Optional, PostDated                  bool
	Lines                                           []LedgerLine
}

// Ledger master.
type Ledger struct {
	Name, GUID, Parent string
	Opening, Closing   Paise
	AlterID, MasterID  int64
	Party              PartyInfo
}

// PartyInfo is a ledger's GST and residence details (latest registration entry).
type PartyInfo struct {
	GSTIN, State, RegType, Country, Currency string
	Transporter                              bool
}

// each streams every top-level element named tag and decodes it into a fresh T,
// so memory stays at one object at a time regardless of response size.
func each[T any](r io.Reader, tag string, fn func(*T) error) error {
	d := xml.NewDecoder(r)
	d.Strict = false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading tally xml: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "LINEERROR" {
			var msg string
			d.DecodeElement(&msg, &se)
			return fmt.Errorf("tally error: %s", strings.TrimSpace(msg))
		}
		// Tally adds a CMPINFO block with same-named count tags (<COMPANY>0</COMPANY>);
		// only objects inside <COLLECTION> are real data.
		if se.Name.Local == "CMPINFO" {
			d.Skip()
			continue
		}
		if se.Name.Local != tag {
			continue
		}
		v := new(T)
		if err := d.DecodeElement(v, &se); err != nil {
			return fmt.Errorf("decoding %s: %w", tag, err)
		}
		if err := fn(v); err != nil {
			return err
		}
	}
}

type xCompany struct {
	Name      string `xml:"NAME,attr"`
	NameEl    string `xml:"NAME"`
	GUID      string `xml:"GUID"`
	BooksFrom string `xml:"BOOKSFROM"`
	GSTIN     string `xml:"GSTREGISTRATIONNUMBER"`
	State     string `xml:"STATENAME"`
	AlterID   int64  `xml:"ALTERID"`
	AltVch    int64  `xml:"ALTVCHID"`
	AltMst    int64  `xml:"ALTMSTID"`
}

// ParseCompanies streams the company list response.
func ParseCompanies(r io.Reader, fn func(Company) error) error {
	return each(r, "COMPANY", func(x *xCompany) error {
		name := x.Name
		if name == "" {
			name = strings.TrimSpace(x.NameEl)
		}
		bf, _ := ParseDate(x.BooksFrom)
		return fn(Company{Name: name, GUID: strings.TrimSpace(x.GUID), GSTIN: strings.TrimSpace(x.GSTIN),
			State: strings.TrimSpace(x.State), BooksFrom: bf, AlterID: x.AlterID, AltVchID: x.AltVch, AltMstID: x.AltMst})
	})
}

type xLedger struct {
	Name     string `xml:"NAME,attr"`
	GUID     string `xml:"GUID"`
	Parent   string `xml:"PARENT"`
	Opening  string `xml:"OPENINGBALANCE"`
	Closing  string `xml:"CLOSINGBALANCE"`
	AlterID  int64  `xml:"ALTERID"`
	MasterID int64  `xml:"MASTERID"`
	Regs     []struct {
		From        string `xml:"APPLICABLEFROM"`
		GSTIN       string `xml:"GSTIN"`
		RegType     string `xml:"GSTREGISTRATIONTYPE"`
		State       string `xml:"STATE"`
		Transporter string `xml:"ISTRANSPORTER"`
	} `xml:"LEDGSTREGDETAILS.LIST"`
	OldGSTIN string `xml:"PARTYGSTIN"`
	OldState string `xml:"LEDSTATENAME"`
	Country  string `xml:"COUNTRYOFRESIDENCE"`
	Currency string `xml:"CURRENCYNAME"`
}

// partyInfo takes the latest registration entry (Tally keeps a dated history), with the
// older single fields as fallback.
func (x *xLedger) partyInfo() PartyInfo {
	p := PartyInfo{GSTIN: strings.TrimSpace(x.OldGSTIN), State: strings.TrimSpace(x.OldState),
		Country: strings.TrimSpace(x.Country), Currency: strings.TrimSpace(x.Currency)}
	best := ""
	for _, r := range x.Regs {
		d := ""
		if t, err := ParseDate(r.From); err == nil {
			d = t.Format("20060102")
		}
		if d < best {
			continue
		}
		best = d
		if g := strings.TrimSpace(r.GSTIN); g != "" {
			p.GSTIN = g
		}
		if s := strings.TrimSpace(r.State); s != "" {
			p.State = s
		}
		if t := strings.TrimSpace(r.RegType); t != "" {
			p.RegType = t
		}
		p.Transporter = strings.EqualFold(strings.TrimSpace(r.Transporter), "Yes")
	}
	if p.Currency == "?" {
		p.Currency = ""
	}
	return p
}

// ParseLedgers streams the ledger response.
func ParseLedgers(r io.Reader, fn func(Ledger) error) error {
	return each(r, "LEDGER", func(x *xLedger) error {
		op, err := ParseAmount(x.Opening)
		if err != nil {
			return err
		}
		cl, err := ParseAmount(x.Closing)
		if err != nil {
			return err
		}
		return fn(Ledger{Name: x.Name, GUID: strings.TrimSpace(x.GUID), Parent: strings.TrimSpace(x.Parent),
			Opening: op, Closing: cl, AlterID: x.AlterID, MasterID: x.MasterID, Party: x.partyInfo()})
	})
}

type xLine struct {
	Ledger string `xml:"LEDGERNAME"`
	Amount string `xml:"AMOUNT"`
}

type xVoucher struct {
	Date      string  `xml:"DATE"`
	GUID      string  `xml:"GUID"`
	AlterID   int64   `xml:"ALTERID"`
	MasterID  int64   `xml:"MASTERID"`
	Type      string  `xml:"VOUCHERTYPENAME"`
	Number    string  `xml:"VOUCHERNUMBER"`
	Party     string  `xml:"PARTYLEDGERNAME"`
	Reference string  `xml:"REFERENCE"`
	Narration string  `xml:"NARRATION"`
	Cancelled string  `xml:"ISCANCELLED"`
	Optional  string  `xml:"ISOPTIONAL"`
	PostDated string  `xml:"ISPOSTDATED"`
	Lines     []xLine `xml:"ALLLEDGERENTRIES.LIST"`
	Lines2    []xLine `xml:"LEDGERENTRIES.LIST"`
}

// ParseVouchers streams the voucher response one voucher at a time.
func ParseVouchers(r io.Reader, fn func(Voucher) error) error {
	return each(r, "VOUCHER", func(x *xVoucher) error {
		dt, err := ParseDate(x.Date)
		if err != nil {
			return fmt.Errorf("voucher %s: bad date %q", x.GUID, x.Date)
		}
		v := Voucher{GUID: strings.TrimSpace(x.GUID), Type: x.Type, Number: x.Number, Party: x.Party,
			Reference: x.Reference, Narration: x.Narration, Date: dt, AlterID: x.AlterID,
			MasterID: x.MasterID, Cancelled: yes(x.Cancelled), Optional: yes(x.Optional), PostDated: yes(x.PostDated)}
		for _, l := range append(x.Lines, x.Lines2...) {
			a, err := ParseAmount(l.Amount)
			if err != nil {
				return fmt.Errorf("voucher %s: %w", v.GUID, err)
			}
			v.Lines = append(v.Lines, LedgerLine{Ledger: strings.TrimSpace(l.Ledger), Amount: a})
		}
		return fn(v)
	})
}

// Group is an account group. Parent "" means primary.
type Group struct {
	Name, Parent                  string
	IsRevenue, AffectsGrossProfit bool
}

type xGroup struct {
	Name     string `xml:"NAME,attr"`
	Parent   string `xml:"PARENT"`
	Revenue  string `xml:"ISREVENUE"`
	AffectGP string `xml:"AFFECTSGROSSPROFIT"`
}

// primaryParent normalises Tally's "&#4; Primary" (sanitised to " Primary") to "".
func primaryParent(p string) string {
	p = strings.TrimSpace(p)
	if p == "Primary" {
		return ""
	}
	return p
}

// ParseGroups streams groups.
func ParseGroups(r io.Reader, fn func(Group) error) error {
	return each(r, "GROUP", func(x *xGroup) error {
		return fn(Group{Name: x.Name, Parent: primaryParent(x.Parent), IsRevenue: yes(x.Revenue), AffectsGrossProfit: yes(x.AffectGP)})
	})
}

// VoucherType maps a voucher type to its parent (base) type.
type VoucherType struct{ Name, Parent string }

type xVType struct {
	Name   string `xml:"NAME,attr"`
	Parent string `xml:"PARENT"`
}

// ParseVoucherTypes streams voucher types.
func ParseVoucherTypes(r io.Reader, fn func(VoucherType) error) error {
	return each(r, "VOUCHERTYPE", func(x *xVType) error {
		return fn(VoucherType{Name: x.Name, Parent: primaryParent(x.Parent)})
	})
}

// StockItem closing position. Quantity is kept as Tally's text (e.g. "10 Nos").
type StockItem struct {
	Name, Parent, Unit, ClosingQty string
	ClosingValue, OpeningValue     Paise
}

type xStock struct {
	Name  string `xml:"NAME,attr"`
	Par   string `xml:"PARENT"`
	Unit  string `xml:"BASEUNITS"`
	Qty   string `xml:"CLOSINGBALANCE"`
	Value string `xml:"CLOSINGVALUE"`
	Open  string `xml:"OPENINGVALUE"`
}

// ParseStockItems streams stock items.
func ParseStockItems(r io.Reader, fn func(StockItem) error) error {
	return each(r, "STOCKITEM", func(x *xStock) error {
		v, err := ParseAmount(x.Value)
		if err != nil {
			return fmt.Errorf("stock item %q: %w", x.Name, err)
		}
		o, err := ParseAmount(x.Open)
		if err != nil {
			return fmt.Errorf("stock item %q opening: %w", x.Name, err)
		}
		return fn(StockItem{OpeningValue: o, Name: x.Name, Parent: primaryParent(x.Par), Unit: strings.TrimSpace(x.Unit),
			ClosingQty: strings.TrimSpace(x.Qty), ClosingValue: v})
	})
}

// Bill is one outstanding bill. Closing uses Tally's sign (negative = Dr, i.e. receivable).
type Bill struct {
	Name, Party string
	Date        time.Time
	Closing     Paise
	Advance     bool
}

type xBill struct {
	Name    string `xml:"NAME,attr"`
	Parent  string `xml:"PARENT"`
	Date    string `xml:"BILLDATE"`
	Closing string `xml:"CLOSINGBALANCE"`
	Advance string `xml:"ISADVANCE"`
}

// ParseBills streams bills.
func ParseBills(r io.Reader, fn func(Bill) error) error {
	return each(r, "BILL", func(x *xBill) error {
		c, err := ParseAmount(x.Closing)
		if err != nil {
			return fmt.Errorf("bill %q: %w", x.Name, err)
		}
		d, _ := ParseDate(x.Date)
		return fn(Bill{Name: x.Name, Party: strings.TrimSpace(x.Parent), Date: d, Closing: c, Advance: yes(x.Advance)})
	})
}
