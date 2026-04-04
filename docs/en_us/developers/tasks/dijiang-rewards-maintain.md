# Development Guide - DijiangRewards Maintenance

This document explains the structure of `DijiangRewards`, how its stage tasks work, and how `assets/tasks/DijiangRewards.json` overrides Pipeline behavior through interface options.

## File Overview

| Module | Path | Purpose |
| --- | --- | --- |
| Interface import | `assets/interface.json` | Imports `tasks/DijiangRewards.json` into the `daily` group |
| Task and option definition | `assets/tasks/DijiangRewards.json` | Defines task entry, UI options, child options, and `pipeline_override` |
| Task entry | `assets/resource/pipeline/DijiangRewards/Entry.json` | Enters the Dijiang Control Nexus |
| Main flow | `assets/resource/pipeline/DijiangRewards/MainFlow.json` | Dispatches from the Control Nexus into four stage tasks |
| Mood recovery | `assets/resource/pipeline/DijiangRewards/RecoveryEmotion.json` | Uses friend assist to recover operator mood |
| Reception Room | `assets/resource/pipeline/DijiangRewards/ReceptionRoom.json` | Collects, receives, places, gifts, and exchanges clues |
| Manufacturing Cabin | `assets/resource/pipeline/DijiangRewards/Manufacturing.json` | Claims output, restocks, and uses assist |
| Growth Chamber | `assets/resource/pipeline/DijiangRewards/GrowthChamber.json` | Claims rewards, performs normal grow or grow again, and extracts seeds |
| Shared scene templates | `assets/resource/pipeline/DijiangRewards/Template/Location.json` | Scene-location recognitions |
| Shared text templates | `assets/resource/pipeline/DijiangRewards/Template/TextTemplate.json` | OCR templates for buttons and labels |
| Shared status templates | `assets/resource/pipeline/DijiangRewards/Template/Status.json` | Helper recognitions for red dots, counts, and growth inventory |

## Overall Flow

The task enters the Dijiang Control Nexus first, then `ControlNexus` in `MainFlow.json` tries four stages in order:

1. Mood recovery
2. Reception Room
3. Manufacturing Cabin
4. Growth Chamber

Each stage returns to `InDijiangControlNexus` after finishing. When none of the stage entries match anymore, the task ends.

This design keeps the task modular:

- each stage can be enabled or disabled independently
- stage logic stays local to its own file
- interface options mostly override stage entries or branch nodes, not the main skeleton

## Stage Responsibilities

### 1. Mood Recovery

This stage detects the assist entry in the Control Nexus, uses assist, selects operators whose mood is not full, handles edge prompts, and returns to the Control Nexus.

### 2. Reception Room

This stage handles the Reception Room in roughly this order:

1. handle "clue exchange ended" popups if present
2. collect clues
3. receive clues
4. place or replace clues
5. optionally start clue exchange
6. exit the Reception Room

Clue gifting is not a top-level stage. It is an overflow-handling branch used when clue inventory exceeds the configured threshold.

### 3. Manufacturing Cabin

This stage:

1. claims output
2. restocks
3. uses assist
4. exits the cabin

It has relatively little option-driven behavior compared with the Growth Chamber.

### 4. Growth Chamber

The Growth Chamber is the most option-driven stage in the whole task.

After entering the Growth Chamber, the task first confirms it is on the chamber detail page, then tries:

1. claim mature rewards
2. choose one of two mutually exclusive branches:
   1. normal grow
   2. grow again
3. exit the chamber

Without interface overrides, the default behavior is effectively "claim rewards + normal grow + exit". The grow-again branch is disabled by default and only replaces normal grow when explicitly enabled by the UI option.

The default normal-grow flow is:

1. confirm the chamber detail page
2. claim mature crops if "Claim All" exists
3. enter the target-selection screen if normal grow is allowed
4. inside the target-selection screen:
   1. optionally adjust sorting
   2. search the current page for a valid target
   3. scroll if nothing suitable is found
   4. return if nothing else can be done
5. before clicking a row, require both:
   1. the row name matches the configured target scope
   2. either crop count or seed count is greater than 0
6. after clicking a row, the task goes into one of three mutually exclusive outcomes:
   1. start growing directly
   2. extract seeds first
   3. return to the list if extraction is not allowed

