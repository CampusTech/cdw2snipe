package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/michellepellon/go-snipeit"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/xuri/excelize/v2"
)

// version is set at build time via -ldflags.
var version = "dev"

// Column indices (0-based) in the xlsx
const (
	colOrderNum    = 1
	colOrderDate   = 2
	colPurchaser   = 3
	colPO          = 4
	colInvoice     = 6
	colInvoiceDate = 7
	colCategory    = 10
	colSubcategory = 11
	colDescription = 12
	colQty         = 13
	colPrice       = 14
	colSerial      = 27
	colMFGName     = 28
	colShipDate    = 31
)

// cdwCustomFields defines the custom fields to create in Snipe-IT during setup.
var cdwCustomFields = []struct {
	Name     string
	Element  string
	Format   string
	HelpText string
}{
	{"CDW: Order Date", "text", "DATE", "CDW order date (YYYY-MM-DD)"},
	{"CDW: Invoice Date", "text", "DATE", "CDW invoice date (YYYY-MM-DD)"},
	{"CDW: Purchaser", "text", "ANY", "CDW order purchaser name"},
	{"CDW: Purchase Order #", "text", "ANY", "CDW purchase order number"},
	{"CDW: Invoice #", "text", "ANY", "CDW invoice number"},
	{"CDW: Ship Date", "text", "DATE", "CDW ship date (YYYY-MM-DD)"},
}

// orderRow represents a single row from the CDW orders xlsx.
type orderRow struct {
	SourceFile  string
	OrderNum    string
	OrderDate   time.Time
	Purchaser   string
	PO          string
	Invoice     string
	InvoiceDate time.Time
	Category    string
	Subcategory string
	Description string
	Qty         int
	Price       float64
	Serials     []string
	MFGName     string
	ShipDates   []string
}

// snipeITLogger adapts logrus to the go-snipeit Logger interface.
type snipeITLogger struct{}

func (l *snipeITLogger) LogRequest(method, url string, body []byte) {
	log.WithFields(log.Fields{"method": method, "url": url}).Debug("Snipe-IT API request")
	if len(body) > 0 {
		log.WithField("body", string(body)).Trace("Request body")
	}
}

func (l *snipeITLogger) LogResponse(method, url string, statusCode int, body []byte) {
	log.WithFields(log.Fields{"method": method, "url": url, "status": statusCode}).Debug("Snipe-IT API response")
	if len(body) > 0 {
		log.WithField("body", string(body)).Trace("Response body")
	}
}

// computerCategories are the CDW categories considered "computers/laptops".
var computerCategories = map[string]bool{
	"Laptops & 2-in-1s": true,
	"Desktops":          true,
	"Computers":         true,
}

