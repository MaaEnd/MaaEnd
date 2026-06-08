# Development Manual - Auto-Stock Staple Maintenance Documentation

This document explains the overall structure of `AutoStockStaple`, how task options override Pipeline behavior, and the core logic for item identification and quantity control, facilitating future maintenance and extension.

This document uses **Valley IV** as the main example for introduction. Wuling's Pipeline structure is completely identical to Valley IV's, differing only in node name suffixes, scene recognition, and discount node names; when adding new regions, you can refer to the Valley IV implementation.

## File Overview

| Module                          | Path                                                                       | Purpose                                                                                                      |
| ------------------------------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| Project Interface Mount         | `assets/interface.json`                                                    | Mount `tasks/AutoStockStaple.json` to the task group                                                         |
| Task and Option Definition      | `assets/tasks/AutoStockStaple.json`                                        | Define task entry, region switches, item checkboxes, quantity limits, discount strategy, `pipeline_override` |
| Task Entry                      | `assets/resource/pipeline/AutoStockStaple/Main.json`                       | Scheduling cycle, main entry initialization, Valley IV/Wuling sub-task entries                               |
| Region Scan Loop                | `assets/resource/pipeline/AutoStockStaple/ValleyIV.json`                   | Valley IV staple item list scan, purchase click, swipe                                                       |
| Region Scan Loop                | `assets/resource/pipeline/AutoStockStaple/Wuling.json`                     | Wuling staple item list scan (structure symmetric with Valley IV)                                            |
| Item List Recognition           | `assets/resource/pipeline/AutoStockStaple/General/Item.json`               | Anchor, item name, discount, BetterSliding, confirm purchase, etc.                                           |
| Purchase Popup Item Recognition | `assets/resource/pipeline/AutoStockStaple/General/Goods.json`              | OCR recognition of items in the purchase popup                                                               |
| Owned Quantity Recognition      | `assets/resource/pipeline/AutoStockStaple/General/GoodsCountValidate.json` | Popup top-right current owned quantity OCR + each item Buy/Exclude expression validation                     |
| Quantity Control                | `assets/resource/pipeline/AutoStockStaple/General/QuantityControl.json`    | Branch dispatch after purchase popup opens, exclude items, confirm purchase                                  |
| General Template                | `assets/resource/pipeline/AutoStockStaple/General/Template.json`           | Sold out, dispatch ticket OCR, confirm purchase text, etc.                                                   |
| Scene Recognition               | `assets/resource/pipeline/Interface/InScene/StockStaple.json`              | `InValleyIVText`, `InWulingText`, `InStapleColor`                                                            |
| Go Quantity Control Action      | `agent/go-service/autostockstaple/action.go`                               | Calculate required purchase quantity and override BetterSliding `Target`                                     |
| Go Regex Initialization         | `agent/go-service/common/attachregex/action.go`                            | `AttachToExpectedRegexAction`: Merge attach keywords into an OCR whitelist regex                             |
| Multilingual Text               | `assets/locales/interface/*.json`                                          | Task names, options, and focus text                                                                          |

> [!NOTE]
> Under the ADB controller, some ROI offsets are located in `assets/resource_adb/pipeline/AutoStockStaple/`. When maintaining Win32 and ADB, both need to be checked simultaneously.

## Overall Execution Logic

The task entry is `AutoStockStapleMain` in `Main.json`:

1. **Initialize Regex Whitelist**: Execute `AttachToExpectedRegexAction`, read all `attach` keywords from the `AutoStockInStapleItemName` node (from user-selected item options), merge them, and override the `expected` regex for that node.
2. **Execute by Region Sub-tasks**: Sequentially attempt `[JumpBack]AutoStockStapleValleyIV`, `[JumpBack]AutoStockStapleWuling`; unenabled region nodes default to `enabled: false`.
3. **Enter Staple Item Interface**: Sub-tasks use SceneManager to jump to the corresponding region's item dispatch interface, then enter the `AutoStockInStapleValleyIV` / `AutoStockInStapleWuling` scan loop.
4. **After All Complete**, hit `AutoStockStapleDone` to finish.

### How Task Options Write to Pipeline

