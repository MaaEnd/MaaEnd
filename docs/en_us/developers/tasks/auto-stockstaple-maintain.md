# Development Manual - Auto Stock Staple Maintenance Documentation

This document explains the file distribution and execution flow of `AutoStockStaple`.  
It takes **Valley IV** as the main line; the **Wuling** Pipeline structure is completely symmetrical, differing only in the regional suffix and scene recognition.  
This document was updated on October 30, 2026.

## File Paths

| Path | Purpose |
| -------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `assets/interface.json` | Task mounting (`regional_development` group) |
| `assets/tasks/AutoStockStaple.json` | Task entry point, region switches, item selection, upper limits, and per-group discount options |
| `assets/resource/pipeline/AutoStockStaple/Main.json` | Execution cycle, entry initialization, regional sub-task dispatch |
| `assets/resource/pipeline/AutoStockStaple/ValleyIV.json` | Valley IV list scan loop |
| `assets/resource/pipeline/AutoStockStaple/Wuling.json` | Wuling list scan loop |
| `assets/resource/pipeline/AutoStockStaple/General/Item.json` | Item anchors, name/discount recognition, BetterSliding, purchase confirmation |
| `assets/resource/pipeline/AutoStockStaple/General/Goods.json` | Item OCR within the purchase popup |
| `assets/resource/pipeline/AutoStockStaple/General/GoodsCountValidate.json` | OCR of held quantity in the top-right corner of the popup |
| `assets/resource/pipeline/AutoStockStaple/General/QuantityControl.json` | Popup branch dispatch, item exclusion, purchase confirmation |
| `assets/resource/pipeline/AutoStockStaple/General/Template.json` | Common templates for sold-out, stock bills, purchase confirmation, etc. |
| `assets/resource/pipeline/Interface/InScene/StockStaple.json` | Region and staple goods interface scene recognition |
| `assets/resource_adb/pipeline/AutoStockStaple/` | ADB ROI offset mirror (must be checked in sync with Win32) |
| `agent/go-service/autostockstaple/action.go` | Calculates purchase quantity and drives BetterSliding |
| `agent/go-service/common/attachregex/action.go` | Merges attach keywords into an OCR whitelist regex |
| `assets/locales/interface/*.json` | Task, option, and focus text |

## Execution Flow

