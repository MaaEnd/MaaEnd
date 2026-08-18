# IMS (Item Management System)

IMS keeps an in-process cache of cultivation-item counts so tasks can answer “is it enough?” and “should we farm?”. Pipeline still owns flow control; IMS only provides recognitions and actions.

There are **2 recognitions + 3 actions**:

| Code | Registered name | Role |
| --- | --- | --- |
| **A2** | `SyncItemData` | Core: scan the current screen and write the full cache |
| **A1** | `UpdateItemQuantity` | Add/subtract one item in the cache |
| **A3** | `AddItemData` | Scan the current screen and **add** recognized counts into the cache |
| **R1** | `ItemQuantitySatisfied` | Whether cached counts meet a boolean expression |
| **R2** | `ItemDataReady` | Whether the whole cache is usable (exists + not expired) |

Codes `A1` / `A2` / `A3` and `R1` / `R2` only reflect implementation order, not priority. **A2 was the second action written, but it is the core of IMS.**

On-disk path: `./debug/record/IMS.json` (relative to the run directory).

Cache keys and recognition IDs use [IconRecognition](./icon-recognition.md) catalog top-level keys (for example `item_char_break_stage_1_2`). Display names prefer `iconRecognition.name.*` / `ims.item.*`.

---

## A2: `SyncItemData` (core)

A2 means “look at the current screen and record item quantities”. Callers must **not** invent their own stash-scan flow—use the reserved entry for the region you need:

| Entry | Screen |
| --- | --- |
| `SyncItemData` | Valuables **Progression** tab (`grid_type: valuables`) |
| `SyncShopItemData` | **Procurement Center** (Origeometry / Oroberyl header OCR) |
| `SyncValuablesItemData` | Valuables **Valuables** tab (e.g. Chartered HH Permit) |

### Calling convention (required)

Every task that needs a region’s IMS cache must **actively refresh that region once before its business logic**: run the matching reserved entry once.

Effects:

1. **Every IMS task declares “I want a fresh cache”**, instead of silently trusting possibly stale on-disk data.
2. **Within one Resource lifetime, each regional entry only scans once**; a successful A2 closes further scan entry for that region.
3. **Later tasks still call the same node, but skip immediately** and reuse the cache written by the first task.

### Parameters (IconRecognition IDs)

IMS does **not** keep an item allowlist: whatever IconRecognition finds on screen is OCR’d and cached/announced.

| Field | Meaning |
| --- | --- |
| `grid_type` | Grid screen. Progression/valuables tabs use `valuables`; required for IconRecognition scan |
| `roi` | Optional; omit to use the IconRecognition reference ROI (valuables Win32 `[24,76,950,570]` / ADB `[100,85,790,540]`) |
| `item_filters` | Narrows IconRecognition **candidate templates** only (e.g. `ValuableDepot:SpecialItem`); does not filter which IDs IMS keeps |
| `items` | **Anchored quantity nodes** (still required for region rebuild): cache ID → Pipeline recognition node. The node may be pure OCR, or And with `box_index` selecting the OCR digit result (top-bar currencies, shop digits, etc.) |
| `deduplicate` | IconRecognition dedupe; A2 defaults to `true` |
| `page_dedup` / `notify_ui` | Same semantics as before |

Provide `grid_type` and/or `items`. Shop-only OCR entries may pass only `items` (e.g. `ORIGEOMETRY` / `item_diamond`). Keys in `items` always join `page_dedup=false` region rebuild (miss removes the ID).

Example (Progression tab):

```json
{
    "grid_type": "valuables",
    "item_filters": ["ValuableDepot:SpecialItem"],
    "items": {
        "item_gold": "item_gold_NUMBER",
        "item_diamond": "item_diamond_NUMBER"
    },
    "page_dedup": false
}
```

### What runs

1. IconRecognition path: one full-grid scan via `item_filters` (or grid defaults), OCR quantity from each `cell_box`, cache every hit.
2. If `items` is set: run OCR-only nodes in sorted key order via `box_index`.
3. **Hit + valid quantity:** record `item ID → quantity`.
4. **Miss:** do not record that ID this round (see region rebuild / overwrite below).
5. Persist memory and `./debug/record/IMS.json`, update `updated_at`.

Hits also emit localized item name + quantity via UI Focus by default (`ims.sync_item_found`). Pass `notify_ui: false` to silence (omit defaults to `true`).

### Region rebuild vs overwrite (`page_dedup`)

| `page_dedup` | Mode | Behavior |
| --- | --- | --- |
| `false` (default) | **Region rebuild** | Clear this region’s candidates, then write hits: (1) IDs expanded from `item_filters` (or grid defaults) via IconRecognition `recognition_items.json`; (2) every key in `items` (anchored OCR / And+`box_index`). Misses are removed. Cached IDs from **other regions** are kept. |
| `true` | **Overwrite** | Start from existing cache; overwrite quantities for IDs hit this round; keep old values for IDs not seen. |

