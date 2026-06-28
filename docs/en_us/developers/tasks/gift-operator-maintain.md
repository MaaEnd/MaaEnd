# Developer Guide — Gift Operator Maintenance

This document describes the file layout of `GiftOperator` and its two execution routes.  
Last updated June 28, 2026.

## File overview

| Path                                                                | Role                                              |
| ------------------------------------------------------------------- | ------------------------------------------------- |
| `assets/interface.json`                                             | Task mount (`dijiang_ship` group)                 |
| `assets/tasks/GiftOperator.json`                                    | Task entry and UI options                         |
| `assets/resource/pipeline/GiftOperator/GiftOperatorMain.json`       | Entry and Dijiang positioning                     |
| `assets/resource/pipeline/GiftOperator/GiftOperatorNavigation.json` | Pathfinding and contact terminal interaction      |
| `assets/resource/pipeline/GiftOperator/GiftOperatorContact.json`    | Operator selection on the contact screen          |
| `assets/resource/pipeline/GiftOperator/GiftOperatorGiftFlow.json`   | Gift giving / receiving in dialogue               |
| `assets/resource/pipeline/GiftOperator/GiftOperatorBagFull.json`    | Full-inventory handling                           |
| `assets/resource/pipeline/GiftOperator/Operator/Operator.json`      | Operator recognition in receive-only mode         |
| `assets/resource/image/GiftOperator/`                               | Win32 recognition images                          |
| `assets/resource_adb/image/GiftOperator/`                           | ADB recognition images                            |
| `assets/resource_adb/pipeline/GiftOperator/`                        | ADB Pipeline mirror                               |
| `tools/gift_operator/fill_gift_operator_green_box.py`               | Operator portrait `green_mask` formatting         |
| `assets/locales/interface/*.json`                                   | Task, option, and operator name strings           |

## Paths to update when adding an operator

When adding a new operator, update at least these 6 places (`<Name>` is the operator identifier and must match the template filename and option case name):

| #   | Path                                                               | Description                                                                                                    |
| --- | ------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------- |
| 1   | `assets/resource/image/GiftOperator/Operators/<Name>.png`          | Win32 operator portrait template; run through `tools/gift_operator/fill_gift_operator_green_box.py` before commit |
| 2   | `assets/resource_adb/image/GiftOperator/Operators/<Name>.png`      | ADB operator portrait template; same processing as above                                                       |
| 3   | `assets/tasks/GiftOperator.json` → `SelectOperator`                | Add a case for UI selection and to supply operator info for the receive-only route                             |
| 4   | `assets/resource/pipeline/GiftOperator/Operator/Operator.json`     | Per-operator OCR recognition and whitelist in receive-only mode                                                |
| 5   | `assets/resource/pipeline/GiftOperator/GiftOperatorContact.json` → `GiftOperatorSelectGiftOp.next` | Append `GiftOperatorSelect_<Name>` to the `next` array, or the receive-only route never triggers the new operator node |
| 6   | `assets/locales/interface/*.json` → `operator.<Name>`              | Localized operator display names                                                                               |

## Route 1: Default (give + receive)

Used when **Receive gifts only** is off. Gift target and gift count are configurable.

