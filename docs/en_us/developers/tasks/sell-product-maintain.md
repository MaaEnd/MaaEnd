# Developer Manual - SellProduct

This document describes the `SellProduct` main flow, automatic operator rules, automatic selling rules, and generator maintenance.

The task follows “Pipeline owns flow, Go owns algorithms.” Pipeline enters screens, recognizes state, and performs each UI action. `agent/go-service/sellproduct/` owns operator planning, item priority, session state, and reserve-quantity calculation.

> [!IMPORTANT]
>
> `assets/data/SellProduct/selection_data.json`, `assets/tasks/SellProduct.json`, `assets/resource/pipeline/SellProduct/OperatorSession.json`, `Loop.json`, and every JSON inside the region-grouped `{Region}/` folders of both resource packs are generated artifacts. Do not edit them directly. Update the model, data projection, or template under `tools/pipeline-generate/SellProduct/`, then regenerate.

## Main Flow

The task starts at `SellProductSchedule` and enters `SellProductMain` when the configured weekday is enabled. Initialization then enters Regional Development, captures the hashed UID, and registers reserve rules and enabled outposts for this run:

```text
SellProductSchedule                                  (Task entry, weekday gate)
  └─ SellProductMain                                 (selling flow entry)
       └─ SellProductEnterRegionalDevelopment        (SceneManager: enter Regional Development)
            └─ SellProductCaptureUid                 (capture hashed UID for account-scoped cache)
                 └─ SellProductInitializeReserveSession (clear previous reserve/selection state)
                      └─ SellProductRegisterReserveRule{1..6} (fixed chain; skip empty slots)
                           └─ SellProductConfigurePrioritySession (record the priority master switch and strict policy)
                                └─ SellProductInitializeOperatorSession (initialize plans and locks)
                                     └─ SellProductRegisterLocation{LocationId} × N (fixed chain; skip inactive outposts)
                                          └─ SellProductOperatorSessionReady
                                               └─ SellProductLoop         (begin region traversal)
```

The six reserve-rule registration nodes are always enabled and form a fixed slot-order chain. Task options override the stable `itemId` only for configured slots. Unconfigured slots keep an empty `item_id`, which the Custom Action treats as a successful no-op. The next node records the independent priority-selling master switch and whether only preferred products may be sold. Outpost registration nodes use the same fixed-chain approach: task options set `active` to `true` only for enabled outposts, while inactive outposts are successful no-ops. Neither initialization stage needs progressively shortened `next` candidate lists for every possible enabled-slot combination.

`SellProductLoop` always executes regions in reverse order so that newer regions are processed first. Outposts within each region still follow the stable order in the generated model. Disabled regions and outposts are skipped by their entries, so the same enabled set always produces the same selling order. A region entry uses SceneManager to open outpost management, prepares the operator cache, then executes each outpost through `[JumpBack]`:

```text
SellProductLoop                                      (Regional Development main loop)
  ├─ SellProductWulingSell                           (enter Wuling outpost management)
  │    ├─ SellProductWulingInitializePrioritySession
  │    │    └─ SellProductWulingRegisterPriorityItem{1..6} (activate regional priority table)
  │    ├─ SellProductWulingPrepareOperatorCache      (prepare/reuse operator cache)
  │    └─ [JumpBack]SellProductSkyKingFlatsConstructionSite
  │       → SellProductCardiacRemediationStation → SellProductXiranflowCloudseederStation
  │         (execute three outposts through JumpBack)
  ├─ SellProductValleyIVSell                         (enter Valley IV outpost management)
  │    ├─ SellProductValleyIVInitializePrioritySession
  │    │    └─ SellProductValleyIVRegisterPriorityItem{1..6} (activate regional priority table)
  │    ├─ SellProductValleyIVPrepareOperatorCache    (prepare operator cache)
  │    └─ [JumpBack]SellProductRefugeeCamp → SellProductInfraStation
  │       → SellProductReconstructionHQ
  │         (execute three outposts through JumpBack)
  └─ SellProductTaskEnd                              (all enabled regions are complete)
```

Each outpost binds both pre-sell and post-sell target anchors in one node, then enters the shared dispatch nodes in `SellCore.json`. The pre-sell dispatcher switches to the planned contact operator before the common sell loop; when selling ends, the post-sell dispatcher assigns production operators from the global plan before returning to the region node.

