#!/usr/bin/env python3
"""
create_demo_asset.py — Create a demo fieldset, model, and asset in Snipe-IT
for README screenshot purposes.

Usage:
    SNIPE_URL=https://your-instance.snipe-it.io \\
    SNIPE_KEY=your-api-key \\
    python3 .github/scripts/create_demo_asset.py

The script is idempotent: if a fieldset/model/asset with the same name already
exists it prints the existing ID and skips creation.

To clean up afterwards, run with --delete:
    python3 .github/scripts/create_demo_asset.py --delete
"""

import html
import json
import os
import sys
import urllib.request
import urllib.error

SNIPE_URL = os.environ.get("SNIPE_URL", "").rstrip("/")
SNIPE_KEY = os.environ.get("SNIPE_KEY", "")

if not SNIPE_URL or not SNIPE_KEY:
    print("ERROR: set SNIPE_URL and SNIPE_KEY environment variables", file=sys.stderr)
    sys.exit(1)

HEADERS = {
    "Authorization": f"Bearer {SNIPE_KEY}",
    "Accept": "application/json",
    "Content-Type": "application/json",
}

# IDs that must already exist in the Snipe-IT instance
MANUFACTURER_ID = 1   # Apple
STATUS_ID       = 2   # Ready to Deploy
CATEGORY_ID     = 2   # Computers
SUPPLIER_ID     = 1   # CDW-G

FIELDSET_NAME = "cdw2snipe Demo"
MODEL_NAME    = "MacBook Pro 16\" M4 Pro"
MODEL_NUMBER  = "Mac16,8"
ASSET_TAG     = "DEMO-CDW-001"
SERIAL        = "C02ZR4XHMD6V"

# CDW custom field definitions (created by `cdw2snipe setup`)
CDW_FIELDS = [
    {"name": "CDW: Order Date",        "element": "text", "format": "DATE",  "help_text": "CDW order date (YYYY-MM-DD)"},
    {"name": "CDW: Invoice Date",      "element": "text", "format": "DATE",  "help_text": "CDW invoice date (YYYY-MM-DD)"},
    {"name": "CDW: Purchaser",         "element": "text", "format": "ANY",   "help_text": "CDW order purchaser name"},
    {"name": "CDW: Purchase Order #",  "element": "text", "format": "ANY",   "help_text": "CDW purchase order number"},
    {"name": "CDW: Invoice #",         "element": "text", "format": "ANY",   "help_text": "CDW invoice number"},
    {"name": "CDW: Ship Date",         "element": "text", "format": "DATE",  "help_text": "CDW ship date (YYYY-MM-DD)"},
]


def api(method, path, body=None, fatal=True):
    url = f"{SNIPE_URL}/api/v1{path}"
    data = json.dumps(body).encode() if body else None
    req = urllib.request.Request(url, data=data, headers=HEADERS, method=method)
    try:
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        msg = f"HTTP {e.code} {method} {path}: {e.read().decode()}"
        if fatal:
            print(msg, file=sys.stderr)
            sys.exit(1)
        print(f"  WARNING: {msg}", file=sys.stderr)
        return {}


def find_by_name(rows, name):
    """Match by name, handling Snipe-IT's HTML entity encoding (e.g. &quot;)."""
    for r in rows:
        row_name = html.unescape(r.get("name", ""))
        if row_name == name:
            return r
    return None


