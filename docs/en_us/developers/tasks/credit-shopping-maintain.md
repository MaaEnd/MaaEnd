# Development Guide - CreditShopping Maintenance

This document explains the overall structure of `CreditShopping`, its purchase priorities, credit-gain linkage, refresh strategy, and how the `interface` options in `assets/tasks/CreditShopping.json` override Pipeline behavior, so future maintenance and extension work is easier.

## File Overview

The current implementation is mainly distributed across the following files:

| Module | Path | Purpose |
| --- | --- | --- |
| Interface registration | `assets/interface.json` | Registers `tasks/CreditShopping.json` under the `daily` task group |
| Task and option definitions | `assets/tasks/CreditShopping.json` | Defines the task entry, UI options, child options, and `pipeline_override` |
| Task entry | `assets/resource/pipeline/CreditShopping/GoToShop.json` | Enters the shop and switches to the Credit Exchange tab |
| Credit claiming | `assets/resource/pipeline/CreditShopping/ClaimCredit.json` | Claims pending credits and closes the reward popup |
| Main item scan loop | `assets/resource/pipeline/CreditShopping/Shopping.json` | Initializes runtime parameters, scans items, purchases by priority, refreshes, or exits |
| Item list recognition | `assets/resource/pipeline/CreditShopping/Item.json` | Recognizes item icons, sold-out state, affordability, item names, and discount labels |
| Purchase dialog flow | `assets/resource/pipeline/CreditShopping/BuyItem.json` | Handles purchase confirmation, purchase failure, and returning to the item list |
| Purchase result focus | `assets/resource/pipeline/CreditShopping/BuyItemFocus.json` | Identifies which item was actually purchased inside the purchase dialog and records focus |
| Refresh-related recognition | `assets/resource/pipeline/CreditShopping/Reflash.json` | Recognizes the refresh button, refresh cost, and "cannot refresh" state |
| Credit-gain linkage | `assets/resource/pipeline/DijiangRewards/NeedCredit.json` | Returns to the base to start clue exchange or send clues when more credits are needed |
| Go parameter parsing | `agent/go-service/creditshopping/creditshopping.go` | Merges `attach` keywords from task options into OCR matching conditions and overrides Pipeline nodes |
| Localization | `assets/locales/interface/*.json` | UI text for `CreditShopping` tasks and options |

## Overall Execution Flow

The task entry is `CreditShoppingMain` in `GoToShop.json`:

1. It first tries to hit `CreditShoppingShopping`. If the task is already on the Credit Exchange page, it directly enters the scan loop.
2. If it is only on the general shop page, it clicks the Credit Exchange tab through `CreditShoppingCheckShopPage`.
3. After switching tabs, it goes through `ClaimCredit.json` first:
   1. If there are credits to claim, it clicks `CreditShoppingClaimCredit`
   2. If there are no credits to claim, it hits `CreditShoppingNoCreditClaim`
4. After returning to `CreditShoppingShopping` in `Shopping.json`, it runs `CreditShoppingInit` once.
5. `CreditShoppingInit` invokes the custom action `CreditShoppingParseParams` to read node `attach` data and prepare the runtime whitelist OCR conditions.
6. It then enters `CreditShoppingScanItem`, which evaluates the following in a fixed order:
   1. Whether it should go get more credits first
   2. Whether Priority 1 purchase conditions are met
   3. Whether the reserved credit threshold has been reached
   4. Whether Priority 2 purchase conditions are met
   5. Whether Priority 3 purchase conditions are met
   6. Whether refresh-related rules should convert into direct purchase
   7. Whether it should buy blacklist items or refresh based on the forced strategy
   8. If none of the above matches, the task ends

The most important design points are:

- `CreditShoppingInit` only runs once and lets Go prepare the runtime whitelist conditions.
- The order of `CreditShoppingScanItem.next` is itself business logic priority. Do not inspect only one node in isolation.
- `Priority1` and `Priority2/3` have different semantics: the former ignores the reserved credit threshold, while the latter respects it.

## Purchase Priority Model

The task currently splits items into three levels.

### Priority1

- Entry node: `CreditShoppingBuyPriority1`
- Recognition conditions: item exists, is not sold out, is affordable, item name matches `BuyFirstOCR`, and discount matches `IsDiscountPriority1`
- Meaning: high-priority whitelist that does **not** respect `CreditShoppingReserve`

This level is intended for items worth buying even when credits are running low.

### Priority2

