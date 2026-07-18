# Development Manual - Auto Stock Staple Maintenance Documentation

This document explains the file distribution and execution flow of `AutoStockStaple`.  
It takes **Valley IV** as the main line; the **Wuling** Pipeline structure is completely symmetrical, differing only in the regional suffix and scene recognition.  
This document was updated on June 6, 2026.

## File Paths

| Path                                                                       | Purpose                                                                               |
| -------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `assets/interface.json`                                                    | Task mounting (`regional_development` group)                                          |
| `assets/tasks/AutoStockStaple.json`                                        | Task entry point, region switches, item selection, upper limits, and discount options |
| `assets/resource/pipeline/AutoStockStaple/Main.json`                       | Execution cycle, entry initialization, regional sub-task dispatch                     |
| `assets/resource/pipeline/AutoStockStaple/ValleyIV.json`                   | Valley IV list scan loop                                                              |
| `assets/resource/pipeline/AutoStockStaple/Wuling.json`                     | Wuling list scan loop                                                                 |
| `assets/resource/pipeline/AutoStockStaple/General/Item.json`               | Item anchors, name/discount recognition, BetterSliding, purchase confirmation         |
| `assets/resource/pipeline/AutoStockStaple/General/Goods.json`              | Item OCR within the purchase popup                                                    |
| `assets/resource/pipeline/AutoStockStaple/General/GoodsCountValidate.json` | OCR of held quantity in the top-right corner of the popup                             |
| `assets/resource/pipeline/AutoStockStaple/General/QuantityControl.json`    | Popup branch dispatch, item exclusion, purchase confirmation                          |
| `assets/resource/pipeline/AutoStockStaple/General/Template.json`           | Common templates for sold-out, dispatch tickets, purchase confirmation, etc.          |
| `assets/resource/pipeline/Interface/InScene/StockStaple.json`              | Region and stable supply interface scene recognition                                  |
| `assets/resource_adb/pipeline/AutoStockStaple/`                            | ADB ROI offset mirror (must be checked in sync with Win32)                            |
| `agent/go-service/autostockstaple/action.go`                               | Calculates purchase quantity and drives BetterSliding                                 |
| `agent/go-service/common/attachregex/action.go`                            | Merges attach keywords into an OCR whitelist regex                                    |
| `tools/pipeline-generate/AutoStockStaple/General/`                         | Batch generation of Goods / CountValidate / QuantityControl                           |
| `assets/locales/interface/*.json`                                          | Task, option, and focus text                                                          |

## Execution Flow

