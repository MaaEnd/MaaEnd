# Development Manual - AutoStockpile Maintenance Documentation

This document explains how to maintain `AutoStockpile` during version updates.

## Concept Explanation

### Item ID

`item_map.json` stores not image paths, but **internal item IDs**, in a fixed format:

```text
{Region}/{BaseName}.Tier{N}
```

For example:

```text
ValleyIV/OriginiumSaplings.Tier3
Wuling/WulingFrozenPears.Tier1
```

Where:

1. `Region`: Region ID.
2. `BaseName`: English file name base. The corresponding name can be found in [EndFieldTranslationReferrer](https://susieglitter.github.io/EndFieldTranslationReferrer/).
3. `Tier{N}`: Price fluctuation range.

### Template Image Path

The Go code automatically constructs the template path based on the item ID:

```text
AutoStockpile/Goods/{Region}/{BaseName}.Tier{N}.png
```

The actual file location in the repository is:

```text
assets/resource/image/AutoStockpile/Goods/{Region}/{BaseName}.Tier{N}.png
```

### Region and Price Options

Regions and tiers currently used in the repository:

| Chinese Name | Region ID  |
| ------------ | ---------- |
| 四号谷地     | `ValleyIV` |
| 武陵         | `Wuling`   |

| Tier    | Price Fluctuation Range |
| ------- | ----------------------- |
| `Tier1` | Moderate                |
| `Tier2` | Large                   |
| `Tier3` | Extreme                 |

## Adding an Item

When adding a new item, two parts need to be maintained: the **item mapping** and the **template image**.

### 1. Modify `item_map.json`

File: `agent/go-service/autostockpile/item_map.json`

Add a new mapping from the item's Chinese name to the item ID under `zh_cn`:

```json
{
    "zh_cn": {
        "{Chinese Item Name}": "{Region}/{BaseName}.Tier{N}"
    }
}
```

### 2. Add Template Image

Save a screenshot of the item detail page to the corresponding directory:

```text
assets/resource/image/AutoStockpile/Goods/{Region}/{BaseName}.Tier{N}.png
```

> [!important]
> Template images must be based on a 1280×720 resolution.

## Adding a Region

Adding a new region requires synchronously modifying the following files:

### 1. Prepare Resources

- Create the `assets/resource/image/AutoStockpile/Goods/{NewRegion}/` directory and place the template images inside.
- Add the new region's item name to item ID mapping in `agent/go-service/autostockpile/item_map.json`.

### 2. Configure Task Options

File: `assets/tasks/AutoStockpile.json`

Add a new `AutoStockpileElastic{NewRegion}` switch option in the `option` list to control whether the corresponding region node in `Main.json` is enabled via `pipeline_override.enabled`. The template is as follows:

```json
"AutoStockpileElastic{NewRegion}": {
    "type": "switch",
    "label": "$task.AutoStockpile.option.AutoStockpileElastic{NewRegion}.label",
    "default_case": "Yes",
    "cases": [
        {
            "name": "Yes",
            "pipeline_override": {
                "AutoStockpileElastic{NewRegion}": {
                    "enabled": true
                }
            }
        },
        {
            "name": "No"
        }
    ]
}
```

Also, add `"AutoStockpileElastic{NewRegion}"` to the `task[0].option` array.

### 3. Confirm Scene Navigation Entry Exists

It is typically named `SceneEnterMenuRegionalDevelopment{Region}StockRedistribution`.

If it does not exist, you can wait for other maintainers to provide the corresponding node, or add it yourself. This will not be elaborated here.

### 4. Pipeline Nodes

#### Main.json

File: `assets/resource/pipeline/AutoStockpile/Main.json`

1. Add `"[JumpBack]AutoStockpileElastic{NewRegion}"` to the `next` list of `AutoStockpileStart`.
2. Add a new `AutoStockpileElastic{NewRegion}` node, following the structure of `AutoStockpileElasticValleyIV`:
    - Fill in the corresponding scene navigation sub-task name in `action.param.custom_action_param.sub`.
    - Set `anchor` to `"AutoStockpileDecision": "AutoStockpileDecision{NewRegion}"`.
    - Fill in the region's Chinese name in `focus`.

#### DecisionLoop.json

File: `assets/resource/pipeline/AutoStockpile/DecisionLoop.json`

Add a new `AutoStockpileDecision{NewRegion}` node, and write `"Region": "{NewRegion}"` in `action.param.custom_action_param`.

### 5. Go Logic

File: `agent/go-service/autostockpile/strategy.go`

Add the new region and its base value to `regionBases`:

```go
var regionBases = map[string]int{
    "ValleyIV":   regionBaseValleyIV,
    "Wuling":     regionBaseWuling,
    "{NewRegion}": regionBase{NewRegion},  // New
}
```

Confirm that the shared `tierBases` covers the tiers required by the new region.

### 6. Internationalization

Add the following fields in all language versions of `assets/locales/interface/*.json`:

| Field Key                                                         | Purpose                    |
| ----------------------------------------------------------------- | -------------------------- |
| `global.region.{NewRegion}`                                       | Region global display name |
| `task.AutoStockpile.option.AutoStockpileElastic{NewRegion}.label` | Task option label          |

Language files to update: `zh_cn.json`, `en_us.json`, `ja_jp.json`, `ko_kr.json`, `zh_tw.json`.

### 7. Update Price Record Schema

File: `docs/zh_cn/protocol/autostockpile-daily-storage/daily_storage.schema.json` (and the corresponding `en_us` version)

Add the new region identifier (e.g., `"NewRegion"`) to the `enum` list of the `region` field to ensure third-party tools can validate data for the new region via the Schema.
