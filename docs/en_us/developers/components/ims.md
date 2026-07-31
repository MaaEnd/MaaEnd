# IMS (Item Management System)

IMS keeps an in-process cache of cultivation-item counts for task gates and quantity checks. Pipeline still owns flow control.

> [!NOTE]
> Shipped today: `ItemDataReady` (R2), `ItemQuantitySatisfied` (R1), `UpdateItemQuantity` (A1), `SyncItemData` (A2), and `AddItemData` (A3).

## Locations

| Path | Role |
| --- | --- |
| `agent/go-service/ims/` | Custom components and cache |
| `assets/resource/pipeline/IMS/` | Pipeline: one file per API |
| `assets/resource/image/IMS/item/` | Item template images (`*_TEMPLATE.png`) |
| `tools/SupplyPlan/mask_ims_item_corner.py` | Top-left green mask tool (Protocol Space reward badge) |
| `tools/schema/components/ims.schema.json` | Parameter JSON Schema |
| `tools/schema/custom.recognition.schema.json` | Registers recognitions and refs the schema |
| `tools/schema/custom.action.schema.json` | Registers actions and refs the schema |

### Pipeline layout

| File | Contents |
| --- | --- |
| `ItemDataReady.json` | R2 `ItemDataReady` + `EnsureItemDataReady*` (calls `SyncItemData` when not ready) |
| `ItemQuantitySatisfied.json` | R1 `ItemQuantitySatisfied` (override `item` / `quantity`) |
| `UpdateItemQuantity.json` | A1 `UpdateItemQuantity` (override `item` / `delta`) |
| `SyncItemData.json` | A2 entry `SyncItemData` (any screen → Progression tab → scan) |
| `AddItemData.json` | A3 best practice: add under `CloseRewardsButton`, then close rewards |
| `common.json` | Shared rarity ColorMatch nodes |
| `item/*.json` | Per-item: rarity color → template → count (grey text And OCR); templates use `green_mask` |

## Item template green mask

Protocol Space reward icons often have a top-left badge that breaks matching against Progression-tab crops. Therefore:

1. Paint a **31×18** RGB `(0, 255, 0)` rectangle on the top-left of each `assets/resource/image/IMS/item/*_TEMPLATE.png`.
2. Enable `"green_mask": true` on the matching `__*_TEMPLATE` TemplateMatch nodes so green pixels are ignored.

```bash
python tools/SupplyPlan/mask_ims_item_corner.py
# preview: python tools/SupplyPlan/mask_ims_item_corner.py --dry-run
```

Run this tool before committing new item templates, and keep `green_mask` on TemplateMatch.

## Recognition: `ItemDataReady`

Reports whether the inventory cache is usable. Read-only for callers; the first cold-start access may hydrate memory from disk once.

### Hit conditions

1. At least one successful sync (`hasData`; written by A2).
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

## Recognition: `ItemQuantitySatisfied`

Reports whether the cached quantity for one item meets the requirement. Read-only for callers (cold start may hydrate). **Does not check readiness** (missing item counts as 0). For “ready **and** enough”, use `And` with `ItemDataReady`.

### Hit conditions

Cached quantity `>= quantity`. Miss reason (log `reason`): `insufficient`.

On each comparison, UI Focus shows current stock vs target (`ims.quantity_ok` / `ims.quantity_short`), throttled ~10s for identical lines to avoid dispatch scan spam.

### Parameters

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `item` | `string` | required | Item ID (same keys as `SyncItemData.items` / `IMS.json`) |
| `quantity` | `integer` | required | Minimum required count (inclusive); `>= 0` |

### Pipeline example

```json
"MyItemEnough": {
    "recognition": {
        "type": "Custom",
        "param": {
            "custom_recognition": "ItemQuantitySatisfied",
            "custom_recognition_param": {
                "item": "PROTODISK",
                "quantity": 10
            }
        }
    },
    "pre_delay": 0,
    "post_delay": 0,
    "rate_limit": 0
}
```

You may also override `ItemQuantitySatisfied`’s `custom_recognition_param`.

## Action: `UpdateItemQuantity`

After a known gain/spend, adjust the cache by a signed delta without a full rescan. **Does not change readiness** (`hasData` / `last_sync` are updated only by A2). Result is clamped to `>= 0`.

### Parameters

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `item` | `string` | required | Item ID |
| `delta` | `integer` | required | Signed change (positive gain, negative spend) |

On success, updates in-memory items and rewrites `items` in `debug/record/IMS.json` (keeps the previous sync timestamp).

### Pipeline example

