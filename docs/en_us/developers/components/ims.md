# IMS (Item Management System)

IMS keeps an in-process cache of cultivation-item counts for task gates and quantity checks. Pipeline still owns flow control.

> [!NOTE]
> Shipped today: `ItemDataReady` (R2) and `SyncItemData` (A2). Quantity adjust (A1) and quantity-met recognition (R1) are planned.

## Locations

| Path | Role |
| --- | --- |
| `agent/go-service/ims/` | Custom components and cache |
| `assets/resource/pipeline/IMS/` | Pipeline: one file per API |
| `tools/schema/components/ims.schema.json` | Parameter JSON Schema |
| `tools/schema/custom.recognition.schema.json` | Registers `ItemDataReady` and refs the schema |

### Pipeline layout

| File | Contents |
| --- | --- |
| `ItemDataReady.json` | R2 `ItemDataReady` + `EnsureItemDataReady*` (calls `SyncItemData` when not ready) |
| `ItemQuantitySatisfied.json` | R1 stub |
| `UpdateItemQuantity.json` | A1 stub |
| `SyncItemData.json` | A2 entry `SyncItemData` (any screen → Progression tab → scan) |

## Recognition: `ItemDataReady`

Reports whether the inventory cache is usable. Read-only; no side effects.

### Hit conditions

1. At least one successful sync (`hasData`; written by A2 later).
2. Within the TTL from `refresh_days`. `0` means never expire due to age once data exists; missing data still misses.

Miss reasons (log `reason`): `no_data` / `stale`.

### Parameters

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `refresh_days` | `0` / `1` / `7` / `30` | `7` | Days the cache stays fresh after sync; `0` = never expire by age |

Omitting `custom_recognition_param` or the field defaults to `7`.

### Pipeline example

```json
"ItemDataReady": {
    "recognition": {
        "type": "Custom",
        "param": {
            "custom_recognition": "ItemDataReady",
            "custom_recognition_param": {
                "refresh_days": 7
            }
        }
    },
    "pre_delay": 0,
    "post_delay": 0,
    "rate_limit": 0
}
```

See `EnsureItemDataReadyMain` for the entry gate.

## A2: `SyncItemData`

Callers only need Pipeline node **`SyncItemData`**: any screen → Progression tab → Custom Action `SyncItemData`.

### Action parameters

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `items` | `object` | required | Map: item ID → And recognition node name (run in sorted key order) |
| `page_dedup` | `bool` | `false` | `false`: rebuild map from this scan (create); `true`: merge into existing cache and **overwrite** quantities for hit IDs (after paging) |

Quantity = OCR at the node’s `box_index` child. Item ID comes from the `items` key, not the quantity child node name.

Misses are skipped (with `page_dedup=true`, previous values kept). After all items, writes `debug/record/IMS.json` (with `updated_at`) and updates the in-process cache.

### Paging

```text
Page 1: page_dedup=false
After swipe: page_dedup=true
```

`EnsureItemDataReadyMain` jumps to `SyncItemData` when not ready.

> [!NOTE]
> Example `SyncItemDataRun.items`: `{"ADVANCED_COGNITIVE_CARRIER": "ADVANCED_COGNITIVE_CARRIER_Item"}`. Unselected Progression tab switching needs `SceneManager/ProgressionTabNotChoose.png`.

### Combined with quantity checks

For “ready **and** enough”, use `And` with `ItemDataReady` and the future R1 so “not ready” is not treated as “need to farm”.

Run `EnsureItemDataReadyMain` at task entry, or call `SyncItemData` directly when a forced refresh is needed.

## Cache conventions (decided)

- In-process memory only; restart clears readiness until the next sync.
- “Never refresh” ≠ never scan: missing data still triggers a scan.
- Small drift is acceptable; periodic sync corrects it.
- IMS does not attribute writers; debug via caller logs.

## Go helpers (for A2 / tests)

| Function | Description |
| --- | --- |
| `ims.MarkSynced(at, items)` | Record a successful sync (`hasData`, timestamp, item map) |
| `ims.ClearCache()` | Clear cache (tests / account switch, etc.) |

## Planned APIs

| Kind | Name | Pipeline file | Role |
| --- | --- | --- | --- |
| Action | `UpdateItemQuantity` | `UpdateItemQuantity.json` | Adjust cached count by item name (stub) |
| Action | `SyncItemData` | `SyncItemData.json` | Scan Valuables and sync cache (stub) |
| Recognition | `ItemQuantitySatisfied` | `ItemQuantitySatisfied.json` | Read-only compare cache vs required amount (stub) |
| Recognition | `ItemDataReady` | `ItemDataReady.json` | Whether cache is ready |
