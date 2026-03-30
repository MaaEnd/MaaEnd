# Development Guide - AutoStockpile Maintenance Document

This document explains how to maintain item templates, item mappings, task options (global switches / regional thresholds / reserve stock bill), and region expansion for `AutoStockpile`.

The current implementation consists of two cooperating parts:

- `assets/resource/pipeline/AutoStockpile/`: Responsible for entering the screen, switching regions, executing the purchase flow, and maintaining default parameters for recognition nodes in `Helper.json`.
- `agent/go-service/autostockpile/`: Responsible for runtime overrides of recognition-node parameters, parsing recognition results, and deciding which items to purchase.

## Overview

The core maintenance points of AutoStockpile are as follows:

| Module                        | Path                                                       | Purpose                                                                                              |
| ----------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| Item name mapping             | `agent/go-service/autostockpile/item_map.json`             | Maps OCR item names to internal item IDs                                                             |
| Item template images          | `assets/resource/image/AutoStockpile/Goods/`               | Template images for matching on the item details page                                                |
| Task options                  | `assets/tasks/AutoStockpile.json`                          | User-configurable global switches, region toggles, price thresholds, and reserve stock bill settings |
| Region entry Pipeline         | `assets/resource/pipeline/AutoStockpile/Main.json`         | Defines entry subtasks and anchor mappings for each region                                           |
| Stockpile entry Pipeline      | `assets/resource/pipeline/AutoStockpile/Entry.json`        | Enters the elastic goods interface and scrolls to the bottom                                         |
| Decision loop Pipeline        | `assets/resource/pipeline/AutoStockpile/DecisionLoop.json` | Executes core flows: recognition, decision, reconciliation, skip                                     |
| Purchase flow Pipeline        | `assets/resource/pipeline/AutoStockpile/Purchase.json`     | Executes purchase quantity adjustment, purchase, cancel operations                                   |
| Recognition node defaults     | `assets/resource/pipeline/AutoStockpile/Helper.json`       | Default parameters for overflow detection, goods OCR, template matching, etc.                        |
| Go recognition/decision logic | `agent/go-service/autostockpile/`                          | Applies runtime recognition overrides, parses results, and applies thresholds                        |
| Multilingual copy             | `assets/locales/interface/*.json`                          | UI text for AutoStockpile tasks and options                                                          |

## Naming Conventions

### Item ID

`item_map.json` stores **internal item IDs**, not image paths. The format is always:

```text
{Region}/{BaseName}.Tier{N}
```

Example:

```text
ValleyIV/OriginiumSaplings.Tier3
Wuling/WulingFrozenPears.Tier1
```

Where:

1. `Region`: Region ID.
2. `BaseName`: English filename stem.
3. `Tier{N}`: Value tier (variation range).

### Template Image Path

Go code automatically builds the template path from the item ID:

```text
AutoStockpile/Goods/{Region}/{BaseName}.Tier{N}.png
```

The actual file location in the repository is:

```text
assets/resource/image/AutoStockpile/Goods/{Region}/{BaseName}.Tier{N}.png
```

### Region and Tier Coverage

Current regions and tiers supported in the repository:

| Region    | Region ID  | Included Tiers            |
| --------- | ---------- | ------------------------- |
| Valley IV | `ValleyIV` | `Tier1`, `Tier2`, `Tier3` |
| Wuling    | `Wuling`   | `Tier1`, `Tier2`          |

> [!NOTE]
>
> `agent/go-service/autostockpile` calls `InitItemMap("zh_cn")` during registration. Initialization failure only logs a warning and does not block service startup. However, if `item_map` is still unavailable when later parsing item names or validating regions, those operations will fail. The `item_map.json` file is embedded in the binary.

### Current Task Options and Attach Keys

In the current `assets/tasks/AutoStockpile.json`, task options write to the `attach` field of the `AutoStockpileDecisionAttach` node via `pipeline_override`, which the Go service reads uniformly. The keys actually written are:

| Purpose                      | Task option / input                           | Attach key                        |
| ---------------------------- | --------------------------------------------- | --------------------------------- |
| Relax threshold on overflow  | `AutoStockpileOverflowBuyLowPriceGoods`       | `overflow_mode=true`              |
| Relax threshold on Sundays   | `AutoStockpileBuyAllGoodsOnSunday`            | `sunday_mode=true`                |
| Valley IV price thresholds   | `ValleyIVTier1PriceLimit` / `Tier2` / `Tier3` | `price_limits_ValleyIV.Tier1/2/3` |
| Valley IV reserve stock bill | `ValleyIVReserveStockBillAmount`              | `reserve_stock_bill_ValleyIV`     |
| Wuling price thresholds      | `WulingTier1PriceLimit` / `Tier2`             | `price_limits_Wuling.Tier1/2`     |
| Wuling reserve stock bill    | `WulingReserveStockBillAmount`                | `reserve_stock_bill_Wuling`       |

