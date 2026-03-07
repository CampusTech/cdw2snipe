package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestCleanPrice(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"$4,539.11", "4539.11"},
		{"$100.00", "100.00"},
		{"1234.56", "1234.56"},
		{"$0.00", "0.00"},
		{"  $1,234.56  ", "1234.56"},
		{"", ""},
		{"$12,345,678.90", "12345678.90"},
	}
	for _, tc := range tests {
		got := cleanPrice(tc.input)
		if got != tc.want {
			t.Errorf("cleanPrice(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFormatPrice(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{4539.11, "4539.11"},
		{100.0, "100.00"},
		{0.0, "0.00"},
		{99.999, "100.00"},
		{99.994, "99.99"},
		{1234.5, "1234.50"},
		{0.1 + 0.2, "0.30"},
	}
	for _, tc := range tests {
		got := formatPrice(tc.input)
		if got != tc.want {
			t.Errorf("formatPrice(%v) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		input string
		want  string // expected YYYY-MM-DD or "" for zero
	}{
		{"1/14/2025", "2025-01-14"},
		{"01/14/2025", "2025-01-14"},
		{"2025-01-14", "2025-01-14"},
		{"01-14-2025", "2025-01-14"},
		{"01-14-25", "2025-01-14"},
		{"1/2/25", "2025-01-02"},
		{"2025-01-14 10:30:00", "2025-01-14"},
		{"", ""},
		{"  ", ""},
		{"not-a-date", ""},
	}
	for _, tc := range tests {
		got := parseDate(tc.input)
		gotStr := ""
		if !got.IsZero() {
			gotStr = got.Format("2006-01-02")
		}
		if gotStr != tc.want {
			t.Errorf("parseDate(%q) = %q, want %q", tc.input, gotStr, tc.want)
		}
	}
}

func TestDeviceTypeFromDesc(t *testing.T) {
	tests := []struct {
		desc string
		want string
	}{
		// Mac laptops
		{"Apple MacBook Pro 16-inch M4 Pro", "macbook_pro_16"},
		{"MacBook Pro 14\" M4 Max", "macbook_pro_14"},
		{"MacBook Air 15-inch M3", "macbook_air_15"},
		{"MacBook Air 13 M2", "macbook_air_13"},
		// Mac desktops
		{"Mac Studio M2 Ultra", "mac_studio"},
		{"Mac mini M4", "mac_mini"},
		{"Mac Pro Tower", "mac_pro"},
		{"iMac 24-inch", "imac"},
		// iPhones
		{"iPhone 16 Pro Max 256GB", "iphone_16_pro_max"},
		{"iPhone 16 Pro 128GB", "iphone_16_pro"},
		{"iPhone 16 128GB", "iphone_16"},
		{"iPhone 15 Pro Max", "iphone_15_pro_max"},
		{"iPhone 15 Pro", "iphone_15_pro"},
		{"iPhone 15", "iphone_15"},
		{"iPhone SE", "iphone"},
		// iPads
		{"iPad Pro 13-inch M4", "ipad_pro_13"},
		{"iPad Pro 11-inch M4", "ipad_pro_11"},
		{"iPad Air M2", "ipad_air"},
		{"iPad mini 7th gen", "ipad_mini"},
		{"iPad 10th gen", "ipad"},
		// Other
		{"Apple Watch Ultra 2", "apple_watch"},
		{"Apple TV 4K", "apple_tv"},
		// No match
		{"Dell Latitude 5540", ""},
		{"AppleCare+ for MacBook Pro", ""}, // no size suffix, no match
		{"Random warranty item", ""},
	}
	for _, tc := range tests {
		got := deviceTypeFromDesc(tc.desc)
		if got != tc.want {
			t.Errorf("deviceTypeFromDesc(%q) = %q, want %q", tc.desc, got, tc.want)
		}
	}
}

func TestGroupByOrder(t *testing.T) {
	rows := []orderRow{
		{OrderNum: "ORD-001", Description: "MacBook Pro"},
		{OrderNum: "ORD-001", Description: "AppleCare"},
		{OrderNum: "ORD-002", Description: "Mac mini"},
	}

	groups := groupByOrder(rows)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if len(groups["ORD-001"]) != 2 {
		t.Errorf("expected 2 rows in ORD-001, got %d", len(groups["ORD-001"]))
	}
	if len(groups["ORD-002"]) != 1 {
		t.Errorf("expected 1 row in ORD-002, got %d", len(groups["ORD-002"]))
	}
}

func TestMatchWarrantiesToHardware_NoWarranties(t *testing.T) {
	hw := []orderRow{{Description: "MacBook Pro 16-inch M4 Pro", Serials: []string{"ABC123"}}}
	result := matchWarrantiesToHardware(nil, hw)
	if len(result) != 0 {
		t.Errorf("expected empty result with no warranties, got %d", len(result))
	}
}

func TestMatchWarrantiesToHardware_SingleWarrantySingleType(t *testing.T) {
	hw := []orderRow{
		{Description: "MacBook Pro 16-inch M4 Pro", Serials: []string{"ABC123"}},
		{Description: "MacBook Pro 16-inch M4 Pro", Serials: []string{"DEF456"}},
	}
	warranties := []orderRow{
		{Description: "AppleCare+ for MacBook Pro 16", Price: 299.0, Invoice: "INV-001"},
	}

	result := matchWarrantiesToHardware(warranties, hw)
	if len(result) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(result))
	}
	w, ok := result["MacBook Pro 16-inch M4 Pro"]
	if !ok {
		t.Fatal("expected warranty mapping for MacBook Pro 16-inch M4 Pro")
	}
	if w.Price != 299.0 {
		t.Errorf("expected warranty price 299.0, got %v", w.Price)
	}
}

func TestMatchWarrantiesToHardware_MultipleTypes(t *testing.T) {
	hw := []orderRow{
		{Description: "MacBook Pro 16-inch M4 Pro", Serials: []string{"ABC123"}},
		{Description: "Mac mini M4", Serials: []string{"DEF456"}},
	}
	warranties := []orderRow{
		{Description: "AppleCare+ for MacBook Pro 16", Price: 299.0},
		{Description: "AppleCare for Mac mini", Price: 99.0},
	}

	result := matchWarrantiesToHardware(warranties, hw)
	if len(result) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(result))
	}
	if result["MacBook Pro 16-inch M4 Pro"].Price != 299.0 {
		t.Errorf("MacBook Pro warranty price = %v, want 299.0", result["MacBook Pro 16-inch M4 Pro"].Price)
	}
	if result["Mac mini M4"].Price != 99.0 {
		t.Errorf("Mac mini warranty price = %v, want 99.0", result["Mac mini M4"].Price)
	}
}

