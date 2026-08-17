# IconRecognition

`IconRecognition` is a C++ Custom Recognition for locating and classifying item icons inside a known game screen and ROI. It does not navigate screens, click items, or control task flow.

Provide a Maa ROI in 1280x720 coordinates as `[x,y,width,height]`. Before capturing or recognizing a frame, move the cursor somewhere that does not cover the item grid, such as the top-left corner, and wait for the target region to settle.

## Pipeline

Use the `Custom` recognition type with the fixed registration name `IconRecognition`. Put the native `roi` in `recognition.param.roi` and component fields in `recognition.param.custom_recognition_param`; do not put `roi` inside the component object.

### Find an item by ID

Use a top-level key from [`assets/data/IconRecognition/recognition_items.json`](/assets/data/IconRecognition/recognition_items.json) as the item ID:

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
                    "item_filters": ["Normal:Ore"],
                    "item_recheck_filters": ["Normal:Ore"],
                    "deduplicate": true
                },
                "roi": [
                    154,
                    202,
                    983,
                    291
                ]
            }
        },
        "action": "DoNothing"
    }
}
```

All accepted cell positions are returned by default. With `deduplicate: true`, only the highest-scoring cell is kept for each `item_id`; different items retain separate results.

### Recognize every item in a grid

Omit `item_ids`. The component locates the selected screen's item grid inside the ROI and recognizes each cell. Recognition time grows with the number of candidate templates, so prefer `item_filters` to keep the candidate set small.

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
                "roi": [
                    154,
                    202,
                    983,
                    291
                ]
            }
        },
        "action": "DoNothing"
    }
}
```