```text
SellProduct{LocationId}                              (recognize/enter the target outpost)
  └─ SellProduct{LocationId}SetOperatorAnchors       (bind pre-sell and post-sell targets)
       └─ SellProductSellMain                        (shared selling entry)
            └─ SellProductBeforeSellOperator         (shared pre-sell dispatcher)
                 └─ [Anchor]SellProductBeforeSellOperatorTarget
                      └─ SellProductSellLoop         (select and sell goods by priority)
                           └─ SellProductSellLoopEnd (vouchers or candidates exhausted)
                                └─ SellProductAfterSellOperator (shared post-sell dispatcher)
                                     └─ [Anchor]SellProductAfterSellOperatorTarget
```

When outpost management is locked, SceneManager cannot enter it and the task stops. If submitted aid exceeds the outpost's voucher exchange limit, `SellProductAidQuotaExceededStop` stops the entire task. The limit dialog is not confirmed automatically.

## Automatic Operator Rules

`assets/data/SellProduct/selection_data.json` contains the operator candidates, bonus types, multilingual names, and stable ordering derived from `tools/pipeline-generate/data/settlement_trade.json`. Go uses this data to plan selling operators and post-sale production assignments for enabled outposts, and Pipeline performs the corresponding list operations.

Automatic selection has two phases: pre-sell assignment and post-sale production assignment. Both use the same “check current operator → open list → scan pages → replan from the complete owned set” loop. In code, `restore` refers to the post-sale production-assignment phase:

```text
SellProductBeforeSellOperator                            (enter the shared pre-sell dispatcher)
  └─ [Anchor]SellProductBeforeSellOperatorTarget → SellProduct{LocationId}BeforeSellOperator
       ├─ SellProduct{LocationId}CurrentOperatorUncached (assigned operator missing from the snapshot; invalidate it and rescan)
       ├─ SellProduct{LocationId}CurrentTargetOperator   (first planned candidate is already assigned)
       │    └─ SellProductSellLoop
       └─ SellProduct{LocationId}OpenTargetOperatorList  (open the contact-operator list)
            └─ SellProduct{LocationId}InTargetOperatorList
                 ├─ SelectTargetOperator → ConfirmTargetOperator
                 │    → CloseTargetOperatorLiaison → TargetOperatorDone
                 │      (assign the planned candidate and enter the sell loop)
                 ├─ TargetOperatorManagedConflict → CloseTargetOperatorLiaison
                 │      (source outpost is enabled; confirm reassignment to the current outpost)
                 ├─ TargetOperatorProtectedConflict → CancelTargetOperatorAlreadyAssigned
                 │    → CloseTargetOperatorLiaisonAfterAlreadyAssigned → OpenTargetOperatorList
                 │      (source is disabled or unknown; cancel, exclude temporarily, and restart at the top)
                 ├─ SwipeTargetOperatorList → InTargetOperatorList
                 │      (no match on this page; continue scanning downward)
                 ├─ RetryTargetOperatorAfterScan → CloseTargetOperatorLiaisonAfterScan
                 │    → OpenTargetOperatorList
                 │      (replan from the cached snapshot at the bottom, then reopen and retry once)
                 ├─ TargetOperatorNotFound              (no candidate after a complete scan; stop task)
                 └─ TargetOperatorScanFailed            (scan/cache failure; stop task)

SellProductAfterSellOperator                             (enter the shared post-sell dispatcher)
  └─ [Anchor]SellProductAfterSellOperatorTarget → SellProduct{LocationId}AfterSellOperator
       ├─ SellProduct{LocationId}CurrentRestoreOperator  (restore target is already assigned)
       └─ SellProduct{LocationId}OpenRestoreOperatorList
            └─ SellProduct{LocationId}InRestoreOperatorList
                 ├─ SelectRestoreOperator → ConfirmRestoreOperator
                 │    → CloseRestoreOperatorLiaison → RestoreOperatorDone
                 │      (assign and lock the restored location -> operator pair)
                 ├─ RestoreOperatorManagedConflict → CloseRestoreOperatorLiaison
                 │      (source outpost is enabled; confirm reassignment to the current outpost)
                 ├─ RestoreOperatorProtectedConflict → CancelRestoreOperatorAlreadyAssigned
                 │    → CloseRestoreOperatorLiaisonAfterAlreadyAssigned → OpenRestoreOperatorList
                 │      (source is disabled or unknown; cancel, exclude temporarily, and replan)
                 ├─ SwipeRestoreOperatorList → InRestoreOperatorList
                 │      (no match on this page; continue scanning downward)
                 ├─ RetryRestoreOperatorAfterScan → CloseRestoreOperatorLiaisonAfterScan
                 │    → OpenRestoreOperatorList
                 │      (reallocate from the cached snapshot at the bottom, then reopen and retry once)
                 ├─ RestoreOperatorNotFoundAtBottom → CloseRestoreOperatorLiaisonAfterNotFound
                 │    → SkipRestoreOperatorDone          (record unavailable restoration and continue)
                 └─ RestoreOperatorScanFailed            (scan/cache failure; stop task)
```

