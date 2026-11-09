package xlsx

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	data, err := Write([]Sheet{
		{Name: "Students", Widths: []float64{6, 20}, Rows: [][]Cell{
			{Text("No."), Text("Name"), Text("Student ID"), Text("Marks")},
			{Number(1), Text("Amaya <Silva> & co"), Text("007"), Number(7.5)},
			{Number(2), Text("නෙත්මි"), Text(""), Text("")},
		}},
		{Name: "Groups: [all]", Rows: [][]Cell{{Text("Group")}, {Text("Lions")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sheets, err := Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 2 || sheets[0].Name != "Students" || sheets[1].Name != "Groups all" {
		t.Fatalf("sheets = %+v", sheets)
	}
	r := sheets[0].Rows
	if len(r) != 3 || r[1][1] != "Amaya <Silva> & co" || r[1][2] != "007" || r[1][3] != "7.5" || r[2][1] != "නෙත්මි" || r[0][3] != "Marks" {
		t.Fatalf("rows = %q", r)
	}
	if sheets[1].Rows[1][0] != "Lions" {
		t.Fatalf("second sheet = %q", sheets[1].Rows)
	}
}

// Excel saves text as shared strings, rows and cells it skips are missing,
// and rich text comes in runs.
func TestReadsExcelsOwnFiles(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, _ := z.Create(name)
		_, _ = w.Write([]byte(body))
	}
	add("xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Marks" sheetId="1" r:id="rId3"/></sheets></workbook>`)
	add("xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId3" Target="/xl/worksheets/s.xml"/></Relationships>`)
	add("xl/sharedStrings.xml", `<sst><si><t>Name</t></si><si><r><t>Ka</t></r><r><t>sun</t></r></si></sst>`)
	add("xl/worksheets/s.xml", `<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c></row><row r="3"><c r="A3" t="s"><v>1</v></c><c r="C3"><v>12.000000000000002</v></c><c r="D3" t="b"><v>1</v></c></row></sheetData></worksheet>`)
	_ = z.Close()
	sheets, err := Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	r := sheets[0].Rows
	if len(r) != 3 || r[0][0] != "Name" || r[1] != nil || r[2][0] != "Kasun" || r[2][1] != "" || r[2][2] != "12.000000000000002" || r[2][3] != "TRUE" {
		t.Fatalf("rows = %q", r)
	}
}

func TestNotAWorkbook(t *testing.T) {
	if _, err := Read([]byte("name,marks\n")); err != ErrNotWorkbook {
		t.Fatalf("csv: %v", err)
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	_, _ = z.Create("hello.txt")
	_ = z.Close()
	if _, err := Read(buf.Bytes()); err != ErrNotWorkbook {
		t.Fatalf("zip: %v", err)
	}
}

func TestNames(t *testing.T) {
	if ColName(0) != "A" || ColName(25) != "Z" || ColName(26) != "AA" || colIndex("AB12") != 27 {
		t.Fatal("columns")
	}
	if sheetName(strings.Repeat("x", 40), 1) != strings.Repeat("x", 31) || sheetName("???", 2) != "Sheet2" {
		t.Fatal("sheet names")
	}
}