The `AutoStockpileValleyIV` and `AutoStockpileWuling` region switches themselves do not write to `attach`. Instead, they control whether the corresponding region nodes in `Main.json` are enabled via `pipeline_override.enabled`.

## Threshold Resolution Mechanism

The system determines the purchase threshold using the following priority:

1. **Explicit Region Tier Threshold**: Reads the value configured in task options for `price_limits_{Region}.Tier{N}`.
2. **Minimum Positive Region Threshold**: If no specific tier threshold is set, it uses the minimum positive value among all configured prices for that region.
3. **fallback_threshold from attach**: If present and a positive integer, this value is used. Current task options do not expose this configuration, but the Go parsing logic supports reading it from attach.
4. **Global Default**: If neither of the above is available, it falls back to `defaultFallbackBuyThreshold` (800).

The default per-tier threshold table (e.g., 800 for `ValleyIVTier1`) is maintained in `agent/go-service/autostockpile/thresholds.go`, not `options.go`.

> [!TIP]
>
> Threshold-related task inputs and attach values must be **positive integers**. Empty strings, `0`, and negative values do not trigger fallback; they are rejected by task input validation or by the Go-side config parser.

## Reserve Stock Bill

AutoStockpile supports reserving a specific amount of stock bills (scheduling coupons).

- **Input Unit**: The value entered in the UI is in units of 10k (e.g., entering 60 represents 600,000).
- **Parsing Logic**: Go code parses the `reserve_stock_bill_{Region}` option and multiplies the value by 10,000 to get the actual reserve amount. If the result exceeds `math.MaxInt/10000`, an error is returned.
- **Prerequisite**: The reserve stock bill feature only takes effect when the corresponding region's reserve switch is enabled. Once enabled, the current stock bill balance must be OCR-readable. If OCR fails, or if the balance is less than or equal to the reserve amount, the recognition phase ends early and the purchase flow is skipped.
- **Purchase Limit**: If the current stock bill balance, after subtracting the reserve amount, is insufficient for the target item, the purchase quantity will be limited or the item will be skipped.

## Runtime Override Behavior

The Go Service dynamically overrides Pipeline node parameters at runtime:

- **AutoStockpileLocateGoods**: Overrides the `template` list and `roi`.
- **AutoStockpileGetGoods**: Overrides the recognition `roi`.
- **AutoStockpileSelectedGoodsClick**: Overrides `template`, the `y` coordinate of the ROI, and the `enabled` state.
- **AutoStockpileRelayNodeDecisionReady**: Overrides the `enabled` state.
- **AutoStockpileSwipeSpecificQuantity**: Overrides the `Target` value and `enabled` state.
- **AutoStockpileSwipeMax**: Overrides the `enabled` state.

When the decision finds no qualifying items or needs to skip, Go resets the purchase-branch nodes (`AutoStockpileRelayNodeDecisionReady`, `AutoStockpileSelectedGoodsClick`, `AutoStockpileSwipeSpecificQuantity`, and `AutoStockpileSwipeMax`) by setting them all to `enabled: false`, then redirects the flow to the skip branch via `OverrideNext`.

## Adding Items

Adding a new item requires updating both the **item mapping** and the **template image**.

### 1. Update `item_map.json`

File: `agent/go-service/autostockpile/item_map.json`

Add a new mapping from the Chinese item name to the item ID under `zh_cn`:

```json
{
    "zh_cn": {
        "{ChineseItemName}": "{Region}/{BaseName}.Tier{N}"
    }
}
```

Notes:

- Do **not** include the `AutoStockpile/Goods/` prefix or the `.png` suffix in the value.
- The Chinese item name should match the OCR result as closely as possible.

### 2. Add Template Image

Save the item details page screenshot to the corresponding directory:

```text
assets/resource/image/AutoStockpile/Goods/{Region}/{BaseName}.Tier{N}.png
```

Notes:

- The filename must exactly match the item ID in `item_map.json`.
- The baseline resolution is **1280x720**.
- `BaseName` should not contain extra `.` characters to avoid parsing errors.

### 3. Pipeline Changes

**Usually, adding a normal new item does not require Pipeline changes.**

The recognition flow first attempts to bind prices using OCR item names. Only items that remain unbound in the current region are then supplemented by template matching using the path built via `BuildTemplatePath()`. Since Go overrides templates and ROIs at runtime, simply providing `item_map.json` and the template image is sufficient.

## Adding Value Tiers

If you are just adding a new tier for an existing item (e.g., adding `Tier3` for a product), follow the "Adding Items" steps:

