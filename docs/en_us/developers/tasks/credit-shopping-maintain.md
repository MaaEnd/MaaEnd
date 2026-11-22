# Developer Guide - Credit Shop Maintenance

This document describes the file layout and execution flow of `CreditShopping`.  
Purchase conditions are not a few independent switches but a whole filter chain running from `Item/` to `Flow/`; maintenance must be understood along that chain.  
This document was updated on November 22, 2026.

## File Paths

```text
CreditShopping/
├── Flow/                  # Main shopping flow
│   ├── Entry.json         # Tab detection + enter scan loop
│   ├── Scan.json          # Shelf scan gate (credit readable + triggers the buy decision loop)
│   ├── BuyAction.json     # Three-tier purchase / forced purchase / buy instead when balance runs short / attempts exhausted
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
│   └── ShelfChecks.json   # Affordable / discount (offsets from the item box)
├── GoToShop.json           # Navigate to shop and switch to credit exchange tab
└── Reflash.json            # Refresh cost / used up / unable to refresh
```

Other related files:

| Path | Function |
| --------------------------------------------------------- | -------------------------------------------------------------------- |
| `assets/interface.json` | Task mounting (`other_menu` group; also in the `DailyFull` / `QuickDaily` presets) |
| `assets/tasks/CreditShopping.json` | Task entry, three-tier purchases, reserve threshold, refresh options |
| `assets/resource/pipeline/DijiangRewards/Entry.json` (`NeedCredit`), `ReceptionRoom.json` | Return to base to replenish credit when insufficient |
| `assets/locales/interface/*.json` | Task, option, and focus text |

## Execution Flow

