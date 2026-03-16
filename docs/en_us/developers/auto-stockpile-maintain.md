# Development Guide - AutoStockpile Maintenance Document

This document explains how to maintain item templates, item mappings, price thresholds, and region expansion for `AutoStockpile`.

The current implementation consists of two parts:

- Pipeline is responsible for entering the screen, switching regions, and executing the purchase flow.
- `agent/go-service/autostockpile/` is responsible for item recognition, config loading, and deciding what to buy.

## Overview

The core maintenance points of AutoStockpile are as follows:

| Module                        | Path                                               | Purpose                                                   |
| ----------------------------- | -------------------------------------------------- | --------------------------------------------------------- |
| Item name mapping             | `agent/go-service/autostockpile/item_map.json`     | Maps OCR item names to internal item IDs                  |
| Item template images          | `assets/resource/image/AutoStockpile/Goods/`       | Template images for matching on the item details page     |
| Region and price options      | `assets/tasks/AutoStockpile.json`                  | User-configurable region toggles and price thresholds     |
| Region entry Pipeline         | `assets/resource/pipeline/AutoStockpile/Main.json` | Defines entry subtasks for each region                    |
| Main stockpiling Pipeline     | `assets/resource/pipeline/AutoStockpile/Task.json` | Runs recognition, clicking, purchasing, and related flow  |
| Go recognition/decision logic | `agent/go-service/autostockpile/`                  | Loads templates, recognizes items, and applies thresholds |
| Multilingual copy             | `assets/misc/locales/*.json`                       | UI text for AutoStockpile tasks and options               |

## Naming conventions

### Item ID

`item_map.json` stores **internal item IDs**, not image paths. The format is always:

```text
{Region}/{BaseName}.Tier{N}
```

For example:

```text
ValleyIV/OriginiumSaplings.Tier3
Wuling/WulingFrozenPears.Tier1
```

Where:

1. `Region`: region ID.
2. `BaseName`: English filename stem.
3. `Tier{N}`: value tier.

### Template image path

Go code automatically builds the template path from the item ID:

```text
AutoStockpile/Goods/{Region}/{BaseName}.Tier{N}.png
```

The actual file location in the repository is:

```text
assets/resource/image/AutoStockpile/Goods/{Region}/{BaseName}.Tier{N}.png
```

### Region ID

The region IDs currently used in the repository are:

The Chinese strings below are intentionally kept as literal `zh_cn` names used by the current project.

| zh_cn Name | Region     |
| ---------- | ---------- |
| 四号谷地   | `ValleyIV` |
| 武陵       | `Wuling`   |

### Value tier ID

The tiers currently used in task configuration are:

The UI text below is intentionally kept as the literal `zh_cn` text shown in the current task configuration.

| zh_cn UI Text | Tier ID |
| ------------- | ------- |
| 适中          | `Tier1` |
| 较大          | `Tier2` |
| 极大          | `Tier3` |

> [!NOTE]
>
> `agent/go-service/autostockpile` initializes `InitItemMap("zh_cn")` during registration, so the `zh_cn` mapping must always be maintained. You cannot add only other languages.

## Adding items

When adding a new item, you must maintain at least **item mapping** and **template images**.

### 1. Edit `item_map.json`

File: `agent/go-service/autostockpile/item_map.json`

Add a new mapping from the Chinese item name to the item ID under `zh_cn`.
The example key below intentionally keeps the original `zh_cn` item name, because that is the real mapping target used by OCR:

```json
{
    "zh_cn": {
        "{ChineseItemName}": "{Region}/{BaseName}.Tier{N}"
    }
}
```

Example:

```json
{
    "zh_cn": {
        "源石树幼苗货组": "ValleyIV/OriginiumSaplings.Tier3"
    }
}
```

Notes:

- Do **not** include the `AutoStockpile/Goods/` prefix in the value.
- Do **not** include the `.png` suffix in the value.
- The Chinese item name should match the OCR-stable name as closely as possible.

### 2. Add the template image

Save the item detail page screenshot to the corresponding directory:

```text
assets/resource/image/AutoStockpile/Goods/{Region}/{BaseName}.Tier{N}.png
```

Example:

```text
assets/resource/image/AutoStockpile/Goods/ValleyIV/OriginiumSaplings.Tier3.png
```

Notes:

- The image filename must exactly match the item ID in `item_map.json`.
- The baseline resolution is still **1280×720**.
- `BaseName` in the filename should not contain extra `.` characters, otherwise the Go code that parses `BaseName.TierN.png` may be affected.

### 3. Do you need to modify the Pipeline?

**Usually, adding a normal new item does not require Pipeline changes.**

Why:

- Go automatically scans templates under `assets/resource/image/AutoStockpile/Goods/{Region}/`.
- When selecting an item, it dynamically overrides template paths for nodes such as `AutoStockpileSelectedGoodsClick` based on the item ID.

