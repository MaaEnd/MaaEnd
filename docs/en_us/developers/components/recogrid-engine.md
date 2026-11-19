# RecoGrid / GridTracker / EssenceGrid Architecture

Essence inventory scanning is composed of three C++ modules that can each be understood on their own, instead of one monolithic engine that owns recognition, tracking, and business flow:

```mermaid
flowchart LR
    P["Pipeline screenshot"] --> E["EssenceGrid business instance"]
    E --> R["RecoGrid single-frame recognition"]
    R --> T["GridTracker cross-frame tracking"]
    T --> E
    E --> C["Pipeline cell click"]
    C --> G["Go OCR and match decision"]
    G --> A["Pipeline lock / discard / skip"]
```

## Module boundaries

| Module | Owns | Does not own |
| --- | --- | --- |
| `RecoGrid` | Rows, columns, cell rectangles, occupancy, pHash, and comparison features for one image | Frame history, scroll order, business quality, clicks |
| `GridTracker` | Adjacent-frame alignment, global row/column indices, deduplication, order, and end confirmation | Image segmentation, template loading, Pipeline routing |
| `EssenceGrid` | Essence quality, lock/discard thumbnails, pending queue, and Maa callbacks | Skill OCR, match rules, actual UI actions |
| Go `essencefilter` | OCR normalization, target combination matching, Lock/Discard/Skip decisions | Grid recognition and button clicks |
| Pipeline | Screenshot timing, clicks, swipes, waits, and post-action verification | Image algorithms and match data structures |

More precise module documentation lives next to the source code:

- [RecoGrid README](../../../../agent/cpp-algo/source/RecoGrid/README.md)
- [GridTracker README](../../../../agent/cpp-algo/source/GridTracker/README.md)
- [EssenceGrid README](../../../../agent/cpp-algo/source/EssenceGridScan/README.md)
- [EssenceFilter Go README](../../../../agent/go-service/essencefilter/README.md)

## RecoGrid: one frame only

Public entry point:

```cpp
recogrid::GridFrame RecognizeGrid(
    const cv::Mat& image,
    const recogrid::GridRecognitionOptions& options = {});
```

Recognition performs these steps:

1. map the screenshot to `normalizedSize`;
2. crop `roi` and detect row/column projection segments;
3. construct regular cells and project them back to screenshot coordinates;
4. mask fixed cell decorations;
5. compute occupancy, pHash, and compact color features.

### Main inputs

| Field | Meaning |
| --- | --- |
| `detect.normalizedSize` | Coordinate base; MaaEnd normally uses `1280x720` |
| `detect.roi` | Grid region |
| `rowThresholdRatio` / `colThresholdRatio` | Row/column projection thresholds |
| `minRawSegmentLength` | Minimum raw segment length |
| `minKeptSegmentRatio` | Segment size retained relative to the modal cell size |
| `lockedRowHeight` / `lockedColWidth` | Cell size locked after the first frame to stabilize later detection |
| `mask` | Ignores fixed corner badges or the bottom area inside each cell |
| occupancy brightness options | Decide whether a cell contains content |

### Main outputs

`GridFrame` contains the row and column counts, modal cell width and height, diagnostics, and the `GridCell` list. Each `GridCell` contains its local row and column, current-frame index, screen rectangle, features, and `occupied`.

RecoGrid does not load item-classification templates or keep sessions. An empty image, invalid channels, or no detected grid returns an empty `GridFrame`, and the caller decides how to retry.

## GridTracker: order and end across frames

Public entry point:

```cpp
gridtracker::Result observe(
    const recogrid::GridFrame& frame,
    const gridtracker::Options& options);
```

Each scrolling task holds one `GridTracker`. Call `reset()` when a new task starts.

### Status

| Status | Meaning | EssenceGrid route |
| --- | --- | --- |
| `Initial` | Top confirmed and first frame placed | Process new cells |
| `Advanced` | Additional positions verified | Process new cells |
| `Stalled` | Frame aligned with no additional positions | Try one short swipe, then fail if still stalled |
| `Complete` | Bottom confirmed and unique positions match the inventory count | Drain the queue and verify completion |
| `Rejected` | Origin, grid or overlap could not be verified | Seek the top on entry or scroll back to recover overlap |

### Alignment and deduplication

The controller's first cell, card pitch and scrollbar identify the layout. Complete card contours locate rows, and each column is tested for occupancy. The thumb position bounds the possible absolute row offsets. When the thumb and counter admit exactly one row offset, a complete row of matching cores confirms the overlap. With multiple possible offsets, at least two rows and a unique visual match are required. Ambiguous offsets preserve the last accepted state. Only positions consistent with a densely packed inventory of the displayed size enter the coordinate map.