1.  Check if today is within the [execution cycle](#execution-cycle); if not, end directly.
2.  Read the user-selected purchase items, [merge them into a product name OCR whitelist](#attach-and-whitelist-initialization) (implementation in `Main.json` + `attachregex/action.go`).
3.  Sequentially enter enabled regions (Valley IV / Wuling) based on options, and jump to the stable supply interface for that region.
4.  Loop scan on the list page, judging sequentially each round:
    - Whether remaining dispatch tickets are [below the retention threshold](#dispatch-ticket-retention-threshold) → stop scanning this region.
    - Whether a [buyable target item is recognized](#item-recognition-chain) → click to enter the purchase popup.
    - Whether it is sold out → stop scanning this region.
    - Otherwise, swipe down the list to continue searching (max 25 swipes per region).
5.  Handle the purchase popup according to the [three-branch quantity control](#quantity-control-three-branches): exit due to insufficient tickets / purchase if under the limit / exclude if the limit is reached.
6.  End after all enabled regions are completed.

> Recognizing and clicking an item in the list does not equal a successful purchase; an order is only placed when the quantity control reaches the confirmation step.

## Special Processing

### Execution Cycle

Implemented in `Main.json`. The days of the week selected by the user are written into the `attach` of the cycle node. `ScheduleRecognition` determines if execution should occur today; for unselected days, the task ends directly and does not enter the purchase flow.

### Attach and Whitelist Initialization

This task does not rely on runtime concatenation of user input strings, but rather:

1.  User selects items in the interface → `assets/tasks/AutoStockStaple.json` writes the multilingual item names into `attach.{slug}`.
2.  The task entry executes `AttachToExpectedRegexAction`, reads all attach keywords, and merges them into a `^(alias1|alias2|...)$` regex, which overrides the item name OCR node on the list page.
3.  Keys where `attach` is `false` are excluded and no longer enter the whitelist.

The Exclude branch (when an item has reached its target or is removed due to insufficient tickets) also triggers re-initialization, ensuring subsequent list scans do not click on excluded items.  
The exclusion action uses `PipelineOverrideAction` to set the corresponding `attach.{slug}` to `false`.

### Dispatch Ticket Retention Threshold

Implemented in `ValleyIV.json` / `Wuling.json` (node names for Wuling have the suffix `Wuling`).

The **first check** in the scan loop: the dispatch ticket OCR reading in the top-right corner is compared using an expression with the user-configured retention threshold.  
If "retention threshold > current remaining tickets", it indicates there are not enough tickets to continue, ending the region scan; otherwise, it continues searching for items.

The list scan phase does **not** judge whether the unit price is affordable; affordability is handled within the purchase popup.

### Item Recognition Chain

Implemented in `General/Item.json` + the discount node within the regional JSON. The approach is similar to the [Credit Store](./credit-shopping-maintain.md): **first find the anchor, then offset to recognize subsequent fields**, but the anchor is the **remaining refresh time frame** in the top-left corner of the item card (cyan ColorMatch), not the credit icon.

```text
Remaining Time Anchor -> Item Name (Color + OCR Whitelist) -> Discount (OCR or ColorMatch)
```

1.  **Anchor**: Locates the time region of each item card in the list, serving as the basis for subsequent offsets.
2.  **Item Name**: Anchor -> Name label color -> Text background color -> OCR; only matches items selected by the user in the whitelist.
3.  **Discount**: Offsets from the name region to the discount position; by default, OCR recognizes specific discount values (95/90/85/...), which can be changed by an option to "any discount" (pass if a discount color block exists) or to specify a minimum discount tier.

Only when all three match is the item clicked, entering quantity control.

### Quantity Control Three-Branches

Implementation distributed across `General/QuantityControl.json`, `Goods.json`, `GoodsCountValidate.json`, and `autostockstaple/action.go`.  
After the popup opens, it attempts the dedicated branch for each item sequentially; each item has a fixed three-way path:

#### Branch 1: Insufficient Dispatch Tickets

When the bottom red "Insufficient Dispatch Tickets" prompt is identified in the popup:

1.  Exclude the item from the attach whitelist.
2.  Re-merge the regex whitelist.
3.  Close the popup, return to the list—avoid repeatedly clicking unaffordable items.

#### Branch 2: Held Quantity Below Limit, Execute Purchase

Read the OCR of the current held quantity in the top-right corner of the popup, and compare it using an expression with the user-configured limit (matched when `limit > current held quantity`):

1.  The Go action calculates `quantity_to_buy = limit - current_held_quantity`.
2.  Write the result into BetterSliding's `Target` to smoothly adjust the purchase quantity slider.
3.  Click confirm purchase, close the reward popup, and return to the list to continue scanning.

#### Branch 3: Held Quantity Reaches or Exceeds Limit, Exclude

Matched when `limit <= current_held_quantity`:

1.  Exclude the item from the attach whitelist.
2.  Re-merge the regex whitelist.
3.  Close the popup, do not purchase, and continue scanning other items.

> **Example**: Valley Engraving Permit limit is 50, current held is 48; Branch 2 will calculate buying 2 more. If 50 are already held, Branch 3 triggers direct removal, and this item will not be clicked again this round.

### Runtime Override Summary

| Timing                     | Action                                                        | Purpose                                                         |
| -------------------------- | ------------------------------------------------------------- | --------------------------------------------------------------- |
| Task Entry                 | `AttachToExpectedRegexAction`                                 | Merge attach → item name OCR regex                              |
| After Item Exclusion       | `PipelineOverrideAction` + then `AttachToExpectedRegexAction` | Remove attach key and refresh whitelist                         |
| Before Confirming Purchase | `AutoStockStapleQuantityControlAction`                        | Calculate difference and override BetterSliding target quantity |

## Paths to Modify When Adding a New Item

1.  `tools/pipeline-generate/AutoStockStaple/General/data.mjs` (`id`, `slug`, multilingual `expected`)
2.  Re-generate (in repository root directory):

```bash
npx @joebao/maa-pipeline-generate --config tools/pipeline-generate/AutoStockStaple/General/goods-config.json
npx @joebao/maa-pipeline-generate --config tools/pipeline-generate/AutoStockStaple/General/goods-count-validate-config.json
npx @joebao/maa-pipeline-generate --config tools/pipeline-generate/AutoStockStaple/General/quantity-control-config.json
```

1.  `assets/tasks/AutoStockStaple.json` (checkbox case + limit override)
2.  `assets/locales/interface/*.json` (options and `quantity_control.buy.*` focus text)

Generation rules are in [`tools/pipeline-generate/AutoStockStaple/General/README.md`](../../../../tools/pipeline-generate/AutoStockStaple/General/README.md).

## Paths to Modify When Adding a New Region

Copy a set corresponding to Valley IV (Wuling is already an existing mirror):

```text
Anchor AutoStockInStapleItem
  -> Item Name AutoStockInStapleItemName_Expected
  -> Discount AutoStockInStapleItemDiscountsValleyIV
  -> Click and Enter Quantity Control
```

### 1. Anchor: Remaining Refresh Time Frame

`AutoStockInStapleItem` uses `ColorMatch` to identify the remaining time area in the top-left corner of the item card (cyan connected domain, `order_by: Vertical`), serving as the basis `box` for all subsequent offset recognition.

### 2. Offset Recognition of Item Name

Offset sequentially based on the anchor:

1.  `AutoStockInStapleItemNameLabelColor`: Name label background color.
2.  `AutoStockInStapleItemNameTextColor`: Name text color.
3.  `AutoStockInStapleItemName`: OCR recognizes the item name, `expected` written during runtime initialization.
4.  `AutoStockInStapleItemName_Expected`: `And` combination of the above three, `box_index: 2` takes the box from the item name OCR.

Item names selected by the user are written into multilingual aliases via `AutoStockInStapleItemName.attach.{slug}`; at task start, `AttachToExpectedRegexAction` merges them into:

```text
^(alias1|alias2|...)$
```

Unselected items do not enter the whitelist; OCR will not match them.

### 3. Offset Recognition of Discount

`AutoStockInStapleItemDiscountsValleyIV` uses the box of `AutoStockInStapleItemName` as the basis, with `roi_offset` offset to the discount area. By default, OCR recognizes discount values like `95/90/85/...`.

The `AutoStockUseDiscountsValleyIV` option can rewrite this node:

- Select **Any Discount**: Changes the recognition type to `ColorMatch`; passes as long as the discount area has content.
- Select a specific discount tier: Rewrites the `expected` list to only allow discounts not lower than that tier (including handling of placeholders like `-99`).

### 4. Judgment of "Affordability"

Unlike the Credit Store, the stable supply list scan **does not** have a separate "unit price ColorMatch / CanAfford" node. Affordability is handled in two layers:

| Stage                 | Mechanism                                                                                                             |
| --------------------- | --------------------------------------------------------------------------------------------------------------------- |
| Before List Scan      | `AutoStockTargetCanNotBuyValleyIV`: Whether remaining dispatch tickets are still above the retention threshold        |
| Within Purchase Popup | `AutoStockStapleGoodsStockBillInsufficientValidate`: Recognizes the bottom red "Insufficient Dispatch Tickets" prompt |

Therefore, the `And` conditions for `AutoStockBuyItemValleyIVTask` are:

- `AutoStockInStapleItem`
- `AutoStockInStapleItemName_Expected`
- `AutoStockInStapleItemDiscountsValleyIV`

After all three match, click the item card (`target_offset: [-50, 95, 0, 0]`), and `next` enters `AutoStockStapleQuantityControl`.

> [!IMPORTANT] > `AutoStockBuyItemValleyIVTask` only means "a candidate item was recognized and entered purchase judgment," **it does not** mean the purchase is completed. Whether an order is actually placed depends on if the quantity control branch reaches `AutoStockStapleQuantityControlConfirmBuy`.

### 5. Sold-Out and Swiping

- `SoldOut`: OCR recognizes text like "Sold Out / 已售罄" on the left; once matched, it stops swiping.
- `AutoStockSwipeValleyIV`: Swipes down within the Valley IV page, `post_wait_freezes` waits for the list area to stabilize before entering the next round of recognition.

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

- `AutoStockStapleGoodsCountValidateColor`: Quantity area color anchor.
- `AutoStockStapleGoodsCountValidateText`: OCR reads `\d+`.

Each item's Buy/Exclude branch uses `ExpressionRecognition` to compare with the user-configured limit, for example:

```text
Buy:     {ValleyEngravingPermitLimit} > {AutoStockStapleGoodsCountValidate}
Exclude: {ValleyEngravingPermitLimit} <= {AutoStockStapleGoodsCountValidate}
```

### Branch 1: Insufficient Dispatch Tickets, Direct Exit

`AutoStockStapleQuantityControl{Item}StockBillInsufficient` combines:

- Current item OCR (e.g., `AutoStockStapleGoodsValleyEngravingPermit`)
- `AutoStockStapleGoodsStockBillInsufficientValidate` (bottom red area ColorMatch)

Upon match:

1.  `[JumpBack]` to `{Item}RemoveFilter`, **excludes** the item from `AutoStockInStapleItemName.attach` (`attach.{slug}: false`), and triggers `AutoStockStapleQuantityControlResetRecognitionParams` to regenerate the whitelist regex.
2.  Close the purchase popup (`AutoStockStapleQuantityControlCloseBuyWindow`).

This ensures subsequent list scans do not repeatedly click unaffordable items.

### Branch 2: Quantity Below Target, Execute Purchase

`AutoStockStapleQuantityControl{Item}Buy` matches when both item OCR + `Validate` expression conditions are met, executing the dedicated Custom action `AutoStockStapleQuantityControlAction` (`agent/go-service/autostockstaple/action.go`).

Action logic:

1.  Read the corresponding `AutoStockStapleGoods{Item}Validate` node expression, parsing the **target limit** and **quantity OCR node name**.
2.  Run quantity OCR on the current screenshot to get the **current held quantity**.
3.  Calculate `target = target_limit - current_held_quantity`.
4.  If `target <= 0`, skip sliding (disable `AutoStockStapleBetterSliding`).
5.  Otherwise, enable `AutoStockStapleBetterSliding` via `OverridePipeline` and write `target` into its `attach.Target`.

Go **no longer** executes `RunTask` to perform sliding; quantity adjustment is completed by the low-code branch:

```text
{Item}Buy (Go: calculate target + OverridePipeline)
  ├ AutoStockStapleCheckSliding              (Skip sliding when slider is hidden and default quantity is 1)
  ├ AutoStockStapleBetterSliding             (Execute BetterSliding after Go enables it)
  └ AutoStockStapleQuantityControlRelayConfirm (Fallback for target<=0, etc.)
  -> AutoStockStapleQuantityControlConfirmBuy
```

`AutoStockStapleBetterSliding` is defined in `General/Item.json`, with `enabled: false` by default; it is only enabled after Go override; the default value of `attach.Target` is just a placeholder.

After the purchase quantity adjustment is complete, `next` enters `AutoStockStapleQuantityControlConfirmBuy` to click the yellow confirm button, then closes the reward popup and returns to the list.

### Branch 3: Quantity Reached or Exceeded Target, Exclude and Re-initialize

`AutoStockStapleQuantityControl{Item}Exclude` matches when `ExcludeValidate` holds (current held quantity is **not less than** the user limit).

Flow:

1.  `{Item}RemoveFilter`: Calls `PipelineOverrideAction`, setting the item's attach key to `false`, effectively removing it from the whitelist.
2.  `AutoStockStapleQuantityControlResetRecognitionParams`: Executes `AttachToExpectedRegexAction` again, **regenerating** the `AutoStockInStapleItemName.expected` regex based on the latest attach state.
3.  Close the purchase popup, return to the list to continue scanning other items.

The Exclude branch **does not** purchase; it only removes "reached target" items from the current scan objectives.

## Summary of Initialization and Override Mechanism

This task has two types of runtime overrides; do not confuse them during maintenance:

| Action                                 | Trigger Location                                  | Purpose                                                                |
| -------------------------------------- | ------------------------------------------------- | ---------------------------------------------------------------------- |
| `AttachToExpectedRegexAction`          | `AutoStockStapleMain` entry; Exclude → Reset node | Merge attach keywords → OCR whitelist regex                            |
| `PipelineOverrideAction`               | Each item's `{Item}RemoveFilter`                  | Set specified attach key to `false`, excluding the item                |
| `AutoStockStapleQuantityControlAction` | Each item's `{Item}Buy`                           | Calculate difference and override BetterSliding's `Target` / `enabled` |

`attach` semantics (see `attachregex/action.go`):

- `string` / `string[]`: Add keyword to whitelist.
- `false`: Explicitly exclude this attach key, no longer participate in merging.
- `true`: Does not add keyword under current implementation.

## Adding a New Item

When adding a new stable demand supply item, the following typically need to be modified in sync:

1.  **`assets/resource/pipeline/AutoStockStaple/General/Goods.json`**: Add `AutoStockStapleGoods{Item}` OCR node and multilingual `expected`.
2.  **`assets/resource/pipeline/AutoStockStaple/General/GoodsCountValidate.json`**: Add `{Item}Validate` / `{Item}ExcludeValidate` expression nodes.
3.  **`assets/resource/pipeline/AutoStockStaple/General/QuantityControl.json`**: Append `{Item}` control node in `AutoStockStapleQuantityControl.next`, and complete sub-nodes like Buy / Exclude / StockBillInsufficient / RemoveFilter (refer to existing items in the same region for examples).
4.  **`assets/tasks/AutoStockStaple.json`**: Add a case in the corresponding region checkbox, writing `AutoStockInStapleItemName.attach.{slug}` and the quantity limit override.
5.  **`assets/locales/interface/*.json`**: Add `option.CreditShoppingItems.cases.{Item}.label` and focus text (e.g., `quantity_control.buy.*`).

During maintenance, directly edit the above Pipeline and task configuration; **do not** rely on code generators to overwrite outputs.

## Adding a New Region (Referencing Valley IV)

If a third region for stable supply purchase is added in the future, a set of nodes can be copied corresponding to Valley IV:

1.  Create `assets/resource/pipeline/AutoStockStaple/{Region}.json`:
    - `{Region}InStaple` scan loop (the four-branch `next` structure remains unchanged).
    - `AutoStockTargetCompare{Region}` / `AutoStockTargetCanNotBuy{Region}`.
    - `AutoStockBuyItem{Region}Task` (replace discount node names).
    - `AutoStockSwipe{Region}`.
2.  Add `[JumpBack]AutoStockStaple{Region}` sub-task and SceneManager jump in `Main.json`.
3.  Add scene OCR in `StockStaple.json` or the regional InScene file.
4.  Add `AutoStockInStapleItemDiscounts{Region}` in `Item.json` (if the UI layout differs from existing regions).
5.  Add region switch and option group in `assets/tasks/AutoStockStaple.json`.

The existing Wuling implementation is a mirror of Valley IV; you can directly diff `ValleyIV.json` and `Wuling.json` to see the differences.

## Debugging Suggestions

| Symptom                                                     | Priority Check                                                                                                                           |
| ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| Target item not recognized in list                          | `AttachToExpectedRegexAction`'s `expected` regex in `go-service.log`; whether anchor `AutoStockInStapleItem` matched                     |
| Item recognized but not purchased                           | Whether quantity control went to `Exclude` or `StockBillInsufficient`; `AutoStockStapleQuantityControl{Item}Buy/Exclude` in `maafw*.log` |
| Purchase quantity is incorrect                              | `threshold/current_count/target` in `AutoStockStapleQuantityControlAction` log; BetterSliding ROI                                        |
| Scanning stops even though dispatch tickets seem sufficient | `AutoStockTargetCompareValleyIV` expression and user input `{ReserveValleyIV}`                                                           |
| Repeatedly clicking the same reached-target item            | Whether `{Item}RemoveFilter` and `ResetRecognitionParams` executed after Exclude                                                         |

Log analysis can reference the skill: `.agents/skills/autostockstaple-log-analysis/SKILL.md`.

## Differences from AutoStockpile

| Aspect           | AutoStockStaple (Stable Demand)     | AutoStockpile (Flexible Stockpiling) |
| ---------------- | ----------------------------------- | ------------------------------------ |
| Decision Core    | Pipeline + minimal Go               | Go Service dominated                 |
| Item Location    | List time anchor + OCR offset chain | Template matching + OCR mapping      |
| Quantity Control | Popup BetterSliding + expressions   | Go parses detail page to adjust      |

Both have similar interfaces but independent logic; log analysis is in `.agents/skills/autostockstaple-log-analysis/SKILL.md`.