That means as long as the item mapping and template image are correct, the existing flow can recognize and click the new item.

## Adding a value tier

If you are only adding a new tier for an existing item (for example, a new `Tier3` for that item), you usually maintain it the same way as “adding an item”:

- Add the corresponding `{BaseName}.Tier{N}` mapping in `item_map.json`.
- Add the corresponding template image under `assets/resource/image/AutoStockpile/Goods/{Region}/`.

If you want the **entire task UI** to support a new common tier (for example, adding `Tier4`), you also need to maintain the following.

### 1. Add task input fields

File: `assets/tasks/AutoStockpile.json`

For the corresponding region's price input section, add:

- The input field definition;
- The key in `pipeline_override.attach`;
- The key name format must be: `price_limits_{Region}.Tier{N}`.

For example:

```json
"price_limits_ValleyIV.Tier4": "{ValleyIVTier4PriceLimit}"
```

### 2. Add the default price

File: `agent/go-service/autostockpile/options.go`

Add the corresponding default value to `autoStockpileDefaultPriceLimits`, otherwise an empty input string cannot fall back to the default threshold.

### 3. Add multilingual copy

File: `assets/misc/locales/zh_cn.json` and other language files.

At minimum, add:

- The label for the region's price configuration item;
- The label for the new tier input field.

### 4. Behavior when no dedicated threshold is configured

In `selector.go`, `resolveTierThreshold()` first checks `cfg.PriceLimits[tierID]`.

If the new tier does not have a dedicated threshold, it falls back to `FallbackThreshold`. The current region's `FallbackThreshold` is the smallest positive value among the configured prices for that region. In other words:

- **The flow can still run, but the behavior may not be reasonable;**
- If you want the new tier to be purchased as expected, it is best to explicitly add `price_limits_{Region}.Tier{N}`.

## Adding a region

Adding a new region is not just creating one more directory. You need to wire up **region recognition, region entry, price configuration, item templates, and localized copy** together.

### 1. Add the item template directory and item mappings

Create the directory first:

```text
assets/resource/image/AutoStockpile/Goods/{NewRegion}/
```

Then:

- Put each template image for that region's items into it;
- Add the corresponding item mappings to `agent/go-service/autostockpile/item_map.json`.

### 2. Add the task entry and price configuration

File: `assets/tasks/AutoStockpile.json`

You need to add a complete region configuration group, usually including:

- `AutoStockpile{NewRegion}` region toggle;
- `AutoStockpile{NewRegion}PriceLimits` price input group;
- The corresponding `pipeline_override`;
- One `price_limits_{NewRegion}.Tier{N}` key for each tier.

### 3. Add the region Pipeline nodes

File: `assets/resource/pipeline/AutoStockpile/Main.json`

You need to:

- Add the new region subtask to the `sub` list of `AutoStockpileMain`;
- Add a new region node;
- Configure the anchor for that region node.

Illustration:

```json
"AutoStockpileNewRegion": {
    "enabled": false,
    "anchor": {
        "AutoStockpileGotoTargetRegion": "GoToNewRegion"
    },
    "next": ["AutoStockpileTask"]
}
```

### 4. Update the Go region resolution logic

File: `agent/go-service/autostockpile/recognition.go`

`resolveGoodsRegion()` currently recognizes only:

- `GoToValleyIV` -> `ValleyIV`
- `GoToWuling` -> `Wuling`

When adding a new region, you must add the corresponding anchor branch here. Otherwise it will fall back to `Wuling`.

### 5. Add default prices

File: `agent/go-service/autostockpile/options.go`

Add default `price_limits_{NewRegion}.Tier{N}` values for each tier of the new region.

### 6. Add multilingual copy

File: `assets/misc/locales/*.json`

At minimum, add:

- The region toggle name;
- The region price configuration name;
- The UI text for each tier input field.

## Self-checklist

After making changes, check at least the following:

1. Whether values in `item_map.json` use the format `{Region}/{BaseName}.Tier{N}` and match the image filenames.
2. Whether template images are placed under `assets/resource/image/AutoStockpile/Goods/{Region}/`.
3. Whether keys in `assets/tasks/AutoStockpile.json` use the format `price_limits_{Region}.Tier{N}`.
4. When adding a new region, whether `Main.json`, `recognition.go`, `options.go`, and `assets/misc/locales/*.json` are updated together.

## Common pitfalls

- **Adding images without `item_map.json`**: the OCR name cannot be mapped to an item ID, so recognition results are incomplete.
- **Adding `item_map.json` without images**: the name can be matched, but template clicking cannot be completed.
- **Adding a new region without updating `resolveGoodsRegion()`**: at runtime, it will incorrectly fall back to `Wuling`.
- **Adding a new tier without configuring thresholds**: the flow may continue, but the purchase threshold falls back to the fallback logic and may not match expectations.
- **Using extra `.` characters in filenames**: this affects parsing of the item name and `Tier`.
