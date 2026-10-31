# Developer Manual - OutpostTrading

`OutpostTrading` automatically sells products at the regions/outposts selected by the user. The task follows "Pipeline owns flow, Go owns algorithms": Pipeline (`assets/resource/pipeline/OutpostTrading*.json`) handles screen recognition and clicking; the Go service (`agent/go-service/outposttrading/`) owns selection strategy, reserve rules, operator planning, and caching.  
This document was updated on October 23, 2026.

## At a Glance

- **Entry**: the `SellProduct` task in `assets/tasks/OutpostTrading.json` → Pipeline entry `OutpostTradingSchedule` (weekday-gated). The task name and every option name are persistence keys in the MXU config `config/mxu-MaaEnd.json`, so they deliberately keep the legacy `SellProduct*` spelling; only directories, files and node names moved to `OutpostTrading`.
- **Responsibility split**: Go has only two business packages — `goods/` (item selection, reserve, out-of-stock, priority lists) and `operator/` (operator recognition, assignment planning, cache), independent of each other; top-level `runtime.go` combines both to print the per-outpost plan.
- **Do not hand-edit generated artifacts**: `assets/data/OutpostTrading/selection_data.json`, `assets/tasks/OutpostTrading.json`, `pipeline/OutpostTrading/{OperatorSession.json, Loop.json, {Region}/}` (both Win32 and ADB packs). Always edit the model/templates/projections under `tools/pipeline-generate/OutpostTrading/` and regenerate.
- **Two kinds of state**: the out-of-stock set, tried items, and satisfied reserves are **task-scoped session state** — never persisted, cleared on the next task init. The only persisted state is `debug/record/OutpostTradingCache.json` (operator snapshot + outpost prosperity state, isolated per hashed UID).

## Flow Overview

```text
OutpostTradingSchedule ──weekday hit──> OutpostTradingMain
  └─ OutpostTradingEnterRegionalDevelopment   SceneManager: enter Regional Development
  └─ OutpostTradingCaptureUid                 capture hashed UID (account-scoped cache)
  └─ OutpostTradingPrepareSession             SubTask running 5 init stages in order (reset before configure):
  │    ├─ InitializeReserveSession → RegisterReserveRule1..6   reset and register reserve rules (empty slots no-op)
  │    ├─ InitializeOperatorSession → RegisterLocation × 6     reset operator session and register enabled outposts
  │    ├─ ConfigurePrioritySession                             priority master switch / strict mode
  │    ├─ ConfigureSelectionStrategy                           selection strategy (rarity/price/stock)
  │    └─ ConfigureMinimumPrice                                minimum unit price (applies to every strategy)
  └─ OutpostTradingLoop                       newest region first: Wuling → Valley IV → OutpostTradingTaskEnd

Per region (OutpostTrading{Region} → OutpostTrading{Region}Sell)
  ├─ SceneManager enters the region, then the region's outpost management (two SubTask runs)
  ├─ {Region}InitializePrioritySession → RegisterPriorityItem1..6   switch region priority list
  ├─ {Region}PrepareOperatorCache → ScanOperatorList     full operator scan when no snapshot
  └─ [JumpBack] run the region's outposts in fixed order → back to OutpostTradingLoop

Per outpost (OutpostTrading{LocationId}Sell)
  ├─ bind outpost anchors (ZeroMoneyHandler / CurrentGoodsReady / SelectPriorityItem / PriorityItemsExhausted
  │    / CommitPriorityItem / MarkOutOfStock / BetterSliding; before & after OperatorTarget are bound by SetOperatorAnchors below)
  ├─ detect whether outpost prosperity is maxed → ReportLocationPlan (print the outpost plan)
  ├─ SetOperatorAnchors → OutpostTradingSellMain
  │    ├─ BeforeSellOperator → [Anchor]…Target   pre-sell: switch to the planned selling operator
  │    ├─ OutpostTradingSellLoop                 shared selling loop (below)
  │    └─ AfterSellOperator → [Anchor]…Target    post-sell: assign production operators per global plan
  └─ return to the region node for the next outpost

OutpostTradingSellLoop (unlimited rounds; stock bills checked first each round)
  ├─ [Anchor]ZeroMoneyHandler     stock bills insufficient → OutpostTradingSellLoopEnd (post-sell)
  ├─ [Anchor]CurrentGoodsReady    current goods match the current selection rules → adopt → OutpostTradingAtSell
  └─ OutpostTradingChangeGoods    click "Switch Goods" → ResetGoodsSelection → ChangeGoodsRelay
       ├─ [Anchor]SelectPriorityItem → SelectNewGoodConfirm → [Anchor]CommitPriorityItem
       │    → OutpostTradingAtSell: re-check stock bills → out of stock → [Anchor]MarkOutOfStock
       │      → "select goods" prompt → retry switching once → [Anchor]BetterSliding applies the reserve rule → trade/skip
       └─ [Anchor]PriorityItemsExhausted → CloseGoodsAfterExhausted → SellLoopEnd
```