def create():
    # ── 1. Fieldset + CDW custom fields ───────────────────────────────────────
    print(f"\n[1/3] Fieldset: {FIELDSET_NAME!r}")
    fs = find_by_name(api("GET", "/fieldsets")["rows"], FIELDSET_NAME)
    if fs:
        fieldset_id = fs["id"]
        print(f"  → already exists (id={fieldset_id}), skipping")
    else:
        result = api("POST", "/fieldsets", {"name": FIELDSET_NAME})
        if result.get("status") != "success":
            print(f"  ERROR: {result}", file=sys.stderr); sys.exit(1)
        fieldset_id = result["payload"]["id"]
        print(f"  → created (id={fieldset_id})")

    # Create CDW custom fields and associate with fieldset
    cdw_field_db_cols = {}
    existing_fields = api("GET", "/fields?limit=500")["rows"]
    for cf in CDW_FIELDS:
        existing = find_by_name(existing_fields, cf["name"])
        if existing:
            fid = existing["id"]
            db_col = existing.get("db_column_name", "")
            print(f"  field {cf['name']!r} already exists (id={fid}, db_col={db_col})")
        else:
            result = api("POST", "/fields", {
                "name":      cf["name"],
                "element":   cf["element"],
                "format":    cf["format"],
                "help_text": cf["help_text"],
            })
            if result.get("status") != "success":
                print(f"  ERROR creating field {cf['name']!r}: {result}", file=sys.stderr); sys.exit(1)
            fid = result["payload"]["id"]
            db_col = result["payload"].get("db_column_name", "")
            print(f"  → created field {cf['name']!r} (id={fid}, db_col={db_col})")

        # Associate with fieldset
        r = api("POST", f"/fields/{fid}/associate", {"fieldset_id": fieldset_id})
        print(f"     associate field {fid}: {r.get('status', '?')}")

        if db_col:
            cdw_field_db_cols[cf["name"]] = db_col

    # Re-fetch fields to get db_column_name if we didn't have them
    if len(cdw_field_db_cols) < len(CDW_FIELDS):
        all_fields = api("GET", "/fields?limit=500")["rows"]
        for cf in CDW_FIELDS:
            if cf["name"] not in cdw_field_db_cols:
                f = find_by_name(all_fields, cf["name"])
                if f and f.get("db_column_name"):
                    cdw_field_db_cols[cf["name"]] = f["db_column_name"]

    # ── 2. Model ──────────────────────────────────────────────────────────────
    print(f"\n[2/3] Model: {MODEL_NAME!r}")
    mdl = find_by_name(api("GET", "/models?limit=500")["rows"], MODEL_NAME)
    if mdl:
        model_id = mdl["id"]
        print(f"  → already exists (id={model_id}), skipping")
    else:
        result = api("POST", "/models", {
            "name":            MODEL_NAME,
            "model_number":    MODEL_NUMBER,
            "manufacturer_id": MANUFACTURER_ID,
            "category_id":     CATEGORY_ID,
            "fieldset_id":     fieldset_id,
        })
        if result.get("status") != "success":
            print(f"  ERROR: {result}", file=sys.stderr); sys.exit(1)
        model_id = result["payload"]["id"]
        print(f"  → created (id={model_id})")

    # ── 3. Asset ──────────────────────────────────────────────────────────────
    print(f"\n[3/3] Asset: {ASSET_TAG!r} (serial {SERIAL})")
    existing = api("GET", f"/hardware/byserial/{SERIAL}")
    if existing.get("total", 0) > 0:
        asset_id = existing["rows"][0]["id"]
        print(f"  → already exists (id={asset_id}), skipping")
    else:
        # Build asset payload with CDW custom fields
        asset_data = {
            "asset_tag":      ASSET_TAG,
            "serial":         SERIAL,
            "name":           "MacBook Pro 16\" M4 Pro (Space Black)",
            "model_id":       model_id,
            "status_id":      STATUS_ID,
            "supplier_id":    SUPPLIER_ID,
            "purchase_date":  "2025-01-14",
            "purchase_cost":  "4838.11",
        }

        # Map CDW field values by db_column_name
        cdw_values = {
            "CDW: Order Date":       "2025-01-14",
            "CDW: Invoice Date":     "2025-01-15",
            "CDW: Purchaser":        "Jane Smith",
            "CDW: Purchase Order #": "PO-2025-0042",
            "CDW: Invoice #":        "INV-9876543",
            "CDW: Ship Date":        "2025-01-17",
        }
        for field_name, value in cdw_values.items():
            db_col = cdw_field_db_cols.get(field_name)
            if db_col:
                asset_data[db_col] = value

        result = api("POST", "/hardware", asset_data)
        if result.get("status") != "success":
            print(f"  ERROR: {result}", file=sys.stderr); sys.exit(1)
        asset_id = result["payload"]["id"]
        print(f"  → created (id={asset_id})")

    print(f"\nDone.")
    print(f"  Asset:    {SNIPE_URL}/hardware/{asset_id}")
    print(f"  Fieldset: {SNIPE_URL}/fields/fieldsets/{fieldset_id}/edit")


def delete():
    print("Deleting demo data...\n")

    # Delete asset by serial
    existing = api("GET", f"/hardware/byserial/{SERIAL}")
    if existing.get("total", 0) > 0:
        asset_id = existing["rows"][0]["id"]
        api("DELETE", f"/hardware/{asset_id}", fatal=False)
        print(f"  Deleted asset id={asset_id}")
    else:
        print("  Asset not found, skipping")

    # Delete model by name
    mdl = find_by_name(api("GET", "/models?limit=500")["rows"], MODEL_NAME)
    if mdl:
        api("DELETE", f"/models/{mdl['id']}", fatal=False)
        print(f"  Deleted model id={mdl['id']}")
    else:
        print("  Model not found, skipping")

    # Delete fieldset by name (disassociate fields first, but do NOT delete the
    # fields themselves — they may be shared with the real cdw2snipe setup)
    fs = find_by_name(api("GET", "/fieldsets")["rows"], FIELDSET_NAME)
    if fs:
        fid = fs["id"]
        for field in api("GET", f"/fieldsets/{fid}").get("fields", {}).get("rows", []):
            api("POST", f"/fields/{field['id']}/disassociate", {"fieldset_id": fid}, fatal=False)
            print(f"  Disassociated field {field['id']} from fieldset")
        api("DELETE", f"/fieldsets/{fid}", fatal=False)
        print(f"  Deleted fieldset id={fid}")
    else:
        print("  Fieldset not found, skipping")

    print("\nDone.")


if "--delete" in sys.argv:
    delete()
else:
    create()
