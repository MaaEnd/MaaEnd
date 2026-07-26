# RecoGrid Grid Scanning Engine Integration Guide

`RecoGrid` is a general-purpose grid recognition and rolling cumulative scanning engine in `cpp-algo`, with source code located at `agent/cpp-algo/source/RecoGrid/`. It is suitable for scenarios where "a list is composed of regular grid cells, each containing an icon, and the list requires scrolling down to be fully scanned."

Existing production integrations include `WeaponInventoryScan` and `EssenceGridScan`. When developing a new integration, do not copy parameters directly. First use screenshots to confirm stable grid detection, then adjust template classification, and finally integrate rolling accumulation and Pipeline.

## First Determine if it Can Be Used

Suitable for using `RecoGridEngine`:

- The target area is a regular grid of rows and columns.
- The main icon position in each valid cell is relatively stable.
- Template images for each item can be prepared.
- After scrolling, the current page content can still be determined from screenshots, rather than relying only on fixed scroll counts.

Not suitable for using:

- Grid boundaries are not obvious, and stable segments cannot be found through row/column projection.
- The position, scaling, and occlusion of items of the same class within a cell vary greatly.
- C++ is responsible for clicking, navigating interfaces, failure retries, and other business flows. The flow should be managed by Pipeline; RecoGrid only handles recognition and accumulation.

Before starting to write code, at least prepare:

- 720p baseline screenshot, resolution annotated as `1280x720`.
- Grid area `roi`.
- Consecutive screenshots before and after a single scroll, preferably including the first screen, middle pages, and last page.
- Template image directory.
- Entry method, scrolling method, and stable waiting area of the target interface.

Do not hard-code Pipeline if this information is missing. RecoGrid is sensitive to `roi`, grid boundaries, and template quality; parameters guessed from speculation are basically unmaintainable.

## Engine Integration Method

`RecoGridEngine` is a C++ internal API and cannot be called directly from Pipeline. New business needs to write its own C++ wrapper, call the engine within the wrapper, and then write the results to Maa.

`RecoGridEngine` is responsible for the complete scanning process:

- Loading a template directory.
- Detecting the grid on the current page.
- Filtering empty cells.
- Performing multi-template classification on cells.
- Using `sessionId` to accumulate results across multiple pages.
- Determining if scrolling has reached the end.

## Core API Input and Output

The business wrapper ultimately only needs to organize code around this call:

```cpp
GridScanResult Scan(
    const std::string& sessionId,
    const cv::Mat& image,
    const GridScanOptions& options = {});
```

Input:

| Input       | How to Obtain                                             | Notes                                                                                                                  |
| ----------- | --------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `sessionId` | Business-customized string, e.g., `"WeaponInventoryScan"` | Must remain consistent for the same scrolling list; reset at new task start; do not share between different businesses |
| `image`     | Convert `MaaImageBuffer` from Maa callback to `cv::Mat`   | Empty image will return failure; coordinates will be normalized based on `normalizedSize`                              |
| `options`   | Business default values + Pipeline override parameters    | Let default values run through first, then expose a few parameters to Pipeline                                         |

Output `GridScanResult`:

| Field                                            | Meaning                                                                    | How to View During Debugging                                |
| ------------------------------------------------ | -------------------------------------------------------------------------- | ----------------------------------------------------------- |
| `success` / `message`                            | Whether this frame was accepted, and the reason for failure                | Check the image, templates, ROI, and alignment state        |
| `rows` / `cols`                                  | Rows and columns detected on the current visible page                      | Determine if grid detection is stable                       |
| `totalCells`                                     | Number of valid cells on current page, not just `rows * cols`              | Affected by empty cell filtering                            |
| `sessionRows` / `sessionCols`                    | Rows and columns of the accumulated session                                | Determine the shape of the accumulated list                 |
| `sessionTotalCells`                              | Accumulated number of valid cells                                          | Usually the primary concern for business                    |
| `knownCells` / `unknownCells`                    | Accumulated count of classified / unclassified cells                       | Determine template classification quality                   |
| `rowOffset`                                      | Accepted non-negative progress; always `0` when the frame is not committed | Determine whether this frame actually advanced the viewport |
| `rawAlignmentOffset` / `adjustedAlignmentOffset` | Raw alignment / candidate after leading-partial adjustment                 | Inspect rejected negative or unstable candidates            |
| `supportRows` / `alignmentStatus`                | Candidate overlap rows / alignment decision state                          | Check the two-row minimum and rejection reason              |
| `fallbackUsed` / `fallbackStreak`                | Whether trusted historical progress was used / consecutive fallback count  | The consecutive value must not exceed `1`                   |
| `deltaReliable`                                  | Whether alignment between the current and committed pages is reliable      | Prioritize this when rolling accumulation is incorrect      |
| `hasProgress`                                    | Whether this frame brought new cells                                       | Should usually be true for middle pages                     |
| `reachedEnd`                                     | Whether the end has been confirmed                                         | Used to decide whether to continue scrolling or stop        |
| `matchRatio` / `averageDistance`                 | Page overlap matching quality                                              | Check these when adjusting scroll parameters                |
| `newCellIndices`                                 | Indices of new cells on the current page                                   | Shows how much content this frame added                     |
| `cells`                                          | Sorted list of accumulated cells                                           | Do not write the complete list to `out_detail` by default   |

