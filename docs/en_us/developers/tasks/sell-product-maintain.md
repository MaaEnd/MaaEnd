# Developer Manual - SellProduct Maintenance Documentation

This document explains the generation chain, Pipeline organization, task options, priority item matching, reserve quantity, and maintenance procedures for adding new outposts/items for the `SellProduct` (Sell Product) task.

The core feature of `SellProduct` is **zmdmap data-driven + Pipeline template generation**: outposts, sellable items, task options, and duplicate nodes for each outpost are not hand-written individually but are batch-rendered by `tools/pipeline-generate/SellProduct/` after reading `tools/pipeline-generate/data/settlement_trade.json`. The `settlement_trade.json` is downloaded and cached from the zmdmap API by `pnpm fetch:zmdmap`.

> [!IMPORTANT]
>
> `assets/tasks/SellProduct.json`, `assets/resource/pipeline/SellProduct/Outposts/*.json`, and `assets/resource_adb/pipeline/SellProduct/Outposts/*.json` are all **generated artifacts**. Do not directly hand-edit these files; to modify outposts, product lists, priority item candidates, sell attempt templates, or Win/ADB coordinates, you should modify the data assembly or templates under `tools/pipeline-generate/SellProduct/` and then regenerate.

## Overview

The core maintenance points for SellProduct are as follows:

| Module                           | Path                                                              | Function                                                                                                                  |
| -------------------------------- | ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| zmdmap cache data                | `tools/pipeline-generate/data/settlement_trade.json`              | Raw data for outposts, prosperity, tradeable items, multilingual names, rarity, unit price, etc.                          |
| Data assembly                    | `tools/pipeline-generate/SellProduct/data.mjs`                    | Converts zmdmap data into `settlementFlatRows` consumable by templates                                                    |
| Outpost Pipeline template        | `tools/pipeline-generate/SellProduct/pipeline-template.jsonc`     | Generates each outpost sell node for the Win resource pack                                                                |
| ADB outpost template             | `tools/pipeline-generate/SellProduct/pipeline-adb-template.jsonc` | Generates each outpost sell node for the ADB resource pack, mainly differing in the quantity OCR region                   |
| Task options template            | `tools/pipeline-generate/SellProduct/task-template.jsonc`         | Generates region, outpost, sell attempts, priority items, and reserve quantity options in `assets/tasks/SellProduct.json` |
| Win outpost generation config    | `tools/pipeline-generate/SellProduct/pipeline-config.json`        | Output to `assets/resource/pipeline/SellProduct/Outposts/${LocationId}.json`                                              |
| ADB outpost generation config    | `tools/pipeline-generate/SellProduct/pipeline-adb-config.json`    | Output to `assets/resource_adb/pipeline/SellProduct/Outposts/${LocationId}.json`                                          |
| Task options generation config   | `tools/pipeline-generate/SellProduct/task-config.json`            | Output to `assets/tasks/SellProduct.json`                                                                                 |
| Task entry point                 | `assets/resource/pipeline/SellProduct.json`                       | `ScheduleAction`, main loop, region entry; manually maintained                                                            |
| Region sell entry point          | `assets/resource/pipeline/SellProduct/Sell.json`                  | `next` list from region to outposts; manually maintained                                                                  |
| Common sell core                 | `assets/resource/pipeline/SellProduct/SellCore.json`              | Sell loop, handling for out-of-stock/insufficient dispatch coupons/exceeded redemption limits, final transaction process  |
| Common exchange process          | `assets/resource/pipeline/SellProduct/ChangeGoods.json`           | Enter item selection interface, select priority item or default item                                                      |
| Common outpost recognition       | `assets/resource/pipeline/SellProduct/EnterOutpost.json`          | Outpost interface, region outpost page, and outpost management text recognition                                           |
| ADB common sell core             | `assets/resource_adb/pipeline/SellProduct/SellCore.json`          | Common sell core under ADB resource pack                                                                                  |
| Priority item custom recognition | `agent/go-service/sellproduct/normalized_match.go`                | `SellProductNormalizedItemMatch`, anti-noise exact matching for OCR results and candidate names                           |
| Multilingual text                | `assets/locales/interface/*.json`                                 | `SellProduct` task text, outpost names, item labels                                                                       |
| Generation entry point           | `package.json`'s `generate:SellProduct` / `fetch:zmdmap`          | Updates zmdmap cache and re-renders generated artifacts                                                                   |

