# RecoGrid

RecoGrid is a stateless C++ module that recognizes one regular grid from one
image. It detects row and column geometry, projects cell rectangles back to the
original screenshot, estimates occupancy, and computes comparison features.

RecoGrid does not remember previous frames, infer scroll order, classify
Essence quality, or control Pipeline nodes.

## Public API

```cpp
recogrid::GridFrame RecognizeGrid(
    const cv::Mat& image,
    const recogrid::GridRecognitionOptions& options = {});
```

`GridRecognitionOptions` contains:

- `detect.normalizedSize` and `detect.roi`: the 720p coordinate system and grid
  region;
- row/column projection thresholds and segment-length filters;
- optional locked row/column sizes used to stabilize geometry after the first
  frame;
- `mask`: fixed cell regions excluded from occupancy and feature generation;
- brightness thresholds used to set `GridCell::occupied`.

`GridFrame` contains detected row/column counts, modal cell size, one
`GridCell` per detected position, and a diagnostic message. Every cell carries
its local row/column, frame index, screen rectangle, perceptual hash, compact
color feature, and occupancy flag.

## Typical use

```cpp
recogrid::GridRecognitionOptions options;
options.detect.normalizedSize = { 1280, 720 };
options.detect.roi = { 18, 72, 956, 570 };

const recogrid::GridFrame frame = recogrid::RecognizeGrid(image, options);
if (frame.empty()) {
    // Treat this as recognition failure in the caller.
}
```

Use RecoGrid directly when one frame is sufficient. Compose it with
`gridtracker::GridTracker` when the caller must preserve order and deduplicate
cells across scrolling frames.

## Files

- `GridDetector.*`: normalization, ROI projection, row/column segmentation, and
  cell rectangle construction;
- `GridRecognizer.*`: occupancy evaluation and `GridFrame` construction;
- `CellMask.*`: masks fixed decorations inside a cell;
- `PHashFilter.*`: perceptual hashes and compact full-cell comparison features;
- `GridGeometry.h`: rectangle conversion helpers.

## Failure contract

Invalid or unsupported images return an empty `GridFrame` with a message.
Normal recognition failure is data, not an exception-based control path. The
business adapter decides whether Maa should retry the same node.

## Build target

RecoGrid is built as the static CMake target `recogrid` and is linked by
`grid-tracker` and `cpp-algo`.