func main() {
	rootCmd := &cobra.Command{
		Use:     "cdw2snipe",
		Short:   "Update Snipe-IT assets with CDW order data from an xlsx export",
		Version: version,
		Long: `cdw2snipe reads a CDW orders xlsx export and updates matching assets in Snipe-IT
by serial number with purchase date, purchase cost, and warranty information.

For computers/laptops, it finds the matching AppleCare warranty line from the same
order and adds the warranty cost to the total purchase price.

All operations are idempotent - running multiple times produces the same result.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			level, err := log.ParseLevel(viper.GetString("log-level"))
			if err != nil {
				return fmt.Errorf("invalid log level %q: %w", viper.GetString("log-level"), err)
			}
			log.SetLevel(level)
			log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
			return nil
		},
	}

	// Persistent flags available to all subcommands
	rootCmd.PersistentFlags().String("snipe-url", "", "Snipe-IT base URL (env: SNIPE_URL)")
	rootCmd.PersistentFlags().String("snipe-token", "", "Snipe-IT API token (env: SNIPE_TOKEN)")
	rootCmd.PersistentFlags().String("log-level", "info", "Log level (trace, debug, info, warn, error)")
	rootCmd.PersistentFlags().String("config", "", "Config file path (default: ./cdw2snipe.yaml)")

	viper.BindPFlag("snipe-url", rootCmd.PersistentFlags().Lookup("snipe-url"))
	viper.BindPFlag("snipe-token", rootCmd.PersistentFlags().Lookup("snipe-token"))
	viper.BindPFlag("log-level", rootCmd.PersistentFlags().Lookup("log-level"))

	viper.BindEnv("snipe-url", "SNIPE_URL")
	viper.BindEnv("snipe-token", "SNIPE_TOKEN")

	// Setup config file
	cobra.OnInitialize(func() {
		cfgFile, _ := rootCmd.PersistentFlags().GetString("config")
		if cfgFile != "" {
			viper.SetConfigFile(cfgFile)
		} else {
			viper.SetConfigName("cdw2snipe")
			viper.SetConfigType("yaml")
			viper.AddConfigPath(".")
		}
		if err := viper.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				log.WithError(err).Warn("Error reading config file")
			}
		} else {
			log.WithField("file", viper.ConfigFileUsed()).Debug("Using config file")
		}
	})

	// Sync subcommand
	syncCmd := &cobra.Command{
		Use:   "sync [xlsx-files...]",
		Short: "Sync CDW order data to Snipe-IT assets",
		RunE:  runSync,
	}
	syncCmd.Flags().StringSlice("xlsx", nil, "Path(s) to CDW orders xlsx file(s)")
	syncCmd.Flags().Bool("dry-run", false, "Log what would be changed without making updates")
	syncCmd.Flags().Bool("computers-only", false, "Only process computers and laptops/2-in-1s")
	syncCmd.Flags().Bool("apple-only", false, "Only process Apple products")
	syncCmd.Flags().String("serial", "", "Only process a single serial number")

	viper.BindPFlag("xlsx", syncCmd.Flags().Lookup("xlsx"))
	viper.BindPFlag("dry-run", syncCmd.Flags().Lookup("dry-run"))
	viper.BindPFlag("computers-only", syncCmd.Flags().Lookup("computers-only"))
	viper.BindPFlag("apple-only", syncCmd.Flags().Lookup("apple-only"))
	viper.BindPFlag("serial", syncCmd.Flags().Lookup("serial"))

	// Setup subcommand
	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Create CDW custom fields in Snipe-IT and save field mappings",
		RunE:  runSetup,
	}

	rootCmd.AddCommand(syncCmd, setupCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newSnipeClient() (*snipeit.Client, error) {
	snipeURL := viper.GetString("snipe-url")
	snipeToken := viper.GetString("snipe-token")

	if snipeURL == "" {
		return nil, fmt.Errorf("--snipe-url or SNIPE_URL is required")
	}
	if snipeToken == "" {
		return nil, fmt.Errorf("--snipe-token or SNIPE_TOKEN is required")
	}

	opts := &snipeit.ClientOptions{
		RateLimiter: snipeit.NewTokenBucketRateLimiter(5, 10),
	}
	if log.GetLevel() <= log.DebugLevel {
		opts.Logger = &snipeITLogger{}
	}

	return snipeit.NewClientWithOptions(snipeURL, snipeToken, opts)
}

func runSetup(cmd *cobra.Command, args []string) error {
	client, err := newSnipeClient()
	if err != nil {
		return err
	}

	// Select or create a fieldset
	fieldsetID, err := selectOrCreateFieldset(client)
	if err != nil {
		return fmt.Errorf("setting up fieldset: %w", err)
	}

	// List existing fields to check for duplicates
	existingFields, _, err := client.Fields.List(&snipeit.ListOptions{Limit: 500})
	if err != nil {
		return fmt.Errorf("listing existing fields: %w", err)
	}

	existingByName := make(map[string]snipeit.Field)
	for _, f := range existingFields.Rows {
		existingByName[f.Name] = f
	}

	// Track all CDW field IDs (for association)
	type fieldInfo struct {
		Name string
		ID   int
	}
	var allCDWFields []fieldInfo

	for _, cf := range cdwCustomFields {
		if existing, ok := existingByName[cf.Name]; ok {
			log.WithFields(log.Fields{
				"name": cf.Name,
				"id":   existing.ID,
			}).Info("Custom field already exists, skipping creation")
			allCDWFields = append(allCDWFields, fieldInfo{Name: cf.Name, ID: existing.ID})
			continue
		}

		field := snipeit.Field{
			Element:  cf.Element,
			Format:   cf.Format,
			HelpText: cf.HelpText,
		}
		field.Name = cf.Name

		resp, _, err := client.Fields.Create(field)
		if err != nil {
			return fmt.Errorf("creating field %q: %w", cf.Name, err)
		}

		if resp.Status != "success" {
			return fmt.Errorf("creating field %q: %s", cf.Name, resp.Message.String())
		}

		log.WithFields(log.Fields{
			"name": cf.Name,
			"id":   resp.Payload.ID,
		}).Info("Created custom field")

		allCDWFields = append(allCDWFields, fieldInfo{Name: cf.Name, ID: resp.Payload.ID})
	}

	// Associate all CDW fields with the selected fieldset (idempotent)
	for _, fi := range allCDWFields {
		_, err := client.Fields.Associate(fi.ID, fieldsetID)
		if err != nil {
			return fmt.Errorf("associating field %q with fieldset: %w", fi.Name, err)
		}

		log.WithFields(log.Fields{
			"field":    fi.Name,
			"fieldset": fieldsetID,
		}).Info("Associated field with fieldset")
	}

	// Re-fetch all fields via List to get db_column_name values
	allFields, _, err := client.Fields.List(&snipeit.ListOptions{Limit: 500})
	if err != nil {
		return fmt.Errorf("re-listing fields: %w", err)
	}

	fieldsByID := make(map[int]snipeit.Field)
	for _, f := range allFields.Rows {
		fieldsByID[f.ID] = f
	}

	fieldMappings := make(map[string]string)
	for _, fi := range allCDWFields {
		if f, ok := fieldsByID[fi.ID]; ok && f.DBColumnName != "" {
			fieldMappings[fi.Name] = f.DBColumnName
			log.WithFields(log.Fields{
				"name":           fi.Name,
				"db_column_name": f.DBColumnName,
			}).Info("Got db_column_name for field")
		} else {
			log.WithField("name", fi.Name).Warn("Field still has no db_column_name after association")
		}
	}

	// Save field mappings to config
	viper.Set("custom-fields", fieldMappings)

	cfgFile := viper.ConfigFileUsed()
	if cfgFile == "" {
		cfgFile = "cdw2snipe.yaml"
	}
	if err := viper.WriteConfigAs(cfgFile); err != nil {
		return fmt.Errorf("saving config to %s: %w", cfgFile, err)
	}

	log.WithField("file", cfgFile).Info("Saved field mappings to config file")
	return nil
}

func selectOrCreateFieldset(client *snipeit.Client) (int, error) {
	fieldsets, _, err := client.Fieldsets.List(&snipeit.ListOptions{Limit: 500})
	if err != nil {
		return 0, fmt.Errorf("listing fieldsets: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("\nAvailable fieldsets:")
	for _, fs := range fieldsets.Rows {
		fmt.Printf("  [%d] %s\n", fs.ID, fs.Name)
	}
	fmt.Println("  [0] Create new fieldset")
	fmt.Print("\nSelect a fieldset ID (or 0 to create new): ")

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	choice, err := strconv.Atoi(input)
	if err != nil {
		return 0, fmt.Errorf("invalid input %q: expected a number", input)
	}

	if choice != 0 {
		// Verify the chosen ID exists
		for _, fs := range fieldsets.Rows {
			if fs.ID == choice {
				log.WithFields(log.Fields{"name": fs.Name, "id": fs.ID}).Info("Selected existing fieldset")
				return fs.ID, nil
			}
		}
		return 0, fmt.Errorf("fieldset ID %d not found", choice)
	}

	// Create new fieldset
	fmt.Print("Enter name for new fieldset: ")
	name, _ := reader.ReadString('\n')
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("fieldset name cannot be empty")
	}

	fs := snipeit.Fieldset{}
	fs.Name = name
	resp, _, err := client.Fieldsets.Create(fs)
	if err != nil {
		return 0, fmt.Errorf("creating fieldset %q: %w", name, err)
	}

	if resp.Status != "success" {
		return 0, fmt.Errorf("creating fieldset %q: %s", name, resp.Message.String())
	}

	log.WithFields(log.Fields{"name": name, "id": resp.Payload.ID}).Info("Created fieldset")
	return resp.Payload.ID, nil
}

func runSync(cmd *cobra.Command, args []string) error {
	snipeURL := viper.GetString("snipe-url")
	snipeToken := viper.GetString("snipe-token")

	if snipeURL == "" {
		return fmt.Errorf("--snipe-url or SNIPE_URL is required")
	}
	if snipeToken == "" {
		return fmt.Errorf("--snipe-token or SNIPE_TOKEN is required")
	}

	// Collect xlsx paths from positional args and --xlsx flag
	var xlsxPaths []string
	if len(args) > 0 {
		xlsxPaths = append(xlsxPaths, args...)
	}
	if flagPaths := viper.GetStringSlice("xlsx"); len(flagPaths) > 0 {
		xlsxPaths = append(xlsxPaths, flagPaths...)
	}
	if len(xlsxPaths) == 0 {
		return fmt.Errorf("at least one xlsx file is required (as argument or via --xlsx)")
	}

	dryRun := viper.GetBool("dry-run")
	computersOnly := viper.GetBool("computers-only")
	appleOnly := viper.GetBool("apple-only")
	filterSerial := viper.GetString("serial")

	// Load custom field mappings from config
	customFieldMap := viper.GetStringMapString("custom-fields")
	if len(customFieldMap) > 0 {
		log.WithField("fields", len(customFieldMap)).Info("Loaded custom field mappings from config")
	}

	// Parse all xlsx files
	var orders []orderRow
	for _, xlsxPath := range xlsxPaths {
		fileOrders, err := parseXLSX(xlsxPath)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", xlsxPath, err)
		}
		log.WithFields(log.Fields{"file": xlsxPath, "rows": len(fileOrders)}).Info("Parsed CDW orders xlsx")
		orders = append(orders, fileOrders...)
	}

	// Group rows by order number
	orderGroups := groupByOrder(orders)
	log.WithField("orders", len(orderGroups)).Info("Grouped into orders")

	// Build serial -> hardware row mapping and find warranty info per order
	type assetUpdate struct {
		SourceFile      string
		Serial          string
		PurchaseDate    time.Time
		HardwarePrice   float64
		WarrantyPrice   float64
		WarrantyInvoice string
		WarrantyDesc    string
		OrderNum        string
		OrderDate       string
		PO              string
		Invoice         string
		InvoiceDate     string
		Purchaser       string
		ShipDate        string
	}

	var updates []assetUpdate

	for orderNum, rows := range orderGroups {
		var warranties []orderRow
		var hardwareRows []orderRow

		for _, r := range rows {
			if r.Subcategory == "Warranties" {
				warranties = append(warranties, r)
			}
			if len(r.Serials) > 0 {
				if computersOnly && !computerCategories[r.Category] {
					continue
				}
				if appleOnly && !strings.Contains(strings.ToLower(r.MFGName), "apple") {
					continue
				}
				hardwareRows = append(hardwareRows, r)
			}
		}

		warrantyByType := matchWarrantiesToHardware(warranties, hardwareRows)

		for _, hw := range hardwareRows {
			perUnitWarrantyPrice := 0.0
			warrantyInvoice := ""
			warrantyDesc := ""

			if w, ok := warrantyByType[hw.Description]; ok {
				perUnitWarrantyPrice = w.Price
				warrantyInvoice = w.Invoice
				warrantyDesc = w.Description
			}

			orderDateStr := ""
			if !hw.OrderDate.IsZero() {
				orderDateStr = hw.OrderDate.Format("2006-01-02")
			}
			invoiceDateStr := ""
			if !hw.InvoiceDate.IsZero() {
				invoiceDateStr = hw.InvoiceDate.Format("2006-01-02")
			}
			for i, serial := range hw.Serials {
				shipDateStr := ""
				if i < len(hw.ShipDates) {
					shipDateStr = hw.ShipDates[i]
				} else if len(hw.ShipDates) > 0 {
					// Fewer ship dates than serials — use the last available
					shipDateStr = hw.ShipDates[len(hw.ShipDates)-1]
				}
				updates = append(updates, assetUpdate{
					SourceFile:      hw.SourceFile,
					Serial:          serial,
					PurchaseDate:    hw.OrderDate,
					HardwarePrice:   hw.Price,
					WarrantyPrice:   perUnitWarrantyPrice,
					WarrantyInvoice: warrantyInvoice,
					WarrantyDesc:    warrantyDesc,
					OrderNum:        orderNum,
					OrderDate:       orderDateStr,
					PO:              hw.PO,
					Invoice:         hw.Invoice,
					InvoiceDate:     invoiceDateStr,
					Purchaser:       hw.Purchaser,
					ShipDate:        shipDateStr,
				})
			}
		}
	}

	log.WithField("assets", len(updates)).Info("Found serial numbers to process")

	client, err := newSnipeClient()
	if err != nil {
		return err
	}

	var updated, skipped, notFound, errCount int
	for _, u := range updates {
		if filterSerial != "" && u.Serial != filterSerial {
			continue
		}

		logger := log.WithFields(log.Fields{
			"file":     u.SourceFile,
			"serial":   u.Serial,
			"order":    u.OrderNum,
			"hw_price": u.HardwarePrice,
		})

		if u.WarrantyPrice > 0 {
			logger = logger.WithFields(log.Fields{
				"warranty_price":   u.WarrantyPrice,
				"warranty_invoice": u.WarrantyInvoice,
			})
		}

		// Look up asset by serial in Snipe-IT
		assetsResp, _, err := client.Assets.GetAssetBySerial(u.Serial)
		if err != nil {
			logger.WithError(err).Error("Failed to look up asset by serial")
			errCount++
			continue
		}

		if assetsResp.Total == 0 {
			logger.Warn("Asset not found in Snipe-IT")
			notFound++
			continue
		}

		asset := assetsResp.Rows[0]
		logger = logger.WithFields(log.Fields{
			"asset_id":  asset.ID,
			"asset_tag": asset.AssetTag,
			"name":      asset.Name,
		})

		// Build the update payload - only include fields that need changing
		totalPrice := u.HardwarePrice + u.WarrantyPrice
		totalPriceStr := formatPrice(totalPrice)
		purchaseDateStr := u.PurchaseDate.Format("2006-01-02")

		needsUpdate := false
		updateAsset := snipeit.Asset{}

		// Check purchase cost
		existingCost := strings.TrimSpace(asset.PurchaseCost)
		existingCostNorm := strings.ReplaceAll(existingCost, ",", "")
		if existingCostNorm != totalPriceStr {
			logger.WithFields(log.Fields{
				"current": existingCost,
				"new":     totalPriceStr,
			}).Debug("Updating purchase cost")
			updateAsset.PurchaseCost = totalPriceStr
			needsUpdate = true
		}

		// Check purchase date
		existingDate := ""
		if asset.PurchaseDate != nil && !asset.PurchaseDate.IsZero() {
			existingDate = asset.PurchaseDate.Format("2006-01-02")
		}
		if existingDate != purchaseDateStr {
			logger.WithFields(log.Fields{
				"current": existingDate,
				"new":     purchaseDateStr,
			}).Debug("Updating purchase date")
			if updateAsset.CustomFields == nil {
				updateAsset.CustomFields = make(map[string]string)
			}
			updateAsset.CustomFields["purchase_date"] = purchaseDateStr
			needsUpdate = true
		}

		// Check and set CDW custom fields if mappings are configured
		if len(customFieldMap) > 0 {
			if updateAsset.CustomFields == nil {
				updateAsset.CustomFields = make(map[string]string)
			}

			cdwValues := map[string]string{
				"CDW: Order Date":        u.OrderDate,
				"CDW: Invoice Date":      u.InvoiceDate,
				"CDW: Purchaser":         u.Purchaser,
				"CDW: Purchase Order #":  u.PO,
				"CDW: Invoice #":         u.Invoice,
				"CDW: Ship Date":         u.ShipDate,
			}

			for fieldName, newValue := range cdwValues {
				dbCol, ok := customFieldMap[strings.ToLower(fieldName)]
				if !ok || newValue == "" {
					continue
				}

				// Check existing value on the asset
				existingValue := ""
				if asset.CustomFields != nil {
					existingValue = asset.CustomFields[dbCol]
				}

				if existingValue != newValue {
					logger.WithFields(log.Fields{
						"field":   fieldName,
						"current": existingValue,
						"new":     newValue,
					}).Debug("Updating custom field")
					updateAsset.CustomFields[dbCol] = newValue
					needsUpdate = true
				}
			}
		}

		if !needsUpdate {
			logger.Debug("Asset already up to date")
			skipped++
			continue
		}

		if dryRun {
			logger.Info("[DRY RUN] Would update asset")
			updated++
			continue
		}

		// Use Patch for partial update
		resp, _, err := client.Assets.Patch(asset.ID, updateAsset)
		if err != nil {
			logger.WithError(err).Error("Failed to update asset")
			errCount++
			continue
		}

		if resp.Status != "success" {
			logger.WithField("message", resp.Message.String()).Error("Snipe-IT returned error status")
			errCount++
			continue
		}

		logger.Info("Successfully updated asset")
		updated++
	}

	log.WithFields(log.Fields{
		"updated":   updated,
		"skipped":   skipped,
		"not_found": notFound,
		"errors":    errCount,
	}).Info("Processing complete")

	if errCount > 0 {
		return fmt.Errorf("%d errors occurred during processing", errCount)
	}

	return nil
}

func parseXLSX(path string) ([]orderRow, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, err
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("xlsx has no data rows")
	}

	var result []orderRow
	for i, row := range rows[1:] { // Skip header
		if len(row) <= colSerial {
			log.WithFields(log.Fields{"file": path, "row": i + 2}).Warn("Row has fewer columns than expected, skipping")
			continue
		}

		qty, _ := strconv.Atoi(strings.TrimSpace(row[colQty]))
		if qty == 0 {
			qty = 1
		}

		price, _ := strconv.ParseFloat(cleanPrice(row[colPrice]), 64)

		var serials []string
		serialStr := strings.TrimSpace(row[colSerial])
		if serialStr != "" {
			for _, s := range strings.Split(serialStr, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					serials = append(serials, s)
				}
			}
		}

		mfgName := ""
		if len(row) > colMFGName {
			mfgName = strings.TrimSpace(row[colMFGName])
		}

		orderDate := parseDate(row[colOrderDate])
		invoiceDate := parseDate(row[colInvoiceDate])

		// Parse ship dates - comma-separated, one per serial
		var shipDates []string
		if len(row) > colShipDate {
			for _, sd := range strings.Split(row[colShipDate], ",") {
				t := parseDate(sd)
				if !t.IsZero() {
					shipDates = append(shipDates, t.Format("2006-01-02"))
				}
			}
		}

		result = append(result, orderRow{
			SourceFile:  path,
			OrderNum:    strings.TrimSpace(row[colOrderNum]),
			OrderDate:   orderDate,
			Purchaser:   strings.TrimSpace(row[colPurchaser]),
			PO:          strings.TrimSpace(row[colPO]),
			Invoice:     strings.TrimSpace(row[colInvoice]),
			InvoiceDate: invoiceDate,
			Category:    strings.TrimSpace(row[colCategory]),
			Subcategory: strings.TrimSpace(row[colSubcategory]),
			Description: strings.TrimSpace(row[colDescription]),
			Qty:         qty,
			Price:       price,
			Serials:     serials,
			MFGName:     mfgName,
			ShipDates:   shipDates,
		})
	}

	return result, nil
}

func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		"01-02-06",
		"1/2/06",
		"2006-01-02",
		"01/02/2006",
		"1/2/2006",
		"2006-01-02 15:04:05",
		"01-02-2006",
	}
	for _, fmt := range formats {
		t, err := time.Parse(fmt, s)
		if err == nil {
			return t
		}
	}
	log.WithField("value", s).Warn("Could not parse date")
	return time.Time{}
}

func groupByOrder(rows []orderRow) map[string][]orderRow {
	groups := make(map[string][]orderRow)
	for _, r := range rows {
		groups[r.OrderNum] = append(groups[r.OrderNum], r)
	}
	return groups
}

func deviceTypeFromDesc(desc string) string {
	desc = strings.ToLower(desc)

	patterns := []struct {
		re  *regexp.Regexp
		key string
	}{
		{regexp.MustCompile(`macbook\s+pro.*16`), "macbook_pro_16"},
		{regexp.MustCompile(`macbook\s+pro.*14`), "macbook_pro_14"},
		{regexp.MustCompile(`macbook\s+air.*15`), "macbook_air_15"},
		{regexp.MustCompile(`macbook\s+air.*13`), "macbook_air_13"},
		{regexp.MustCompile(`mac\s+studio`), "mac_studio"},
		{regexp.MustCompile(`mac\s+mini`), "mac_mini"},
		{regexp.MustCompile(`mac\s+pro\b`), "mac_pro"},
		{regexp.MustCompile(`imac`), "imac"},
		{regexp.MustCompile(`iphone.*16.*pro.*max`), "iphone_16_pro_max"},
		{regexp.MustCompile(`iphone.*16.*pro`), "iphone_16_pro"},
		{regexp.MustCompile(`iphone.*16`), "iphone_16"},
		{regexp.MustCompile(`iphone.*15.*pro.*max`), "iphone_15_pro_max"},
		{regexp.MustCompile(`iphone.*15.*pro`), "iphone_15_pro"},
		{regexp.MustCompile(`iphone.*15`), "iphone_15"},
		{regexp.MustCompile(`iphone`), "iphone"},
		{regexp.MustCompile(`ipad.*pro.*13`), "ipad_pro_13"},
		{regexp.MustCompile(`ipad.*pro.*11`), "ipad_pro_11"},
		{regexp.MustCompile(`ipad.*air`), "ipad_air"},
		{regexp.MustCompile(`ipad.*mini`), "ipad_mini"},
		{regexp.MustCompile(`ipad`), "ipad"},
		{regexp.MustCompile(`apple\s+watch`), "apple_watch"},
		{regexp.MustCompile(`apple\s+tv`), "apple_tv"},
	}

	for _, p := range patterns {
		if p.re.MatchString(desc) {
			return p.key
		}
	}

	return ""
}

func matchWarrantiesToHardware(warranties, hardwareRows []orderRow) map[string]orderRow {
	result := make(map[string]orderRow)

	if len(warranties) == 0 {
		return result
	}

	warrantyByType := make(map[string]orderRow)
	for _, w := range warranties {
		dt := deviceTypeFromDesc(w.Description)
		if dt != "" {
			warrantyByType[dt] = w
		}
	}

	if len(warranties) == 1 {
		hwTypes := make(map[string]bool)
		for _, hw := range hardwareRows {
			dt := deviceTypeFromDesc(hw.Description)
			if dt != "" {
				hwTypes[dt] = true
			}
		}
		if len(hwTypes) == 1 {
			for _, hw := range hardwareRows {
				result[hw.Description] = warranties[0]
			}
			return result
		}
	}

	for _, hw := range hardwareRows {
		dt := deviceTypeFromDesc(hw.Description)
		if dt == "" {
			continue
		}
		if w, ok := warrantyByType[dt]; ok {
			result[hw.Description] = w
		}
	}

	return result
}

func cleanPrice(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	return s
}

func formatPrice(price float64) string {
	price = math.Round(price*100) / 100
	return strconv.FormatFloat(price, 'f', 2, 64)
}