- Entry node: `CreditShoppingBuyPriority2`
- Recognition conditions: item exists, is not sold out, is affordable, item name matches `Priority2OCR`, and discount matches `IsDiscountPriority2`
- Meaning: normal purchase level 1, which **does** respect `CreditShoppingReserve`

### Priority3

- Entry node: `CreditShoppingBuyPriority3`
- Recognition conditions: item exists, is not sold out, is affordable, item name matches `Priority3OCR`, and discount matches `IsDiscountPriority3`
- Meaning: normal purchase level 2, which **does** respect `CreditShoppingReserve`

### Why the Reserve Threshold Sits Between Priority1 and Priority2

The order in `CreditShoppingScanItem.next` is:

1. `AutoGetCredits`
2. `CreditShoppingBuyPriority1`
3. `CreditShoppingReserveCredit`
4. `CreditShoppingBuyPriority2`
5. `CreditShoppingBuyPriority3`

This means:

- `Priority1` is checked before the reserve threshold, so it is not blocked by it.
- Once `CreditShoppingReserveCredit` is hit, the flow stops immediately and will not continue to `Priority2/3`.

If you want to change which items ignore the reserve threshold, adjust the priority grouping instead of changing `CreditShoppingReserveCredit` itself.

## Runtime Parameter Override

The whitelist for `CreditShopping` is not maintained as one long hardcoded regex in the task file. Instead, it is built in two steps:

1. Checkbox cases in `assets/tasks/CreditShopping.json` write multilingual item names into the `attach` fields of different OCR nodes
2. `agent/go-service/creditshopping/creditshopping.go` reads those `attach` values in `CreditShoppingInit`, converts them into runtime OCR matching conditions, and then calls `OverridePipeline`

The nodes that are dynamically overridden are:

- `BuyFirstOCR`
- `BuyFirstOCR_CanNotAfford`
- `Priority2OCR`
- `Priority2OCR_CanNotAfford`
- `Priority3OCR`
- `Priority3OCR_CanNotAfford`

The actual rules are:

- Keywords from `Priority1Items` are written to both `BuyFirstOCR` and `BuyFirstOCR_CanNotAfford`
- Go merges and deduplicates the `attach` values on both sides so they use the same whitelist condition
- `Priority2Items` and `Priority3Items` only affect their own levels
- If a level has no checked items, Go changes the corresponding `expected` to `a^`, which means "never match"

Benefits of this design:

- The task layer remains maintainable, with one case per item
- Pipeline only consumes simple OCR conditions at runtime
- Go can centralize deduplication and empty-list fallback handling

### Go Work Logic

The Go part in `CreditShopping` has a very narrow responsibility: it does not drive the shopping process itself. It only converts task options into runtime parameters that Pipeline can use directly.

The execution order can be understood like this:

1. `CreditShoppingInit` triggers `CreditShoppingParseParams` once before the main shopping scan loop
2. Go reads `attach` values from nodes such as `BuyFirstOCR`, `Priority2OCR`, and `Priority3OCR`
3. It converts the checked multilingual item names into whitelist matching conditions for each priority level
4. It writes those conditions back into the OCR nodes through `OverridePipeline`
5. After that, item scanning is still handled entirely by Pipeline, and Go no longer participates in per-item decisions

In other words, the division of work here is:

- `assets/tasks/CreditShopping.json` defines which items the user selected
- Go converts those selections into OCR-usable matching conditions
- `assets/resource/pipeline/CreditShopping/*.json` handles the actual recognition, clicking, purchasing, and refreshing flow

For maintenance, remember one rule: if you are changing how whitelist items are organized, mainly inspect task options and Go; if you are changing when to buy, how to buy, or where to go after buying, mainly inspect Pipeline.

## Credit-Gain Linkage

`CreditShopping` can temporarily jump back to the base to gain more credits when it wants to buy something but cannot afford it, or when it wants to refresh but lacks enough credits.

### `CreditShoppingGetCreditsSetting`

This is the master switch for the credit-gain linkage:

- `Yes`: shows `AutoGetCredits` and `CreditShoppingSendCluesWhenInsufficient`
- `No`: changes the OCR condition of `AutoGetCredits` to `a^` and disables `ReceptionRoomSendCluesEntry_NeedCredit`

That means turning this switch off disables the entire "go back to the base for more credits" chain.

### `AutoGetCredits`

This option controls whether the task should jump to the base when it cannot afford a whitelist item:

- Node: `AutoGetCredits`
- Trigger source: `AutoGetCreditsBuyPriority1`

The current implementation only auto-fetches credits for the "cannot afford" case of `Priority1`. It does not do this for `Priority2/3`.