`transfer` (inventory and storage) and `port_storager` (portable storage) accept either a full two-sided ROI or a one-sided ROI. A full ROI recognizes both sides, while a one-sided ROI recognizes only that side. Workflows commonly pass the two sides separately according to the current operation.

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
                    "item_filters": [
                        "Normal:Product",
                        "Normal:Usable"
                    ]
                },
                "roi": [
                    1177,
                    450,
                    54,
                    54
                ]
            }
        },
        "action": "DoNothing"
    }
}
```

54x54 is only an example for this screen. The component resizes its built-in icons to the requested ROI size; callers do not provide icon assets.

## Parameter reference

Pipeline, Go Service, and the C++ API use the same recognition semantics. Only the field locations differ:

| Content | Pipeline | Go Service | C++ API |
| ---------------- | -------------------------- | ----------------------------------------------- | --------------------------------------------------------------------- |
| Native ROI | `recognition.param.roi` | `CustomRecognitionParam.ROI` | `RecognitionRequest.roi` |
| Component fields | `custom_recognition_param` | `CustomRecognitionParam.CustomRecognitionParam` | `RecognitionRequest`; candidate fields are under `request.candidates` |
| Registration | `IconRecognition` | `IconRecognition` | Construct `IconRecognizer` directly; no registration name is used |

The native ROI uses 1280x720 `[x,y,width,height]` coordinates. Width and height must be positive, and the rectangle must be fully inside the image. `single_roi` additionally requires equal width and height.

### Supported controllers

`IconRecognition` supports these two 1280x720 controller captures:

| Controller | Capture requirement | Grid profile |
| --- | --- | --- |
| Win32 | 1280x720 | Standard 720p UI |
| ADB | 1280x720 at 240 dpi | Enlarged ADB UI |

Regular grids select the matching profile automatically from repeated structures inside the request ROI. `rewards` selects from the white reward-card size. Insufficient evidence or similar profile scores returns `exception`; callers cannot specify a scale. The detector temporarily normalizes the image in memory for grid localization and maps cell coordinates back before returning. Item templates are still generated at the final source-cell size and matched on the original image, so `cell_box` and `item_box` always use source-image coordinates. `single_roi` does not run grid detection and never resizes the input image.

The component does not infer, move, or expand the request ROI from the selected controller profile. The caller remains responsible for a native ROI that is fully inside the image and completely covers every target cell. Profile selection reads only pixels inside that ROI. A rewards ROI may cover the whole reward group or one complete reward card.

### custom_recognition_param

| Field | Type | Required | Default | Description |
| -------------------- | ------------------- | ---------------------------------------- | ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `grid_type` | string / `GridType` | Yes for Custom; set it explicitly in C++ | None for Custom | Selects the grid locator for the current screen. See the table below. The C++ member initializer is only a construction placeholder |
| `item_ids` | string[] | No | `[]` | Keeps only the listed items. Multiple IDs form a union; duplicates are rejected |
| `item_filters` | string[] | No | Depends on `grid_type` | Selects candidates by `storageKind:categoryType`. Multiple filters form a union; `*` selects every category under that `storageKind` |
| `item_recheck_filters` | string[] | No | `[]` | Active when both this field and `item_ids` are non-empty; uses the same format as `item_filters`; performs a single-cell recheck on matched candidate cells |
| `threshold` | number | No | `0.85` | Minimum final match score, enforced uniformly for every grid type |
| `subpixel_threshold` | number | No | `0.60` | Tries finer position offsets when the base score reaches this value but remains below `threshold` |
| `deduplicate` | boolean | No | `false` | Keeps only the highest-scoring cell for each `item_id` |
| `debug` | boolean | No | `false` | Grid and cell diagnostics are collected when recognition reaches the result-assembly stage; early `invalid_image` or `exception` returns may lack them. `debug` controls performance timing and Custom debug-file writing |

- **`threshold` and `subpixel_threshold`**: Thresholds must satisfy `0 <= subpixel_threshold < threshold <= 1`. When a base score is below `subpixel_threshold`, the component considers that candidate clearly unreliable, skips the finer position search, and does not add it to `matches`. Scores between the two thresholds are refined. A result is returned only when its final score reaches `threshold` and it passes the low-texture check. Shipment quantity bars and valuable-depot portrait regions are excluded from the template-matching mask, but they do not bypass the uniform threshold. Check the ROI, frame stability, and candidate filters before lowering thresholds.
- **`item_ids` and `item_filters`**: `item_ids` specifies the items to find, while `item_filters` limits the candidate templates by category. When both are supplied, their intersection is used. Unknown or duplicate IDs, malformed filters, an empty filtered set, or an ID excluded by the filters returns `exception`.
- **`item_recheck_filters`**: When `item_ids` is used to find specific items, visually similar items outside that set may be mistaken for a target item. `item_recheck_filters` performs a single-cell recheck on matched candidate cells, keeping only candidates confirmed as the target `item_id`. Compared with omitting `item_ids` and using only `item_filters` to recognize the entire grid, this approach rechecks only matched cells and is usually faster. Set `deduplicate: true` as well to avoid repeated rechecks of the same item.

### grid_type, default candidates, and reference ROIs

| `grid_type` | C++ `GridType` | Screen | Default `item_filters` | Win32 reference ROI | ADB reference ROI |
| --------------- | ------------------------ | ------------------------ | ---------------------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `trade` | `GridType::Trade` | Settlement trade | `Normal:Product`, `Normal:Usable` | `[170,165,935,385]` | `[32,46,1216,549]` |
| `transfer` | `GridType::Transfer` | Inventory and storage | `Normal:*` | Full `[154,202,983,291]`; left `[154,202,585,291]`; right `[739,202,398,291]` | Full `[30,160,1220,370]`; left `[30,160,710,370]`; right `[780,160,470,370]` |
| `port_storager` | `GridType::PortStorager` | Portable storage | `Normal:*` | Full `[190,250,880,350]`; left `[190,250,318,350]`; right `[570,250,500,350]` | Full `[78,228,1150,410]`; left `[78,228,368,410]`; right `[562,326,620,293]` |
| `valuables` | `GridType::Valuables` | Valuable depot | `ValuableDepot:*` | `[24,76,950,570]` | `[100,85,790,540]` |
| `shipment` | `GridType::Shipment` | Shipment screen | `Normal:*` | `[34,132,386,474]` | `[43,169,480,408]` |
| `credit_trade` | `GridType::CreditTrade` | Credit trade | `ValuableDepot:SpecialItem`, `Isolate:*` | `[70,95,1140,415]` | `[10,120,1250,510]` |
| `rewards` | `GridType::Rewards` | Rewards screen | `Isolate:*`, `ValuableDepot:*` | `[39,82,1205,511]` | `[178,140,935,440]` |
| `single_roi` | `GridType::SingleRoi` | One caller-selected cell | `Normal:*` | Any square inside the image; example `[1177,450,54,54]` | Any square inside the image; example `[1151,393,66,66]` |

Reference ROIs are absolute 1280x720 screen coordinates, not coordinates relative to another ROI. Each value applies only to the listed screen, and the caller must keep every target cell fully covered. One-sided storage ROIs still use absolute screen coordinates.

`rewards` locates white reward cards and determines the horizontal origin of each row independently, so rows do not need aligned columns. Public results still treat them as one multi-row grid: `row` increases from top to bottom, while `column` restarts at zero in each row. Multiple game features reuse this screen type and may show different item categories. When `item_filters` is omitted or empty, the default candidate set is `Isolate:*`, `ValuableDepot:*`; a non-empty `item_filters` replaces that default set completely.

### Item IDs

An item ID is a top-level key from [`recognition_items.json`](/assets/data/IconRecognition/recognition_items.json), for example:

```json
{
    "item_copper_ore": {
        "category": "矿物",
        "storageKind": "Normal",
        "categoryType": "Ore",
        "rarity": 1,
        "iconId": "item_copper_ore"
    }
}
```

Pass `item_copper_ore`, not `iconId`, a locale key, or a display name. `iconId` is used only to locate the published icon asset.

### `item_filters` categories

Filters only reduce the built-in icon set used for matching; they do not change grid detection. The format is `storageKind:categoryType`. Both parts are case-sensitive and map directly to the same-named catalog fields.

Valid `storageKind` values:

| `storageKind` | Meaning |
| --------------- | -------------------- |
| `Normal` | Normal items |
| `ValuableDepot` | Valuable depot items |
| `Isolate` | Standalone resources |

Valid `categoryType` values by storage kind:

| `storageKind` | `categoryType` | Meaning |
| --------------- | ---------------- | -------------------- |
| `Normal` | `Ore` | Ores |
| `Normal` | `Plant` | Plants |
| `Normal` | `Product` | Products |
| `Normal` | `Doodad` | Gathered materials |
| `Normal` | `Nurturance` | Upgrade materials |
| `Normal` | `Usable` | Usable items |
| `Normal` | `Producer` | Production tools |
| `Normal` | `PortableDevice` | Portable devices |
| `ValuableDepot` | `Weapon` | Weapons |
| `ValuableDepot` | `CommercialItem` | Commercial valuables |
| `ValuableDepot` | `SpecialItem` | Upgrade materials |
| `Isolate` | `Gold` | Oroberyl vouchers |
| `Isolate` | `Diamond` | Origeometry |
| `Isolate` | `WeaponGold` | Arsenal quota |

Wildcard forms include `Normal:*`, `ValuableDepot:*`, and `Isolate:*`. The wildcard is supported only for `categoryType`; `*:Ore` is not valid.

## Results and the Pipeline hit box

`RecognitionResult` and Custom detail use the same structure:

| Field | Type | Description |
| ---------------- | ------- | --------------------------------------------------------------------------------- |
| `detail_version` | integer | Detail contract version; currently `2` |
| `matched` | boolean | Whether at least one result was accepted |
| `grid_type` | string | Requested grid type. It may be absent when parsing fails before the type is known |
| `roi` | integer[4] | Request ROI as `[x,y,width,height]` |
| `matches` | array | Accepted results ordered by score and position |
| `error` | object | Present on failure, with a stable `code` and a readable `message` |

Fields in `matches[]`:

| Field | Type | Description |
| -------------------------------- | ------- | ----------------------------------------------------------- |
| `item_id` | string | Top-level catalog item ID |
| `name` | string | Locale key, such as `iconRecognition.name.item_copper_ore` |
| `category` | string | Catalog category label |
| `storage_kind` / `category_type` | string | Classification fields for later filtering or business rules |
| `rarity` | integer | Catalog rarity |
| `cell_box` | integer[4] | Owning grid cell as `[x,y,width,height]`; equal to the request ROI for `single_roi` |
| `item_box` | integer[4] | Final template match location as `[x,y,width,height]` |
| `score` | number | Final match score |
| `row` / `column` | integer | Row and column for real grids; absent for `single_roi` |

Results are sorted by descending `score`, then by `cell_box.y`, `cell_box.x`, and `item_id`. With `deduplicate=true`, only the first sorted result for each `item_id` remains.

Custom returns `MAA_TRUE` when at least one result is accepted. The Pipeline recognition box `out_box` equals `matches[0].cell_box`. With no accepted result, Custom returns `MAA_FALSE`. MaaFramework wraps callback detail in `all/filtered/best`: the complete component payload for a hit is in `best.detail`; on a miss, `best` is `null` and the component payload remains in `all[0].detail`.

### `error.code`

| `code` | Trigger | Caller handling |
| --------------- | --------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `invalid_image` | The input image is empty | Check the capture or direct-call input; normal recognition did not run |
| `exception` | Parameter validation, resource loading, grid detection, or matching failed | Log `message` for diagnosis, but do not branch on its text |
| `no_match` | Recognition completed normally, but no sufficiently reliable item was found | Handle it as “item not found this time,” not as a component failure |

All three cases return `MAA_FALSE`, but they do not mean the same thing. `invalid_image` and `exception` mean the request did not complete normally. `no_match` means image handling, parameter parsing, grid detection, and per-cell matching completed without an exception, but every candidate in the located cells scored too low or was rejected by the low-texture check. If grid detection cannot produce a formal cell, the result is `exception`, not `no_match`. For `no_match`, `matched=false` and `matches=[]`; callers should normally continue as if the target item is currently absent instead of reporting a component failure.

`no_match` preserves the parsed `grid_type` and `roi`. An `exception` raised during parsing may not contain every request field.

## Reference screens

> [!NOTE]
> The screenshots and highlighted ROIs below come from the Win32 controller and do not apply to ADB. Use the ADB reference ROIs from the table above.

<details>
<summary>Settlement trade</summary>

![settlement-trade](https://github.com/user-attachments/assets/7e8623ad-61be-4415-add8-c1c2abf95390)

</details>

<details>
<summary>Inventory and storage</summary>

![inventory-transfer](https://github.com/user-attachments/assets/bc1bbea5-5aa4-4421-9ccb-b3de8314688e)

</details>

<details>
<summary>Portable storage</summary>

![port-storager](https://github.com/user-attachments/assets/f0fab5de-186d-40df-b212-7b8ae6714103)

</details>

<details>
<summary>Valuable depot</summary>

![valuables](https://github.com/user-attachments/assets/4121c648-94b8-4032-8afd-3436cf31f99b)

</details>

<details>
<summary>Shipment</summary>

![shipment](https://github.com/user-attachments/assets/61eb016e-13f6-4e5d-80f1-c1d1eecb57d7)

</details>

<details>
<summary>Credit trade</summary>

![credit-trade](https://github.com/user-attachments/assets/c4415a7c-56d0-4aa2-bf2e-230bae21211d)

</details>

<details>
<summary>single_roi example</summary>

![specific-roi-example](https://github.com/user-attachments/assets/76e7f9d0-ed4e-4feb-b1b4-afbc40ac6003)

</details>

## Go Service

Go services call the `IconRecognition` registration through `ctx.RunRecognitionDirect`. Set the native ROI through `CustomRecognitionParam.ROI` and keep component-specific fields in `CustomRecognitionParam.CustomRecognitionParam`:

MaaEnd Go Service callers should reuse `agent/go-service/pkg/iconrecognition` instead of declaring IconRecognition parameter or detail structs in each business package. `Params` represents `custom_recognition_param`; `Detail` and `Match` represent the component detail, and `Match.CellBox` is the cell rectangle for follow-up actions:

```go
import "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"

