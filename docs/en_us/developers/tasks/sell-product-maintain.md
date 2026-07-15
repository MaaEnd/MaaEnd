# Developer Manual - SellProduct Maintenance Documentation

This document explains the generation pipeline, Pipeline organization, global item priorities, automatic operator selection, and maintenance procedures for adding new outposts or items to the `SellProduct` task.

The core feature of `SellProduct` is **zmdmap data-driven + Pipeline template generation**: outposts, sellable items, task options, and outpost repeat nodes are not manually written one by one, but are batch-rendered by `tools/pipeline-generate/SellProduct/` after reading `tools/pipeline-generate/data/settlement_trade.json`. The `settlement_trade.json` is downloaded and cached from the zmdmap API via `pnpm fetch:zmdmap`.

> [!IMPORTANT]
>
> `assets/tasks/SellProduct.json`, `assets/resource/pipeline/SellProduct/OperatorSession.json`, `assets/resource/pipeline/SellProduct/Outposts/*.json`, and `assets/resource_adb/pipeline/SellProduct/Outposts/*.json` are all **generated artifacts**. Do not edit these files directly; to modify outposts, item lists, automatic operator registrations, priority item candidates, sell attempt templates, or Win/ADB coordinates, modify the data assembly or templates under `tools/pipeline-generate/SellProduct/`, then regenerate.

## Overview

The core maintenance points for SellProduct are as follows:

| Module                           | Path                                                              | Purpose                                                                                                                                    |
| -------------------------------- | ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| zmdmap Cached Data               | `tools/pipeline-generate/data/settlement_trade.json`              | Raw data for outposts, prosperity, tradeable items, multilingual names, rarity, unit price, etc.                                           |
| Shared Outpost Model             | `tools/pipeline-generate/SellProduct/model.mjs`                   | Reads zmdmap and derives `RegionPrefix`, `LocationId`, multilingual OCR candidates, and locale keys.                                       |
| Win Template Data                | `tools/pipeline-generate/SellProduct/pipeline-data.mjs`           | Exposes only the outpost fields and quantity boxes required by the Win Pipeline template.                                                  |
| ADB Template Data                | `tools/pipeline-generate/SellProduct/pipeline-adb-data.mjs`       | Exposes only the outpost IDs and quantity boxes required by the ADB Pipeline template.                                                     |
| Session Template Data            | `tools/pipeline-generate/SellProduct/session-data.mjs`            | Exposes only the fields required by automatic-operator outpost registration nodes.                                                        |
| Task Template Data               | `tools/pipeline-generate/SellProduct/task-data.mjs`               | Generates global item priorities, cache refresh, and region/outpost switches.                                                             |
| Outpost Pipeline Template        | `tools/pipeline-generate/SellProduct/pipeline-template.jsonc`     | Generates each outpost selling node for the Win resource pack.                                                                             |
| Session Pipeline Template        | `tools/pipeline-generate/SellProduct/session-template.jsonc`      | Generates automatic-mode outpost registration nodes.                                                                                       |
| ADB Outpost Template             | `tools/pipeline-generate/SellProduct/pipeline-adb-template.jsonc` | Generates outpost quantity OCR override nodes for the ADB resource pack.                                                                   |
| Task Option Template             | `tools/pipeline-generate/SellProduct/task-template.jsonc`         | Generates global priorities, cache refresh, and region/outpost switches in `assets/tasks/SellProduct.json`.                               |
| Win Outpost Generation Config    | `tools/pipeline-generate/SellProduct/pipeline-config.json`        | Outputs to `assets/resource/pipeline/SellProduct/Outposts/${LocationId}.json`.                                                             |
| ADB Outpost Generation Config    | `tools/pipeline-generate/SellProduct/pipeline-adb-config.json`    | Outputs to `assets/resource_adb/pipeline/SellProduct/Outposts/${LocationId}.json`.                                                         |
| Task Option Generation Config    | `tools/pipeline-generate/SellProduct/task-config.json`            | Outputs to `assets/tasks/SellProduct.json`.                                                                                                |
| Session Generation Config        | `tools/pipeline-generate/SellProduct/session-config.json`         | Outputs to `assets/resource/pipeline/SellProduct/OperatorSession.json`.                                                                    |
| Task Entry                       | `assets/resource/pipeline/SellProduct.json`                       | Main loop and region entry; manually maintained.                                                                                           |
| Region Sell Entry                | `assets/resource/pipeline/SellProduct/Sell.json`                  | `next` list for region to outpost mapping; manually maintained.                                                                            |
| Generic Sell Core                | `assets/resource/pipeline/SellProduct/SellCore.json`              | Sell loop, out-of-stock/dispatch ticket insufficient/exchange limit exceeded handling, final trade flow.                                   |
| Generic Change Goods Flow        | `assets/resource/pipeline/SellProduct/ChangeGoods.json`           | Enter goods selection interface, select priority item or default item.                                                                     |
| Generic Outpost Recognition      | `assets/resource/pipeline/SellProduct/EnterOutpost.json`          | Outpost interface, region outpost page, and outpost management text recognition.                                                           |
| Contact Operator Recognition     | `assets/resource/pipeline/SellProduct/Operator.json`              | Contact operator list interface and open button recognition.                                                                               |
| Automatic Operator Session/Logic | `agent/go-service/sellproduct/operator_*.go`                      | Theoretical/exact candidates, cache completeness, task sessions, and unique restoration assignment.                                        |
| ADB Generic Sell Core            | `assets/resource_adb/pipeline/SellProduct/SellCore.json`          | Generic sell core under the ADB resource pack.                                                                                             |
| Priority Item Custom Recognition | `agent/go-service/sellproduct/normalized_match.go`                | `SellProductNormalizedItemMatch`, performs noise-resistant exact matching on OCR results and candidate names.                              |
| Multilingual Text                | `assets/locales/interface/*.json`                                 | `SellProduct` task text, outpost names, item labels.                                                                                       |
| Generation Entry                 | `package.json`'s `generate:SellProduct` / `fetch:zmdmap`          | Updates zmdmap cache and re-renders generated artifacts.                                                                                   |

