# IMS (Item Management System)

IMS (Item Management System) keeps a cache of counts for cultivation items and other items inside the go-service process so tasks can answer “is it enough?” and “should we farm?”. Pipeline still owns flow control; IMS only provides recognitions and actions.

There are **2 recognitions + 3 actions**:

| Code | Registered name | Role |
| --- | --- | --- |
| **A2** | `SyncItemData` | Core: scan the current screen and write the full cache |
| **A1** | `UpdateItemQuantity` | Add/subtract one item in the cache |
| **A3** | `AddItemData` | Scan the current screen and **add** recognized counts into the cache |
| **R1** | `ItemQuantitySatisfied` | Whether cached counts meet a boolean expression |
| **R2** | `ItemDataReady` | Whether the whole cache is usable (exists + not expired) |

Codes `A1` / `A2` / `A3` and `R1` / `R2` only reflect implementation order, not priority. **A2 was the second action written, but it is the core of IMS.**

On-disk path: `debug/record/IMS.json` (relative to the working directory go-service was started in, resolved by `fsutil.OutputPath`).

---

## A2: `SyncItemData` (core)

A2 means “look at the current screen and record item quantities”. Callers must **not** invent their own stash-scan flow—use the reserved entry for the region you need:

| Entry | Screen |
| --- | --- |
| `SyncItemData` | Valuables Vault **Progression** tab |
| `SyncShopItemData` | **Procurement Center** (Origeometry / Oroberyl header counts) |
| `SyncValuablesItemData` | Valuables Vault **Valuables** tab (e.g. Chartered HH Permit) |
| `SyncDepotItemData` | Combined **Valley IV + Wuling left-side depots** on the Dijiang depot/backpack screen (normal collectables; excludes the right-side backpack) |

Tasks that must declare “I want a fresh cache” should call the matching entry explicitly (it goes to Skipped if already scanned).

### Dijiang depot entry

`SyncDepotItemData` calls SceneManager's [Dijiang depot interfaces](../scene-manager.md#dijiang-depot-interfaces) — `SceneEnterMenuBackpackWithDepotValleyIVAll` then `SceneEnterMenuBackpackWithDepotWulingAll` — to reach the All category of both depots, and calls the existing A2 `SyncItemData` action for each left-side depot it lands on. Entering the depot, switching region, and selecting the category (including ADB asset overrides) are all owned by SceneManager; IMS no longer carries its own depot-switch flow. It uses `grid_type: transfer` with `Normal:Plant`, `Normal:Nurturance`, and `Normal:Doodad`, covering regular plants, special-route collectables, and insect collectables. The resulting cache is **Valley IV depot + Wuling depot**; quantities in the right-side backpack are excluded.

Each depot first checks whether its list has a scrollbar. A single-page list is scanned once; a multi-page list is scrolled to the top and then scanned to the bottom. Before every actual scan, the mouse is moved out of the item grid so a stable hover tooltip cannot cover an icon or quantity. Valley IV uses the default `merge_mode: replace`: its first page rebuilds the candidate region with `page_dedup: false`, and later pages overwrite current-page hits with `page_dedup: true`. Wuling uses `merge_mode: sum`: its first page captures an immutable Valley IV baseline, and every page overwrites the two-depot total with `Valley IV baseline + current Wuling absolute quantity`. Overlapping pages are therefore idempotent and cannot add the same item twice.

Depot pages use `transaction_mode` staging: every page updates only the current Pipeline TaskID's staging snapshot, and the formal IMS cache plus `updated_at` are updated once after both depots finish. A failed or interrupted scan cannot publish a first page, first depot, or partial second depot as a formal snapshot; the next `begin` discards unfinished staging.

After a complete commit, the actual scan path is locked for the current Resource lifetime and later calls take the Skipped path. AutoCollect's Target Inventory mode explicitly verifies the current run's completion marker; an Action failure or prematurely ended flow ignores old cache data and continues selected routes. After successful verification and before any collection route, AutoCollect immediately re-enables the depot entry. It does not perform a second scan or estimate this run's collection deltas; the next depot inventory consumer re-syncs the real depot quantities. Account switching re-enables the entry and clears its completion marker so another account cannot reuse the previous account's state.

### Calling convention (required)

Every task that needs a region’s IMS cache must **actively refresh that region once before its business logic**: run the matching reserved entry once.

Effects:

1. **Every IMS task declares “I want a fresh cache”**, instead of silently trusting possibly stale on-disk data.
2. **Within one Resource lifetime, each regional entry only scans once**; a successful A2 closes further scan entry for that region.
3. **Later tasks still call the same node, but skip immediately** and reuse the cache written by the first task.

