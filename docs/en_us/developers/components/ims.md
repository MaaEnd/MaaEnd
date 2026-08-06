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

---

## A2: `SyncItemData` (core)

A2 means “look at the current screen and record item quantities”. Callers must **not** invent their own stash-scan flow—always use the reserved Pipeline entry `SyncItemData` (any screen → Valuables → Progression tab → Custom Action).

### Calling convention (required)

Every task that needs the IMS cache must **actively refresh once before its business logic**: run the reserved node `SyncItemData` once.

Effects:

1. **Every IMS task declares “I want a fresh cache”**, instead of silently trusting possibly stale on-disk data.
2. **Within one Resource lifetime, only the first call actually scans**; a successful A2 closes further scan entry.
3. **Later IMS tasks still call the same node, but skip immediately** and reuse the cache written by the first task.

Do not skip the entry because “the cache should still be fine”, and do not reimplement scanning inside the task. Always go through `SyncItemData` so data stays fresh without every task re-entering the Progression tab.

### Parameter: `items` map

| | Meaning |
| --- | --- |
| **Key** | Item ID (stored in the cache / `IMS.json`) |
| **Value** | Pipeline node name that recognizes the item and its quantity |

Nodes are usually `And` (rarity color / template / quantity OCR, etc.). If the UI only shows a number for that item, a plain OCR node is fine (e.g. Progression-tab header `T_CREDS_NUMBER` / `OROBERYL_NUMBER`).

**A2 always requires an explicit `items` map** and does not use `items.json`. Progression / valuables / shop entry points each keep their own subset (fixed OCR nodes also differ by screen).

**Requirement:** the node’s `box_index` chain must end at the **quantity** recognition (digits only). A2 only records an entry when the node hits and the quantity text is a valid number.

Example:

```json
{
    "items": {
        "PROTODISK": "PROTODISK",
        "T_CREDS": "T_CREDS_NUMBER"
    },
    "page_dedup": false
}
```

### What runs

1. Sort `items` keys and run each recognition node in order.
2. **Hit + valid quantity:** record `item ID → quantity`.
3. **Miss:** do not record that ID this round (see regenerate / overwrite below).
4. After all nodes finish, update memory, write `./debug/record/IMS.json`, and set `updated_at` to when this snapshot was produced.

Hits also emit localized item name + quantity via UI Focus.

### Regenerate vs overwrite (`page_dedup`)

| `page_dedup` | Mode | Behavior |
| --- | --- | --- |
| `false` (default) | **Regenerate** | Rebuild the cache from this round’s hits only. IDs not seen this round disappear from the new map. |
| `true` | **Overwrite** | Start from the existing cache; **overwrite** quantities for IDs hit this round; keep old values for IDs not seen. |

Overwrite is for paged lists: page 1 regenerates; later pages overwrite only the IDs visible on that page so earlier pages are not wiped.

The reserved entry `SyncItemData` defaults to:

```text
First pass: SyncItemDataRunFull (page_dedup = false, full rebuild)
  next[0]: [JumpBack]SyncItemDataScrollPage → swipe, then SyncItemDataRunInc (page_dedup = true)
  next[1]: SyncItemDataLock (scan finished)
```

Extra page rounds are controlled only by `SyncItemDataScrollPage.max_hit` (currently 1). When `max_hit` is exhausted, JumpBack no longer matches and Lock runs. The node is `enabled=false` on Win32-Front by default; ADB enables it and overrides the action with a swipe-up.

### Once per Resource (implementation)

The entry node handles this automatically; callers only need to keep invoking `SyncItemData`:

1. **First call:** actually open the Progression tab, scan, write the cache, then close the “scan again” path with a Resource-level override for the rest of this run.
2. **Later calls:** the entry is still reachable, but it immediately decides “already synced this run”, skips the scan, and continues.
3. **Next cold start:** reloading the Resource / restarting the client restores the path; the next IMS task will scan again.

