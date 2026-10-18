# Development Manual - Gift to Operator Maintenance Documentation

This document explains the file distribution and two execution routes of `GiftOperator`.  
This documentation was last updated on October 18, 2026.

## File Paths

| Path | Purpose |
| ------------------------------------------------------------------- | --------------------------------------------- |
| `assets/interface.json` | Task mounting (`dijiang_ship` group) |
| `assets/tasks/GiftOperator.json` | Task entry and interface options |
| `assets/resource/pipeline/GiftOperator/GiftOperatorMain.json` | Entry, Dijiang ship location |
| `assets/resource/pipeline/GiftOperator/GiftOperatorNavigation.json` | Pathfinding and Operator Contact Panel interaction |
| `assets/resource/pipeline/GiftOperator/GiftOperatorContact.json` | Contact interface operator selection |
| `assets/resource/pipeline/GiftOperator/GiftOperatorGiftFlow.json` | Gift giving / receiving during dialogue |
| `assets/resource/pipeline/GiftOperator/GiftOperatorBagFull.json` | Bag full handling |
| `assets/resource/pipeline/GiftOperator/Operator/Operator.json` | Operator identification for "Only Receive Gifts" mode |
| `assets/resource/image/GiftOperator/` | Win32 recognition images |
| `assets/resource_adb/image/GiftOperator/` | ADB recognition images |
| `assets/resource_adb/pipeline/GiftOperator/` | ADB Pipeline mirror |
| `tools/gift_operator/fill_gift_operator_green_box.py` | Operator avatar green_mask formatting |
| `tools/pipeline-generate/GiftOperator/generate.mjs` | Add missing operator configuration from zmdmap data and invoke avatar generation |
| `tools/gift_operator/build_avatars.py` | Generate missing PC / ADB avatar templates from CharIcon images |
| `assets/locales/interface/*.json` | Task, option, and operator name text |

## Incremental Generation for New Operators

Run `pnpm generate:GiftOperator`: the existing zmdmap downloader updates `tools/zmdmap/data/sell_product.json`, then the generator reads its `operators`, excludes the male and female Endministrators, and adds the configuration and templates listed below for new operators. The Pipeline data synchronization job in `.github/workflows/sync-zmdmap.yml` also runs this command.

Existing options, nodes, translations, and templates are preserved; only missing entries are added. Existing options are never renamed, reordered, or removed. The existing configuration is the baseline, with no separate operator ID mapping. If an entire baseline file is missing, restore it from Git before running the generator. Source avatars are downloaded on demand without requiring a local image cache.

Names come from `sell_product.operators`; source images come from `https://voice.fz.wiki/assets/beyond/dynamicassets/gameplay/ui/sprites/charicon/icon_<charId>.png@raw`. Each 152×212 PNG is alpha-composited onto a light gray background, bilinearly scaled, and cropped with a fixed offset: PC uses scale 0.55, horizontal offset −1 pixel, and output 80×72; ADB uses scale 0.6875, horizontal offset −1.25 pixels, and output 100×90. Both templates are generated directly from the source. Fixed green masks cover the information icon, gift icon, upper-right status icon, and borders, with no per-operator tuning.

Download, decoding, or source dimension failures stop generation before configuration is written; check the upstream image and crop parameters before rerunning. The fixed ADB parameters have been verified on one real 1280×720 screenshot: all 12 fully visible operators matched correctly, with a minimum score of 0.904 and no incorrect operator hits. Si is absent from that image; a positive ADB sample for Si and the complete interaction flow still need device validation. In the table below, `<Name>` follows the existing option naming convention; new names default to a value derived from the upstream English name.

| # | Path | Description |
| --- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1 | `assets/resource/image/GiftOperator/Operators/<Name>.png` | PC operator avatar template, generated with green_mask included |
| 2 | `assets/resource_adb/image/GiftOperator/Operators/<Name>.png` | ADB operator avatar template, generated with green_mask included |
| 3 | `assets/tasks/GiftOperator.json` → `SelectOperator` | Add a case to provide the operator in the UI and supply the avatar template and name whitelist for the "Specific Operator" route |
| 4 | `assets/resource/pipeline/GiftOperator/Operator/Operator.json` | Avatar re-matching and name OCR whitelist for each operator in "Only Receive Gifts" mode |
| 5 | `assets/resource/pipeline/GiftOperator/GiftOperatorContact.json` → `GiftOperatorSelectGiftOp.next` | Append `GiftOperatorSelect_<Name>` to the `next` array at the "Only Receive Gifts" operator selection node; otherwise, the operator node will not be triggered |
| 6 | `assets/locales/interface/*.json` → `operator.<Name>` | Operator display name for each language |

## Route 1: Default (Give + Receive)

