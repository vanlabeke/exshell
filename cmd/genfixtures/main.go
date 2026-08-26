// Command genfixtures generates the sample CSV/XLSX files under testdata/
// used by exshell's manual verification pass (see README.md). Fixtures are
// generated rather than committed as opaque binary blobs so a reviewer (or a
// future contributor) can see exactly how each one was built by reading this
// file, and can regenerate them at will via `make fixtures`.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/xuri/excelize/v2"
)

func main() {
	dir := flag.String("dir", "testdata", "directory to write fixtures into")
	flag.Parse()

	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fatal(err)
	}

	generators := []struct {
		name string
		fn   func(dir string) error
	}{
		{"simple.csv", genSimpleCSV},
		{"semicolon.csv", genSemicolonCSV},
		{"wide.csv", genWideCSV},
		{"big.csv", genBigCSV},
		{"book.xlsx", genBookXLSX},
		{"dates.xlsx", genDatesXLSX},
		{"encrypted.xlsx", genEncryptedXLSX},
	}

	for _, g := range generators {
		if err := g.fn(*dir); err != nil {
			fatal(fmt.Errorf("%s: %w", g.name, err))
		}
		fmt.Println("wrote", filepath.Join(*dir, g.name))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "genfixtures:", err)
	os.Exit(1)
}

