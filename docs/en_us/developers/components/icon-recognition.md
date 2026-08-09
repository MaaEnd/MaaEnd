# IconRecognition

`IconRecognition` is a C++ Custom Recognition for locating and classifying item icons inside a known game screen and ROI. It does not navigate screens, click items, or control task flow.

Provide a Maa ROI in 1280x720 coordinates as `[x,y,width,height]`. Before capturing or recognizing a frame, move the cursor somewhere that does not cover the item grid, such as the top-left corner, and wait for the target region to settle.

## Pipeline

All three recognition modes use the `IconRecognition` registration and the same parameter object.

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

## Go Service

Go services call the `IconRecognition` registration through `ctx.RunRecognitionDirect`. Set the native ROI through `CustomRecognitionParam.ROI` and keep component-specific fields in `CustomRecognitionParam`:

```go
detail, err := ctx.RunRecognitionDirect(
    maa.RecognitionTypeCustom,
    &maa.CustomRecognitionParam{
        ROI:                maa.NewTargetRect(maa.Rect{154, 202, 983, 291}),
        CustomRecognition: "IconRecognition",
        CustomRecognitionParam: map[string]any{
            "grid_type":  "transfer",
            "item_ids":   []string{"item_copper_ore"},
            "deduplicate": true,
        },
    },
    img,
)
```

MaaFramework wraps Custom detail, so parse the component payload from `best.detail`. Go uses the same `grid_type`, candidate filters, thresholds, and deduplication semantics as Pipeline.

## C++ API

C++ callers can construct `IconRecognizer` directly without going through the Maa Custom Recognition registration. Point `data_root` at `assets/data/IconRecognition` and place request fields in `RecognitionRequest`:

```cpp
iconrecognition::IconRecognizer recognizer("assets/data/IconRecognition");
if (!recognizer.initialize()) {
    return;
}

iconrecognition::RecognitionRequest request;
request.grid_type = iconrecognition::GridType::Transfer;
request.roi = cv::Rect(154, 202, 983, 291);
request.candidates.item_ids = { "item_copper_ore" };
request.deduplicate = true;

// Optional warm-up before concurrent recognition.
if (!recognizer.preload({ request })) {
    return;
}

const iconrecognition::RecognitionResult result = recognizer.recognize(image, request);
```

`request.candidates.item_ids` and `item_filters` correspond to the same-named Pipeline and Go parameters. `RecognitionResult` uses the same field semantics as Custom detail but returns C++ structures instead of a JSON wrapper.

## Shared parameters and results

All three entry points share one recognition contract. Pipeline uses `recognition.param.roi`, Go uses `CustomRecognitionParam.ROI`, and C++ uses `RecognitionRequest.roi`. See the [parameter and data contract](/agent/cpp-algo/source/IconRecognition/docs/architecture.md) for all remaining parameters, defaults, `storageKind:categoryType` filters, threshold constraints, result fields, error codes, and debug output.

## References

- [Tests and manual review output](/agent/cpp-algo/source/IconRecognition/docs/testing.md)
- [Recognition algorithm](/agent/cpp-algo/source/IconRecognition/docs/algorithm.md)
- [Internal grid types and reference ROIs](/agent/cpp-algo/source/IconRecognition/docs/grid-profiles.md)
- [Resource download and publishing](/tools/icon_recognition/README.md)