1. Check if today is within the [execution cycle](#execution-cycle); if not, end directly.
2. Read the user-selected purchase items, [merge them into per-group product name OCR whitelists](#attach-and-whitelist-initialization) (implementation in `Main.json` + `attachregex/action.go`).
3. Sequentially enter enabled regions (Valley IV / Wuling) based on options, and jump to the staple goods interface for that region.
4. Loop scan on the list page, judging sequentially each round:
    - Whether remaining stock bills are [below the retention threshold](#stock-bill-retention-threshold) → stop scanning this region.
    - Whether a [buyable target item is recognized](#item-recognition-chain-and-discount-groups) → click to enter the purchase popup; discount groups are tried in order within the same round.
    - Whether it is sold out → stop scanning this region.
    - Otherwise, swipe down the list to continue searching, at most 25 swipes per region (`AutoStockSwipe{Region}.max_hit`); still being on the staple page after the budget runs out means the list has reached its end.
5. Handle the purchase popup according to the [three-branch quantity control](#quantity-control-three-branches): exit due to insufficient stock bills / purchase if under the limit / exclude if the limit is reached.
6. End after all enabled regions are completed.

> Recognizing and clicking an item in the list does not equal a successful purchase; an order is only placed when the quantity control reaches the confirmation step.

## Special Processing

### Execution Cycle

Implemented in `Main.json`. The days of the week selected by the user are written into the `attach` of the cycle node. `ScheduleRecognition` determines if execution should occur today; for unselected days, the task ends directly and does not enter the purchase flow.

### Attach and Whitelist Initialization

This task does not rely on runtime concatenation of user input strings, but rather:

1. User selects items in the interface (after the category switch is on, the item checkboxes are nested under it) → `assets/tasks/AutoStockStaple.json` writes the multilingual item names into the item name node of the **discount group the item belongs to**: `AutoStockInStapleItemName{Group}.attach.{slug}`.
2. The task entry executes `AttachToExpectedRegexAction` with `targets` listing every group item name node. For each node it reads that node's attach keywords and merges them into a `^(alias1|alias2|...)$` regex written back to the same node.
3. Keys where `attach` is `false` are excluded and no longer enter the whitelist.

The Exclude branch (when an item has reached its target or is removed due to insufficient stock bills) also triggers re-initialization, ensuring subsequent list scans do not click on excluded items.  
The exclusion action uses `PipelineOverrideAction` to set `attach.{slug}` to `false` in the item's discount group.

### Stock Bill Retention Threshold

Implemented in `ValleyIV.json` / `Wuling.json` (node names for Wuling have the suffix `Wuling`).

The **first check** in the scan loop: the stock bill OCR reading in the top-right corner is compared using an expression with the user-configured retention threshold.  
If "retention threshold > current remaining stock bills", it indicates there are not enough stock bills to continue, ending the region scan; otherwise, it continues searching for items.

The list scan phase does **not** judge whether the unit price is affordable; affordability is handled within the purchase popup.

### Item Recognition Chain and Discount Groups

Implemented in `General/Item.json` (item name and discount nodes) + `AutoStockBuyItem{Region}Task{Group}` in the regional JSON. The approach is similar to the [Credit Store](./credit-shopping-maintain.md): **first find the anchor, then offset to recognize subsequent fields**, but the anchor is the **remaining refresh time frame** in the top-left corner of the item card (ColorMatch over three colors: cyan / yellow / red), not the credit icon.

```text
Remaining Time Anchor -> Item Name (Color + group OCR whitelist) -> Discount (OCR matches this group's selected tier)
```

Discounts are configured per group, and the groups are the same as the [quantity limit groups](#quantity-control-purchase-popup):

| Group `{Group}` | Covered items | Item name node | Discount node |
| --- | --- | --- | --- |
| `Daily` | Household goods except the engraving permit | `AutoStockInStapleItemNameDaily` | `AutoStockInStapleItemDiscounts{Region}Daily` |
| `Industrial` | Industrial goods except the artificing catalyst | `AutoStockInStapleItemNameIndustrial` | `AutoStockInStapleItemDiscounts{Region}Industrial` |
| `Cultural` | Cultural goods | `AutoStockInStapleItemNameCultural` | `AutoStockInStapleItemDiscounts{Region}Cultural` |
| `EngravingPermit` | Engraving permit (`item_gem_stable_energy_point_*`) | `AutoStockInStapleItemNameEngravingPermit` | `AutoStockInStapleItemDiscounts{Region}EngravingPermit` |
| `ArtificingCatalyst` | Artificing catalyst (`item_equip_enhance_*`) | `AutoStockInStapleItemNameArtificingCatalyst` | `AutoStockInStapleItemDiscounts{Region}ArtificingCatalyst` |

`{Region}` is `ValleyIV` or `Wuling`. Only Wuling sells the artificing catalyst, so `AutoStockInStapleItemDiscountsValleyIVArtificingCatalyst` does not exist.

1. **Anchor**: Locates the time region of each item card in the list, serving as the basis for subsequent offsets.
2. **Item Name**: Anchor -> Name label color -> Name text color -> that group's item name OCR; only items written into that group's whitelist match. `AutoStockInStapleItemName{Group}_Expected` combines the three steps into "this group has a buyable item".
3. **Discount**: Offsets from that group's item name region to the discount position; the OCR `expected` lists only that group's selected tier and deeper discounts (e.g. `-99` / `-95` / `-90` for the `-90%` tier, `order_by: Expected`); a reading that lost its minus sign does not match — a missed purchase instead of a wrong one.
4. **Discount Tier**: The numeric tiers of `AutoStockMinDiscount{Region}{Group}` rewrite that group's discount node `expected` directly; there is no separate comparison node. Items without a discount badge produce no OCR text and never match, so they are never bought unless the "Any" tier is selected.
5. **Purchase Task**: `AutoStockBuyItem{Region}Task{Group}` = anchor + `AutoStockInStapleItemName{Group}_Expected` + that group's discount node (`box_index: 2`). The regional scan loop tries `EngravingPermit` -> `ArtificingCatalyst` -> `Daily` -> `Industrial` -> `Cultural` in order and buys on the first hit; groups are disjoint, so the order only decides which of several buyable cards is clicked first.

Only when the anchor, item name, and discount all match is the item clicked, entering quantity control.

> [!NOTE]
> The group only decides **which discount tier applies**; it does not affect quantity limits: the engraving permit uses the engraving permit limit, the artificing catalyst uses the artificing catalyst limit, and the rest follow their category limit. See [Adding a New Item](#adding-a-new-item).

### Quantity Control Three-Branches

Implementation distributed across `General/QuantityControl.json`, `Goods.json`, `GoodsCountValidate.json`, and `autostockstaple/action.go`.  
After the popup opens, it attempts the dedicated branch for each item sequentially; each item has a fixed three-way path:

#### Branch 1: Insufficient Stock Bills

When the bottom red `调度券不足` (insufficient stock bills) prompt is identified in the popup:

1. Exclude the item from the attach whitelist of its group.
2. Re-merge the regex whitelists of all groups.
3. Close the popup, return to the list—avoid repeatedly clicking unaffordable items.

#### Branch 2: Held Quantity Below Limit, Execute Purchase

Read the OCR of the current held quantity in the top-right corner of the popup, and compare it using an expression with the user-configured limit (matched when `limit > current held quantity`):

1. The Go action calculates `quantity_to_buy = limit - current_held_quantity`.
2. Write the result into BetterSliding's `TargetQuantity` to smoothly adjust the purchase quantity slider.
3. Click confirm purchase, close the reward popup, and return to the list to continue scanning.

#### Branch 3: Held Quantity Reaches or Exceeds Limit, Exclude

Matched when `limit <= current_held_quantity`:

1. Exclude the item from the attach whitelist of its group.
2. Re-merge the regex whitelists of all groups.
3. Close the popup, do not purchase, and continue scanning other items.

> **Example**: Valley Engraving Permit limit is 200, current held is 198; Branch 2 will calculate buying 2 more. If 200 are already held, Branch 3 triggers direct removal, and this item will not be clicked again this round.

### Runtime Override Summary

| Timing | Action | Purpose |
| -------------------------- | ------------------------------------------------------------- | --------------------------------------------------------------- |
| Task Entry | `AttachToExpectedRegexAction` | Merge attach per group → that group's item name OCR regex |
| Task Entry | Option `pipeline_override` (category switch / group discount select) | When a category is off, its overrides are not collected; a group's numeric tier rewrites that group's discount OCR `expected`, and the "Any" tier replaces that group's discount recognition node with an always-matching `ColorMatch` |
| After Item Exclusion | `PipelineOverrideAction` + then `AttachToExpectedRegexAction` | Remove the attach key from its group and refresh all group whitelists |
| Before Confirming Purchase | `AutoStockStapleQuantityControlAction` | Calculate difference and override BetterSliding target quantity |

## Data Source and Incremental Updates

`tools/zmdmap/datasets/stock_staple.py` builds the product roster, categories, prices, and names in five languages from raw TableCfg. The consumer maintains no fixed product binding table. The data relationships are:

```text
DomainDataTable → ShopGroupTable → ShopTable → ShopGoodsTable
                                           ├ RewardTable → ItemTable → five I18nTextTable languages
                                           ├ ShopGoodsTagTable → localized category names
                                           └ ShopChannelDevelopmentTable → supply by channel level
                  ↓
stock_staple.json → AutoStockStaple/generate.mjs
                  ├ assets/tasks/AutoStockStaple.json
                  ├ General/{Goods,GoodsCountValidate,QuantityControl}.json
                  └ task text in assets/locales/interface/*.json
```

Only weekly-restocked goods (`limitCountRefreshType=Weekly/2`) in regular regional shops (`shopRefreshType=None/0`) are selected; goods that are not restocked weekly, such as `Forever` (0 in integer form), and fluctuating shops are excluded. TableCfg enums may be written as names or integers, and the filter accepts both. `goods` preserves prices, discounts, currency IDs, reward quantities, refresh types, unlock conditions, and channel supply records by goods ID. Task options are then deduplicated by `item_id`, preserving different price tiers of the same item.

Existing task, Pipeline, and locale files form the baseline. The generator identifies registered items from their task option's `resource/image/UI/Item/<item_id>.png` path and adds only new upstream items, without a separate binding table. Existing keys, names, OCR adjustments, ROIs, node order, and quantity policies stay unchanged. Upstream renames, category changes, and removals do not automatically rewrite or delete existing configurations, so saved selections remain valid.

`tools/pipeline-generate/AutoStockStaple/pipeline-template.json` contains only the node template for new items; `messages.json` contains event text patterns in five languages. New option names remove apostrophes from the upstream English name and convert it to PascalCase. Attach keys and messages use game `item_id` values; node suffixes use their PascalCase form. Previously generated items only receive missing nodes and messages; existing values are preserved. When there are no additions, no files are written.

Download the published upstream data and incrementally extend the task configuration:

```shell
pnpm generate:AutoStockStaple
```

With local raw tables, data rebuilding and the same incremental update work offline. They only need the eight business tables above and `I18nTextTable_{CN,TC,EN,JP,KR}.json`, without JsonData, navmesh, other compact datasets, or website query APIs:

```shell
uv run zmdmap generate --only stock_staple --table-cfg-dir /path/to/TableCfg
node tools/pipeline-generate/AutoStockStaple/generate.mjs
```

Restore missing or corrupt baseline files from Git before running the incremental update. Upstream item tables do not contain historical OCR adjustments and cannot reconstruct those handwritten configurations. The generator writes only after all inputs parse and all additions build successfully, preserving JSONC comments and other tasks' locale content. The existing icon publishing workflow continues to maintain UI icons.

`.github/workflows/sync-zmdmap.yml` runs the same command in the existing synchronization job. Like other datasets, `stock_staple` is automatically discovered, built, and included in the manifest by zmdmap, then downloaded and materialized through the existing `fetch` command. Valid caches can rebuild missing, corrupt, or stale materialized copies; changed manifest hashes trigger downloads even within the same version.

Static prices preserve complete sales data. Runtime purchasing still recognizes actual discounts, held quantities, and stock bills through the existing flow rather than calculating the current transaction from configured prices.

## Paths to Modify When Adding a New Region

Copy a set corresponding to Valley IV (Wuling is already an existing mirror):

```text
Anchor AutoStockInStapleItem
  -> Group Item Name AutoStockInStapleItemName{Group}_Expected (one per group)
  -> Group Discount AutoStockInStapleItemDiscounts{Region}{Group}
  -> Click and Enter Quantity Control (AutoStockBuyItem{Region}Task{Group})
```

### 1. Anchor: Remaining Refresh Time Frame

`AutoStockInStapleItem` uses `ColorMatch` to identify the remaining time area in the top-left corner of the item card (connected domain over the union of three HSV ranges — cyan / yellow / red — `order_by: Vertical`), serving as the basis `box` for all subsequent offset recognition.

### 2. Offset Recognition of Item Name

Offset sequentially based on the anchor:

1. `AutoStockInStapleItemNameLabelColor`: Name label background color.
2. `AutoStockInStapleItemNameTextColor`: Name text color.
3. `AutoStockInStapleItemName{Group}`: OCR recognizes the item name, `expected` written during runtime initialization; every discount group has one such node.
4. `AutoStockInStapleItemName{Group}_Expected`: `And` combination of the above three, `box_index: 2` takes the box from the item name OCR.

Item names selected by the user are written into multilingual aliases via `AutoStockInStapleItemName{Group}.attach.{slug}` of their discount group; at task start, `AttachToExpectedRegexAction` merges each group node into:

```text
^(alias1|alias2|...)$
```

Unselected items do not enter the whitelist; OCR will not match them.

### 3. Offset Recognition of Discount

`AutoStockInStapleItemDiscounts{Region}{Group}` uses the box of that group's `AutoStockInStapleItemName{Group}` as the basis, with `roi_offset` offset to the discount area. By default its OCR `expected` lists every discount value from `-99` to `-50` (`order_by: Expected`).

Each group has its own discount threshold controlled by `AutoStockMinDiscount{Region}{Group}` (select), which shares the `option.DiscountValue.*` locale key family with the Credit Store:

- **Numeric tiers** (`-95%` … `-50%`): Rewrite the `expected` of that group's discount node to that tier and deeper discounts (e.g. `-90%` → `-99`, `-95`, `-90`); the OCR must match one of them.
- **Any**: Replaces that group's discount OCR node entirely by `ColorMatch` (full-range threshold `[0,0,0]`–`[255,255,255]` with `count` defaulting to 1, so any non-empty ROI passes and the presence of a discount color block does not affect the result) — equivalent to buying all selected items of that group without checking the discount.

Tiers list only the discounts that actually occur for staple goods (`-95/-90/-85/-80/-75/-70/-65/-60/-50`); the default tier comes from each group select's `default_case` (currently every Valley IV group is `-50%`; in Wuling the engraving permit and cultural goods default to `-95%`, industrial goods to `-75%`, the artificing catalyst to `-90%`, and daily goods to `-50%`). Items without a discount badge produce no OCR text and never match any numeric tier; select "Any" to buy them.

### 4. Judgment of "Affordability"

Unlike the Credit Store, the staple goods list scan **does not** have a separate "unit price ColorMatch / CanAfford" node. Affordability is handled in two layers:

| Stage | Mechanism |
| --------------------- | --------------------------------------------------------------------------------------------------------------------- |
| Before List Scan | `AutoStockTargetCanNotBuyValleyIV`: Whether remaining stock bills are still above the retention threshold |
| Within Purchase Popup | `AutoStockStapleGoodsStockBillInsufficientValidate`: Recognizes the bottom red `调度券不足` (insufficient stock bills) prompt |

Therefore, the `And` conditions for `AutoStockBuyItemValleyIVTask{Group}` are:

- `AutoStockInStapleItem`
- `AutoStockInStapleItemName{Group}_Expected`
- `AutoStockInStapleItemDiscountsValleyIV{Group}` (that group's discount OCR; `box_index: 2` points to this node)

After all three match, click the item card (`target_offset: [-45, 85, -30, 0]`), and `next` enters `AutoStockStapleQuantityControl`.

> [!IMPORTANT]
> `AutoStockBuyItemValleyIVTask{Group}` only means "a candidate item was recognized and entered purchase judgment," **it does not** mean the purchase is completed. Whether an order is actually placed depends on if the quantity control branch reaches `AutoStockStapleQuantityControlConfirmBuy`.

### 5. Sold-Out and Swiping

- `SoldOut`: OCR recognizes text like "Sold Out / 已售罄" on the left; once matched, it stops swiping.
- `AutoStockSwipe{Region}`: swipes down within the regional page, `post_wait_freezes` waits for the list area to stabilize before entering the next round of recognition. `max_hit` is `25`, i.e. at most 25 swipes per region; the count accumulates within one task run, and purchase round-trips do not consume it.
    - The loop has only two exit signals: `SoldOut` matches, or the 25 swipes run out and `AutoStockNoMoreTarget{Region}` catches it (it sits after the swipe node, so it only hits once that node is skipped by `max_hit`), ending the region scan as "list at its end and no matching item".
    - So `max_hit` and `AutoStockNoMoreTarget{Region}` must be kept as a pair: together they are the only exit for "swiped everything without a result"; without it the region spins. Raise `max_hit` only when the list is longer than 25 screens.
    - `ListCompleteRecognition` (comparing two consecutive frames to detect the end) briefly replaced this attempt cap: when the swipe itself does not take effect, both frames are equally static, so it mistook "did not scroll" for "reached the end" and skipped a whole screen of the list — hence the revert to an attempt cap.

## Quantity Control (Purchase Popup)

After clicking an item, enter the purchase popup. `AutoStockStapleQuantityControl` confirms the popup is open via title OCR ("Purchase / 购买商品"), then attempts each item's `AutoStockStapleQuantityControl{Item}` node sequentially via the `next` list.

Taking `AutoStockStapleQuantityControlValleyEngravingPermit` (Valley Engraving Permit) as an example, its `next` order is fixed as:

```text
AutoStockStapleQuantityControlValleyEngravingPermitStockBillInsufficient
  -> AutoStockStapleQuantityControlValleyEngravingPermitBuy
  -> AutoStockStapleQuantityControlValleyEngravingPermitExclude
```

### Reading Current Held Quantity

The held quantity in the top-right corner of the popup is recognized by `AutoStockStapleGoodsCountValidate`:

- `AutoStockStapleGoodsCountValidateArea` → `AutoStockStapleGoodsCountValidateColor`: first find the gray quantity box in a fixed ROI, then the bright digit area inside it.
- `AutoStockStapleGoodsCountValidateText`: OCR reads `\d+`.

Each item's Buy/Exclude branch uses `ExpressionRecognition` to compare with the user-configured limit, for example:

```text
Buy:     {ValleyEngravingPermitLimit} > {AutoStockStapleGoodsCountValidate}
Exclude: {ValleyEngravingPermitLimit} <= {AutoStockStapleGoodsCountValidate}
```

### Branch 1: Insufficient Stock Bills, Direct Exit

`AutoStockStapleQuantityControl{Item}StockBillInsufficient` combines:

- Current item OCR (e.g., `AutoStockStapleGoodsValleyEngravingPermit`)
- `AutoStockStapleGoodsStockBillInsufficientValidate` (bottom red area ColorMatch)

Upon match:

1. `[JumpBack]` to `{Item}RemoveFilter`, **excludes** the item from `AutoStockInStapleItemName.attach` (`attach.{slug}: false`), and triggers `AutoStockStapleQuantityControlResetRecognitionParams` to regenerate the whitelist regex.
2. Close the purchase popup (`AutoStockStapleQuantityControlCloseBuyWindow`).

This ensures subsequent list scans do not repeatedly click unaffordable items.

### Branch 2: Quantity Below Target, Execute Purchase

`AutoStockStapleQuantityControl{Item}Buy` matches when both item OCR + `Validate` expression conditions are met, executing the dedicated Custom action `AutoStockStapleQuantityControlAction` (`agent/go-service/autostockstaple/action.go`).

Action logic:

1. Read the corresponding `AutoStockStapleGoods{Item}Validate` node expression, parsing the **target limit** and **quantity OCR node name**.
2. Run quantity OCR on the current screenshot to get the **current held quantity**.
3. Calculate `target = target_limit - current_held_quantity`.
4. If `target <= 0`, only log and return success without any override (`AutoStockStapleBetterSliding` keeps its current state).
5. Otherwise, enable `AutoStockStapleBetterSliding` via `OverridePipeline` and write `target` into its `attach.TargetQuantity`.

Go **no longer** executes `RunTask` to perform sliding; quantity adjustment is completed by the low-code branch:

```text
{Item}Buy (Go: calculate target + OverridePipeline)
  ├ AutoStockStapleCheckSliding              (Skip sliding when slider is hidden and default quantity is 1)
  └ AutoStockStapleBetterSliding             (Execute BetterSliding after Go enables it)
  -> AutoStockStapleQuantityControlConfirmBuy
```

`AutoStockStapleBetterSliding` is defined in `General/Item.json`, with `enabled: false` by default; it is only enabled after Go override; the default value of `attach.TargetQuantity` is just a placeholder.

After the purchase quantity adjustment is complete, `next` enters `AutoStockStapleQuantityControlConfirmBuy` to click the yellow confirm button, then closes the reward popup and returns to the list.

### Branch 3: Quantity Reached or Exceeded Target, Exclude and Re-initialize

`AutoStockStapleQuantityControl{Item}Exclude` matches when `ExcludeValidate` holds (current held quantity is **not less than** the user limit).

Flow:

1. `{Item}RemoveFilter`: Calls `PipelineOverrideAction`, setting the item's attach key in its discount group to `false`, effectively removing it from that group's whitelist.
2. `AutoStockStapleQuantityControlResetRecognitionParams`: Executes `AttachToExpectedRegexAction` again (`targets` covers every group), **regenerating** each group's `AutoStockInStapleItemName{Group}.expected` regex based on the latest attach state.
3. Close the purchase popup, return to the list to continue scanning other items.

The Exclude branch **does not** purchase; it only removes "reached target" items from the current scan objectives.

## Summary of Initialization and Override Mechanism

This task involves several types of runtime overrides; do not confuse them during maintenance:

| Action | Trigger Location | Purpose |
| -------------------------------------- | ------------------------------------------------- | ------------------------------------------------------------------------------ |
| Option `pipeline_override` (declared in `interface.json`) | Task option collection (MXU render stage) | When a category is off, its overrides are not collected; a group's numeric tier rewrites that group's discount OCR `expected`, and the "Any" tier replaces that group's discount recognition node with `ColorMatch` |
| `AttachToExpectedRegexAction` | `AutoStockStapleMain` entry; Exclude → Reset node | Merge attach keywords per group → that group's OCR whitelist regex |
| `PipelineOverrideAction` | Each item's `{Item}RemoveFilter` | Set the specified attach key to `false` in its group, excluding the item |
| `AutoStockStapleQuantityControlAction` | Each item's `{Item}Buy` | Calculate difference and override BetterSliding's `TargetQuantity` / `enabled` |

`attach` semantics (see `attachregex/action.go`):

- `string` / `string[]`: Add keyword to whitelist.
- `false`: Explicitly exclude this attach key, no longer participate in merging.
- `true`: Does not add keyword under current implementation.

## Adding a New Item

After upstream tables add a product, republish `stock_staple.json` and run `pnpm generate:AutoStockStaple`. Product cases, name OCR, quantity comparison nodes, three purchase branches, and localized messages are generated together. Do not manually add product names, binding tables, or legacy aliases.

Existing quantity grouping remains task policy: the engraving-permit ID family in household goods uses the engraving-permit limit, while other household goods use the food/buff limit; the equipment-enhancement material ID family in industrial goods uses the artificing-catalyst limit, while the rest use the detector limit; cultural goods use the gift limit. Discount groups follow the same split (the engraving permit and the artificing catalyst each form their own group, the rest follow their category), so new items automatically fall under the right group's discount control and no new discount option is needed. New categories or regions requiring navigation fail explicitly until their task options and flows are adapted. If screenshots show that a product needs an OCR adjustment, edit its node directly; incremental updates preserve these adjustments.

Maintain existing flows in their task and Pipeline files. The per-item template only defines future additions and does not overwrite existing nodes.

## Adding a New Region (Referencing Valley IV)

If a third region for staple goods purchase is added in the future, a set of nodes can be copied corresponding to Valley IV:

1. Create `assets/resource/pipeline/AutoStockStaple/{Region}.json`:
    - `AutoStockInStaple{Region}` scan loop (keep the `next` order; it always ends with `[JumpBack]AutoStockSwipe{Region}` plus `AutoStockNoMoreTarget{Region}` for when the swipe budget runs out, and the group judgment entries become one `[JumpBack]AutoStockBuyItem{Region}Task{Group}` per group).
    - `AutoStockTargetCompare{Region}` / `AutoStockTargetCanNotBuy{Region}`.
    - One `AutoStockBuyItem{Region}Task{Group}` per discount group (replace the group discount node names).
    - `AutoStockSwipe{Region}` (size `max_hit` to the list length, 25 for Valley IV) and the fallback exit `AutoStockNoMoreTarget{Region}`.
2. Add `[JumpBack]AutoStockStaple{Region}` sub-task and SceneManager jump in `Main.json`.
3. Add scene OCR in `StockStaple.json` or the regional InScene file.
4. Add `AutoStockInStapleItemDiscounts{Region}{Group}` for every group in `Item.json` (`Daily` / `Industrial` / `Cultural` / `EngravingPermit`, plus `ArtificingCatalyst` when the region sells the artificing catalyst; even with the same layout each group needs its own node, since `AutoStockMinDiscount{Region}{Group}` overrides it), and add its `roi_offset` to the `resource_adb` `Item.json`.
5. Add regional switches, empty item option groups, and the per-group discount options (nested under the matching category switch) in `assets/tasks/AutoStockStaple.json`, connect the region ID to its navigation suffix in the generator, and run the incremental update.

The existing Wuling implementation is a mirror of Valley IV; you can directly diff `ValleyIV.json` and `Wuling.json` to see the differences.

## Debugging Suggestions

| Symptom | Priority Check |
| ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| Target item not recognized in list | The per-group `expected` regexes printed by `AttachToExpectedRegexAction` in `go-service.log`; whether anchor `AutoStockInStapleItem` matched |
| Only one category is never bought | Whether that category switch is on; whether the tier selected in `AutoStockMinDiscount{Region}{Group}` covers the actual discount in that group's discount node `expected` |
| Scanning stops before the list ends | Whether `AutoStockSwipe{Region}.max_hit` is too small (long list, short travel per swipe); whether `AutoStockNoMoreTarget{Region}` matched early |
| Keeps swiping without ending | Whether `AutoStockSwipe{Region}.max_hit` was removed or raised too much; whether `AutoStockNoMoreTarget{Region}` exists and sits after the swipe node |
| Item recognized but not purchased | Whether quantity control went to `Exclude` or `StockBillInsufficient`; `AutoStockStapleQuantityControl{Item}Buy/Exclude` in `maafw*.log` |
| Purchase quantity is incorrect | `threshold/current_count/target` in `AutoStockStapleQuantityControlAction` log; BetterSliding ROI |
| Scanning stops even though stock bills seem sufficient | `AutoStockTargetCompareValleyIV` expression and user input `{ReserveValleyIV}` |
| Repeatedly clicking the same reached-target item | Whether `{Item}RemoveFilter` and `ResetRecognitionParams` executed after Exclude |

Log analysis can reference the skill: `.agents/skills/autostockstaple-log-analysis/SKILL.md`.

## Differences from AutoStockpile

| Aspect | AutoStockStaple (Staple Demand) | AutoStockpile (Elastic Demand) |
| ---------------- | ----------------------------------- | ------------------------------------ |
| Decision Core | Pipeline + minimal Go | Go Service dominated |
| Item Location | List time anchor + OCR offset chain | Template matching + OCR mapping |
| Quantity Control | Popup BetterSliding + expressions | Go parses detail page to adjust |

Both have similar interfaces but independent logic; log analysis is in `.agents/skills/autostockstaple-log-analysis/SKILL.md`.
