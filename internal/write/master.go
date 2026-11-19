package write

import (
	"fmt"
	"strings"

	"github.com/mayagurutech/tallyguru/internal/gst"
)

// Master kinds.
const (
	KLedger     = "ledger"
	KGroup      = "group"
	KStockItem  = "stock_item"
	KUnit       = "unit"
	KCostCentre = "cost_centre"
)

// Master is a draft create/alter of a master. Empty fields are left unchanged on alter.
type Master struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	Alter    bool   `json:"alter,omitempty"`
	Name     string `json:"name"`               // existing name when altering
	NewName  string `json:"new_name,omitempty"` // rename
	Parent   string `json:"parent,omitempty"`   // group / stock group / parent cost centre
	Opening  *int64 `json:"opening,omitempty"`  // paise, Tally sign (negative = Dr)
	GSTIN    string `json:"gstin,omitempty"`
	State    string `json:"state,omitempty"`
	Unit     string `json:"unit,omitempty"` // stock item base unit
	HSN      string `json:"hsn,omitempty"`
	Decimal  int    `json:"decimals,omitempty"`  // unit
	Before   string `json:"before,omitempty"`    // current values when altering (undo record)
	CurGSTIN string `json:"cur_gstin,omitempty"` // existing GSTIN, kept when only the state changes
	From     string `json:"from,omitempty"`      // books-from date for dated GST/mailing details
}

// MasterFromArgs reads a master draft from tool arguments.
func MasterFromArgs(a map[string]any) (Master, error) {
	s := func(k string) string { x, _ := a[k].(string); return strings.TrimSpace(x) }
	m := Master{Kind: s("kind"), Name: s("name"), NewName: s("new_name"), Parent: s("parent"),
		GSTIN: strings.ToUpper(s("gstin")), State: s("state"), Unit: s("unit"), HSN: s("hsn")}
	m.Alter = s("action") == "alter"
	if a := s("action"); a != "" && a != "alter" && a != "create" {
		return m, fmt.Errorf("action is create or alter")
	}
	switch m.Kind {
	case KLedger, KGroup, KStockItem, KUnit, KCostCentre:
	default:
		return m, fmt.Errorf("kind must be ledger, group, stock_item, unit or cost_centre")
	}
	if m.Name == "" {
		return m, fmt.Errorf("name is required")
	}
	if dr, cr := a["opening_dr"], a["opening_cr"]; dr != nil || cr != nil {
		d, err1 := Rupees(dr)
		c, err2 := Rupees(cr)
		if err := firstErr(err1, err2); err != nil {
			return m, err
		}
		o := c - d
		m.Opening = &o
	}
	if f, ok := a["decimals"].(float64); ok {
		m.Decimal = int(f)
	}
	return m, nil
}

// CheckMaster validates a master draft and fills Before for alters.
func (b *Book) CheckMaster(m *Master) (errs, warns []string) {
	exists := b.masterExists(m.Kind, m.Name)
	switch {
	case m.Alter && exists == "" && (m.Kind == KLedger || m.Kind == KGroup || m.Kind == KStockItem):
		errs = append(errs, fmt.Sprintf("%s %q not found to alter%s", m.Kind, m.Name, b.suggestFor(m.Kind, m.Name)))
	case !m.Alter && exists != "":
		errs = append(errs, fmt.Sprintf("%s %q already exists (use action alter)", m.Kind, exists))
	}
	if exists != "" {
		m.Name = exists // Tally's spelling
	}
	if m.NewName != "" && b.masterExists(m.Kind, m.NewName) != "" {
		errs = append(errs, fmt.Sprintf("new name %q already exists", m.NewName))
	}
	// Parent
	switch m.Kind {
	case KLedger, KGroup:
		if m.Parent == "" && !m.Alter {
			errs = append(errs, "parent group is required")
		} else if m.Parent != "" {
			if g := b.group(m.Parent); g == "" {
				errs = append(errs, fmt.Sprintf("group %q not found%s", m.Parent, b.suggestFor(KGroup, m.Parent)))
			} else {
				m.Parent = g
			}
		}
	case KStockItem:
		if m.Unit == "" && !m.Alter {
			errs = append(errs, "unit is required (e.g. Nos, Kg)")
		}
	}
	if m.Opening != nil && m.Kind != KLedger {
		errs = append(errs, "opening balance is only for ledgers here")
	}
	// GST
	if m.GSTIN != "" {
		if m.Kind != KLedger {
			errs = append(errs, "GSTIN is only for ledgers")
		} else if p := gst.Problem(m.GSTIN); p != "" {
			errs = append(errs, "GSTIN "+p)
		} else {
			st := gst.StateOf(m.GSTIN)
			if m.State == "" {
				m.State = st
			} else if !gst.SameState(st, m.State) {
				errs = append(errs, fmt.Sprintf("GSTIN is for %s but state given is %s", st, m.State))
			}
		}
	}
	if m.State != "" {
		if s := canonState(m.State); s == "" {
			errs = append(errs, fmt.Sprintf("state %q not recognised", m.State))
		} else {
			m.State = s
		}
	}
	if m.Kind == KLedger && m.Parent != "" && !m.Alter {
		if g := strings.ToLower(m.Parent); (strings.Contains(g, "debtor") || strings.Contains(g, "creditor")) && m.State == "" {
			warns = append(warns, "no state or GSTIN: GST checks (IGST vs CGST+SGST) need the party's state")
		}
	}
	m.From = strings.ReplaceAll(b.from, "-", "")
	if m.From == "" {
		m.From = "20170701"
	}
	if m.HSN != "" && m.Kind != KStockItem {
		errs = append(errs, "HSN is only for stock items here")
	}
	if m.Alter && exists != "" {
		m.Before = b.describe(m.Kind, exists)
		if m.Kind == KLedger {
			b.db.QueryRow(`SELECT gstin FROM ledger_party WHERE name=?`, exists).Scan(&m.CurGSTIN)
		}
	}
	if m.Alter && m.NewName == "" && m.Parent == "" && m.Opening == nil && m.GSTIN == "" && m.State == "" && m.Unit == "" && m.HSN == "" {
		errs = append(errs, "nothing to change")
	}
	return
}