## Generated Artifacts vs. Hand-Maintained Files

### Generated Artifacts

The following files are rendered by `@joebao/maa-pipeline-generate` and will be overwritten upon regeneration:

- `assets/tasks/SellProduct.json`
- `assets/resource/pipeline/SellProduct/Outposts/*.json`
- `assets/resource_adb/pipeline/SellProduct/Outposts/*.json`

The sources for these files are:

| Artifact                        | Template                      | Data Source               |
| ------------------------------- | ----------------------------- | ------------------------- |
| `assets/tasks/SellProduct.json` | `task-template.jsonc`         | `data.mjs` + zmdmap cache |
| Win outpost Pipeline            | `pipeline-template.jsonc`     | `data.mjs` + zmdmap cache |
| ADB outpost Pipeline            | `pipeline-adb-template.jsonc` | `data.mjs` + zmdmap cache |

### Hand-Maintained Files

The following files are not overwritten by the SellProduct generator and must be manually updated by maintainers as business changes occur:

- `assets/resource/pipeline/SellProduct.json`
- `assets/resource/pipeline/SellProduct/Sell.json`
- `assets/resource/pipeline/SellProduct/SellCore.json`
- `assets/resource/pipeline/SellProduct/ChangeGoods.json`
- `assets/resource/pipeline/SellProduct/EnterOutpost.json`
- `assets/resource_adb/pipeline/SellProduct/SellCore.json`
- `agent/go-service/sellproduct/*.go`
- `assets/locales/interface/*.json`

When adding new regions or outposts, the generator can create task options and outpost nodes, but the region entry point, the `next` list from region to outposts, SceneManager jump nodes, and outpost management page entry recognition may still need to be hand-completed.

## Naming Rules and Data Model

### Outpost Node ID (`LocationId`)

`LocationId` is the prefix and filename for generated outpost nodes:

```text
assets/resource/pipeline/SellProduct/Outposts/${LocationId}.json
assets/resource_adb/pipeline/SellProduct/Outposts/${LocationId}.json
```

By default, `LocationId` is derived by converting the zmdmap English outpost name to PascalCase. In actual maintenance, first check `data.mjs`'s `SETTLEMENT_OVERRIDE`: if a `LocationId` is configured for an outpost there, the generator will use the override value.

`LocationId` is only used for node names and filenames, not display text. The outpost name shown in the user interface is provided by `task.SellProduct.{RegionPrefix}{LocationId}` in `assets/locales/interface/*.json`.

### Region Prefix (`RegionPrefix`)

`RegionPrefix` is the region ID used for task options and region entry points, for example, `ValleyIV`, `Wuling`. It is mapped from the zmdmap `domainId` by `DOMAIN_REGION_PREFIX`.

When adding a new region, do not directly rely on default fallback names like `domain_3`; instead, first configure a stable project region ID in `DOMAIN_REGION_PREFIX`.

### zmdmap Data Fields

`settlement_trade.json` currently mainly provides:

- `settlements`: List of outposts, with keys like `stm_tundra_1`.
- `settlement.domainId`: The region the outpost belongs to, for example, `domain_1`, `domain_2`.
- `settlement.settlementName`: Multilingual outpost name.
- `settlement.byProsperityLevel[*].tradeItems`: List of tradeable items under different prosperity levels.
- `tradeItems[*].itemId`: Item ID.
- `tradeItems[*].name`: Multilingual item name.
- `tradeItems[*].rarity` / `unitPrice`: Used for sorting priority item options.

`data.mjs` assembles this raw data into `settlementFlatRows` (one row per outpost), which is then consumed by the three generation configs.

Currently generated outposts are:

| zmdmap settlementId | Region   | LocationId                  | Outpost Name                |
| ------------------- | -------- | --------------------------- | --------------------------- |
| `stm_tundra_1`      | ValleyIV | `RefugeeCamp`               | Refugee Temporary Camp      |
| `stm_tundra_2`      | ValleyIV | `InfrastructureOutpost`     | Infrastructure Outpost      |
| `stm_tundra_3`      | ValleyIV | `ReconstructionCommand`     | Reconstruction Command      |
| `stm_hongs_1`       | Wuling   | `SkyKingFlats`              | Sky King Flats Aid Station  |
| `stm_hongs_2`       | Wuling   | `CardiacRemediationStation` | Cardiac Remediation Station |

## Automatic Generation Mechanism

### Run Command

```shell
# Recommended: Run from repository root, automatically updates zmdmap cache and regenerates
pnpm generate:SellProduct

# Only update zmdmap cache
pnpm fetch:zmdmap

# When cache is already updated, can also render individually in the generator directory
cd tools/pipeline-generate/SellProduct
npx @joebao/maa-pipeline-generate --config pipeline-config.json
npx @joebao/maa-pipeline-generate --config task-config.json
npx @joebao/maa-pipeline-generate --config pipeline-adb-config.json
```

### Win Outpost Pipeline: `pipeline-config.json`

```json
{
    "template": "pipeline-template.jsonc",
    "data": "data.mjs",
    "outputDir": "../../../assets/resource/pipeline/SellProduct/Outposts",
    "outputPattern": "${LocationId}.json",
    "format": true,
    "merged": false
}
```

Each data row generates one Win resource pack outpost file.

### ADB Outpost Pipeline: `pipeline-adb-config.json`

```json
{
    "template": "pipeline-adb-template.jsonc",
    "data": "data.mjs",
    "outputDir": "../../../assets/resource_adb/pipeline/SellProduct/Outposts",
    "outputPattern": "${LocationId}.json",
    "format": true,
    "merged": false
}
```

The ADB outpost template is structurally similar to the Win template, with the main difference being the quantity OCR region using `QuantityBoxAdb` and `MaxTargetBoxAdb`.

### Task Options: `task-config.json`

```json
{
    "task": true,
    "template": "task-template.jsonc",
    "data": "data.mjs",
    "outputDir": "../../../assets/tasks/",
    "outputFile": "SellProduct.json",
    "format": true
}
```

This configuration generates region switches, outpost switches, 4 sell attempts, priority item, and reserve quantity configuration in the user interface.

### Data Assembly: `data.mjs`

`tools/pipeline-generate/SellProduct/data.mjs` is the main maintenance entry point for the SellProduct generator.

It currently handles:

1. Reading `tools/pipeline-generate/data/settlement_trade.json`.
2. Looking up `item.*` keys from `assets/locales/interface/zh_cn.json` to generate task option labels as `$item.xxx` where possible.
3. Building a global item dictionary from zmdmap's `tradeItems`.
4. Counting sellable items per outpost and sorting them by `rarity`, `unitPrice` in descending order.
5. Mapping `domainId` to the `RegionPrefix` used in tasks.
6. Generating `LocationId`, outpost OCR `TextExpected`, task options, priority item candidate names for outposts.
7. Injecting Win/ADB BetterSliding quantity OCR regions.

### Outpost Naming Override

`SETTLEMENT_OVERRIDE` is used when the zmdmap original name is unsuitable for direct node ID generation or when OCR requires special candidate text.

Current overrides include:

- `LocationId`: Overrides the default `toPascalCase(EN)`, determining the generated node prefix and filename.
- `TextExpected`: Overrides the outpost OCR candidate. Once filled, it completely replaces the default CN/TC/JP/EN candidates; you need to manually override necessary languages and common OCR noise.

Typical scenarios:

- English name is too long or doesn't fit project naming conventions.
- Actual display in game UI differs from zmdmap name.
- OCR frequently misrecognizes a certain outpost as a fixed erroneous text, for example, reading `HQ` incorrectly.

### Region Mapping Override

`DOMAIN_REGION_PREFIX` is responsible for mapping zmdmap's `domainId` to region IDs in the project:

```js
const DOMAIN_REGION_PREFIX = {
    domain_1: "ValleyIV",
    domain_2: "Wuling",
};
```