func TestMatchWarrantiesToHardware_NoDeviceTypeMatch(t *testing.T) {
	hw := []orderRow{
		{Description: "Dell Latitude 5540", Serials: []string{"DELL001"}},
	}
	warranties := []orderRow{
		{Description: "Dell ProSupport", Price: 150.0},
	}

	result := matchWarrantiesToHardware(warranties, hw)
	if len(result) != 0 {
		t.Errorf("expected no matches for non-Apple devices, got %d", len(result))
	}
}

// createTestXLSX creates a temporary xlsx file with the given rows.
// Each row is a slice of strings corresponding to cell values.
// Returns the path to the temporary file.
func createTestXLSX(t *testing.T, header []string, dataRows [][]string) string {
	t.Helper()
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)

	for i, val := range header {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, val)
	}
	for rowIdx, row := range dataRows {
		for colIdx, val := range row {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
			f.SetCellValue(sheet, cell, val)
		}
	}

	path := filepath.Join(t.TempDir(), "test.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("failed to save test xlsx: %v", err)
	}
	return path
}

// makeRow builds a full row with the right number of columns.
// Fills unused columns with empty strings.
func makeRow(orderNum, orderDate, purchaser, po, col5, invoice, invoiceDate, col8, col9, col10, category, subcategory, description, qty, price string, remaining ...string) []string {
	row := make([]string, colShipDate+1)
	row[colOrderNum] = orderNum
	row[colOrderDate] = orderDate
	row[colPurchaser] = purchaser
	row[colPO] = po
	if len(row) > 5 {
		row[5] = col5
	}
	row[colInvoice] = invoice
	row[colInvoiceDate] = invoiceDate
	if len(row) > 8 {
		row[8] = col8
	}
	if len(row) > 9 {
		row[9] = col9
	}
	row[colCategory] = category
	row[colSubcategory] = subcategory
	row[colDescription] = description
	row[colQty] = qty
	row[colPrice] = price
	// Fill serial, mfg, and ship date from remaining args
	if len(remaining) > 0 {
		row[colSerial] = remaining[0]
	}
	if len(remaining) > 1 {
		row[colMFGName] = remaining[1]
	}
	if len(remaining) > 2 {
		row[colShipDate] = remaining[2]
	}
	return row
}

