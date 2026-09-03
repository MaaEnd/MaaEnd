# EssenceGrid

`EssenceGridScan/EssenceGrid` is the Essence inventory adapter built on
RecoGrid and GridTracker. It converts Maa screenshots and merged Pipeline
`attach` data into one stateful inventory-scanning instance.

## Ownership

One `EssenceGrid` is created in `main.cpp`. The same instance is passed through
`trans_arg` to both registered custom recognitions:

- `EssenceGridAdvanceRecognition` scans or consumes the current queue and
  chooses the next Pipeline node;
- `EssenceGridPendingRecognition` returns the exact rectangle of the selected
  cell so Pipeline can click it.

The instance owns:

- one `gridtracker::GridTracker`;
- the current task id and merged configuration;
- loaded lock/discard thumbnail templates;
- the latest scan result;
- the current page's Essence cells, dispatch queue, queue cursor, and pending
  cell.

No process-global inventory state or secondary deduplication set is required.

## Pipeline attach

`EssenceGridAdvance.attach` is parsed from `MaaContextGetNodeData`, after
MaaFramework has merged task and controller overrides.

| Field | Owner |
| --- | --- |
| `roi`, `normalized_size`, row/column thresholds, segment filters | RecoGrid geometry |
| `repeat_match_ratio` | GridTracker end confirmation |
| `thumb_lock_template_paths`, `thumb_discard_template_path` | Essence thumbnail state |
| `flawless_essence`, `pure_essence` | Essence quality filter |
| `skip_thumb_lock`, `skip_thumb_discard` | Essence dispatch filter |

Controller resources should override only geometry, templates, and swipe
coordinates that genuinely differ from the base resource.

## Frame processing

1. RecoGrid recognizes one frame.
2. GridTracker aligns it and returns `new_cells`.
3. EssenceGrid samples the bottom strip for gold/purple quality and the
   lower-left cell area for lock/discard thumbnails.
4. Only cells passing the configured quality and thumbnail filters enter the
   dispatch queue.
5. Pipeline clicks the pending cell and runs OCR.
6. Go decides `Lock`, `Discard`, or `Skip`.
7. Pipeline performs and verifies the chosen UI action.

## Routing

| Condition | Next node |
| --- | --- |
| Queue has a cell | `EssenceGridClickPending` |
| Accepted frame needs more scrolling | `EssenceGridSwipeNext` |
| First complete repeat | `EssenceGridSwipeNext` for confirmation |
| Second consecutive complete repeat | `EssenceFilterFinish` |
| Rejected frame | Recognition returns false; Maa retries the current node |

Pipeline owns clicks, swipes, waits, and post-action verification. Go owns OCR
normalization and match decisions. EssenceGrid owns only the inventory-specific
adaptation around RecoGrid and GridTracker.

## Error boundary

Configuration, template-loading, and normal recognition failures return
`MaaBool` failure with diagnostic detail. OpenCV exceptions are caught at the
Maa callback boundary so no exception crosses the C ABI.

## Build target

EssenceGrid is compiled into the production `cpp-algo` executable. It is not a
standalone executable or Pipeline component by itself.
