# RecoGrid Developer Notes

RecoGrid is the grid-recognition and stateful-scroll scan code shared by
`WeaponInventoryScan` and `EssenceGridScan`. It also provides the generic Maa
custom recognition entry registered as `RecoGridRecognition`.

## Runtime Entry Points

- `RecoGridRecognitionRun`: generic custom recognition. It parses
  `custom_recognition_param` into `GridRecognitionRequest`, runs
  `RecognizeGridRequest`, and returns compact match/detail JSON.
- `WeaponInventoryScanRecognitionRun`: weapon inventory scanner. It configures
  `GridScanOptions`, loads weapon icon templates from `data/WeaponIcon/iconbig`
  or `assets/data/WeaponIcon/iconbig`, calls `RecoGridEngine::Scan`, and
  overrides the next pipeline node to swipe or finish.
- `EssenceGridScanRecognitionRun`: essence inventory scanner. It uses the same
  engine for grid transition and placement, then adds essence-specific
  click/inspection state outside RecoGrid.
- `RecoGridEngine`: reusable stateful scan engine. Public API is in
  `RecoGridEngine.h`; keep `GridScanOptions`, `GridScanCell`,
  `GridScanResult`, and method signatures stable unless all callers are updated.

## Engine Model

The engine scans one visible grid page at a time and accumulates a session.

1. `RecognizeGrid` detects row/column segments, builds cell rectangles, and
   computes per-cell pHashes plus compact full-cell color features.
2. Occupancy filtering keeps only cells that look non-empty after applying the
   configured mask.
3. The first page initializes a session and classifies all occupied cells.
4. Later pages compute a row delta against the previous snapshot. The delta
   prefers full-cell color features so colored icons remain distinguishable,
   and falls back to pHash only when features are unavailable.
5. A reliable positive delta is the only normal source of global row progress.
   The engine commits the current frame at
   `previousViewportStartRow + rowOffset`.
6. Placement is a pure projection step. It consumes the resolved viewport start
   row and maps local grid cells to global `(row, col)` cells; it does not score
   offsets or choose between candidates.
7. Row alignment evaluates both positive and negative offsets, requires at
   least two overlapping rows, and keeps the globally best supported candidate.
   A negative winner or tied forward/backward evidence is a direction error; the
   engine never substitutes a weaker positive candidate.
8. An unaccepted transition returns failure without changing the committed
   snapshot, viewport, cells, or end confirmations. The wrapper must leave the
   recognition node on its normal next-list retry path, so Maa rechecks the same
   screen without swiping.
9. The first failed transition stores only a same-screen verification snapshot.
   If the next frame is strongly identical to that snapshot, has the same shape,
   and has at least two rows of support, the engine may commit once with the last
   visually confirmed positive offset. This fallback does not update the trusted
   offset and cannot be used again until a new positive transition is confirmed.
10. End detection is deliberately conservative. A zero-offset match must be
    strong, compare enough cells, and repeat across consecutive confirmations
    before `reachedEnd` is reported. This avoids treating visually similar icon
    pages as a real scroll boundary.
11. The session stores global `(row, col)` cells. If a later visible cell is a
    better classification for the same key, it replaces the old one.

Totals must come from detected visible cells plus session merge. Do not add OCR
total padding/trimming or hard total compensation.

## File Guide

- `GridDetector.*`: image normalization, ROI crop, row/column projection, segment
  filtering, and cell rectangle construction.
- `GridRecognizer.*`: request parsing, generic grid recognition, screen-space
  geometry, and single-template matching entry points.
- `GridClassifier.cpp`: multi-cell classifier for occupied cells. It uses pHash
  candidate filtering followed by normalized direct cell/template comparison.
- `GridMatcher.*`: generic single-template matcher. Its hue scoring is local
  template-match scoring; keep it separate from `GridClassifier.cpp`.
- `GridAlignment.*`: full-cell feature row alignment and `ComputeGridDelta`.
- `PHashFilter.*`: pHash generation, full-cell feature generation, Hamming
  distance, feature distance, and candidate filtering.
- `CellMask.*`: cell mask construction for pHash, classification, matching, and
  occupancy checks.
- `GridGeometry.h`: OpenCV rectangle helpers only. Do not mix JSON output schema
  helpers into this file.
- `RecoGridEngine.*`: public engine methods, template loading, session map, and
  top-level `Scan` orchestration.
- `RecoGridEngineTypes.h`: public scan structs included by `RecoGridEngine.h`.
- `RecoGridSession.*`: committed session state, same-screen verification
  evidence, trusted positive-offset fallback state, merge/replace rules,
  visible-cell hiding, sorted output, counts, and partial-row detection.
- `RecoGridScanCells.*`: occupied-cell detection, scan-cell construction,
  classification application, cell indices, and leading partial-row delta
  adjustment.
- `RecoGridPlacement.*`: pure local-to-global cell projection.
- `RecoGridTransition.*`: direction-aware transition acceptance, bounded
  last-positive-offset fallback, and end detection.

## WeaponInventoryScan Defaults

`WeaponInventoryScan.cpp` owns the production defaults:

- ROI: `[20, 70, 960, 600]` at `1280x720`.
- Row threshold ratio: `0.2`.
- Column threshold ratio: `0.4`.
- Minimum raw segment length: `10`.
- Minimum kept segment ratio: `0.9`.
- pHash distance: `10`.
- Classification score: `0.6`.
- Hue weight: `0.4`.
- End match ratio: `0.95`.

The weapon icon mask ignores weapon UI chrome/rarity/header regions. Keep it
aligned between production code and any temporary debugging runner.

## Algorithm Invariants

- Use the strict row-size strategy: `min_kept_segment_ratio = 0.9`.
- Do not restore leading partial rows into the main grid snapshot.
- Do not merge small split row segments when the merged segment touches the ROI
  boundary; that would restore trailing partial rows as full page rows.
- If `page_rows` flips between 5 and 6, inspect `GridDetector` segment filtering
  before changing alignment thresholds.
- Alignment requires at least two overlapping rows. A one-row match is
  diagnostic evidence only and must never advance the committed viewport.
- Production scrolling is forward-only. Negative alignment is an impossible
  workflow event and must fail for same-screen re-recognition, not be clamped,
  absolutized, or replaced with a weaker positive candidate.
- Repeated or settling frames can produce zero new cells. Do not add a blanket
  "zero growth is illegal" rule; fix detection or placement evidence instead.
- `GridMatcher.cpp` and `GridClassifier.cpp` both score hue, but for different
  algorithms. Do not merge them without preserving local-match versus full-cell
  semantics.

## Pipeline Contract

`assets/resource/pipeline/WeaponInventoryScan.json` calls
`WeaponInventoryScanRecognition` with the same field names used by
`GridRecognitionRequest` plus scan options such as:

- `incremental`
- `end_min_match_ratio`
- `min_kept_segment_ratio`
- `max_phash_distance`
- `min_score`
- `hue_weight`

Pipeline flow should remain state-driven:

- recognize current page,
- an accepted scan lets the engine choose `WeaponInventoryScanSwipeNext` or
  `WeaponInventoryScanFinish`,
- swipe,
- wait for grid freeze,
- recognize again.

If transition recognition fails, the wrapper must not override the next node to
the swipe action. Maa then retries the recognition node from its next list on a
new screenshot. This same-screen retry is part of transition validation, not a
second scrolling engine.

Avoid hard delays unless there is a confirmed reason; prefer freeze waits and
recognition validation.

## Debugging

Useful runtime logs from `WeaponInventoryScan.cpp`:

- `WeaponInventoryScan cumulative grid`: cumulative count, unknown count, rows,
  cols, visible page count, and new cells.
- `WeaponInventoryScan scan delta`: delta reliability, progress, end state, row
  offset, matched/compared cells, match ratio, average distance, and score.
- `WeaponInventoryScan override next`: chosen next pipeline node.

Useful detail fields returned to Maa:

- `page_grid`, `cumulative_grid`, `unknown`
- `rows`, `cols`, `page_rows`, `page_cols`
- `new_cells`, `row_offset`, `current_viewport_start_row`
- `raw_alignment_offset`, `adjusted_alignment_offset`, `support_rows`
- `alignment_status`, `fallback_used`, `fallback_streak`
- `delta_reliable`, `unresolved_reason`
- `dispatchableCells`: committed cells with current-frame coordinates for
  clicking or sampling
- `has_progress`, `reached_end`
- `matched_cells`, `compared_cells`, `match_ratio`

`row_offset` is zero unless the frame was committed; it never exposes a rejected
negative candidate. Use `raw_alignment_offset` and
`adjusted_alignment_offset` to diagnose rejected alignment, and use
`support_rows` plus `alignment_status` to understand why it was rejected.
Stable `alignment_status` values are:

- non-rejection states: `not_applicable`, `accepted`, `fallback_accepted`,
  `end_confirmation`, and `reached_end`;
- rejection states: `grid_missing`, `shape_rejected`, `insufficient_support`,
  `direction_ambiguous`, `direction_violation`, `low_confidence`,
  `zero_unconfirmed`, `no_progress`, `fallback_unavailable`, and
  `fallback_limit_reached`.

Every rejection enters same-screen verification. Because the frame was not
committed, `row_offset` remains zero and the wrapper must not advance to Swipe.
Use `unresolved_reason` for any additional failure detail.

If totals are wrong, inspect per-page `page_rows`, accepted `row_offset`,
alignment diagnostics, viewport start rows, fallback state, and `unknown`.
Production placement is offset-only; do not reintroduce candidate scoring for
global rows.

## Validation

Build the production C++ agent:

```powershell
cmake --build agent\cpp-algo\build --config RelWithDebInfo --target cpp-algo
```

Install if needed:

```powershell
cmake --install agent\cpp-algo\build --config RelWithDebInfo
```

On this machine, sandboxed MSBuild may fail with `FileTracker` access denied.
Rerunning the same build outside the sandbox has worked. A `pwsh.exe is not
recognized` post-build message has been observed and does not necessarily mean
the target failed.

The old `weapon-scan-lab` and screenshot debug hook were temporary dataset
scaffolding and are not part of the production path. If future dataset debugging
needs heavy reports, prefer a temporary standalone runner outside production
logic, keep the marker `TEST SCAFFOLD: WeaponInventoryScan`, and remove the
runner/artifacts after the investigation.