// writeCSV writes rows to path using delim as the field separator.
func writeCSV(path string, delim rune, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Comma = delim
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// genSimpleCSV writes a small, comma-delimited file mixing text and numeric
// columns — the everyday case exshell targets.
func genSimpleCSV(dir string) error {
	rows := [][]string{
		{"id", "name", "department", "salary", "active"},
		{"1", "Alice Johnson", "Engineering", "95000", "true"},
		{"2", "Bob Smith", "Sales", "72000", "true"},
		{"3", "Carla Diaz", "Marketing", "68000", "false"},
		{"4", "David Lee", "Engineering", "101000", "true"},
		{"5", "Eve Turner", "Support", "58000", "true"},
		{"6", "Frank Wu", "Engineering", "99000", "false"},
		{"7", "Grace Kim", "Sales", "75000", "true"},
		{"8", "Hank Ortiz", "Marketing", "64000", "true"},
		{"9", "Ivy Chen", "Support", "61000", "false"},
		{"10", "Jack Brown", "Engineering", "108000", "true"},
	}
	return writeCSV(filepath.Join(dir, "simple.csv"), ',', rows)
}

// genSemicolonCSV writes a semicolon-delimited file with European-style
// decimal commas in its numeric-looking column, mimicking a real export from
// a European-locale Excel install. This is the fixture that exercises
// delimiter sniffing with zero flags.
func genSemicolonCSV(dir string) error {
	rows := [][]string{
		{"Datum", "Produkt", "Menge", "Preis"},
		{"2026-01-05", "Schrauben", "120", "4,50"},
		{"2026-01-06", "Muttern", "300", "1,20"},
		{"2026-01-07", "Unterlegscheiben", "500", "0,75"},
		{"2026-01-08", "Bolzen", "80", "6,10"},
		{"2026-01-09", "Nagel", "1000", "0,05"},
		{"2026-01-10", "Klammern", "650", "0,10"},
		{"2026-01-11", "Duebel", "220", "0,35"},
		{"2026-01-12", "Winkel", "90", "2,80"},
	}
	return writeCSV(filepath.Join(dir, "semicolon.csv"), ';', rows)
}

// genWideCSV writes a file wide enough (32 columns) to force horizontal
// scrolling in the viewer and column-fitting in the print path.
func genWideCSV(dir string) error {
	const nCols = 32
	header := make([]string, nCols)
	for i := range header {
		header[i] = fmt.Sprintf("col_%02d", i+1)
	}
	rows := [][]string{header}
	for r := 0; r < 8; r++ {
		row := make([]string, nCols)
		for c := range row {
			row[c] = fmt.Sprintf("r%dc%d", r+1, c+1)
		}
		rows = append(rows, row)
	}
	return writeCSV(filepath.Join(dir, "wide.csv"), ',', rows)
}

// genBigCSV writes a 100,000-row file for the startup/scroll performance
// check. Values are deterministic (fixed random seed) so the file's content
// doesn't churn on every regeneration.
func genBigCSV(dir string) error {
	path := filepath.Join(dir, "big.csv")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write([]string{"id", "timestamp", "value", "label"}); err != nil {
		return err
	}

	rng := rand.New(rand.NewSource(42))
	labels := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	const n = 100_000
	for i := 1; i <= n; i++ {
		ts := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		val := rng.Float64() * 1000
		row := []string{
			strconv.Itoa(i),
			ts,
			strconv.FormatFloat(val, 'f', 2, 64),
			labels[i%len(labels)],
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// cellName builds an A1-style cell reference from a column letter and a
// 1-based row number.
func cellName(col string, row int) string {
	return fmt.Sprintf("%s%d", col, row)
}

// writeSheetRows fills sheet starting at A1 with rows, one excelize
// SetCellValue call per cell.
func writeSheetRows(f *excelize.File, sheet string, rows [][]string) error {
	for r, row := range rows {
		for c, val := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				return err
			}
			if err := f.SetCellValue(sheet, cell, val); err != nil {
				return err
			}
		}
	}
	return nil
}

// genBookXLSX writes a workbook with four sheets of differing column
// layouts — including one hidden sheet, so --list-sheets and the viewer's
// tab bar both have something worth exercising.
func genBookXLSX(dir string) error {
	f := excelize.NewFile()
	defer f.Close()

	if err := f.SetSheetName("Sheet1", "Overview"); err != nil {
		return err
	}
	overview := [][]string{
		{"Metric", "Q1", "Q2"},
		{"Revenue", "120000", "134000"},
		{"Expenses", "80000", "91000"},
		{"Headcount", "42", "47"},
	}
	if err := writeSheetRows(f, "Overview", overview); err != nil {
		return err
	}

	if _, err := f.NewSheet("Employees"); err != nil {
		return err
	}
	employees := [][]string{
		{"ID", "Name", "Department", "Start Date", "Salary"},
		{"1", "Alice Johnson", "Engineering", "2019-03-01", "95000"},
		{"2", "Bob Smith", "Sales", "2020-07-15", "72000"},
		{"3", "Carla Diaz", "Marketing", "2021-01-10", "68000"},
	}
	if err := writeSheetRows(f, "Employees", employees); err != nil {
		return err
	}

	if _, err := f.NewSheet("Regions"); err != nil {
		return err
	}
	regions := [][]string{
		{"Region", "Manager"},
		{"North", "Dana White"},
		{"South", "Evan Reyes"},
		{"East", "Farah Nasser"},
		{"West", "Gil Torres"},
	}
	if err := writeSheetRows(f, "Regions", regions); err != nil {
		return err
	}

	if _, err := f.NewSheet("Archive"); err != nil {
		return err
	}
	archive := [][]string{
		{"Note"},
		{"Old data, retained for audit"},
	}
	if err := writeSheetRows(f, "Archive", archive); err != nil {
		return err
	}
	if err := f.SetSheetVisible("Archive", false); err != nil {
		return err
	}

	f.SetActiveSheet(0)

	return f.SaveAs(filepath.Join(dir, "book.xlsx"))
}

// genDatesXLSX writes a workbook exercising excelize's number formatting:
// real Excel date cells (not serial numbers once read back), a custom
// currency format, and a built-in percentage format.
func genDatesXLSX(dir string) error {
	f := excelize.NewFile()
	defer f.Close()

	header := []string{"Order Date", "Product", "Price", "Discount"}
	for i, h := range header {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return err
		}
		if err := f.SetCellValue("Sheet1", cell, h); err != nil {
			return err
		}
	}

	dates := []time.Time{
		time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
	}
	products := []string{"Widget", "Gadget", "Gizmo"}
	prices := []float64{19.99, 249.5, 5.75}
	discounts := []float64{0.1, 0, 0.25}

	currencyFmt := `"$"#,##0.00`
	currencyStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: &currencyFmt})
	if err != nil {
		return err
	}
	pctStyle, err := f.NewStyle(&excelize.Style{NumFmt: 9}) // built-in "0%"
	if err != nil {
		return err
	}

	for i, d := range dates {
		row := i + 2
		if err := f.SetCellValue("Sheet1", cellName("A", row), d); err != nil {
			return err
		}
		if err := f.SetCellValue("Sheet1", cellName("B", row), products[i]); err != nil {
			return err
		}
		if err := f.SetCellValue("Sheet1", cellName("C", row), prices[i]); err != nil {
			return err
		}
		if err := f.SetCellStyle("Sheet1", cellName("C", row), cellName("C", row), currencyStyle); err != nil {
			return err
		}
		if err := f.SetCellValue("Sheet1", cellName("D", row), discounts[i]); err != nil {
			return err
		}
		if err := f.SetCellStyle("Sheet1", cellName("D", row), cellName("D", row), pctStyle); err != nil {
			return err
		}
	}

	return f.SaveAs(filepath.Join(dir, "dates.xlsx"))
}

// genEncryptedXLSX writes a password-protected workbook via excelize's
// native SaveAs(path, excelize.Options{Password: ...}) — the same mechanism
// xlsxsrc's own encryption test uses — so exshell's "this file appears to be
// password-protected" error path has a real fixture to run against.
func genEncryptedXLSX(dir string) error {
	f := excelize.NewFile()
	defer f.Close()

	if err := f.SetCellValue("Sheet1", "A1", "This file is password-protected."); err != nil {
		return err
	}
	if err := f.SetCellValue("Sheet1", "A2", "Password used by the fixture: secret"); err != nil {
		return err
	}

	return f.SaveAs(filepath.Join(dir, "encrypted.xlsx"), excelize.Options{Password: "secret"})
}