Corresponds to the "Only Receive Gifts" option being disabled. Configurable gift recipient and quantity.

1. Store bag before starting the task, then enter the Dijiang ship bridge world.
2. Navigate to the Operator Contact Panel and open the contact interface.
3. Select operators (must pass [Selection State Verification](#selection-state-verification) after clicking; implementation in `GiftOperatorContact.json`):
    - **Any**: Switch to trust ascending order, select three consecutive operators (including those at max trust); verify sequence number 1 → 2 → 3 for each click, confirm the call only after all three are selected.
    - **Specific Operator**: Match the target operator in the list using the avatar template, then verify the selection state before calling.
4. Confirm the call; if the operator is not in position, fall back to [stepping in preset directions](#after-calling-operator-what-to-do-if-dialogue-button-not-found) (implementation in `GiftOperatorNavigation.json`).
5. Wait for the operator to appear, enter dialogue.
6. Handle dialogue based on priority:
    - Can give gift → Select gift (the highlight color is verified after selection, see [Selection State Verification](#selection-state-verification); implementation in `GiftOperatorGiftFlow.json`), confirm giving, skip dialogue, leave. Operators at max trust can still receive gifts.
    - Can receive gift → Collect gift, skip dialogue, leave.
7. The number of gifts is controlled by the "Gift Count" option: `GiftOperatorClickGift` clicks the gift cell that many times before a single confirmation; the default is 1.

## Route 2: Only Receive Gifts

Corresponds to the "Only Receive Gifts" option being enabled. No longer actively gives gifts, only receives gifts from operators.

1. Similarly, store the bag first, then navigate to the Operator Contact Panel.
2. In the contact list, [identify operators with gift icons](#receive-mode-how-to-correctly-select-the-target-operator) (implementation in `GiftOperatorContact.json` and `Operator/Operator.json`), rather than selecting by trust order or specific operator.
3. Confirm the call, enter dialogue, prioritize clicking "Accept Gift".
4. After receiving:
    - **Accept all gifts** disabled → Skip dialogue, then leave, task ends.
    - **Accept all gifts** enabled → Return to the start of the task, continue finding the next operator with a gift; only leave when no selectable operators remain in the contact interface.
5. If the bag is full, prompt and end the task.

## Special Handling

### Selection State Verification

In this task, "clicking once" does not equal "selected". Operator selection at the Operator Contact Panel uses a **three-layer serial judgment** to avoid empty or off-target clicks; gift selection in the gift interface currently keeps only the first layer (see below):

```text
Selection highlight color → Text background color within the highlighted area → OCR read key text
```

#### Operator Selection at the Operator Contact Panel

Implementation is in `GiftOperatorContact.json`. After clicking a list row, use `And` to simultaneously satisfy:

1. **Tag Highlight Color**: Identify the HSV color block of the selected state in that row (yellow label background).
2. **Sequence Number Text Background**: Use the hit area from the previous step as an anchor, then identify the text background color of the sequence number.
3. **Sequence Number OCR**: Read `1` / `2` / `3` in the text area, corresponding to the current operator to be selected.

The "Any" route gradually confirms that the first, second, and third operators are all queued; the "Specific Operator" and receive gift routes verify sequence number `1` is correct after clicking the target, then click confirm call.

#### Gift Interface Gift Selection

Implementation is in `GiftOperatorGiftFlow.json`; unlike operator selection, the quantity OCR happens before the click:

1. First, color matching plus a quantity OCR (`[1-9]\d*`) locates a clickable item in the bottom gift bar, and the quantity position is clicked.
2. After the click, only the gift cell's selection highlight color (`GiftOperatorGiftSelectedLabelColor`) is verified.
3. The text-color and OCR (`\d+`) layers are commented out in `GiftOperatorGiftSelected`; once the highlight matches, the flow clicks "Confirm".

When maintaining, if selection state recognition drifts, prioritize checking the color thresholds and OCR area offsets for these three layers; both operator and gift locations should be checked in parallel.

### Receive Mode: How to Correctly Select the Target Operator

Receive mode cannot rely on OCR of operator names to directly click the list. Instead, it **first finds the gift, then recognizes the avatar, and finally verifies the name**. Logic is distributed in `GiftOperatorContact.json` and `Operator/Operator.json`.

1. **Step 1: Locate the "Row with a Gift"**  
   In the contact list area, use `Gift.png` template matching to find the gift icon (`green_mask`). After a hit, click 40 px to the right of it to select that operator row.

2. **Step 2: Confirm Which Operator**  
   Use the gift icon hit position as an anchor, and in the adjacent area, perform secondary matching of that operator's avatar (`Operators/<Name>.png`, also `green_mask`).  
   After a successful match, temporarily change the operator name OCR whitelist used in subsequent dialogue stages to this operator's multilingual name.  
   This step is written in `Operator/Operator.json`, one entry per operator; must be maintained synchronously when adding new operators.

    > **Example**: If `Operators/Gilberta.png` is secondarily matched next to a gift row in the contact list, the whitelist is narrowed to "洁尔佩塔 / Gilberta / …" for only that operator. After the call, when waiting for dialogue in the world, it must simultaneously see the dialogue icon and the name OCR hit that whitelist before clicking; if other operators like Perlica or Yvonne appear on the field, the names don't match, **no mis-clicks**.

3. **Step 3: Confirm Selection State**  
   Reuse the [Selection State Verification](#selection-state-verification) logic above, confirm the list row is highlighted and the sequence number is `1`, then click the yellow confirm button to call.

4. **Step 4: Secondary Verification During Dialogue Stage**  
   After the operator arrives, simultaneously recognize the "dialogue icon" and "operator name OCR"; interaction is initiated only if both hit.  
   This way, even if a gift row is clicked in the list, another check prevents "calling the wrong person" before dialogue.

Generated avatar templates already include green masks and do not need `fill_gift_operator_green_box.py`. Manually captured templates still use that script (green border + upper-right mask); Win32 and ADB each have their own set of images and must be processed separately.

On ADB, both the specific-operator node `GiftOperatorSelectSpecifiedOp` and the gift-receiving node `GiftOperatorSelectGiftOp` search the list within `[520, 130, 690, 390]`, covering the first row and both edges. Avatars retain the fixed generation parameters; enlarging the ROI does not require recropping templates.

If the target operator is not found on the current screen, the list scrolls up to 4 times, returning to contact-screen recognition and another search after each swipe. If still not found, it falls into "no selectable operator", which can serve as a round-end condition when "Accept all gifts" is enabled.

### After Calling Operator: What to Do If Dialogue Button Not Found

After call confirmation, the task first waits for the operator to appear and attempts to click the dialogue entry. If the interactive dialogue button is still not visible on screen, it won't wait indefinitely but enters **Position Correction Fallback**, logic in `GiftOperatorNavigation.json`.

The fallback is driven by `GiftOperatorMoveAhead`: it first uses `MapLocateAssertLocation` to confirm the character is still near the Operator Contact Panel, then turns in a fixed order; the number of turns per heading is the `max_hit` of the corresponding node (the counts are reset at the start of the task to avoid residuals from the previous round):

| Order | Node | Orientation | Count |
| ----- | ----------------------------- | ------------------------ | ----- |
| 1 | `GiftOperatorTurnEast` | East (90°) | 2 |
| 2 | `GiftOperatorTurnNorthwest` | Northwest (315°) | 1 |
| 3 | `GiftOperatorTurnNorthByWest` | 15° west of north (345°) | 1 |
| 4 | `GiftOperatorTurnWest` | West (270°) | 2 |
| 5 | `GiftOperatorTurnSoutheast` | Southeast (135°) | 2 |

Each turn is a single `HEADING` waypoint: the camera turns to the given heading and `W` is pressed once, so the character also takes a small step that way; the 8 steps together make a small loop starting from the Operator Contact Panel. After each step the flow jumps back (`[JumpBack]`) to the post-call check and looks for the dialogue button again; once it is found, the dialogue starts and no more turns are made.

`GiftOperatorMoveAhead` also has `max_hit` 8, equal to the sum of the counts above: once the turns are used up it stops matching as well, so the flow can fall through to the failure branch. The file still contains the fixed-position move nodes `GiftOperatorMoveLocation{1,2,3}Move`, but they are not in any `next` list and do not run.

If all 8 steps are used and the button is still not found, or the character is no longer near the Operator Contact Panel, the flow falls into `GiftOperatorMoveFail`: the task ends with an error and prompts "Failed to navigate or locate operator", while retaining a screenshot for troubleshooting. Common causes are the operator spawning outside the area these steps cover, or the position after the call deviating too much from the template ROI.

Two other similar retries handle click offsets caused by the operator walking over:

- Dialogue button found but no dialogue entered after clicking → Step back one step first (this happens only once), then find and click the dialogue button again.
- Dialogue entered but right-side action buttons not yet appeared → The skip button's `next` includes itself, so it keeps clicking until the give/receive buttons appear; with "Only Receive Gifts" enabled, `next` does not include itself, so it clicks only once.

### Differences in Default Mode Operator Selection (Compared to Receive)

The "Any" route doesn't rely on avatars, but instead: switches to trust ascending order → from top to bottom, finds **unselected** rows, and clicks three consecutively.  
The "Specific Operator" route is similar to receive mode's second step, directly matching in the list using the avatar template, but without needing to first find the gift icon.
