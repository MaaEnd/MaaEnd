# AutoStockpile Local Daily Goods-Price Records — Third-Party Read Protocol

When the `AutoStockpileAllowDataUpload` option is enabled, the Go Service writes the per-round recognized goods prices to `debug/record/ElasticGoodsPrices.json` after successful goods recognition and region/server-day resolution. This path is for local file records only and does not trigger remote uploads.

This document defines the file format and path resolution rules for third-party tools (data analysis dashboards, web frontends, users) to read reliably.

---

## JSON Schema

The top-level file structure is fixed as:

```json
{
    "schema_version": 2,
    "records": [
        {
            "server_date": "2026-05-04",
            "weekday": 1,
            "utc_time": "2026-05-04T12:00:00Z",
            "region": "Wuling",
            "uid": "abc123def4567890",
            "goods": [
                {
                    "id": "Wuling/WulingFrozenPears.Tier1",
                    "name": "武陵冻梨",
                    "tier": "Wuling.Tier1",
                    "price": 1000
                }
            ]
        }
    ]
}
```

A corresponding JSON Schema file is provided for third-party tools to validate the data format:
[daily_storage.schema.json](./daily_storage.schema.json)

### Top-level Fields

- `schema_version: int` — Schema version number, currently fixed at `2`. Increments on protocol upgrades; only new fields are added, never removed or changed.
- `records: array` — Array of price records; see below for elements.

### `records[]` Fields

- `server_date: string` — Server date in `YYYY-MM-DD` format. Calculated using the target timezone's `04:00` boundary (`04:00 ~ next 03:59` belongs to the same server day).
- `weekday: int` — Weekday of the server date, `1`=Monday through `7`=Sunday.
- `utc_time: string` — UTC time when the record was written, RFC 3339 format (e.g., `2026-05-04T12:00:00Z`).
- `region: string` — Region identifier. Current possible values: `Wuling`, `ValleyIV` (Valley IV).
- `uid: string` — Player identifier. Acquired by `CaptureUid`: the in-game UID digits are hashed via SHA256 with a user-locally-generated random salt, taking the first 16 hex characters. Irreversible. Falls back to `"unknown"` when no valid UID is available.
- `goods: array` — Array of goods recognized in this round; see below for elements.

### `goods[]` Fields

- `id: string` — Internal goods ID, format `{Region}/{BaseName}.Tier{N}`, e.g. `Wuling/WulingFrozenPears.Tier1`.
- `name: string` — Chinese item name, e.g. "武陵冻梨".
- `tier: string` — Value tier identifier, format `{Region}.Tier{N}`, e.g. `Wuling.Tier1`.
- `price: int` — Recognized price for this good in the current round (elastic goods unit price).

### Read/Write Constraints

- Records contain only `server_date`, `weekday`, `utc_time`, `region`, `uid`, and `goods`. They **do not** contain `quota` or other extra user data. Third parties must not assume additional fields exist.
- **Do not contain** the old `captured_at_utc` field. This field is deprecated in files with `schema_version ≥ 2`.
- A new record with the same `server_date + region + uid` **overwrites** the old record. Different `uid` values on the same server date and region are kept independently.
- At most **120 distinct** `server_date` values are retained. When exceeded, the earliest date and all its region records are discarded.
- Writes use a same-directory temporary file + rename **atomic write** process. Readers can safely read `ElasticGoodsPrices.json` at any time without seeing a partially written file.
- Write failures only log a warning and continue AutoStockpile; the task is not aborted.

---

## Path Resolution

### Target File

```text
debug/record/ElasticGoodsPrices.json
```

The path is a fixed relative path from the MaaEnd working directory. No upward search or environment variable resolution is performed. The directory is created via `MkdirAll` if it does not exist.

### Atomic Write Process

1. Create a temporary file in the same directory (naming pattern `.{ElasticGoodsPrices.json}.*.tmp`)
2. Write the complete JSON content
3. `chmod` the file to `0644`
4. `Sync` to flush to disk
5. `Close` the file handle
6. `os.Rename` to atomically replace the target file

Readers can safely read `ElasticGoodsPrices.json` at any time and will never see a partially written file.

### Write Failure Behavior

- Write failures only log at **warning level**
- AutoStockpile task flow is not aborted
- No retry is attempted

---

## UID Hash Algorithm

UID acquisition is handled by `CaptureUid`: AutoStockpile invokes this custom action at the `AutoStockpileGetUid` node, reads the UID via screenshot OCR, and caches the raw digits; the hash result is used when writing records (see the [CaptureUid reference](../../developers/components/capture-uid.md)). For third parties needing to verify or compare UIDs, the algorithm is:

1. `CaptureUid` captures a stable screen and runs OCR, extracting all consecutive digit segments from the result and concatenating them into a single string (e.g., `"123456789"`); the digit count is validated to be 8–12 before caching
2. Read the user-locally-generated random salt: on first use, a 16-byte random salt (32 hex characters) is generated and written to `debug/record/random_salt.txt`, then reused; no fixed salt is used
3. Compute `SHA256(digit_string + salt)`
4. Take the first 16 characters of the hex digest as the final UID

When no valid digits can be extracted (OCR failure or digit count outside the 8–12 range), the UID is `"unknown"`.

> This hash is an **irreversible salted digest** — the original in-game UID cannot be reversed from the UID in the file. The salt is generated locally per installation: it is stable across sessions within the same installation (usable to correlate the same player), but differs between installations, so the same player's hash cannot be compared across installations.

---

## Server Day Calculation

- **Default timezone**: `UTC+8`
- **Day boundary**: `04:00` (i.e., each day from `04:00 ~ next 03:59` belongs to the same server day)
- **Weekday mapping**: Go standard library `time.Weekday` mapped as `1` (Monday) through `7` (Sunday)

Users can override the timezone offset via the `AutoStockpileServerTime` task option. Current option mappings: CN/Asia `UTC+8`, US/EU `UTC-5`.

---

## Version Compatibility

- The `schema_version` field allows users to read it first and decide their parsing strategy.
- Version increments only add new fields; existing field semantics are never changed.
- Records with `schema_version: 1` may have an empty `uid`; the Go Service normalizes empty `uid` to `"unknown"` when reading back before rewriting. Third-party readers should also tolerate empty `uid`.