Common fields for each `GridScanCell` in `cells`:

| Field                                  | Meaning                                           |
| -------------------------------------- | ------------------------------------------------- |
| `row` / `col`                          | Global row and column in the session              |
| `cellIndex`                            | Cell index within the current visible page        |
| `screenCell`                           | Cell rectangle in original screenshot coordinates |
| `templateId`                           | Classification id, from the template filename     |
| `matched`                              | Whether classification was successful             |
| `visible`                              | Whether it is visible in this frame               |
| `score` / `templateScore` / `hueScore` | Classification score                              |
| `phashDistance`                        | pHash distance from the template                  |

## Recommended Development Order

Do not start by integrating the complete Pipeline. Follow the order below:

1.  Take screenshots and define `roi`
2.  Adjust until grid rows and columns are stable for the current page
3.  Prepare template directory and confirm template ids
4.  Adjust occupancy filtering to prevent empty cells from entering classification
5.  Adjust template classification to reduce unknowns and misclassifications
6.  Adjust scroll delta to make `rowOffset` stable
7.  Adjust end detection to make `reachedEnd` accurate
8.  Write business wrapper
9.  Integrate Pipeline recognition, scrolling, freeze wait
10. Review `out_detail` and logs to re-test the first screen, middle pages, and last page

The following expands on these steps.

## Step 1: Define ROI and Grid Detection Parameters

Grid detection only looks at `recognition.detect`. The internal process is:

1.  Resize the screenshot to `normalizedSize`.
2.  Crop `roi`.
3.  Convert to grayscale and apply Otsu binarization.
4.  Project rows and columns separately.
5.  Use thresholds to find row/col segments.
6.  Filter out segments that are too small.
7.  Intersect row segments and col segments to generate cells.

The most important initial parameters:

```cpp
options.recognition.detect.normalizedSize = { 1280, 720 };
options.recognition.detect.roi = { x, y, width, height };
options.recognition.detect.rowThresholdRatio = 0.3;
options.recognition.detect.colThresholdRatio = 0.4;
options.recognition.detect.minRawSegmentLength = 10;
options.recognition.detect.minKeptSegmentRatio = 0.8;
```

How to set parameters:

| Parameter             | How to Fill Initially                           | What to Observe                                                    | How to Adjust                                                            |
| --------------------- | ----------------------------------------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `normalizedSize`      | Usually fixed `{1280, 720}`                     | Overall coordinate offset                                          | Confirm if screenshot annotation is based on 720p                        |
| `roi`                 | Frame the complete grid, minimize irrelevant UI | Wrong row/column count, misidentification of titles/buttons        | Tighten to the cell area; do not cut off the main body of complete cells |
| `rowThresholdRatio`   | `0.2` to `0.4`                                  | Too many rows: noise mistaken for rows; too few rows: cells missed | Too many rows: increase; too few rows: decrease                          |
| `colThresholdRatio`   | `0.3` to `0.5`                                  | Too many or too few columns                                        | Too many columns: increase; too few columns: decrease                    |
| `minRawSegmentLength` | `8` to `12`                                     | Many small fragments                                               | Increase; if thin cells are missed, decrease                             |
| `minKeptSegmentRatio` | `0.8` to `0.9` recommended for scrolling lists  | Top/bottom half cells treated as full rows                         | Increase; if valid rows are filtered, decrease                           |

