# Protocol Space Growth Mode

Growth mode farms Protocol Space materials for a selected operator based on the shortages shown in the in-game Progression Overview. It checks requirements again after each battle's rewards are claimed and selects the next missing material automatically; it does not level up operators, upgrade skills, or enhance weapons.

This document describes the current Growth mode implementation and was updated on November 10, 2026.

## Differences from Other Farming Modes

| Mode | Farming criteria | Task configuration |
| --- | --- | --- |
| By run count | Selected stage and number of runs | Select the stage, rewards, and run count manually |
| Target inventory | Target inventory for each item | Enter the material quantities to keep |
| Growth mode | Material shortages for the selected operator in Progression Overview | Select the operator and reward claim mode |

## How to Use

1. In the game, confirm that the target operator's Progression Overview and material shortages match your goals and that the corresponding Protocol Spaces are unlocked.
2. Add the **⚔️Protocol Space** task in MaaEnd and set **Farming mode** to **Growth mode**.
3. Select **Operator to develop**, then configure **Reward Claim Mode** and what to do when Sanity is insufficient.
4. Configure the execution days, team, and auto-combat options as needed, then run the task.

Growth mode handles one operator per task and requires no target inventory or run count. The materials to farm are determined by what the game displays.

## Available Options

| Option | Behavior | Default |
| --- | --- | --- |
| Operator to develop | Select a target from the configured operator list; both male and female Endministrator portraits are supported | Perlica |
| Reward Claim Mode | **Prefer Double Claim** or **Single Claim**; Growth mode always claims rewards and has no combat-only option | Prefer Double Claim |
| When Sanity Insufficient | **End Task** or **Use Limited-Time Potion to Restore** | Use Limited-Time Potion to Restore |
| Within days | When using limited-time potions, select all, within 10 days, within 7 days, within 3 days, or within 1 day | Within 3 days |

Reward claim settings are shared with Target inventory mode. Prefer Double Claim uses the existing Sanity Usage Permit flow and can fall back to a single claim when Sanity is insufficient and cannot be restored; Single Claim does not use permits.

## Execution Flow

1. Open the operator screen and wait for the top entry area to stabilize before checking for Progression Overview; enter it when found, resetting list scroll counters before each entry.
2. Return the left operator list to the top, find the target by portrait, and confirm the selection using the name on the right.
3. Check the operator level, skills and talents, and weapon level tabs in order; inspect the current view and the view after one short swipe on each tab.
4. Identify shortages using the material icon whitelist and red quantity text, then click the Protocol Space source in the material popup.
5. Select the entry flow for that material type and reuse the existing team, combat, and reward claim flows.
6. After claiming one battle's rewards, exit and return to Progression Overview to check shortages again before choosing the next run.

If the same material is still missing, farming continues. Once it is sufficient, the task looks for the next shortage in tab and recognition order rather than ranking shortages by ratio.

## Stage Selection and Exit Conditions

| Material source | Entry rule |
| --- | --- |
| Operator EXP, Promotions, Skill Up, Weapon Tune | Select level five and set reward A/B from the item name in the popup; end if level five is locked |
| T-Creds, Weapon EXP, and the six hazardous material sources | Enter the stage linked by the source directly, without changing its level or switching A/B rewards |

The task ends when its checks of all three tabs find no shortages of whitelisted materials. It also ends when rewards cannot be claimed under the configured Sanity policy, the target stage is locked, or combat fails; Growth mode does not automatically retry failed battles.

If the Progression Overview entry is still missing after the operator screen stabilizes, the task displays a message that the feature may not be unlocked for the account or the entry could not be recognized, then ends Growth mode.

If the target operator or material source cannot be recognized, the flow may time out and exit. This does not mean all development materials are sufficient.

## Current Scope

- Only materials obtainable from Protocol Space and included in the icon whitelist are replenished; materials from other sources are not handled.
- Only the configured tab regions and scroll range are inspected; finishing the checks does not mean every development requirement is met.
- Each return to the overview selects the operator and checks materials again to avoid repeatedly farming a material that is no longer missing; a single battle's rewards can still exceed the remaining shortage.
- New option labels, Progression Overview entry recognition, and material source recognition include Simplified Chinese, Traditional Chinese, English, Japanese, and Korean.
- New portrait and material templates are in the desktop resource directory. Dedicated mobile templates and layout overrides are not included, so recognition must be verified on the device.

See the [task options](../../../assets/tasks/ProtocolSpace.json) and [Growth mode flow](../../../assets/resource/pipeline/ProtocolSpace/Growth.json) for the implementation.