## Generated Artifacts vs. Handwritten File Boundary

### Generated Artifacts

The following files are rendered by `@joebao/maa-pipeline-generate` and will be overwritten upon regeneration:

- `assets/tasks/SellProduct.json`
- `assets/resource/pipeline/SellProduct/Outposts/*.json`
- `assets/resource/pipeline/SellProduct/OperatorSession.json`
- `assets/resource_adb/pipeline/SellProduct/Outposts/*.json`

The sources for these files are:

| Artifact                          | Template                      | Data Source                           |
| --------------------------------- | ----------------------------- | ------------------------------------- |
| `assets/tasks/SellProduct.json`   | `task-template.jsonc`         | `task-data.mjs` + `model.mjs`         |
| Win Outpost Pipeline              | `pipeline-template.jsonc`     | `pipeline-data.mjs` + `model.mjs`     |
| Automatic operator registrations  | `session-template.jsonc`      | `session-data.mjs` + `model.mjs`      |
| ADB Outpost Quantity OCR Override | `pipeline-adb-template.jsonc` | `pipeline-adb-data.mjs` + `model.mjs` |

### Handwritten Maintenance Files

The following files are not overwritten by the SellProduct generator and must be manually updated by maintainers based on business changes:

- `assets/resource/pipeline/SellProduct.json`
- `assets/resource/pipeline/SellProduct/Sell.json`
- `assets/resource/pipeline/SellProduct/SellCore.json`
- `assets/resource/pipeline/SellProduct/ChangeGoods.json`
- `assets/resource/pipeline/SellProduct/EnterOutpost.json`
- `assets/resource/pipeline/SellProduct/Operator.json`
- `assets/resource/pipeline/SellProduct/OperatorScan.json`
- `assets/resource_adb/pipeline/SellProduct/SellCore.json`
- `agent/go-service/sellproduct/*.go`
- `assets/locales/interface/*.json`

When adding new regions or outposts, the generator can create task options and outpost nodes, but the region entry, region-to-outpost `next` list, SceneManager jump nodes, and outpost management page entry recognition may still need to be manually supplemented.

## Naming Conventions & Data Model

### Outpost Node ID (`LocationId`)

`LocationId` is the prefix and filename for generated outpost nodes:

```text
assets/resource/pipeline/SellProduct/Outposts/${LocationId}.json
assets/resource_adb/pipeline/SellProduct/Outposts/${LocationId}.json
```

`LocationId` is always derived from the current zmdmap English outpost name converted to PascalCase; handwritten ID overrides are not used. If the English name changes, regeneration updates node names, filenames, Task options, and locale keys together.