When onboarding a new region, if zmdmap adds `domain_3`, you typically need to first add a stable `RegionPrefix` here. Unconfigured domains will fall back to `toPascalCase(domainId)`, which is usually unsuitable as user-visible configuration and Pipeline prefixes.

### Temporarily Excluding Event Items

`TEMP_EXCLUDED_ITEM_CN_NAMES` is used to temporarily exclude event items that still appear in zmdmap data but should no longer appear in the sell configuration.

Maintenance rules:

- Only for short-term compatibility with event data.
- The comment should clearly state the deletion condition.
- When zmdmap data is updated and the event item is confirmed removed, the corresponding exclusion entry should be deleted.

### Priority Item Candidate Names

Each generated priority item option overrides the corresponding node:

```text
SellProduct{LocationId}SelectItem{N}
```

Override content includes:

- `enabled: true`
- `custom_recognition_param.candidates`
- Miss handling anchor

`candidates` comes from zmdmap's CN/TC/JP/EN names. The English name has certain easily interfering symbols removed before entering candidates.

## Main Flow

The overall flow can be understood by the following chain:

```text
SellProductSchedule
-> SellProductMain
-> SellProductLoop
-> SellProductAuto / SellProductValleyIV / SellProductWuling
-> SellProduct{Region}Sell
-> SellProduct{LocationId}
-> SellProduct{LocationId}Sell
-> SellProductSellLoop
-> SellProduct{LocationId}SellAttempt{1..4}
-> SellProductChangeGoods
-> SellProduct{LocationId}SelectItem{1..4} / SellProductSelectFirstGood / SellProductSelectNextGood
-> SellProduct{LocationId}BetterSliding{1..4}
-> SellProductSell
```

Key points:

- `SellProductSchedule` determines the weekday selected by the user via `ScheduleAction` and actually executes `SellProductMain`.
- `SellProductLoop` only continues execution in the region construction interface; when not on the target interface, it hands over to `SceneEnterMenuRegionalDevelopment`.
- `SellProductAuto` automatically selects Valley IV or Wuling based on the current region construction page.
- `SellProduct{Region}Sell` enters the outpost management page of the corresponding region, then iterates through all outposts in that region via `next`.
- Each outpost node is generated by a template, responsible for recognizing the current outpost, clicking the outpost tag, and setting the sell anchor.
- `SellProductSellLoop` chains up to 4 sell attempts via anchor.
- Each attempt first exchanges goods, then adjusts the quantity to the target value using BetterSliding, and finally clicks to trade.

## How Task Options Modify Pipeline

`assets/tasks/SellProduct.json` is generated by `task-template.jsonc`. Configurations selected by the user in the interface modify the Pipeline via `pipeline_override`.

### Top-Level Options

| Option                | Behavior                                                                                                                                     |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `SellProductSchedule` | Writes weekday booleans to `SellProductSchedule.attach`                                                                                      |
| `SellBeyondAidQuota`  | Controls whether to stop the task or automatically confirm to continue trading when exceeding the outpost's redeemable dispatch coupon limit |
| `{RegionPrefix}Sell`  | Controls whether the region entry node `SellProduct{RegionPrefix}` is enabled                                                                |

### Outposts and Sell Attempts

Each outpost generates a set of switches:

```text
{RegionPrefix}{LocationId}
{RegionPrefix}{LocationId}Attempt1
{RegionPrefix}{LocationId}Attempt2
{RegionPrefix}{LocationId}Attempt3
{RegionPrefix}{LocationId}Attempt4
```

Default behavior:

- Outpost switches are enabled by default.
- The 1st and 2nd sell attempts are enabled by default.
- The 3rd and 4th sell attempts are disabled by default.

### Priority Items

Each sell attempt has a priority item selection:

```text
{RegionPrefix}{LocationId}Item{1..4}
```

The default is `None`. When a specific item is selected, the task option will:

- Enable `SellProduct{LocationId}SelectItem{N}`.
- Write the multilingual candidate names for that item.
- Set the miss handling to `SellProductPriorityGoodMissWarning`.