### `CreditShoppingSendCluesWhenInsufficient`

This option controls whether the task is allowed to try sending clues after returning to the base, when clue exchange cannot be started directly.

- `No`: `ReceptionRoomSendCluesEntry_NeedCredit.enabled=false`
- `Yes`: enables `ReceptionRoomSendCluesEntry_NeedCredit` and expands `CreditShoppingClueSend` and `CreditShoppingClueStockLimit`

Among them:

- `CreditShoppingClueSend` changes `ReceptionRoomSendCluesSelectClues_NeedCredit.max_hit` to the custom send count
- `CreditShoppingClueStockLimit` changes `ClueItemCount_NeedCredit.expected` to determine when clue stock is high enough to send

The default threshold `2` effectively means "send only when one clue type has at least 3 copies", which is equivalent to keeping 2 in reserve.

## What the Discount Options Actually Mean

All three priority levels have a discount filter:

- `CreditShoppingPriority1DiscountValue`
- `CreditShoppingPriority2DiscountValue`
- `CreditShoppingPriority3DiscountValue`

These options do not control "which discount button to click". They directly override `IsDiscountPriority{N}` and the corresponding `CanNotAfford` nodes.

For example:

- `Any`: changes to `ColorMatch`, only requiring the discount area to exist
- `-75%`: only accepts `75|95|99`
- `-99%`: only accepts `99`

When maintaining this logic, note two things:

1. `Any` is implemented differently from the other cases. It is not looser OCR, but a guaranteed-hit color match, because changing it to a direct hit would lose the target ROI.
2. `AutoGetCredits` depends on the `*_CanNotAfford` discount nodes, so discount rules must be applied to both the affordable and unaffordable branches.

## Forced Strategy and Refresh Strategy

When there are no more suitable whitelist items to buy, the next behavior is controlled by `CreditShoppingForce`.

### `CreditShoppingForce=Exit`

- Disables `CreditShoppingBuyBlacklist`
- Disables `RefreshItem`
- Disables `CreditShippingCanNotToBuy`

Meaning: end the task immediately when there is nothing suitable to buy; do not buy blacklist items and do not refresh.

### `CreditShoppingForce=IgnoreBlackList`

- Enables `CreditShoppingBuyBlacklist`
- Disables all refresh-related nodes

Meaning: after all whitelist logic is finished, continue buying any affordable and not-sold-out item even if it is not in the whitelist.

### `CreditShoppingForce=Refresh`

- Disables `CreditShoppingBuyBlacklist`
- Enables `RefreshItem`
- Enables `CreditShippingCanNotToBuy`
- Expands `RefreshGetCredits` and `PrudentRefresh`

Meaning: when there are no suitable items to buy, try refreshing the shop.

### `RefreshGetCredits`

This switch only appears when `Force=Refresh`, and handles the case where the task wants to refresh but does not even have enough credits for the refresh cost:

- `Yes`: enables `RefreshGetCredits`, so hitting `CanNotFlash` will jump to `NeedCredit`
- `No`: disables this extra credit-gain path

### `PrudentRefresh`

Prudent refresh does not mean "refresh more carefully". It means:

- when the expression `{current credits}-{refresh cost}<{threshold}` is true
- and there is still any affordable, not-sold-out item in the current shop list

the task will stop refreshing and directly buy the current available item instead.

The default threshold is written into:

```text
{CreditShoppingReserveCreditOCRInternal}-{RefreshCost}<{PrudentRefreshThreshold}
```

So this rule is about "credits left after refreshing would be too low", not simply "current credits are lower than some value".

## Adding or Modifying Items

Current item maintenance has two layers:

1. Whitelist matching on the item list page
2. Item confirmation and focus recording inside the purchase dialog

If you add a new item, at minimum you need to update the following.

### 1. Add Item Cases in Task Options

File: `assets/tasks/CreditShopping.json`

Add a case to one or more of:

- `CreditShoppingPriority1Items`
- `CreditShoppingPriority2Items`
- `CreditShoppingPriority3Items`

Each case should include at least:

- `name`
- `label`
- `attach` for the corresponding OCR node

Notes:

- `Priority1` must update both `BuyFirstOCR` and `BuyFirstOCR_CanNotAfford`
- `Priority2/3` only need to update their own OCR nodes
- `attach` values should contain stable OCR text for all supported languages of that item

### 2. Add the Purchase Dialog Entry

File: `assets/resource/pipeline/CreditShopping/BuyItem.json`

