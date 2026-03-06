# CLAUDE.md

## Project Overview

cdw2snipe is a Go CLI tool that imports CDW order data from xlsx exports into Snipe-IT asset management. It matches assets by serial number and updates purchase info and custom fields.

## Build & Run

```
go build ./...
go vet ./...
./cdw2snipe setup        # interactive - creates custom fields in Snipe-IT
./cdw2snipe sync file.xlsx  # syncs CDW data to Snipe-IT
```

## Architecture

Single-file Go application (`main.go`) with two cobra subcommands:
- `setup` - creates CDW custom fields in Snipe-IT, associates with a fieldset, saves field mappings to config
- `sync` - parses CDW xlsx, matches serials to Snipe-IT assets, updates purchase cost/date and custom fields

## Key Dependencies

- `github.com/CampusTech/go-snipeit` (imported as `github.com/michellepellon/go-snipeit` with replace directive in go.mod)
- `github.com/spf13/cobra` + `github.com/spf13/viper` for CLI and config
- `github.com/xuri/excelize/v2` for xlsx parsing
- `github.com/sirupsen/logrus` for logging

## Important Details

- The go-snipeit module path is `github.com/michellepellon/go-snipeit` but the actual repo is `github.com/CampusTech/go-snipeit`. The `replace` directive in go.mod handles this.
- excelize `GetRows` returns all cell values as formatted strings (e.g., `"$4,539.11"`, `"1/14/2025"`). The `cleanPrice()` and `parseDate()` functions handle this.
- Snipe-IT's purchase_date field requires `YYYY-MM-DD` format. We send it via `CustomFields["purchase_date"]` as a plain string, not the SnipeTime type.
- Ship dates in the xlsx are comma-separated (one per serial). They're paired positionally with the comma-separated serials.
- Config file (`cdw2snipe.yaml`) contains API tokens - never commit it. Use `cdw2snipe.example.yaml` as template.
- All operations must be idempotent - check existing values before updating.