> Reserved entry and scan parameters: `assets/resource/pipeline/IMS/SyncItemData.json`.
>
> Use `EnsureItemDataReadyMain` only for special “enter A2 only if R2 says stale” cases. Normal IMS tasks should call `SyncItemData` directly, not rely on the expiry gate alone.

---

## A1: `UpdateItemQuantity`

When you already know a gain or spend, adjust one cached item without a full rescan.

| Param | Meaning |
| --- | --- |
| `item` | Item ID |
| `delta` | Signed change (positive gain, negative spend) |

Result is clamped to `>= 0`. Persists `items` in `IMS.json` but **does not** change readiness / sync timestamp (`hasData` / `updated_at` are established only by A2).

---

## A3: `AddItemData`

A3 takes the same optional `items` map as A2 (key = item ID, value = recognition node; `box_index` must point at quantity).

**Default full set:** when `items` is **omitted or empty** (or `custom_action_param` is `{}`), A3 loads the **`a3`** section of [`assets/data/IMS/items.json`](../../../assets/data/IMS/items.json). Maintain reward-recognizable items there; call sites usually need not copy a large `items` map.

A3-only optional parameter:

| Param | Meaning |
| --- | --- |
| `mask_hit_region` | After each hit, paint that item’s region green `(0,255,0)` on the working screenshot and **keep recognizing the same item ID** until no more hits. Handles dual stacks on one reward popup (e.g. base 100 + bonus 15). Defaults to **`true`** when omitted. |

It recognizes items on the **current screen**, then **computes** against the IMS cache: each hit quantity is added as a **positive delta** (same as repeated A1 `+n`). It does **not** refresh the sync timestamp / readiness.

### Difference from A2

| | A2 | A3 |
| --- | --- | --- |
| Write model | **Absolute:** scanned count becomes current stock | **Computed:** scanned count is added to existing stock |
| Typical use | Full Progression-tab sync | Reward popup “how much more did we get?” |
| Establishes readiness | Yes (`updated_at`, `hasData`) | No |

### Works without a cache

Unlike the other IMS pieces, A3 **does not require** an existing IMS cache.

If A2 has never succeeded (`hasData=false`), A3 still recognizes rewards but **does not write the cache**, and still returns success so closing rewards is not blocked. Each hit prints one Focus line (e.g. “Gained xxx ×n”); it does not mention IMS init / skip-persist, and does not print a summary.

When the cache is ready it also prints one Focus per hit — no Pipeline Starting/Succeeded Focus and no summary line.

> Progression-tab `IMS/item/*` ROIs often do not fit the rewards UI—pass nodes for the current screen. Reward popups animate in; use `pre_wait_freezes` on the item ROI before A3 (see `ProtocolSpaceRewardAddItemData`).
>
> Reference Pipeline: `AddItemDataOnRewards` → `AddItemDataCloseRewards`.
>
> Close-reward paths already wired to A3: `SceneNoticeRewardsConfirm` (DailyRewards / Dijiang fast collect, etc.), `CreditShoppingClaimConfirm`, `MFGCabinClaimRewardClose`, `GrowthChamberClaimRewardClose`.

---

## R1: `ItemQuantitySatisfied`

Checks whether cached item quantities meet a boolean expression.