detail, err := ctx.RunRecognitionDirect(
    maa.RecognitionTypeCustom,
    &maa.CustomRecognitionParam{
        ROI:                maa.NewTargetRect(maa.Rect{154, 202, 983, 291}),
        CustomRecognition: iconrecognition.CustomRecognitionName,
        CustomRecognitionParam: iconrecognition.NewParams(
            iconrecognition.WithGridType(iconrecognition.GridTypeTransfer),
            iconrecognition.WithItemIDs("item_copper_ore"),
            iconrecognition.WithItemFilters(iconrecognition.StorageFilter().Normal.Ore),
            iconrecognition.WithDeduplicate(true),
        ),
    },
    img,
)

parsed, _, err := iconrecognition.ParseRecognitionDetail(detail)
if err != nil {
    return
}
for _, match := range parsed.Matches {
    _ = match.CellBox
}
```

`iconrecognition.ParseRecognitionDetail` selects the Custom payload from Maa results: it uses `Results.Best` on a hit and `Results.All[0]` on a miss, so callers do not need to merge or deduplicate result buckets. When a `CustomRecognitionResult.Detail` string is already available, parse it directly with `iconrecognition.ParseDetail`.

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

`request.candidates.item_ids` and `item_filters` correspond to the same-named Pipeline and Go parameters. C++ returns `RecognitionResult` directly, without MaaFramework's outer JSON wrapper.

## Debug output

With `debug=true`, the Custom entry point writes the recognition under `exe_dir/../debug/vision/IconRecognition`:

- `raw/<stem>.png`: input image;
- `annotated/<stem>.png`: ROI, cells, candidate boxes, and scores;
- `detail/<stem>.json`: public result plus internal diagnostics.

The three files with one stem form a group. Older groups are cleaned automatically. When recognition reaches the result-assembly stage, core C++ results retain grid and cell diagnostics; early `invalid_image` or `exception` returns may lack them. With `debug=true`, performance diagnostics are recorded too. Public Custom detail omits these internal fields; only debug files and manual test output include them.

## Tests and internals

- [Tests and manual review output](/agent/cpp-algo/source/IconRecognition/docs/testing.md)
- [Internal architecture and maintenance boundaries](/agent/cpp-algo/source/IconRecognition/docs/architecture.md)
- [Recognition algorithm](/agent/cpp-algo/source/IconRecognition/docs/algorithm.md)
- [Grid configuration maintenance](/agent/cpp-algo/source/IconRecognition/docs/grid-profiles.md)
- [Resource download and publishing](/tools/icon_recognition/README.md)