Options in `assets/tasks/AutoStockStaple.json` directly modify node fields through `pipeline_override`, typically including:

| Option Type                       | Override Target Example                         | Purpose                                                                  |
| --------------------------------- | ----------------------------------------------- | ------------------------------------------------------------------------ |
| Region Switch                     | `AutoStockStapleValleyIV.enabled`               | Whether to execute Valley IV purchase                                    |
| Dispatch Ticket Reserve Threshold | `AutoStockTargetCompareValleyIV`'s `expression` | Stop purchasing when remaining dispatch tickets fall below threshold     |
| Selected Purchase Items           | `AutoStockInStapleItemName.attach.{slug}`       | Write item names in various languages to attach for initialization merge |
| Item Holding Limit                | `AutoStockStapleGoods{Item}Validate`, etc.      | Override `{Limit} > {AutoStockStapleGoodsCountValidate}`                 |
| Discount Strategy                 | `AutoStockInStapleItemDiscountsValleyIV`        | Override discount OCR `expected` or change to ColorMatch                 |

The initialization action **does not** directly read user-input strings, but relies on content already written to the target node's `attach` via interface; the Go side then converts it into an OCR regex.

## Valley IV List Scan Loop

After entering the Valley IV staple item interface, `AutoStockInStapleValleyIV` sequentially determines within the same heartbeat according to `next` order:

```text
AutoStockTargetCanNotBuyValleyIV
  -> [JumpBack]AutoStockBuyItemValleyIVTask
  -> SoldOut
  -> [JumpBack]AutoStockSwipeValleyIV
```

| Order | Node                               | Meaning                                                                                         |
| ----- | ---------------------------------- | ----------------------------------------------------------------------------------------------- |
| 1     | `AutoStockTargetCanNotBuyValleyIV` | Whether current remaining dispatch tickets are **lower than** user-configured reserve threshold |
| 2     | `AutoStockBuyItemValleyIVTask`     | Whether a **purchasable target item** is recognized                                             |
| 3     | `SoldOut`                          | Whether a **sold out** sign is seen; after hit, the task stops scanning further in this region  |
| 4     | `AutoStockSwipeValleyIV`           | Swipe down the list to continue searching for items (`max_hit: 25`)                             |

The recognition condition for `AutoStockInStapleValleyIV` is: `InValleyIVText` + `InStapleColor` + `InStockStaple`, ensuring we are currently on the Valley IV staple item page.

### Dispatch Ticket Threshold Judgment

`AutoStockTargetCanNotBuyValleyIV` combines `InStapleColor` and `AutoStockTargetCompareValleyIV`.

`AutoStockTargetCompareValleyIV` uses `ExpressionRecognition`:

```text
{ReserveValleyIV} > {AutoStockCurrentStockBill}
```

- `AutoStockCurrentStockBill`: Top-right dispatch ticket OCR (`CurrentStockBillColor` + `CurrentStockBillText`).
- `{ReserveValleyIV}`: Reserve threshold entered by user in `AutoStockReserveValleyIV`, default `240000`.
- If the expression holds, it means "remaining dispatch tickets are **lower than** the reserve threshold, purchasing should stop", thus hitting `AutoStockTargetCanNotBuyValleyIV` to end region scanning; if not, purchasing can continue.

## Item Identification Chain (Whether Items Are Available for Purchase)

The implementation approach for `AutoStockBuyItemValleyIVTask` is similar to the [Credit Shop](./credit-shopping-maintain.md) item scan: **first find the anchor, then recognize subsequent fields based on anchor offset**. The staple item list page uses the **remaining refresh time frame** in the item's top-left corner as the anchor, instead of the `CreditIcon` from the credit shop.

Recommended image reading sequence:

```text
Anchor AutoStockInStapleItem
  -> Item Name AutoStockInStapleItemName_Expected
  -> Discount AutoStockInStapleItemDiscountsValleyIV
  -> Click and enter quantity control
```

### 1. Anchor: Remaining Refresh Time Frame

`AutoStockInStapleItem` uses `ColorMatch` to recognize the remaining time area in the top-left corner of the item card (cyan-green connected component, `order_by: Vertical`), serving as the base box for all subsequent offset recognition.

