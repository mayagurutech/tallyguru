// Package write builds, checks and posts entries to Tally. Every write goes
// draft -> preview -> post by draft ID; nothing reaches Tally without a preview.
package write

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Remark is added to every narration so the entry can be traced in Tally.
const Remark = "Posted via LedgerGuru"

// Marker is the per-draft tag in the narration, used to find the voucher after posting.
func Marker(id int64) string { return fmt.Sprintf("[TMG-%d]", id) }

func esc(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// tallyAmt formats paise with Tally's sign (negative = debit).
func tallyAmt(p int64) string {
	s := ""
	if p < 0 {
		s, p = "-", -p
	}
	return fmt.Sprintf("%s%d.%02d", s, p/100, p%100)
}

func yn(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func envelope(company, report, data string) string {
	return "<ENVELOPE><HEADER><TALLYREQUEST>Import Data</TALLYREQUEST></HEADER><BODY><IMPORTDATA>" +
		"<REQUESTDESC><REPORTNAME>" + report + "</REPORTNAME><STATICVARIABLES><SVCURRENTCOMPANY>" +
		esc(company) + "</SVCURRENTCOMPANY></STATICVARIABLES></REQUESTDESC><REQUESTDATA>" + data +
		"</REQUESTDATA></IMPORTDATA></BODY></ENVELOPE>"
}

// Narration is the text sent to Tally: the user's narration, the remark and the marker.
func (v *Voucher) Narration() string {
	n := strings.TrimSpace(v.Narr)
	if n != "" && !strings.HasSuffix(n, ".") {
		n += "."
	}
	return strings.TrimSpace(n + " " + Remark + " " + Marker(v.ID))
}

// VouchersXML builds one import request for a batch of vouchers.
func VouchersXML(company string, vs []Voucher) string {
	var b strings.Builder
	for i := range vs {
		v := &vs[i]
		sj := v.Base == "Stock Journal"
		view := "Accounting Voucher View"
		switch {
		case sj:
			view = "Consumption Voucher View"
		case len(v.Items) > 0:
			view = "Invoice Voucher View"
		}
		d := strings.ReplaceAll(v.Date, "-", "")
		fmt.Fprintf(&b, `<TALLYMESSAGE><VOUCHER VCHTYPE="%s" ACTION="Create" OBJVIEW="%s">`+
			`<DATE>%s</DATE><EFFECTIVEDATE>%s</EFFECTIVEDATE><VOUCHERTYPENAME>%s</VOUCHERTYPENAME>`,
			esc(v.Type), view, d, d, esc(v.Type))
		if v.Number != "" {
			fmt.Fprintf(&b, "<VOUCHERNUMBER>%s</VOUCHERNUMBER>", esc(v.Number))
		}
		if v.Reference != "" {
			fmt.Fprintf(&b, "<REFERENCE>%s</REFERENCE>", esc(v.Reference))
		}
		fmt.Fprintf(&b, "<PERSISTEDVIEW>%s</PERSISTEDVIEW><NARRATION>%s</NARRATION>", view, esc(v.Narration()))
		if v.Party != "" {
			fmt.Fprintf(&b, "<PARTYLEDGERNAME>%s</PARTYLEDGERNAME>", esc(v.Party))
		}
		if len(v.Items) > 0 && !sj {
			b.WriteString("<ISINVOICE>Yes</ISINVOICE>")
		}
		for _, it := range v.Items {
			if sj {
				// Out: consumption/source (amount +); in: production/destination (amount -).
				tag := "INVENTORYENTRIESIN.LIST"
				if it.Out {
					tag = "INVENTORYENTRIESOUT.LIST"
				}
				fmt.Fprintf(&b, "<%s><STOCKITEMNAME>%s</STOCKITEMNAME><ISDEEMEDPOSITIVE>%s</ISDEEMEDPOSITIVE><RATE>%s/%s</RATE><AMOUNT>%s</AMOUNT>"+
					"<ACTUALQTY>%s %s</ACTUALQTY><BILLEDQTY>%s %s</BILLEDQTY></%s>",
					tag, esc(it.Item), yn(!it.Out), tallyAmt(abs(it.Rate)), esc(it.Unit), tallyAmt(it.Amount), it.Qty, esc(it.Unit), it.Qty, esc(it.Unit), tag)
				continue
			}
			// Inventory line: amount with the sign of its accounting line (sales Cr +, purchase Dr -).
			fmt.Fprintf(&b, "<ALLINVENTORYENTRIES.LIST><STOCKITEMNAME>%s</STOCKITEMNAME><ISDEEMEDPOSITIVE>%s</ISDEEMEDPOSITIVE>"+
				"<RATE>%s/%s</RATE><AMOUNT>%s</AMOUNT><ACTUALQTY>%s %s</ACTUALQTY><BILLEDQTY>%s %s</BILLEDQTY>"+
				"<ACCOUNTINGALLOCATIONS.LIST><LEDGERNAME>%s</LEDGERNAME><ISDEEMEDPOSITIVE>%s</ISDEEMEDPOSITIVE><AMOUNT>%s</AMOUNT></ACCOUNTINGALLOCATIONS.LIST>"+
				"</ALLINVENTORYENTRIES.LIST>",
				esc(it.Item), yn(it.Amount < 0), tallyAmt(abs(it.Rate)), esc(it.Unit), tallyAmt(it.Amount),
				it.Qty, esc(it.Unit), it.Qty, esc(it.Unit),
				esc(it.Ledger), yn(it.Amount < 0), tallyAmt(it.Amount))
		}
		tag := "ALLLEDGERENTRIES.LIST"
		if len(v.Items) > 0 {
			tag = "LEDGERENTRIES.LIST"
		}
		for _, l := range v.Lines {
			fmt.Fprintf(&b, "<%s><LEDGERNAME>%s</LEDGERNAME><ISDEEMEDPOSITIVE>%s</ISDEEMEDPOSITIVE>"+
				"<ISPARTYLEDGER>%s</ISPARTYLEDGER><AMOUNT>%s</AMOUNT></%s>",
				tag, esc(l.Ledger), yn(l.Amount < 0), yn(l.Ledger == v.Party), tallyAmt(l.Amount), tag)
		}
		b.WriteString("</VOUCHER></TALLYMESSAGE>")
	}
	return envelope(company, "Vouchers", b.String())
}

// Result is Tally's import summary.
type Result struct {
	Created, Altered, Deleted, Cancelled, Errors, Exceptions int
	LineErrors                                               []string
}

func (r Result) String() string {
	s := fmt.Sprintf("created %d, errors %d", r.Created, r.Errors+r.Exceptions)
	if r.Altered+r.Deleted+r.Cancelled > 0 {
		s += fmt.Sprintf(", altered %d, deleted %d, cancelled %d", r.Altered, r.Deleted, r.Cancelled)
	}
	if len(r.LineErrors) > 0 {
		s += ": " + strings.Join(r.LineErrors, "; ")
	}
	return s
}

// ParseResult reads Tally's import <RESPONSE>.
func ParseResult(r io.Reader) (Result, error) {
	var res Result
	dec := xml.NewDecoder(r)
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return res, nil
		}
		if err != nil {
			return res, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		var p *int
		switch se.Name.Local {
		case "CREATED":
			p = &res.Created
		case "ALTERED":
			p = &res.Altered
		case "DELETED":
			p = &res.Deleted
		case "CANCELLED":
			p = &res.Cancelled
		case "ERRORS":
			p = &res.Errors
		case "EXCEPTIONS":
			p = &res.Exceptions
		case "LINEERROR":
			var v string
			dec.DecodeElement(&v, &se)
			res.LineErrors = append(res.LineErrors, strings.TrimSpace(v))
			continue
		default:
			continue
		}
		var v string
		dec.DecodeElement(&v, &se)
		var n int
		fmt.Sscan(strings.TrimSpace(v), &n)
		*p += n
	}
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
