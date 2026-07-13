# Development Manual - Credit Shop Maintenance Documentation

This document describes the file distribution and execution flow of `CreditShopping`.  
Purchase conditions are not independent switches but an entire screening chain from `Item/` to `Flow/`; maintenance requires understanding this chain.  
This document was updated on July 13, 2026.

## File Paths

```
CreditShopping/
├── Flow/                  # Main shopping flow
│   ├── Entry.json         # Entry detection + whitelist init
│   ├── Scan.json          # Shelf scan gate + snapshot (triggers buy decision loop)
│   ├── BuyAction.json     # 3-tier purchase / blacklist / stable refresh / nothing to buy
│   ├── BuyItem.json       # Purchase popup confirm / failure / close
│   └── Special.json       # ADB swipe / unable to buy on refresh / refresh item
├── Credit/                # Credit management
│   ├── Reserve.json       # Reserve threshold detection + current credit OCR
│   ├── AutoGetCredits.json# Auto credit replenishment (jump to base when unaffordable)
│   └── ClaimCredit.json   # Claim pending credits
├── Item/                  # Shelf item recognition (recognition chain)
│   ├── ShelfBase.json     # Anchor CreditIcon + sold out + price check
│   ├── Priority1.json     # Priority 1 whitelist + discount
│   ├── Priority2.json     # Priority 2 whitelist + discount
│   ├── Priority3.json     # Priority 3 whitelist + discount
│   └── Blacklist.json     # Blacklist filter + discount
├── BuyItem/               # Purchase popup (one file per item)
│   ├── ArsenalTicket.json # Per-item popup OCR
│   ├── Oroberyl.json
│   ├── TCreds.json
│   ├── ElementaryCombatRecord.json
│   ├── IntermediateCombatRecord.json
│   ├── ElementaryCognitiveCarrier.json
│   ├── ArmsInspector.json
│   ├── ArmsINSPKit.json
│   ├── CastDie.json
│   ├── HeavyCastDie.json
│   ├── Protodisk.json
│   ├── Protoset.json
│   ├── Protoprism.json
│   └── Protohedron.json
├── GoToShop.json           # Navigate to shop and switch to credit exchange tab
├── record.json             # Record item name and discount during shelf snapshot
└── Reflash.json            # Refresh button / cost / used up / unable to refresh
```

Other related files:

| Path                                              | Function                                                                    |
| ------------------------------------------------- | --------------------------------------------------------------------------- |
| `assets/interface.json`                           | Task mounting (`other_menu` / `daily` group)                                |
| `assets/tasks/CreditShopping.json`                | Task entry, three-tier purchases, reserve threshold, refresh options        |
| `assets/resource/pipeline/DijiangRewards/NeedCredit.json` | Return to base to replenish credit when insufficient               |
| `agent/go-service/common/attachregex/action.go`   | Merge attach keywords into OCR whitelist regex                              |
| `assets/locales/interface/*.json`                 | Task, option, and focus text                                                |

## Execution Flow

