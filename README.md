# cdw2snipe

Import CDW order data from xlsx exports into [Snipe-IT](https://snipeitapp.com/) asset management.

## Features

- Looks up assets in Snipe-IT by serial number and updates purchase date, purchase cost, and CDW-specific custom fields
- For computers/laptops, finds matching AppleCare warranty line items from the same order and adds warranty cost to total purchase price
- Creates and manages CDW custom fields in Snipe-IT (order date, invoice date, purchaser, PO #, invoice #, ship date)
- All operations are idempotent -- running multiple times produces the same result
- Supports filtering by Apple products, computer categories, or a single serial number
- YAML config file support for all settings

## Installation

```
go install github.com/CampusTech/cdw2snipe@latest
```

Or build from source:

```
git clone https://github.com/CampusTech/cdw2snipe.git
cd cdw2snipe
go build
```

## Setup

### 1. Create a config file

Copy the example config and fill in your Snipe-IT credentials:

```
cp cdw2snipe.example.yaml cdw2snipe.yaml
```

Edit `cdw2snipe.yaml` with your Snipe-IT URL and API token. You can also set these via environment variables (`SNIPE_URL`, `SNIPE_TOKEN`) or CLI flags.

### 2. Create custom fields in Snipe-IT

```
./cdw2snipe setup
```

This will:
- Prompt you to select or create a fieldset
- Create the CDW custom fields in Snipe-IT (idempotent -- safe to re-run)
- Associate fields with the selected fieldset
- Save the field mappings to your config file

### 3. Assign the fieldset

In the Snipe-IT admin UI, assign the fieldset created during setup to the asset models you want to populate with CDW data.

## Usage

### Sync CDW data to Snipe-IT

```
# Sync a specific xlsx file
./cdw2snipe sync orders2024.xlsx

# Sync multiple xlsx files at once
./cdw2snipe sync orders2024.xlsx orders2025.xlsx orders2026.xlsx

# Or use the --xlsx flag (repeatable)
./cdw2snipe sync --xlsx orders2024.xlsx --xlsx orders2025.xlsx

# Dry run to see what would change
./cdw2snipe sync --dry-run orders2024.xlsx orders2025.xlsx

# Only process Apple computers/laptops
./cdw2snipe sync --computers-only --apple-only orders2024.xlsx

# Process a single serial number
./cdw2snipe sync --serial CG2JY1061K orders2024.xlsx
```

### Configuration

All flags can be set in `cdw2snipe.yaml`, via environment variables, or on the command line:

| Flag | Config Key | Env Var | Description |
|------|-----------|---------|-------------|
| `--snipe-url` | `snipe-url` | `SNIPE_URL` | Snipe-IT base URL |
| `--snipe-token` | `snipe-token` | `SNIPE_TOKEN` | Snipe-IT API token |
| `--log-level` | `log-level` | | Log level (trace, debug, info, warn, error) |
| `--xlsx` | `xlsx` | | Path(s) to CDW orders xlsx file(s) (repeatable) |
| `--dry-run` | `dry-run` | | Log changes without making updates |
| `--computers-only` | `computers-only` | | Only process computers and laptops |
| `--apple-only` | `apple-only` | | Only process Apple products |
| `--serial` | `serial` | | Only process a single serial number |
| `--config` | | | Path to config file (default: `./cdw2snipe.yaml`) |

## CDW xlsx Format

The tool expects a CDW orders export xlsx with the following columns:

| Column | Index | Description |
|--------|-------|-------------|
| Order # | 1 | CDW order number |
| Order Date | 2 | Order date |
| Purchaser | 3 | Name of purchaser |
| PO # | 4 | Purchase order number |
| Invoice # | 6 | Invoice number |
| Invoice Date | 7 | Invoice date |
| Category | 10 | Product category |
| Subcategory | 11 | Product subcategory |
| Part Description | 12 | Product description |
| Qty | 13 | Quantity |
| Price | 14 | Unit price |
| Serial # | 27 | Serial number(s), comma-separated |
| MFG Name | 28 | Manufacturer name |
| Ship Date | 31 | Ship date(s), comma-separated |

## Warranty Matching

For orders containing both hardware and warranty line items (e.g., AppleCare), the tool automatically matches warranties to hardware by device type within the same order. The warranty cost is added to the hardware unit price to compute the total purchase cost stored in Snipe-IT.

## License

MIT - see [LICENSE](LICENSE)
