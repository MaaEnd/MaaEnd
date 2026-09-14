# Development Manual - Stash and Retrieve Backpack Maintenance

This document describes the state lifecycle and maintenance boundaries of `StashBackpack`, `RetrieveBackpack`, and embedded stashing.
This documentation was last updated on September 13, 2026.

## Supported Scope

- Stashing and retrieval are options of one task, available for `Win32-Front`, `ADB`, and `CloudADB`. Embedded stashing shares the same operations and allows Win32 / Adb controllers as well.
- Both use `InventoryTransferStackAction`. ADB overrides cover item and scrollbar ROIs, the quick-stash region, and preparation before recognition; navigation and categories reuse existing SceneManager support. Four common nodes define upward and downward scrolling for the repository and backpack. Replenishment holds the source item before dragging it onto the matching backpack stack. See the [Inventory contract](../../../../agent/go-service/common/inventory/README.md).
- Replenishment searches the backpack first. A stack-count OCR match of `50` skips that target; otherwise the flow searches the repository and drags the matching item into the backpack stack.
- ADB is open for testing and has not passed device acceptance. Validate inertia and page overlap, hold-to-drag replenishment, recognition after menu closure, consecutive transfers, and cancellation cleanup. CloudADB also needs multitouch validation. Static screenshot checks do not establish workflow stability.
- Pipeline owns business flow, navigation, category switching, and item movement. Go Service encapsulates complete snapshot scans, the stored-items record, derived snapshots, and target queues.

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

A snapshot stores a merged list of `item_id`, `category_type`, and reindexed logical `row` / `column` values, together with per-page recognition results. Logical rows and columns preserve ordering; they must not infer empty slots or click coordinates. Input targets come from current-page recognition boxes. Counts represent occupied cells, not stack quantities. Capture real snapshots when the workflow needs actual backpack state. Manual stashing keeps the books by deducting the `working` snapshot at every confirmed store, so no second scan or derivation step exists after stashing.

| Design name | Implementation name | Meaning |
| ----------- | ------------------- | --------------------------------------------- |
| `S0` | `s0` | Backpack after quick stash and before manual stash; quick-stashed items are excluded from retrieval |
| Intermediate | `working` | Established as a copy of `S0` when it is captured and deducted as each store is confirmed; backpack before usable-item replenishment |
| `S1` | `s1` | Backpack after the stash task is fully complete; copied from `working` (replenishment changes counts only, not cells) |
| `T` | `temporary` | Temporary real snapshot for a host or retrieve task |

The full stash task publishes usable state only after `working` and `s1` exist and `complete_full` succeeds. Partial snapshots left by an interrupted run must not be used by retrieve or host tasks. A duplicate full stash task in the same queue prints a red warning and exits successfully to preserve snapshots and the stored-items record needed by later tasks.

Retrieval is based on the **stored-items record**, not snapshot differences:

1. Every confirmed manual stash (recognized by the `manual_item_stored` reason) appends to the record. Quick stash, new-item stashing, and replenishment never do.
2. When the retrieve task starts and new-item stashing is disabled while the record is empty, it finishes immediately without entering the Depot or scanning the backpack.
3. Only when new-item stashing is enabled is temporary snapshot `T` captured, optionally stashing `T - S1`. Newly stashed items stay in the Depot and never take part in retrieval.
4. The stored record becomes the retrieval target queue. Items are retrieved from the Depot in record order, gated by the categories selected by the user.

The record lives exactly as long as the stash/retrieve pair: the entry guard rejects duplicate stashes, and an aborted retrieval writes remaining targets back into the record. The record is batch-scoped and disappears with the Agent state when the batch ends. The retrieve entry admits a non-empty record even when the previous stash was interrupted without complete snapshots; complete snapshots are only the prerequisite of new-item stashing, which is skipped with a warning when they are missing.

## Stashing Newly Acquired Items

`StoreNewItemsWithStashBackpackSubTask` is called by AutoCollect, AutoEcoFarm, and GiftOperator after they acquire items:

1. Confirm that the controller is Win32 or Adb and that the current batch has a complete `S0/S1` pair. Otherwise, print a red warning and exit successfully without affecting the host task.
2. Enter the batch's selected Depot, optionally quick-stash using the original setting, then capture `T` and prepare `T - S1`. Never overwrite `S0/S1`.
3. Return to the top once before batch stashing. Recognize all remaining target IDs on the current page and transfer them in grid order. After processing the cached page results, recognize again and confirm success by decreased cell counts. Each target gets at most three total attempts, including the first; skip exhausted targets. Finish immediately when the queue is empty, otherwise continue downward.
4. Items confirmed by new-item stashing are never appended to the stored-items record; by design they stay in the Depot.

The full stash task also records the selected Depot. Retrieval and embedded stash operations in the same batch reuse it instead of asking for another selection.

Retrieval verification uses a backpack-side count check: one `retrieve_current` snapshot is captured before retrieval, then the bag is returned to the top and searched downward. Retrieval succeeds only when the current page contains more cells of the target item than its pre-retrieval baseline; the page baseline is updated after success so repeated same-item targets cannot be confirmed by the first transfer. A failed transfer or reaching the bottom keeps the remaining records for a later run.

## Recognition and Search Constraints

- Snapshot scans use `IconRecognition` with `item_filters: ["Normal:*"]`.
- Backpack batch recognition uses remaining target IDs and `item_recheck_filters: ["Normal:*"]`, preserving multiple cells of the same ID.
- Depot reverse lookup uses the current `item_id` and a concrete `Normal:<Category>` filter.
- Batch stashing returns to the top once at the start, then proceeds downward without repeatedly scanning the backpack in both directions for each item.
- Stashing requires decreased target cell counts on the current page. Retrieval records a per-page cell-count baseline before each transfer and requires the count to drop after it; it does not rely on backpack page scans.
- Paged scans merge the largest exact overlap between the existing suffix and new-page prefix. This removes adjacent-page overlap while preserving real duplicate items.

## Extending Categories

When adding or changing a category, update all of the following:

1. Manual stash and retrieve checkboxes in `assets/tasks/StashBackpack.json`.
2. Category gates and Depot category-switch nodes in `Category.json` and `Retrieve.json`.
3. The `Category` enum in `stash_backpack.schema.json`.
4. Five-language category labels in `assets/locales/interface/*.json`.
5. The `categoryType` in `IconRecognition` data and its corresponding Depot filter `Normal:<Category>`.

After changes, run `pnpm format`, `pnpm format:go`, `pnpm check`, and `pnpm test`.