Determining if grid detection is qualified:

- `page_cols` should be stable on the first screen, middle pages, and last page.
- `page_rows` should not frequently jump between two numbers during normal scrolling.
- `page_grid = page_rows * page_cols` should match the number of visible cells seen by the naked eye.
- If `page_rows` is frequently wrong, first adjust `roi` and segment parameters, not the rolling accumulation parameters.

The current parameters in `WeaponInventoryScan` are only instance references:

```cpp
options.recognition.detect.roi = { 20, 70, 960, 600 };
options.recognition.detect.rowThresholdRatio = 0.2;
options.recognition.detect.colThresholdRatio = 0.4;
options.recognition.detect.minRawSegmentLength = 10;
options.recognition.detect.minKeptSegmentRatio = 0.9;
```

## Step 2: Set Mask

A mask is used to ignore areas within a cell that are unsuitable for recognition, such as the top-left level indicator, top-right corner badge, bottom text strip, and rarity border. It affects:

- pHash.
- Template classification.
- Empty cell occupancy judgment.

The fields are cell size ratios, not pixels:

```cpp
options.recognition.mask.leftHeaderWidth = 0.0;
options.recognition.mask.leftHeaderHeight = 0.0;
options.recognition.mask.rightHeaderWidth = 0.0;
options.recognition.mask.rightHeaderHeight = 0.0;
options.recognition.mask.bottomHeight = 0.0;
```

Meanings:

| Field                                    | Ignored Area        |
| ---------------------------------------- | ------------------- |
| `leftHeaderWidth` + `leftHeaderHeight`   | Top-left rectangle  |
| `rightHeaderWidth` + `rightHeaderHeight` | Top-right rectangle |
| `bottomHeight`                           | Entire bottom strip |

For example, if a cell is approximately `96x96` and you want to ignore the top-left `20x20`, top-right `30x30`, and bottom `20px`:

```cpp
options.recognition.mask.leftHeaderWidth = 20.0 / 96.0;
options.recognition.mask.leftHeaderHeight = 20.0 / 96.0;
options.recognition.mask.rightHeaderWidth = 30.0 / 96.0;
options.recognition.mask.rightHeaderHeight = 30.0 / 96.0;
options.recognition.mask.bottomHeight = 20.0 / 96.0;
```

Parameter adjustment suggestions:

- If the same item has unstable matching due to corner badges, levels, or quantities, expand the corresponding mask.
- If different items' main bodies are occluded by the mask causing confusion, reduce the mask.
- The mask should be consistent with the template cropping logic. The cleaner the effective main body retained in the template, the more stable the classification.

## Step 3: Prepare Template Directory

`RecoGridEngine` must load templates before it can `Scan()`:

```cpp
g_engine.LoadTemplatesFromDirectory("assets/data/YourIcon");
```

Template rules:

- Supports `.png`, `.jpg`, `.jpeg`, `.webp`, `.bmp`.
- By default, only reads the first level of the directory.
- The file name stem is the `templateId` for the classification result, e.g., `ak47.png` outputs `ak47`.
- The id cannot be empty or duplicate.
- The image must be readable normally.

For recursive reading:

```cpp
recogrid::TemplateLoadOptions loadOptions;
loadOptions.recursive = true;
g_engine.LoadTemplatesFromDirectory("assets/data/YourIcon", loadOptions);
```

Templates can also be set manually:

```cpp
std::vector<recogrid::GridClassifyTemplate> templates;
templates.push_back({ "template_id", imageMat });
g_engine.SetTemplates(std::move(templates));
```

Template suggestions:

- Try to crop to icons consistent with the actual cell main body, avoiding extra background.
- The style, resolution, and cropping range of the same set of templates should be unified.
- If there is fixed UI noise in the cell, prioritize using a mask to ignore it; do not force the template to absorb noise.
- Do not put "unknown" as a template; when unmatched, the engine will use `unknownTemplateId`.

## Step 4: Adjust Empty Cell Filtering

The engine does not classify all cells; it first determines if a cell "appears to have content." Related parameters:

```cpp
options.occupiedBrightThreshold = 70;
options.minOccupiedMean = 55.0;
options.minOccupiedBrightRatio = 0.20;
```

Internal judgment logic:

- Apply mask first.
- Calculate the average grayscale `mean` of the retained area.
- Count the ratio of bright pixels above `occupiedBrightThreshold` as `brightRatio`.
- Only if `mean >= minOccupiedMean` and `brightRatio >= minOccupiedBrightRatio` is the cell considered occupied.

Parameter adjustment table:

| Phenomenon                                          | Priority Adjustment                                                               |
| --------------------------------------------------- | --------------------------------------------------------------------------------- |
| Empty cells treated as items, `page_grid` too large | Increase `minOccupiedMean` or `minOccupiedBrightRatio`                            |
| Dark items missed, `page_grid` too small            | Decrease `minOccupiedMean` or `minOccupiedBrightRatio`                            |
| Bright border/badge causes empty cell misjudgment   | Set mask, or increase `minOccupiedBrightRatio`                                    |
| Very few bright icons on a dark background          | Decrease `minOccupiedBrightRatio`, do not just decrease `occupiedBrightThreshold` |

It is recommended to first make `page_grid` close to the number of "cells with content" seen by the naked eye, then adjust classification. Otherwise, classification parameters will be skewed by empty cell noise.

## Step 5: Adjust Template Classification

Classification has two stages:

1.  pHash initial screening: only keep templates with Hamming distance not exceeding `maxPhashDistance`.
2.  Fine sorting: scale the template to cell size, and calculate the final `score` using grayscale template matching and optional hue scoring.

Main parameters:

```cpp
options.recognition.maxPhashDistance = 10;
options.recognition.minScore = 0.6;
options.recognition.hueWeight = 0.4;
options.recognition.maxRankedCandidates = 0;
```

However, note: in multi-template classification, `maxRankedCandidates = 0` is not truly unlimited. The source code defaults to fine-sorting up to 5 pHash-closest templates per cell. Setting it to greater than 0 indicates the number of templates to fine-sort per cell.

Parameter adjustment table:

| Parameter             | Effect                                          | Increase                                                            | Decrease                                                                            |
| --------------------- | ----------------------------------------------- | ------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| `maxPhashDistance`    | pHash initial screening distance                | More candidates, reduces unknown, but slower, easier to misclassify | Fewer candidates, faster, but may miss matches                                      |
| `minScore`            | Final acceptance threshold                      | Reduces misclassification, increases unknown                        | Reduces unknown, increases misclassification risk                                   |
| `hueWeight`           | Hue score weight                                | Values color more, suitable for distinguishing colorful icons       | Values shape/brightness more, suitable when color is easily affected by environment |
| `maxRankedCandidates` | Number of templates entering fine sort per cell | Reduces false screening risk, but slower                            | Faster, but correct templates ranked lower in pHash may not enter                   |

Recommended adjustment method:

1.  Start with `maxPhashDistance = 10`, `minScore = 0.6`, `hueWeight = 0.3~0.4`.
2.  If many valid cells are unknown, check `phashDistance` and `score`:
    - `phashDistance` often slightly above threshold: increase `maxPhashDistance`.
    - `score` often slightly below threshold: decrease `minScore` or check mask/template cropping.
3.  If many misclassifications:
    - Increase `minScore`.
    - Decrease `maxPhashDistance`.
    - Check if templates are too similar; if necessary, increase `hueWeight`.
4.  If colors are similar but shapes differ, decrease `hueWeight`.
5.  If shapes are similar but colors differ, increase `hueWeight`.

Do not eliminate unknowns by infinitely lowering `minScore`. Unknowns are valuable signals indicating that templates, masks, screenshots, or thresholds need checking.

## Step 6: Adjust Rolling Accumulation

Rolling accumulation is maintained by `sessionId`. Each call:

```cpp
const recogrid::GridScanResult result = g_engine.Scan(kSessionId, imageMat, options);
```

The same `sessionId` accumulates into the same session. It must be reset at the start of a new task:

```cpp
g_engine.ResetSession(kSessionId);
```

Scrolling-related parameters:

```cpp
options.incremental = true;
options.matchDistanceThreshold = 12;
options.minMatchRatio = 0.5;
options.endMinMatchRatio = 0.95;
```

These parameters look at "how many cells' pHash can match between adjacent pages."