- Add the `{BaseName}.Tier{N}` mapping in `item_map.json`.
- Add the corresponding template image in `assets/resource/image/AutoStockpile/Goods/{Region}/`.

To support a new general tier in the task configuration (e.g., adding `Tier3` inputs for `Wuling`), also maintain the following:

1. **Task Options**: Add the `price_limits_{Region}.Tier{N}` input and `pipeline_override.attach` key in `assets/tasks/AutoStockpile.json`.
2. **Default Thresholds**: Update `autoStockpileDefaultPriceLimits` in `agent/go-service/autostockpile/thresholds.go`.
3. **Localization**: Add labels and descriptions for the new tier in `assets/locales/interface/*.json`.

If no specific threshold is configured for a new tier, it will fall back following the "minimum positive region threshold -> `fallback_threshold` (if present) -> 800" order. The task will continue, but purchase decisions might not be ideal.

## Adding Regions

Adding a new region involves several steps across the project:

### 1. Resources

- Create the `assets/resource/image/AutoStockpile/Goods/{NewRegion}/` directory and add templates.
- Add item mappings in `agent/go-service/autostockpile/item_map.json`.

### 2. Task Configuration

File: `assets/tasks/AutoStockpile.json`

- Add an `AutoStockpile{NewRegion}` toggle.
- Add the matching price input fields and `price_limits_{NewRegion}.Tier{N}` `pipeline_override.attach` keys.
- If reserve support is needed, add the corresponding switch / input fields and the `reserve_stock_bill_{NewRegion}` attach key.

### 3. Pipeline Nodes

Files: `assets/resource/pipeline/AutoStockpile/Main.json` and `assets/resource/pipeline/AutoStockpile/DecisionLoop.json`

- Add `[JumpBack]AutoStockpile{NewRegion}` to the `next` list of `AutoStockpileMain` in `Main.json`.
- Define the corresponding region node in `Main.json` (e.g., `AutoStockpileValleyIV`), setting the `anchor` field to point `AutoStockpileDecision` to the decision node in `DecisionLoop.json` (e.g., `AutoStockpileDecisionValleyIV`).
- Add a matching `AutoStockpileDecision{NewRegion}` node in `DecisionLoop.json`, and set `{NewRegion}` in `action.param.custom_action_param.Region`.

Note: The Pipeline still maintains the hardcoded region-to-decision anchor mapping via the `anchor` field in `Main.json`.

### 4. Go Logic

File: `agent/go-service/autostockpile/params.go`

- Go now reads the region directly from `custom_action_param.Region` on the `AutoStockpileDecision{Region}` node and validates that the region exists in `item_map.json`.
- `normalizeCustomActionParam()` supports receiving parameters in either map or JSON string format.
- **Note**: There is no fallback here. Missing, empty, or unknown `Region` values will cause the recognition/task flow to fail immediately.

### 5. Default Values

File: `agent/go-service/autostockpile/thresholds.go`

- Add default prices for each tier of the new region in `autoStockpileDefaultPriceLimits`.

### 6. Internationalization

- Add labels and descriptions for all new options in `assets/locales/interface/`.

## Self-Checklist

Ensure the following after any changes:

1. Values in `item_map.json` use the `{Region}/{BaseName}.Tier{N}` format and match image filenames.
2. Template images are placed in `assets/resource/image/AutoStockpile/Goods/{Region}/`.
3. Key names in `assets/tasks/AutoStockpile.json` follow the `price_limits_{Region}.Tier{N}` format. If reserve stock bill is enabled, `reserve_stock_bill_{Region}` is also present.
4. When adding a tier, `thresholds.go` and `locales/*.json` are updated.
5. When adding a region, `Main.json`, `DecisionLoop.json` (especially `AutoStockpileDecision{Region}.action.param.custom_action_param.Region`), `assets/tasks/AutoStockpile.json`, `item_map.json`, `thresholds.go`, and `locales/*.json` are all updated.

## Common Pitfalls

- **Missing `item_map.json`**: Adding images without mapping prevents OCR names from being linked to item IDs, leading to incomplete recognition.
- **Missing Images**: Adding mappings without templates prevents clicking the items.
- **Missing `custom_action_param.Region` on `AutoStockpileDecision{Region}`**: Adding a region without setting the decision node's region causes the recognition/task flow to fail immediately.
- **Missing Thresholds**: New tiers without configured thresholds will use fallback values, which may not match expectations.
- **Missing `reserve_stock_bill_{Region}`**: The region will work for purchasing, but the "Reserve Stock Bill" feature won't be available in task options.
- **Extra Dots in Filenames**: Using extra `.` characters in filenames interferes with parsing the item name and tier.