Selling operators are first grouped by bonus type:

1. Both outpost-prosperity and trade-profit bonuses;
2. Outpost-prosperity bonus only;
3. Trade-profit bonus only;
4. Preserve stable in-game order within the same tier.

Once an outpost's prosperity is maxed, the prosperity bonus no longer applies, and selling candidates are regrouped with the maxed-prosperity tiers: operators with a trade-profit bonus form one tier, and the rest form another.

The in-game operator list order changes with the current outpost. The generator derives the observed rule from zmdmap: sort operators first by the number of matching `settlementFeatures` for that outpost in descending order, then by the numeric portion of `charId` in descending order; rarity does not participate. Selling candidates apply this order only within the same tier, while restoration candidates apply it directly. As a result, when bonus tiers and restoration outcomes are equivalent, the Go planner prefers an operator closer to the top of the list and requires less scrolling.

In this document, the “highest bonus tier” means the intersection of the current outpost's best selling tier and its restoration candidates: operators that perfectly satisfy both selling and restoration (perfect candidates). If the account owns at least one perfect candidate, pre-sell planning considers only those candidates, even when one is currently assigned to another enabled outpost. Only when no perfect candidate is owned does planning fall back to the best available selling tier.

`selection_data.json` retains a `bonus_tier` for every selling candidate so stable ordering is not mistaken for a benefit difference. If the currently assigned operator already belongs to the available highest bonus tier, Pipeline keeps it without opening the operator list. When a switch is required, Go simulates the global restoration plan for each candidate in that tier and compares, in order, the number of restorable outposts, the number of outposts that keep their pre-sell selling operator, the number of final assignments that later runs can sell with directly, and the total candidate `Priority`. Any remaining tie is broken by stable candidate order.

The unified SellProduct cache is stored in `debug/record/SellProductCache.json`. Each hashed-UID partition contains both a complete operator snapshot and outpost prosperity states:

- Both `operators` and `locations` store stable IDs from `selection_data.json`; they do not store localized names or depend on the client language.
- Account keys use the 16-character lowercase hexadecimal salted hash produced by CaptureUID, or `unknown` before a UID is captured. SellProduct no longer performs a second character-replacement pass that could make distinct keys collide.
- `operators` is a complete list-scan snapshot containing `updated_at` and `ids`. A missing or `null` field means scanning has not completed, while an empty `ids` array means a complete scan found no relevant operators.
- The operator snapshot's `updated_at` changes only when a complete scan is written. Outpost-prosperity updates modify only `locations`, so an old operator list cannot appear freshly scanned.
- The cache has no format-version field and does not migrate the old `SellProductOwnedOperators.json` file, flat operator arrays, or caches containing Chinese names. Invalid JSON, an incompatible top-level structure, or an unknown top-level field invalidates the entire cache. When the top-level structure is valid, a non-canonical UID, incompatible account field, invalid timestamp, localized name, empty ID, or ID absent from the current generated data invalidates only that account partition; other accounts remain usable.
- If the current account has no snapshot, Pipeline performs a full operator-list scan and writes the cache before planning or selling.
- Existing snapshots are reused directly. “Force Refresh Operator Cache” ignores the existing snapshot and performs one full scan when the task first enters a region; later regions in the same task reuse the result.
- During the pre-sale check, if the currently assigned operator is recognized as a known operator that is missing from the snapshot, the snapshot is considered stale: Go invalidates the current account's snapshot and Pipeline performs one full operator-list rescan before continuing to plan. This triggers at most once per task and is skipped when the task has already completed a full scan, avoiding endless rescans when the scan itself misses an operator.
- When a complete snapshot is reused, the runtime UI converts the current account's `operators.updated_at` value to local time and reports it once so users can judge the operator-list age.
- Only the global scan that creates the first snapshot or performs an explicit forced refresh may write the cache. Local scrolling while selecting an operator never overwrites an existing snapshot.
- Planning and selection use only the real owned set from a complete snapshot. Incomplete observations are never treated as a theoretical optimum.
- `locations` stores the last confirmed prosperity-limit state for each outpost. The task loads these states for global planning at startup; uncached outposts are treated as not maxed. Entering an outpost still rechecks and persists the latest state, immediately replanning unfinished outposts when it changes.
- When the list reaches the bottom, Go replans with the latest session state. If a viable plan exists, Pipeline closes and reopens the list to retry once with the new plan.
- Pipeline must recognize the list, click the candidate, recognize Assign, and confirm the return to the outpost before committing the switch.
- If assigning opens a confirmation that the candidate is already assigned elsewhere, Pipeline uses `And` to combine the prompt, Go source classification, and the corresponding button. When the source outpost is enabled for this run, Pipeline confirms reassignment to the current outpost. When the source is disabled or cannot be identified reliably, Pipeline cancels, adds the candidate to a task-scoped global exclusion set, and replans. The exclusion set resets when the next task initializes.