| Parameter                | Effect                                            | Increase                                                                                 | Decrease                                                                       |
| ------------------------ | ------------------------------------------------- | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| `incremental`            | Whether to enable session accumulation            | Usually keep `true`                                                                      | `false` only scans current page                                                |
| `matchDistanceThreshold` | Maximum pHash distance for two cells to match     | Easier to consider same cell match, delta more reliable, but higher risk of misalignment | Stricter, less misalignment, but slight changes during scrolling may not match |
| `minMatchRatio`          | Matching ratio required for delta reliability     | More conservative, reduces misaligned accumulation                                       | More aggressive, reduces sticking, but may misalign                            |
| `endMinMatchRatio`       | Ratio for judging repeated pages to determine end | Less likely to end prematurely, but may scroll a few extra times                         | Easier to end, but may stop prematurely                                        |

Alignment still evaluates both forward and backward candidates, but only the globally best candidate with at least two valid overlap rows is eligible. A negative winner, or equal forward/backward evidence, is a direction error in the forward-only workflow. The engine never applies an absolute value, clamps it to zero, or promotes a weaker positive candidate.

A reliable positive offset commits immediately. A strong zero offset must be confirmed twice before the engine reports the end. Every other result returns `success = false` without changing the committed snapshot, viewport, cells, or end-confirmation count. The wrapper must not override next to Swipe in this case; MaaFW re-enters the recognition node through its next list and captures the same screen again.

The first failure stores only a same-screen verification snapshot; failed evidence is never carried across another swipe. The next frame must be a strong zero-offset match against that snapshot, have the same shape, and have at least two support rows. If the session already has a trusted `lastPositiveRowOffset`, the engine may use it for one fallback commit. A fallback does not update the trusted offset, and a new visually confirmed positive transition is required before fallback becomes eligible again. Without trusted history, or when a second consecutive fallback would be required, recognition keeps retrying the same screen until the existing recognition window expires.

Observe `out_detail`:

| Field                       | How to View                                                                  |
| --------------------------- | ---------------------------------------------------------------------------- |
| `row_offset`                | Only accepted non-negative progress; `0` when the frame was not committed    |
| `raw_alignment_offset`      | Raw globally best candidate; a negative value diagnoses a direction error    |
| `adjusted_alignment_offset` | Candidate after the leading-partial-row adjustment                           |
| `support_rows`              | Valid overlap rows; an accepted candidate requires at least `2`              |
| `alignment_status`          | Commit, end confirmation, or a specific rejection state                      |
| `fallback_used`             | Whether same-screen verification used the trusted historical positive offset |
| `fallback_streak`           | Consecutive fallback count; production flow must not exceed `1`              |
| `delta_reliable`            | Should often be `true` on normal middle pages                                |
| `match_ratio`               | Higher means page overlap is more obvious                                    |
| `new_cells`                 | Should be > 0 on middle pages; may be 0 on repeated pages or at the end      |
| `has_progress`              | Whether new content was actually added                                       |
| `reached_end`               | Whether the list end is determined                                           |

Stable `alignment_status` values are:

- Non-rejection states: `not_applicable`, `accepted`, `fallback_accepted`, `end_confirmation`, and `reached_end`.
- Rejection states: `grid_missing`, `shape_rejected`, `insufficient_support`, `direction_ambiguous`, `direction_violation`, `low_confidence`, `zero_unconfirmed`, `no_progress`, `fallback_unavailable`, and `fallback_limit_reached`.

Every rejection enters same-screen verification. Because the current frame was not committed, `row_offset` remains `0` and the wrapper must not advance to Swipe. Use `unresolved_reason` for additional failure details.

Common issues:

| Phenomenon                             | First Check                                                           | Adjustment Direction                                                                                              |
| -------------------------------------- | --------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| No accumulation growth after scrolling | `alignment_status`, `raw_alignment_offset`, `support_rows`            | Confirm whether this is a direction error or insufficient support, then check actual motion and the freeze region |
| Accumulation skips rows or repeats     | Accepted `row_offset`, `adjusted_alignment_offset`, `fallback_streak` | Check stable `page_rows/page_cols`, then inspect partial-row adjustment and alignment thresholds                  |
| Recognition failure still swipes       | The wrapper's next-node override                                      | Never override to Swipe on failure; let MaaFW recognize the same screen again                                     |
| Repeated direction errors              | `raw_alignment_offset < 0`, `match_ratio`, `support_rows`             | Check repetitive icons, scroll direction, and stability; never use abs or a weaker positive candidate             |
| The next page also needs fallback      | `fallback_used`, `fallback_streak`                                    | Require a new reliable positive transition; reject a second consecutive fallback                                  |
| Ends prematurely                       | `reached_end = true`, `match_ratio`, `support_rows`                   | Increase `endMinMatchRatio`, or adjust scroll distance to avoid duplicate middle pages                            |
| Keeps scrolling at the bottom          | End-page `match_ratio`, `alignment_status`                            | Decrease `endMinMatchRatio`, or ensure the waiting area is stable after scrolling                                 |

