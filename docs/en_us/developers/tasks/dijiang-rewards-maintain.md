# Developer Guide — Base Facility Task Maintenance

This document explains the overall structure of `DijiangRewards` (the base facility task), the responsibilities of its four phase tasks, and how each `interface` option in `assets/tasks/DijiangRewards.json` overrides Pipeline behavior and why—so future maintenance and extensions are easier.  
This document was last updated on April 7, 2026, and is aligned with [fix(GrowthChamber): fix "Plant again" button sometimes being ignored (#2003)](https://github.com/MaaEnd/MaaEnd/pull/2003).

## File overview

The current implementation is spread across these files:

| Module                       | Path                                                                 | Role                                                                                              |
| ---------------------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| Project interface hook       | `assets/interface.json`                                              | Mounts `tasks/DijiangRewards.json` under the `daily` task group                                   |
| Task and option definitions  | `assets/tasks/DijiangRewards.json`                                   | Defines task entry, UI options, sub-options, and `pipeline_override`                              |
| Task entry                   | `assets/resource/pipeline/DijiangRewards/Entry.json`                 | Enters Dijiang Control Nexus from the task entry                                                  |
| Main flow dispatch           | `assets/resource/pipeline/DijiangRewards/MainFlow.json`              | Dispatches from Control Nexus into four sub-phases in order                                       |
| Mood recovery                | `assets/resource/pipeline/DijiangRewards/RecoveryEmotion.json`       | Handles friend-assist mood recovery in Control Nexus                                              |
| Reception room               | `assets/resource/pipeline/DijiangRewards/ReceptionRoom.json`         | Handles clue collection, receipt, placement, gifting, and clue exchange                           |
| Manufacturing bay            | `assets/resource/pipeline/DijiangRewards/Manufacturing.json`         | Handles harvest, restock, and assists                                                             |
| Growth chamber               | `assets/resource/pipeline/DijiangRewards/GrowthChamber.json`         | Handles claim, the post-reward "plant again" branch, normal cultivation, and seed core extraction |
| Shared location template     | `assets/resource/pipeline/DijiangRewards/Template/Location.json`     | Maintains location nodes for each bay UI                                                          |
| Shared text template         | `assets/resource/pipeline/DijiangRewards/Template/TextTemplate.json` | Maintains OCR templates for button/state text                                                     |
| Supplemental status template | `assets/resource/pipeline/DijiangRewards/Template/Status.json`       | Maintains auxiliary recognition for red dots, counts, cultivation stock, etc.                     |

## Overall execution logic

The task entry is `DijiangRewards` in `Entry.json`:

1. Enter Dijiang Control Nexus via `SceneEnterMenuDijiangControlNexus`.
2. After hitting `ControlNexus` in `MainFlow.json`, try the four phases in `next` order:
    1. `[JumpBack]RecoveryEmotionMain`
    2. `[JumpBack]ReceptionRoomMain`
    3. `[JumpBack]MFGCabinMain`
    4. `[JumpBack]GrowthChamberMain`
3. After each phase completes, return to `InDijiangControlNexus`, then check the next phase.
4. When none of the four phases match anymore, go to `FinishDijiangRewards` to finish.

The design centers on **Control Nexus dispatch + per-phase jump-back**:

- The four phases are independent, so they can be enabled, disabled, and maintained separately.
- Each phase only cares about **how to enter its bay, finish that bay’s logic, and return to Control Nexus**.
- `interface` options only need to override each phase’s entry or branch nodes; the main flow skeleton does not change.

## Responsibilities of the four phases

### 1. Mood recovery

`RecoveryEmotionMain` enters the assist UI from Control Nexus when the "needs assist" red dot is recognized:

- Tap "Use assist" first.
- On the "select an Operator whose mood to restore" screen, find an Operator whose mood bar has an empty slot.
- Handle cleanup when "Operator mood is full" or "no more mood boost points".
- Finally return to Control Nexus.

This is essentially a phase that **spends assist points from Control Nexus**.

### 2. Reception room

After `ReceptionRoomMain` enters the reception room, the core order is:

1. Fallback-handle the "intel exchange ended" popup first.
2. In `ReceptionRoomViewIn`, try in order:
    1. Collect clues
    2. Receive clues
    3. Place/replace clues
    4. Start clue exchange
    5. Leave the reception room

"Gift clues" is not a top-level phase; it is a branch when clues overflow:

- When clue stock is full, enter `ReceptionRoomSendCluesEntry`.
- Use `ClueItem` + `ClueItemCount` to find clues that meet the threshold.
- Complete gifting via missing friend color or a direct send button.

### 3. Manufacturing bay

After `MFGCabinMain` enters the manufacturing bay, `MFGCabinViewIn` tries in order:

1. Claim output
2. Restock
3. Use assist
4. Leave the manufacturing bay

There are no complex option overrides here; maintenance cost is mostly button recognition and exit stability.

### 4. Growth chamber

After entering the growth chamber, the task first confirms it is on the growth chamber detail screen (`GrowthChamberMain` → `GrowthChamberViewIn`), then tries in order:

1. Claim cultivation rewards
2. If `GrowAgain` is enabled in the current config, try "Plant again" after the reward UI closes
3. Otherwise enter normal cultivation, or leave the growth chamber directly

The growth chamber is the phase that depends most on `interface` overrides. Its base skeleton is not very complex, but much behavior is **not** hard-coded in `GrowthChamber.json`; it is rewritten at runtime by `assets/tasks/DijiangRewards.json`.

If you ignore UI options and only look at the default Pipeline, the default growth chamber behavior is **claim + normal cultivation + exit**; the "plant again" branch is off by default (`GrowthChamberGrowAgain` defaults to `enabled=false`), and only when explicitly turned on by `interface` does it insert one attempt after the reward UI closes. Given that, the default flow breaks down as:

1. Confirm you are back on the growth chamber detail page (`GrowthChamberViewIn`).
2. If "Claim all" appears on screen, claim crops first (`GrowthChamberClaimReward`).
3. After claiming, close the reward UI (`GrowthChamberClaimRewardClose`); only in `GrowAgain` mode does execution continue to `GrowthChamberGrowAgain` to try the "Plant again" button.
4. If normal cultivation is allowed by config and the "Cultivate" button can be recognized, enter material selection (`GrowthChamberGrow`). This is not batch cultivation; it picks one of nine cultivation targets to process.
5. After entering material selection, loop in this order (`GrowthChamberGrowViewIn`):
    1. Adjust sort mode or sort direction if needed (`GrowthChamberSortBy`, `GrowthChamberSortOrder`)
    2. Find a matching target in the current list (`GrowthChamberFindTarget`)
    3. If not found on this screen, scroll down one screen (`GrowthChamberTargetNotFound`)
    4. If nothing actionable remains, return to the growth chamber detail page (`GrowthChamberReturn`)
6. Before actually tapping, the task confirms the row satisfies both: the name matches the configured target range, and either "crop count" or "seed core count" on that row is greater than zero (`GrowthChamberSelectTarget` + `GrowthChamberCheckTargetNotEmpty`; the latter defaults to `GrowthChamberCheckSeedNotEmpty` OR `GrowthChamberCheckPlantNotEmpty`).
7. After tapping a target, one of three mutually exclusive outcomes follows:
    1. If cultivation can start immediately, confirm start (`GrowthChamberGrowConfirm`)
    2. If seed cores must be topped up first, branch to "go extract seed cores" (`GrowthChamberSeedExtract`)
    3. If extraction is not allowed by config, return to the list (`GrowthChamberGrowExit`)
8. After one round finishes, return to the growth chamber detail or material selection and decide if more actions remain (back to `GrowthChamberViewIn` or `GrowthChamberGrowViewIn`).

Steps 4–6 need the most maintenance attention, because **who turns sorting on, what target to find, when tapping in is allowed, and whether to extract seed cores after** are almost entirely decided by `interface` options.

You can think of growth chamber `interface` options as **two main decisions + two supplemental decisions**:

1. `SelectToGrow`: whether to cultivate at all, and whom to cultivate.
2. `AutoExtractSeed`: when the target lacks seed cores, whether the extract branch is allowed.
3. `SortBy`: in "any material" mode only, how candidate rows are ordered.
4. `SortOrder`: in "any material" mode only, sort direction.

The following sections map UI options to actual behavior.

#### `SelectToGrow` sets the "cultivation direction"

This is the growth chamber’s main switch. It decides whether, after entering the growth chamber, you **only claim rewards, use post-reward plant-again, or enter normal material-selection cultivation**.

##### `SelectToGrow=DoNothing`

Actual behavior:

- Via `pipeline_override`, disable the "Cultivate" entry (`GrowthChamberGrow.enabled=false`).
- The task only handles mature rewards and no longer enters material selection.
- After rewards are claimed, the only meaningful actions left in this phase are mostly exit.

So this option corresponds to **claim mature rewards only; do no new cultivation**.

##### `SelectToGrow=GrowAgain`

Actual behavior:

- Disable the normal cultivation entry into material selection (`GrowthChamberGrow.enabled=false`).
- Enable the "Plant again" entry (`GrowthChamberGrowAgain.enabled=true`).

In practice, on the growth chamber detail page you no longer take the "pick materials" chain; instead, after closing rewards you preferentially try:

1. Match the "Plant again" button
2. After tap, enter plant-again confirmation
3. After confirming, return straight to the growth chamber main screen

One detail that is easy to miss after this update:

- `GrowthChamberGrowAgain` is no longer listed directly on `GrowthChamberViewIn.next`; it is triggered from `GrowthChamberClaimRewardClose.next`.
- So `GrowAgain` mode prioritizes **plant again immediately after claiming rewards**, not unconditionally tapping "Plant again" from the detail page.

This mode bypasses the material list entirely, so:

- "Target name filtering" (`GrowthChamberSelectTarget`) is not used
- `AutoExtractSeed` is not used
- `SortBy` and `SortOrder` are not used

##### `SelectToGrow=Any`

This is the default mode and the most complex.

Actual behavior:

- Override "matchable target names" to the full multilingual list of cultivatable materials (`GrowthChamberSelectTarget.expected`).
- Keep the normal cultivation branch into material selection (`GrowthChamberGrow`).
- Expose three sub-options to the user: `AutoExtractSeed`, `SortBy`, `SortOrder`.

So in `GrowthChamberGrowViewIn`:

1. You can adjust list order first.
2. Then find the best-matching target on the current screen from the **full candidate set**.
3. After finding one, verify that row still has usable stock.

Here "any" does **not** mean random tapping:

- Use `SortBy` / `SortOrder` to change list order.
- Then tap the first viable target under the current sort (`GrowthChamberFindTarget`).

When maintaining, remember that `Any` mode’s final behavior depends on **both** the candidate set and the sort configuration.

##### `SelectToGrow=<specific material>`

Examples: `Wulingstone`, `Igneosite`, `FalseAggela`, etc. The pattern is the same:

1. Narrow "target name filter" to that material’s multilingual names (`GrowthChamberSelectTarget.expected`).
2. Tighten "whether this row can still be processed" to recognition closer to that row (override `GrowthChamberCheckSeedNotEmpty.recognition`).
3. Only expose `AutoExtractSeed` to the user; do not show sort options.

In practice:

- After entering material selection, you no longer "pick a suitable row from all candidates"; you **only look for this name**.
- Once the target row is found, all logic revolves around whether cultivation can continue for that one target.

`SortBy` and `SortOrder` are omitted by design, not by oversight:

- Fixed-material semantics are "find this material and process it".
- Sort only affects where it appears in the list, not **which** material is the goal.
- So sort is not part of the business semantics and does not need to be exposed.

#### `AutoExtractSeed` decides "whether targets short on seed cores are acceptable"

This option only appears when `SelectToGrow=Any` or a specific material, because those modes actually enter material selection.

##### `AutoExtractSeed=Yes`

Actual behavior:

- Enable the "go extract seed cores" branch (`GrowthChamberSeedExtract.enabled=true`).
- Disable the branch that returns to the list when extraction is shown (`GrowthChamberGrowExit.enabled=false`).
- Recognition conditions are the same; only the post-hit flow differs.

Under default recognition, "whether this target row is processable" (`GrowthChamberCheckTargetNotEmpty`) means:

- This row already has usable seed cores, **or**
- This row still has crop bodies that can be converted to seed cores (`GrowthChamberCheckSeedNotEmpty` / `GrowthChamberCheckPlantNotEmpty`)

Two intuitive examples:

- Example 1: A row shows seed core count `3` and crop body count `0`. It can cultivate directly and counts as processable.
- Example 2: Seed cores `0`, crop bodies `5`. It cannot cultivate yet, but extraction is still possible, so with `AutoExtractSeed=Yes` it still counts as processable.

So default logic does not only look for rows that "can cultivate immediately"; it accepts:

- Rows that already have seed cores and can cultivate directly
- Rows that only have crop stock but can extract seed cores first

Therefore when `AutoExtractSeed=Yes`, after tapping a candidate the real actions are:

1. If this row already has usable seed cores, confirm cultivation (`GrowthChamberGrowConfirm`).
2. If only bodies exist and seed cores must be topped up, enter extraction (`GrowthChamberSeedExtract`).
3. After extraction, close reward UI and return to material selection (`GrowthChamberSeedExtractClose`, `GrowthChamberGrowBack`).
4. Continue the next search or cultivation

This effectively allows **body-to-seed-core** topping-up behavior.

##### `AutoExtractSeed=No`

Actual behavior:

- Tighten "processable row" to **must already have seed cores** (`GrowthChamberCheckTargetNotEmpty` depends only on `GrowthChamberCheckSeedNotEmpty`).
- Disable the extract branch (`GrowthChamberSeedExtract.enabled=false`).
- Enable the branch that returns to the list when extraction UI appears (`GrowthChamberGrowExit.enabled=true`).

These three overrides must be read together:

1. At search time, exclude targets that **only have bodies and no seed cores**.  
   Example: a row shows seed cores `0`, bodies `5`—with `AutoExtractSeed=Yes` it can still be processable; with `AutoExtractSeed=No` it is filtered out during search.
2. Even if tapping leads to the extraction entry, the extract branch must not be taken.  
   Example: due to recognition noise or UI state, you still land on "go extract seed cores"; the task must not continue tapping "Extract"—that path is treated as non-executable.
3. If the extraction entry still appears, return to the list immediately (`GrowthChamberGrowExit`).  
   Example: you intended a row that "already has seed cores", but tapping opened extraction; the task returns to the material list and looks for the next target instead of spending time on the current one.  
   This task acts as a **fallback**: in some cases repeated plant taps return straight to the growth chamber main screen; this node is needed to guide the flow back.

So this option is not merely "do not tap extract after finding a target"; it tries to **exclude targets that require seed core extraction from the filter conditions** from the start.

#### `SortBy` decides "in Any mode, in what order targets are picked"

This option only appears when `SelectToGrow=Any`. It is supplemental and does not drive main growth-chamber branching.

Its role is simple:

- After entering material selection, switch sort mode if needed (`GrowthChamberSortBy`, `GrowthChamberSortByChoose`).
- Sort mode only affects candidate order in "any material" mode.
- It does not change whether the flow is claim / normal cultivation / plant again / exit; it only affects which candidate is easier to hit first.

#### `SortOrder` decides "sort direction"

`SortOrder` also only appears when `SelectToGrow=Any`, as a supplement to sorting.

It:

- After sort mode is set, chooses ascending vs descending list order (`GrowthChamberSortOrder`).
- Like `SortBy`, it only affects candidate order, not main flow structure.
- For maintenance: the task taps the direction toggle once when the current direction does not match the user’s setting.

#### Putting the four options together

If you chain actual growth chamber behavior by `interface` config, the mental model is:

1. `SelectToGrow` first chooses:
    1. No cultivation
    2. Plant again
    3. Normal cultivation
2. If normal cultivation:
    1. Any-target cultivation
    2. Fixed-material cultivation
3. If normal cultivation:
    1. In `Any` mode, use `SortBy` + `SortOrder` for list order
    2. Use target name matching (`GrowthChamberSelectTarget`) to decide **whom** to find
    3. Use `AutoExtractSeed` to decide whether "only seed cores count as processable" vs "crop bodies are OK and extraction may follow"
4. After a target is found:
    1. If direct cultivation is possible, confirm
    2. If extraction is needed and allowed, take the extract branch
    3. If extraction is needed but not allowed, return to the list

When debugging growth chamber issues, ask three questions first:

1. Which mode is current `SelectToGrow`?
2. Is there a sort override in this mode?
3. Does `AutoExtractSeed` change candidate filtering in this mode?

If those three are clear, most questions like "why this material was tapped", "why it didn’t extract", or "why cultivation didn’t start" can be traced along the node graph.

## `interface` option structure

`DijiangRewards` exposes only four top-level options at the task layer:

- `AutoStartExchange`
- `StageTaskSetting`
- `ClueSetting`
- `SelectToGrow`

The last three are "parent" options:

- When `StageTaskSetting=Yes`, four phase toggles are shown.
- When `ClueSetting=Yes`, clue send count and stock threshold are shown.
- When `SelectToGrow=Any`, seed-core extraction and sort-related options are shown.
- When `SelectToGrow=<specific material>`, only `AutoExtractSeed` is shown.

So the task’s `interface` design does not flatten all settings; it exposes high-frequency decisions first, then expands advanced items as needed.

## Option overrides and rationale

Below, each option is explained by **what it does** and **why**.

### AutoStartExchange

| Setting | Overridden node              | Override        | Rationale                                                                               |
| ------- | ---------------------------- | --------------- | --------------------------------------------------------------------------------------- |
| `Yes`   | `ReceptionRoomStartExchange` | `enabled=true`  | Allows the base task to start clue exchange directly inside the reception room          |
| `No`    | `ReceptionRoomStartExchange` | `enabled=false` | By default do not start exchange proactively; leave timing to the LMD credit chain task |

Rationale:

- Clue exchange consumes reception room state and is tightly coupled to the LMD credit path.
- Default `No` leaves "start exchange" to higher-value credit scenarios.
- This option only toggles `enabled` because it controls **whether** to do it, not **how** to recognize it.

### StageTaskSetting and the four phase toggles

| Option                 | Overridden node           | Override                 | Rationale                                                          |
| ---------------------- | ------------------------- | ------------------------ | ------------------------------------------------------------------ |
| `StageTaskSetting=Yes` | None directly in Pipeline | Only expands sub-options | Folds "advanced phase control" so casual users are not overwhelmed |
| `RecoveryEmotionStage` | `RecoveryEmotionMain`     | `enabled=true/false`     | Whether to run the mood recovery phase                             |
| `ReceptionRoomStage`   | `ReceptionRoomMain`       | `enabled=true/false`     | Whether to run the reception room phase                            |
| `ManufacturingStage`   | `MFGCabinMain`            | `enabled=true/false`     | Whether to run the manufacturing bay phase                         |
| `GrowthChamberStage`   | `GrowthChamberMain`       | `enabled=true/false`     | Whether to run the growth chamber phase                            |

Rationale:

- `ControlNexus` already dispatches by phase; the safest approach is toggling each phase entry node.
- When a sub-phase is off, the main flow stays the same—no need to edit `MainFlow.json` for maintenance.
- Default `StageTaskSetting=No` gives the recommended full run for regular users; maintainers or power users can subdivide.

### ClueSetting, ClueSend, ClueStockLimit

| Option               | Overridden node                     | Override                             | Rationale                                                            |
| -------------------- | ----------------------------------- | ------------------------------------ | -------------------------------------------------------------------- |
| `ClueSetting=Yes`    | None directly in Pipeline           | Expands `ClueSend`, `ClueStockLimit` | Lets users customize gifting strategy                                |
| `ClueSetting=No`     | `ReceptionRoomSendCluesSelectClues` | `max_hit=3`                          | Default at most 3 gifts per run, limiting gifting scope per task     |
| `ClueSetting=No`     | `ClueItemCount`                     | `expected=^(?:[3-9]\|[1-9]\\d+)$`    | Default: gift only when single-clue stock ≥ 3, i.e. "keep 2 of each" |
| `ClueSend`           | `ReceptionRoomSendCluesSelectClues` | `max_hit={MaxClueSend}`              | Maps "max gifts" to node hit count                                   |
| `ClueStockLimit=1/2` | `ClueItemCount`                     | Adjusts OCR regex threshold          | Maps "stock cap" to gifting filter conditions                        |

Rationale:

- Reception gifting is centered on `ReceptionRoomSendCluesSelectClues`, so changing `max_hit` is natural.
- Stock threshold is "which clues count as overflow", so adjusting `ClueItemCount.expected` regex avoids extra judgment nodes.
- When `ClueSetting=No`, default overrides are explicit so "hiding advanced options" and "using default policy" stay aligned—behavior stays transparent when sub-options are hidden.

Default policy can be summarized as:

- Keep at least 2 of each clue type.
- At most 3 gifts per base facility run.

### SelectToGrow

This is the most important option for growth chamber maintenance. It is not only "pick a target"; it also decides which sub-options appear.

#### 1. `DoNothing`

| Overridden node     | Override        | Rationale                                                     |
| ------------------- | --------------- | ------------------------------------------------------------- |
| `GrowthChamberGrow` | `enabled=false` | Claim cultivation rewards only; do not enter cultivation flow |

The most direct "disable cultivation" mode.

#### 2. `GrowAgain`

| Overridden node          | Override        | Rationale                                         |
| ------------------------ | --------------- | ------------------------------------------------- |
| `GrowthChamberGrow`      | `enabled=false` | Avoid conflict with normal cultivation entry      |
| `GrowthChamberGrowAgain` | `enabled=true`  | Enable "plant again" branch after closing rewards |

Rationale:

- `GrowthChamberViewIn` now lists `GrowthChamberClaimReward`, `GrowthChamberGrow`, and `GrowthChamberExit` directly; `GrowthChamberGrowAgain` is triggered from `GrowthChamberClaimRewardClose.next`.
- `GrowAgain` must still disable normal cultivation so that after rewards close or when returning to the detail page, the "Cultivate" button does not send you back into the normal chain.

#### 3. `Any`

| Overridden node             | Override                                          | Rationale                                           |
| --------------------------- | ------------------------------------------------- | --------------------------------------------------- |
| `GrowthChamberSelectTarget` | Set `expected` to full multilingual material list | Match any cultivatable target in the candidate list |
| Sub-options                 | `AutoExtractSeed`, `SortBy`, `SortOrder`          | Any mode needs extra decisions on material order    |

Rationale:

- "Any" mode cares about stably picking a viable target from the list, not a specific material name.
- With many candidates, sort mode and direction strongly affect which target is picked, so `SortBy` and `SortOrder` are only exposed in `Any`.
- Specific-material mode locks the name; sort does not change the semantic target, so sort options are not exposed.

#### 4. `<specific material>`

Each specific-material case does two things:

1. Narrow `GrowthChamberSelectTarget.expected` to that material’s multilingual names.
2. Override `GrowthChamberCheckSeedNotEmpty` recognition.

The first is obvious; the second is easy to overlook in maintenance.

Rationale:

- In fixed-material mode, the priority is to **bind taps stably to that material’s row**.
- These cases retarget `GrowthChamberCheckSeedNotEmpty` to whole-row recognition tied to `GrowthChamberSelectTarget`, reducing reliance on small-number OCR and avoiding missed rows due to count jitter.
- Only `AutoExtractSeed` is expanded; with a fixed target, sort does not need to influence selection.

When adding new material cases, keep in sync:

- `SelectToGrow.cases.*.pipeline_override.GrowthChamberSelectTarget.expected`
- Corresponding multilingual names
- Whether the current `GrowthChamberCheckSeedNotEmpty` override strategy still applies

### AutoExtractSeed

| Setting | Overridden node                    | Override                                      | Rationale                                                             |
| ------- | ---------------------------------- | --------------------------------------------- | --------------------------------------------------------------------- |
| `Yes`   | `GrowthChamberSeedExtract`         | `enabled=true`                                | Allow entering seed core extraction when short on cores               |
| `Yes`   | `GrowthChamberGrowExit`            | `enabled=false`                               | Avoid the "return without extracting" branch                          |
| `No`    | `GrowthChamberCheckTargetNotEmpty` | Require only `GrowthChamberCheckSeedNotEmpty` | When not extracting, only rows that already have seed cores are valid |
| `No`    | `GrowthChamberSeedExtract`         | `enabled=false`                               | Block the extract branch                                              |
| `No`    | `GrowthChamberGrowExit`            | `enabled=true`                                | On "go extract seed cores" UI, return to list directly                |

Rationale:

- Default `GrowthChamberCheckTargetNotEmpty` is "seed cores OK OR crop bodies OK" because bodies can be converted to cores.
- When `AutoExtractSeed` is off, this must tighten to "seed cores OK only", or you tap rows that cannot proceed.
- So `AutoExtractSeed=No` is not only disabling `GrowthChamberSeedExtract`; the filter must change too.

### SortBy and SortOrder

These only appear when `SelectToGrow=Any`.

#### SortBy

| case                                                    | Overridden node             | Override                                                    | Rationale                                                    |
| ------------------------------------------------------- | --------------------------- | ----------------------------------------------------------- | ------------------------------------------------------------ |
| `Default` / `AmountOwned` / `AmountOfSeeds` / `Quality` | `GrowthChamberSortBy`       | Non-target option text the current sort button should match | Only open the sort panel when current sort is not the target |
| Same                                                    | `GrowthChamberSortByChoose` | Text to tap in the sort panel                               | Points to the sort mode to switch to                         |
| `Default`                                               | `GrowthChamberSortBySwipe`  | `enabled=false`                                             | Default option usually needs no extra scroll                 |

Rationale:

- `GrowthChamberSortBy` is not "tap blindly"; it first checks whether the target sort is already active.
- So case overrides are not a single string but a set of "allowed matches for current state", ensuring taps only when switching is needed.
- `GrowthChamberSortByChoose` then hits the concrete item—"check first, then switch".

#### SortOrder

| case   | Overridden node                   | Override                                          | Rationale                                                  |
| ------ | --------------------------------- | ------------------------------------------------- | ---------------------------------------------------------- |
| `ASC`  | `GrowthChamberSortOrder.expected` | Recognize "descending/DESC" on the current button | When ascending is wanted, tap only if currently descending |
| `DESC` | `GrowthChamberSortOrder.expected` | Recognize "ascending/ASC" on the current button   | When descending is wanted, tap only if currently ascending |

Rationale:

- Recognition is **current state**, not target state.
- Tap only when current order mismatches the goal, avoiding flip-flopping.

## Easy-to-miss sync points in maintenance

### 1. If you change phase entry, don’t only edit the sub-file

When adding or refactoring phase tasks, also check:

- `ControlNexus.next` in `MainFlow.json`
- Whether `assets/tasks/DijiangRewards.json` needs new phase toggles
- Copy in `assets/locales/interface/*.json`

### 2. If you change default clue policy, update the "hide advanced" branch too

Default policy is not only in raw Pipeline nodes; it lives in:

- `pipeline_override` for `ClueSetting=No`

If you only change `ClueSend` or `ClueStockLimit` sub-options but not `ClueSetting=No`, you get:

- One behavior when advanced options are expanded
- Another when they are hidden

### 3. Growth chamber options interact; don’t toggle a single node in isolation

At least three kinds of coupling:

- `SelectToGrow` chooses normal vs plant-again vs any vs fixed material.
- `AutoExtractSeed` chooses whether "bodies only, no cores" rows are acceptable.
- `SortBy` / `SortOrder` choose pick order in any mode.

If you change one, also verify:

- `GrowthChamberViewIn.next`
- `GrowthChamberClaimRewardClose.next`
- `GrowthChamberFindTarget`
- `GrowthChamberCheckTargetNotEmpty`
- `GrowthChamberSeedExtract`
- `GrowthChamberGrowExit`

### 4. Keep multilingual copy in sync with OCR lists

This task relies heavily on OCR for:

- Bay titles
- Button text
- Clue names
- Cultivation target names
- Sort option names

Editing only `assets/locales/interface/*.json` display strings is not enough. If in-game text or translations change, also check:

- `Template/Location.json`
- `Template/TextTemplate.json`
- Case overrides to `expected` in `GrowthChamber.json`

## Recommended mental model

When maintaining `DijiangRewards`, split it into three layers:

1. **Main flow**: `Entry.json` + `MainFlow.json` — **which bay to go to**.
2. **Phase logic**: `RecoveryEmotion.json`, `ReceptionRoom.json`, `Manufacturing.json`, `GrowthChamber.json` — **what to do in this bay**.
3. **UI config**: `assets/tasks/DijiangRewards.json` — **which phases and branches are enabled, which recognition conditions are rewritten**.

That makes triage easier:

- Cannot enter base → main flow layer.
- Wrong actions inside a bay → phase logic layer.
- Same task behaves differently under different options → UI config layer.

## Self-check checklist

After changes, at least verify:

1. `assets/interface.json` still imports `tasks/DijiangRewards.json`.
2. `ControlNexus.next` still matches phase entry nodes.
3. New or changed options have copy in `assets/locales/interface/*.json`.
4. If clue gifting rules change, `ClueSetting=No` default overrides are updated.
5. If cultivation target logic changes, `SelectToGrow`, `AutoExtractSeed`, `SortBy`, and `SortOrder` remain semantically consistent.
6. If a new fixed material is added, multilingual names are complete and row binding stays stable.
