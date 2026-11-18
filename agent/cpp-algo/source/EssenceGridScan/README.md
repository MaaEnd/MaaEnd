# EssenceGrid

`EssenceGridScan/EssenceGrid` is the Essence inventory adapter built on
RecoGrid and GridTracker. It converts Maa screenshots and merged Pipeline
`attach` data into one stateful inventory-scanning instance.

## Ownership

One `EssenceGrid` is created in `main.cpp`. The same instance is passed through
`trans_arg` to the registered custom recognitions and action:

- `EssenceGridAdvanceRecognition` scans or consumes the current queue and
  chooses the next Pipeline node;
- `EssenceGridPendingRecognition` supplies the pending cell's center to the click node and verifies the resulting selection.
- `EssenceGridResetAction` clears the tracking state, so the next advance starts
  a fresh scan. Loaded templates and configuration stay cached.

The instance owns:

- one `gridtracker::GridTracker`;
- the current task id and merged configuration;
- loaded lock/discard thumbnail templates;
- the latest scan result;
- the current page's Essence cells, dispatch queue, queue cursor, and pending
  cell.

The instance also records the starting inventory count and the global coordinates resolved by filtering or by successful item processing.

## Pipeline attach

`EssenceGridAdvance.attach` is parsed from `MaaContextGetNodeData`, after
MaaFramework has merged task and controller overrides.

| Field | Owner |
| --- | --- |
| `roi`, `normalized_size`, `first_cell`, `row_pitch`, `scrollbar_track` | Controller-specific layout and position |
| `preview_core` | Symbol in the right-hand detail panel |
| `thumb_lock_template_paths`, `thumb_discard_template_path` | Essence thumbnail state |
| `flawless_essence`, `pure_essence` | Essence quality filter |
| `skip_thumb_lock`, `skip_thumb_discard` | Essence dispatch filter |

Controller resources should override only geometry, templates, and swipe
coordinates that genuinely differ from the base resource.

## Frame processing

1. Read the displayed inventory count. Locate complete cards at the configured pitch and pass their grid to RecoGrid for occupancy and features.
2. GridTracker aligns it and returns `new_cells`.
3. EssenceGrid samples the bottom strip for gold/purple quality and the
   lower-left cell area for lock/discard thumbnails.
4. Direct callers receive cells passing the configured quality and thumbnail
   filters. A warehouse scan opts into `collect_inventory` and dispatches every
   occupied cell. `selected_for_filter` preserves the user's marking policy.
5. Pipeline clicks the pending coordinate. EssenceGrid compares its pre-click symbol with the detail panel. Cells with the same symbol need spatial evidence from the selection marks or the change in card edges. OCR starts once selection is confirmed.
6. Go decides `Lock`, `Discard`, or `Skip`.
7. Pipeline performs and verifies the chosen UI action. The scanner acknowledges that coordinate after checking the selection, the page icons and the scrollbar again.

## Routing

| Condition | Next node |
| --- | --- |
| Queue has a cell | `EssenceGridClickPending` |
| Accepted frame needs more scrolling | `EssenceGridSwipeNext` |
| Entry away from the top | `EssenceGridSwipeToTop` while the thumb moves upwards |
| Accepted page with no additional positions | `EssenceGridSwipeShort` once at the current coverage |
| Lost overlap | `EssenceGridSwipeBack` once at the current coverage |
| Bottom and all positions resolved | `EssenceGridVerifyComplete` checks a fresh screenshot before `EssenceFilterFinish` |
| Unresolved coverage | Fail the scan |

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