First ensure screenshots are stable after scrolling before adjusting these parameters. Unfinished scroll animation frames will make pHash delta appear random, and parameters will be unreliable no matter how adjusted.

## Step 7: Write Business Wrapper

The business wrapper's responsibilities:

- Hold a `recogrid::RecoGridEngine`.
- Load template directory.
- Set business default parameters.
- Reset session at new task start.
- Call `Scan()`.
- Write `GridScanResult` as `out_detail`.
- Only when `success = true`, override the next Pipeline node based on
  `reachedEnd`; preserve same-screen recognition retry on failure.

Minimum structure:

```cpp
#include "../RecoGrid/RecoGridEngine.h"
#include "../utils.h"

namespace yourscan
{
namespace
{

constexpr const char* kSessionId = "YourScan";
recogrid::RecoGridEngine g_engine;
bool g_loaded = false;
MaaTaskId g_lastTaskId = MaaInvalidId;

void EnsureLoaded()
{
    if (!g_loaded) {
        g_engine.LoadTemplatesFromDirectory("assets/data/YourIcon");
        g_loaded = true;
    }
}

void ResetSessionForNewTask(MaaTaskId taskId)
{
    if (taskId == MaaInvalidId || taskId == g_lastTaskId) {
        return;
    }
    g_engine.ResetSession(kSessionId);
    g_lastTaskId = taskId;
}

void ApplyScanDefaults(recogrid::GridScanOptions& options)
{
    options.recognition.detect.normalizedSize = { 1280, 720 };
    options.recognition.detect.roi = { 20, 70, 960, 600 };
    options.recognition.detect.rowThresholdRatio = 0.3;
    options.recognition.detect.colThresholdRatio = 0.4;
    options.recognition.detect.minRawSegmentLength = 10;
    options.recognition.detect.minKeptSegmentRatio = 0.85;

    options.recognition.maxPhashDistance = 10;
    options.recognition.minScore = 0.6;
    options.recognition.hueWeight = 0.4;
    options.recognition.maxRankedCandidates = 0;

    options.incremental = true;
    options.matchDistanceThreshold = 12;
    options.minMatchRatio = 0.5;
    options.endMinMatchRatio = 0.95;
}

} // namespace
} // namespace yourscan
```

Key calls in the callback:

```cpp
EnsureLoaded();
ResetSessionForNewTask(task_id);

recogrid::GridScanOptions options;
ApplyScanDefaults(options);

const recogrid::GridScanResult result = g_engine.Scan(kSessionId, to_mat(image), options);
```

Register in `main.cpp`:

```cpp
MaaAgentServerRegisterCustomRecognition("YourScanRecognition", yourscan::YourScanRecognitionRun, nullptr);
```

`agent/cpp-algo/source/CMakeLists.txt` currently uses `GLOB_RECURSE` to collect source files; adding new `.cpp` / `.h` files will usually automatically enter the build.

## Step 8: Design out_detail

It is recommended that the wrapper outputs a compact summary by default, not stuffing the entire `cells` into it. The complete list can be large, slowing down logs and Maa detail.

Recommended fields:

```json
{
    "success": true,
    "page_grid": 30,
    "cumulative_grid": 84,
    "known": 82,
    "unknown": 2,
    "page_rows": 5,
    "page_cols": 6,
    "rows": 14,
    "cols": 6,
    "new_cells": 12,
    "row_offset": 2,
    "raw_alignment_offset": 2,
    "adjusted_alignment_offset": 2,
    "support_rows": 3,
    "alignment_status": "accepted",
    "delta_reliable": true,
    "fallback_used": false,
    "fallback_streak": 0,
    "has_progress": true,
    "reached_end": false,
    "matched_cells": 24,
    "compared_cells": 30,
    "match_ratio": 0.8,
    "average_distance": 4.2,
    "delta_score": 221.5
}
```