func canonState(s string) string {
	for _, n := range gst.States {
		if gst.SameState(n, s) {
			return n
		}
	}
	return ""
}

func (b *Book) masterExists(kind, name string) string {
	low := strings.ToLower(name)
	switch kind {
	case KLedger:
		return b.lower[low]
	case KGroup:
		return b.group(name)
	case KStockItem:
		for n := range b.items {
			if strings.ToLower(n) == low {
				return n
			}
		}
	}
	return "" // units and cost centres are not synced: Tally itself refuses duplicates
}

func (b *Book) group(name string) string {
	var g string
	b.db.QueryRow(`SELECT name FROM groups WHERE name = ? COLLATE NOCASE`, name).Scan(&g)
	return g
}

func (b *Book) suggestFor(kind, name string) string {
	if kind == KLedger {
		return b.suggest(name)
	}
	if kind == KGroup {
		rows, err := b.db.Query(`SELECT name FROM groups WHERE name LIKE ? LIMIT 5`, "%"+firstWord(name)+"%")
		if err != nil {
			return ""
		}
		defer rows.Close()
		var hits []string
		for rows.Next() {
			var n string
			rows.Scan(&n)
			hits = append(hits, n)
		}
		if len(hits) > 0 {
			return " (did you mean: " + strings.Join(hits, ", ") + "?)"
		}
	}
	return ""
}

func firstWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return s
	}
	return f[0]
}

func (b *Book) describe(kind, name string) string {
	switch kind {
	case KLedger:
		var parent, gstin, state string
		var opening int64
		b.db.QueryRow(`SELECT l.parent, l.opening, COALESCE(p.gstin,''), COALESCE(p.state,'') FROM ledgers l
			LEFT JOIN ledger_party p ON p.name=l.name WHERE l.name=?`, name).Scan(&parent, &opening, &gstin, &state)
		return fmt.Sprintf("parent %s, opening %s, GSTIN %s, state %s", parent, drcr(opening), dashS(gstin), dashS(state))
	case KGroup:
		var parent string
		b.db.QueryRow(`SELECT parent FROM groups WHERE name=?`, name).Scan(&parent)
		return "parent " + dashS(parent)
	case KStockItem:
		var parent, unit string
		b.db.QueryRow(`SELECT parent, unit FROM stock_items WHERE name=?`, name).Scan(&parent, &unit)
		return fmt.Sprintf("group %s, unit %s", dashS(parent), dashS(unit))
	}
	return ""
}

func drcr(p int64) string {
	if p == 0 {
		return "0"
	}
	if p < 0 {
		return rs(p) + " Dr"
	}
	return rs(p) + " Cr"
}

