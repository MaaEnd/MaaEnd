# Developer Guide - Credit Shop Maintenance

This document describes the file layout and execution flow of `CreditShopping`.  
Purchase conditions are not a few independent switches but a whole filter chain running from `Item/` to `Flow/`; maintenance must be understood along that chain.  
This document was updated on November 9, 2026.

## File Paths

```text
CreditShopping/
├── Flow/                  # Main shopping flow
│   ├── ApplyPriorityItems.json # Selected items → per-tier item_ids (Custom: CreditShoppingApplyPriorityItemsAction)
│   ├── Entry.json         # Tab detection + enter scan loop
│   ├── Scan.json          # Shelf scan gate (triggers the buy decision loop)
│   ├── BuyAction.json     # Three-tier purchase / forced purchase / prudent refresh / attempts exhausted
│   ├── BuyItem.json       # Purchase confirm / reserve threshold check
│   └── Special.json       # Unable to buy while refreshing / refresh items
├── Credit/                # Credit management
│   ├── Reserve.json       # Reserve threshold check + current credit OCR
│   ├── AutoGetCredits.json# Automatic credit replenishment (jump to base when unaffordable)
│   └── ClaimCredit.json   # Claim pending credits (includes IMS A3 add)
├── Item/                  # Shelf item recognition (recognition chain)
│   ├── Priority1Items.json# Purchase Item Option 1: IconRecognition item_ids
│   ├── Priority2Items.json# Purchase Item Option 2: IconRecognition item_ids
│   ├── Priority3Items.json# Purchase Item Option 3: IconRecognition item_ids
│   ├── AnyItems.json      # Any purchasable item: full-catalog IconRecognition
│   └── ShelfChecks.json   # Not sold out / affordable / discount (offsets from the item box)
├── GoToShop.json           # Navigate to shop and switch to credit exchange tab
└── Reflash.json            # Refresh button / cost / used up / unable to refresh
```

Other related files:

| Path | Function |
| --------------------------------------------------------- | -------------------------------------------------------------------- |
| `assets/interface.json` | Task mounting (`other_menu` group; also in the `DailyFull` / `QuickDaily` presets) |
| `assets/tasks/CreditShopping.json` | Task entry, three-tier purchases, reserve threshold, refresh options |
| `agent/go-service/creditshopping/` | Copies selected items into each tier's `IconRecognition` `item_ids` (the only CustomAction) |
| `assets/resource/pipeline/DijiangRewards/Entry.json` (`NeedCredit`), `ReceptionRoom.json` | Return to base to replenish credit when insufficient |
| `assets/locales/interface/*.json` | Task, option, and focus text |

## Execution Flow

