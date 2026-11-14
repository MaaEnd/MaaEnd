# Development Manual - AutoStockpile Maintenance Documentation

This document explains how to maintain `AutoStockpile` during version updates.  
This document was updated on November 14, 2026.

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

Recognition scans the elastic goods shelf **page by page**: at most 3 frames (the entry frame plus one frame after each of two swipes down), independent of how many goods were recognized.

1. OCR + template match on the first-screen screenshot.
2. Run `AutoStockpileSwipeShelfDown` to scroll one step, then screencap and compare with the previous screen; if the pixels are identical, the shelf is already at the bottom (or the current controller does not scroll at all, e.g. Win32) and scanning stops.
3. If the pixels differ, scan that screen and then compare its goods ID set with the previous screen: an identical set means the swipe did not advance the shelf (the ValleyIV view has micro-animations, so the pixel comparison alone keeps reporting "changed"), and scanning also stops.
4. If the shelf really advanced, keep that screen and repeat step 2. One scroll step is far smaller than the recognition ROI, so every row appears fully in at least one screen and no row is skipped.
5. Merge by goods **ID** (on duplicates keep the first-screen entry; IDs seen only on page N (0-based) go into `SwipeCountToReveal`, whose value is the number of swipes down needed).
6. Restore the shelf: run `AutoStockpileSwipeShelfUp` as many times as the **effective swipes down** of this scan, using the same distance in the opposite direction, so the list returns to the scan origin.

Page scanning always performs two swipes down (`shelfScanSwipeTimes`): the entry frame plus one frame per swipe. Scanning stops early when the shelf stops advancing, logged as `shelf_swipe_skip` (`reason` is `frame_unchanged` or `goods_unchanged`), so Win32 (full list on one screen) only recognizes one frame. If the frames are not enough, raise `shelfScanSwipeTimes` and confirm that `AutoStockpileSwipeShelfDown` on ADB still scrolls clearly less than the recognition ROI height.

Restoration relies on the swipe down and the swipe up covering the same distance: both are a 200 px slow drag with `duration: 700`, so dragging back the same number of times returns to the origin, which is what makes `SwipeCountToReveal` ("swipe down N times from the origin") valid. Do not restore by clicking the market entry: it is a fixed sidebar entry, and clicking the already-selected entry does not re-render the list at the top. Recognizing it is also unreliable because the "unselected" template never matches the selected icon.

If a swipe, a screencap or a page scan fails, keep the pages already scanned without aborting; a failed swipe up only logs a warning and stops the remaining restoration, and the merged results are still used.

After selection:

- If the chosen ID is in `SwipeCountToReveal` (not visible on the first screen), `AutoStockpileSwipeShelfDown` runs that many times before the click, and `AutoStockpileSelectedGoodsClick` then performs the template click.
- If the item was already on the first screen (page0), click without an extra swipe.

Swipe nodes:

- Win32 default: `AutoStockpileSwipeShelfDown` / `AutoStockpileSwipeShelfUp` are `DoNothing` in `assets/resource/pipeline/AutoStockpile/Helper.json` (full list fits one screen), so the first frame comparison of page scanning reports "not scrolling" and stops, and no restoration drag is triggered.
- ADB / PlayCover: real `Swipe` overrides in `assets/resource_adb/pipeline/AutoStockpile/Helper.json`: both the swipe down and the swipe up are a 200 px slow drag with `duration: 700` and `end_hold: 200` (the swipe down is the scan step, the swipe up is used only for restoration). Coordinates are 720p; if real devices miss items, first tune the ADB-side `begin` / `end`, `duration` and `post_wait_freezes.target`; if the restoration does not reach the origin, first confirm both nodes still use the same drag distance.