### 2. Offset Recognition of Item Name

Based on the anchor, offset sequentially:

1. `AutoStockInStapleItemNameLabelColor`: Name label background color.
2. `AutoStockInStapleItemNameTextColor`: Name text color.
3. `AutoStockInStapleItemName`: OCR recognition of item name, `expected` written by runtime initialization.
4. `AutoStockInStapleItemName_Expected`: `And` combines the above three, `box_index: 2` takes the box from item name OCR.

Item names selected by the user are written to various language aliases via `AutoStockInStapleItemName.attach.{slug}`; at task start, `AttachToExpectedRegexAction` merges them into:

```text
^(alias1|alias2|...)$
```

Unselected items will not enter the whitelist, and OCR will not hit them.

### 3. Offset Recognition of Discount

`AutoStockInStapleItemDiscountsValleyIV` uses the box from `AutoStockInStapleItemName` as the base, `roi_offset` shifts to the discount area, by default using OCR to recognize discount values like `95/90/85/...`.

The `AutoStockUseDiscountsValleyIV` option can override this node:

- Select **Any Discount**: Change recognition type to `ColorMatch`, pass as long as the discount area has content.
- Select specific discount tier: Override the `expected` list, only allow discounts not lower than that tier (including handling placeholders like `-99`).

### 4. "Can Afford" Judgment

Unlike the credit shop, the staple item list scan **does not** have a separate "unit price ColorMatch / CanAfford" node. Whether you can afford it is handled in two layers:

| Stage                 | Mechanism                                                                                                            |
| --------------------- | -------------------------------------------------------------------------------------------------------------------- |
| Before List Scan      | `AutoStockTargetCanNotBuyValleyIV`: Whether remaining dispatch tickets are still above the reserve threshold         |
| Inside Purchase Popup | `AutoStockStapleGoodsStockBillInsufficientValidate`: Recognize the bottom red "Insufficient dispatch tickets" prompt |

Therefore, the `And` conditions for `AutoStockBuyItemValleyIVTask` are:

- `AutoStockInStapleItem`
- `AutoStockInStapleItemName_Expected`
- `AutoStockInStapleItemDiscountsValleyIV`

When all three hit simultaneously, click the item card (`target_offset: [-50, 95, 0, 0]`), `next` enters `AutoStockStapleQuantityControl`.

> [!IMPORTANT]
> `AutoStockBuyItemValleyIVTask` only means "a candidate item was recognized and purchase determination entered", **it does not equal** a completed purchase. Whether an order is actually placed depends on whether the quantity control branch reaches `AutoStockStapleQuantityControlConfirmBuy`.

### 5. Sold Out and Swipe

- `SoldOut`: OCR recognizes text like "Sold Out" on the left side; after hit, no further swiping occurs.
- `AutoStockSwipeValleyIV`: Swipe down within the Valley IV page, `post_wait_freezes` waits for the list area to stabilize before entering the next recognition round.

## Quantity Control (Purchase Popup)

After clicking an item, the purchase popup opens. `AutoStockStapleQuantityControl` uses title OCR ("Purchase") to confirm the popup is open, then sequentially attempts each item's `AutoStockStapleQuantityControl{Item}` node according to the `next` list.

Using `AutoStockStapleQuantityControlValleyEngravingPermit` (Valley Engraving Permit) as an example, its `next` order is fixed as:

```text
AutoStockStapleQuantityControlValleyEngravingPermitStockBillInsufficient
  -> AutoStockStapleQuantityControlValleyEngravingPermitBuy
  -> AutoStockStapleQuantityControlValleyEngravingPermitExclude
```

### Reading Current Owned Quantity

The top-right owned quantity in the popup is recognized by `AutoStockStapleGoodsCountValidate`:

- `AutoStockStapleGoodsCountValidateColor`: Quantity area color anchor.
- `AutoStockStapleGoodsCountValidateText`: OCR reads `\d+`.

Each item's Buy / Exclude branch uses `ExpressionRecognition` to compare with user-configured limits, for example:

```text
Buy:     {ValleyEngravingPermitLimit} > {AutoStockStapleGoodsCountValidate}
Exclude: {ValleyEngravingPermitLimit} <= {AutoStockStapleGoodsCountValidate}
```

