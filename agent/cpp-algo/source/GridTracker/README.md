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
    const gridtracker::Options& options);

void reset();
```

The caller must keep one `GridTracker` instance for one scrolling session and
call `reset()` when a new task starts.

## Status model

| Status | Meaning |
| --- | --- |
| `Initial` | First frame anchored at the verified top. |
| `Advanced` | Additional global positions found. |
| `Stalled` | Accepted overlap yielded no new positions. |
| `Complete` | Bottom confirmed and the number of distinct positions matches the displayed count. |
| `Rejected` | Grid, position, occupancy or alignment could not be verified. The previous accepted frame remains available for recovery. |

## Alignment and placement

The scrollbar bounds the possible absolute row positions. The tracker registers each overlapping row and compares every occupied icon core with the same column in the other frame. A forward step with a unique geometric offset can use one full matching row. Otherwise it needs at least two matching rows and a unique visual offset. Competing offsets and a one-column shift provide comparison scores when the scrollbar admits multiple positions. An interval that permits a disjoint viewport leaves the last confirmed position intact.

The tracker validates each visible slot against its expected global index before publishing `new_cells`. A single screen can reach `Complete` immediately. The caller tracks per-item acknowledgements and verifies the final screen before reporting task success.

## Result fields

`Result` exposes frame shape, cumulative rows/cells, previous and current
viewport starts, alignment evidence, currently visible occupied cells, and new
cells. `screen_rect` remains the rectangle from the current screenshot, so a
business adapter can click a newly published cell immediately.

## Build target

GridTracker is built as the static CMake target `grid-tracker` and links
`recogrid`.