1. Enter the credit exchange tab; if not in the shop, navigate first (`GoToShop.json`), then [claim pending credits](#claiming-credits) (`Credit/ClaimCredit.json`).
2. Before entering the scan loop, [write each tier's selected items into `item_ids` once](#item-selection-and-initialization) (`Flow/ApplyPriorityItems.json` + Go).
3. In each round, scan the current shelf and evaluate in a fixed priority order (`Flow/Scan.json` → `Flow/BuyAction.json`):
    - A tier has "Automatically collect credits" enabled and its target item [is unaffordable (not enough credit)](#automatic-credit-replenishment) → jump to base to replenish credit and return; tiers with it disabled skip this check.
    - Whether it matches [Purchase Item Option 1 / 2 / 3](#three-tier-purchase-priority) → confirm purchase directly.
    - Whether the current credit is [below the reserve threshold](#credit-point-reserve-threshold) → end the task.
    - Whether refresh attempts are exhausted, whether to trigger [prudent refresh switching to direct purchase](#force-strategy-and-refresh).
    - Follow the [force strategy](#force-strategy-and-refresh) to purchase any affordable item / refresh the shelf / end directly.
4. After confirm, enter the reward UI: IMS A3 adds cultivation items and announces them, then close and continue scanning. Force-buy checks reserve via "held − price" before confirm.

> The `next` order of the scan loop defines the business priority; to change behavior, examine the entire chain, not just a single recognizer.

## Special Handling

### Claiming Credits

Implemented in `Credit/ClaimCredit.json`. After entering the credit exchange, first attempt to claim pending credits; if none are available, proceed directly to scanning without blocking the main flow.

### Item Selection and Initialization

1. The user selects items in the three tier checkboxes; each case's `pipeline_override` writes one `attach` entry (`priority1__<CaseID>: <CaseID>`) onto the `CreditShoppingApplyPriorityItems` node.
2. The CustomAction in `Flow/ApplyPriorityItems.json` reads that node's `attach`, maps each CaseID to the catalog `item_ids` used by IconRecognition, and overwrites `CreditShoppingPriority1/2/3Item` via `OverridePipeline`. Go only performs this copy; it takes no part in purchase decisions.
3. All three checkboxes set `min_count: 1`, so the UI requires at least one selected item per tier. A tier with nothing selected is not overwritten and keeps the Pipeline default `item_ids` (`Priority1` defaults to `item_gold`).
4. The array order of `item_ids` follows `attach` iteration order and does not affect recognition (the nodes use `order_by: natural`, i.e. shelf position).

### Item Recognition Chain

Implemented in `Item/` plus `Item/ShelfChecks.json`; recognition has two layers:

1. **Items**: `IconRecognition` (`grid_type: credit_trade`) recognizes the card row inside `roi` and returns `cell_box` / `row` / `column` / `item_id` per cell. `CreditShoppingPriority{N}Item` only matches that tier's `item_ids`; `CreditShoppingShelfAnyItem` matches the full catalog. Both use `index: 0`, i.e. the first hit in natural shelf order.
2. **Checks**: `ShelfChecks.json` offsets from the item box to derive
    - not sold out `*NotSoldOut` (ColorMatch, grayscale)
    - affordable `*CanAfford` / unaffordable `*CanNotAfford` (price text color)
    - discount `*Discount` (OCR of the badge)

`CreditShoppingBuyPriority{N}` is an And chain: item → not sold out → affordable → discount → reserve gate, with `box_index: 0` so the click lands on the item cell center. **If a preceding layer misses, the whole chain fails**; troubleshoot in order item → not sold out → affordable → discount.

Controller differences are maintained separately: the ADB IconRecognition ROI lives in `assets/resource_adb/pipeline/CreditShopping/IconRecognitionRoi.json`, cloud color thresholds in `assets/resource_cloud/pipeline/CreditShopping/Item.json`. Neither the Pipeline nor Go pins a ROI, so region changes belong in those resource overrides.

### Three-Tier Purchase Priority

The structure is the same for all three tiers, but the default strategies differ (`CreditShopping.json`):

| Tier | Default "Unconditional Purchase" | Default "Automatically collect credits" | Typical Use Case |
| ---------- | -------------------------------- | ------------------------------- | ---------------------------------------------------------------- |
| Purchase Item Option 1 | On | On | Essential items worth buying even when credit is nearly depleted |
| Purchase Item Option 2 | Off | Off | Only buy if reserve threshold is met |
| Purchase Item Option 3 | Off | Off | Same as above, lower priority |

Each tier can be independently configured: selected items (at least 1 per tier), minimum discount, whether to skip the reserve threshold, whether to allow credit replenishment when unaffordable.
Only after all three tiers fail to match does the unified reserve threshold node handle the fallback exit.

### Credit Point Reserve Threshold

`CreditShoppingReserve` modifies the reserve threshold expression (`Credit/Reserve.json`).
Whether each tier is subject to the threshold limit is controlled by the tier's "Unconditional Purchase" switch, not by the scan `next` order.
To make a tier ignore the threshold, modify the corresponding tier's "Unconditional Purchase"; do not reinsert the threshold check into the middle of the purchase chain.

### Automatic Credit Replenishment

If a tier has "Automatically collect credits" enabled, and the tier's affordable chain matches, it jumps via `GoToNeedCredit` (`Credit/AutoGetCredits.json`) to the `NeedCredit` node (`DijiangRewards/Entry.json`):

1. Return to the base reception room, and according to the configuration, gift clues or initiate clue exchange.
2. The number of one-by-one gifts is controlled by `CreditShoppingClueSend` (`0` = none; with "Give Away All Duplicates" on, the one-click give still runs).
3. The inventory lower limit for gift-able clues is controlled by `CreditShoppingClueStockLimit` (default is to keep 2, i.e., only gift if inventory ≥ 3).
4. "Give Away All Duplicates" is controlled by `CreditShoppingQuickGiveDuplicateClues` (off by default; when on, prefer the one-click action after entering the Send Clues screen).

Each tier has its own independent switch; insufficient refresh cost **will not** trigger credit replenishment (the old `RefreshGetCredits` has been removed).

### Discount Options

The discount option for each tier modifies the corresponding tier's discount OCR `expected`, or switches to the "Any" case.
Numeric tiers only rewrite `expected` (e.g. "-25% and above"); the "Any" case replaces the discount node with `DirectHit`, so **no discount badge is checked** — items without a discount are purchased too, matching the "Any" semantics of `AutoStockStaple`.
Discount rules must cover both the affordable and unaffordable sides.

### Force Strategy and Refresh

The fallback after all three tiers fail to match is determined by `CreditShoppingForce` (`Flow/BuyAction.json` + `Flow/Special.json`):

| Strategy | Behavior |
| ---------------- | ------------------------------------------------------------ |
| Exit | Do not purchase any item, do not refresh, end directly |
| Buy any item | Purchase any affordable, not sold-out item; before confirm, `BuyItemBelowReserve` checks reserve via "held − price", and cancels/stops if insufficient |
| Strictly buy by whitelist and enable refresh | Attempt to refresh the shelf; can expand to "Prudent refresh" and "Check price before direct buy" |

**Prudent refresh**: If "Current credit − Refresh cost < Prudent refresh threshold" and there are still purchasable items on the shelf, then do not refresh, but directly purchase instead.
This threshold and "Reserved Credits" are two independent conditions; do not mix them.

**Check price before direct buy** (Refresh sub-option, default on): When enabled, refresh-exhausted / prudent direct buys check reserve via "held − price" before confirm and cancel/stop if insufficient; when disabled, buy first, then the "Reserved Credits" node decides whether to stop.

## Paths to Modify When Adding New Items

1. `assets/tasks/CreditShopping.json` — Add a case in the corresponding tier's checkbox; its `pipeline_override` writes one `attach` entry (`priority<N>__<CaseID>`)
2. `assets/locales/interface/*.json` — `option.CreditShoppingItems.cases.*.label`

Once selected, Go copies it into `item_ids` and it becomes purchasable; reward announcements and quantity adds are handled by IMS A3 (`ClaimCredit.json`). No per-item popup OCR is needed.
After adding a case, remember to check whether `default_case` also needs to include the new item.

## Maintenance Key Points

| Phenomenon | Priority Check |
| ---------------------------------------- | -------------------------------------------------------------------------------- |
| Target item not recognized | The tier's `item_ids` (node after the Go override) and the `IconRecognition` ROI |
| Affordable but not purchasing | The tier's reserve threshold / unconditional purchase switch |
| Unaffordable but not replenishing credit | The tier's AutoGetCredits switch; the unaffordable side's discount and price color thresholds |
| Abnormal refresh behavior | `CreditShoppingForce`; prudent refresh threshold |
| Inconsistent behavior between options | `CreditShopping.json`'s `pipeline_override` and scan `next` order |

Maintenance location is done in four layers: Entry (`GoToShop.json`) → Scan Decision (`Flow/`) → Recognition Chain (`Item/` + `ShelfChecks.json`) → Parameter Assembly (task options + Go).