If the priority item is missed, the flow will prompt "Priority item configured but no matching item currently recognized," then select the default item to continue selling, preventing the entire task from stopping at the item selection interface.

### Reserve Quantity

Each sell attempt has a reserve quantity configuration:

```text
{RegionPrefix}{LocationId}Reserve{1..4}
{RegionPrefix}{LocationId}ReserveValue{1..4}
```

The default is `Sell All`. When `Reserve Specific Quantity` is selected, it overrides the corresponding BetterSliding node:

- `next` is changed to first attempt `SellProductSkipToNextSellLoop`, then attempt `SellProductSellThenLoop`.
- `attach.Target` is written with the user-input reserve quantity.
- `attach.TargetReverse` is set to `true`.

This means BetterSliding will calculate the target as "Current maximum sellable quantity - Reserve quantity". If the reserve quantity is greater than or equal to current inventory, it goes to `SellProductSkipToNextSellLoop`, skipping this sell attempt and proceeding to the next.

## Priority Item Recognition

Priority item nodes use Go custom recognition:

```text
SellProductNormalizedItemMatch
```

Implementation file:

```text
agent/go-service/sellproduct/normalized_match.go
```

This recognizer runs OCR within the ROI of the item selection interface, then performs two layers of strict matching between OCR text and `candidates`:

1. Tier A: Strips whitespace, brackets, vertical bars, hyphens, dots, commas, and other common separators, unifies ASCII case, then checks for strict equality.
2. Tier B: Based on Tier A, additionally strips ASCII letters and numbers, used for handling CJK names mixed with English noise.

Notes during maintenance:

- Do not change it to loose edit distance matching, otherwise it's easy to mistakenly match "Citrus Preserves" to "Premium Citrus Preserves" or "Select Citrus Preserves."
- When adding candidate names, prioritize generating them from zmdmap multilingual names.
- If OCR has fixed noise, prioritize adding accurate candidates to `data.mjs`'s data assembly logic rather than expanding the matching algorithm.
- After modifying the matching algorithm, run the regression tests covered by `agent/go-service/sellproduct/normalized_match_test.go`.

## BetterSliding and Quantity Region

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
- `MaxTarget.Box`: Reads maximum sellable quantity.
- `Quantity.Box`: Reads current transaction quantity.
- `ExceedingOverrideEnable: "SellProductSkipToNextSellLoop"`

Quantity regions are uniformly maintained in `data.mjs`:

| Constant               | Purpose                                            |
| ---------------------- | -------------------------------------------------- |
| `QUANTITY_BOX`         | Win resource pack current transaction quantity OCR |
| `MAX_QUANTITY_BOX`     | Win resource pack maximum sellable quantity OCR    |
| `QUANTITY_BOX_ADB`     | ADB resource pack current transaction quantity OCR |
| `MAX_QUANTITY_BOX_ADB` | ADB resource pack maximum sellable quantity OCR    |

If the game UI adjusts quantity positions, only these constants need to be changed, then regenerated to synchronize all outposts and 4 attempts.

## Maintenance Process

### Update zmdmap Data and Regenerate

```shell
pnpm generate:SellProduct
```

This command first executes the equivalent logic of `pnpm fetch:zmdmap`, updating `tools/pipeline-generate/data/settlement_trade.json`, then runs the generation configs under the `SellProduct` directory sequentially.

### zmdmap Adds New Sellable Item

1. Run `pnpm generate:SellProduct`.
2. Check if the new item appears in the priority item options for the corresponding outpost in `assets/tasks/SellProduct.json`.
3. If the new item label didn't generate `$item.xxx`, add the corresponding `item.*` multilingual text in `assets/locales/interface/*.json`.
4. If OCR name has fixed misrecognition, then evaluate whether you need to adjust `data.mjs` candidate name assembly logic.

Ordinary new items typically don't require modifying outpost Pipeline templates.

### zmdmap Adds New Outpost

