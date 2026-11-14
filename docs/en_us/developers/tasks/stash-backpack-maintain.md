# Development Manual - Stash Backpack Maintenance

This document describes the file layout and both stash routes of `StashBackpack`.  
Last updated on November 14, 2026.

Stash Backpack has two modes: quick stash, and stashing only usable items whose count is 4 or less. The second one sorts first and then moves items one at a time, so its node chain is longer and all the known pitfalls live there.

## File paths

- Task definition: `assets/tasks/StashBackpack.json`
- Main flow: `assets/resource/pipeline/StashBackpack.json`
- ADB overrides: `assets/resource_adb/pipeline/StashBackpack.json`

## Main flow

`StashBackpackSubTask` is the entry used when another task runs this one as a sub task; it brings up `StashBackpackMain`.
`StashBackpackMain` tries `StashBackpackEnterSuccess` first, and falls back to `[JumpBack]SceneEnterMenuBackpackWithDepot` to open the backpack. When the area option is set to `ValleyIV` or `Wuling`, that fallback node is replaced by `SceneEnterMenuBackpackWithDepotValleyIV` / `SceneEnterMenuBackpackWithDepotWuling`, so check the actual option before debugging navigation.

Once inside the backpack, `StashBackpackEnterSuccess` runs both modes through SubTask in order, then finishes with `StashBackpackResetUsableItemsLimitedHitCount` to clear the drag counter so the next run still gets a full ten attempts.

Each mode has a wrapper node; the content node ships with `enabled: false` and is turned on by the option:

- Quick stash: `StashBackpackStashQuickSubTask` brings up `StashBackpackStashQuick`
- Usable items ≤4: `StashBackpackUsableItemsLimitedSubTask` brings up `StashBackpackUsableItemsLimited`

Quick stash itself is simple: look for `StashBackpack/QuickStash.png` inside `[871, 472, 162, 125]`, click the best-scoring match, then wait 200 ms for the frame to settle.

## The "usable items ≤4" chain

`StashBackpackUsableItemsLimited` first runs a SubTask that calls `StashBackpackSort` to click the sort button once, then tries these three in order:

1. `StashBackpackUsableItemsStoreFull` — OCR the warehouse-full banner
2. `[JumpBack]StashBackpackUsableItemsLimitedDrag` — find an item with count 0 to 4 and hold Shift
3. `StashBackpackSort` — sort once more as a fallback

`StashBackpackUsableItemsStoreFull` has no `next`, so a successful hit ends the chain right there. The node exists to "stop once the warehouse is full"; when it fails to fire, what is lost is not a hint but the whole exit.

`StashBackpackUsableItemsLimitedDrag` uses `And`: a template match finds the usable-item icon in the backpack area, then OCR reads a single digit at offset `[20, 44, 4, -2]` from that icon. `box_index: 1` selects the digit box. `max_hit: 10` allows at most ten drags per run.

After the drag comes `QuickMoveClick`, which clicks 24 pixels above the drag box (`target_offset: [0, -24, 0, 0]`), then `QuickMoveKeyUp` releases Shift and waits for the frame to settle.

## Two pitfalls

### The sort animation starts later than the freeze check

`StashBackpackSort` uses `post_wait_freezes` with 200 ms on region `[741, 116, 385, 416]`. That waits for the picture to stop changing; it is not a fixed delay.

On some machines the reflow caused by sorting only starts after more than a second. At 200 ms the frame is still the old layout, the freeze check passes early, and the drag that follows reads a pre-sort cell and moves the wrong item.

Pull request 7280 enlarged the region but left the duration alone, so the window is still there.

### The warehouse-full banner is never recognized

`StashBackpackUsableItemsStoreFull` OCRs for "目标空间已满，部分物品未能完全移动" inside roi `[319, 0, 601, 122]`, a strip near the top of the screen.

In the log attached to issue 7284 this node failed on all eleven heartbeats, returning only fragments like `M`, `T`, `CN`, `4`. Without a hit the flow keeps dragging, `max_hit` gets consumed by one item that cannot be stored, and the task falls through to the fallback sort node, reporting success while items are still sitting in the backpack.

## ADB differences

The ADB overlay replaces four nodes with a different implementation:

- `StashBackpackEnterSuccess` does not use `InBackpack`; it confirms the backpack with the `StashBackpack/QuickStash.png` template
- `StashBackpackUsableItemsLimitedDrag` does not press Shift; it uses `TouchDown` on contact 4 with `auto_up`
- `StashBackpackUsableItemsLimitedQuickMoveClick` never clicks a cell; it looks for the transfer-to-warehouse icon and clicks it with `RepeatUntilNotFoundAction`, at most five times
- `StashBackpackUsableItemsLimitedQuickMoveKeyUp` switches to `TouchUp` to release contact 4 instead of releasing Shift

Mind how the third node decides: `repeat_count` is 5 and `interval_ms` is 500. After each click it keeps recognizing that icon for up to 500 ms, and a single miss counts as success; the action only fails when every one of the five windows still sees the icon. In other words, a recognition error, a failed screencap, or a blurry frame is taken as "already moved". When an item did not get moved, count how many times this node fired first.

Check whether ADB has its own copy before tuning parameters, and do not change only one side.

## Debugging

Run nodes individually as described in `docs/en_us/developers/node-testing.md`. Pay attention to:

- which cell the drag recognized — look at the `box` printed by `Recognizer`
- whether `StashBackpackUsableItemsStoreFull` ever logs `Recognition.Succeeded`; if it never does, that exit is dead
- whether it stays first in the `next` list on every heartbeat while failing
- how many times `StashBackpackUsableItemsLimitedDrag` fired — count `Node.Action.Starting` entries in the log to see if `max_hit` ran out early