`LocationId` is only used for node names and filenames, not for display text. The outpost name displayed in the user interface is provided by `task.SellProduct.{RegionPrefix}{LocationId}` in `assets/locales/interface/*.json`.

### Region Prefix (`RegionPrefix`)

`RegionPrefix` is the region ID used by task options and region entry nodes, e.g., `ValleyIV`, `Wuling`. It is mapped from zmdmap's `domainId` by `DOMAIN_REGION_PREFIX`.

When adding a new region, do not rely directly on default fallback names like `domain_3`; first configure a stable project region ID in `DOMAIN_REGION_PREFIX`.

### zmdmap Data Fields

`settlement_trade.json` currently mainly provides:

- `settlements`: List of outposts, with keys like `stm_tundra_1`.
- `settlement.domainId`: The region the outpost belongs to, e.g., `domain_1`, `domain_2`.
- `settlement.settlementName`: Multilingual outpost names.
- `settlement.byProsperityLevel[*].tradeItems`: List of tradeable items under different prosperity levels.
- `tradeItems[*].itemId`: Item ID.
- `tradeItems[*].name`: Multilingual item name.
- `tradeItems[*].rarity` / `unitPrice`: Used for sorting to generate priority item options.

`model.mjs` normalizes this raw data into `sellProductLocations`; the Win, ADB, session, and Task data projections then build their own minimal template rows.

The currently generated outposts are:

| zmdmap settlementId | Region   | LocationId                     | Outpost Name                     |
| ------------------- | -------- | ------------------------------ | -------------------------------- |
| `stm_tundra_1`      | ValleyIV | `RefugeeCamp`                  | Refugee Camp                     |
| `stm_tundra_2`      | ValleyIV | `InfraStation`                 | Infra-Station                    |
| `stm_tundra_3`      | ValleyIV | `ReconstructionHQ`             | Reconstruction HQ                |
| `stm_hongs_1`       | Wuling   | `SkyKingFlatsConstructionSite` | Sky King Flats Construction Site |
| `stm_hongs_2`       | Wuling   | `CardiacRemediationStation`    | Cardiac Remediation Station      |
| `stm_hongs_3`       | Wuling   | `XiranflowCloudseederStation`  | Xiranflow Cloudseeder Station    |

## Auto-Generation Mechanism

### Running Commands

```shell
# Recommended: Run in repository root to automatically update zmdmap cache and regenerate
pnpm generate:SellProduct

# Only update zmdmap cache
pnpm fetch:zmdmap

# If cache is already updated, you can also render individually in the generator directory
cd tools/pipeline-generate/SellProduct
npx @joebao/maa-pipeline-generate --config pipeline-config.json
npx @joebao/maa-pipeline-generate --config session-config.json
npx @joebao/maa-pipeline-generate --config task-config.json
npx @joebao/maa-pipeline-generate --config pipeline-adb-config.json
```

### Win Outpost Pipeline: `pipeline-config.json`

```json
{
    "template": "pipeline-template.jsonc",
    "data": "pipeline-data.mjs",
    "outputDir": "../../../assets/resource/pipeline/SellProduct/Outposts",
    "outputPattern": "${LocationId}.json",
    "format": true,
    "merged": false
}
```

Each row of data generates one Win resource pack outpost file.

### ADB Outpost Pipeline: `pipeline-adb-config.json`

```json
{
    "template": "pipeline-adb-template.jsonc",
    "data": "pipeline-adb-data.mjs",
    "outputDir": "../../../assets/resource_adb/pipeline/SellProduct/Outposts",
    "outputPattern": "${LocationId}.json",
    "format": true,
    "merged": false
}
```

The ADB outpost template does not fully copy the Win outpost flow; instead, it only generates override configurations for each outpost's 4 `BetterSliding` nodes. These replace the quantity OCR area with `QuantityBoxAdb` and `MaxTargetBoxAdb`, while the rest of the outpost flow continues to reuse the node structure generated by the Win resource pack.

### Task Options: `task-config.json`

```json
{
    "task": true,
    "template": "task-template.jsonc",
    "data": "task-data.mjs",
    "outputDir": "../../../assets/tasks/",
    "outputFile": "SellProduct.json",
    "format": true
}
```

This configuration generates global item priorities, forced operator-cache refresh, and region/outpost sell switches in the user interface.

### Shared Model and Template Projections

