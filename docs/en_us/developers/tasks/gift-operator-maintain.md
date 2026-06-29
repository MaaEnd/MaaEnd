# Development Manual - Gift Operator Maintenance Documentation

This document explains the file distribution and two execution routes of `GiftOperator`.  
This documentation was last updated on June 28, 2026.

## File Paths

| Path                                                                | Purpose                                       |
| ------------------------------------------------------------------- | --------------------------------------------- |
| `assets/interface.json`                                             | Task mounting (`dijiang_ship` group)          |
| `assets/tasks/GiftOperator.json`                                    | Task entry and interface options              |
| `assets/resource/pipeline/GiftOperator/GiftOperatorMain.json`       | Entry, Di Jiang ship location                 |
| `assets/resource/pipeline/GiftOperator/GiftOperatorNavigation.json` | Pathfinding and contact point interaction     |
| `assets/resource/pipeline/GiftOperator/GiftOperatorContact.json`    | Contact interface operator selection          |
| `assets/resource/pipeline/GiftOperator/GiftOperatorGiftFlow.json`   | Gift giving / receiving during dialogue       |
| `assets/resource/pipeline/GiftOperator/GiftOperatorBagFull.json`    | Bag full handling                             |
| `assets/resource/pipeline/GiftOperator/Operator/Operator.json`      | Operator identification for receive-only mode |
| `assets/resource/image/GiftOperator/`                               | Win32 recognition images                      |
| `assets/resource_adb/image/GiftOperator/`                           | ADB recognition images                        |
| `assets/resource_adb/pipeline/GiftOperator/`                        | ADB Pipeline mirror                           |
| `tools/gift_operator/fill_gift_operator_green_box.py`               | Operator avatar green_mask formatting         |
| `assets/locales/interface/*.json`                                   | Task, option, and operator name text          |

## Paths to Modify When Adding a New Operator

When adding a new operator, at least the following 6 locations need to be updated synchronously (`<Name>` is the operator identifier, consistent with the template filename and option case name):

| #   | Path                                                                                               | Description                                                                                                                                              |
| --- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `assets/resource/image/GiftOperator/Operators/<Name>.png`                                          | Win32 operator avatar template; must be processed with `tools/gift_operator/fill_gift_operator_green_box.py` before storage                              |
| 2   | `assets/resource_adb/image/GiftOperator/Operators/<Name>.png`                                      | ADB operator avatar template; processed similarly                                                                                                        |
| 3   | `assets/tasks/GiftOperator.json` → `SelectOperator`                                                | Add a case to provide selectable operators in the UI and supply operator information for the "Receive Only" route                                        |
| 4   | `assets/resource/pipeline/GiftOperator/Operator/Operator.json`                                     | OCR recognition and whitelist for each operator in "Receive Only" mode                                                                                   |
| 5   | `assets/resource/pipeline/GiftOperator/GiftOperatorContact.json` → `GiftOperatorSelectGiftOp.next` | Append `GiftOperatorSelect_<Name>` to the `next` array at the "Receive Only" operator selection node; otherwise, the operator node will not be triggered |
| 6   | `assets/locales/interface/*.json` → `operator.<Name>`                                              | Operator display name for each language                                                                                                                  |

## Route 1: Default (Give + Receive)

Corresponds to the "Receive Only" option being disabled. Configurable gift recipient and quantity.