Post-sale production assignment must prevent one operator from occupying multiple outposts. Go assigns operators in this order:

1. Maximize the number of outposts that can be restored;
2. With equal coverage, keep the selling operator already assigned before selling whenever possible to avoid unnecessary switches;
3. With the same number of kept operators, maximize final assignments in each outpost's highest bonus tier (perfect for both selling and restoration) so later runs need no switch;
4. With the same number of reusable assignments, choose the plan with the smaller total candidate `Priority` when both plans cover the same outpost set; when they cover different outposts, preserve the newer-region-first order;
5. Lock each confirmed `location -> operator` assignment so later outposts cannot reuse it.

Regional selling uses the same newer-region-first order, while outposts within each region remain in game order. This lets completed assignments in newer regions be locked first, preventing older regions from taking those operators later.

A missing selling target or failed scan stops the task to avoid selling with the wrong operator. An unavailable restoration target is recorded as skipped so the current outpost can finish and the task continues.

## Automatic Selling Rules

`selection_data.json` contains prosperity-level entries merged by `itemId`, five-language names, event-item filtering results, and the default order for each outpost. Go applies user-priority overrides to that order. The default order is:

1. Rarity descending;
2. Unit price descending;
3. Stable source order for ties.

The task provides a priority-selling master switch that is disabled by default and independent of the region selling switches. Enabling it expands “Sell Priority Products Only” plus separate Valley IV and Wuling priority switches; each region then exposes six slots listing only items sold by at least one outpost in that region. The master switch only controls whether these settings affect runtime ordering; it neither clears regional selections nor enables or disables selling in any region. On entering a region, Pipeline replaces both the active-region flag and priority table with that region's settings. By default, configured items move ahead of the default order from slot 1 through 6, duplicate selections keep only the earliest slot, and all remaining items retain the default order above. In strict-priority mode, only regions whose regional priority switch is also enabled are restricted to explicitly configured items available at the current outpost; regions without regional priority enabled continue selling in default order. An enabled region with no applicable selections ends normally after two consecutive stable empty-candidate observations. Switching regions preserves the task-wide out-of-stock set and each outpost's attempted items.

During execution, enabling “Sell Priority Products Only” first makes the UI report that other products will be skipped and that the setting applies only to regions with regional priority selling enabled. After entry into each outpost is confirmed, the UI reports that outpost's selling-operator target, post-sale production-assignment target, effective selling order, items excluded because they were already confirmed out of stock during this task, items skipped because their reserve quantity has been reached, items configured by the user as never sell, and applicable reserve rules. Unlisted items are sold without a reserve in the default mode or when the current region's priority settings are disabled; they are omitted only when strict-priority mode and the current region's priority settings are both enabled. It then reports whether the operator was actually kept or switched, the currently selected goods, and completed trades. When the current outpost newly confirms an out-of-stock item, the UI immediately reports the item and outpost names; when an item reaches its reserve quantity, the UI immediately reports the item name and the reserve quantity. When the currently assigned operator is missing from the snapshot and triggers a rescan, the UI reports that operator's name. For an operator assigned elsewhere, the log reports whether the source outpost is managed by this run; protected candidates also report exclusion and replanning reasons. A new plan produced by a complete scan reports the outpost, purpose, and selected operator. The log also reports an unavailable selling operator, operator-scan failures, and skipped post-sale assignment when no production operator is available. Every UI message in the task uses the current client language.

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
                                 ├─ SellProductSell → SellProductSellCheck
                                 │      (no reserve rule; sell all)
                                 ├─ SellProductSellThenLoop → SellProductSellCheckThenLoop
                                 │      (sell with a reserve; after the trade, check vouchers first and
                                 │        end if exhausted; otherwise mark the item satisfied via
                                 │        SellProductReserveQuantityReached when the reserve quantity is reached,
                                 │        or return to BetterSliding and continue selling)
                                 └─ SellProductReserveAlreadySatisfied
                                      (stock not above the reserve; mark satisfied and skip)
                                     └─ SellProductSellLoop
                                          (continue until an exit condition is met)
