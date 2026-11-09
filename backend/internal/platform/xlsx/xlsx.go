// Package xlsx writes and reads simple Excel workbooks: sheets of text and
// numbers, a bold first row, nothing else (D-159). It is small on purpose:
// a mark sheet a teacher downloads, fills in and uploads again.
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// Cell is text, or a number when Num is set.
type Cell struct {
	Text string
	Num  *float64
}

// Text and Number make cells.
func Text(s string) Cell { return Cell{Text: s} }
func Number(f float64) Cell {
	return Cell{Num: &f}
}

// Sheet is one tab: its first row is the heading and shows in bold.
type Sheet struct {
	Name   string
	Widths []float64 // column widths in characters; 0 or missing: Excel's
	Rows   [][]Cell
}

// ContentType is an Excel workbook's media type.
const ContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// ColName is a column's letters: 0 is A, 26 is AA.
func ColName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// Write makes a workbook of the sheets.
func Write(sheets []Sheet) ([]byte, error) {
	if len(sheets) == 0 {
		return nil, errors.New("xlsx: no sheets")
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	put := func(name, body string) error {
		w, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+"\n"+body)
		return err
	}
	var types, wbSheets, rels strings.Builder
	for i, sh := range sheets {
		n := i + 1
		fmt.Fprintf(&types, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&wbSheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, esc(sheetName(sh.Name, n)), n, n)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
	}
	fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, len(sheets)+1)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` + types.String() + `</Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` + wbSheets.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + `</Relationships>`},
		// Style 1 is bold, for headings; style 2 is text, so IDs keep their zeros.
		{"xl/styles.xml", `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="3"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/><xf numFmtId="49" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/></cellXfs></styleSheet>`},
	}
	for _, p := range parts {
		if err := put(p.name, p.body); err != nil {
			return nil, err
		}
	}
	for i, sh := range sheets {
		if err := put(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), sheetXML(sh)); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sheetName keeps to Excel's rules: 1-31 characters, none of []:*?/\.
func sheetName(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 31 {
		s = string(r[:31])
	}
	if strings.TrimSpace(s) == "" {
		s = fmt.Sprintf("Sheet%d", n)
	}
	return s
}

func sheetXML(sh Sheet) string {
	var b strings.Builder
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	b.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	if len(sh.Widths) > 0 {
		b.WriteString(`<cols>`)
		for i, w := range sh.Widths {
			if w > 0 {
				fmt.Fprintf(&b, `<col min="%d" max="%d" width="%g" customWidth="1"/>`, i+1, i+1, w)
			}
		}
		b.WriteString(`</cols>`)
	}
	b.WriteString(`<sheetData>`)
	for r, row := range sh.Rows {
		fmt.Fprintf(&b, `<row r="%d">`, r+1)
		for c, cell := range row {
			ref := ColName(c) + strconv.Itoa(r+1)
			switch {
			case cell.Num != nil:
				fmt.Fprintf(&b, `<c r="%s"><v>%s</v></c>`, ref, strconv.FormatFloat(*cell.Num, 'f', -1, 64))
			case cell.Text == "":
				continue
			default:
				style := 2
				if r == 0 {
					style = 1
				}
				fmt.Fprintf(&b, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, esc(cell.Text))
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

// ReadSheet is a sheet as read: its name and rows of cell text. Numbers come
// as Excel stores them ("7.5"); empty cells are "".
type ReadSheet struct {
	Name string
	Rows [][]string
}

// ErrNotWorkbook is returned for a file that isn't an Excel workbook.
var ErrNotWorkbook = errors.New("this isn't an Excel workbook (.xlsx)")

const maxPart = 20 << 20 // no part of a mark sheet is anywhere near this

// Read gives the workbook's sheets in order.
func Read(data []byte) ([]ReadSheet, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrNotWorkbook
	}
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[strings.TrimPrefix(f.Name, "/")] = f
	}
	open := func(name string) ([]byte, error) {
		f := files[name]
		if f == nil {
			return nil, ErrNotWorkbook
		}
		if f.UncompressedSize64 > maxPart {
			return nil, errors.New("this workbook is too big")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, ErrNotWorkbook
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, maxPart))
	}

	var wb struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	raw, err := open("xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	if xml.Unmarshal(raw, &wb) != nil {
		return nil, ErrNotWorkbook
	}
	var rels struct {
		Rel []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if raw, err = open("xl/_rels/workbook.xml.rels"); err != nil {
		return nil, err
	}
	if xml.Unmarshal(raw, &rels) != nil {
		return nil, ErrNotWorkbook
	}
	target := map[string]string{}
	for _, r := range rels.Rel {
		t := r.Target
		if strings.HasPrefix(t, "/") {
			t = strings.TrimPrefix(t, "/")
		} else {
			t = path.Join("xl", t)
		}
		target[r.ID] = t
	}

	var shared []string
	if raw, err := open("xl/sharedStrings.xml"); err == nil {
		var sst struct {
			SI []richText `xml:"si"`
		}
		if xml.Unmarshal(raw, &sst) != nil {
			return nil, ErrNotWorkbook
		}
		for _, si := range sst.SI {
			shared = append(shared, si.String())
		}
	}

	out := []ReadSheet{}
	for _, s := range wb.Sheets {
		raw, err := open(target[s.RID])
		if err != nil {
			return nil, err
		}
		rows, err := readRows(raw, shared)
		if err != nil {
			return nil, err
		}
		out = append(out, ReadSheet{Name: s.Name, Rows: rows})
	}
	return out, nil
}

// richText is a string item: plain, or in runs.
type richText struct {
	T string `xml:"t"`
	R []struct {
		T string `xml:"t"`
	} `xml:"r"`
}

func (x richText) String() string {
	if len(x.R) == 0 {
		return x.T
	}
	var b strings.Builder
	for _, r := range x.R {
		b.WriteString(r.T)
	}
	return b.String()
}

func readRows(raw []byte, shared []string) ([][]string, error) {
	var ws struct {
		Rows []struct {
			R     int `xml:"r,attr"`
			Cells []struct {
				Ref string   `xml:"r,attr"`
				T   string   `xml:"t,attr"`
				V   string   `xml:"v"`
				Is  richText `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if xml.Unmarshal(raw, &ws) != nil {
		return nil, ErrNotWorkbook
	}
	var rows [][]string
	for i, row := range ws.Rows {
		n := row.R
		if n <= 0 {
			n = len(rows) + 1
		}
		if n > 100000 {
			return nil, errors.New("this workbook has too many rows")
		}
		for len(rows) < n {
			rows = append(rows, nil)
		}
		_ = i
		var cells []string
		for j, c := range row.Cells {
			col := j
			if c.Ref != "" {
				col = colIndex(c.Ref)
			}
			if col < 0 || col > 1000 {
				continue
			}
			var v string
			switch c.T {
			case "s":
				k, err := strconv.Atoi(strings.TrimSpace(c.V))
				if err == nil && k >= 0 && k < len(shared) {
					v = shared[k]
				}
			case "inlineStr":
				v = c.Is.String()
			case "b":
				v = map[string]string{"1": "TRUE", "0": "FALSE"}[c.V]
			case "e":
				v = ""
			default:
				v = c.V
			}
			for len(cells) <= col {
				cells = append(cells, "")
			}
			cells[col] = v
		}
		rows[n-1] = cells
	}
	return rows, nil
}

// colIndex is a cell reference's column: "C7" is 2.
func colIndex(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
	}
	return n - 1
}