Field meanings:

| Field                       | Source                           | Purpose                                                |
| --------------------------- | -------------------------------- | ------------------------------------------------------ |
| `page_grid`                 | `result.totalCells`              | Number of valid cells on current page                  |
| `cumulative_grid`           | `result.sessionTotalCells`       | Accumulated cell count                                 |
| `known`                     | `result.knownCells`              | Number classified                                      |
| `unknown`                   | `result.unknownCells`            | Number unclassified                                    |
| `page_rows/page_cols`       | `result.rows/cols`               | Current page detected rows/columns                     |
| `rows/cols`                 | `result.sessionRows/sessionCols` | Accumulated session rows/columns                       |
| `new_cells`                 | `result.newCellIndices.size()`   | New cells in this frame                                |
| `row_offset`                | `result.rowOffset`               | Accepted non-negative progress; `0` when not committed |
| `raw_alignment_offset`      | `result.rawAlignmentOffset`      | Raw globally best alignment candidate                  |
| `adjusted_alignment_offset` | `result.adjustedAlignmentOffset` | Candidate after leading-partial adjustment             |
| `support_rows`              | `result.supportRows`             | Number of valid candidate overlap rows                 |
| `alignment_status`          | `result.alignmentStatus`         | Reason the alignment was accepted or rejected          |
| `fallback_used`             | `result.fallbackUsed`            | Whether the trusted historical offset was used         |
| `fallback_streak`           | `result.fallbackStreak`          | Consecutive fallback count                             |
| `delta_reliable`            | `result.deltaReliable`           | Whether alignment is reliable                          |
| `reached_end`               | `result.reachedEnd`              | Whether at list end                                    |
| `match_ratio`               | `result.matchRatio`              | Page overlap matching ratio                            |

When exporting the complete result is needed, a business switch can be added, e.g., `return_cells`, to output only during debugging or when there is a consumer:

```json
{
    "cells": [
        {
            "row": 0,
            "col": 0,
            "template_id": "ak47",
            "matched": true,
            "score": 0.91
        }
    ]
}
```

## Step 9: How to Connect Pipeline

Pipeline still needs to follow "Recognition -> Operation -> Re-recognition." RecoGrid is not responsible for entering the interface or performing scroll actions.

Recommended flow:

1.  Enter the target interface.
2.  Recognize title, tab, or other stable elements to confirm you are in the target list.
3.  `pre_wait_freezes` to wait for the grid area to stabilize.
4.  Call business `Custom Recognition` to scan the current page.
5.  Only when `success = true`, the C++ wrapper overrides the next node based on `reachedEnd`:
    - Not at end: `YourScanSwipeNext`
    - At end: `YourScanFinish`
    - Failure: do not override to Swipe; let the current Custom Recognition retry through its next list
6.  Scroll.
7.  Move mouse/touch point away to avoid hover or finger occlusion.
8.  `post_wait_freezes` to wait for the grid area to stabilize.
9.  Return to the scan node.

Example skeleton:

```json
{
    "YourScanRecognizePage": {
        "recognition": {
            "type": "Custom",
            "param": {
                "custom_recognition": "YourScanRecognition",
                "custom_recognition_param": {
                    "roi": [
                        20,
                        70,
                        960,
                        600
                    ],
                    "normalized_size": [
                        1280,
                        720
                    ],
                    "incremental": true
                }
            }
        },
        "pre_wait_freezes": {
            "time": 100,
            "target": [
                20,
                70,
                960,
                600
            ]
        },
        "action": "DoNothing",
        "next": [
            "YourScanFinish"
        ]
    },
    "YourScanSwipeNext": {
        "recognition": "DirectHit",
        "action": {
            "type": "Swipe",
            "param": {
                "begin": [
                    500,
                    540
                ],
                "end": [
                    500,
                    380
                ],
                "end_hold": 400,
                "duration": 200
            }
        },
        "next": [
            "YourScanMoveCursorAway"
        ]
    },
    "YourScanMoveCursorAway": {
        "recognition": "DirectHit",
        "action": "TouchMove",
        "target": [
            0,
            0,
            1,
            1
        ],
        "post_wait_freezes": {
            "time": 100,
            "target": [
                20,
                70,
                960,
                600
            ]
        },
        "next": [
            "YourScanRecognizePage"
        ]
    },
    "YourScanFinish": {
        "recognition": "DirectHit",
        "action": "StopTask"
    }
}
```