func TestParseXLSX_BasicRow(t *testing.T) {
	header := make([]string, colShipDate+1)
	header[colOrderNum] = "Order #"

	dataRow := makeRow(
		"ORD-001", "1/14/2025", "Jane Smith", "PO-123", "",
		"INV-456", "1/15/2025", "", "", "",
		"Laptops & 2-in-1s", "Laptops", "MacBook Pro 16-inch M4 Pro", "1", "$4,539.11",
		"ABC123", "Apple", "1/17/2025",
	)

	path := createTestXLSX(t, header, [][]string{dataRow})
	rows, err := parseXLSX(path)
	if err != nil {
		t.Fatalf("parseXLSX failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	r := rows[0]
	if r.OrderNum != "ORD-001" {
		t.Errorf("OrderNum = %q, want %q", r.OrderNum, "ORD-001")
	}
	if r.OrderDate.Format("2006-01-02") != "2025-01-14" {
		t.Errorf("OrderDate = %v, want 2025-01-14", r.OrderDate)
	}
	if r.Purchaser != "Jane Smith" {
		t.Errorf("Purchaser = %q, want %q", r.Purchaser, "Jane Smith")
	}
	if r.PO != "PO-123" {
		t.Errorf("PO = %q, want %q", r.PO, "PO-123")
	}
	if r.Invoice != "INV-456" {
		t.Errorf("Invoice = %q, want %q", r.Invoice, "INV-456")
	}
	if r.Category != "Laptops & 2-in-1s" {
		t.Errorf("Category = %q, want %q", r.Category, "Laptops & 2-in-1s")
	}
	if r.Subcategory != "Laptops" {
		t.Errorf("Subcategory = %q, want %q", r.Subcategory, "Laptops")
	}
	if r.Description != "MacBook Pro 16-inch M4 Pro" {
		t.Errorf("Description = %q, want %q", r.Description, "MacBook Pro 16-inch M4 Pro")
	}
	if r.Qty != 1 {
		t.Errorf("Qty = %d, want 1", r.Qty)
	}
	if r.Price != 4539.11 {
		t.Errorf("Price = %v, want 4539.11", r.Price)
	}
	if len(r.Serials) != 1 || r.Serials[0] != "ABC123" {
		t.Errorf("Serials = %v, want [ABC123]", r.Serials)
	}
	if r.MFGName != "Apple" {
		t.Errorf("MFGName = %q, want %q", r.MFGName, "Apple")
	}
	if len(r.ShipDates) != 1 || r.ShipDates[0] != "2025-01-17" {
		t.Errorf("ShipDates = %v, want [2025-01-17]", r.ShipDates)
	}
}

func TestParseXLSX_MultipleSerials(t *testing.T) {
	header := make([]string, colShipDate+1)
	dataRow := makeRow(
		"ORD-002", "1/14/2025", "John Doe", "PO-456", "",
		"INV-789", "1/15/2025", "", "", "",
		"Laptops & 2-in-1s", "Laptops", "MacBook Air 13 M3", "3", "$1,299.00",
		"SER1, SER2, SER3", "Apple", "1/16/2025, 1/17/2025, 1/18/2025",
	)

	path := createTestXLSX(t, header, [][]string{dataRow})
	rows, err := parseXLSX(path)
	if err != nil {
		t.Fatalf("parseXLSX failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	r := rows[0]
	if len(r.Serials) != 3 {
		t.Fatalf("expected 3 serials, got %d: %v", len(r.Serials), r.Serials)
	}
	if r.Serials[0] != "SER1" || r.Serials[1] != "SER2" || r.Serials[2] != "SER3" {
		t.Errorf("Serials = %v, want [SER1 SER2 SER3]", r.Serials)
	}
	if len(r.ShipDates) != 3 {
		t.Fatalf("expected 3 ship dates, got %d: %v", len(r.ShipDates), r.ShipDates)
	}
	if r.ShipDates[0] != "2025-01-16" || r.ShipDates[1] != "2025-01-17" || r.ShipDates[2] != "2025-01-18" {
		t.Errorf("ShipDates = %v, want [2025-01-16 2025-01-17 2025-01-18]", r.ShipDates)
	}
}

func TestParseXLSX_NoSerials(t *testing.T) {
	header := make([]string, colShipDate+1)
	// Put a placeholder past colSerial so excelize writes all columns
	dataRow := makeRow(
		"ORD-003", "1/14/2025", "Jane", "PO-789", "",
		"INV-012", "1/15/2025", "", "", "",
		"Warranties", "Warranties", "AppleCare+ for MacBook Pro", "1", "$299.00",
		"", "Apple", "",
	)

	path := createTestXLSX(t, header, [][]string{dataRow})
	rows, err := parseXLSX(path)
	if err != nil {
		t.Fatalf("parseXLSX failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if len(rows[0].Serials) != 0 {
		t.Errorf("expected no serials, got %v", rows[0].Serials)
	}
}

func TestParseXLSX_EmptyFile(t *testing.T) {
	f := excelize.NewFile()
	path := filepath.Join(t.TempDir(), "empty.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	_, err := parseXLSX(path)
	if err == nil {
		t.Error("expected error for xlsx with no data rows")
	}
}

func TestParseXLSX_FileNotFound(t *testing.T) {
	_, err := parseXLSX("/nonexistent/file.xlsx")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestParseXLSX_ShortRow(t *testing.T) {
	// Row with fewer columns than colSerial should be skipped
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	f.SetCellValue(sheet, "A1", "Header")
	f.SetCellValue(sheet, "A2", "short row") // only 1 column

	path := filepath.Join(t.TempDir(), "short.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	rows, err := parseXLSX(path)
	if err != nil {
		t.Fatalf("parseXLSX failed: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows (short row skipped), got %d", len(rows))
	}
}

func TestParseDate_TwoDigitYear(t *testing.T) {
	// Verify two-digit year parsing
	got := parseDate("01-14-25")
	if got.IsZero() {
		t.Fatal("expected non-zero time")
	}
	if got.Year() != 2025 || got.Month() != time.January || got.Day() != 14 {
		t.Errorf("got %v, want 2025-01-14", got)
	}
}

func TestComputerCategories(t *testing.T) {
	for _, cat := range []string{"Laptops & 2-in-1s", "Desktops", "Computers"} {
		if !computerCategories[cat] {
			t.Errorf("expected %q to be a computer category", cat)
		}
	}
	if computerCategories["Monitors"] {
		t.Error("Monitors should not be a computer category")
	}
}

func TestCDWCustomFields(t *testing.T) {
	expectedNames := []string{
		"CDW: Order Date",
		"CDW: Invoice Date",
		"CDW: Purchaser",
		"CDW: Purchase Order #",
		"CDW: Invoice #",
		"CDW: Ship Date",
	}
	if len(cdwCustomFields) != len(expectedNames) {
		t.Fatalf("expected %d custom fields, got %d", len(expectedNames), len(cdwCustomFields))
	}
	for i, name := range expectedNames {
		if cdwCustomFields[i].Name != name {
			t.Errorf("custom field %d: got %q, want %q", i, cdwCustomFields[i].Name, name)
		}
	}
}

func TestParseXLSX_MultipleRows(t *testing.T) {
	header := make([]string, colShipDate+1)
	row1 := makeRow(
		"ORD-001", "1/14/2025", "Jane", "PO-1", "",
		"INV-1", "1/15/2025", "", "", "",
		"Laptops & 2-in-1s", "Laptops", "MacBook Pro 16-inch", "1", "$4,539.11",
		"ABC123", "Apple", "1/17/2025",
	)
	row2 := makeRow(
		"ORD-001", "1/14/2025", "Jane", "PO-1", "",
		"INV-1", "1/15/2025", "", "", "",
		"Warranties", "Warranties", "AppleCare+ for MacBook Pro 16", "1", "$299.00",
		"", "Apple", "",
	)
	row3 := makeRow(
		"ORD-002", "2/1/2025", "John", "PO-2", "",
		"INV-2", "2/2/2025", "", "", "",
		"Desktops", "Desktops", "Mac mini M4", "2", "$599.00",
		"DEF456, GHI789", "Apple", "2/3/2025, 2/4/2025",
	)

	path := createTestXLSX(t, header, [][]string{row1, row2, row3})
	rows, err := parseXLSX(path)
	if err != nil {
		t.Fatalf("parseXLSX failed: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	// Verify grouping
	groups := groupByOrder(rows)
	if len(groups) != 2 {
		t.Fatalf("expected 2 order groups, got %d", len(groups))
	}
	if len(groups["ORD-001"]) != 2 {
		t.Errorf("ORD-001: expected 2 rows, got %d", len(groups["ORD-001"]))
	}
	if len(groups["ORD-002"]) != 1 {
		t.Errorf("ORD-002: expected 1 row, got %d", len(groups["ORD-002"]))
	}
}

func TestVersionIsSet(t *testing.T) {
	if version == "" {
		t.Error("version should not be empty")
	}
}

func TestParseXLSX_HeaderOnly(t *testing.T) {
	header := make([]string, colShipDate+1)
	header[0] = "Header"

	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	for i, val := range header {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, val)
	}
	path := filepath.Join(t.TempDir(), "headeronly.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	_, err := parseXLSX(path)
	if err == nil || err.Error() != "xlsx has no data rows" {
		t.Errorf("expected 'xlsx has no data rows' error, got: %v", err)
	}
}

func TestCleanPrice_EdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"$$$", ""},
		{",,,", ""},
		{"$,", ""},
		{"  $  1,000  ", "  1000"},
	}
	for _, tc := range tests {
		got := cleanPrice(tc.input)
		if got != tc.want {
			t.Errorf("cleanPrice(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFormatPrice_LargeValues(t *testing.T) {
	got := formatPrice(123456789.99)
	if got != "123456789.99" {
		t.Errorf("formatPrice(123456789.99) = %q, want %q", got, "123456789.99")
	}
}

func TestDeviceTypeFromDesc_CaseInsensitive(t *testing.T) {
	tests := []struct {
		desc string
		want string
	}{
		{"MACBOOK PRO 16 INCH", "macbook_pro_16"},
		{"macbook air 13 m3", "macbook_air_13"},
		{"IPHONE 16 PRO MAX", "iphone_16_pro_max"},
		{"iPad Pro 11-inch", "ipad_pro_11"},
	}
	for _, tc := range tests {
		got := deviceTypeFromDesc(tc.desc)
		if got != tc.want {
			t.Errorf("deviceTypeFromDesc(%q) = %q, want %q", tc.desc, got, tc.want)
		}
	}
}

func TestMatchWarrantiesToHardware_SingleWarrantyMultipleHWTypes(t *testing.T) {
	// Single warranty but multiple hardware types — should only match by device type
	hw := []orderRow{
		{Description: "MacBook Pro 16-inch M4 Pro", Serials: []string{"ABC123"}},
		{Description: "Mac mini M4", Serials: []string{"DEF456"}},
	}
	warranties := []orderRow{
		{Description: "AppleCare+ for MacBook Pro 16", Price: 299.0},
	}

	result := matchWarrantiesToHardware(warranties, hw)
	// With multiple hw types and single warranty, falls through to type matching
	if _, ok := result["MacBook Pro 16-inch M4 Pro"]; !ok {
		t.Error("expected MacBook Pro to match warranty")
	}
	if _, ok := result["Mac mini M4"]; ok {
		t.Error("Mac mini should not match a MacBook Pro warranty")
	}
}

func TestMain(m *testing.M) {
	// Suppress log output during tests
	os.Exit(m.Run())
}
