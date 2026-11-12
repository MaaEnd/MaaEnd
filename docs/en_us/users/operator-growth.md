# Operator Growth

**🌱Operator Growth** is a standalone task that farms Protocol Space materials for a selected operator based on the shortages shown in the in-game Progression Overview. It checks requirements again after each battle's rewards are claimed and selects the next missing material automatically; it does not level up operators, upgrade skills, or enhance weapons.

This document describes the current Operator Growth task implementation and was updated on November 12, 2026.

## Differences from the Protocol Space Task

| Task / mode | Farming criteria | Task configuration |
| --- | --- | --- |
| Protocol Space: By run count | Selected stage and number of runs | Select the stage, rewards, and run count manually |
| Protocol Space: Target inventory | Target inventory for each item | Enter the material quantities to keep |
| Operator Growth | Material shortages for the selected operator in Progression Overview | Select the operator and reward claim mode |

## How to Use

1. In the game, confirm that Progression Overview is unlocked for the account, the target operator's displayed material requirements match your goals, and the required Protocol Space stages are unlocked.
2. Add the **🌱Operator Growth** task in MaaEnd. This task runs on its own; adding **⚔️Protocol Space** alongside it is unnecessary.
3. Select **Operator to develop**, then configure **Reward Claim Mode** and what to do when Sanity is insufficient.
4. Configure **Execution cycle (game time)**, **Select Team**, and **Auto Combat Settings** as needed, then run the task.

The Operator Growth task handles one operator at a time, does not read the target inventory options, and provides no run count limit. It detects shortages from red quantity text below material icons in Progression Overview rather than calculating the exact missing quantities. Farming continues until the scan finds no shortages, the Sanity policy prevents further runs, or the flow ends because of an error.

Operator Growth and Protocol Space are separate tasks, each with its own execution cycle and option selections. Target inventory or run counts configured in Protocol Space do not determine when Operator Growth ends.

## Available Options

| Option | Behavior | Default |
| --- | --- | --- |
| Execution cycle (game time) | Check the selected weekdays when the task runs; skip if the current game day is not selected | All seven weekdays selected |
| Select Team | **Default** keeps the current team; a team from 01 to 07 can also be selected | Default |
| Operator to develop | Select a target from the configured operator list; both male and female Endministrator portraits are supported | Perlica |
| Reward Claim Mode | **Prefer Double Claim** or **Single Claim**; the Operator Growth task always claims rewards and has no combat-only option | Prefer Double Claim |
| When Sanity Insufficient | **End Task** or **Use Limited-Time Potion to Restore** | Use Limited-Time Potion to Restore |
| Within days | When using limited-time potions, select all, within 10 days, within 7 days, within 3 days, or within 1 day | Within 3 days |

The execution cycle uses the local time of the device running MaaEnd, with each game day starting at 04:00. It checks whether to run when the task starts; selecting weekdays does not schedule the task to start automatically.

Reward claim and limited-time potion rules reuse the Protocol Space implementation, with options selected separately in Operator Growth. **Within days** appears only when **Use Limited-Time Potion to Restore** is selected and limits eligible limited-time potions; potions without an expiry are excluded.

**Prefer Double Claim** attempts to use a Sanity Usage Permit. If Sanity is insufficient while claiming rewards and no Sanity is restored after following the selected policy, the flow falls back to a single claim without a permit; it ends if that claim also cannot be completed. If Sanity is already insufficient before entry and cannot be restored under the configured policy, the task ends immediately instead of entering combat and falling back afterward. **Single Claim** does not use permits.

## Execution Flow

1. Check the execution cycle first. If execution is allowed, open the operator screen and wait for the top entry area to stabilize before checking for Progression Overview; enter it when found, resetting list scroll counters before each entry.
2. Return the left operator list to the top, find the target by portrait, and confirm the selection using the name on the right.
3. Check the operator level, skills and talents, and weapon level tabs in order. If no shortage is recognized in the current view, make at most one short swipe per tab to check further. A shortage in any view starts farming that material immediately, before the remaining tabs are scanned.
4. Identify shortages using the material icon whitelist and red quantity text, then click the Protocol Space source in the material popup.
5. Select the entry flow for that material type and reuse the existing team, combat, and reward claim flows.
6. After claiming one battle's rewards, exit and return to Progression Overview to check shortages again before choosing the next run.

If the same material is still missing, farming continues. Once it is sufficient, the task looks for the next shortage in tab and recognition order rather than ranking shortages by ratio.

## Stage Selection and Exit Conditions

| Material source | Entry rule |
| --- | --- |
| Operator EXP, Promotions, Skill Up, Weapon Tune | Select level five and set reward A/B from the item name in the popup; end if level five is locked |
| T-Creds, Weapon EXP, and the six hazardous material sources | Enter the stage linked by the source directly, without changing its level or switching A/B rewards |

The task ends when its checks of all three tabs recognize no shortages of whitelisted materials. This means only that no shortage was recognized within the scanned area, not that all development materials are sufficient. It also ends when rewards cannot be claimed under the configured Sanity policy, the target stage is locked, or combat fails. The Operator Growth task does not automatically retry failed battles or switch to a lower stage when level five is locked.

If the Progression Overview entry is still missing after the operator screen stabilizes, the task displays a message that the feature may not be unlocked for the account or the entry could not be recognized, then ends the Operator Growth task.

If the target operator or material source cannot be recognized, the flow may time out and exit. This does not mean all development materials are sufficient.

## Current Scope

- Only materials obtainable from Protocol Space and included in the icon whitelist are replenished; materials from other sources are not handled.
- Only the configured tab regions and scroll range are inspected; finishing the checks does not mean every development requirement is met.
- Each return to the overview selects the operator and checks materials again to avoid repeatedly farming a material that is no longer missing; a single battle's rewards can still exceed the remaining shortage.
- Operator options, Progression Overview entry recognition, and material source recognition include Simplified Chinese, Traditional Chinese, English, Japanese, and Korean; this does not mean every language has been verified in-game.
- The task configuration allows Win32 foreground, the three Linux controllers, ADB, CloudADB, and PlayCover. The growth flow and templates are in the shared resource directory. Dedicated mobile templates and layout overrides for this task are not included, so recognition must be verified with each controller in-game.

See the [task options](../../../assets/tasks/OperatorGrowth.json), [task entry](../../../assets/resource/pipeline/OperatorGrowth.json), and [growth flow](../../../assets/resource/pipeline/OperatorGrowth/Growth.json) for the implementation. Operator portraits and material templates are stored in `assets/resource/image/OperatorGrowth/Operators/` and `assets/resource/image/OperatorGrowth/Materials/`, respectively; team selection, combat, and reward claims reuse shared Protocol Space nodes.