### Completion conditions

Coverage starts at the verified top and continues across overlapping pages. The adapter records each position when a configured filter excludes it or when its selection is confirmed and detail processing returns successfully. At the bottom it checks the count, current page, bottom position and resolved count again. An inventory that fits on one screen can finish without scrolling.

The vault option “Filter Flawless Essences for all 5- and 6-star weapons and generate a report” and the read-only scan in Target mode both enable `collect_inventory`. Each reads both qualities and all marks in a single traversal. The vault option locks unmarked exact matches. Target mode reads existing marks. Go verifies position coverage before committing. The account file records the quality, mark, skills and levels of each observed Essence with known skills. When Flawless coverage has a completion timestamp, Target mode uses the current weapon data to count eligible locked Essences.

Custom filtering dispatches cells according to the user’s settings. After custom filtering, “Rescan essence inventory before farming” refreshes the current account’s stock from the marks observed in the vault.

## EssenceGrid: the essence business instance

`main.cpp` creates one `EssenceGrid` and registers all of these through `trans_arg`:

- `EssenceGridAdvanceRecognition`;
- `EssenceGridPendingRecognition`;
- `EssenceGridResetAction`.

The instance holds the tracker, task id, merged configuration, thumbnail templates, current page queue, and pending cell. A change of task id resets the tracker and the queue; running `EssenceGridResetAction` from Pipeline clears the same tracking state immediately so the next `EssenceGridAdvanceRecognition` scans from scratch, while loaded templates and configuration stay cached.

### `EssenceGridAdvance.attach`

| Field | Purpose |
| --- | --- |
| `roi`, `normalized_size`, `first_cell`, `row_pitch`, `scrollbar_track` | Locate cards and the scrollbar |
| `preview_core` | Essence symbol in the detail panel |
| `thumb_lock_template_paths` | Recognize locked thumbnails |
| `thumb_discard_template_path` | Recognize discarded thumbnails |
| `flawless_essence` / `pure_essence` | Quality filter |
| `skip_thumb_lock` / `skip_thumb_discard` | Thumbnail state filter |

MaaFramework first dictionary-merges base resources, task options, and controller resources; C++ then reads the final `attach` through `MaaContextGetNodeData`. Do not mirror these fields in Go.

### Queue construction

EssenceGrid processes only tracker `new_cells`:

1. sample the color at the bottom of the cell to judge Flawless/Pure quality;
2. match lock/discard thumbnails in the lower-left cell region;
3. apply the quality and skip options;
4. enqueue accepted cells;
5. Pipeline clicks the center of the pending cell. The scanner compares the pre-click symbol at that coordinate with the detail panel. Four selection corners or changes in the long edges distinguish cells with the same symbol. All four corners use one vertical offset to account for uncertainty in the row position. If the selection is unconfirmed, Pipeline can click the same coordinate once more. Both attempts compare against the original frame. OCR starts after confirmation. The scanner checks the selection and scrollbar again when processing returns.

## Go and Pipeline

After Pipeline clicks a cell, OCR nodes read the three skills and their levels. The Go function `decideSkillMark` chooses the mark to apply.

- target matched: go to `EssenceFilterLockItem`;
- no match and discarding enabled: go to `EssenceFilterDiscardItem`;
- otherwise: return to `EssenceGridAdvance`.

Pipeline owns the actual button recognition, clicking, and `EssenceFilterCheckLocked` / `EssenceFilterCheckDiscarded` verification. Go must not route directly to a post-action check node, because doing so skips the operation.

## Controller differences

The base resource defines Win32 geometry. `resource_adb` and other controller resources should override only the ROIs, templates, and swipe parameters that actually differ.

Lock and discard buttons need separate ROIs. A Pipeline `Click` uses the box returned by template matching; one broad ROI can let a similar template select the neighboring button.

## Troubleshooting

| Symptom | First owner to inspect |
| --- | --- |
| Wrong rows, columns, or cell rectangles | RecoGrid ROI and segment settings |
| Accumulated row misalignment, duplicates, or early finish | GridTracker offset, support, match ratio, and status |
| Wrong quality or lock/discard thumbnail state | EssenceGrid HSV and thumbnail templates |
| Unexpected skill match | Go OCR normalization, data, and match options |
| Wrong or unconfirmed lock/discard click | Pipeline template, ROI, waits, and next order |

## Build

The three modules are ultimately composed by the production `cpp-algo`:

```powershell
cmake --build agent\cpp-algo\build --config RelWithDebInfo --target cpp-algo
```

In CMake, `recogrid` and `grid-tracker` are independent static libraries; `EssenceGrid` is compiled into `cpp-algo`.
