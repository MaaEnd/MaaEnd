# Development Guide - AutoStockpile Maintenance Document

This document explains how to maintain item templates, item mappings, task options (region toggles), and region expansion for `AutoStockpile`.

The current implementation consists of two cooperating parts:

- `assets/resource/pipeline/AutoStockpile/`: Responsible for entering the screen, switching regions, executing the purchase flow, and maintaining default parameters for recognition nodes in `Helper.json`.
- `agent/go-service/autostockpile/`: Responsible for runtime overrides of recognition-node parameters, parsing recognition results, and deciding which items to purchase.

## Overview

The core maintenance points of AutoStockpile are as follows:

| Module                        | Path                                                       | Purpose                                                                       |
| ----------------------------- | ---------------------------------------------------------- | ----------------------------------------------------------------------------- |
| Item name mapping             | `agent/go-service/autostockpile/item_map.json`             | Maps OCR item names to internal item IDs                                      |
| Item template images          | `assets/resource/image/AutoStockpile/Goods/`               | Template images for matching on the item details page                         |
| Task options                  | `assets/tasks/AutoStockpile.json`                          | User-configurable region toggles (Valley IV / Wuling)                         |
| Region entry Pipeline         | `assets/resource/pipeline/AutoStockpile/Main.json`         | Defines entry subtasks and anchor mappings for each region                    |
| Stockpile entry Pipeline      | `assets/resource/pipeline/AutoStockpile/Entry.json`        | Enters Stock Redistribution (elastic goods) and scrolls to the bottom         |
| Decision loop Pipeline        | `assets/resource/pipeline/AutoStockpile/DecisionLoop.json` | Executes core flows: recognition, decision, reconciliation, skip              |
| Purchase flow Pipeline        | `assets/resource/pipeline/AutoStockpile/Purchase.json`     | Executes purchase quantity adjustment, purchase, cancel operations            |
| Recognition node defaults     | `assets/resource/pipeline/AutoStockpile/Helper.json`       | Default parameters for overflow detection, goods OCR, template matching, etc. |
| Go recognition/decision logic | `agent/go-service/autostockpile/`                          | Applies runtime recognition overrides, parses results, and applies thresholds |
| Multilingual copy             | `assets/locales/interface/*.json`                          | UI text for AutoStockpile tasks and options                                   |

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

### Current Task Options

The current `assets/tasks/AutoStockpile.json` exposes one server-time selector and two region toggles:

| Task option               | Purpose                                                                                    |
| ------------------------- | ------------------------------------------------------------------------------------------ |
| `AutoStockpileServerTime` | Selects the server timezone by writing an integer UTC hour offset to `AutoStockpileAttach` |
| `AutoStockpileValleyIV`   | Enables the Valley IV region node via `pipeline_override.enabled`                          |
| `AutoStockpileWuling`     | Enables the Wuling region node via `pipeline_override.enabled`                             |

The region toggles do not write to `attach`. `AutoStockpileServerTime` writes `server_time` to `AutoStockpileAttach.attach` through `pipeline_override`, and the Go Service reads it at runtime. The current built-in behaviors are:

- **Overflow threshold bypass**: `selector.go` enables threshold bypass automatically only when recognition reports overflow (`Quota.Overflow > 0`); there is no user-facing or attach-based switch.
- **Price thresholds**: `buildSelectionConfig()` in `strategy.go` computes per-region defaults from the `region_base + tier_base + weekday_adjustment` formula. The default server timezone is `UTC+8`, and the server-day boundary is `04:00`. `AutoStockpileServerTime` can override the weekday calculation by writing an integer UTC hour offset to `AutoStockpileAttach.attach.server_time`. If unset, the runtime still falls back to `UTC+8`.
- **Reserve stock bill**: Not implemented as a runtime decision input. The recognition payload only carries quota and goods data, and the downstream decision flow does not consume any reserve-stock-bill state.

If you need different pricing behavior, update the Go defaults in code rather than expanding manual `attach` overrides. The current AutoStockpile flow only reads an attach-based override for `server_time`, which affects weekday calculation only; it still does not read attach-based price-limit, overflow-handling, or reserve-stock-bill settings.