Task-level termination: if outpost management is locked, SceneManager cannot enter and the task stops; the dialog saying the goods have "exceeded the stock bill reserves of the outpost" is handled by `OutpostTradingAidQuotaExceededStop`, which stops the task without auto-confirming.

> [!IMPORTANT]
>
> `InitializeReserveSession` must run before the three `Configure*` stages: its `reset` also resets the whole priority-selection session (master switch, strict mode, selection strategy, minimum unit price), so reversing the order would swallow the configuration. `InitializeOperatorSession` is fully independent of the goods session and may sit anywhere.

## Selling Rules (Go `goods/`)

### Selection Strategy (single-choice task option)

| Strategy | Ordering | Notes |
| ---------------- | ----------------------------------------------------- | ----------------------------------------------------------- |
| Rarity Priority (default) | rarity desc → unit price desc → stable source order | may pick bottom half-visible items with unknown stock |
| Unit Price Priority | unit price desc → rarity desc → stable order | same as above |
| Stock Priority | local stock desc → unit price → rarity → stable order | requires recognized stock |

The minimum unit price (task option `SellProductStockMinimumUnitPrice`, default 10) applies to **every** strategy: goods priced below it are never selected and are listed separately as "excluded below minimum unit price" in the runtime plan. Preferred goods are explicitly pinned by the user and are exempt.

### Selection Recognition (`OutpostTradingPriorityItem` custom recognizer)

- Scans only the first page; never scrolls the list for low-tier items on page two.
- Anchors complete cells with the detail-icon template, then OCRs item names; the name/stock/click offset regions are passed by the Win32 and ADB Pipelines via `stock_*_offset` — Go hardcodes no platform coordinates.
- Fewer names than icons means OCR missed a name → re-recognize without selecting; bottom half-visible cells are recorded as "stock unknown".
- Excluded upfront: zero stock, already tried, confirmed out of stock, never-sell, and reserve-satisfied items.
- A selection is only "pending"; `commit` marks it tried only after the selling screen is recognized again — failed clicks or single-frame OCR flicker never skip a high-priority item.
- When every candidate is unusable, two consecutive stable recognitions of the same set → `PriorityItemsExhausted`, closing the list and ending this outpost. Empty OCR results never count as "nothing left".

### Current Goods Adoption (`OutpostTradingCurrentGoods` custom recognizer)

- Recognizes the currently selected goods icon in the outpost selling screen via IconRecognition `single_roi`, with candidates limited to the outpost's sellable items; the Win32 and ADB Pipelines pass `[1177,450,54,54]` and `[1151,393,66,66]` through `roi`, so Go hardcodes no platform coordinates.
- Rarity and price strategies reuse the normal selection rules. The current item is adopted only when it is exactly the next candidate after preferred slots, tried/out-of-stock state, and reserve rules are applied; otherwise the flow falls back to "Switch Goods" and scans the list.
- Preferred slots take precedence over the stock strategy, so Stock may also adopt the first available preferred item after tried, out-of-stock, and reserve filters are applied. If the current item is not that preferred candidate, or no preferred item remains available, the goods list must be opened to read live storage quantities; an ordinary candidate is never adopted without that scan.
- On hit, the `adopt` operation registers the recognized `item_id` as the outpost's current item with the same session effect as a switching `commit` (marks tried, updates the reserve-rule selection), so selling, reserve rules, and out-of-stock marking never distinguish where the goods came from.

