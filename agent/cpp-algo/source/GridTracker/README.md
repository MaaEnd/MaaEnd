# GridTracker

GridTracker is the stateful companion to RecoGrid. It consumes consecutive
`recogrid::GridFrame` values, aligns overlapping rows, assigns stable global
row/column coordinates, and publishes cells that have not appeared at an
earlier global coordinate.

It does not inspect Essence quality, load UI templates, click cells, or control
Pipeline flow.

## Public API

```cpp
gridtracker::Result observe(
    const recogrid::GridFrame& frame,
    const gridtracker::Options& options = {});

void reset();
```

The caller must keep one `GridTracker` instance for one scrolling session and
call `reset()` when a new task starts.

## Status model

| Status | Meaning |
| --- | --- |
| `Initial` | First valid frame; all occupied cells are published as new. |
| `Advanced` | The viewport moved forward, or the visible shape was refreshed without proving the end. |
| `ConfirmingEnd` | First complete zero-offset repeat; the caller must scroll once more. |
| `Repeated` | Second consecutive complete repeat; the caller may treat the list as finished. |
| `Rejected` | Empty, incompatible, or low-confidence frame; committed grid state is not advanced. |

`Result::accepted()` is false only for `Rejected`.

## Alignment and placement

The tracker compares the current frame against non-negative row offsets in the
previous frame. Candidate ordering is:

1. higher matched-cell ratio;
2. lower average feature distance;
3. more compared cells.

A candidate is reliable only when it has enough overlapping rows and reaches
`Options::min_match_ratio`. Accepted positive offsets advance the viewport.
Occupied cells are then placed at `(viewport_start_row + local_row, col)`.

Business code should consume `Result::new_cells`; it must not add another
coordinate-deduplication map.

## End confirmation

A zero offset is not enough to finish. One repeat counts only when:

- both frames have the same row and column shape;
- both contain the complete `rows * cols` cell vector;
- every cell participates in the comparison;
- the match ratio reaches `Options::repeat_match_ratio`.

The first complete repeat returns `ConfirmingEnd`. Only a second consecutive
complete repeat returns `Repeated`. Forward motion, incompatible shape, or an
invalid comparison clears the pending confirmation.

## Result fields

`Result` exposes frame shape, cumulative rows/cells, previous and current
viewport starts, alignment evidence, currently visible occupied cells, and new
cells. `screen_rect` remains the rectangle from the current screenshot, so a
business adapter can click a newly published cell immediately.

## Build target

GridTracker is built as the static CMake target `grid-tracker` and links
`recogrid`.
