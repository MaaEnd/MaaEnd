# RecoGrid

RecoGrid builds one grid frame from a screenshot and caller-supplied geometry. `EssenceGridScan/InventoryLayout` locates the inventory cards; RecoGrid projects their rectangles back to the screenshot, evaluates occupancy, and samples color tiles for GridTracker.

RecoGrid does not locate rows and columns itself, remember previous frames, classify Essence quality, or control Pipeline nodes.

## Public API

```cpp
recogrid::GridFrame RecognizeGrid(
    const cv::Mat& image,
    const recogrid::GridRecognitionOptions& options,
    const recogrid::GridLayout& detected);
```

`GridLayout` contains the normalized ROI image, row and column segments, and cell rectangles relative to that ROI. The caller supplies all geometry before invoking RecoGrid.

`GridRecognitionOptions` contains:

- `region_.normalized_size_` and `region_.roi_`: the coordinate system and grid region;
- brightness thresholds used to set `GridCell::occupied`.

`GridFrame` contains row/column counts, one `GridCell` per supplied position, a diagnostic message, and color tiles for alignment. Each cell carries its local row/column, frame index, screen rectangle, and occupancy flag. Per-cell hashes and separate comparison features are not computed.

## Files

- `GridGeometry.*`: layout types, normalization and rectangle conversion;
- `GridRecognizer.*`: occupancy evaluation, alignment tiles, and frame construction.

## Failure contract

Empty images or invalid grid geometry return an empty `GridFrame`. The caller owns task failure and recovery decisions.

## Build target

RecoGrid is built as the static CMake target `recogrid` and is linked by `grid-tracker` and `cpp-algo`.