### Priority Selling (master switch, off by default, decoupled from region toggles)

- Expands "Sell Priority Goods Only" plus a per-region toggle with 6 slots (listing only items sellable in that region).
- Entering a region switches that region's priority list; slots are tried 1→6; duplicated items keep only the earliest slot.
- Strict mode (Sell Priority Goods Only) applies only to regions whose priority config is also enabled: those regions sell only explicitly configured items, while other regions keep selling per the selected strategy; an enabled region with no applicable item ends normally after two stable empty-candidate confirmations.

### Reserve Rules (6 independent slots)

- Two modes: Keep Specified Quantity / Never Sell (internally quantity `-1`); quantity `0` means keep nothing; for duplicated items the later slot wins.
- Activity items are sold in one batch against the activity quota, so reserve rules never apply to them and the slots list no activity items; priority selling options are unaffected.
- "Keep Specified Quantity" uses BetterSliding `ReverseTarget` to sell only the excess; reaching the reserve in one trade, or stock already at/below the reserve, both `satisfy` the item for this task so later outposts skip it during selection.
- "Never Sell" excludes the item during selection recognition — no goods switching, never marked out of stock.

### Out of Stock

When the current item's stock is found insufficient after switching or adopting (`OutpostTradingZeroProduct`: red stock text, or a 1–3 digit white stock, i.e. 1–999 units), `[Anchor]MarkOutOfStock` records the outpost's current item (`item_id` of the last `commit` / `adopt`) into a task-scoped set so later outposts skip it during selection; not persisted, cleared on the next task init.

## Operator Rules (Go `operator/`)

Pre-sell (switch to the selling operator) and post-sell (restore production assignments) share one loop: **check current operator → open the list → scan page by page → re-plan from the full snapshot**.

### Selling Operator Tiers

1. both prosperity and trade-income bonuses;
2. prosperity bonus only;
3. trade-income bonus only;
4. within a tier, stable game-list order (count of matched outpost `features` desc → numeric charId desc; rarity is irrelevant).

When outpost prosperity is maxed, prosperity bonuses stop counting and only trade income defines the tiers. A "perfect candidate" is the intersection of the outpost's top selling tier and its restore candidates; if the account owns any perfect candidate, planning picks only from them (even if one is occupied by another enabled outpost — no downgrade), otherwise it falls back to the top available tier. If the current operator already belongs to the best available tier, it is kept without opening the list.

### Post-sell Restore Assignment

One operator cannot occupy multiple outposts. Plans are ranked by: most restorable outposts → most outposts keeping their pre-sell selling operator → most outposts whose final operator still belongs to the top tier → smallest sum of candidate `Priority`. Confirmed `location → operator` pairs are locked immediately and cannot be reused; newer regions finish and lock first.

### Assignment Conflicts

If the candidate is assigned elsewhere, a confirmation dialog appears: source outpost enabled in this task → confirm and pull the operator over; disabled or not reliably recognized → cancel, add the operator to a task-scoped exclusion set, and re-plan (reset on the next task init).

### Failure Policy

Selling operator not found, or a pre-/post-sell list scan failure (including the planned operator still unselectable after one refreshed retry) → stop the task (never trade with the wrong operator); no available restore operator after a full scan → log a skip, finish the outpost, and continue.

### Operator Cache (`debug/record/OutpostTradingCache.json`)

- Account partitions keyed by CaptureUID's 16-char lowercase hex salted hash (`unknown` when not captured); keys are never re-normalized, avoiding collisions.
- `operators` is a full-list snapshot of `updated_at` + `ids` (missing fields = never scanned; empty array = scanned, nothing relevant); `locations` stores each outpost's prosperity-maxed state. Both use stable IDs from `selection_data.json` — no Chinese names, independent of client language.
- No snapshot → full scan before selling; "Force Refresh Operator Cache" → rescan once on first region entry this task, reused afterwards. Partial scrolling scans inside outposts never overwrite the snapshot.
- If a known operator on duty is missing from the snapshot before selling → invalidate the snapshot and run one full rescan (at most once per task).
- Prosperity is re-recognized on every outpost entry and written back; state changes re-plan unfinished outposts immediately, while completed post-sell assignments stay locked.
- No format version, no migration of legacy formats; corrupted JSON or incompatible top-level structure voids the whole cache, while a malformed single-account partition only voids that account.

