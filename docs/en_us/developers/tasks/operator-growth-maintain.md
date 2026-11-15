# Development Manual - Operator Growth Maintenance

This document describes the file layout, the growth flow, and how the material-shortage check works in `OperatorGrowth`.  
Last updated on November 14, 2026.

Operator Growth is a pure-Pipeline task with no Go-side code: enter the growth overview, find the target operator, check the owned count of Protocol Space materials across three tabs, click into a level for anything shown in red, farm it, come back and re-check, until none of the three tabs reports a shortage.

## File paths

- Task definition: `assets/tasks/OperatorGrowth.json`
- Main flow: `assets/resource/pipeline/OperatorGrowth/Growth.json` (the `OperatorGrowthMaterialsSufficient` node is defined in this same file)
- ADB overrides: `assets/resource_adb/pipeline/OperatorGrowth/Growth.json`

## The two options

- `OperatorGrowthSchedule`: which weekdays to run, a `checkbox` with seven day selections, each one producing a `pipeline_override` that fills in an `OperatorGrowthSchedule*` node.
- `OperatorGrowthOperator`: which operator to grow, a `select` over 34 operators; each case uses `pipeline_override` to swap the `template` of `OperatorGrowthTargetAvatar` to the avatar screenshot and the `expected` of `OperatorGrowthTargetName` to that operator's name text.

Adding an operator needs one avatar screenshot (for `OperatorGrowthTargetAvatar`'s `template`) and the name added to `OperatorGrowthTargetName`'s `expected` list — covering every game language (see the zh/fan/EN/ja/ko entries in existing cases); filling only one language makes `OperatorGrowthSelected` fail to recognize the target under others. No node changes.

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

## Caveats

These are the things that bite when you edit this module but do not show up from a quick read of the code.

### Cross-module dependency on Protocol Space

OperatorGrowth rewrites a lot of Protocol Space nodes, and it does so through two separate mechanisms — do not conflate them:

- The `pipeline_override` at task start (declared in the options of `tasks/OperatorGrowth.json`) statically overrides the operator avatar/name and Protocol Space's `enabled`, `next`, and `ProtocolSpaceLevelChoose.index`.
- The runtime `PipelineOverride` action (`OperatorGrowthConfigureRewardA` and `OperatorGrowthConfigureRewardB` in `Growth.json`) dynamically sets `ProtocolSpaceNormalPrepareChooseRewardABText`'s `expected` to `A` or `B`, and only for the current task run.

Before changing any Protocol Space behavior, figure out which mechanism you are touching. If you use `PipelineOverride` and also want to change a node's `next`, note that it defaults to `allow_next: false`, which silently strips `next` from the patch; you must set `allow_next: true` explicitly for it to take.

### Scheduling

`OperatorGrowthScheduleEnabled` uses `ScheduleRecognition`, and the weekday switches live in its own `attach` (all seven default to false), overridden per-case by the seven cases of `OperatorGrowthSchedule`. Put the weekday on a different node, or change `default_case` and miss a day, and that day simply does not run — no error.

Scheduling follows the game day, which cuts over at 04:00 local time: before 04:00 the day counts as the previous one. Picking "Monday" actually means Monday 04:00 through Tuesday 04:00; a run just after midnight lands on Sunday.

### Scroll counters

`OperatorGrowthResetScrollCount` calls `ClearHitCount` to reset the `max_hit` of the scroll-up, scroll-down and three tab scroll nodes. That action defaults to `strict: false`: a mistyped or missing node name still succeeds, but that node's counter is not reset, so finding the operator can stall at `max_hit` without any error. When adding nodes to the `nodes` list or editing names, check each one.

### Recognition sensitivities

- Nine nodes use `box_index: 1`, taking the second matched box (OpenOverview, SelectOperator, ClickInsufficientMaterial, OpenOptionalRewardSource, OpenDirectRewardSource, ClickOperatorLevel, ClickSkills, ClickWeaponLevel, FinishBattle). If the on-screen order of similar elements changes, the wrong one gets clicked.
- The three selected nodes (level, skills, weapon) rely on a `ColorMatch` blue highlight `lower [0,0,220] upper [180,35,255]`. A theme change or different highlight color breaks recognition — check here before touching templates.
- Reward A/B names are a five-language `^...$` regex allowlist (`OperatorGrowthRewardAName` / `OperatorGrowthRewardBName`), tied to the target-stockpile mode mapping. If Protocol Space adds or renames a material, `ConfigureRewardA/B` will not recognize it and growth stops at that step.

### Adding operators and changing config

Adding an operator touches three places: a case in the task options, an avatar at `OperatorGrowth/Operators/<Name>.png`, and `operator.<Name>` in all five language files under `assets/locales/interface/*.json`. Skip the locale and the option shows the raw key. Some operators have more than one avatar — Endministrator has both Male and Female.

The insufficient-material click offset differs between sides: the main file uses `[0,-65,0,0]`, ADB uses `[0,-81,0,0]` (16 px apart). When changing screenshots or resolution-dependent parameters, check both PC and ADB.

### Format and scope

Pipeline files are JSONC — comments and trailing commas are allowed, and a plain JSON validator will error. Run `tools/validate_schema.py` after editing.

The task's `controller` covers ADB, CloudADB, PlayCover, several Linux backends and Win32-Front, and its `group` is `sanity_sink` (the auto-check group). Any flow change must run on all those backends, or CI's sanity_sink fails.

The only node that explicitly ends the task is `OperatorGrowthOverviewUnavailable` (ends when the overview entry is not found). The other nodes that look terminal are just recognition primitives referenced by `all_of` with no `next`. The loop goes `FinishBattle`, back to `BattleCompletedInWorld`, then `OperatorGrowthEntry`, growing the same operator indefinitely with no built-in stop.
