# IconRecognition

`IconRecognition` is a C++ Custom Recognition for locating and classifying item icons inside a known game screen and ROI. It does not navigate screens, click items, or control task flow.

Provide a Maa ROI in 1280x720 coordinates as `[x,y,width,height]`. Before capturing or recognizing a frame, move the cursor somewhere that does not cover the item grid, such as the top-left corner, and wait for the target region to settle.

## Usage

All calls use the `IconRecognition` registration and the same parameter object.

### Find an item by ID

Use a top-level key from `assets/data/IconRecognition/recognition_items.json` as the item ID:

```json
{
    "IconRecognitionFindItem": {
        "recognition": {
            "type": "Custom",
            "param": {
                "custom_recognition": "IconRecognition",
                "custom_recognition_param": {
                    "grid_type": "transfer",
                    "roi": [155, 205, 970, 280],
                    "item_ids": ["item_copper_ore"],
                    "deduplicate": true
                }
            }
        },
        "action": "DoNothing"
    }
}
```

All accepted cell positions are returned by default. With `deduplicate: true`, only the highest-scoring cell is kept for each `item_id`; different items retain separate results.

### Recognize every item in a grid

Omit `item_ids`. The component locates the selected screen's internal grid and recognizes each cell:

For `transfer` (inventory and storage) and `port_storager` (portable storage), the ROI may cover both sides or just one side. A full ROI recognizes both grids, while a one-sided ROI recognizes only that side; workflows commonly pass each side separately.

```json
{
    "ScanTransferItems": {
        "recognition": {
            "type": "Custom",
            "param": {
                "custom_recognition": "IconRecognition",
                "custom_recognition_param": {
                    "grid_type": "transfer",
                    "roi": [155, 205, 970, 280],
                    "item_filters": ["Normal:*"]
                }
            }
        },
        "action": "DoNothing"
    }
}
```

### Recognize one square ROI

`single_roi` skips real grid detection and constructs one temporary cell from the ROI. Width and height must be equal; any positive side length is accepted:

```json
{
    "RecognizeCurrentTradeItem": {
        "recognition": {
            "type": "Custom",
            "param": {
                "custom_recognition": "IconRecognition",
                "custom_recognition_param": {
                    "grid_type": "single_roi",
                    "roi": [1177, 450, 54, 54],
                    "item_filters": ["Normal:Product", "Normal:Usable"]
                }
            }
        },
        "action": "DoNothing"
    }
}
```

54x54 is only an example for this screen. The component resizes its built-in 128px or 256px icons to the requested ROI size; callers do not provide icon assets.

## Parameters and results

Only `grid_type` and `roi` are required. Optional parameters are `item_ids`, `item_filters`, `threshold`, `subpixel_threshold`, `deduplicate`, and `debug`.

See the [interface contract](/agent/cpp-algo/source/IconRecognition/docs/architecture.md) for filter categories, per-grid defaults, threshold constraints, item ID lookup, and the complete result schema.

On a hit, `out_box` equals the primary match's `cell_box`. Component detail contains `detail_version`, `matched`, `grid_type`, `roi`, and `matches`. Each match contains the item ID, localization key, categories, rarity, cell and item boxes, and score. Real-grid matches also contain `row` and `column`. Failures include `error.code` and `error.message`.

## Go Service

Go services call the `IconRecognition` registration through `ctx.RunRecognitionDirect`, using `maa.RecognitionTypeCustom` and `maa.CustomRecognitionParam`. MaaFramework wraps Custom detail, so parse the component payload from `best.detail`. The Chinese guide includes a complete Go example.

## References

- [Tests and manual review output](/agent/cpp-algo/source/IconRecognition/docs/testing.md)
- [Recognition algorithm](/agent/cpp-algo/source/IconRecognition/docs/algorithm.md)
- [Internal grid types and reference ROIs](/agent/cpp-algo/source/IconRecognition/docs/grid-profiles.md)
- [Resource download and publishing](/tools/icon_recognition/README.md)