1. Run `pnpm fetch:zmdmap` to update cache.
2. Check in `data.mjs` if you need to add `SETTLEMENT_OVERRIDE`, ensuring `LocationId`, `TextExpected` are stable.
3. If it's a new region, add `DOMAIN_REGION_PREFIX`.
4. Run `pnpm generate:SellProduct`.
5. Add the new outpost to the corresponding region's `next` list in `assets/resource/pipeline/SellProduct/Sell.json`.
6. If there's a new region, add region entry point and auto-selection logic in `assets/resource/pipeline/SellProduct.json`.
7. Add nodes required for SceneManager to enter the outpost management page of that region.
8. Add `task.SellProduct.{RegionPrefix}{LocationId}` and new region text in `assets/locales/interface/*.json`.
9. Check both Win and ADB generation results.

The generator doesn't automatically determine how a new outpost is accessed in the game UI, nor does it automatically add SceneManager jumps.

### Outpost OCR Unstable

First check:

- `SellProductCheck{LocationId}TabText`
- `SellProductCheck{LocationId}Text`
- `SETTLEMENT_OVERRIDE[settlementId].TextExpected`

If it's fixed misrecognized text, directly add candidates to `TextExpected`. If it's just an unsuitable ROI, you need to modify the `roi` of the corresponding OCR node in `pipeline-template.jsonc` and `pipeline-adb-template.jsonc`, then regenerate.

### Priority Item Often Not Selected

Troubleshooting order:

1. Confirm if the task option really selected that priority item.
2. Check the generated `SellProduct{LocationId}SelectItem{N}.custom_recognition_param.candidates`.
3. Check if zmdmap multilingual names include the actual display name in game UI.
4. Check `SellProductNormalizedItemMatch`'s `ocr_texts` and `candidates` in Go logs.
5. Fixed noise should be supplemented with candidates first; only modify Go matching logic when the algorithm really can't express it.

### Reserve Quantity Doesn't Meet Expectations

First check:

- Whether the corresponding `ReserveValue{N}` overrode the correct `SellProduct{LocationId}BetterSliding{N}`.
- Whether `attach.Target` is the user-input value.
- Whether `attach.TargetReverse` is `true`.
- Whether `MaxTarget.Box` can read the maximum sellable quantity.
- Whether `Quantity.Box` can read the current transaction quantity.
- Whether Win and ADB resource packs used their respective correct OCR regions.

## Self-Check List

After modifying the generator or data, it's recommended to execute:

```shell
pnpm generate:SellProduct
pnpm prettier --write "docs/zh_cn/developers/tasks/sell-product-maintain.md" "docs/zh_cn/developers/README.md"
```

If Go matching logic was changed:

```shell
cd agent/go-service
go test ./sellproduct
```

Before submission, at least check:

1. Whether `assets/tasks/SellProduct.json` conforms to interface V2.
2. Whether generated outpost files have no residual old outposts.
3. Whether region `next` in `SellProduct/Sell.json` contains corresponding outposts.
4. Whether the hierarchy of regions, outposts, attempts, priority items, and reserve quantities in task options is complete.
5. Whether both Win and ADB `Outposts/*.json` have been regenerated.
6. Whether JSON/Markdown conforms to `.prettierrc`.

## Common Pitfalls

- **Directly hand-editing generated artifacts**: Next time `pnpm generate:SellProduct` is run, changes will be overwritten. Should modify `data.mjs`, templates, or hand-written linked files.
- **Only generating Win, not ADB**: `pipeline-adb-config.json` is responsible for ADB outpost nodes. When involving quantity regions, outpost OCR, or sell attempt templates, you must also confirm ADB artifacts.
- **New item without translatable label**: `data.mjs` looks up `item.*` keys from `zh_cn.json`. If not found, it can still generate options, but the label will fall back to the plain name; multilingual text needs to be added.
- **New region has task options but flow can't enter**: Task option generation doesn't mean the entry chain is complete. You also need to add `SellProduct.json`, `Sell.json`, and SceneManager jumps.
- **Expanding priority item matching causes item mix-up**: Don't use loose similarity to replace the current strict matching. There are many similar product names; the matching strategy must avoid substring false positives.

## Acknowledgements

The outpost and tradeable item data for SellProduct comes from `zmdmap`, downloaded to `tools/pipeline-generate/data/settlement_trade.json` by `pnpm fetch:zmdmap` before participating in generation.
