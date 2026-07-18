# Developer Manual - SellProduct

This document describes the `SellProduct` main flow, automatic operator rules, automatic selling rules, and generator maintenance.

The task follows “Pipeline owns flow, Go owns algorithms.” Pipeline enters screens, recognizes state, and performs each UI action. `agent/go-service/sellproduct/` owns operator planning, item priority, session state, and reserve-quantity calculation.

> [!IMPORTANT]
>
> `assets/data/SellProduct/selection_data.json`, `assets/tasks/SellProduct.json`, `assets/resource/pipeline/SellProduct/OperatorSession.json`, both sets of `Outposts/*.json`, and `assets/resource/pipeline/SellProduct/Sell.json` are generated artifacts. Do not edit them directly. Update the model, data projection, or template under `tools/pipeline-generate/SellProduct/`, then regenerate.

## Main Flow

The task starts at `SellProductSchedule` and enters `SellProductMain` when the configured weekday is enabled. Initialization then enters Regional Development, captures the hashed UID, and registers reserve rules and enabled outposts for this run:

```text
SellProductSchedule                                  (Task entry, weekday gate)
  └─ SellProductMain                                 (selling flow entry)
       └─ SellProductEnterRegionalDevelopment        (SceneManager: enter Regional Development)
            └─ SellProductCaptureUid                 (capture hashed UID for account-scoped cache)
                 └─ SellProductInitializeReserveSession (clear previous reserve/selection state)
                      └─ SellProductRegisterReserveRule{1..6} (fixed chain; skip empty slots)
                           └─ SellProductRegisterPriorityItem{1..6} (fixed chain; skip empty slots)
                                └─ SellProductInitializeOperatorSession (initialize plans and locks)
                                     └─ SellProductRegisterAuto{LocationId} × N (fixed chain; skip inactive outposts)
                                          └─ SellProductOperatorSessionReady
                                               └─ SellProductLoop         (begin region traversal)
```

The twelve reserve-rule and priority-item registration nodes are always enabled and form a fixed slot-order chain. Task options override the stable `itemId` only for configured slots. Unconfigured slots keep an empty `item_id`, which the Custom Action treats as a successful no-op. Outpost registration nodes use the same fixed-chain approach: task options set `active` to `true` only for enabled outposts, while inactive outposts are successful no-ops. Neither initialization stage needs progressively shortened `next` candidate lists for every possible enabled-slot combination.

`SellProductLoop` executes Valley IV, Wuling, or automatic region selection according to task configuration. A region entry uses SceneManager to open outpost management, prepares the operator cache, then executes each outpost through `[JumpBack]`:

```text
SellProductLoop                                      (Regional Development main loop)
  ├─ SellProductAuto                                 (recognize the current region)
  │    └─ SellProductValleyIV / SellProductWuling
  ├─ SellProductValleyIVSell                         (enter Valley IV outpost management)
  │    ├─ [Anchor]SellProductValleyIVPrepareOperatorCache (prepare operator cache)
  │    └─ [JumpBack]SellProductRefugeeCamp → SellProductInfraStation
  │       → SellProductReconstructionHQ
  │         (execute three outposts through JumpBack)
  ├─ SellProductWulingSell                           (enter Wuling outpost management)
  │    ├─ [Anchor]SellProductWulingPrepareOperatorCache (prepare/reuse operator cache)
  │    └─ [JumpBack]SellProductSkyKingFlatsConstructionSite
  │       → SellProductCardiacRemediationStation → SellProductXiranflowCloudseederStation
  │         (execute three outposts through JumpBack)
  └─ SellProductTaskEnd                              (all enabled regions are complete)
```

Each outpost binds the generic flow to outpost-specific anchors, switches to the planned contact operator, and enters the common sell loop. When selling ends, the restoration anchor assigns the production operator chosen by the global plan before returning to the region node.

```text
SellProduct{LocationId}                              (recognize/enter the target outpost)
  └─ [Anchor]SellProductBeforeSellOperator           (check and assign selling operator)
       └─ SellProductSellLoop                        (select and sell goods by priority)
            └─ SellProductSellLoopEnd                (vouchers or candidates exhausted)
                 └─ [Anchor]SellProductAfterSellOperator (restore operator and return)
```

When outpost management is locked, `SellProductOutpostLocked` returns to the regional loop. If submitted aid exceeds the outpost's voucher exchange limit, `SellProductAidQuotaExceededStop` stops the entire task. The limit dialog is not confirmed automatically.

## Automatic Operator Rules