```

Each round checks the voucher balance before selecting goods. After a goods change, the insufficient-voucher check still takes precedence over an out-of-stock item, preventing traversal of later priority items once vouchers are exhausted. After a reserve-based trade completes, the flow also checks vouchers before deciding whether the reserve has been reached or running BetterSliding again. Insufficient vouchers on initial entry produce a notice; after a completed trade, the outpost selling loop ends silently.

`SellProductPriorityItem` Custom Recognition only records the selected item as pending during recognition. After Pipeline clicks and confirms it and recognizes the outpost sell screen again, `SellProductPrioritySession` marks it attempted. A failed click or one-frame OCR fluctuation cannot skip a higher-priority item.

When `SellProductZeroProductAfterChangeStillEmpty` confirms zero stock after a goods change, Pipeline calls the `SellProductMarkOutOfStock` anchor bound by the current outpost. Go adds that outpost's last committed `itemId` to a task-wide out-of-stock set, so dynamic selection skips it at later outposts. The set is not persisted and is cleared when the next SellProduct session initializes.

The loop ends only when:

- `SellProductZeroMoney` recognizes insufficient vouchers at the current outpost;
- Every known visible item is unavailable as a candidate (attempted, out of stock, reserve-satisfied, or never-sell), and the same set is recognized twice consecutively;
- `SellProductAidQuotaExceededStop` stops the task because the exchange limit was exceeded.

An empty OCR result does not mean “no remaining goods.” Out-of-stock items remain in the stable recognized set but are no longer candidates. Zero stock, a completed trade, reaching the reserve quantity, or a reserve-based skip continues to the next round.

Independent reserve rules provide six slots. Each stable `itemId` can use either **Keep Specified Quantity** or **Never Sell**:

- Without a matching rule, BetterSliding uses the default sell-all behavior.
- **Keep Specified Quantity** uses `ReverseTarget` to sell only stock above the reserve.
- When a single trade reaches the reserve quantity, the trade confirmation marks the item as satisfied for this task via `SellProductReserveQuantityReached`; below the reserve quantity, the flow returns to the quantity slider and keeps selling the current item.
- If stock is not above the reserve, `SellProductReserveAlreadySatisfied` skips the trade and likewise marks the item as satisfied for this task.
- Satisfied items are skipped during goods selection at later outposts, with a runtime notice on the first mark.
- **Never Sell** excludes the item during selection, before switching goods, and does not report it as out of stock. Internally it uses quantity `-1`; users do not enter this sentinel value.
- Later slots override earlier slots for the same item. Quantity `0` means no reserve.

## Generator

The generator lives under `tools/pipeline-generate/SellProduct/`. `model.mjs` defines outpost IDs, multilingual OCR candidates, task options, and template data from zmdmap. `selection-data.mjs` produces the Go deployment resource at `assets/data/SellProduct/selection_data.json`. `tools/pipeline-generate/data/` is the generator's source-data directory.

| Maintenance entry                                    | Generated artifact                                                       |
| ---------------------------------------------------- | ------------------------------------------------------------------------ |
| `model.mjs`                                          | Shared outpost, region, and multilingual OCR model                       |
| `pipeline-template.jsonc`                            | `assets/resource/pipeline/SellProduct/{Region}/{Location}.json`          |
| `pipeline-adb-template.jsonc`                        | `assets/resource_adb/pipeline/SellProduct/{Region}/{Location}.json`      |
| `sell-template.jsonc`                                | `assets/resource/pipeline/SellProduct/{Region}/SellProduct{Region}.json` |
| `loop-template.jsonc`                                | `assets/resource/pipeline/SellProduct/Loop.json`                         |
| `session-template.jsonc`                             | `assets/resource/pipeline/SellProduct/OperatorSession.json`              |
| `task-template.jsonc`                                | `assets/tasks/SellProduct.json`                                          |
| `sync-locales.mjs`                                   | Five-language outpost and operator keys plus missing item keys           |
| `selection-data.mjs`                                 | `assets/data/SellProduct/selection_data.json`                            |
| `tools/pipeline-generate/data/settlement_trade.json` | Upstream zmdmap trade data                                               |

These files are maintained manually and are outside generator output:

- `assets/resource/pipeline/SellProduct.json`: task entry and initialization chain (region entry and main-loop nodes are generated);
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
- A new item usually only requires a zmdmap cache update. `sync-locales.mjs` adds five-language `item.*` keys for items not covered by an existing key, while items whose Chinese name matches an existing key keep using that key.
- After adding an outpost, check generated region `next`, the SceneManager entry, and both Win and ADB artifacts.
- After adding a region, create its folder under `SellProduct/` in both resource packs before generating; the generator only creates `outputDir` and does not create region subdirectories.
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