The ROI and scroll coordinates in the example are structural examples and must be re-measured based on actual 720p screenshots. Do not add hard delays for "stability"; prioritize using freeze waits and intermediate recognition nodes to confirm page state.

## How custom_recognition_param Overrides Default Values

The business wrapper can, like `WeaponInventoryScan`, first set C++ default values, then parse `custom_recognition_param` to override.

Common exposable parameters:

```json
{
    "roi": [
        20,
        70,
        960,
        600
    ],
    "normalized_size": [
        1280,
        720
    ],
    "row_threshold_ratio": 0.2,
    "col_threshold_ratio": 0.4,
    "min_raw_segment_length": 10,
    "min_kept_segment_ratio": 0.9,
    "mask": {
        "left_header_width": 0.2,
        "left_header_height": 0.2,
        "right_header_width": 0.3,
        "right_header_height": 0.3,
        "bottom_height": 0.2
    },
    "max_phash_distance": 10,
    "max_ranked_candidates": 0,
    "min_score": 0.6,
    "hue_weight": 0.4,
    "incremental": true,
    "end_min_match_ratio": 0.95
}
```

`GridRecognitionRequest::from_json()` natively supports these recognition fields:

- `roi`
- `normalized_size`
- `row_threshold_ratio`
- `col_threshold_ratio`
- `min_raw_segment_length`
- `min_kept_segment_ratio`
- `mask` / `mask_ratios`
- `max_phash_distance`
- `min_score`
- `hue_weight`
- `max_ranked_candidates`
- `return_cells`
- `max_returned_cells`
- `max_returned_matches`
- `template_path`
- `template_paths`

Note: `template_path` / `template_paths` are not used for multi-template classification in `RecoGridEngine`. The business wrapper should load the template directory via `LoadTemplatesFromDirectory()` or `SetTemplates()`.

Scan-specific fields, such as `incremental`, `end_min_match_ratio`, and occupancy filtering thresholds, are not automatically handled by `GridRecognitionRequest`. The business wrapper needs to read them itself.

## Quick Troubleshooting Table

| Problem                                      | First Check                                                             | Common Fix                                                                            |
| -------------------------------------------- | ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| Complete recognition failure on current page | `success/message`, `alignment_status`, `unresolved_reason`, `roi`       | Check ROI and empty input, then whether alignment requested a same-screen recheck     |
| Unstable row/column count                    | `page_rows/page_cols`                                                   | Adjust `roi`, `rowThresholdRatio`, `colThresholdRatio`, `minKeptSegmentRatio`         |
| Many empty cells                             | `page_grid` too large                                                   | Adjust occupancy filtering and mask                                                   |
| Items missed                                 | `page_grid` too small                                                   | Decrease occupancy filtering thresholds, check if mask occludes main body             |
| Many unknowns                                | `unknown`, classification scores                                        | Adjust templates, mask, `maxPhashDistance`, `minScore`                                |
| Many misclassifications                      | `score/templateScore/hueScore`                                          | Increase `minScore`, decrease `maxPhashDistance`, adjust `hueWeight`                  |
| No growth after scrolling                    | `raw_alignment_offset`, `support_rows`, `alignment_status`, `new_cells` | Recheck after stability; inspect scroll direction, support rows, and match thresholds |
| Failure still advances to Swipe              | The wrapper's next-node override                                        | Never override to Swipe on failure                                                    |
| Ends prematurely                             | `reached_end`, `match_ratio`, `support_rows`                            | Increase `endMinMatchRatio`, check scroll distance                                    |
| Does not stop at the end                     | End-page `match_ratio`, `alignment_status`                              | Decrease `endMinMatchRatio`, ensure end repeated page is stable                       |

## Build and Check

Build after modifying C++:

```powershell
cmake --build agent\cpp-algo\build --config RelWithDebInfo --target cpp-algo
```

Installation to run directory is required:

```powershell
cmake --install agent\cpp-algo\build --config RelWithDebInfo
```

It is recommended to run the following before committing:

```powershell
pnpm format
pnpm format:go
pnpm check
pnpm test
```