`assets/data/SellProduct/selection_data.json` contains the operator candidates, bonus types, multilingual names, and stable ordering derived from `tools/pipeline-generate/data/settlement_trade.json`. Go uses this data to plan selling and restoration operators for enabled outposts, and Pipeline performs the corresponding list operations.

Automatic selection has two phases—pre-sell assignment and post-sell restoration. Both use the same “check current operator → open list → scan pages → replan from the complete owned set” loop:

```text
[Anchor]SellProductBeforeSellOperator                    (enter pre-sell assignment for this outpost)
  └─ SellProduct{LocationId}BeforeSellOperator
       ├─ SellProduct{LocationId}CurrentTargetOperator   (first planned candidate is already assigned)
       │    └─ SellProductSellLoop
       └─ SellProduct{LocationId}OpenTargetOperatorList  (open the contact-operator list)
            └─ SellProduct{LocationId}InTargetOperatorList
                 ├─ SelectTargetOperator → ConfirmTargetOperator
                 │    → CloseTargetOperatorLiaison → TargetOperatorDone
                 │      (assign the planned candidate and enter the sell loop)
                 ├─ TargetOperatorAlreadyAssigned → CancelTargetOperatorAlreadyAssigned
                 │    → CloseTargetOperatorLiaisonAfterAlreadyAssigned → OpenTargetOperatorList
                 │      (candidate is assigned elsewhere; temporarily exclude it and restart at the top)
                 ├─ SwipeTargetOperatorList → InTargetOperatorList
                 │      (no match on this page; continue scanning downward)
                 ├─ RetryTargetOperatorAfterScan → CloseTargetOperatorLiaisonAfterScan
                 │    → OpenTargetOperatorList
                 │      (refresh ownership at the bottom, then reopen once for the new plan)
                 ├─ TargetOperatorNotFound              (no candidate after a complete scan; stop task)
                 └─ TargetOperatorScanFailed            (scan/cache failure; stop task)

[Anchor]SellProductAfterSellOperator                     (restore production operator after selling)
  └─ SellProduct{LocationId}AfterSellOperator
       ├─ SellProduct{LocationId}CurrentRestoreOperator  (restore target is already assigned)
       └─ SellProduct{LocationId}OpenRestoreOperatorList
            └─ SellProduct{LocationId}InRestoreOperatorList
                 ├─ SelectRestoreOperator → ConfirmRestoreOperator
                 │    → CloseRestoreOperatorLiaison → RestoreOperatorDone
                 │      (assign and lock the restored location -> operator pair)
                 ├─ RestoreOperatorAlreadyAssigned → CancelRestoreOperatorAlreadyAssigned
                 │    → CloseRestoreOperatorLiaisonAfterAlreadyAssigned → OpenRestoreOperatorList
                 │      (restoration candidate is assigned elsewhere; temporarily exclude and replan)
                 ├─ SwipeRestoreOperatorList → InRestoreOperatorList
                 │      (no match on this page; continue scanning downward)
                 ├─ RetryRestoreOperatorAfterScan → CloseRestoreOperatorLiaisonAfterScan
                 │    → OpenRestoreOperatorList
                 │      (reallocate from complete ownership and retry once)
                 ├─ RestoreOperatorNotFoundAtBottom → CloseRestoreOperatorLiaisonAfterNotFound
                 │    → SkipRestoreOperatorDone          (record unavailable restoration and continue)
                 └─ RestoreOperatorScanFailed            (scan/cache failure; stop task)
```

Selling-operator priority is fixed:

1. Both EXP and credit bonuses;
2. Credit bonus only;
3. EXP bonus only;
4. Within the same bonus tier, keep the currently assigned operator first; when a switch is required, prefer the candidate that lets the global restoration plan keep more selling operators and leaves assignments reusable by later runs;
5. Use the stable in-game operator order when the global restoration result is still tied.

`selection_data.json` retains a `bonus_tier` for every selling candidate so stable ordering is not mistaken for a benefit difference. If the current assignment belongs to the best available tier, Pipeline keeps it without opening the operator list. Otherwise, Go evaluates the global restoration plan for each candidate in that tier.

The owned-operator cache is stored in `debug/record/SellProductOwnedOperators.json` and partitioned by hashed UID:

- The cache stores only complete list-scan snapshots. An account partition is treated as a consumable complete snapshot even when the owned list is empty.
- If the current account has no snapshot, Pipeline performs a full operator-list scan and writes the cache before planning or selling.
- Existing snapshots are reused directly. “Force refresh before this run” ignores the existing snapshot and performs one full scan when the task first enters a region; later regions in the same task reuse the result.
- Only the global scan that creates the first snapshot or performs an explicit forced refresh may write the cache. Local scrolling while selecting an operator never overwrites an existing snapshot.
- Planning and selection use only the real owned set from a complete snapshot. Incomplete observations are never treated as a theoretical optimum.
- After a full scan, if a refresh or replan changes the target, Pipeline may close and reopen the list once to execute the new plan.
- Pipeline must recognize the list, click the candidate, recognize Assign, and confirm the return to the outpost before committing the switch.
- If assigning opens a confirmation that the candidate is already assigned to another outpost, Pipeline cancels the takeover. Go adds that candidate to a task-scoped global exclusion set, clears the unconfirmed assignment, and replans. The exclusion set resets when the next task initializes.

Restoration must prevent one operator from occupying multiple outposts. Go assigns operators in this order:

1. Maximize the number of outposts that can be restored;
2. With equal coverage, keep the selling operator already assigned before selling whenever possible to avoid unnecessary switches;
3. With the same number of operators kept in this run, maximize final assignments that remain in the outpost's best selling-bonus tier so later runs need no switch;
4. With the same number of reusable assignments, choose the plan with the smaller total candidate `Priority`;
5. Lock each confirmed `location -> operator` assignment so later outposts cannot reuse it.

A missing selling target or failed scan stops the task to avoid selling under the wrong operator. An unavailable restoration target can be recorded as skipped so the current outpost can finish.

## Automatic Selling Rules

`selection_data.json` contains prosperity-level entries merged by `itemId`, five-language names, event-item filtering results, and the default order for each outpost. Go applies user-priority overrides to that order. The default order is:

1. Rarity descending;
2. Unit price descending;
3. Stable source order for ties.

The task provides a priority-selling switch that is disabled by default. Enabling it expands six priority slots that directly adjust this list. Configured items move ahead of the default order from slot 1 through 6. Items unavailable at the current outpost are skipped, duplicate selections keep only the earliest slot, and all remaining items retain the default order above.

During execution, after entry into each outpost is confirmed, the UI reports that outpost's selling-operator target, post-sale restoration target, effective selling order, items excluded because they were already confirmed out of stock during this task, and applicable reserve rules; unlisted items are sold without a reserve. It then reports whether the operator was actually kept or switched, the currently selected goods, and completed trades. When the current outpost newly confirms an out-of-stock item, the UI immediately reports the item and outpost names. An operator assigned to another outpost reports the excluded candidate and replanning reason; a new plan produced by a complete scan reports the outpost, purpose, and selected operator. The log also reports an unavailable selling operator, operator-scan failures, and restoration skipped because no restoration operator is available. Every UI message in the task uses the current client language.

Locked goods are absent from the current screen and are skipped naturally. There is no fixed sell-attempt limit. Each round follows this flow:

```text
SellProductSellLoop                                  (unbounded selling loop)
  ├─ [Anchor]SellProductZeroMoneyHandler             (end when no vouchers remain)
  └─ SellProductChangeGoods                          (recognize and click Switch Goods when vouchers remain)
       └─ [Anchor]SellProductSelectPriorityItem      (recognize and click highest priority)
            └─ SellProductSelectNewGoodConfirm       (recognize and click Confirm)
                 └─ [Anchor]SellProductCommitPriorityItem (commit after sell screen returns)
                      └─ [Anchor]SellProductBetterSliding  (apply reserve rule)
                           └─ SellProduct{LocationId}BetterSliding (set sellable quantity)
                                └─ SellProductSell / SellProductSkipToNextSellLoop
                                     (trade or skip because of reserve stock)
                                     └─ SellProductSellLoop
                                          (continue until an exit condition is met)
```

Each round checks the voucher balance before selecting goods. After a goods change, insufficient vouchers also take precedence over an out-of-stock item, preventing traversal of later priority items once vouchers are exhausted. Insufficient vouchers on initial entry produce a notice; after a completed trade, the outpost selling loop ends silently.

`SellProductPriorityItem` Custom Recognition records only a pending item. After Pipeline clicks and confirms it and recognizes the outpost sell screen again, `SellProductPrioritySession` marks it attempted. A failed click or one-frame OCR fluctuation cannot skip a higher-priority item.

When `SellProductZeroProductAfterChangeStillEmpty` confirms zero stock after a goods change, Pipeline calls the `SellProductMarkOutOfStock` anchor bound by the current outpost. Go adds that outpost's last committed `itemId` to a task-wide out-of-stock set, so dynamic selection skips it at later outposts. The set is not persisted and is cleared when the next SellProduct session initializes.