`tools/pipeline-generate/SellProduct/model.mjs` is the shared maintenance entry point for outpost naming, OCR, and locale data. `pipeline-data.mjs`, `pipeline-adb-data.mjs`, `session-data.mjs`, and `task-data.mjs` contain template-specific data.

It currently handles:

1. Reading `tools/pipeline-generate/data/settlement_trade.json`.
2. Reverse-looking up `item.*` keys from `assets/locales/interface/zh_cn.json` to generate task option labels as `$item.xxx` where possible.
3. Building a global item dictionary from zmdmap's `tradeItems`.
4. Aggregating sellable items per outpost and sorting them by `rarity` and `unitPrice` in descending order.
5. Mapping `domainId` to the `RegionPrefix` used by the task.
6. `model.mjs` derives `LocationId` from the English outpost name and builds OCR `TextExpected` from the five-language `settlementName` data.
7. The four projections inject Win / ADB quantity OCR boxes, the session registration chain, and Task options separately.

### OCR Compatibility Aliases

`TextExpected` is generated directly from the full CN / TC / JP / KR / EN strings in zmdmap. Add a candidate to `SETTLEMENT_OCR_ALIASES` only when there is actual evidence of a stable OCR error or UI truncation. Aliases are appended and never replace the official full strings.

### Region Mapping Override

`DOMAIN_REGION_PREFIX` is responsible for mapping zmdmap's `domainId` to the project's region ID:

```js
const DOMAIN_REGION_PREFIX = {
    domain_1: "ValleyIV",
    domain_2: "Wuling",
};
```

When integrating a new region, if zmdmap adds a `domain_3`, a stable `RegionPrefix` usually needs to be added here first. Unconfigured domains will fall back to `toPascalCase(domainId)`, which is generally unsuitable for direct use as a user-visible configuration and Pipeline prefix.

### Temporary Exclusion of Event Items

`TEMP_EXCLUDED_ITEM_CN_NAMES` is used to temporarily exclude event items that still appear in the zmdmap data but should no longer appear in the sell configuration.

Maintenance rules:

- Only for short-term compatibility with event data.
- The comment should clearly state the deletion condition.
- When the zmdmap data is updated and the event items are confirmed removed, the corresponding exclusion should be deleted.

### Priority Item Candidate Names

Each generated priority item option will override the corresponding node:

```text
SellProduct{LocationId}SelectItem{N}
```

The override content includes:

- `enabled: true`
- `custom_recognition_param.candidates`
- Miss handler anchor

`candidates` come from the zmdmap CN / TC / JP / EN names. The English name has certain symbols that might interfere with matching removed before entering the candidates.

## Main Flow

The overall flow can be understood via the following pipeline:

```text
SellProductMain
-> SellProductCaptureUid
-> SellProductInitializeOperatorSession / SellProductRegisterAuto{LocationId}
-> SellProductLoop
-> SellProductAuto / SellProductValleyIV / SellProductWuling
-> SellProduct{Region}Sell
-> SellProduct{LocationId}
-> SellProduct{LocationId}Sell
-> SellProduct{LocationId}SetBeforeSellOperatorAnchor
-> SellProduct{LocationId}SetAfterSellOperatorAnchor
-> SellProduct{LocationId}BeforeSellOperator
-> SellProductSellLoop
-> SellProduct{LocationId}SellAttempt{1..4}
-> SellProductChangeGoods
-> SellProduct{LocationId}SelectItem{1..4} / SellProductSelectFirstGood / SellProductSelectNextGood
-> SellProduct{LocationId}BetterSliding{1..4}
-> SellProductSell
-> SellProductSellCheck / SellProductSellCheckThenLoop
-> SellProductSellLoop or SellProductSellLoopEnd
-> SellProduct{LocationId}AfterSellOperator
```

Key points:

- The task starts directly at `SellProductMain`; day-of-week scheduling is no longer exposed.
- `SellProductCaptureUid` captures the hashed UID, then initializes the task-scoped automatic operator session and registers enabled outposts.
- `SellProductLoop` continues execution only in the region construction interface; when not in the target interface, it hands off to `SceneEnterMenuRegionalDevelopment`.
- `SellProductAuto` automatically selects Valley IV or Wuling based on the current region construction page.
- `SellProduct{Region}Sell` enters the outpost management page of the corresponding region, then traverses all outposts in that region via `next`.
- Each outpost node is generated by a template and handles outpost recognition, tab selection, sell anchors, and automatic optimal-operator selection.
- Before selling, `SellProduct{LocationId}BeforeSellOperator` checks the current operator. If needed, it opens the operator list, selects the planned operator, and confirms the assignment after the button changes to "Assign"; the original production role is restored after selling.
- `SellProductSellLoop` strings up to 4 sell attempts via anchors.
- Each attempt first changes goods, then uses BetterSliding to adjust the quantity to the target value, and finally clicks Trade.
- `SellProductSellLoopEnd` enters `SellProduct{LocationId}AfterSellOperator` through the `SellProductAfterSellOperator` anchor and ends the outpost flow after restoration.