### Branch 1: Insufficient Dispatch Tickets, Exit Directly

`AutoStockStapleQuantityControl{Item}StockBillInsufficient` combines:

- Current item OCR (e.g., `AutoStockStapleGoodsValleyEngravingPermit`)
- `AutoStockStapleGoodsStockBillInsufficientValidate` (bottom red area ColorMatch)

After hit:

1. `[JumpBack]` to `{Item}RemoveFilter`, **exclude** the item from `AutoStockInStapleItemName.attach` (`attach.{slug}: false`), and trigger `AutoStockStapleQuantityControlResetRecognitionParams` to regenerate the whitelist regex.
2. Close the purchase popup (`AutoStockStapleQuantityControlCloseBuyWindow`).

This way, subsequent list scans will not repeatedly click items that cannot be afforded.

### Branch 2: Quantity Below Target, Execute Purchase

`AutoStockStapleQuantityControl{Item}Buy` hits when item OCR + `Validate` expression are both satisfied, executing the dedicated Custom action `AutoStockStapleQuantityControlAction` (`agent/go-service/autostockstaple/action.go`).

Action logic:

1. Read the corresponding `AutoStockStapleGoods{Item}Validate` node expression, parse the **target limit** and **quantity OCR node name**.
2. Run quantity OCR on the current screenshot to get the **current owned quantity**.
3. Calculate `target = target limit - current owned quantity`.
4. If `target <= 0`, skip swiping (disable `AutoStockStapleBetterSliding`).
5. Otherwise, use `OverridePipeline` to enable `AutoStockStapleBetterSliding` and write `target` to its `attach.Target`.

Go **no longer** `RunTask`s to execute sliding; quantity adjustment is handled by the low-code sibling branches:

```text
{Item}Buy (Go: compute target + OverridePipeline)
  ├ AutoStockStapleCheckSliding              (skip sliding when slider hidden and default qty is 1)
  ├ AutoStockStapleBetterSliding             (BetterSliding when enabled by Go)
  └ AutoStockStapleQuantityControlRelayConfirm (fallback when target<=0, etc.)
  → AutoStockStapleQuantityControlConfirmBuy
```

`AutoStockStapleBetterSliding` is defined in `General/Item.json`, default `enabled: false`, only enabled after Go override; the default `attach.Target` is a placeholder.

After purchase quantity adjustment is complete, `next` enters `AutoStockStapleQuantityControlConfirmBuy` to click the yellow confirm button, then close the reward popup to return to the list.

### Branch 3: Quantity At or Above Target, Exclude and Reinitialize

`AutoStockStapleQuantityControl{Item}Exclude` hits when `ExcludeValidate` is satisfied (current owned quantity **not lower than** user limit).

Process:

1. `{Item}RemoveFilter`: Calls `PipelineOverrideAction`, sets the item's attach key to `false`, effectively removing it from the whitelist.
2. `AutoStockStapleQuantityControlResetRecognitionParams`: Executes `AttachToExpectedRegexAction` again, **regenerating** `AutoStockInStapleItemName.expected` regex based on the latest attach state.
3. Close the purchase popup, return to the list to continue scanning other items.

The Exclude branch **does not** purchase; it only removes "target reached" items from this round's scan targets.

## Initialization and Override Mechanism Summary

This task has two types of runtime overrides; do not confuse them during maintenance:

| Action                                 | Trigger Location                                     | Purpose                                                  |
| -------------------------------------- | ---------------------------------------------------- | -------------------------------------------------------- |
| `AttachToExpectedRegexAction`          | `AutoStockStapleMain` entry; Exclude post-reset node | Merge attach keywords → OCR whitelist regex              |
| `PipelineOverrideAction`               | Each item `{Item}RemoveFilter`                       | Set specified attach key to `false`, exclude the item    |
| `AutoStockStapleQuantityControlAction` | Each item `{Item}Buy`                                | Calculate difference and override BetterSliding `Target` |

`attach` semantics (see `attachregex/action.go`):

- `string` / `string[]`: Add keywords to whitelist.
- `false`: Explicitly exclude this attach key, no longer participates in merging.
- `true`: Current implementation does not append keywords.

