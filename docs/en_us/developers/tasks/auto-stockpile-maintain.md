# Development Manual - AutoStockpile Maintenance Documentation

This document explains how to maintain `AutoStockpile` during version updates.  
This document was updated on November 12, 2026.

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

| Chinese Name | Region ID |
| ------------ | ---------- |
| 四号谷地 | `ValleyIV` |
| 武陵 | `Wuling` |

| Tier | Price Fluctuation Range |
| ------- | ----------------------- |
| `Tier1` | Moderate |
| `Tier2` | Large |
| `Tier3` | Extreme |

Price thresholds are produced only by the "region base + tier base + weekday adjustment" formula in `strategy.go`; there is no longer a JSON configuration entry, so adjusting the thresholds requires changing that formula.

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

Crop the item's picture from the elastic-demand goods list and save it to the corresponding directory:

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

Add a new `AutoStockpileDecision{NewRegion}` node, and write `"Region": "{NewRegion}"` in `action.param.custom_action_param`; if the new region's cash icon on the detail page differs from the existing ones, also add the new template to the `template` of `AutoStockpileEnsureCashAnchor`.

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

| Field Key | Purpose |
| ----------------------------------------------------------------- | -------------------------- |
| `global.region.{NewRegion}` | Region global display name |
| `task.AutoStockpile.option.AutoStockpileElastic{NewRegion}.label` | Task option label |

Language files to update: `zh_cn.json`, `en_us.json`, `ja_jp.json`, `ko_kr.json`, `zh_tw.json`.

### 7. Update Price Record Schema

File: `docs/zh_cn/protocol/autostockpile-daily-storage/daily_storage.schema.json` (and the corresponding `en_us` version)

Add the new region identifier (e.g., `"NewRegion"`) to the `enum` list of the `region` field to ensure third-party tools can validate data for the new region via the Schema.

## Page-by-page Shelf Scan

Recognition scans the elastic goods shelf **page by page** until the shelf stops moving, so the number of pages does not matter:

1. OCR + template match on the first-screen screenshot, and record the vertical position of the `市场` (Market) title as the scan-start anchor.
2. Run `AutoStockpileSwipeShelfDown` to scroll one step, then screencap and compare with the previous screen; if nothing changed, the shelf is already at the bottom (or the current controller does not scroll at all, e.g. Win32) and scanning stops.
3. If the frame changed, scan that screen and repeat step 2. One scroll step is far smaller than the recognition ROI, so every row appears fully in at least one screen and no row is skipped.
4. Merge by goods **ID** (on duplicates keep the first-screen entry; IDs seen only on page N (0-based) go into `SwipeCountToReveal`, whose value is the number of swipes down needed).
5. Restore the pre-scan position: run `AutoStockpileSwipeShelfUp` for each actual swipe down, then use `AutoStockpileFindMarketTitle` to read the vertical position of the `市场` (Market) title and compare it with the position recorded at scan start; if the difference exceeds 40 px, fine-tune with a slow drag until the shelf is back.

Page scanning performs at most 10 swipes down (`shelfMaxScrollSteps`), which is only a safety net against endless loops; normal termination is "the frame stopped changing". A longer shelf needs no change to this logic, only a check that `AutoStockpileSwipeShelfDown` on ADB scrolls clearly less than the recognition ROI height.

Fast swipes carry momentum, so the gesture distance is not proportional to the actual scroll amount; simply swiping up as many times as the scan swiped down overshoots the market top and lands in the `当前拥有` (Currently Owned) section above. The ADB swipe-up gesture is therefore about half the swipe-down distance, and the title anchor closes the remaining gap. The scan start position is also the baseline for the pre-click swipes (it is not necessarily the market top).

If a swipe, a screencap or a page scan fails, keep the pages already scanned without aborting; a failed restore only logs a warning, and the merged results are still used.

After selection:

- If the chosen ID is in `SwipeCountToReveal` (not visible on the first screen), `AutoStockpileSwipeShelfDown` runs that many times before the click, and `AutoStockpileSelectedGoodsClick` then performs the template click.
- If the item was already on the first screen (page0), click without an extra swipe.

Swipe nodes:

- Win32 default: `AutoStockpileSwipeShelfDown` / `AutoStockpileSwipeShelfUp` are `DoNothing` in `assets/resource/pipeline/AutoStockpile/Helper.json` (full list fits one screen), so the first frame comparison of page scanning reports "not scrolling" and stops; `AutoStockpileFindMarketTitle` still uses OCR.
- ADB / PlayCover: real `Swipe` overrides in `assets/resource_adb/pipeline/AutoStockpile/Helper.json` (with horizontal end segment and `end_hold`). The swipe-up distance is about half the swipe-down distance so the restore does not overshoot the market top. Coordinates are 720p; if real devices miss items or show too many duplicates, first tune the ADB-side `begin` / `end` and `post_wait_freezes.target`.