Do not skip the entry because “the cache should still be fine”, and do not reimplement scanning inside the task. Always go through the reserved entries so data stays fresh without every task re-entering the same UI.

### Parameters (IconRecognition IDs)

Cache keys and recognition IDs all use [`IconRecognition`](./icon-recognition.md) catalog top-level keys (for example `item_char_break_stage_1_2`). IMS does **not** keep an item allowlist: whatever is scanned on screen is cached/announced.

| Field | Meaning |
| --- | --- |
| `grid_type` | Grid screen. Progression/valuables tabs use `valuables`; required for IconRecognition scan |
| `roi` | Optional; omit to use the IconRecognition reference ROI (Valuables Vault Win32 `[24,76,950,570]` / ADB `[100,85,790,540]`); `transfer` has no default and must pass it explicitly |
| `item_filters` | Narrows IconRecognition **candidate templates** only (e.g. `ValuableDepot:SpecialItem` on the Progression tab); does not filter which IDs IMS keeps |
| `items` | **Anchored quantity nodes** (still required for region rebuild): cache ID → Pipeline recognition node. The node may be pure OCR, or And with `box_index` selecting the OCR digit result (top-bar currencies, shop digits, etc.) |
| `deduplicate` | IconRecognition dedupe; A2 defaults to `true` |
| `merge_mode` | Write mode; defaults to `replace`. `sum` merges a second absolute inventory region into the first-region baseline; it is not a reward delta |
| `page_dedup` | Distinguishes the first page from continuation pages together with `merge_mode`; see below |
| `transaction_mode` | Optional TaskID-scoped staging: `begin` starts and scans, `continue` scans into the same stage, and `commit` skips recognition and publishes the completed stage. Omit for immediate persistence |
| `notify_ui` | Whether to announce hits; defaults to `true`. After each scan, emit one icon HTML summary; scan stages summarize per page, while `commit` summarizes final deduplicated quantities for items hit by the transaction |

Provide `grid_type` (IconRecognition scan) and/or `items`. Shop-only OCR entries may pass only `items` (e.g. `item_originium_recharge` / `item_diamond`). Keys in `items` always join the `merge_mode: replace` + `page_dedup=false` region rebuild (miss removes the ID).

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

1. If `grid_type` is set: **one full-screen grid scan** via `item_filters` (or grid default candidates), derive the bottom quantity band from the grid's reference size and actual `cell_box`, OCR the quantity, and write it to the cache. The quantity path currently supports `transfer`, `valuables`, and `rewards`; the standard Win32 band is 18px high and ADB source cells map it to 22px at 1.25 scale.
2. If `items` is set: run the anchored recognition nodes in sorted key order and take the OCR quantity via `box_index`.
3. **Hit + valid quantity:** record `item ID → quantity`.
4. **Miss:** do not record that ID this round (see region rebuild / overwrite below).
5. A non-transactional call writes memory and `./debug/record/IMS.json` after the scan and updates `updated_at`. Transactional `begin` / `continue` only update staging; only `commit` updates the formal cache and timestamp.

Hits emit one HTML Focus summary after the scan finishes by default (16px item icons + quantities, template `ims.sync_item_summary`), instead of one line per item. Pass `notify_ui: false` to silence (omit defaults to `true`); the side cache taken during the shop universal jump uses `SyncShopItemDataRunNoNotify`. Transactional `begin` / `continue` calls may stay silent and set `notify_ui: true` only on `commit`; after persistence succeeds, that prints one summary of the final deduplicated quantities of items actually hit by the transaction and excludes other IMS cache regions. If `notify_ui` is also enabled on `begin` / `continue`, each page prints its own page summary.

### Write mode and paging (`merge_mode` + `page_dedup`)

| `merge_mode` | `page_dedup` | Mode | Behavior |
| --- | --- | --- | --- |
| `replace` (default) | `false` (default) | **Region rebuild** | Clear this region’s candidates, then write hits: (1) IDs expanded from `item_filters` (or grid defaults) via IconRecognition `recognition_items.json`; (2) every key in `items` (anchored OCR / And+`box_index`). Misses are removed. Cached IDs from **other regions** are kept. |
| `replace` | `true` | **Overwrite** | Start from existing cache; **overwrite quantities** for IDs hit this round; **keep old values** for IDs not seen. |
| `sum` | `false` | **Start sum** | Capture an immutable baseline for the current task and candidate region; keep the baseline and write `baseline + current-page absolute quantity`. |
| `sum` | `true` | **Continue sum** | Must run in the same Pipeline task as Start sum; recompute from the same baseline, making overlapping pages idempotent. |