```json
"GainOneProtoDisk": {
    "recognition": "DirectHit",
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "UpdateItemQuantity",
            "custom_action_param": {
                "item": "PROTODISK",
                "delta": 1
            }
        }
    },
    "pre_delay": 0,
    "post_delay": 0,
    "rate_limit": 0
}
```

## A2: `SyncItemData`

Callers only need Pipeline node **`SyncItemData`**: any screen → Progression tab → Custom Action `SyncItemData`.

### Action parameters

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `items` | `object` | required | Map: item ID → And recognition node name (run in sorted key order) |
| `page_dedup` | `bool` | `false` | `false`: rebuild map from this scan (create); `true`: merge into existing cache and **overwrite** quantities for hit IDs (after paging) |

Quantity = OCR at the end of the node’s `box_index` chain (item And → shared count And → OCR). Item ID comes from the `items` key.

On hit, Focus prints the localized item name and quantity (`ims.sync_item_found` + `ims.item.<ID>`).

Misses are skipped (with `page_dedup=true`, previous values kept). After all items, writes `debug/record/IMS.json` (with `updated_at`) and updates the in-process cache.

### Paging

```text
Page 1: page_dedup=false
After swipe: page_dedup=true
```

`EnsureItemDataReadyMain` jumps to `SyncItemData` when not ready.

> [!NOTE]
> Example `SyncItemDataRun.items`: `{"ADVANCED_COGNITIVE_CARRIER": "ADVANCED_COGNITIVE_CARRIER"}`. Unselected Progression tab switching needs `SceneManager/ProgressionTabNotChoose.png`.

## A3: `AddItemData`

On the **current screen**, run each recognition node in `items` and **add** the OCR quantity into the cache (same as repeated `UpdateItemQuantity` with `+n`). **Does not change readiness**.

Unlike A2 (absolute stash sync + mark ready), A3 accumulates recognized amounts (typical: reward popup).

> [!NOTE]
> If IMS has never been initialized (`hasData=false`: no successful A2 / no sync timestamp on disk), A3 **still recognizes rewards and prints UI Focus**, but **does not write the cache**, and still returns success so reward-close and other Pipeline steps are not blocked.

### Action parameters

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `items` | `object` | required | Map: item ID → recognition node name (sorted key order) |

Misses and quantities `<= 0` are skipped. Outward Focus:

- Each hit: `ims.add_item_found` (item name × quantity)
- After all hits: `ims.add_item_summary` (type count + list); if none: `ims.add_item_none`

> [!IMPORTANT]
> `IMS/item/*` nodes for the Progression tab may not fit the rewards UI. Pass recognition nodes that match the current screen.
> Reward popups often animate in: wait with `pre_wait_freezes` on the reward-item ROI before A3 (see `ProtocolSpaceRewardAddItemData`).

### Best practice

Run under `CloseRewardsButton`, then `next` to click-close:

```json
"AddItemDataOnRewards": {
    "recognition": {
        "type": "And",
        "param": {
            "all_of": ["CloseRewardsButton"]
        }
    },
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "AddItemData",
            "custom_action_param": {
                "items": {
                    "PROTODISK": "PROTODISK"
                }
            }
        }
    },
    "next": ["AddItemDataCloseRewards"]
}
```

See Pipeline nodes `AddItemDataOnRewards` / `AddItemDataCloseRewards`.

### Combined with quantity checks

For “ready **and** enough”, use `And` with `ItemDataReady` and `ItemQuantitySatisfied` so “not ready” is not treated as “need to farm”.

Run `EnsureItemDataReadyMain` at task entry, or call `SyncItemData` directly when a forced refresh is needed.

## Cache conventions (decided)

- In-session, process memory is the source of truth; hot-path reads do not hit disk repeatedly.
- After a cold start, the first IMS access **lazy-hydrates** `debug/record/IMS.json` into memory once; later access stays in memory.
- Successful A2 / A1 / A3 writes also persist to disk for the next cold start.
- `ClearCache` (tests / account switch) clears memory as an intentional empty state and does **not** reload from disk.
- “Never refresh” ≠ never scan: missing data still triggers a scan.
- Small drift is acceptable; periodic sync corrects it.
- IMS does not attribute writers; debug via caller logs.

## Go helpers (tests)

| Function | Description |
| --- | --- |
| `ims.MarkSynced(at, items)` | Record a successful sync (`hasData`, timestamp, item map) |
| `ims.ClearCache()` | Clear cache (tests / account switch); does not reload from disk |
| `ims.ItemsSnapshot()` | Copy of cached item quantities |
