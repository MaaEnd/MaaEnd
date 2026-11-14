# Development Manual - Operator Growth Maintenance

This document describes the file layout, the growth flow, and how the material-shortage check works in `OperatorGrowth`.  
Last updated on November 14, 2026.

Operator Growth is a pure-Pipeline task with no Go-side code: enter the growth overview, find the target operator, check the owned count of Protocol Space materials across three tabs, click into a level for anything shown in red, farm it, come back and re-check, until none of the three tabs reports a shortage.

## File paths

- Task definition: `assets/tasks/OperatorGrowth.json`
- Main flow: `assets/resource/pipeline/OperatorGrowth/Growth.json` (with `OperatorGrowth.json` holding `OperatorGrowthMaterialsSufficient`)
- ADB overrides: `assets/resource_adb/pipeline/OperatorGrowth/Growth.json`
- Material data: `tools/zmdmap/data/operator_growth.json` and `datasets/operator_growth.py`

## The two options

- `OperatorGrowthSchedule`: which weekdays to run, a `checkbox` with seven day selections, each one producing a `pipeline_override` that fills in an `OperatorGrowthSchedule*` node.
- `OperatorGrowthOperator`: which operator to grow, a `select` over eight operators; each case uses `pipeline_override` to swap the `template` of `OperatorGrowthTargetAvatar` and `OperatorGrowthTargetName` to that operator's screenshots.

Adding an operator needs two new screenshots (the left-side avatar and the right-side name) and no node changes.

## Main flow

The entry `OperatorGrowthSchedule` gates by weekday. On entry, `OperatorGrowthCheckOverview` confirms the operator page, and `OperatorGrowthOverviewUnavailable` ends the task when the growth-overview entry cannot be found.

Before entering the overview, `OperatorGrowthOpenOverview` runs `OperatorGrowthResetScrollCount`, which clears the `max_hit` of the scroll-up, scroll-down and all three tab scroll nodes, so every entry starts with a full swipe quota.

Finding the operator is a loop between `OperatorGrowthSearchOperator` and `OperatorGrowthScrollDown`:

- Each screen first tries `OperatorGrowthSelected` (right-side name matches) and `OperatorGrowthSelectOperator` (avatar template hit); if neither, scroll down one screen and look again
- `OperatorGrowthScrollDown` swipes 240 px per attempt with `max_hit: 10`; adjacent screens keep an overlap so an avatar split across two screens is not missed
- When found, `OperatorGrowthSelectOperator` clicks the avatar and `OperatorGrowthSelected` confirms the right-side name matches the target

## The material-shortage check

The three tabs — operator level, skills, weapon level — share one structure: switch tab, confirm selection, check the current screen for red, click into whatever is short, scroll down, repeat.

The red check lives in `OperatorGrowthRedQuantity`: the `ColorMatch` roi is literally `"OperatorGrowthMaterialTemplates"`, meaning every material template hit box is the base, and `roi_offset [0, 65, 0, -22]` takes the owned-count row below it; HSV ranges `[[0,80,130],[5,255,255]]` and `[[175,80,130],[180,255,255]]` bracket the red, `count: 12` requires at least twelve samples, and `order_by: Vertical` sorts by column.

On shortage, `OperatorGrowthClickInsufficientMaterial` clicks that material image, and the source decides the route:

- Coins, weapon EXP and the dive materials go through `OperatorGrowthOpenDirectRewardSource` and enter directly
- Four sources — operator EXP, operator promotion, skill upgrade and weapon upgrade — pick between reward A and B, so they go through `OperatorGrowthOpenOptionalRewardSource` into the material dialog, then `OperatorGrowthConfigureRewardA` / `ConfigureRewardB` switch the tier-five reward to the target material, reusing the reward-switch flow from the target-stockpile mode

After a run, `OperatorGrowthFinishBattle` looks for `ProtocolSpaceRewardClaimSuccess` plus the white cancel button and clicks the reward window away; `OperatorGrowthBattleCompletedInWorld` confirms the return to the open world, then the overview is entered again for a fresh check.

Only after all three tabs pass and `OperatorGrowthMaterialsSufficient` (chained after the weapon tab selection) hits is the growth considered done.

## ADB overrides

ADB overrides 14 nodes, all on the recognition side: OCR for the overview entry and the "All" title, templates for the target avatar and name, roi and color ranges for the material templates and red check, and the tab plus selection confirmation for the three tabs. Actions and flow structure stay untouched.

When changing screenshots or resolution-dependent parameters, check both PC and ADB; do not change only one side.

## Debugging

Run nodes individually as described in `docs/en_us/developers/node-testing.md`. Three places to look first:

- Stuck while finding the operator: check whether `OperatorGrowthScrollDown`'s hit count was already consumed by earlier overview entries, and whether `OperatorGrowthResetScrollCount` actually ran
- Red check misses: `OperatorGrowthMaterialTemplates` template boxes and `roi_offset` are a pair — changing a template's height means changing the offset
- Not returning after a run: `OperatorGrowthBattleCompletedInWorld` relies on the open-world screen; if that step fails, the re-entry into the overview never happens either