## How Task Options Modify Pipeline

`assets/tasks/SellProduct.json` is generated by `task-template.jsonc`. The configuration selected by the user in the interface modifies the Pipeline via `pipeline_override`.

### Retained Options

| Option                                 | Behavior                                                                                                     |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `SellProductGlobalItemPriority`        | Enables global priority items; when disabled, the first two sales use default items                          |
| `SellProductPriorityItem{1..4}`        | Four sibling priority options under the global switch, shared by every outpost                               |
| `SellProductForceRefreshOperatorCache` | Controls whether this run performs a complete operator-cache refresh before selling                          |
| `{RegionPrefix}Sell`                   | Enables or disables a whole region and expands that region's outpost switches                                |
| `{RegionPrefix}{LocationId}`           | Enables or disables one outpost together with its `SellProductRegisterAuto{LocationId}` session registration |

Scheduling, automatic quota confirmation, manual operator selection, per-outpost attempt counts, and reserve quantities have been removed. Fixed behavior is now: stop with a prompt when the exchange quota is exceeded, sell the full selected quantity, and always use and restore automatically selected optimal operators at enabled outposts.

### Automatic Operator Selection State Machine

Pipeline and Go cooperate on automatic selection. Pipeline owns list navigation, scrolling, closing, and assignment confirmation. Go owns candidate data, cache state, and restoration assignment.

After UID capture, the task entry executes:

```text
SellProductInitializeOperatorSession
-> SellProductRegisterAuto{LocationId} (enabled automatic outposts only)
-> SellProductOperatorSessionReady
-> SellProductLoop
```

Initialization clears the previous task's scan completion, plans, and restoration locks. `session-template.jsonc` generates one disabled registration node per outpost, and the outpost sell switch enables it through `pipeline_override`. After adding an outpost, regenerate `OperatorSession.json` and append its registration node to the handwritten `SellProductInitializeOperatorSession.next` list.

#### Cache Completeness

The owned-operator cache is stored at `debug/record/SellProductOwnedOperators.json` and partitioned by hashed UID. Each account contains:

```json
{
    "updated_at": "2026-07-15T00:00:00Z",
    "operators": [
        "Perlica",
        "Laevatain"
    ],
    "complete": true
}
```

- `complete: false` means the operators were only observed on part of the list; absent candidates remain unknown.
- `complete: true` means the list was traversed from top to bottom and can drive an exact plan.
- v1 caches migrate in memory to complete v2 snapshots and are persisted in the new structure on the next write.
- Partial matches only add operators. Only reaching the end of a full traversal may replace the candidate domain and set `complete: true`.

#### Theoretical and Exact Plans

With an incomplete cache, Go assumes every relevant candidate may be owned and computes a theoretical optimum. Each search targets only the first planned candidate:

```text
Compute theoretical optimum
-> Current operator already matches: continue
-> Open list and search only for the planned candidate
   -> Found: select and record a partial observation
   -> Reached bottom without a match: write a complete cache and replan
      -> New plan exists: close and reopen the list, then select once
      -> No selling candidate: stop with an actionable message
      -> No restoration candidate: close the list and continue
```

The second selection is not a blind retry: the first traversal produced a new authoritative ownership set and therefore a different plan. If a candidate derived from the just-completed scan still cannot be recognized, treat it as an OCR or UI-state error instead of looping.

Selling priority is: both EXP and credit bonuses, credit only, then EXP only. Ties follow the stable in-game operator order. Search must not choose a lower-priority candidate visible on the current page.

#### Restoration Assignment and Locks

Restoration must prevent one operator from occupying multiple outposts. Go solves only for automatic outposts registered in the current session:

1. Maximize the number of restored outposts.
2. Minimize total candidate priority among plans with equal coverage.
3. Lock each confirmed `location -> operator` result.
4. After a cache refresh, replan only unfinished outposts and exclude locked operators. An outpost confirmed to have no restoration candidate after a full scan is also marked complete, so it cannot keep reserving a shared operator.