## Threshold Resolution Mechanism

The system currently uses **strict region-tier key lookups** to determine the purchase threshold:

1. **Region-tier defaults generated in `strategy.go`**: `buildPriceLimitsForRegion()` computes per-tier thresholds from the `region_base + tier_base + weekday_adjustment` formula.
2. **Strict `price_limits` resolution in `thresholds.go`**: `resolveTierThreshold()` uses `GoodsItem.Tier` as the lookup key directly. Missing keys, empty tiers, or invalid thresholds all return errors and are handled upstream as fatal failures.

When `weekday_adjustment = 0` (that is, Tuesday), example generated values include `ValleyIV.Tier1=600`, `ValleyIV.Tier2=900`, `ValleyIV.Tier3=1200`, `Wuling.Tier1=1200`, and `Wuling.Tier2=1500`. These values are not fixed defaults for every server day.

The weekday adjustment table is:

| Weekday   | Adjustment |
| --------- | ---------- |
| Monday    | `-50`      |
| Tuesday   | `0`        |
| Wednesday | `-150`     |
| Thursday  | `-200`     |
| Friday    | `-250`     |
| Saturday  | `-200`     |
| Sunday    | `-50`      |

For server-day calculation, AutoStockpile first converts the current time to the target timezone, then treats `04:00 ~ next 03:59` as the same server day. The default production path uses `UTC+8`, while `AutoStockpileServerTime` can override it with `UTC+8`, `UTC+9`, `UTC-8`, or `UTC+1`.

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

To support a new general tier for a region, also maintain the following:

1. **Default Thresholds**: Add the new tier base to `tierBases` in `agent/go-service/autostockpile/strategy.go`.

If a new tier is missing from `tierBases`, `buildPriceLimitsForRegion()` will not generate the corresponding key. Once that tier is recognized, `resolveTierThreshold()` will fail because the exact `{Region}.Tier{N}` key is missing, and the task will stop with a fatal error.

## Adding Regions

Adding a new region involves several steps across the project:

### 1. Resources

- Create the `assets/resource/image/AutoStockpile/Goods/{NewRegion}/` directory and add templates.
- Add item mappings in `agent/go-service/autostockpile/item_map.json`.

### 2. Task Configuration

File: `assets/tasks/AutoStockpile.json`

- Add an `AutoStockpile{NewRegion}` toggle that enables the corresponding region node in `Main.json` via `pipeline_override.enabled`.

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

File: `agent/go-service/autostockpile/strategy.go`

- Add the new region to `regionBases`.
- Ensure the shared `tierBases` table already covers every tier that region should use.

### 6. Internationalization

- Add labels and descriptions for all new options in `assets/locales/interface/`.

## Self-Checklist

Ensure the following after any changes:

1. Values in `item_map.json` use the `{Region}/{BaseName}.Tier{N}` format and match image filenames.
2. Template images are placed in `assets/resource/image/AutoStockpile/Goods/{Region}/`.
3. When adding a tier, `tierBases` in `strategy.go` is updated with the new tier's base value.
4. When adding a region, `Main.json`, `DecisionLoop.json` (especially `AutoStockpileDecision{Region}.action.param.custom_action_param.Region`), `assets/tasks/AutoStockpile.json`, `item_map.json`, `strategy.go`, and `locales/*.json` are all updated.

## Common Pitfalls

- **Missing `item_map.json`**: Adding images without mapping prevents OCR names from being linked to item IDs, leading to incomplete recognition.
- **Missing Images**: Adding mappings without templates prevents clicking the items.
- **Missing `custom_action_param.Region` on `AutoStockpileDecision{Region}`**: Adding a region without setting the decision node's region causes the recognition/task flow to fail immediately.
- **Missing Default Threshold Inputs**: If `strategy.go` (`tierBases` / `regionBases`) does not generate the exact `{Region}.Tier{N}` key for a recognized tier, strict threshold lookup will fail and the task will stop as a fatal error.
- **Extra Dots in Filenames**: Using extra `.` characters in filenames interferes with parsing the item name and tier.