## Runtime Output

- When reusing an operator snapshot, prints the account's `updated_at` in local time.
- On outpost entry, prints: selling/restore operator targets, the goods plan (static strategies list their order; stock strategy notes it decides after scanning), items excluded due to out-of-stock/reserve/never-sell/below minimum price, and applicable reserve rules.
- Afterwards prints actual keep/switch results, completed trades, out-of-stock events, reserve milestones, conflict re-plans, scan failures, and other dynamic states.
- All text follows the client language: Pipeline `focus` uses interface i18n; Go output uses go-service i18n.

## Generator and Maintenance

The generator lives in `tools/pipeline-generate/OutpostTrading/`. Outpost data is published by zmdmap and updated through `pnpm fetch:zmdmap` (`uv run zmdmap fetch`); see the [generator README](../../../../tools/pipeline-generate/OutpostTrading/README.md) for the data flow and the roles of each generator file.

| Maintenance entry | Generated artifact |
| ------------------------------- | ----------------------------------------------------------------- |
| `pipeline(-adb)-template.jsonc` | `pipeline/OutpostTrading/{Region}/{Location}.json` (Win32/ADB packs) |
| `sell-template.jsonc` | `pipeline/OutpostTrading/{Region}/OutpostTrading{Region}.json` |
| `loop-template.jsonc` | `pipeline/OutpostTrading/Loop.json` |
| `session-template.jsonc` | `pipeline/OutpostTrading/OperatorSession.json` |
| `task-template.jsonc` | `assets/tasks/OutpostTrading.json` |
| `sync-locales.mjs` | five-language locale keys for outposts/operators/items |
| `selection-data.mjs` | `assets/data/OutpostTrading/selection_data.json` |

Hand-maintained (untouched by the generator):

- `pipeline/OutpostTrading.json`: task entry and init chain;
- `OutpostTrading/SellCore.json`, `ChangeGoods.json`, `OutpostShared.json`: shared selling loop, goods selection, and outpost screen recognition;
- `OutpostTrading/OperatorScan.json`, `Operator.json`: operator cache scanning and liaison screen recognition;
- `OutpostTrading/ReserveSession.json`: reserve-rule session (also holds `ConfigurePrioritySession`);
- all Go code under `agent/go-service/outposttrading/`: `goods/` (items, with the three pure strategies in `strategy/`), `operator/` (operators), `internal/selectiondata/` (shared deployment-data loading/validation), `internal/ocrmatch/` (shared strict OCR matching), `runtime.go` (the combined `OutpostTradingLocationPlan` output), and `register.go` (component registration).

```shell
# sync the zmdmap compact game data and regenerate everything
pnpm generate:OutpostTrading

# only sync the zmdmap compact game data
pnpm fetch:zmdmap

# render from the generated data only
node tools/pipeline-generate/OutpostTrading/sync-locales.mjs
node tools/pipeline-generate/OutpostTrading/selection-data.mjs
node tools/pipeline-generate/run-all.mjs OutpostTrading
```

Maintenance notes:

- New items usually only require syncing the zmdmap compact game data; `sync-locales.mjs` fills missing five-language `item.*` keys (reusing existing keys with the same Chinese name). Temporary event-item exclusions live in `selection-data.mjs`; clean them up once the source data removes the event items and regenerate.
- After adding an outpost, check the generated region `next` lists, SceneManager entries, and both Win32/ADB artifacts.
- When adding a region, manually create its subfolder under `OutpostTrading/` in both resource packs before generating (the generator only creates `outputDir`).
- Reserve rules: item cases pass `item_id` via `attach`; quantity inputs pass integers via `custom_action_param.quantity`.

Before submitting, check:

```shell
git diff --check
```

Run `pnpm test` locally when the change includes `tests/**`, and if you also changed `assets/**` or `tools/schema/**`, run `pnpm check` as well; otherwise there is no need to run these two commands locally, just follow the relevant CI after opening a PR — see the [coding standards](../coding-standards.md#pre-submission-check).
