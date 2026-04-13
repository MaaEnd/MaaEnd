# Developer Guide — Credit Shop Maintenance

This document explains the overall structure of `CreditShopping` (credit shop), purchase priority, credit acquisition integration, refresh strategy, and how each `interface` option in `assets/tasks/CreditShopping.json` overrides Pipeline behavior, to aid future maintenance and extension.  
Note especially: purchase conditions are not a few scattered toggles, but one full filter chain from `Item.json` through `Shopping.json`. Maintenance must follow the entire chain.  
This document was updated on April 8, 2026,  
after the merge of [perf: each purchase option respects credit threshold (#1980)](https://github.com/MaaEnd/MaaEnd/pull/1980).

## File overview

The current implementation is spread across these files:

| Module                         | Path                                                        | Role                                                                                         |
| ------------------------------ | ----------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Project interface mount        | `assets/interface.json`                                     | Mounts `tasks/CreditShopping.json` under the `daily` task group                              |
| Task and option definitions    | `assets/tasks/CreditShopping.json`                          | Defines task entry, UI options, sub-options, and `pipeline_override`                         |
| Task entry                     | `assets/resource/pipeline/CreditShopping/GoToShop.json`     | Enters the shop and switches to the credit exchange tab                                      |
| Claim credit                   | `assets/resource/pipeline/CreditShopping/ClaimCredit.json`  | Claims pending credit and closes reward popups                                               |
| Main shopping scan loop        | `assets/resource/pipeline/CreditShopping/Shopping.json`     | Initializes parameters, scans items, purchases by priority, refreshes or exits               |
| Item list recognition          | `assets/resource/pipeline/CreditShopping/Item.json`         | Recognizes item icons, sold-out state, affordability, item names, and discounts              |
| Purchase dialog flow           | `assets/resource/pipeline/CreditShopping/BuyItem.json`      | Purchase confirmation, failure handling, and return to the item list                         |
| Purchase result focus          | `assets/resource/pipeline/CreditShopping/BuyItemFocus.json` | In the purchase dialog, recognizes the purchased item and records focus                      |
| Refresh-related recognition    | `assets/resource/pipeline/CreditShopping/Reflash.json`      | Recognizes refresh button, refresh cost, and “cannot refresh” state                          |
| Credit acquisition integration | `assets/resource/pipeline/DijiangRewards/NeedCredit.json`   | When credit is insufficient, returns to base to start clue exchange or gift clues for credit |
| Go parameter parsing           | `agent/go-service/common/attachregex/action.go`             | Merges `attach` keywords from task options into OCR regex and overrides Pipeline             |
| Localized strings              | `assets/locales/interface/*.json`                           | `CreditShopping` task and option text                                                        |

## Overall execution flow

The task entry is `CreditShoppingMain` in `GoToShop.json`:

1. Try to hit `CreditShoppingShopping` first; if already on the credit exchange tab, enter the scan loop directly.
2. If only on the shop page, click the credit exchange tab via `CreditShoppingCheckShopPage`.
3. After entering the tab, go through `ClaimCredit.json` first:
    1. If there is credit to claim, click `CreditShoppingClaimCredit`.
    2. If there is nothing to claim, hit `CreditShoppingNoCreditClaim`.
4. After returning to `CreditShoppingShopping` in `Shopping.json`, run `CreditShoppingInit` once.
5. `CreditShoppingInit` and its follow-up init nodes call generic `AttachToExpectedRegexAction` in sequence, overriding one target OCR whitelist regex at a time.
6. The loop then enters `CreditShoppingScanItem`, which evaluates in fixed order:
    1. Whether to top up credit first
    2. Whether priority purchase 1 matches
    3. Whether priority purchase 2 matches
    4. Whether priority purchase 3 matches
    5. Whether the reserve credit threshold triggers
    6. Whether refresh policy leads to “buy directly after refresh count exhausted” or “prudent refresh switches to direct purchase”
    7. Whether forced policy buys any item or performs a refresh
    8. If none of the above match, end the task

The key design points are:

- `CreditShoppingInit` runs only once; the heavy “multi-select item list → OCR regex” conversion is done in Go.
- The `next` order of `CreditShoppingScanItem` is the business priority—do not maintain single nodes in isolation; consider the whole scan order.
- All three purchase tiers can each configure “unconditional purchase” and “auto acquire credit”; current defaults are: purchase item option 1 on/on, purchase item option 2 off/off, purchase item option 3 off/off.

## Interface task vs Pipeline mapping

The user-facing entry for `CreditShopping` has two layers:

1. `assets/interface.json` only imports `tasks/CreditShopping.json` into the `daily` group.
2. `assets/tasks/CreditShopping.json` is the real interface task definition; it declares:
    1. Task name `CreditShoppingN2`
    2. Entry node `CreditShoppingMain`
    3. Top-level options: `CreditShoppingReserve`, `CreditShoppingClueSend`, `CreditShoppingClueStockLimit`, `CreditShoppingPriority1`, `CreditShoppingPriority2`, `CreditShoppingPriority3`, `CreditShoppingForce`

These top-level options are not “descriptive config”; they directly rewrite specific Pipeline nodes:

- `CreditShoppingReserve` rewrites the expression thresholds on `CreditShoppingReserveCredit` and `CreditShoppingReserveCreditSatisfied`.
- `CreditShoppingPriority1/2/3` control whether the three purchase branches `CreditShoppingBuyPriority1/2/3` are enabled, plus per-tier item whitelist, discount rules, auto top-up credit, and reserve-credit admission logic.
- `CreditShoppingForce` controls fallback when no purchase tier matches: exit, buy any affordable item, or refresh.

In other words, the interface task declares “which conditions may hold this run”; where each item is evaluated against those conditions is `Item.json` and `Shopping.json`.

## How the Item condition chain fits together

This section covers both the “purchase” and “cannot afford → auto get credit” recognition chains. Later sections only discuss option semantics and flow, not the recognizers themselves.

When reading the diagrams, use one order: locate the black box first, then offset from black results step by step.

Color convention:

- **Black:** `CreditIcon` — locates the credit-priced item card first.
- **Blue:** `NotSoldOut` — offset from black; whether the item is not sold out (grayscale recognition).
- **Red:** `CanAfford` / `CanNotAfford` — offset from blue; whether the price region is affordable or not.
- **Green:** `BuyFirstOCR` / `Priority2OCR` / `Priority3OCR` and corresponding cannot-afford branches — offset from red; item name OCR.
- **Pink:** `IsDiscountPriority1/2/3` and corresponding cannot-afford branches — offset from green; discount strength OCR.

### Figure 1: Purchase recognition chain

![image](https://github.com/user-attachments/assets/0e9f7e50-9b08-451f-abd4-2cb49b01986f)

Follow the image order:

1. Recognize black `CreditIcon` to fix the current item card position.
2. Offset from black to blue `NotSoldOut` to exclude sold-out items.
3. Offset from blue to red `CanAfford` to confirm the item is affordable.
4. Offset from red to green item-name OCR to confirm whitelist match for the current tier.
5. Offset from green to pink discount OCR to confirm discount meets the tier requirement.
6. Only when all of the above hold does `Shopping.json` continue to that tier’s reserve-credit admission and proceed to purchase.

One-line summary:

```text
black CreditIcon -> blue NotSoldOut -> red CanAfford -> green item name -> pink discount -> purchase evaluation
one item, not sold out, affordable, wanted item, discount OK -> buy!
```

### Figure 2: Auto get credit when cannot afford

![image](https://github.com/user-attachments/assets/37235adf-9f1c-40ed-aaaa-9f713a80d5a7)

This chain reads like the purchase chain except step 3:

1. Recognize black `CreditIcon`.
2. Offset from black to blue `NotSoldOut`.
3. Offset from blue to red `CanNotAfford` — item not affordable right now.
4. Offset from red to green item-name OCR — still the wanted target for the current tier.
5. Offset from green to pink discount OCR — discount still meets the tier.
6. When all hold, if that tier has `AutoGetCredits` enabled, flow goes to `NeedCredit`.

One-line summary:

```text
black CreditIcon -> blue NotSoldOut -> red CanNotAfford -> green item name -> pink discount -> credit top-up evaluation
one item, not sold out, cannot afford, wanted item and discount OK -> find a way to buy!
```

### Recognizer dependencies

These recognizers are sequential, not parallel:

- Blue depends on black: `NotSoldOut` `roi` comes from `CreditIcon`.
- Red depends on blue: `CanAfford` / `CanNotAfford` `roi` comes from `NotSoldOut`.
- Green depends on red: item name OCR `roi` comes from `CanAfford` or `CanNotAfford`.
- Pink depends on green: discount OCR `roi` comes from the specific item-name OCR node.

Do not maintain these in isolation. If an earlier layer misses, all downstream offset recognition fails together.

### Why purchase and credit top-up need separate node sets

- Purchase branch uses `CanAfford`; credit top-up branch uses `CanNotAfford`.
- Priority 1 must maintain both `BuyFirstOCR` and `BuyFirstOCR_CanNotAfford`.
- Priorities 2 and 3 need both affordable and cannot-afford sides for item name and discount.
- `IsDiscountPriority{N}` and `IsDiscountPriority{N}_CanNotAfford` must share the same discount semantics, or you get “affordable = target, cannot afford = not target” inconsistency.

For debugging recognition, the safest order is: black, blue, red, green, pink.

## Purchase priority model

The task splits items into three tiers:

### Purchase item option 1

- Entry node: `CreditShoppingBuyPriority1`
- Default attachments:
    - `CreditShoppingPriority1UnconditionalPurchase=Yes` — “unconditional purchase”; skips reserve credit threshold check
    - `CreditShoppingPriority1AutoGetCredits=Yes` — when cannot afford, auto get credit is allowed

This tier is usually for items worth buying even when credit is almost gone.

### Purchase item option 2

- Entry node: `CreditShoppingBuyPriority2`
- Default attachments:
    - `CreditShoppingPriority2UnconditionalPurchase=No` — must satisfy reserve credit threshold
    - `CreditShoppingPriority2AutoGetCredits=No` — by default, no auto get credit when cannot afford

### Purchase item option 3

- Entry node: `CreditShoppingBuyPriority3`
- Default attachments:
    - `CreditShoppingPriority3UnconditionalPurchase=No` — must satisfy reserve credit threshold
    - `CreditShoppingPriority3AutoGetCredits=No` — by default, no auto get credit when cannot afford

### Why reserve threshold was moved after the three purchase tiers

`CreditShoppingScanItem.next` order is:

1. `AutoGetCredits`
2. `CreditShoppingBuyPriority1`
3. `CreditShoppingBuyPriority2`
4. `CreditShoppingBuyPriority3`
5. `CreditShoppingReserveCredit`

Implications:

- All three tiers try their purchase recognition first.
- Whether reserve credit threshold applies is not decided by `next` order alone; each tier’s `CreditShoppingPriority{N}ReserveCreditGate` controls admission.
- If no tier matches, unified `CreditShoppingReserveCredit` handles “below threshold → end task” as final fallback.

Naming:

- `CreditShoppingPriority{N}ReserveCreditGate`: whether that tier must pass reserve credit threshold before purchase.
- `CreditShoppingReserveCredit`: after all three tiers miss, unified node for “current credit below reserve → end task.”

To change “which items ignore reserve threshold,” prefer adjusting per-tier “unconditional purchase” rather than inserting `CreditShoppingReserveCredit` between purchase nodes again.

## Runtime parameter override

The multi-select whitelist is not one huge regex in the task file; it is two steps:

1. Checkbox cases in `assets/tasks/CreditShopping.json` write multilingual item names into OCR nodes’ `attach`.
2. `agent/go-service/common/attachregex/action.go` reads `attach` at `CreditShoppingInit`, merges into runtime regex, and calls `OverridePipeline`.

Nodes dynamically overridden today:

- `BuyFirstOCR`
- `BuyFirstOCR_CanNotAfford`
- `Priority2OCR`
- `Priority2OCR_CanNotAfford`
- `Priority3OCR`
- `Priority3OCR_CanNotAfford`

Rules:

- `CreditShoppingPriority1Items` keywords go to both `BuyFirstOCR` and `BuyFirstOCR_CanNotAfford`
- Go merges and deduplicates both `attach` sides into one whitelist regex
- `CreditShoppingPriority2Items` and `CreditShoppingPriority3Items` update both affordable and cannot-afford nodes for that tier
- If a tier has no selections, Go sets `expected` to `a^` (never matches)

Benefits:

- Task layer keeps “one case per item” maintainability
- Pipeline only runs simple OCR at runtime—no giant hard-coded regex
- Go handles dedup, escaping, empty-list fallback

### Go responsibilities

Go’s role in `CreditShopping` is narrow: it does not run purchase flow; it only turns task options into runtime Pipeline parameters.

Execution flow:

1. `CreditShoppingInit` enters an init chain before the shop scan loop, and each init node triggers `AttachToExpectedRegexAction` once
2. Each action reads only one source-node group’s `attach`
3. Selected multilingual item names become whitelist match conditions for one target node
4. `OverridePipeline` writes back that target node’s `expected`
5. After all init nodes finish, normal item scanning continues in Pipeline; Go does not judge per item

Division of labor:

- `assets/tasks/CreditShopping.json`: what the user selected
- Go: turn selections into OCR match conditions
- `assets/resource/pipeline/CreditShopping/*.json`: recognition, clicks, purchase, refresh

Remember: whitelist organization → task options and Go; when to buy, how, and where after buy → Pipeline.

## Credit acquisition integration

`CreditShopping` can jump to base to top up credit when a tier wants to buy but cannot afford.

### `AutoGetCredits`

Auto get credit is no longer one global switch; it sits under each of the three purchase item options:

- `CreditShoppingPriority1AutoGetCredits`
- `CreditShoppingPriority2AutoGetCredits`
- `CreditShoppingPriority3AutoGetCredits`

They control:

- Whether auto top-up is allowed when target items in that option cannot be afforded
- When off, the corresponding `AutoGetCreditsBuyPriority{N}` recognition is set to `a^` to disable triggering

Aggregate entry remains `AutoGetCredits` in `Shopping.json`:

- Node: `AutoGetCredits`
- Trigger: `Or` across the three; any tier hit jumps to `GoToNeedCredit`

So auto credit is not exclusive to purchase item option 1; each tier controls its own behavior.

### Clue gifting settings

There is no longer a top-level “get credit settings” group; only two clue-related settings remain:

- `CreditShoppingClueSend`
- `CreditShoppingClueStockLimit`

Behavior:

- `CreditShoppingClueSend` overrides:
    - `ReceptionRoomSendCluesEntry_NeedCredit.max_hit`
    - `ReceptionRoomSendCluesSelectClues_NeedCredit.max_hit`
- `CreditShoppingClueStockLimit` overrides `ClueItemCount_NeedCredit.expected` to define “stock count that counts as giftable”

`CreditShoppingClueSend` accepts `0`:

- `0`: do not gift clues
- `1+`: max gifts per integration run

Default threshold `2` means “at least 3 of one clue type before gifting,” i.e. keep 2 by default.

## What discount options really mean

Each priority tier has a discount filter:

- `CreditShoppingPriority1DiscountValue`
- `CreditShoppingPriority2DiscountValue`
- `CreditShoppingPriority3DiscountValue`

These do not “click a discount button”; they override `expected` on `IsDiscountPriority{N}` and the corresponding `CanNotAfford` nodes.

Examples:

- `Any`: switch to `ColorMatch`; only requires presence in the discount region
- `-75%`: only accepts `75|95|99`
- `-99%`: only accepts `99`

Maintenance notes:

1. `Any` differs from other cases: it uses guaranteed color match, not loose OCR—direct always-hit would lose the target `roi`.
2. `AutoGetCredits` depends on the `*_CanNotAfford` discount nodes; rules must cover both affordable and cannot-afford sides.

## Force policy and refresh policy

When “no suitable whitelist item to buy,” behavior is driven by `CreditShoppingForce`.

### `CreditShoppingForce=Exit`

- Disable `CreditShoppingBuyBlacklist`
- Disable `RefreshItem`
- Disable `CreditShippingCanNotToBuy`

Semantics: end immediately—no buy-any, no refresh.

### `CreditShoppingForce=IgnoreBlackList`

- Enable `CreditShoppingBuyBlacklist`
- Disable refresh-related nodes

Semantics: after all three purchase options miss, if any affordable, in-stock item remains, continue buying any such item.

### `CreditShoppingForce=Refresh`

- Disable `CreditShoppingBuyBlacklist`
- Enable `RefreshItem`
- Enable `CreditShippingCanNotToBuy`
- Unfold `PrudentRefresh`

Semantics: when no suitable item, prefer refreshing the shop.

### `PrudentRefresh`

“Prudent refresh” is not “refresh more carefully”; it means:

- When `{current credit}-{refresh cost}<{threshold}` is true
- And the list still has any affordable, in-stock item

Then do not refresh; buy an affordable item directly instead.

Default threshold is written via `PrudentRefreshThreshold` as:

```text
{CreditShoppingReserveCreditOCRInternal}-{RefreshCost}<{PrudentRefreshThreshold}
```

So it blocks “remaining credit after refresh would be too low,” not “current credit below some value.”

## Adding or changing items

Item maintenance has two layers:

1. Whitelist matching on the list view
2. Item confirmation and focus in the purchase dialog

Adding an item requires updating at least the following.

### 1. Item cases in task options

File: `assets/tasks/CreditShopping.json`

Add cases under one or more checkboxes:

- `CreditShoppingPriority1Items`
- `CreditShoppingPriority2Items`
- `CreditShoppingPriority3Items`

Each case needs at least:

- `name`
- `label`
- `attach` on the corresponding OCR nodes

Notes:

- `Priority1` must keep `BuyFirstOCR` and `BuyFirstOCR_CanNotAfford` in sync
- For `Priority2/3`, verify cannot-afford branches still match the purchase branch
- `attach` values should list stable OCR text for all supported languages

### 2. Purchase dialog entry list

File: `assets/resource/pipeline/CreditShopping/BuyItem.json`

Add the corresponding `CreditShoppingBuyItemOCR_{ItemName}` node under `CreditShoppingBuyItem.next`; otherwise the dialog cannot hit the item-specific branch even if the item was opened.

### 3. Purchase dialog OCR nodes

File: `assets/resource/pipeline/CreditShopping/BuyItemFocus.json`

Add `CreditShoppingBuyItemOCR_{ItemName}` with:

- Item name OCR `expected`
- `focus.Node.Recognition.Succeeded`

If only the task whitelist changes and this step is skipped, clicks may still work but focus cannot record what was bought.

### 4. Localized strings

File: `assets/locales/interface/*.json`

Add:

- `option.CreditShoppingItems.cases.{ItemName}.label`

If new top-level or sub-options are added, add matching option strings.

## Changing default whitelist or discounts

The easy mistake is confusing “default value” with “optional cases.”

Examples:

- `CreditShoppingPriority1Items.default_case`
- `CreditShoppingPriority2Items.default_case`
- `CreditShoppingPriority3Items.default_case`

Adding cases without updating `default_case` does not add new items to the default user config.

Likewise, when changing default discount policy, check:

- `CreditShoppingPriority1DiscountValue.default_case`
- `CreditShoppingPriority2DiscountValue.default_case`
- `CreditShoppingPriority3DiscountValue.default_case`

## Easy-to-miss sync points in maintenance

### 1. Whitelist only, no purchase confirmation

Results in:

- List page can click the item
- Purchase dialog lacks `CreditShoppingBuyItemOCR_{ItemName}`

Typical symptom: missing focus after buy, or opaque purchase confirmation flow.

### 2. Only `BuyFirstOCR`, forgot `BuyFirstOCR_CanNotAfford`

`AutoGetCredits` uses the cannot-afford branch:

- `AutoGetCreditsBuyPriority1`
- `BuyFirstOCR_CanNotAfford`
- `IsDiscountPriority1_CanNotAfford`

If only the affordable branch is maintained, low credit will not trigger top-up correctly.

### 3. Thinking auto credit belongs only to purchase item option 1

It does not.

Base jump for credit is wired on `AutoGetCreditsBuyPriority1/2/3`; each tier’s `AutoGetCredits` toggle controls whether triggering is allowed.

### 4. Thinking insufficient refresh currency also triggers auto credit

It does not.

`RefreshGetCredits`-style options were removed; insufficient refresh budget no longer starts a separate credit flow. Auto credit only triggers from “cannot afford on a tier’s purchase option.”

### 5. Thinking `PrudentRefresh` is tied to reserve credit threshold

It is not.

`CreditShoppingReserve` and `PrudentRefreshThreshold` are independent:

- Former: per-tier threshold checks and final fallback exit
- Latter: whether to switch from refresh to direct purchase before refreshing

Do not conflate them.

### 6. Forgetting to verify `next` order

Much of `CreditShopping` behavior comes from `CreditShoppingScanItem.next` order, not single toggles.

If you change:

- Reserve threshold trigger point
- Auto credit trigger point
- Force buy or refresh policy

Re-check that `CreditShoppingScanItem.next` still reflects the intended priority.

## Recommended mental model

Maintain `CreditShopping` in four layers:

1. **Entry:** `GoToShop.json` + `ClaimCredit.json` — enter credit exchange and claim daily credit.
2. **Scan / decision:** `Shopping.json` — order of buy, stop, top-up credit, refresh.
3. **Recognition:** `Item.json` + `Reflash.json` — item name, discount, affordability, refresh state.
4. **Parameter assembly:** `assets/tasks/CreditShopping.json` + `agent/go-service/common/attachregex/action.go` — user selections → OCR conditions.

Triage:

- Cannot enter shop → entry layer.
- Wrong decisions in shop → scan/decision layer.
- Wrong item, discount, or price state → recognition layer.
- Same Pipeline behaves differently across options → parameter assembly layer.

## Self-check list

After changes, verify at least:

1. `assets/interface.json` still imports `tasks/CreditShopping.json`.
2. `CreditShoppingInit` and its follow-up init nodes still run `AttachToExpectedRegexAction` in the intended order.
3. For new items: `assets/tasks/CreditShopping.json`, `BuyItem.json`, `BuyItemFocus.json`, `assets/locales/interface/*.json` are updated together.
4. If credit acquisition logic changed: `ReceptionRoomSendCluesEntry_NeedCredit`, `ReceptionRoomSendCluesSelectClues_NeedCredit`, `ClueItemCount_NeedCredit` in `NeedCredit.json` still match task option semantics.
5. If refresh policy changed: order among `RefreshItem`, `CanNotFlash`, `CreditShoppingPrudentRefresh` is still correct.
6. If priority semantics changed: split between `CreditShoppingPriority1/2/3ReserveCreditGate` and `CreditShoppingReserveCredit` stays clear.
7. If auto credit logic changed: `AutoGetCreditsBuyPriority1/2/3` still align with the three purchase options’ `AutoGetCredits` toggles.