`sum` combines two **absolute inventory snapshots**. It does not replace A3 reward-delta semantics and should not be called outside a complete `first region replace → second region sum` flow. Ordinary paging still uses the default `replace`: page 1 does a region rebuild; later pages overwrite only the IDs visible on that page so earlier pages are not wiped. The reserved entry `SyncItemData` defaults to:

```text
First: SyncItemDataRunFull (page_dedup = false, region rebuild)
  next[0]: [JumpBack]SyncItemDataScrollPage → swipe, then SyncItemDataRunInc (page_dedup = true)
  next[1]: SyncItemDataLock (scan finished)
```

Extra pages are controlled by `SyncItemDataScrollPage`: the node defaults to `enabled=false` and no controller resource currently enables it, so only the first page is actually scanned. To scroll, first enable the node in that controller's resource, then set the number of extra pages with `max_hit` (currently 1); when `max_hit` is exhausted, JumpBack no longer matches and Lock runs.

For a multi-page or multi-region snapshot that must not publish partial data when interrupted, use one Pipeline TaskID:

```text
First actual scan: transaction_mode = begin
Remaining pages/regions: transaction_mode = continue
Complete-success endpoint: transaction_mode = commit
```

`begin` replaces unfinished staging left on the same runner. `continue` and `commit` must match the staging TaskID. `commit` needs neither `grid_type` nor `items` and performs no screenshot; with `notify_ui: true`, it sorts by localized item name and prints one icon HTML summary after a successful commit. Omitting `transaction_mode` keeps the original behavior in which every A2 call persists immediately.

### One real scan per Resource (implementation details)

Each entry node handles the convention above automatically; callers only need to keep invoking the matching entry:

1. **First call:** actually open the target screen, scan, write the cache, then close the “scan again” path with a Resource-level override for the rest of this run (an account switch reopens every entry).
2. **Later calls:** the entry is still reachable, but it immediately decides “already synced this run”, skips the scan, and continues.
3. **Next cold start:** reloading the Resource / restarting the client restores the path; the next IMS task will scan again.

The AutoCollect consumption cycle of `SyncDepotItemData` is an exception: once Target Inventory mode has obtained and verified a complete two-depot snapshot, it immediately re-enables the entry before any route runs, so stopping the task midway does not keep the pre-collection snapshot locked for later consumers.

> Reserved entries and scan parameters: `assets/resource/pipeline/IMS/SyncItemData.json`, `SyncShopItemData.json`, `SyncValuablesItemData.json`, `SyncDepotItemData.json`.
>
> Use `EnsureItemDataReadyMain` only for special “enter A2 only if R2 says stale” cases. Normal IMS tasks should call the matching `Sync*ItemData` directly, not rely on the expiry gate alone.

---

## A1: `UpdateItemQuantity`

When you already know a gain or spend, adjust one cached item without a full rescan.

| Param | Meaning |
| --- | --- |
| `item` | Item ID |
| `delta` | Signed change (positive gain, negative spend) |

Result is clamped to `>= 0`. On success it writes back to memory and updates `items` in `IMS.json`, but does **not** change readiness / sync timestamp (`hasData` / `updated_at` are still established only by A2).

---

## A3: `AddItemData`

A3 uses the same path as A2 on the **rewards** UI (default `grid_type: rewards`, `deduplicate: false` so every on-screen stack is kept): one IconRecognition pass, OCR quantity from each `cell_box`, then apply **positive deltas**.

