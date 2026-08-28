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

Cache keys and recognition IDs use [IconRecognition](./icon-recognition.md) catalog top-level keys (for example `item_char_break_stage_1_2`). Display names use `iconRecognition.name.*` only (interface locales, merged at go-service startup). Shop OCR writes `item_originium_recharge` (the pipeline node remains `ORIGEOMETRY_NUMBER`).

---

## A2: `SyncItemData` (core)

A2 means “look at the current screen and record item quantities”. Callers must **not** invent their own stash-scan flow—use the reserved entry for the region you need:

| Entry | Screen |
| --- | --- |
| `SyncItemData` | Valuables **Progression** tab (`grid_type: valuables`) |
| `SyncShopItemData` | **Procurement Center** (Origeometry / Oroberyl header OCR) |
| `SyncValuablesItemData` | Valuables **Valuables** tab (e.g. Chartered HH Permit) |
| `SyncDepotItemData` | Combined **Valley IV + Wuling left-side depots** on the Dijiang depot/backpack screen (normal collectables; excludes the right-side backpack) |

### Dijiang depot entry

`SyncDepotItemData` enters the Dijiang depot/backpack screen, reuses ItemTransfer's depot-switch flow, connects Valley IV and then Wuling, and calls the existing A2 `SyncItemData` action for each connected left-side depot. It uses `grid_type: transfer` with `Normal:Plant`, `Normal:Nurturance`, and `Normal:Doodad`, covering regular plants, special-route collectables, and insect collectables. The resulting cache is **Valley IV depot + Wuling depot**; quantities in the right-side backpack are excluded.

Each depot first checks whether its list has a scrollbar. A single-page list is scanned once; a multi-page list is scrolled to the top and then scanned to the bottom. Before every actual scan, the mouse is moved out of the item grid so a stable hover tooltip cannot cover an icon or quantity. Valley IV uses the default `merge_mode: replace`: its first page rebuilds the candidate region with `page_dedup: false`, and later pages overwrite current-page hits with `page_dedup: true`. Wuling uses `merge_mode: sum`: its first page captures an immutable Valley IV baseline, and every page writes `Valley IV baseline + current Wuling absolute quantity`. Overlapping pages are therefore idempotent and cannot add the same item twice.

Depot pages use `transaction_mode` staging: every page updates only the current Pipeline TaskID's staging snapshot, and the formal IMS cache plus `updated_at` are updated once after both depots finish. A failed or interrupted scan cannot publish a first page, first depot, or partial second depot as a formal snapshot; the next `begin` discards unfinished staging.

After a complete commit, the actual scan path is locked for the current Resource lifetime and later calls take the Skipped path. AutoCollect's Target Inventory mode explicitly verifies the current run's completion marker; an Action failure or prematurely ended flow ignores old cache data and continues selected routes. After successful verification and before any collection route, AutoCollect immediately re-enables the depot entry. It does not perform a second scan or invent collection deltas; the next depot inventory consumer performs a fresh scan. Account switching re-enables the entry and clears its completion marker so another account cannot reuse the previous account's state.

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
| `merge_mode` | Write mode; defaults to `replace`. `sum` merges a second absolute inventory region into the first-region baseline; it is not a reward delta |
| `page_dedup` | Distinguishes the first page from continuation pages together with `merge_mode`; see below |
| `transaction_mode` | Optional TaskID-scoped staging: `begin` starts and scans, `continue` scans into the same stage, and `commit` skips recognition and publishes the completed stage. Omit for immediate persistence |
| `notify_ui` | Whether to announce hits; defaults to `true`. Scan stages announce per page, while `commit` announces only final deduplicated quantities for items hit by the transaction |

Provide `grid_type` and/or `items`. Shop-only OCR entries may pass only `items` (e.g. `item_originium_recharge` / `item_diamond`). Keys in `items` always join `page_dedup=false` region rebuild (miss removes the ID).

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

1. IconRecognition path: one full-grid scan via `item_filters` (or grid defaults), derive the bottom quantity band from the grid's reference size and actual `cell_box`, OCR quantity, and cache every hit. The quantity path currently supports `transfer`, `valuables`, and `rewards`; the standard Win32 band is 18px high and ADB source cells map it to 22px at 1.25 scale.
2. If `items` is set: run OCR-only nodes in sorted key order via `box_index`.
3. **Hit + valid quantity:** record `item ID → quantity`.
4. **Miss:** do not record that ID this round (see region rebuild / overwrite below).
5. A non-transactional call persists memory and `./debug/record/IMS.json` and updates `updated_at`. Transactional `begin` / `continue` only update staging; `commit` updates the formal cache and timestamp.

Hits also emit localized item name + quantity via UI Focus by default (`ims.sync_item_found`). Pass `notify_ui: false` to silence (omit defaults to `true`). Transactional `begin` / `continue` calls may stay silent and set `notify_ui: true` only on `commit`; after persistence succeeds, that emits one final quantity for each item actually hit by the transaction and excludes unrelated IMS cache regions.

### Write mode and paging (`merge_mode` + `page_dedup`)

| `merge_mode` | `page_dedup` | Mode | Behavior |
| --- | --- | --- | --- |
| `replace` (default) | `false` (default) | **Region rebuild** | Clear this region’s candidates, then write hits: (1) IDs expanded from `item_filters` (or grid defaults) via IconRecognition `recognition_items.json`; (2) every key in `items` (anchored OCR / And+`box_index`). Misses are removed. Cached IDs from **other regions** are kept. |
| `replace` | `true` | **Overwrite** | Start from existing cache; overwrite quantities for IDs hit this round; keep old values for IDs not seen. |
| `sum` | `false` | **Start sum** | Capture an immutable baseline for the current task and candidate region; keep the baseline and write `baseline + current-page absolute quantity`. |
| `sum` | `true` | **Continue sum** | Must run in the same Pipeline task as Start sum; recompute from the same baseline, making overlapping pages idempotent. |

`sum` combines two **absolute inventory snapshots**. It does not replace A3 reward-delta semantics and should not be called outside a complete `first region replace → second region sum` flow. Ordinary paged scans keep the default `replace` mode.

For a multi-page or multi-region snapshot that must not publish partial data when interrupted, use one Pipeline TaskID:

```text
First actual scan: transaction_mode = begin
Remaining pages/regions: transaction_mode = continue
Complete-success endpoint: transaction_mode = commit
```

`begin` replaces unfinished staging left on the same runner. `continue` and `commit` must match the staging TaskID. `commit` needs neither `grid_type` nor `items` and performs no screenshot; with `notify_ui: true`, it sorts by localized item name and announces the final deduplicated result after a successful commit. Omitting `transaction_mode` preserves the original immediate-persist behavior.

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

If IMS was never initialized (`hasData=false`), A3 still recognizes and Focus-announces, skips cache write, and returns success so Pipeline can close the rewards UI. An empty rewards grid (`no_match` / `grid_detection_failed`) and a failed disk hydrate are also considered a success: A3 must not block the close-rewards next node. Per-item Focus only; no IMS init / summary lines.

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
| `assets/resource/pipeline/IMS/SyncDepotItemData.json` | A2 entry for combined normal collectables in the Dijiang Valley IV + Wuling left-side depots |
| `assets/resource/pipeline/IMS/item/` | OCR-only nodes (`item_gold` / `item_diamond` / `ORIGEOMETRY.json`) |
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
