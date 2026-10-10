# Development Manual - Gift Operator Maintenance Documentation

This document explains the files and independent receive and give stages of `GiftOperator`.
This documentation was last updated on October 8, 2026.

Only the give stage of "Only Give Gifts to Operators Below Max Trust" (`AnyNonMaxTrust`) is independent of registered operator avatars, name tables, and recipient cases. Collection, "Any Operator" (`Any`), and specific recipients still use the maintained catalog. The default task collects first: an unregistered operator with a collectible gift can still stop collection because its avatar cannot be identified. The complete task is therefore not maintenance-free.

## File Paths

| Path | Purpose |
| ------------------------------------------------------------------- | --------------------------------------------- |
| `assets/interface.json` | Task mounting (`dijiang_ship` group) |
| `assets/tasks/GiftOperator.json` | Task entry and interface options |
| `assets/resource/pipeline/GiftOperator/GiftOperatorMain.json` | Entry, Di Jiang ship location |
| `assets/resource/pipeline/GiftOperator/GiftOperatorNavigation.json` | Pathfinding and contact point interaction |
| `assets/resource/pipeline/GiftOperator/GiftOperatorContact.json` | Contact interface operator selection |
| `assets/resource/pipeline/GiftOperator/GiftOperatorReceiveFlow.json` | Receive-stage selection, collection, and daily five-gift count |
| `assets/resource/pipeline/GiftOperator/GiftOperatorGiftFlow.json` | Gift giving during dialogue |
| `agent/go-service/giftoperator/` | Single-recipient recognition and remaining, completed, and excluded recipient records |
| `tests/GiftOperator/test_gift_ui_status.json` | Gift UI name, trust, daily-limit, and selection-toast recognition tests |
| `tests/GiftOperator/test_contact_candidates.json` | Contact trust-icon and post-collection world recognition tests |
| `assets/resource/pipeline/GiftOperator/GiftOperatorBagFull.json` | Bag full handling |
| `assets/resource/pipeline/GiftOperator/Operator/Operator.json` | Receive-stage operator identification and name whitelist |
| `assets/resource/image/GiftOperator/` | Win32 recognition images |
| `assets/resource_adb/image/GiftOperator/` | ADB recognition images |
| `assets/resource_adb/pipeline/GiftOperator/` | ADB Pipeline mirror |
| `tools/gift_operator/fill_gift_operator_green_box.py` | Operator avatar green_mask formatting |
| `assets/locales/interface/*.json` | Task, option, and operator name text |

## Paths to Modify When Adding a New Operator

To add a new operator to collection, "Any Operator", or specific-recipient selection, update at least the following 7 locations (`<Name>` is the operator identifier, consistent with the template filename and option case name). The give stage of `AnyNonMaxTrust` does not read these registrations; supporting a new operator only in that mode requires no avatar template or name entry.

| # | Path | Description |
| --- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1 | `assets/resource/image/GiftOperator/Operators/<Name>.png` | Win32 operator avatar template; must be processed with `tools/gift_operator/fill_gift_operator_green_box.py` before storage |
| 2 | `assets/resource_adb/image/GiftOperator/Operators/<Name>.png` | ADB operator avatar template; processed similarly |
| 3 | `assets/tasks/GiftOperator.json` → `SelectOperator` | Add a recipient case in the UI and configure `GiftOperatorSendCandidate.attach.templates` and the give-stage name whitelist `GiftOperatorName` |
| 4 | `assets/resource/pipeline/GiftOperator/Operator/Operator.json` | Recognize operator avatars in the receive stage and override the separate name whitelist `GiftOperatorReceiveName` |
| 5 | `assets/resource/pipeline/GiftOperator/GiftOperatorReceiveFlow.json` → `GiftOperatorSelectGiftOp.next` | Append `GiftOperatorSelect_<Name>` to the receive-stage selection node's `next` array; otherwise, the operator node will not be triggered |
| 6 | `assets/resource/pipeline/GiftOperator/GiftOperatorGiftFlow.json` → `GiftOperatorSendCandidate.attach.operators` | Append `GiftOperatorSelect_<Name>` so the give-stage candidate recognizer can reuse that operator's avatar and multilingual names |
| 7 | `assets/locales/interface/*.json` → `operator.<Name>` | Operator display name for each language |

