// Package xlsx writes simple Excel workbooks (.xlsx) with the standard library only: plain
// sheets, a bold header row, text and number cells. Rows are written as they come, so a
// large Day Book does not sit in memory.
package xlsx

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Workbook is an .xlsx file being written. Sheets are written one after another.
type Workbook struct {
	f      *os.File
	z      *zip.Writer
	sheets []string
	cur    *bufio.Writer
	row    int
}

// Create starts a workbook at path.
func Create(path string) (*Workbook, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &Workbook{f: f, z: zip.NewWriter(f)}, nil
}

// Sheet starts a new sheet with a bold header row; it ends the previous sheet.
func (w *Workbook) Sheet(name string, header ...string) error {
	if err := w.endSheet(); err != nil {
		return err
	}
	name = sheetName(name)
	w.sheets = append(w.sheets, name)
	fw, err := w.z.Create(fmt.Sprintf("xl/worksheets/sheet%d.xml", len(w.sheets)))
	if err != nil {
		return err
	}
	w.cur, w.row = bufio.NewWriter(fw), 0
	w.cur.WriteString(xml.Header + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><sheetData>`)
	if len(header) > 0 {
		cells := make([]any, len(header))
		for i, h := range header {
			cells[i] = h
		}
		return w.write(cells, true)
	}
	return nil
}

// Row writes one row. Cells may be string, int, int64 or float64; Money writes paise as rupees.
func (w *Workbook) Row(cells ...any) error { return w.write(cells, false) }

// Money is an amount in paise, written as a rupee number with two decimals.
type Money int64

func (w *Workbook) write(cells []any, bold bool) error {
	if w.cur == nil {
		return fmt.Errorf("xlsx: Row before Sheet")
	}
	w.row++
	fmt.Fprintf(w.cur, `<row r="%d">`, w.row)
	for i, c := range cells {
		ref := col(i) + strconv.Itoa(w.row)
		style := ""
		if bold {
			style = ` s="1"`
		}
		switch v := c.(type) {
		case Money:
			fmt.Fprintf(w.cur, `<c r="%s" s="2"><v>%s</v></c>`, ref, rupees(int64(v)))
		case int:
			fmt.Fprintf(w.cur, `<c r="%s"%s><v>%d</v></c>`, ref, style, v)
		case int64:
			fmt.Fprintf(w.cur, `<c r="%s"%s><v>%d</v></c>`, ref, style, v)
		case float64:
			fmt.Fprintf(w.cur, `<c r="%s"%s><v>%s</v></c>`, ref, style, strconv.FormatFloat(v, 'f', -1, 64))
		case nil:
		default:
			fmt.Fprintf(w.cur, `<c r="%s" t="inlineStr"%s><is><t xml:space="preserve">`, ref, style)
			xml.EscapeText(w.cur, []byte(fmt.Sprint(v)))
			w.cur.WriteString(`</t></is></c>`)
		}
	}
	_, err := w.cur.WriteString(`</row>`)
	return err
}

func (w *Workbook) endSheet() error {
	if w.cur == nil {
		return nil
	}
	w.cur.WriteString(`</sheetData></worksheet>`)
	err := w.cur.Flush()
	w.cur = nil
	return err
}

// Close finishes the workbook.
func (w *Workbook) Close() error {
	if err := w.endSheet(); err != nil {
		w.f.Close()
		return err
	}
	if len(w.sheets) == 0 {
		w.Sheet("Sheet1")
		w.endSheet()
	}
	var sheets, rels, types strings.Builder
	for i, n := range w.sheets {
		var esc strings.Builder
		xml.EscapeText(&esc, []byte(n))
		fmt.Fprintf(&sheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, esc.String(), i+1, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
		fmt.Fprintf(&types, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i+1)
	}
	n := len(w.sheets)
	files := map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
			types.String() + `</Types>`,
		"_rels/.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` +
			sheets.String() + `</sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() +
			fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, n+1) + `</Relationships>`,
		// Style 1: bold header. Style 2: number with two decimals and thousands separator.
		"xl/styles.xml": `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
			`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
			`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
			`<borders count="1"><border/></borders><cellStyleXfs count="1"><xf/></cellStyleXfs>` +
			`<cellXfs count="3"><xf/><xf fontId="1" applyFont="1"/><xf numFmtId="4" applyNumberFormat="1"/></cellXfs></styleSheet>`,
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml"} {
		fw, err := w.z.Create(name)
		if err != nil {
			w.f.Close()
			return err
		}
		io.WriteString(fw, xml.Header+files[name])
	}
	if err := w.z.Close(); err != nil {
		w.f.Close()
		return err
	}
	return w.f.Close()
}

// col gives the column letters for index i (0 -> A, 26 -> AA).
func col(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

func rupees(p int64) string {
	neg := ""
	if p < 0 {
		neg, p = "-", -p
	}
	return fmt.Sprintf("%s%d.%02d", neg, p/100, p%100)
}

// sheetName keeps Excel's rules: at most 31 characters, none of []:*?/\ .
func sheetName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '-'
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 31 {
		s = string(r[:31])
	}
	if s == "" {
		s = "Sheet"
	}
	return s
}