1. Stash inventory before the task, then enter the Dijiang bridge open world.
2. Pathfind to the operator contact terminal and open the contact UI.
3. Select operators (each click must pass [selection-state verification](#selection-state-verification); see `GiftOperatorContact.json`):
    - **Any**: Switch to ascending trust order and pick three operators below max trust; verify slot 1 → 2 → 3 after each click, then confirm summon only when all three are selected.
    - **Specific operator**: Match the target portrait in the list; after a hit, verify selection state again before summoning.
4. Confirm summon; if the operator does not appear in time, fall back to [preset facing and coordinate moves](#what-if-the-dialogue-button-is-not-found-after-summoning) (see `GiftOperatorNavigation.json`).
5. Wait for the operator to appear and enter dialogue.
6. In dialogue, handle by priority:
    - Can give → select gift (also uses [selection-state verification](#selection-state-verification); see `GiftOperatorGiftFlow.json`), confirm, skip dialogue, leave.
    - Can receive → claim gift, skip dialogue, leave.
    - Affinity maxed → leave directly.
7. Gift count is controlled by the **Gift count** option; multiple gifts in a row are allowed by default.

## Route 2: Receive gifts only

Used when **Receive gifts only** is on. The task no longer sends gifts proactively; it only claims gifts from operators.

1. Stash inventory, then pathfind to the operator contact terminal as usual.
2. On the contact list, [identify operators with a gift icon](#receive-only-mode-how-to-select-the-right-operator) (see `GiftOperatorContact.json` and `Operator/Operator.json`) instead of trust sorting or a specific operator.
3. Confirm summon, enter dialogue, and prioritize **Receive gift**.
4. After claiming:
    - **Accept all gifts** off → skip dialogue and leave; task ends.
    - **Accept all gifts** on → return to task start and look for the next operator with a gift; leave only when no selectable operator remains on the contact screen.
5. If inventory is full, show a message and end the task.

## Special handling

### Selection-state verification

In this task, a click does not mean selection. Contact-list operator picks and gift-bar picks share the same **three-layer chain** to avoid empty or mis-clicks:

```text
Selected highlight color → text background inside the highlight → OCR of key text
```

#### Contact list operator selection

Implemented in `GiftOperatorContact.json`. After each list-row click, an `And` must satisfy:

1. **Label highlight color**: HSV block for the selected row (cyan-green label background).
2. **Slot number text background**: anchored on the previous hit, match the background behind the slot number.
3. **Slot OCR**: read `1` / `2` / `3` in the text region and match the expected slot.

The **Any** route uses this to confirm slots 1, 2, and 3 in order. **Specific operator** and receive-only routes verify slot `1` before confirming summon.

#### Gift selection in the gift UI

Implemented in `GiftOperatorGiftFlow.json` with the same chain but different anchors and OCR targets:

1. Use color match to locate a clickable item in the bottom gift bar and click it.
2. Verify the gift cell’s selected highlight color and text background.
3. OCR the quantity digit (`\d+`) in that cell to confirm selection before clicking **Confirm gift**.

If selection-state recognition drifts during maintenance, check color thresholds and OCR ROI offsets in all three layers for both operator and gift paths.

### Receive-only mode: how to select the right operator

Receive mode cannot rely on operator-name OCR to click list rows directly. It uses **find gift → match portrait → verify name**, split across `GiftOperatorContact.json` and `Operator/Operator.json`.

1. **Step 1: locate a row with a gift**  
   Template-match `Gift.png` (`green_mask`) in the contact list. After a hit, click leftward to select that row.

2. **Step 2: identify which operator it is**  
   Anchor on the gift icon hit and second-match the adjacent portrait (`Operators/<Name>.png`, also `green_mask`).  
   On success, temporarily narrow the dialogue-stage operator-name OCR whitelist to that operator’s localized names.  
   This lives in `Operator/Operator.json` with one entry per operator; new operators must be added here.

    > **Example**: If the list second-match hits `Operators/Gilberta.png`, the whitelist narrows to Gilberta’s names only. While waiting for dialogue in the open world, the task clicks only when both the dialogue icon and name OCR match; Perlica, Yvonne, etc. on the field will **not** be clicked by mistake.

3. **Step 3: confirm selection state**  
   Reuse [selection-state verification](#selection-state-verification): list row highlighted and slot `1`, then click the yellow confirm button to summon.

4. **Step 4: second check in dialogue**  
   After the operator arrives, require both **dialogue icon** and **operator name OCR** before interacting.  
   This blocks “wrong operator summoned” even if the gift row was clicked correctly.

Portrait templates must be processed with `fill_gift_operator_green_box.py` (green outline + top-right mask); otherwise `green_mask` matching is unstable. Win32 and ADB each have their own image set and must be processed separately.

If no gift row is visible on the current screen, the list scrolls at most twice; if still not found, the flow falls through to “no selectable operator”, which can end a full **Accept all gifts** round.

### What if the dialogue button is not found after summoning?

After confirm summon, the task waits for the operator and tries to click the dialogue entry. If no interactive dialogue button is visible, it does not wait indefinitely; it runs **position correction fallback** in `GiftOperatorNavigation.json`.

Correction uses three fixed presets, each tried once (counters reset at task start to avoid carry-over):

| Order | Facing              | Move target    |
| ----- | ------------------- | -------------- |
| 1     | West (270°)         | (186.6, 175.0) |
| 2     | North (0°)          | (188.0, 175.3) |
| 3     | East (90°)          | (188.6, 176.2) |

Each preset: turn → short move → wait for the character to stop, then retry dialogue-button search.

If all three fail, the task errors with “failed to find operator for recognition” and keeps a screenshot for debugging. Common causes: operator spawned outside preset areas, or post-summon position deviates from template ROIs.

Two similar retries handle click offset while the operator walks over:

- Dialogue button found but dialogue did not open → click again in place.
- In dialogue but right-side action buttons not yet visible → skip button self-retries once, then wait for give / receive / affinity-maxed buttons.

### Default mode selection vs receive-only

**Any** does not use portraits: switch to ascending trust → pick the first three rows that are **below max trust and not yet selected**.  
**Specific operator** is like receive-only step 2 (portrait match in the list) but does not require finding a gift icon first.