The loop ends only when:

- `SellProductZeroMoney` recognizes insufficient vouchers at the current outpost;
- Every known visible item is either attempted or marked out of stock for this task, and the same set is recognized twice consecutively;
- `SellProductAidQuotaExceededStop` stops the task because the exchange limit was exceeded.

An empty OCR result does not mean “no remaining goods.” Out-of-stock items remain in the stable recognized set but are no longer candidates. Zero stock, a successful trade, or a reserve-based skip continues to the next round.

Independent reserve rules provide six slots, each keyed by stable `itemId`:

- Without a matching rule, BetterSliding uses the default sell-all behavior.
- With a matching rule, `TargetReverse` sells only stock above the reserve.
- If stock is not above the reserve, `SellProductSkipToNextSellLoop` skips the trade.
- Later slots override earlier slots for the same item. Quantity `0` means no reserve.

## Generator

The generator lives under `tools/pipeline-generate/SellProduct/`. `model.mjs` defines outpost IDs, multilingual OCR candidates, task options, and template data from zmdmap. `selection-data.mjs` produces the Go deployment resource at `assets/data/SellProduct/selection_data.json`. `tools/pipeline-generate/data/` is the generator's source-data directory.

| Maintenance entry                                    | Generated artifact                                          |
| ---------------------------------------------------- | ----------------------------------------------------------- |
| `model.mjs`                                          | Shared outpost, region, and multilingual OCR model          |
| `pipeline-template.jsonc`                            | `assets/resource/pipeline/SellProduct/Outposts/*.json`      |
| `pipeline-adb-template.jsonc`                        | `assets/resource_adb/pipeline/SellProduct/Outposts/*.json`  |
| `sell-template.jsonc`                                | `assets/resource/pipeline/SellProduct/Sell.json`            |
| `session-template.jsonc`                             | `assets/resource/pipeline/SellProduct/OperatorSession.json` |
| `task-template.jsonc`                                | `assets/tasks/SellProduct.json`                             |
| `sync-locales.mjs`                                   | Five-language outpost and operator keys                     |
| `selection-data.mjs`                                 | `assets/data/SellProduct/selection_data.json`               |
| `tools/pipeline-generate/data/settlement_trade.json` | Upstream zmdmap trade data                                  |

These files are maintained manually and are outside generator output:

- `assets/resource/pipeline/SellProduct.json`: task entry and region loop;
- `SellProduct/SellCore.json` and `ChangeGoods.json`: common sell and goods-selection flow;
- `SellProduct/OperatorScan.json`: operator-cache scan;
- `SellProduct/ReserveSession.json`: reserve-rule session;
- `agent/go-service/sellproduct/operator_selection.go`: selling-operator filtering and global restoration assignment;
- `agent/go-service/sellproduct/selection_data.go`: load and validate the deployment data shipped with the application;
- `agent/go-service/sellproduct/item_ordering.go`: expand generated ordering, apply user-priority overrides, and select the next item;
- other files under `agent/go-service/sellproduct/`: Custom component integration, session state, cache, and reserve rules.

Common commands:

```shell
# Fetch current zmdmap data and regenerate
pnpm generate:SellProduct

# Fully regenerate from the current cache without network access
node tools/pipeline-generate/SellProduct/sync-locales.mjs
node tools/pipeline-generate/SellProduct/selection-data.mjs
node tools/pipeline-generate/run-all.mjs SellProduct
```

Maintenance rules:

- Do not edit generated artifacts. Change their template or projection and regenerate.
- A new item usually only requires a zmdmap cache update. Add five-language `item.*` text when it needs a reserve-rule label.
- After adding an outpost, check generated region `next`, the SceneManager entry, and both Win and ADB artifacts.
- Temporary event-item exclusions are centralized in `selection-data.mjs` and affect both Task choices and runtime data. Remove the filter and regenerate after upstream drops the event data.
- Reserve-item cases provide `item_id` through `attach`; the quantity `input` provides an integer through `custom_action_param.quantity`.

Before committing, run at least:

```shell
node --test tools/pipeline-generate/SellProduct/data.test.mjs tools/pipeline-generate/SellProduct/selection-data.test.mjs tools/pipeline-generate/SellProduct/sync-locales.test.mjs
# Run from agent/go-service/
go test ./sellproduct
# Return to the repository root
pnpm check
pnpm test
git diff --check
```