## Route 1: Default (Receive, Then Give)

Corresponds to the "Receive Only" option being disabled. The task entry `GiftOperatorMain` runs `StashBackpackSubTask`, `GiftOperatorReceiveMain`, and `GiftOperatorSendMain` in order through `SubTask`. Giving starts only after collection finishes and uses the configured recipients, recipient count, and gift target.

The recipient option `SelectOperator` offers "Any Operator", specific operators, and "Only Give Gifts to Operators Below Max Trust" (`AnyNonMaxTrust`). "Gift Recipient Count" (`GiftOperatorCount`) is a separate positive-integer input, defaulting to `1`; "Gift Target" (`GiftRingCount`) selects one, two, or three filled rings and defaults to `ThreeRings`. The target is that operator's cumulative filled-ring count for the day, including earlier progress, rather than additional rings for this run or a fixed number of gifts. These settings affect only giving and do not change collection of the five daily gifts.

1. Store the bag before starting the task.
2. Run the separate receive stage to collect the five daily gifts one at a time. If some gifts were already collected that day, finish the stage after scanning and collecting the remaining gifts. See [Route 2](#route-2-receive-only) for the detailed flow.
3. Once collection finishes and the task confirms a return to the Di Jiang world, start the give stage and reopen the contact interface.
4. Select exactly one recipient per round (must pass [Selection State Verification](#selection-state-verification) after clicking; implementation in `GiftOperatorContact.json`):
    - **Any Operator**: Switch to trust ascending order. The candidate recognizer uses the existing 31 operator avatars, reads trust separately within each card, and selects an eligible identity in row and column order. Cards with `200%` or unconfirmed trust are skipped, along with identities already completed or excluded.
    - **Only Give Gifts to Operators Below Max Trust**: Set `GiftOperatorSendCandidate.attach.generic` to `true`. Locate cards through the generic trust icon, read each card's trust, and capture an avatar core excluding the information button, border, and trust text. Selection and exclusion use runtime identities for this task; this path does not read `attach.operators`, fixed avatars, or multilingual name tables.
    - **Specific Operator**: `GiftOperatorSendCandidate.attach.templates` contains only that operator's avatar. Once completed or excluded, that identity cannot be selected again. Exhausting the specified recipient ends the stage and reports the uncompleted count, without repeating gifts to meet the requested count.
5. Confirm the call; if the operator is not in position, use [Heading Correction and One-Time Teleport Recovery](#after-calling-operator-what-to-do-if-dialogue-button-not-found) (implementation in `GiftOperatorNavigation.json`).
6. Wait for the operator to appear, enter dialogue, and open the gift UI. All recipient options first read the cumulative filled-ring count, the current ring's high/medium/low progress tier, trust, and the fixed daily-limit text. This daily cumulative progress persists after reopening the gift UI. If the selected ring target is already reached, trust is already `200%`, or the daily limit is already reached, exclude that identity without reducing the remaining recipient count.
7. When eligible, click one gift at a time, pass [Selection State Verification](#selection-state-verification), and read the preview progress again. Continue selecting while below the target. Stop selecting and confirm giving once the preview reaches the target or caps at three filled rings, the daily limit, or `200%` trust. Skip dialogue and leave, then talk to the same operator and reopen the gift UI to verify the actual cumulative rings and cap state. Preview progress cannot establish success.
8. Only a successful `observe_after` observation and commit decrements `remaining`, adds the identity to `completed`, and excludes it from future candidates. Continue with another recipient while the count is nonzero. Exhausting available targets ends the stage and reports the uncompleted count, including when the requested count exceeds the available recipients or a specified single recipient is exhausted. `finish` summarizes completed identities, excluded identities, and the remaining count; it returns an incomplete-result error if that count is greater than zero.

Go's `GiftOperatorCandidateRecognition` selects its recognition path through `attach.generic`. `false` serves "Any Operator" and specific recipients: it derives a canonical identity from the avatar filename and reuses avatars and multilingual names from `Operator/Operator.json`; an empty `attach.templates` selects from registered operators, while a specified list restricts candidates. `true` serves only `AnyNonMaxTrust`, using generic trust icons, runtime portraits, and names read from the current UI. All other recipient cases explicitly set `generic: false` to prevent leftover overrides. `GiftOperatorSessionAction` only maintains the gift target, remaining recipients, identities, observations, and completed/excluded records; Pipeline still controls every UI action and phase transition.

### Below-Max-Trust Mode: Runtime Identity Verification

1. Locate cards with the generic `GiftOperatorTrustIcon` and save the selected avatar core. Match that runtime template again before clicking, then confirm that exactly one operator is selected. Completed and daily-full portraits cannot be selected again; trust sorting can change, so card indices cannot identify excluded operators.
2. Once `GiftOperatorSendSetGenericDialoguePhase` is hit, the call-confirmation dialogue anchor points to `GiftOperatorSendFindDialogue`. `GiftOperatorDialogueRecognition` reads visible interaction names, skips processed operators and names encountered incorrectly in this round, then restricts `GiftOperatorName` before entering dialogue.
3. In the gift UI, match the top-right avatar against the runtime portrait before reading its actual name through `GiftOperatorGiftName`. Only a matched portrait can bind the gift-screen and interaction names and record the initial cumulative ring progress, trust, and daily-limit state. The two OCR spellings may differ and are stored separately; neither comes from a multilingual catalog.
4. If an old nearby operator is encountered, `GiftOperatorSendWrongRecipient` records only that interaction name for avoidance in the current round, closes the gift UI, leaves dialogue, and resumes searching for the reserved operator. It preserves the portrait and remaining count instead of marking the reserved operator completed or excluded.
5. After submission, reopen the same operator's gift UI and verify the runtime portrait and bound gift-screen name again. Only the confirmed actual cumulative ring target or actual cap state commits success. An unconfirmed portrait, name, or outcome preserves the remaining count; recognition failure is never successful gifting.

New operators normally need no registration for this give mode. Changes to card layout, gift-avatar scaling, or name position still require maintaining generic recognition. Validate each controller layout separately; the trust-icon size alone cannot determine avatar dimensions. Existing daily-limit screenshots cover name, trust, and state recognition, not successful gifting. Simulate an unknown operator in an isolated resource copy with its registration removed and verify the give entry separately, preventing catalog-based collection from stopping the test first.

Success evidence comes from the stable gift UI reopened after submission, confirming that the actual cumulative ring target was reached. Three actual filled rings, the fixed text "[今日赠礼可提升的信赖]已达上限，请明天再来吧", or an actual `200%` trust cap can also finish the current recipient. Preview rings and the selection toast "[今日赠礼可提升的信赖]已达上限，无法选择更多" only stop further preselection; neither independently establishes success or decrements the recipient count.

Success no longer requires an increase in the integer trust percentage; it depends on actual cumulative ring progress and the cap state confirmed after submission. If actual progress is below the target without a confirmed cap, or progress cannot be recognized, stop and retain the uncompleted count. Preview progress or successful clicks do not establish completed gifting.

Both stages share contact-point navigation and call confirmation, but use separate selection, dialogue, and name-whitelist chains. `GiftOperatorCheckContact.next` dispatches through `[Anchor]GiftOperatorSelectPhase` to receive-stage or give-stage selection; `GiftOperatorConfirmSelect.next` dispatches through `[Anchor]GiftOperatorWaitChatPhase` to collection's `GiftOperatorReceiveWaitChat`, catalog-based giving's `GiftOperatorWaitChat`, or generic giving's `GiftOperatorSendFindDialogue`. Collection only overrides `GiftOperatorReceiveName`, preserving the give-stage `GiftOperatorName` configured by the recipient option.

## Route 2: Receive Only

Corresponds to the "Receive Only" option being enabled. It disables `GiftOperatorSendMain`, so the task ends after collection. Collection is identical to the first stage of the default route and is independent of the configured recipients, recipient count, and gift target. The "Accept All Gifts" option has been removed.

1. Similarly, store the bag first, then navigate to the operator contact point.
2. At the start of each round, `GiftOperatorReceiveListToTop` scrolls in reverse and uses `ListCompleteRecognition` to confirm that the contact list has returned to the top, preventing a saved scroll position from hiding earlier gifts. Then [identify operators with gift icons](#receive-mode-how-to-correctly-select-the-target-operator) (implementation in `GiftOperatorReceiveFlow.json` and `Operator/Operator.json`), rather than selecting by trust order or specific operator.
3. Confirm the call and enter dialogue. Only click "Accept Gift"; do not enter a giving branch.
4. After collection, skip dialogue and leave. `GiftOperatorReceiveBackInWorld` must confirm `InDijiangWorld` before `GiftOperatorReceiveContinue` starts another collection round.
5. `GiftOperatorReceiveContinue.max_hit` is `4`: the initial collection plus four more rounds yields at most five gifts. The count is cleared only at the receive-stage entry, and remains intact when looking for the next gift. This limit represents the five daily gifts rather than failed-operation retries.
6. If fewer than five gifts remain that day, continue scrolling whenever the current page contains no gifts. `ListCompleteRecognition` confirms that the list no longer changes after scrolling, then the task closes the contact interface. The receive stage ends after confirming a return to the world.
7. If the bag is full, prompt and end the task.

## Special Handling

Selected quantities are pending items in the current transaction. The preview includes gifts already delivered that day: with one previously delivered item, selecting 29 more previews a daily total of 30 and the first filled ring. The observed 29, 59, and 74 quantities are not fixed stopping thresholds. The game numbers rings from zero; the one-ring option fills ring 0 and the two-ring option fills rings 0 and 1. For three rings, selection continues one item at a time until the game refuses further selection, then delivery is confirmed and the gift screen is reopened for verification.

### Selection State Verification

In this task, a click does not establish selection. Contact selection verifies highlight color, text background, and sequence number. Gift selection reads complete quantities within yellow labels and requires the selected total to increase after each click.

```text
Selection highlight color → Text background color within the highlighted area → OCR read key text
```

#### Contact Point Operator Selection

Implementation is in `GiftOperatorContact.json`. After clicking a list row, use `And` to simultaneously satisfy:

1. **Tag Highlight Color**: Identify the HSV color block of the selected state in that row (cyan-green label background).
2. **Sequence Number Text Background**: Use the hit area from the previous step as an anchor, then identify the text background color of the sequence number.
3. **Sequence Number OCR**: Both receive and give stages now select one operator per round; read sequence number `1` to confirm that the target is queued.

The give-stage candidate recognizer reserves one operator identity, then recognizes that identity's avatar again before clicking, avoiding a click box from the previous frame. Every give-stage selection and the receive route verify sequence number `1` after clicking the target, then confirm the call.

#### Gift Interface Gift Selection

Implementation is in `GiftOperatorGiftFlow.json`:

1. First, use color matching to locate and click a clickable item in the bottom gift bar.
2. Locate the yellow quantity label for each selected gift cell.
3. Exclude the icon and lower border from each label, read its complete positive integer, sum all selected labels, and read the cumulative preview ring progress. Recognize the state after every click; only click "Confirm Gift" once the target or preview cap is reached. Success still requires reopening the UI after submission and verifying actual progress.

When maintaining, if selection state recognition drifts, prioritize checking the color thresholds and OCR area offsets for these three layers; both operator and gift locations should be checked in parallel.

### Receive Mode: How to Correctly Select the Target Operator

Receive mode cannot rely on OCR of operator names to directly click the list. Instead, it **first finds the gift, then recognizes the avatar, and finally verifies the name**. Logic is distributed in `GiftOperatorReceiveFlow.json` and `Operator/Operator.json`.

1. **Step 1: Locate the "Row with a Gift"**  
   In the contact list area, use `Gift.png` / `Gift_2.png` template matching to find the gift icon (`green_mask`). After a hit, offset to the adjacent click area and select that operator row.

2. **Step 2: Confirm Which Operator**  
   Use the gift icon hit position as an anchor, and in the adjacent area, perform secondary matching of that operator's avatar (`Operators/<Name>.png`, also `green_mask`).  
   After a successful match, set the separate receive-stage name OCR whitelist `GiftOperatorReceiveName` to this operator's multilingual name, preserving the give-stage whitelist `GiftOperatorName`.
   This step is written in `Operator/Operator.json`, one entry per operator; must be maintained synchronously when adding new operators.

    > **Example**: If `Operators/Gilberta.png` is secondarily matched next to a gift row in the contact list, the whitelist is narrowed to "Gilberta / Gilberta / …" for only that operator. After the call, when waiting for dialogue in the world, it must simultaneously see the dialogue icon and the name OCR hit that whitelist before clicking; if other operators like Pelica or Yvonne appear on the field, the names don't match, **no mis-clicks**.

3. **Step 3: Confirm Selection State**  
   Reuse the [Selection State Verification](#selection-state-verification) logic above, confirm the list row is highlighted and the sequence number is `1`, then click the yellow confirm button to call.

4. **Step 4: Secondary Verification During Dialogue Stage**  
   After the operator arrives, simultaneously recognize the "dialogue icon" and "operator name OCR"; interaction is initiated only if both hit.  
   This way, even if a gift row is clicked in the list, another check prevents "calling the wrong person" before dialogue.

Avatar templates must be processed by `fill_gift_operator_green_box.py` (green border + upper-right mask); otherwise, `green_mask` matching is unstable. Win32 and ADB each have their own set of images and must be processed separately.

Each search starts with `GiftOperatorReceiveListToTop` returning to the top, followed by `GiftOperatorReceiveSelect` scanning downward. If no operator with a gift is found on the current screen, `GiftOperatorReceiveSwipe` scrolls the list and waits for it to stabilize before scanning again. `GiftOperatorReceiveListComplete` uses `ListCompleteRecognition` to compare the list before and after scrolling; collection ends once the list no longer changes and the current page has no gifts. `GiftOperatorReceiveRoundEntry` resets `attach.ready` on both `GiftOperatorReceiveListTopComplete` and `GiftOperatorReceiveListComplete` for each new search, preserving the gift count.

### After Calling Operator: What to Do If Dialogue Button Not Found

Before and after calling an operator, the task first recognizes the contact-point or target-operator interaction. If it is absent, the task applies the connected heading corrections and recognizes the interaction again. This logic is in `GiftOperatorNavigation.json`. Each step follows "recognize interaction → correct heading → recognize interaction again"; the former three coordinate-movement groups no longer describe the active flow.

The current correction order and per-round `max_hit` values are:

| Order | Node | Heading | `max_hit` |
| ----- | -------------------------------- | --------------------- | --------- |
| 1 | `GiftOperatorTurnEast` | East (90°) | 2 |
| 2 | `GiftOperatorTurnNorthwest` | Northwest (315°) | 1 |
| 3 | `GiftOperatorTurnNorthByWest` | North by west (345°) | 1 |
| 4 | `GiftOperatorTurnWest` | West (270°) | 2 |
| 5 | `GiftOperatorTurnSoutheast` | Southeast (135°) | 2 |

Only heading nodes connected to the current flow participate. Unconnected coordinate-movement nodes remain inactive and do not provide evidence for inventing new movement coordinates.

If the round starts in place or away from the teleport point, a navigation failure or failed positioning before or after the call triggers `SceneEnterWorldDijiang0`, followed by one complete traversal from the teleport point. If the round already started there, or the recovery traversal fails again, terminate through `ERROR` without another teleport.

Each round initializes the navigation recovery anchors and heading-correction counts so that previous recovery state cannot persist. Recovery preserves the collected-gift count and the give-stage session. When giving, call the same operator held in `Pending` again, retaining its identity, remaining count, and processed records; do not `reserve` a new candidate.

Teleport recovery covers only navigation and calling before gift submission. Failure to verify identity, actual ring progress, or success after reopening the gift UI does not enter this recovery path. Stop with the uncompleted count preserved to avoid repeating a delivery.

Two other similar retries handle click offsets caused by the operator walking over:

- Dialogue button found but no dialogue entered after clicking → Retry click once in place.
- Dialogue entered but right-side action buttons not yet appeared → The skip button also self-retries once, then waits for give/receive buttons to appear.

### Give-Stage Operator Selection Differences (Compared to Receive)

Giving selects one operator per round without first locating a gift icon. `AnyNonMaxTrust` uses generic trust icons, runtime portraits, and names read from the UI. "Any Operator" and specific recipients still use fixed avatar identities; specific recipients additionally restrict `attach.templates`. Collection still finds the gift icon before identifying the avatar and name through the maintained catalog. Only verified gift results commit success, and each mode excludes operators using its corresponding identity.