Add the corresponding `CreditShoppingBuyItemOCR_{ItemName}` node into `CreditShoppingBuyItem.next`. Otherwise, even if the item can be clicked from the list page, the purchase dialog will not be able to match the correct item branch.

### 3. Add the Purchase Dialog OCR Node

File: `assets/resource/pipeline/CreditShopping/BuyItemFocus.json`

Add the corresponding `CreditShoppingBuyItemOCR_{ItemName}` node and maintain:

- the item-name OCR `expected`
- `focus.Node.Recognition.Succeeded`

If you only update the task whitelist and forget this step, the task may still open the item, but the purchased item will not be identified clearly afterward.

### 4. Update Localization

File: `assets/locales/interface/*.json`

You need to add:

- `option.CreditShoppingItems.cases.{ItemName}.label`

If you also add new top-level or child options, you must add the corresponding option texts as well.

## Things Easy to Miss During Maintenance

### 1. Updating the Whitelist but Not the Purchase Confirmation

This leads to a situation where:

- the item can be selected on the list page
- but there is no corresponding `CreditShoppingBuyItemOCR_{ItemName}` node in the purchase dialog

The usual result is missing focus or unclear post-purchase behavior.

### 2. Updating `BuyFirstOCR` but Forgetting `BuyFirstOCR_CanNotAfford`

`AutoGetCredits` depends on the "cannot afford" branch:

- `AutoGetCreditsBuyPriority1`
- `BuyFirstOCR_CanNotAfford`
- `IsDiscountPriority1_CanNotAfford`

If you only maintain the affordable branch, the task will not trigger the credit-gain flow correctly when credits are insufficient.

### 3. Assuming `Priority2/3` Also Auto-Fetch Credits

They do not.

Automatic credit fetching only exists on the "cannot afford" path of `Priority1`, plus the optional "not enough credits to refresh" path. If you want to extend this to `Priority2/3`, you must update both `AutoGetCredits` recognition sources and this documentation.

### 4. Assuming `PrudentRefresh` Is Controlled by the Reserve Threshold

It is not.

`CreditShoppingReserve` and `PrudentRefreshThreshold` are two separate conditions:

- the former controls whether `Priority2/3` are allowed to continue
- the latter controls whether refresh should be replaced by direct purchase

Do not mix them up.

### 5. Forgetting to Check `next` Order

Many `CreditShopping` behaviors are decided not by one switch alone, but by the order in `CreditShoppingScanItem.next`.

Whenever you change:

- where the reserve threshold is checked
- where auto credit-gain is triggered
- the forced purchase or refresh strategy

you should always review whether `CreditShoppingScanItem.next` still reflects the intended priority.

## Recommended Way to Understand It

When maintaining `CreditShopping`, it helps to think in four layers:

1. **Entry layer**: `GoToShop.json` + `ClaimCredit.json`, which handle entering the Credit Exchange and claiming daily credits
2. **Decision layer**: `Shopping.json`, which decides when to buy, stop, get more credits, or refresh
3. **Recognition layer**: `Item.json` + `Reflash.json`, which determine how item names, discounts, affordability, and refresh state are recognized
4. **Parameter assembly layer**: `assets/tasks/CreditShopping.json` + `agent/go-service/creditshopping/creditshopping.go`, which determine how user choices become runtime OCR conditions

This makes troubleshooting much easier:

- If it cannot enter the credit shop, inspect the entry layer
- If it enters the shop but makes the wrong decision, inspect the decision layer
- If it misidentifies items, discounts, or affordability, inspect the recognition layer
- If behavior changes across different option combinations, inspect the parameter assembly layer

## Checklist

After making changes, check at least the following:

1. `assets/interface.json` still imports `tasks/CreditShopping.json`
2. `CreditShoppingInit` can still invoke `CreditShoppingParseParams` correctly
3. When adding items, `assets/tasks/CreditShopping.json`, `BuyItem.json`, `BuyItemFocus.json`, and `assets/locales/interface/*.json` are updated together
4. If you changed credit-gain logic, verify that `ReceptionRoomSendCluesEntry_NeedCredit`, `ReceptionRoomSendCluesSelectClues_NeedCredit`, and `ClueItemCount_NeedCredit` in `NeedCredit.json` still match the intended option semantics
5. If you changed refresh logic, verify that `RefreshItem`, `CanNotFlash`, `RefreshGetCredits`, and `CreditShoppingPrudentRefresh` still have the correct relationship and order
6. If you changed priority semantics, verify that `CreditShoppingReserveCredit` still sits between `Priority1` and `Priority2` in `CreditShoppingScanItem.next`