For maintenance purposes, the most important part is still the target-selection flow, because that is where most option overrides take effect.

## Growth Chamber Options

You can treat the Growth Chamber options as:

1. two main decisions:
   1. `SelectToGrow`
   2. `AutoExtractSeed`
2. two supplementary decisions used only in `Any` mode:
   1. `SortBy`
   2. `SortOrder`

### `SelectToGrow`

This is the main strategy switch.

#### `DoNothing`

- disables the normal grow entry
- the task only claims mature rewards, then exits

#### `GrowAgain`

- disables the normal grow entry
- enables the grow-again entry
- bypasses the target-selection list completely

This mode does not use target matching, extraction settings, or sorting.

#### `Any`

- replaces target-name matching with the multilingual list of all growable materials
- keeps the normal grow entry enabled
- exposes `AutoExtractSeed`, `SortBy`, and `SortOrder`

This mode does not mean random choice. It means:

1. optionally reorder the candidate list
2. click the first valid target under the current order

#### Specific material cases

Each specific-material case:

1. narrows name matching to that material only
2. overrides the row-availability recognition to bind more closely to that target row
3. exposes only `AutoExtractSeed`

Sorting is intentionally hidden here because it does not change the final target, only its position in the list.

### `AutoExtractSeed`

This option only matters when the task really enters target selection.

#### `AutoExtractSeed=Yes`

- allows the extraction branch
- disables the direct-return branch

By default, a target row is considered processable if:

- it already has usable seeds
- or it still has crop inventory that can be converted into seeds

Examples:

- `seed=3, crop=0`: valid target, can grow directly
- `seed=0, crop=5`: still valid in this mode, because seeds can be extracted first

So in this mode, the task accepts both "ready to grow now" rows and "can become ready after extraction" rows.

#### `AutoExtractSeed=No`

- tightens the target filter so the row must already have seeds
- disables the extraction branch
- enables the return-to-list branch when extraction is encountered

Examples:

- `seed=0, crop=5`: valid in `AutoExtractSeed=Yes`, but filtered out earlier in `AutoExtractSeed=No`
- if the task still lands on an extraction-entry screen due to recognition jitter or state changes, it immediately returns to the list instead of continuing there

This means `AutoExtractSeed=No` is not just "do not tap Extract". It changes the target-filtering rule from the search stage onward.

### `SortBy`

This option appears only in `SelectToGrow=Any`.

It is supplementary and does not change the main stage structure. Its only role is to adjust how candidate rows are ordered before the task clicks the first valid one.

### `SortOrder`

This option also appears only in `SelectToGrow=Any`.

Like `SortBy`, it only affects candidate order and does not change the main branch structure. It simply decides whether the chosen sort should be ascending or descending.

## Interface Structure

`DijiangRewards` directly exposes 4 top-level options:

- `AutoStartExchange`
- `StageTaskSetting`
- `ClueSetting`
- `SelectToGrow`

The last three are parent options:

- `StageTaskSetting=Yes` expands the four stage toggles
- `ClueSetting=Yes` expands clue gifting controls
- `SelectToGrow=Any` expands extraction and sorting options
- `SelectToGrow=<specific material>` expands only `AutoExtractSeed`

So the UI is intentionally layered: common decisions first, advanced controls only when needed.

## Option Override Logic

### AutoStartExchange

| Config | Overridden node | Override | Reason |
| --- | --- | --- | --- |
| `Yes` | `ReceptionRoomStartExchange` | `enabled=true` | Allow clue exchange inside the base task |
| `No` | `ReceptionRoomStartExchange` | `enabled=false` | Keep clue-exchange timing for the credit-linked task |

### StageTaskSetting and stage toggles

| Option | Overridden node | Override | Reason |
| --- | --- | --- | --- |
| `StageTaskSetting=Yes` | none | expand child options | Hide advanced stage control unless needed |
| `RecoveryEmotionStage` | `RecoveryEmotionMain` | `enabled=true/false` | Control mood recovery stage |
| `ReceptionRoomStage` | `ReceptionRoomMain` | `enabled=true/false` | Control Reception Room stage |
| `ManufacturingStage` | `MFGCabinMain` | `enabled=true/false` | Control Manufacturing Cabin stage |
| `GrowthChamberStage` | `GrowthChamberMain` | `enabled=true/false` | Control Growth Chamber stage |