1. Enter the credit exchange tab; if not in the shop, navigate first (`GoToShop.json`), then [claim pending credits](#claiming-credits) (`Credit/ClaimCredit.json`).
2. Before entering the scan loop, [initialize the item name whitelist for each tier once](#attach-and-whitelist-initialization) (`Flow/Entry.json` + `attachregex/action.go`).
3. In each round, for the current shelf snapshot, evaluate in a fixed priority order (`Flow/Scan.json` → `Flow/BuyAction.json`):
    - A target item in some tier [is affordable but credit is insufficient](#automatic-credit-replenishment) → jump to base to replenish credit and return.
    - Whether it matches [Priority Purchase 1 / 2 / 3](#three-tier-purchase-priority) → enter the purchase popup.
    - Whether the current credit is [below the reserve threshold](#credit-point-reserve-threshold) → end the task.
    - Whether refresh attempts are exhausted, whether to trigger [stable refresh to direct purchase](#force-strategy-and-refresh).
    - Follow the [force strategy](#force-strategy-and-refresh) to purchase any affordable item / refresh the shelf / end directly.
4. In the purchase popup, confirm the item, record the focus (`Flow/BuyItem.json` + `BuyItem/*.json`), return to the list, and continue scanning.

> The `next` order of the scan loop defines the business priority; to change behavior, examine the entire chain, not just a single recognizer.

## Special Handling

### Claiming Credits

Implemented in `Credit/ClaimCredit.json`. After entering the credit exchange, first attempt to claim pending credits; if none are available, proceed directly to scanning without blocking the main flow.

### attach and Whitelist Initialization

1. The user selects items in `CreditShopping.json` → the names in each language are written to the corresponding tier's `attach`.
2. Before scanning, serially execute `AttachToExpectedRegexAction` to merge attach into `^(alias1|alias2|...)$` and overwrite the item name OCR node.
3. Each tier maintains both an "affordable" and "unaffordable" whitelist simultaneously; if no items are selected for a tier, the regex is changed to `a^` (never matches).

Go is only responsible for parameter assembly; when and how to purchase is determined by the Pipeline.

### Item Recognition Chain

Implemented in the `Item/` directory. Image reading order: **anchor first, then continuous offset**; layers depend on each other; if a preceding layer is not matched, all subsequent layers fail.

Color conventions (for maintenance documentation and screenshot comparison):

- Black: Credit item card anchor (`Item/ShelfBase.json` — `CreditIcon`)
- Blue: Whether not sold out (`Item/ShelfBase.json` — `NotSoldOut`)
- Red: Affordable / Unaffordable (`Item/ShelfBase.json` — `CanAfford` / `CanNotAfford`, the two chains branch here)
- Green: Item name OCR (whitelist, per-tier `Item/Priority{N}.json` / `Item/Blacklist.json`)
- Pink: Discount OCR (per-tier `Item/Priority{N}.json` / `Item/Blacklist.json`)

#### Purchase Chain

![Purchase Recognition Chain](https://github.com/user-attachments/assets/0e9f7e50-9b08-451f-abd4-2cb49b01986f)

```text
ShelfBase → Whitelist → Discount → Enter Purchase Decision
```

#### Credit Replenishment Chain

![Credit Replenishment Recognition Chain](https://github.com/user-attachments/assets/37235adf-9f1c-40ed-aaaa-9f713a80d5a7)

```text
ShelfBase → Whitelist → Discount → Enter Credit Replenishment Decision
```

The item name and discount semantics for both chains must be consistent; otherwise, a contradiction may occur where "it is a target when affordable, but not when unaffordable."  
Each tier needs to maintain nodes on both sides simultaneously; when troubleshooting, examine layer by layer in order: black → blue → red → green → pink.

### Three-Tier Purchase Priority

The structure is the same for all three tiers, but the default strategies differ (`CreditShopping.json`):

| Tier       | Default "Unconditional Purchase" | Default "Auto Replenish Credit" | Typical Use Case                                                 |
| ---------- | -------------------------------- | ------------------------------- | ---------------------------------------------------------------- |
| Priority 1 | On                               | On                              | Essential items worth buying even when credit is nearly depleted |
| Priority 2 | Off                              | Off                             | Only buy if reserve threshold is met                             |
| Priority 3 | Off                              | Off                             | Same as above, lower priority                                    |

Each tier can be independently configured: selected items, minimum discount, whether to skip the reserve threshold, whether to allow credit replenishment when unaffordable.  
Only after all three tiers fail to match does the unified reserve threshold node handle the fallback exit.

### Credit Point Reserve Threshold

`CreditShoppingReserve` modifies the reserve threshold expression (`Credit/Reserve.json`).  
Whether each tier is subject to the threshold limit is controlled by the tier's "Unconditional Purchase" switch, not by the scan `next` order.  
To make a tier ignore the threshold, modify the corresponding tier's "Unconditional Purchase"; do not reinsert the threshold check into the middle of the purchase chain.

### Automatic Credit Replenishment

If a tier has "Auto Replenish Credit" enabled, and the [credit replenishment recognition chain](#credit-replenishment-chain) matches, it jumps to `NeedCredit.json` (`Credit/AutoGetCredits.json`):

1. Return to the base reception room, and according to the configuration, gift clues or initiate clue exchange.
2. The number of gifts is controlled by `CreditShoppingClueSend` (`0` = no gifts).
3. The inventory lower limit for gift-able clues is controlled by `CreditShoppingClueStockLimit` (default is to keep 2, i.e., only gift if inventory ≥ 3).

Each tier has its own independent switch; insufficient refresh cost **will not** trigger credit replenishment (the old `RefreshGetCredits` has been removed).

### Discount Options

The discount option for each tier modifies the corresponding tier's discount OCR `expected`, or changes it to ColorMatch ("any discount").  
"Any discount" uses color matching instead of loose OCR to preserve the offset anchor ROI.  
Discount rules must cover both the affordable and unaffordable sides.

### Force Strategy and Refresh

The fallback after all three tiers fail to match is determined by `CreditShoppingForce` (`Flow/BuyAction.json` + `Flow/Special.json`):

| Strategy         | Behavior                                                     |
| ---------------- | ------------------------------------------------------------ |
| Exit             | Do not purchase any item, do not refresh, end directly       |
| Ignore Blacklist | Purchase any affordable, not sold-out item                   |
| Refresh          | Attempt to refresh the shelf; can expand to "stable refresh" |

**Stable Refresh**: If "Current credit − Refresh cost < Stable refresh threshold" and there are still purchasable items on the shelf, then do not refresh, but directly purchase instead.  
This threshold and the "Reserve Credit Points" are two independent conditions; do not mix them.

## Paths to Modify When Adding New Items

1. `assets/tasks/CreditShopping.json` — Add a case in the corresponding tier's checkbox, and simultaneously write `attach` for both the "affordable" and "unaffordable" sides.
2. `assets/resource/pipeline/CreditShopping/Flow/BuyItem.json` — Add this item's popup branch to the `next` list.
3. `assets/resource/pipeline/CreditShopping/BuyItem/` — Create a new item file (popup OCR + focus text), following the existing file structure.
4. `assets/locales/interface/*.json` — `option.CreditShoppingItems.cases.*.label`

If only the list whitelist is changed without modifying the popup confirmation, the issue of being able to open it but missing the focus will occur.  
After adding a case, remember to check whether `default_case` also needs to include the new item.

## Maintenance Key Points

| Phenomenon                               | Priority Check                                                                   |
| ---------------------------------------- | -------------------------------------------------------------------------------- |
| Target item not recognized               | The merged attach regex; recognition chain from black → pink layer by layer      |
| Affordable but not purchasing            | The tier's reserve threshold / unconditional purchase switch                     |
| Unaffordable but not replenishing credit | The tier's AutoGetCredits switch; the unaffordable side's whitelist and discount |
| Abnormal refresh behavior                | `CreditShoppingForce`; stable refresh threshold                                  |
| Inconsistent behavior between options    | `CreditShopping.json`'s `pipeline_override` and scan `next` order                |

Maintenance location is done in four layers: Entry (`GoToShop.json`) → Scan Decision (`Flow/`) → Recognition Chain (`Item/`) → Parameter Assembly (task options + Go).