1. Store bag before starting the task, then enter the Di Jiang ship bridge world.
2. Navigate to the operator contact point and open the contact interface.
3. Select operators (must pass [Selection State Verification](#selection-state-verification) after clicking; implementation in `GiftOperatorContact.json`):
    - **Any**: Switch to trust ascending order, select three consecutive operators with incomplete trust; verify sequence number 1 → 2 → 3 for each click, confirm the call only after all three are selected.
    - **Specific Operator**: Match the target operator in the list using the avatar template, then verify the selection state before calling.
4. Confirm the call; if the operator is not in position, use [Preset Orientation and Coordinate Movement Fallback](#after-calling-operator-what-to-do-if-dialogue-button-not-found) (implementation in `GiftOperatorNavigation.json`).
5. Wait for the operator to appear, enter dialogue.
6. Handle dialogue based on priority:
    - Can give gift → Select gift (also pass [Selection State Verification](#selection-state-verification) after selection; implementation in `GiftOperatorGiftFlow.json`), confirm giving, skip dialogue, leave.
    - Can receive gift → Collect gift, skip dialogue, leave.
    - Affection is full → Leave directly.
7. Number of gifts given is controlled by the "Gift Quantity" option, default allows multiple consecutive gifts.

## Route 2: Receive Only

Corresponds to the "Receive Only" option being enabled. No longer actively gives gifts, only receives gifts from operators.

1. Similarly, store the bag first, then navigate to the operator contact point.
2. In the contact list, [identify operators with gift icons](#receive-mode-how-to-correctly-select-the-target-operator) (implementation in `GiftOperatorContact.json` and `Operator/Operator.json`), rather than selecting by trust order or specific operator.
3. Confirm the call, enter dialogue, prioritize clicking "Accept Gift".
4. After receiving:
    - **Accept All Gifts** disabled → Skip dialogue, then leave, task ends.
    - **Accept All Gifts** enabled → Return to the start of the task, continue finding the next operator with a gift; only leave when no selectable operators remain in the contact interface.
5. If the bag is full, prompt and end the task.

## Special Handling

### Selection State Verification

In this task, "clicking once" does not equal "selected". The contact point operator selection and gift interface selection share the same **three-layer serial judgment** to avoid empty or off-target clicks:

```text
Selection highlight color → Text background color within the highlighted area → OCR read key text
```

#### Contact Point Operator Selection

Implementation is in `GiftOperatorContact.json`. After clicking a list row, use `And` to simultaneously satisfy:

1. **Tag Highlight Color**: Identify the HSV color block of the selected state in that row (cyan-green label background).
2. **Sequence Number Text Background**: Use the hit area from the previous step as an anchor, then identify the text background color of the sequence number.
3. **Sequence Number OCR**: Read `1` / `2` / `3` in the text area, corresponding to the current operator to be selected.

The "Any" route gradually confirms that the first, second, and third operators are all queued; the "Specific Operator" and receive gift routes verify sequence number `1` is correct after clicking the target, then click confirm call.

#### Gift Interface Gift Selection

Implementation is in `GiftOperatorGiftFlow.json`, same link chain, only anchor and OCR target differ:

1. First, use color matching to locate and click a clickable item in the bottom gift bar.
2. Then verify the selection highlight color + text background color of the gift cell.
3. Finally, OCR reads the quantity number (`\d+`) in that cell, confirming the gift is indeed in a selected state, then proceed to click "Confirm Gift".

When maintaining, if selection state recognition drifts, prioritize checking the color thresholds and OCR area offsets for these three layers; both operator and gift locations should be checked in parallel.

### Receive Mode: How to Correctly Select the Target Operator

Receive mode cannot rely on OCR of operator names to directly click the list. Instead, it **first finds the gift, then recognizes the avatar, and finally verifies the name**. Logic is distributed in `GiftOperatorContact.json` and `Operator/Operator.json`.

1. **Step 1: Locate the "Row with a Gift"**  
   In the contact list area, use `Gift.png` template matching to find the gift icon (`green_mask`). After a hit, offset to the left and click to select that operator row.

2. **Step 2: Confirm Which Operator**  
   Use the gift icon hit position as an anchor, and in the adjacent area, perform secondary matching of that operator's avatar (`Operators/<Name>.png`, also `green_mask`).  
   After a successful match, temporarily change the operator name OCR whitelist used in subsequent dialogue stages to this operator's multilingual name.  
   This step is written in `Operator/Operator.json`, one entry per operator; must be maintained synchronously when adding new operators.

    > **Example**: If `Operators/Gilberta.png` is secondarily matched next to a gift row in the contact list, the whitelist is narrowed to "Gilberta / Gilberta / …" for only that operator. After the call, when waiting for dialogue in the world, it must simultaneously see the dialogue icon and the name OCR hit that whitelist before clicking; if other operators like Pelica or Yvonne appear on the field, the names don't match, **no mis-clicks**.

3. **Step 3: Confirm Selection State**  
   Reuse the [Selection State Verification](#selection-state-verification) logic above, confirm the list row is highlighted and the sequence number is `1`, then click the yellow confirm button to call.

4. **Step 4: Secondary Verification During Dialogue Stage**  
   After the operator arrives, simultaneously recognize the "dialogue icon" and "operator name OCR"; interaction is initiated only if both hit.  
   This way, even if a gift row is clicked in the list, another check prevents "calling the wrong person" before dialogue.

Avatar templates must be processed by `fill_gift_operator_green_box.py` (green border + upper-right mask); otherwise, `green_mask` matching is unstable. Win32 and ADB each have their own set of images and must be processed separately.

If no operator with a gift is found on the current screen, the list scrolls up to 2 times; if still not found, it falls into "no selectable operator", which can serve as a round-end condition when "Accept All Gifts" is enabled.

### After Calling Operator: What to Do If Dialogue Button Not Found

After call confirmation, the task first waits for the operator to appear and attempts to click the dialogue entry. If the interactive dialogue button is still not visible on screen, it won't wait indefinitely but enters **Position Correction Fallback**, logic in `GiftOperatorNavigation.json`.

Correction sequence is fixed to three preset groups, each attempted once (the count is reset at the start of the task to avoid residuals from the previous round):

| Order | Orientation | Movement Target |
| ----- | ----------- | --------------- |
| 1     | West (270°) | (186.6, 175.0)  |
| 2     | North (0°)  | (188.0, 175.3)  |
| 3     | East (90°)  | (188.6, 176.2)  |

Each group is "turn first → then move a short distance → wait for the character to stop", then re-attempt to find the dialogue button.

If all three groups are tried and still not found, the task ends with an error and prompts "Failed to find operator identification", while retaining a screenshot for troubleshooting. Common causes are the operator spawning outside the preset area, or the position after the call deviating too much from the template ROI.

Two other similar retries handle click offsets caused by the operator walking over:

- Dialogue button found but no dialogue entered after clicking → Retry click once in place.
- Dialogue entered but right-side action buttons not yet appeared → The skip button also self-retries once, then waits for give/receive/affection full buttons to appear.

### Differences in Default Mode Operator Selection (Compared to Receive)

The "Any" route doesn't rely on avatars, but instead: switches to trust ascending order → from top to bottom, finds rows with **incomplete trust and not selected**, and clicks three consecutively.  
The "Specific Operator" route is similar to receive mode's second step, directly matching in the list using the avatar template, but without needing to first find the gift icon.