`SellProduct{LocationId}CurrentRestoreOperator` locks immediately when the current operator is already correct. The normal selection path locks at `SellProduct{LocationId}RestoreOperatorDone`, after the Pipeline has confirmed the return to the outpost screen.

#### Forced Refresh

"Force refresh before this run" sets the task session to `refresh`. `SellProductScanOperatorList` runs before regional selling only while the current task has not completed a full scan. The completion marker is task-scoped and is not reused by later runs; one successful scan is shared by all regions and outposts in the same task.

### Global Item Priorities

`SellProductGlobalItemPriority` is disabled by default. In that mode, priority-item recognition is skipped and every enabled outpost performs its first two sales with default items. When enabled, the four `SellProductPriorityItem{1..4}` slots apply uniformly to every enabled outpost:

- Priorities 1 and 2 default to `Auto`, selecting the first and second items from each outpost's rarity-then-unit-price ordering.
- Priorities 3 and 4 default to `None`, so their sell attempts do not run.
- Selecting a concrete item enables that slot only at outposts that actually sell the item. Other outposts skip the slot instead of accidentally selling a fallback item.
- Matching items use the default BetterSliding configuration and sell the full available quantity; reserve quantities are no longer configurable.
- If an enabled priority item is not recognized, `SellProductPriorityGoodMissWarning` still reports the miss and falls back to the default item.

## Priority Item Recognition

The priority item node uses Go custom recognition:

```text
SellProductNormalizedItemMatch
```

Implementation file:

```text
agent/go-service/sellproduct/normalized_match.go
```

This recognizer runs OCR within the ROI of the goods selection interface, then performs two layers of strict matching on the OCR text and `candidates`:

1. Tier A: Strips whitespace, brackets, vertical bars, hyphens, periods, enumeration commas, etc., and standardizes ASCII case before strict equality check.
2. Tier B: Based on Tier A, strips ASCII letters and numbers, used to handle cases where CJK names are mixed with English noise.

When maintaining, note:

- Do not change it to loose edit distance matching, as it could easily mis-match "Citrus Can" to "Premium Citrus Can" or "Select Citrus Can".
- When adding candidate names, prioritize generating them from zmdmap multilingual names.
- If OCR has fixed noise, add evidence-backed candidates to `SETTLEMENT_OCR_ALIASES` in `model.mjs` rather than expanding the matching algorithm.
- After modifying the matching algorithm, run the regression test covered by `agent/go-service/sellproduct/normalized_match_test.go`.

## BetterSliding & Quantity Regions

Each outpost generates 4 BetterSliding nodes:

```text
SellProduct{LocationId}BetterSliding1
SellProduct{LocationId}BetterSliding2
SellProduct{LocationId}BetterSliding3
SellProduct{LocationId}BetterSliding4
```

Default parameters:

- `Target: 999999`
- `ClampTargetToMax: true`
- `Direction: "right"`
- `MaxTarget.Box`: Reads the maximum sellable quantity.
- `Quantity.Box`: Reads the current trade quantity.
- `ExceedingOverrideEnable: "SellProductSkipToNextSellLoop"`

Quantity regions are maintained separately in `pipeline-data.mjs` and `pipeline-adb-data.mjs`:

| Constant               | Purpose                                         |
| ---------------------- | ----------------------------------------------- |
| `QUANTITY_BOX`         | Win resource pack current trade quantity OCR    |
| `MAX_QUANTITY_BOX`     | Win resource pack maximum sellable quantity OCR |
| `QUANTITY_BOX_ADB`     | ADB resource pack current trade quantity OCR    |
| `MAX_QUANTITY_BOX_ADB` | ADB resource pack maximum sellable quantity OCR |

If the game UI adjusts the quantity positions, only these constants need to be changed, and regeneration will synchronize all outposts and 4 attempts.

## Maintenance Procedures

### Update zmdmap Data & Regenerate

```shell
pnpm generate:SellProduct
```

This command first executes logic equivalent to `pnpm fetch:zmdmap`, updating `tools/pipeline-generate/data/settlement_trade.json`, then runs the generation configs under the `SellProduct` directory sequentially.

### zmdmap Adds New Sellable Item