Same operators as [`ExpressionRecognition`](../custom.md#expressionrecognition), but placeholders read **IMS cache item IDs**, not on-screen OCR nodes.

| Param | Meaning |
| --- | --- |
| `expression` | Boolean expression; use `{ITEM_ID}` for cached quantities (missing = `0`) |
| `notify_ui` | Whether to announce the resolved expression on UI Focus; default `false` (off) |

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
        "expression": "({PROTODISK}+{CAST_DIE})>=100",
        "notify_ui": false
    }
}
```

More examples:

- `{PROTODISK}>=40`
- `{PROTODISK}+{CAST_DIE}>=100 && {T_CREDS}<50`
- `!({HEAVY_CAST_DIE}<10)`

The expression result must be boolean.

R1 does **not** check readiness. For “ready **and** enough”, `And` R2 (`ItemDataReady`) with R1 so “not synced yet” is not treated as “need to farm”.

UI Focus announce runs only when `notify_ui` is `true` (resolved expression; identical lines throttled ~10s). Keep the default off for dispatch-style `next` scans to avoid spam.

---

## R2: `ItemDataReady`

Checks whether the **whole** IMS cache is usable for business decisions.

### Conditions

1. **Cache exists**  
   At least one successful A2 (`hasData=true`). Otherwise miss (`reason=no_data`).

2. **Not expired**  
   Compare sync time (`updated_at` / in-memory last sync) against `refresh_days`. Expired → miss (`reason=stale`).

Both must pass for a hit.

### What “expired” means

A2 writes `updated_at`. R2 treats the cache as stale when “now − sync time” exceeds `refresh_days`:

| `refresh_days` | Meaning |
| --- | --- |
| `7` (default) | Fresh for 7 days after sync |
| `1` / `30` | Fresh for 1 / 30 days |
| `0` | **Never expire by age** once a successful sync exists |

Notes:

- `refresh_days = 0` only disables age expiry; **missing data still misses**.
- “Never expire by age” ≠ “never scan”: with no data, `EnsureItemDataReadyMain` still goes to A2.

After a cold start, the first IMS access may lazy-hydrate `IMS.json` into memory once; later hot paths stay in memory.

---

## Recommended wiring

```text
Task entry
  └─ run SyncItemData once          ← real scan only once per Resource
       └─ business logic
            ├─ R2 ItemDataReady          (optional: cache OK?)
            ├─ R1 ItemQuantitySatisfied  (enough?)
            ├─ A1 UpdateItemQuantity     (known gain/spend)
            └─ A3 AddItemData            (reward add / announce)
```

When you need ready **and** enough:

```json
"all_of": [
    "ItemDataReady",
    "ItemQuantitySatisfied"
]
```

---

## Appendix

### Locations

| Path | Role |
| --- | --- |
| `agent/go-service/ims/` | Custom components and cache |
| `assets/data/IMS/items.json` | A3 reward-item catalog (default when `items` omitted; unused by A2) |
| `assets/resource/pipeline/IMS/` | Pipeline (one file per API) |
| `assets/resource/image/IMS/item/` | Item templates (`*_TEMPLATE.png`) |
| `tools/SupplyPlan/mask_ims_item_corner.py` | Top-left green mask tool |
| `tools/schema/components/ims.schema.json` | Parameter JSON Schema |

| Pipeline file | Contents |
| --- | --- |
| `SyncItemData.json` | A2 entry + once-per-Resource lock |
| `UpdateItemQuantity.json` | A1 |
| `AddItemData.json` | A3 best practice (close rewards) |
| `ItemQuantitySatisfied.json` | R1 (override `expression`) |
| `ItemDataReady.json` | R2 + `EnsureItemDataReady*` |
| `common.json` / `item/*.json` | Rarity colors and per-item nodes |

### Item template green mask

Protocol Space reward badges often sit on the top-left of icons and break Progression-tab templates. Before commit:

1. Paint a **31×18** RGB `(0, 255, 0)` block on the template top-left;
2. Enable `"green_mask": true` on the TemplateMatch node.

```bash
python tools/SupplyPlan/mask_ims_item_corner.py
# preview: python tools/SupplyPlan/mask_ims_item_corner.py --dry-run
```

### Cache conventions

- In-session, process memory is authoritative; hot paths do not reread disk.
- Successful A2 / A1 / A3 (when `hasData`) also persist for the next cold start.
- `ClearCache` (tests / account switch) clears memory and does **not** reload from disk.
- Small drift is OK; periodic A2 corrects it.

### Go helpers (tests)

| Function | Description |
| --- | --- |
| `ims.MarkSynced(at, items)` | Record a successful sync |
| `ims.ClearCache()` | Clear cache (no disk reload) |
| `ims.ItemsSnapshot()` | Copy of cached quantities |

### On-disk format

```json
{
    "updated_at": "2026-07-29T12:00:00Z",
    "items": {
        "ADVANCED_COGNITIVE_CARRIER": 12,
        "PROTODISK": 40
    }
}
```
