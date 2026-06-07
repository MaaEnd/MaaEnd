# Weapon Inventory Scan Agent Notes

This note records the current `WeaponInventoryScan` implementation after the inventory-total fix and cleanup pass. It is meant as a handoff for future agents working near `RecoGridEngine`.

## Current Behavior

`WeaponInventoryScanRecognition` scans the visible weapon inventory grid, classifies occupied cells against `data/WeaponIcon/iconbig`, and keeps a per-task cumulative session in `RecoGridEngine`. The pipeline node then overrides its next node at runtime:

- `WeaponInventoryScanSwipeNext` while more rows are expected.
- `WeaponInventoryScanFinish` once the engine confirms the bottom of the inventory.

The scanner defaults are intentionally local to `WeaponInventoryScan.cpp`:

- grid ROI: `[20, 70, 960, 600]` at `1280x720`
- row threshold ratio: `0.2`
- col threshold ratio: `0.4`
- pHash distance: `10`
- match score: `0.6`
- hue weight: `0.4`
- end match ratio: `0.95`

Keep the weapon icon mask. It prevents weapon rarity/header chrome from dominating pHash and occupancy checks.

## End Detection

The important stop condition is in `RecoGridEngine.cpp` when incremental delta is reliable but shows no progress:

```cpp
reachedEnd =
    delta.rowOffset == 0 &&
    !hasNewVisibleKey &&
    (delta.matchRatio >= endMinMatchRatio || HasTrailingPartialRow(...));
```

In plain terms:

- the page did not move by a row offset,
- the current visible grid did not map any new global cell positions,
- and either the page is a high-confidence repeat or the visible grid ends with a partial row.

Do not replace this with a simple "no row offset means end" rule. Normal scroll settling and partial rows can otherwise create premature termination.

## No Total-Count Fallback

Do not fix inventory totals by reading the UI total count and padding or trimming the grid session to match it. That hides recognition errors instead of solving them.

The total reported by `WeaponInventoryScan` must come from actual visible-cell detection, scroll alignment, and cumulative session merging. If a run reports `453` when the UI says `471`, treat it as a grid recognition or scroll-session bug and inspect the per-page grid/delta logs.

## Intentionally Retained Diagnostics

These are production diagnostics, not debug scaffolding. Keep them unless there is a better replacement.

Runtime log lines:

- `WeaponInventoryScan cumulative grid`: reports cumulative count, unknown count, cumulative rows/cols, visible page count, and new cells.
- `WeaponInventoryScan scan delta`: reports delta reliability, progress, end state, row offset, matched/compared cells, match ratio, average distance, and delta score.
- `WeaponInventoryScan override next`: shows whether the pipeline will swipe again or finish.

Recognition detail fields:

- `cumulative_grid`
- `page_grid`
- `unknown`
- `rows`, `cols`
- `page_rows`, `page_cols`
- `new_cells`
- `row_offset`
- `delta_reliable`
- `has_progress`
- `reached_end`
- `matched_cells`
- `compared_cells`
- `match_ratio`

These fields are compact enough for normal logs and are useful when validating user reports.

## Removed Debug Scaffolding

The cleanup removed the one-off investigation support that should not return unless a new debugging session explicitly needs it:

- the `weapon-scan-debug` target and standalone debug runner
- generated `debug/scan/weapon_scan*` report/context files
- screenshot saving parameters and runtime screenshot dumps
- direct OCR total-count fallback and end-of-scan pad/trim normalization
- large per-cell detail output such as `visibleCells`
- delta-candidate dump structures used only for reports
- the separate `WeaponInventoryScanConfig` files that only existed to share defaults with the debug runner

If future debugging needs heavy reports again, prefer keeping the report generator outside the production target, and remove it once the issue is closed.

## Build And Install Note

Build with:

```powershell
cmake --build agent\cpp-algo\build --config RelWithDebInfo
```

Install with:

```powershell
cmake --install agent\cpp-algo\build --config RelWithDebInfo
```

If `install/agent/cpp-algo.exe` is locked, close Maa/MaaPiCli and any running `cpp-algo` process, then rerun the install command. The build output under `agent/cpp-algo/build/bin/RelWithDebInfo/cpp-algo.exe` can be newer than the installed runtime until that succeeds.