1. Run `pnpm generate:SellProduct`.
2. Check if the new item appears in the priority item options for the corresponding outpost in `assets/tasks/SellProduct.json`.
3. If the new item label did not generate as `$item.xxx`, add the corresponding `item.*` multilingual text in `assets/locales/interface/*.json`.
4. If OCR names have fixed misrecognitions, add evidence-backed candidates to `SETTLEMENT_OCR_ALIASES` in `model.mjs`.

Normal item additions usually do not require changes to the outpost Pipeline template.

### zmdmap Adds New Outpost

1. Run `pnpm fetch:zmdmap` to update the cache.
2. Verify the `LocationId` derived from the English name and the five-language `TextExpected` in `model.mjs`; add `SETTLEMENT_OCR_ALIASES` only for observed OCR exceptions.
3. If it's a new region, add `DOMAIN_REGION_PREFIX`.
4. Run `pnpm generate:SellProduct`.
5. Add the new outpost to the `next` list of the corresponding region in `assets/resource/pipeline/SellProduct/Sell.json`.
6. If it's a new region, add the region entry and automatic selection logic in `assets/resource/pipeline/SellProduct.json`.
7. Supplement the nodes required for SceneManager to enter the outpost management page of that region.
8. Add `task.SellProduct.{RegionPrefix}{LocationId}` and the new region text in `assets/locales/interface/*.json`.
9. Check both Win and ADB generation results.

The generator does not automatically determine how to enter a new outpost in the game UI, nor does it automatically add SceneManager jumps.

### Unstable Outpost OCR

Prioritize checking:

- `SellProductCheck{LocationId}TabText`
- `SellProductCheck{LocationId}Text`
- `SETTLEMENT_OCR_ALIASES[settlementId]`

If it's a fixed misrecognized text, directly add the candidate to `TextExpected`. If it's just an unsuitable ROI, modify the `roi` of the corresponding OCR node in `pipeline-template.jsonc` and `pipeline-adb-template.jsonc`, then regenerate.

### Priority Item Often Not Selected

Troubleshooting order:

1. Confirm the task option actually selected that priority item.
2. View the generated `SellProduct{LocationId}SelectItem{N}.custom_recognition_param.candidates`.
3. Check if zmdmap multilingual names contain the name actually displayed in the game UI.
4. View the `ocr_texts` and `candidates` of `SellProductNormalizedItemMatch` in the Go logs.
5. Prioritize adding candidates for fixed noise; only modify the Go matching logic when the algorithm truly cannot express it.

## Self-Check List

After modifying the generator or data, it is recommended to execute:

```shell
pnpm generate:SellProduct
pnpm prettier --write "docs/zh_cn/developers/tasks/sell-product-maintain.md" "docs/zh_cn/developers/README.md"
```

If the Go matching logic was modified:

```shell
cd agent/go-service
go test ./sellproduct
```

Before committing, at least check:

1. Whether `assets/tasks/SellProduct.json` conforms to interface V2.
2. Whether the generated outpost files have no residual old outposts.
3. Whether the region `next` in `SellProduct/Sell.json` includes the corresponding outposts.
4. Whether the global priorities, cache refresh, region, and outpost switch hierarchy is complete.
5. Whether both Win and ADB `Outposts/*.json` have been regenerated.
6. Whether JSON/Markdown conforms to `.prettierrc`.

## Common Pitfalls

- **Directly editing generated artifacts**: The next run of `pnpm generate:SellProduct` will overwrite the changes. Modify `model.mjs`, the relevant template data, templates, or handwritten linked files instead.
- **Generating only Win but not ADB**: `pipeline-adb-config.json` is responsible for ADB outpost nodes. When involving quantity regions, outpost OCR, or sell attempt templates, confirm the ADB artifacts as well.
- **New item has no translatable label**: `task-data.mjs` reverse-looks up `item.*` keys from `zh_cn.json`. If not found, options can still be generated, but the label falls back to the normal name; multilingual text needs to be added.
- **New region has task options but flow cannot enter**: Task option generation does not equal the entry pipeline being complete. `SellProduct.json`, `Sell.json`, and SceneManager jumps still need to be added.
- **Expanding priority item matching causes item mix-up**: Do not replace the current strict matching with loose similarity. Similar item names are common, and matching strategies must avoid substring false positives.

## Acknowledgments

The outpost and tradeable item data for SellProduct comes from `zmdmap`, downloaded to `tools/pipeline-generate/data/settlement_trade.json` via `pnpm fetch:zmdmap` for generation.
