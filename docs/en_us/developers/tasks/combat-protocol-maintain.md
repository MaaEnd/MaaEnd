# CombatProtocol Cycle Maintenance

This document explains the slot-based rotation, state file and integration points of the "Combat Protocol Cycle" task. Last updated: 2026-10-16.  
Before maintaining it, read the CombatProtocol section in the [Custom actions and recognitions reference](../custom.md).

## Task Overview

`CombatProtocol` (Combat Protocol Cycle) is a container task: **every combat task entry dragged into the container is an independent rotation slot** (several entries of the same task are allowed, e.g. three Protocol Space entries with different configs), and the container rotates among the slots with one of the two modes below. Once included, neither task goes through its own weekday schedule gate; the container enters the slot's main flow directly (`AutoEssenceMain` / `ProtocolSpaceEntry`).

- Sanity-accumulation cycle: each slot has its own threshold (from that entry's own "Cycle threshold" option; multiples of 80 recommended). Every completed run adds 80 or 160 sanity depending on whether the sanity consumption permit was used; when a slot reaches its threshold, its accumulator is cleared, its turn is marked completed, and **the current run ends** — the client (MXU) sees the rotation advanced and automatically starts the next run with the next slot's own configuration. The relay continues until a run finishes without advancing the rotation (sanity ran out, etc.).
- Date-based cycle: one slot per game day (day boundary at 04:00 local time); the day's slot runs until sanity runs out, and the next slot runs the following day, wrapping around after the last one.

Failure handling: a failed run marks the current slot as failed and, in sanity mode, advances to the next slot (in date mode it is only marked, because the calendar drives the rotation); a failure does not trigger the automatic relay. The per-round status of every slot (not run yet / succeeded / failed) is persisted, and is reset once every slot has had its turn.

## State File

The rotation state is stored in `debug/record/CombatProtocol.json` (the slot index is the entry's position in the container):

```json
{
    "schema_version": 2,
    "mode": "sanity",
    "slots": [
        { "task": "ProtocolSpace", "threshold": 240 },
        { "task": "ProtocolSpace", "threshold": 240 },
        { "task": "AutoEssence", "threshold": 480 }
    ],
    "current_index": 2,
    "accum": [0, 0, 320],
    "progress": ["success", "success", "pending"],
    "last_run_date": "2026-10-16",
    "pending_coupon": false,
    "rotated_this_battle": false,
    "updated_at": "2026-10-16T04:00:00Z"
}
```

- `slots` is the slot table written when assembling a run (task and threshold); `accum` / `progress` are parallel arrays indexed like `slots`.
- `current_index` points at the slot about to run (or currently running).
- `progress` is one of `pending` / `success` / `failed`; it is reset as a whole once no slot is `pending`.
- `rotated_this_battle` is a transient in-run flag: it is set when a threshold rotation happens so the pipeline can end the run, and cleared on the next entry.
- A changed slot table or mode, a corrupt file, or an outdated schema resets the rotation; deleting the file resets all progress.

## Options and Configuration Flow

The container task itself only exposes the "rotation mode" option; the included set and rotation order are maintained by MXU's drag-and-drop UI. When assembling a run, MXU:

1. writes one synthetic override putting the slot table into the container entry node's `attach`: `slots` (task names in entry order) and `slot_thresholds` (per-slot thresholds read from each entry's own "Cycle threshold" option value);
2. merges **only the current slot's entry option overrides** (so each slot runs by its own config), with the container's static overrides last (they restore the loop-count caps to unlimited).

Both modes carry the same set of static overrides (only `attach.mode` differs), hooking the following points into the tasks' flows:

| Overridden node | Purpose |
| --- | --- |
| `AutoEssenceOnSuccess` | Enter the accumulator after a completed run |
| `AutoEssenceChooseScalingPre` | Clear the coupon flag before each claim |
| `AutoEssenceChooseScaling2` | Mark the coupon on a double claim |
| `AutoEssenceLowSpFallbackScaling1` | Clear the flag when a double claim falls back to single |
| `ProtocolSpaceRewardClaimSuccessNext` | Enter the accumulator after a completed claim |
| `ProtocolSpaceRewardSelectClaim` | Clear the coupon flag before each claim |
| `ProtocolSpaceRewardUsePermit` | Mark the coupon when the permit is used |
| `ProtocolSpaceRewardFallbackSingle` | Clear the flag when a double claim falls back to single |

After the accumulator, `CombatProtocolStopAfterRotation` (recognition `expect: "rotated"`) runs `StopTask` to end the run once the threshold is reached; otherwise the flow continues with the current slot (Essence Farm via `CombatProtocolRunAutoEssence`, Protocol Space by clicking Continue on the claim screen via `CombatProtocolContinueProtocolSpace`).

## Known Limitations

- The controller list is the intersection of the tasks' support: Essence Farm is not available on ADB / Cloud ADB, so the container is not offered there either.
- Protocol Space's "Target Inventory" (`TargetInventory`) and "do not claim" modes are not offered by the container; it always uses the claim-by-count flow.
- The tasks' own run-count options (`AutoEssenceRepeatCount`, etc.) have no effect inside the container: the container's static overrides restore the loop-count caps to unlimited. The loop is controlled by the slot threshold (sanity mode) or by sanity running out (both modes).
- "One run executes exactly one slot": changing slots changes the configuration, and the run boundary keeps each entry's config from bleeding into the others; the automatic relay is done by MXU, and other clients must start the runs manually.
- With no slot table assembled (a client without container assembly support), the task falls back to the default order: Essence Farm (480) → Protocol Space (240), so it still works out of the box.

## Adding a New Combat Task

1. Register the task identifier in `agent/go-service/combatprotocol/` (the constants and `taskLabelKey` in `state.go`, the default threshold in `options.go`).
2. Provide a "Cycle threshold" input option in the task's own definition (MXU reads the entry's option value and writes it into `slot_thresholds`; the option's own `attach` override is a fallback only).
3. Hook its flow according to the override table above in `assets/tasks/CombatProtocol.json`, and add the corresponding branch nodes in `assets/resource/pipeline/CombatProtocol.json` (enter the main flow, accumulate, continue, coupon marking).
4. Update the `task` / `expect` enums in `tools/schema/` for `CombatProtocolAccumulate` / `CombatProtocolState` and the five-language texts.