func dashS(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Preview of a master draft.
func (m *Master) Preview() string {
	var b strings.Builder
	verb := "Create"
	if m.Alter {
		verb = "Alter"
	}
	fmt.Fprintf(&b, "Draft %d: %s %s %q", m.ID, verb, strings.ReplaceAll(m.Kind, "_", " "), m.Name)
	var ch []string
	add := func(k, v string) {
		if v != "" {
			ch = append(ch, k+" "+v)
		}
	}
	add("rename to", m.NewName)
	add("under", m.Parent)
	if m.Opening != nil {
		add("opening", drcr(*m.Opening))
	}
	add("GSTIN", m.GSTIN)
	add("state", m.State)
	add("unit", m.Unit)
	add("HSN", m.HSN)
	if len(ch) > 0 {
		b.WriteString(": " + strings.Join(ch, ", "))
	}
	if m.Before != "" {
		b.WriteString("\n  Now: " + m.Before)
	}
	return b.String()
}

// MasterXML builds the import request for one master.
func MasterXML(company string, m Master) string {
	action := "Create"
	if m.Alter {
		action = "Alter"
	}
	name := m.Name
	if m.NewName != "" {
		name = m.NewName
	}
	nameList := fmt.Sprintf("<NAME.LIST><NAME>%s</NAME></NAME.LIST>", esc(name))
	var x strings.Builder
	switch m.Kind {
	case KLedger:
		fmt.Fprintf(&x, `<LEDGER NAME="%s" ACTION="%s">%s`, esc(m.Name), action, nameList)
		if m.Parent != "" {
			fmt.Fprintf(&x, "<PARENT>%s</PARENT>", esc(m.Parent))
		}
		if m.Opening != nil {
			fmt.Fprintf(&x, "<OPENINGBALANCE>%s</OPENINGBALANCE><ISDEEMEDPOSITIVE>%s</ISDEEMEDPOSITIVE>", tallyAmt(*m.Opening), yn(*m.Opening < 0))
		}
		if m.State != "" || m.GSTIN != "" {
			// TallyPrime keeps GST and address details as dated lists; the old single fields are
			// sent too for older releases.
			g := m.GSTIN
			if g == "" {
				g = m.CurGSTIN
			}
			reg := "Unregistered/Consumer"
			if g != "" {
				reg = "Regular"
				fmt.Fprintf(&x, "<PARTYGSTIN>%s</PARTYGSTIN>", esc(g))
			}
			fmt.Fprintf(&x, "<GSTREGISTRATIONTYPE>%s</GSTREGISTRATIONTYPE>", reg)
			if m.State != "" {
				fmt.Fprintf(&x, "<LEDSTATENAME>%s</LEDSTATENAME><COUNTRYOFRESIDENCE>India</COUNTRYOFRESIDENCE>", esc(m.State))
			}
			fmt.Fprintf(&x, "<LEDGSTREGDETAILS.LIST><APPLICABLEFROM>%s</APPLICABLEFROM><GSTREGISTRATIONTYPE>%s</GSTREGISTRATIONTYPE>", m.From, reg)
			if m.State != "" {
				fmt.Fprintf(&x, "<STATE>%s</STATE><PLACEOFSUPPLY>%s</PLACEOFSUPPLY>", esc(m.State), esc(m.State))
			}
			if g != "" {
				fmt.Fprintf(&x, "<GSTIN>%s</GSTIN>", esc(g))
			}
			x.WriteString("</LEDGSTREGDETAILS.LIST>")
			if m.State != "" {
				fmt.Fprintf(&x, "<LEDMAILINGDETAILS.LIST><APPLICABLEFROM>%s</APPLICABLEFROM><STATE>%s</STATE><COUNTRY>India</COUNTRY></LEDMAILINGDETAILS.LIST>", m.From, esc(m.State))
			}
		}
		x.WriteString("</LEDGER>")
	case KGroup:
		fmt.Fprintf(&x, `<GROUP NAME="%s" ACTION="%s">%s`, esc(m.Name), action, nameList)
		if m.Parent != "" {
			fmt.Fprintf(&x, "<PARENT>%s</PARENT>", esc(m.Parent))
		}
		x.WriteString("</GROUP>")
	case KStockItem:
		fmt.Fprintf(&x, `<STOCKITEM NAME="%s" ACTION="%s">%s`, esc(m.Name), action, nameList)
		if m.Parent != "" {
			fmt.Fprintf(&x, "<PARENT>%s</PARENT>", esc(m.Parent))
		}
		if m.Unit != "" {
			fmt.Fprintf(&x, "<BASEUNITS>%s</BASEUNITS>", esc(m.Unit))
		}
		if m.HSN != "" {
			fmt.Fprintf(&x, "<GSTDETAILS.LIST><APPLICABLEFROM>20170701</APPLICABLEFROM><HSNCODE>%s</HSNCODE></GSTDETAILS.LIST>", esc(m.HSN))
		}
		x.WriteString("</STOCKITEM>")
	case KUnit:
		fmt.Fprintf(&x, `<UNIT NAME="%s" ACTION="%s"><NAME>%s</NAME><ISSIMPLEUNIT>Yes</ISSIMPLEUNIT><DECIMALPLACES>%d</DECIMALPLACES></UNIT>`,
			esc(m.Name), action, esc(name), m.Decimal)
	case KCostCentre:
		fmt.Fprintf(&x, `<COSTCENTRE NAME="%s" ACTION="%s">%s<CATEGORY>Primary Cost Category</CATEGORY>`, esc(m.Name), action, nameList)
		if m.Parent != "" {
			fmt.Fprintf(&x, "<PARENT>%s</PARENT>", esc(m.Parent))
		}
		x.WriteString("</COSTCENTRE>")
	}
	return envelope(company, "All Masters", "<TALLYMESSAGE>"+x.String()+"</TALLYMESSAGE>")
}