| Param | Meaning |
| --- | --- |
| `grid_type` | Defaults to `rewards` |
| `roi` | Optional; default rewards reference ROI (Win32 `[39,82,1205,511]` / ADB `[178,140,935,440]`) |
| `item_filters` | Optional; omit for rewards defaults (`Isolate:*` + `ValuableDepot:*`) |
| `item_ids` | Optional; **union** with `item_filters` (filters are expanded first, then the IDs are merged). With only `item_ids`, the covering filters are derived from the catalog. Use to select an exact subset of a broad category (e.g. Trial of Swordmancy's `TrialOfSwordmancyFightSuccess` passes only `item_domain_jinlong_coupon`: it adds only the Wuling Stock Bill, not the Valley Stock Bill from the same `Isolate:DomainGold` category) |

`custom_action_param` may be `{}`. Does **not** update sync timestamp / readiness.

### Fundamental difference from A2

| | A2 | A3 |
| --- | --- | --- |
| Write mode | **Absolute:** the scanned count becomes current stock | **Computed:** the scanned count is added to existing stock |
| Typical use | Full Valuables Vault sync (`valuables`) | Rewards popup “how much more did we get?” (`rewards`) |
| Establishes readiness | Yes (writes `updated_at`, sets `hasData`) | No |

### Works without a cache

A3 **does not require** an existing IMS cache.

If A2 has never succeeded (`hasData=false`), A3 still recognizes the rewards but **does not write the cache**, and still returns success so closing the rewards and later steps are not blocked. An empty rewards grid (IconRecognition `no_match` / `grid_detection_failed`) and a failed disk hydrate are also considered a success. An IconRecognition `exception`, and a missing `recognition_items.json` while resolving candidates with `item_ids`, are logged and treated as an empty grid, so the close-rewards click still runs. Illegal parameters (unparseable JSON, empty or duplicate entries, unknown `item_id`) still fail the action. Hits print one HTML summary after recognition (quantities of same-ID stacks are merged first; template `ims.add_item_summary`, with icons), without IMS header/footer lines such as “not initialized / not writing the cache”.

With a cache it also prints one summary; no Pipeline Starting/Succeeded Focus is stacked on top.

> Reward popups animate in; use `pre_wait_freezes` on the item area before calling A3 (for Protocol Space see `ProtocolSpaceRewardAddItemData`).
>
> Reference Pipeline: `AddItemDataOnRewards` → `AddItemDataCloseRewards`.
>
> Close-reward paths already wired to A3: `SceneNoticeRewardsConfirm` (Claim Daily Rewards, etc.), `DijiangRewardsFastCollectAddItemData` (base Fast Collect, separate candidates), `CreditShoppingClaimConfirm` (Credit Shop: Oroberyl / T-Creds / Arsenal Ticket / Credits + `ValuableDepot:SpecialItem`), `MFGCabinClaimRewardClose`, `GrowthChamberClaimRewardClose`, `SceneNoticeRewardsConfirmWithAnchor`, `ReceptionRoomSendCluesQuickGiveClose` (Reception Room quick gift), `AutoDeliveryCloseRewardDialog` (automatic delivery), `ProtocolSpaceRewardAddItemData` (Protocol Space), `TrialOfSwordmancyFightSuccess` (Trial of Swordmancy).

---

## R1: `ItemQuantitySatisfied`

Checks whether cached item quantities meet a conditional expression; it can also run in announce-only mode.

Same syntax as the generic recognition [`ExpressionRecognition`](../custom.md#expressionrecognition), but placeholders read **IconRecognition item IDs in the IMS cache**, not on-screen OCR nodes.

| Field | Notes |
| --- | --- |
| `expression` | Boolean expression with `{ITEM_ID}` placeholders (missing = `0`). With `report_only`, must contain exactly one `{ITEM_ID}` |
| `notify_ui` | Announce resolved expression to UI Focus; default `false`. Ignored when `report_only` is true (always announces) |
| `report_only` | Announce-only mode: reject multi-item expressions, print “Current XXX: quantity”, and **always hit**; default `false` |

### Condition check (default)

Supported operators:

- Arithmetic: `+` `-` `*` `/` `%`
- Comparison: `<` `<=` `>` `>=` `==` `!=`
- Logic: `&&` `||` `!`
- Grouping: `(...)`

Example:

```json
{
    "custom_recognition": "ItemQuantitySatisfied",
    "custom_recognition_param": {
        "expression": "({item_char_break_stage_1_2}+{item_weapon_break_low})>=100",
        "notify_ui": false
    }
}
```

More examples:

- `{item_char_break_stage_1_2}>=40`
- `{item_char_break_stage_1_2}+{item_weapon_break_low}>=100 && {item_gold}<50`
- `!({item_weapon_break_high}<10)`

The expression result must be boolean.

UI Focus announce runs only when `notify_ui` is `true` (resolved expression; identical lines throttled ~10s). Keep the default off for dispatch-style `next` scans to avoid spam.

### Announce only (`report_only`)

For “read the cache and tell the user the current quantity”, without taking part in the enough-or-not decision:

```json
{
    "custom_recognition": "ItemQuantitySatisfied",
    "custom_recognition_param": {
        "expression": "{item_gold}",
        "report_only": true
    }
}
```

Constraints and behavior:

- `expression` must contain exactly one `{ITEM_ID}`; `{a}+{b}` and the like are rejected
- UI announcement: `Current T-Creds: 40` (text key `ims.item_current`)
- The recognition result **always returns true** (missing cache = `0`)
- Identical lines are throttled for about 10 seconds

R1 does **not** check readiness. For “data ready and enough”, use `And` to attach both R2 (`ItemDataReady`) and R1, so “not synced yet” is not treated as “not enough, go farm”.

---

## R2: `ItemDataReady`

Checks whether the **whole** IMS cache is currently valid and usable for business decisions.

### Check conditions

1. **Cache exists**  
   At least one successful A2 (a sync record in memory / on disk, `hasData=true`). Otherwise → miss (log `reason=no_data`).

2. **Not expired**  
   Use the `refresh_days` parameter to check whether the sync timestamp `updated_at` (or the in-memory last sync time) is still within the validity period. Expired → miss (log `reason=stale`).

Both must pass for a hit.

### What “expired” means

A2 writes `updated_at` when it persists. R2 judges whether the data is still fresh by whether “now − sync time” exceeds `refresh_days` days:

| `refresh_days` | Meaning |
| --- | --- |
| `7` (default) | Valid for 7 days after sync |
| `1` / `30` | Valid for 1 / 30 days after sync |
| `0` | **Never expires by age**: once a sync has ever succeeded, the cache is always treated as not expired |

Notes:

- `refresh_days = 0` only means “does not expire by age”; **missing data still misses**.
- “Never expires by age” ≠ “never scans”: with no data, `EnsureItemDataReadyMain` still goes to A2.

After a cold start, the first IMS access may lazy-hydrate `IMS.json` from disk into memory once; later hot paths only read memory.

---

## Recommended usage (how to chain it)

```text
Task entry
  └─ run SyncItemData once          ← real scan only once per Resource
       └─ business logic
            ├─ R2 ItemDataReady          (optional: confirm the cache is valid)
            ├─ R1 ItemQuantitySatisfied  (enough?)
            ├─ A1 UpdateItemQuantity     (adjust on a known spend/gain)
            └─ A3 AddItemData            (add / announce on the rewards screen)
```

When you need “ready and quantity satisfied”:

```json
"all_of": [
    "ItemDataReady",
    "ItemQuantitySatisfied"
]
```

---

## Appendix

### Implementation locations

| Path | Notes |
| --- | --- |
| `agent/go-service/ims/` | Custom components and cache |
| `agent/go-service/pkg/iconqty/` | Shared A2/A3: IconRecognition scan + `cell_box` quantity OCR |
| `assets/data/IconRecognition/recognition_items.json` | IconRecognition item catalog; A2 region rebuild expands `item_filters` |
| `assets/resource/pipeline/IMS/` | Pipeline (one file per interface) |
| `assets/resource/pipeline/IMS/item/` | Anchored OCR nodes (e.g. `item_gold` / `item_diamond` / `ORIGEOMETRY.json`) |
| `tools/schema/components/ims.schema.json` | Parameter JSON Schema |
| [`IconRecognition`](./icon-recognition.md) | Icon matching and localized names `iconRecognition.name.*` |

| Pipeline file | Contents |
| --- | --- |
| `SyncItemData.json` | A2 Progression-tab entry + once-per-Resource lock |
| `SyncShopItemData.json` | A2 Procurement Center entry |
| `SyncValuablesItemData.json` | A2 Valuables-tab entry |
| `SyncDepotItemData.json` | A2 entry for combined normal collectables in the Dijiang Valley IV + Wuling left-side depots |
| `UpdateItemQuantity.json` | A1 |
| `AddItemData.json` | A3 best practice (close rewards) |
| `ItemQuantitySatisfied.json` | R1 (callers override `expression`) |
| `ItemDataReady.json` | R2 + `EnsureItemDataReady*` |
| `item/*.json` | Anchored OCR (`item_gold` / `item_diamond` / `item_originium_recharge`) |

### Cache conventions

- In-session, process memory is authoritative; hot paths do not reread disk.
- Successful writes by non-transactional A2 calls, A2 transactional `commit`, A1, and A3 (when `hasData`) are also persisted for the next cold start; A2 transactional `begin` / `continue` do not modify the formal cache.
- `ClearCache` clears memory and does **not** reload from disk.
- Small drift in the cache is acceptable; periodic A2 corrects it.

### Go helper API (tests)

| Function | Description |
| --- | --- |
| `ims.MarkSynced(at, items)` | Record a successful sync |
| `ims.ClearCache()` | Clear cache (no disk reload) |
| `ims.ItemsSnapshot()` | Copy of cached quantities |

### On-disk format example

```json
{
    "updated_at": "2026-07-29T12:00:00Z",
    "items": {
        "item_expcard_stage2_high": 12,
        "item_char_break_stage_1_2": 40
    }
}
```