The reserved entry `SyncItemData` defaults to:

```text
First: SyncItemDataRunFull (page_dedup = false, region rebuild)
  next[0]: [JumpBack]SyncItemDataScrollPage → then SyncItemDataRunInc (page_dedup = true)
  next[1]: SyncItemDataLock (scan finished)
```

Extra pages are controlled by `SyncItemDataScrollPage.max_hit` (currently 1). Win32-Front defaults `enabled=false`; ADB enables swipe-up.

---

## A1: `UpdateItemQuantity`

| Param | Meaning |
| --- | --- |
| `item` | IconRecognition item ID |
| `delta` | Signed change (positive gain, negative spend) |

Result is clamped to `>= 0`. Persists `IMS.json` items but does **not** change readiness / `updated_at` (only A2 does).

---

## A3: `AddItemData`

A3 uses the same path as A2 on the **rewards** UI (default `grid_type: rewards`, `deduplicate: false` so every on-screen stack is kept): one IconRecognition pass, OCR quantity from each `cell_box`, then apply **positive deltas**.

| Param | Meaning |
| --- | --- |
| `grid_type` | Defaults to `rewards` |
| `roi` | Optional; default rewards reference ROI (Win32 `[39,82,1205,511]` / ADB `[178,140,935,440]`) |
| `item_filters` | Optional; omit for rewards defaults (`Isolate:*` + `ValuableDepot:*`) |
| `item_ids` | Optional; **union** with expanded `item_filters` (IMS expands before calling IconRecognition). Use to add a SpecialItem subset without pulling in molds / check kits |

`custom_action_param` may be `{}`. Does **not** update sync timestamp / readiness.

| | A2 | A3 |
| --- | --- | --- |
| Write mode | **Absolute** stock | **Additive** delta |
| Typical screen | Valuables (`valuables`) | Rewards popup (`rewards`) |
| Establishes ready | Yes | No |

If IMS was never initialized (`hasData=false`), A3 still recognizes and Focus-announces, skips cache write, and returns success so Pipeline can close the rewards UI.

> Use `pre_wait_freezes` on the reward area before A3. Reference: `AddItemDataOnRewards` → `AddItemDataCloseRewards`.

---

## R1: `ItemQuantitySatisfied`

Placeholders read **IMS cache IconRecognition IDs**, not on-screen OCR nodes.

| Field | Notes |
| --- | --- |
| `expression` | Boolean expression with `{ITEM_ID}` placeholders (missing = `0`). With `report_only`, must contain exactly one `{ITEM_ID}` |
| `notify_ui` | Announce resolved expression to UI Focus; default `false`. Ignored when `report_only` is true |
| `report_only` | Announce-only mode: reject multi-item expressions, print current quantity, always hit; default `false` |

Default mode:

```json
{
    "custom_recognition": "ItemQuantitySatisfied",
    "custom_recognition_param": {
        "expression": "({item_char_break_stage_1_2}+{item_weapon_break_low})>=100",
        "notify_ui": false
    }
}
```

Examples: `{item_char_break_stage_1_2}>=40`, `{item_gold}<50`. Result must be boolean. R1 does **not** check readiness—combine with R2 via `And` when needed.

Report-only mode:

```json
{
    "custom_recognition": "ItemQuantitySatisfied",
    "custom_recognition_param": {
        "expression": "{item_gold}",
        "report_only": true
    }
}
```

Prints `Current T-Creds: 40` (`ims.item_current`), always returns a recognition hit, and rejects expressions with more than one item placeholder.

---

## R2: `ItemDataReady`

Ready when (1) at least one successful A2 exists (`hasData=true`) and (2) `updated_at` is within `refresh_days` (default `7`; `0` means never expire by age).

---

## Appendix

| Path | Notes |
| --- | --- |
| `agent/go-service/ims/` | Custom components and cache |
| `agent/go-service/pkg/iconqty/` | Shared A2/A3: IconRecognition scan + `cell_box` quantity OCR |
| `assets/data/IconRecognition/recognition_items.json` | IconRecognition catalog; A2 region rebuild expands `item_filters` |
| `assets/resource/pipeline/IMS/` | Pipeline entries |
| `assets/resource/pipeline/IMS/item/` | OCR-only nodes (`item_gold` / `item_diamond` / `ORIGEOMETRY`) |
| [IconRecognition](./icon-recognition.md) | Icon matching and `iconRecognition.name.*` |

On-disk example:

```json
{
    "updated_at": "2026-07-29T12:00:00Z",
    "items": {
        "item_expcard_stage2_high": 12,
        "item_char_break_stage_1_2": 40
    }
}
```
