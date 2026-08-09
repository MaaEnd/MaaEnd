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
                    "item_ids": ["item_copper_ore"],
                    "deduplicate": true
                },
                "roi": [154, 202, 983, 291]
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

**Performance note: recognition time grows with the number of candidate templates. Prefer `item_filters` to keep the item set as small as the workflow allows, and use `item_ids` when the exact targets are known; avoid unnecessary all-item candidate sets on a single screen.**

```json
{
    "ScanTransferItems": {
        "recognition": {
            "type": "Custom",
            "param": {
                "custom_recognition": "IconRecognition",
                "custom_recognition_param": {
                    "grid_type": "transfer",
                    "item_filters": ["Normal:*"]
                },
                "roi": [154, 202, 983, 291]
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
                    "item_filters": ["Normal:Product", "Normal:Usable"]
                },
                "roi": [1177, 450, 54, 54]
            }
        },
        "action": "DoNothing"
    }
}
```

54x54 is only an example for this screen. The component resizes its built-in 128px or 256px icons to the requested ROI size; callers do not provide icon assets.

## Parameters and results

Put the native `roi` in `recognition.param` alongside `custom_recognition`, using Maa `[x,y,width,height]` coordinates at the 1280x720 baseline. `custom_recognition_param` must contain `grid_type`.

| Parameter | Required | Default | Description |
| --- | --- | --- | --- |
| `grid_type` | Yes | None | Current screen: `trade`, `transfer`, `port_storager`, `valuables`, `shipment`, `credit_trade`, or the temporary `single_roi` mode |
| `item_ids` | No | Empty | Restricts candidates to top-level catalog item IDs; duplicates are rejected; intersects with `item_filters` when both are present |
| `item_filters` | No | Per `grid_type` | Uses `storage:category`; multiple filters form a union, and `*` selects every category in that storage |
| `threshold` | No | `0.85` | Final acceptance threshold; lowering it increases false-positive risk |
| `subpixel_threshold` | No | `0.60` | Runs subpixel refinement when the base score reaches this value but remains below `threshold`; lower scores are rejected |
| `deduplicate` | No | `false` | Keeps only the highest-scoring cell for each `item_id` when enabled |
| `debug` | No | `false` | Saves raw, annotated, and detail files under `exe_dir/../debug/vision/IconRecognition`, retaining the latest 20 groups |

See the [filter category tables in the interface contract](/agent/cpp-algo/source/IconRecognition/docs/architecture.md#item_filters-分类) for every storage, category value, business meaning, and per-`grid_type` default.

The base template score uses masked `TM_CCOEFF_NORMED`; the final `score` combines 85% template score with 15% Lab color score. Thresholds must satisfy `0 <= subpixel_threshold < threshold <= 1`. When recognition is unstable, verify the ROI, settled screen state, and candidate filters before tuning thresholds. See the [interface contract](/agent/cpp-algo/source/IconRecognition/docs/architecture.md) for error codes and the complete result schema.

On a hit, `out_box` equals the primary match's `cell_box`. Component detail contains `detail_version`, `matched`, `grid_type`, `roi`, and `matches`. Each match contains the item ID, localization key, categories, rarity, cell and item boxes, and score. Real-grid matches also contain `row` and `column`. Failures include `error.code` and `error.message`.

## Go Service

Go services call the `IconRecognition` registration through `ctx.RunRecognitionDirect`, using `maa.RecognitionTypeCustom` and `maa.CustomRecognitionParam`. Set the native ROI through `CustomRecognitionParam.ROI`; keep component-specific fields in `CustomRecognitionParam`. MaaFramework wraps Custom detail, so parse the component payload from `best.detail`. The Chinese guide includes a complete Go example.

## References

- [Tests and manual review output](/agent/cpp-algo/source/IconRecognition/docs/testing.md)
- [Recognition algorithm](/agent/cpp-algo/source/IconRecognition/docs/algorithm.md)
- [Internal grid types and reference ROIs](/agent/cpp-algo/source/IconRecognition/docs/grid-profiles.md)
- [Resource download and publishing](/tools/icon_recognition/README.md)
