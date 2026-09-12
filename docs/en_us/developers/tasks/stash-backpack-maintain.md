# Development Manual - Stash and Retrieve Backpack Maintenance

This document describes the state lifecycle and maintenance boundaries of `StashBackpack`, `RetrieveBackpack`, and embedded stashing.
This documentation was last updated on September 9, 2026.

## Supported Scope

- Stashing and retrieval are options of one task, available for `Win32-Front`, `ADB`, and `CloudADB`. Embedded stashing shares the same operations and allows Win32 / Adb controllers as well.
- Both use `InventoryTransferStackAction`. ADB overrides cover item and scrollbar ROIs, the quick-stash region, and preparation before recognition; navigation and categories reuse existing SceneManager support. Four common nodes define upward and downward scrolling for the repository and backpack. Replenishment holds the source item before dragging it onto the matching backpack stack. See the [Inventory contract](../../../../agent/go-service/common/inventory/README.md).
- ADB is open for testing and has not passed device acceptance. Validate inertia and page overlap, hold-to-drag replenishment, recognition after menu closure, consecutive transfers, and cancellation cleanup. CloudADB also needs multitouch validation. Static screenshot checks do not establish workflow stability.
- Pipeline owns business flow, navigation, category switching, and item movement. Go Service encapsulates complete snapshot scans and maintains difference queues and page verification state.

## File Layout

| Path | Purpose |
| --------------------------------------------------------------------- | --------------------------------------------- |
| `assets/tasks/StashBackpack.json` | Combined stash and retrieval options |
| `assets/resource/pipeline/StashBackpack.json` | Main stash flow and embedded entry |
| `assets/resource/pipeline/StashBackpack/Snapshot.json` | Real backpack snapshots |
| `assets/resource/pipeline/StashBackpack/Search.json` | Backpack and Depot paged search |
| `assets/resource/pipeline/StashBackpack/Category.json` | Manual stash category gates |
| `assets/resource/pipeline/StashBackpack/Retrieve.json` | Retrieve flow and category gates |
| `agent/go-service/stashbackpack/` | Snapshots, differences, target queues, and batch state |
| `tools/schema/components/stash_backpack.schema.json` | Custom component parameter contracts |
| `assets/locales/interface/*.json` | Task, Depot, and category labels |

## Snapshot Lifecycle

A snapshot stores a merged list of `item_id`, `category_type`, and reindexed logical `row` / `column` values, together with per-page recognition results for retrieval count baselines. Logical rows and columns preserve ordering; they must not infer empty slots or click coordinates. Input targets come from current-page recognition boxes. Counts represent occupied cells, not stack quantities. Capture real snapshots when the workflow needs actual backpack state; retrieval verification reuses the initial snapshot and updates counts in memory after success.

| Design name | Implementation name | Meaning |
| ----------- | ------------------- | --------------------------------------------- |
| `S0` | `s0` | Backpack after quick stash and before manual stash; quick-stashed items are excluded from retrieval |
| Intermediate | `working` | Backpack after base stashing and before usable-item replenishment |
| `S1` | `s1` | Backpack after the stash task is fully complete |
| `T` | `temporary` | Temporary real snapshot for a host or retrieve task |
| `R1` | `retrieve_current` | Backpack after new-item stashing; reuses `T` if unchanged |

The full stash task publishes usable state only after both `s0` and `s1` have been captured and `complete_full` succeeds. Partial snapshots left by an interrupted run must not be used by retrieve or host tasks. A duplicate full stash task in the same queue prints a red warning and exits successfully to preserve snapshots needed by later tasks.

Retrieval always follows this order:

1. Capture temporary snapshot `T`.
2. Optionally stash `T - S1`, which represents items acquired after the stash task finished.
3. Capture `R1` if the backpack changed; otherwise copy `T`.
4. Retrieve `S0 - R1` from the Depot, gated by the categories selected by the user.

The difference is multiset subtraction that preserves the `S0` grid order. Repeated occurrences of the same `item_id` must not be deduplicated first.

## Stashing Newly Acquired Items

`StoreNewItemsWithStashBackpackSubTask` is called by AutoCollect, AutoEcoFarm, and GiftOperator after they acquire items:

1. Confirm that the controller is Win32 or Adb and that the current batch has a complete `S0/S1` pair. Otherwise, print a red warning and exit successfully without affecting the host task.
2. Enter the batch's selected Depot, optionally quick-stash using the original setting, then capture `T` and prepare `T - S1`. Never overwrite `S0/S1`.
3. Return to the top once before batch stashing. Recognize all remaining target IDs on the current page and transfer them in grid order. After processing the cached page results, recognize again and confirm success by decreased cell counts. Each target gets at most three total attempts, including the first; skip exhausted targets. Finish immediately when the queue is empty, otherwise continue downward.
4. MXU stops the Agent process after all top-level tasks in a batch finish. Process-local Go state is therefore batch-scoped, and top-level tasks in the same batch share the complete snapshots.

The full stash task also records the selected Depot. Retrieval and embedded stash operations in the same batch reuse it instead of asking for another selection.

The Depot source cell may remain after retrieval, so stash verification cannot be reused. Return the backpack to the top once before retrieval and initialize per-page item-ID cell counts from `R1`. After each transfer, recognize the target ID on the current page and require an increase over its baseline. Update the baseline in memory after success so later transfers of the same ID cannot reuse that success. If verification fails on the current page, continue downward; subsequent items start from the page already reached. Do not infer empty cells from row/column continuity or rescan snapshots before every transfer.

## Recognition and Search Constraints

- Snapshot scans use `IconRecognition` with `item_filters: ["Normal:*"]`.
- Backpack batch recognition uses remaining target IDs and `item_recheck_filters: ["Normal:*"]`, preserving multiple cells of the same ID.
- Depot reverse lookup uses the current `item_id` and a concrete `Normal:<Category>` filter.
- Batch stashing and retrieval verification return to the top once at the start, then proceed downward without repeatedly scanning the backpack in both directions for each item.
- Stashing requires decreased target cell counts on the current page; retrieval requires increased cell counts for the target ID. The current retrieval implementation uses the target's `Normal:<Category>` for both candidate and reverse-lookup filters. Neither uses a single-cell ROI for verification.
- Paged scans merge the largest exact overlap between the existing suffix and new-page prefix. This removes adjacent-page overlap while preserving real duplicate items.

## Extending Categories

When adding or changing a category, update all of the following:

1. Manual stash and retrieve checkboxes in `assets/tasks/StashBackpack.json`.
2. Category gates and Depot category-switch nodes in `Category.json` and `Retrieve.json`.
3. The `Category` enum in `stash_backpack.schema.json`.
4. Five-language category labels in `assets/locales/interface/*.json`.
5. The `categoryType` in `IconRecognition` data and its corresponding Depot filter `Normal:<Category>`.

After changes, run `pnpm format`, `pnpm format:go`, `pnpm check`, and `pnpm test`.
