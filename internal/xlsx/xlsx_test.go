package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestCol(t *testing.T) {
	for i, want := range map[int]string{0: "A", 25: "Z", 26: "AA", 51: "AZ", 52: "BA", 701: "ZZ", 702: "AAA"} {
		if got := col(i); got != want {
			t.Errorf("col(%d) = %s, want %s", i, got, want)
		}
	}
	if sheetName("Bills: Receivable/Payable [FY 2026-27] long name") != "Bills- Receivable-Payable -FY 2" {
		t.Errorf("sheet name %q", sheetName("Bills: Receivable/Payable [FY 2026-27] long name"))
	}
}

func TestWorkbook(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.xlsx")
	w, err := Create(p)
	if err != nil {
		t.Fatal(err)
	}
	w.Sheet("Day Book", "Date", "Party", "Amount")
	w.Row("01-04-2026", `Ram & Sons <"Pvt">`, Money(-1234567))
	w.Row("02-04-2026", "Shyam", Money(50))
	w.Sheet("Summary", "Type", "Count")
	w.Row("Sales", 2)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	got := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		got[f.Name] = string(b)
		// Every part must be well-formed XML.
		d := xml.NewDecoder(strings.NewReader(string(b)))
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s: %v", f.Name, err)
			}
		}
	}
	for _, n := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/styles.xml", "xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml"} {
		if _, ok := got[n]; !ok {
			t.Errorf("missing %s", n)
		}
	}
	s := got["xl/worksheets/sheet1.xml"]
	for _, want := range []string{`<c r="C2" s="2"><v>-12345.67</v></c>`, `Ram &amp; Sons &lt;&#34;Pvt&#34;&gt;`, `<c r="C3" s="2"><v>0.50</v></c>`, `s="1"`} {
		if !strings.Contains(s, want) {
			t.Errorf("sheet1 lacks %s", want)
		}
	}
	if !strings.Contains(got["xl/workbook.xml"], `name="Summary"`) {
		t.Error("second sheet not listed")
	}
}