1. Enter the credit exchange tab; if not in the shop, navigate first (`GoToShop.json`), then [claim pending credits](#claiming-credits) (`Credit/ClaimCredit.json`).
2. Selected items are applied directly to `item_ids` through each tier item node's [`attach`](#item-selection), so no separate initialization step runs before the scan loop.
3. In each round, scan the current shelf and evaluate in a fixed priority order (`Flow/Scan.json` → `Flow/BuyAction.json`). The scan itself requires [readable credits](#scan-gate); when they cannot be read, that round makes no purchase or refresh decision:
    - A tier has "Automatically collect credits" enabled and its target item [is unaffordable (not enough credit)](#automatic-credit-replenishment) → jump to base to replenish credit and return; tiers with it disabled skip this check.
    - Whether it matches [Purchase Item Option 1 / 2 / 3](#three-tier-purchase-priority) → confirm purchase directly.
    - Whether the current credit is [below the reserve threshold](#credit-point-reserve-threshold) → end the task.
    - Whether refresh attempts are exhausted, and whether the [balance check before refreshing](#force-strategy-and-refresh) hits.
    - Follow the [force strategy](#force-strategy-and-refresh) to purchase any affordable item / refresh the shelf / end directly.
4. After confirm, enter the reward UI: IMS A3 adds cultivation items and announces them, then close and continue scanning. Force-buy checks reserve via "held − price" before confirm.

> The `next` order of the scan loop defines the business priority; to change behavior, examine the entire chain, not just a single recognizer.

## Special Handling

### Scan Gate

`CreditShoppingScanItem` (`Flow/Scan.json`) is the only entry point for purchase and refresh decisions. Its recognition requires both of: the credit exchange tab is selected (`CreditShoppingScanItemTabSelected`), and the current credits are readable (`CreditShoppingScanCreditGate`, which wraps `CreditShoppingReserveCreditOCRInternal`).

The credit gate is its own node rather than part of `CreditShoppingScanItem`'s recognition chain. When the current credits cannot be read, every downstream node that depends on the balance (reserve threshold, tier gates, balance check before refreshing, price check before a forced buy) fails to resolve its expression and does not match, so the flow falls through to the fallback branches. Among them, `CreditShoppingRefreshCountReached` (buy the rest once refresh attempts are used up) carries no balance condition, so it clicks an item directly, and the "held − price" check before confirming cannot be evaluated either, so the purchase bypasses the reserve threshold. With the gate in place, unreadable credits mean no purchase or refresh decision at all: the node retries in place, and if the credits stay unreadable the task ends on a recognition timeout with the log stopping at that gate node.

The scan gate does **not** require the refresh button to be recognizable: refresh cost and exhausted refresh attempts are checked by the nodes that need them (`CreditShoppingRefreshCountReached` matches `RefreshUsedUp`, `RefreshItem` matches `RefreshCost`), so an unreadable refresh area does not block the purchase decision.

### Claiming Credits

Implemented in `Credit/ClaimCredit.json`. After entering the credit exchange, first attempt to claim pending credits; if none are available, proceed directly to scanning without blocking the main flow.

### Item Selection

1. The user selects items in the three tier checkboxes; each case's `pipeline_override` writes one `attach` entry (`IconRecognition.item_ids.<item_id>: true`) onto that tier's `CreditShoppingPriority{N}Item` node.
2. `IconRecognition` reads the **calling node's** `attach` at recognition time and replaces `IconRecognition.item_ids.*` with that tier's `item_ids` array. Selection therefore takes effect immediately; no CustomAction or startup `OverridePipeline` is needed.
3. `attach` is a dict merge, so keys from multiple cases accumulate. A tier with nothing selected is not overridden and keeps the Pipeline default `item_ids` (`Priority1` defaults to `item_gold`).
4. `<item_id>` must match a top-level key of `assets/data/IconRecognition/recognition_items.json`; a typo makes that tier's recognition fail on every attempt with an `unknown item_id` error in the log, but the task itself keeps running, so the tier simply never buys anything.
5. All three checkboxes set `min_count: 1`, so the UI requires at least one selected item per tier; the array order of `item_ids` follows the `attach` merge order and does not affect recognition (the nodes use `order_by: natural`, i.e. shelf position).

### Item Recognition Chain

Implemented in `Item/` plus `Item/ShelfChecks.json`; recognition has two layers:

1. **Items**: `IconRecognition` (`grid_type: credit_trade`) recognizes the card row inside `roi` and returns `cell_box` / `row` / `column` / `item_id` per cell. `CreditShoppingPriority{N}Item` only matches that tier's `item_ids`; `CreditShoppingShelfAnyItem` matches the full catalog. Both use `index: 0`, i.e. the first hit in natural shelf order.
2. **Checks**: `ShelfChecks.json` offsets from the item box to derive
    - affordable `*CanAfford` / unaffordable `*CanNotAfford` (price text color)
    - discount `*Discount` (OCR of the badge)

Sold-out cards (dimmed card + "Sold Out") are not matched by IconRecognition, so the Pipeline no longer checks sold-out state separately.

`CreditShoppingBuyPriority{N}` is an And chain: item → affordable → discount → reserve gate. The click takes the discount node's box (`box_index: 2`) and offsets it with `target_offset: [-45, 58, 0, 0]` into the item icon area, so **the discount node's box must stay inside the item cell** — the "Any" case, which switches to an always-true color match, has to set `roi` in the same override (see [Discount Options](#discount-options)). **If a preceding layer misses, the whole chain fails**; troubleshoot in order item → affordable → discount.

Controller differences are maintained separately: the ADB IconRecognition ROI lives in `assets/resource_adb/pipeline/CreditShopping/IconRecognitionRoi.json`, cloud color thresholds in `assets/resource_cloud/pipeline/CreditShopping/Item.json`. The Pipeline does not pin a ROI, so region changes belong in those resource overrides.

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
Numeric tiers only rewrite `expected` (e.g. "-25% and above"); the "Any" case **stops checking the discount badge** — items without a discount are purchased too, matching the "Any" semantics of `AutoStockStaple`. It writes a full-range `ColorMatch` (`method: 4`, `lower: [0, 0, 0]`, `upper: [255, 255, 255]`) and **must set `roi` and `roi_offset` in the same override**: when an override changes the `recognition` type, MaaFramework re-parses the recognition parameters for the new type, so the node's own `roi` / `roi_offset` (including the `assets/resource_adb` controller override) are not inherited; a missing `roi` matches the whole frame, the box degrades to full screen, and the `box_index: 2` offset click becomes a random point inside that large box.
Discount rules must cover both the affordable and unaffordable sides (`*Discount` together with `*DiscountCannotAfford`).

### Force Strategy and Refresh

The fallback after all three tiers fail to match is determined by `CreditShoppingForce` (`Flow/BuyAction.json` + `Flow/Special.json`):

| Strategy | Behavior |
| ---------------- | ------------------------------------------------------------ |
| Exit | Do not purchase any item, do not refresh, end directly |
| Buy any item | Purchase any affordable item (sold-out cards are not recognized); before confirm, `BuyItemBelowReserve` checks reserve via "held − price", and cancels/stops if insufficient |
| Strictly buy by whitelist and enable refresh | Attempt to refresh the shelf; can expand to "Check balance before refreshing" and "Check price before direct buy" |

**Check balance before refreshing** (Refresh sub-option, default off): the condition is "Current credit − Refresh cost < Balance threshold", decided by the single gate node `CreditShoppingRefreshBalanceGate`; the threshold comes from `CreditShoppingRefreshBalanceThreshold` (default 400). Which route it takes is decided by the nested "Buy any item when the balance runs short" switch, which rewrites that gate node's `next`:

- Off (default): `next` points straight at `CreditShoppingNothingToBuy`, ending the task; `CreditShoppingRefreshReserveCreditGate` then also requires the refresh cost and the current credits to be readable, otherwise it does not refresh.
- On: `next` points at `CreditShoppingPrudentRefresh` and then `RefreshItem` — `CreditShoppingPrudentRefresh` only clicks any affordable item on the shelf (non-discounted included), and only when nothing is affordable does it continue to refresh.

Both routes share one gate node and one balance threshold. It and "Reserved Credits" are two independent conditions; do not mix them.

**Check price before direct buy** (Refresh sub-option, default on): When enabled, refresh-exhausted / balance-short direct buys check reserve via "held − price" before confirm and cancel/stop if insufficient; when disabled, buy first, then the "Reserved Credits" node decides whether to stop.

## Paths to Modify When Adding New Items

1. `assets/tasks/CreditShopping.json` — Add a case in the corresponding tier's checkbox; `icon` points to `resource/image/UI/Item/<item_id>.png`, and `pipeline_override` writes one `attach` entry (`IconRecognition.item_ids.<item_id>: true`) onto that tier's `CreditShoppingPriority<N>Item`
2. `assets/locales/interface/*.json` — `option.CreditShoppingItems.cases.*.label`

`<item_id>` in `attach` must match both the `icon` file name and a top-level key of `assets/data/IconRecognition/recognition_items.json`. Once selected, the item becomes purchasable; reward announcements and quantity adds are handled by IMS A3 (`ClaimCredit.json`). No per-item popup OCR is needed.
After adding a case, remember to check whether `default_case` also needs to include the new item.

## Maintenance Key Points

| Phenomenon | Priority Check |
| ---------------------------------------- | -------------------------------------------------------------------------------- |
| Target item not recognized | The tier item node's `attach` (`IconRecognition.item_ids.*`) and the `IconRecognition` ROI |
| Affordable but not purchasing | The tier's reserve threshold / unconditional purchase switch |
| Unaffordable but not replenishing credit | The tier's AutoGetCredits switch; the unaffordable side's discount and price color thresholds |
| Abnormal refresh behavior | `CreditShoppingForce`; `CreditShoppingRefreshBalanceGate` (including the balance threshold) |
| Scan stalls until timeout | [Credit gate](#scan-gate) `CreditShoppingScanCreditGate`: color ROI and OCR of `CreditShoppingReserveCreditOCRInternal` |
| Inconsistent behavior between options | `CreditShopping.json`'s `pipeline_override` and scan `next` order |

Maintenance location is done in four layers: Entry (`GoToShop.json`) → Scan Decision (`Flow/`) → Recognition Chain (`Item/` + `ShelfChecks.json`) → Parameter Assembly (task options `attach`).