## Adding New Items

When adding a new staple demand item, typically the following need to be modified simultaneously:

1. **`assets/resource/pipeline/AutoStockStaple/General/Goods.json`**: Add `AutoStockStapleGoods{Item}` OCR node and multilingual `expected` strings.
2. **`assets/resource/pipeline/AutoStockStaple/General/GoodsCountValidate.json`**: Add `{Item}Validate` / `{Item}ExcludeValidate` expression nodes.
3. **`assets/resource/pipeline/AutoStockStaple/General/QuantityControl.json`**: Append `{Item}` control node to `AutoStockStapleQuantityControl.next`, and add Buy / Exclude / StockBillInsufficient / RemoveFilter subnodes (copy an existing item in the same region as reference).
4. **`assets/tasks/AutoStockStaple.json`**: Add a case in the corresponding region checkbox, writing `AutoStockInStapleItemName.attach.{slug}` and quantity limit override.
5. **`assets/locales/interface/*.json`**: Add `option.CreditShoppingItems.cases.{Item}.label` and focus text (e.g., `quantity_control.buy.*`).

Edit the Pipeline and task config directly. **Do not** rely on a code generator to overwrite these artifacts.

## Adding New Regions (Referencing Valley IV)

If a third region's staple item purchase is added in the future, you can copy a set of nodes by referencing Valley IV:

1. Create `assets/resource/pipeline/AutoStockStaple/{Region}.json`:
    - `{Region}InStaple` scan loop (`next` four-branch structure unchanged).
    - `AutoStockTargetCompare{Region}` / `AutoStockTargetCanNotBuy{Region}`.
    - `AutoStockBuyItem{Region}Task` (replace discount node name).
    - `AutoStockSwipe{Region}`.
2. Add `[JumpBack]AutoStockStaple{Region}` sub-task and SceneManager jump in `Main.json`.
3. Add scene OCR in `StockStaple.json` or region InScene file.
4. Add `AutoStockInStapleItemDiscounts{Region}` in `Item.json` (if UI layout differs from existing regions).
5. Add region switch and option group in `assets/tasks/AutoStockStaple.json`.

Wuling's existing implementation is a mirror of Valley IV; you can directly diff `ValleyIV.json` and `Wuling.json` to see the differences.

## Debugging Suggestions

| Symptom                                    | Priority Check                                                                                                                           |
| ------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------- |
| Target item not recognized in list         | `expected` regex in `AttachToExpectedRegexAction` in `go-service.log`; whether anchor `AutoStockInStapleItem` hits                       |
| Item recognized but not purchased          | Whether quantity control went to `Exclude` or `StockBillInsufficient`; `AutoStockStapleQuantityControl{Item}Buy/Exclude` in `maafw*.log` |
| Incorrect purchase quantity                | `threshold/current_count/target` in `AutoStockStapleQuantityControlAction` logs; BetterSliding ROI                                       |
| Stops scanning despite sufficient tickets  | `AutoStockTargetCompareValleyIV` expression and user-input `{ReserveValleyIV}`                                                           |
| Repeatedly clicks same target-reached item | Whether `{Item}RemoveFilter` and `ResetRecognitionParams` executed after Exclude                                                         |

Log analysis can refer to the skill: `.claude/skills/autostockstaple-log-analysis/SKILL.md`.

## Difference from AutoStockpile

| Item             | AutoStockStaple (Staple Demand Items)              | AutoStockpile (Flexible Demand Item Stockpiling)           |
| ---------------- | -------------------------------------------------- | ---------------------------------------------------------- |
| Decision Maker   | Pipeline + few Go Custom                           | Go Service dominates recognition and decision              |
| Item Location    | List page remaining time anchor + OCR offset chain | Template matching + OCR mapping                            |
| Quantity Control | In-popup BetterSliding + expression validation     | Go side parses detail page and adjusts quantity            |
| Maintenance Doc  | This document                                      | [auto-stockpile-maintain.md](./auto-stockpile-maintain.md) |

Both enter the "Item Dispatch" interface, but purchase logic is completely independent; do not mix log analysis procedures when troubleshooting.
