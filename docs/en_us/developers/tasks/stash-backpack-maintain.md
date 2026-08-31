# Development Manual - Stash and Retrieve Backpack Maintenance

This document describes the state lifecycle and maintenance boundaries of `StashBackpack`, `RetrieveBackpack`, and embedded stashing.
This documentation was last updated on August 31, 2026.

## Supported Scope

- The standalone stash and retrieve tasks currently support `Win32-Front` only.
- Stashing and retrieval both use `Shift + Click`. ADB resources retain an override point for `AutoShiftClickAction` and fail explicitly through `FalseAction`, preventing an unsafe fallback to a normal click.
- Pipeline owns UI navigation, category switching, scrolling, and item movement. Go Service only maintains ordered snapshots, difference queues, and the current target.

## File Layout

| Path | Purpose |
| --------------------------------------------------------------------- | --------------------------------------------- |
| `assets/tasks/StashBackpack.json` | Both standalone tasks and their options |
| `assets/resource/pipeline/StashBackpack.json` | Main stash flow and embedded entry |
| `assets/resource/pipeline/StashBackpack/Snapshot.json` | Real backpack snapshots |
| `assets/resource/pipeline/StashBackpack/Search.json` | Backpack and Depot paged search |
| `assets/resource/pipeline/StashBackpack/Category.json` | Manual stash category gates |
| `assets/resource/pipeline/StashBackpack/Retrieve.json` | Retrieve flow and category gates |
| `agent/go-service/stashbackpack/` | Snapshots, differences, target queues, and batch state |
| `tools/schema/components/stash_backpack.schema.json` | Custom component parameter contracts |
| `assets/locales/interface/*.json` | Task, Depot, and category labels |

## Snapshot Lifecycle

A snapshot stores only `item_id`, `category_type`, and logical `row` / `column` values reindexed after page merging. It never stores quantities or screen coordinates. Logical rows and columns preserve stable ordering and must not be used to infer drag coordinates. After each batch of backpack changes, the flow must scroll and create a real snapshot again instead of deriving a new list from movement results.

| Design name | Implementation name | Meaning |
| ----------- | ------------------- | --------------------------------------------- |
| `S0` | `s0` | Backpack before the stash task |
| Intermediate | `working` | Backpack after base stashing and before usable-item replenishment |
| `S1` | `s1` | Backpack after the stash task is fully complete |
| `T` | `temporary` | Temporary real snapshot for a host or retrieve task |
| `R1` | `retrieve_current` | Backpack rescanned after new items are stashed |

The full stash task publishes usable state only after both `s0` and `s1` have been captured and `complete_full` succeeds. Partial snapshots left by an interrupted run must not be used by retrieve or host tasks. A duplicate full stash task in the same queue prints a red warning and exits successfully to preserve snapshots needed by later tasks.

Retrieval always follows this order:

1. Capture temporary snapshot `T`.
2. Optionally stash `T - S1`, which represents items acquired after the stash task finished.
3. Capture a new real snapshot as `R1`.
4. Retrieve `S0 - R1` from the Depot, gated by the categories selected by the user.

The difference is multiset subtraction that preserves the `S0` grid order. Repeated occurrences of the same `item_id` must not be deduplicated first.

## Stashing Newly Acquired Items

`StoreNewItemsWithStashBackpackSubTask` is called by AutoCollect, AutoEcoFarm, and GiftOperator after they acquire items:

1. Confirm that the controller is Win32 and that the current batch has a complete `S0/S1` pair. Otherwise, print a red warning and exit successfully without affecting the host task.
2. Enter the Depot, overwrite temporary snapshot `T`, and always prepare `T - S1`. Never create a baseline or overwrite `S0/S1`.
3. Use `Shift + Click` for each target and recheck only the recorded backpack source cell. Consume the target only after that cell no longer contains the target item; never prove absence by scrolling the whole backpack back and forth.
4. MXU stops the Agent process after all top-level tasks in a batch finish. Process-local Go state is therefore batch-scoped, and top-level tasks in the same batch share the complete snapshots.

The full stash task also records the selected Depot. Retrieval and embedded stash operations in the same batch reuse it instead of asking for another selection.

Retrieval also uses `Shift + Click`, but its Depot source cell may remain when stock is still available, so stash verification cannot be reused. Before each retrieval, the flow finds the first gap in the current backpack page's `row` / `column` sequence. It scrolls down only when the visible 4x5 grid is full, then verifies the current target only inside the recorded empty cell.

## Recognition and Search Constraints

- Snapshot scans use `IconRecognition` with `item_filters: ["Normal:*"]`.
- Backpack reverse lookup uses the current `item_id` and `item_recheck_filters: ["Normal:*"]`.
- Depot reverse lookup uses the current `item_id` and a concrete `Normal:<Category>` filter.
- Consecutive searches start at the current position and continue toward the end. Only after reaching the end without a match does the flow return to the top for a full scan.
- Post-stash verification checks only the clicked source cell; post-retrieval verification checks only the recorded destination cell. Neither path may invoke a full-list absence scan.
- Paged scans merge the largest exact overlap between the existing suffix and new-page prefix. This removes adjacent-page overlap while preserving real duplicate items.

## Extending Categories

When adding or changing a category, update all of the following:

1. Manual stash and retrieve checkboxes in `assets/tasks/StashBackpack.json`.
2. Category gates and Depot category-switch nodes in `Category.json` and `Retrieve.json`.
3. The `Category` enum in `stash_backpack.schema.json`.
4. Five-language category labels in `assets/locales/interface/*.json`.
5. The `categoryType` in `IconRecognition` data and its corresponding Depot filter `Normal:<Category>`.

After changes, run `pnpm format`, `pnpm format:go`, `pnpm check`, and `pnpm test`.