### ClueSetting, ClueSend, ClueStockLimit

| Option | Overridden node | Override | Reason |
| --- | --- | --- | --- |
| `ClueSetting=Yes` | none | expand child options | Allow custom gifting rules |
| `ClueSetting=No` | `ReceptionRoomSendCluesSelectClues` | `max_hit=3` | Default max send count |
| `ClueSetting=No` | `ClueItemCount` | threshold regex | Default keep-2-per-type behavior |
| `ClueSend` | `ReceptionRoomSendCluesSelectClues` | `max_hit={MaxClueSend}` | Map UI count to node hit count |
| `ClueStockLimit` | `ClueItemCount` | threshold regex | Turn stock limit into actual filter condition |

### SelectToGrow

This is the core Growth Chamber option.

| Case | Main effect |
| --- | --- |
| `DoNothing` | Disable normal grow and only claim rewards |
| `GrowAgain` | Disable normal grow and force the grow-again branch |
| `Any` | Match all growable targets and expose extraction + sorting options |
| Specific material | Match one material only and expose extraction only |

### AutoExtractSeed

| Config | Overridden node | Override | Reason |
| --- | --- | --- | --- |
| `Yes` | `GrowthChamberSeedExtract` | `enabled=true` | Allow extraction branch |
| `Yes` | `GrowthChamberGrowExit` | `enabled=false` | Avoid immediate return when extraction is allowed |
| `No` | `GrowthChamberCheckTargetNotEmpty` | depend only on seed availability | Require seeds to already exist |
| `No` | `GrowthChamberSeedExtract` | `enabled=false` | Disable extraction |
| `No` | `GrowthChamberGrowExit` | `enabled=true` | Return when extraction would be needed |

### SortBy and SortOrder

These only matter in `Any` mode:

- `SortBy` changes the sort category
- `SortOrder` changes ascending vs descending
- neither changes the main branch structure

## Common Synchronization Pitfalls

### 1. When changing stage entries, do not edit only the stage file

Also check:

- `ControlNexus.next` in `MainFlow.json`
- whether `assets/tasks/DijiangRewards.json` needs new stage toggles
- related texts in `assets/locales/interface/*.json`

### 2. When changing default clue behavior, update the hidden advanced branch too

The default clue strategy is written in `ClueSetting=No` overrides. If you only change the child options but forget that branch, expanded and collapsed configurations may behave differently.

### 3. Growth Chamber options are linked

At minimum:

- `SelectToGrow` decides the branch family
- `AutoExtractSeed` decides whether crop-only rows are still acceptable
- `SortBy` / `SortOrder` decide pick order in `Any` mode

If one of them changes, also inspect:

- `GrowthChamberViewIn.next`
- `GrowthChamberFindTarget`
- `GrowthChamberCheckTargetNotEmpty`
- `GrowthChamberSeedExtract`
- `GrowthChamberGrowExit`

### 4. Multilingual text and OCR lists must stay in sync

Changing only UI texts in `assets/locales/interface/*.json` is not enough. If game wording changes, also inspect:

- `Template/Location.json`
- `Template/TextTemplate.json`
- overridden `expected` values in `GrowthChamber.json`

## Recommended Mental Model

Maintain `DijiangRewards` as three layers:

1. **Main flow**: `Entry.json` + `MainFlow.json`
2. **Stage business logic**: the four stage Pipeline files
3. **Interface configuration**: `assets/tasks/DijiangRewards.json`

This makes debugging easier:

- cannot enter the base -> check main flow
- wrong behavior inside a cabin -> check stage logic
- different behavior under different options -> check interface configuration

## Checklist

After changes, at least check:

1. `assets/interface.json` still imports `tasks/DijiangRewards.json`
2. `ControlNexus.next` is still consistent with stage entries
3. modified options are reflected in `assets/locales/interface/*.json`
4. `ClueSetting=No` still matches the intended default clue strategy
5. `SelectToGrow`, `AutoExtractSeed`, `SortBy`, and `SortOrder` still make sense together
6. if you add a new fixed material case, multilingual names and row binding remain stable
