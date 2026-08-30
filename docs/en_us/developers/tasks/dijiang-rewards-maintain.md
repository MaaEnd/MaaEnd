# Development Manual - Infrastructure Task Maintenance Documentation

This document explains the file distribution and execution flow of `DijiangRewards`.
The core design is "central hub dispatching + sub-stage callback": each sub-stage is independent, and options only modify stage entry points or branches without altering the main flow skeleton.
This document was updated on August 29, 2026 (synchronized with the multi-select growth material and dynamic OCR whitelist design).

## File Paths

| Path | Function |
| -------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `assets/interface.json` | Task mounting (`dijiang_ship` / `daily` group) |
| `assets/tasks/DijiangRewards.json` | Task entry, stage switches, reception room and cultivation chamber options |
| `assets/resource/pipeline/DijiangRewards/Entry.json` | Enter Dijiang ship central hub |
| `assets/resource/pipeline/DijiangRewards/MainFlow.json` | Central hub dispatches sub-stages in sequence |
| `assets/resource/pipeline/DijiangRewards/FastCollect.json` | Central hub one-click collect products / clues |
| `assets/resource/pipeline/DijiangRewards/RecoveryEmotion.json` | Friend assistance to recover mood |
| `assets/resource/pipeline/DijiangRewards/ReceptionRoom.json` | Reception room clue collection, exchange, gifting |
| `assets/resource/pipeline/DijiangRewards/Manufacturing.json` | Manufacturing chamber harvest, restock, assist |
| `assets/resource/pipeline/DijiangRewards/GrowthChamber.json` | Cultivation chamber claim rewards, replant, select material for growth |
| `assets/resource/pipeline/DijiangRewards/NeedCredit.json` | Credit store linkage sub-process for obtaining credit points |
| `assets/resource/pipeline/DijiangRewards/Template/Location.json` | Chamber interface positioning |
| `assets/resource/pipeline/DijiangRewards/Template/TextTemplate.json` | Button and state OCR templates |
| `assets/resource/pipeline/DijiangRewards/Template/Status.json` | Auxiliary recognition for red dots, quantities, inventory, etc. |
| `agent/go-service/common/attachregex/action.go` | Dynamically generates OCR whitelists from node `attach` data |
| `assets/locales/interface/*.json` | Task, option, and focus copy |

## Execution Flow

1. Enter the Dijiang ship central hub from the task entry (`Entry.json`).
2. At the central hub, attempt each sub-stage in a fixed sequence (`MainFlow.json`); after completing one stage, return to the hub to continue to the next:
    - (Optional) [One-click collect](#one-click-collect) products and clues
    - [Recover mood](#recover-mood)
    - [Reception room](#reception-room)
    - Manufacturing chamber: claim output → restock → assist → exit
    - [Cultivation chamber](#cultivation-chamber-options) (most option overrides)
3. End the task when no stages are triggered.

Each stage can be individually toggled via `StageTaskSetting`; by default, the recommended full process is followed.

## Sub-stage Descriptions

### One-click Collect

Implemented in `FastCollect.json`, directly clicks "products" and "clues" shortcuts at the central hub for collection without entering the corresponding chamber.
Controlled by the `StageTaskSetting` → `FastCollect` switch, disabled by default.

### Recover Mood

`RecoveryEmotionMain` triggers only once per central hub scan (`max_hit: 1`).

Operator selection logic: Click the first operator on the left → check if mood is full or remaining attempts are 0 → if both are false, click the second operator on the left → finish and return to the central hub.

### Reception Room

Upon entering the reception room, attempt in order: handle exchange completion popup → collect clues → receive clues → place/replace clues → (optional) start clue exchange → exit.

When clue inventory is full, follow the [clue gifting](#clue-gifting) branch, which is not an independent top-level stage.
Whether to actively "start clue exchange" is controlled by `AutoStartExchange`, disabled by default, reserved for credit store linkage.

### Manufacturing Chamber

Upon entry: claim output → restock → use assist → exit. Maintenance focus is on button recognition stability, with fewer option overrides.

### Cultivation Chamber

Default behavior: claim mature rewards → normal material selection for growth → exit. "Replant" is disabled by default and must be explicitly enabled by an option.

Detail page loop: claim reward → (optional) replant → (optional) enter material selection list → find target → confirm growth or extract base core → return to detail page to continue.
Material selection logic is almost entirely overridden by [cultivation chamber options](#cultivation-chamber-options), which is the maintenance focus.

## Special Handling

### Clue Gifting

Implemented in `ReceptionRoom.json`. When clues overflow, enter the gifting process: identify clue type and inventory quantity → select clues that meet the threshold → combine with friend's missing color or send button to complete gifting.

| Configuration | Behavior |
| -------------------------- | ------------------------------------------------------------------------------------- |
| `ClueSetting=No` (default) | Maximum of 3 gifts per session; send only if each clue inventory ≥ 3 (retain 2) |
| `ClueSetting=Yes` | Expand `ClueSend`, `ClueStockLimit` for customizing attempts and inventory thresholds |

The attempt limit modifies the `max_hit` of the gifting loop; the inventory threshold modifies the quantity OCR regex.

### Cultivation Chamber Options

Implemented in `GrowthChamber.json` + `pipeline_override` of `DijiangRewards.json`.

#### `SelectToGrow`: Overall Growth Direction

| Mode | Actual Behavior |
| ----------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `DoNothing` (No Growth) | Only claim mature rewards, do not enter material selection |
| `GrowAgain` (Replant) | Disable normal growth; after reward claiming closes, attempt "replant" and confirm ([#2003](https://github.com/MaaEnd/MaaEnd/pull/2003) triggered after reward claiming closes, not directly from detail page) |
| `Any` (displayed as "Specific Materials") | Expands material multi-select, base core extraction, and sorting sub-options, then finds an available target among the selected materials |

#### `SelectToGrowItems`: Select Growth Materials

`SelectToGrowItems` is a checkbox option that supports selecting any combination of 18 materials. All materials are selected by default.

Each material case writes its aliases in all five supported languages to `GrowthChamberSelectTarget.attach` through `pipeline_override`. After entering the material list, `GrowthChamberInitSelectTarget` calls `AttachToExpectedRegexAction` to merge aliases from the selected cases and generate an exact OCR whitelist in the form `^(alias1|alias2|...)$` for `GrowthChamberSelectTarget.expected` at runtime. If no material is selected, the whitelist is empty and the action generates the never-matching regex `a^`, preventing other materials from being selected accidentally.

#### `AutoExtractSeed`: What to Do When Base Core is Missing

This is a separate switch under the "Specific Materials" mode and is independent of the material checkbox selection.

| Configuration | Actual Behavior |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Yes | Accept targets with "base core" or "can extract base core"; follow extraction branch when base core is missing |
| No | Filter tightened to must have base core; if accidentally entering extraction entry, retreat to list to continue search (also serves as fallback for accidental return after continuous planting) |

#### `SortBy` / `SortOrder`

Only apply to the "Specific Materials" mode. They affect candidate list order but do not change the selected material set.

When maintaining cultivation chamber issues, first confirm the current `SelectToGrow` mode, the `SelectToGrowItems` selection, the runtime whitelist, sorting settings, and whether `AutoExtractSeed` has changed the acceptable target range.

### Option Hierarchy

```text
DijiangRewards
├── AutoStartExchange          # Whether reception room actively starts clue exchange
├── StageTaskSetting           # Expand stage sub-switches
│   ├── FastCollect            # One-click collect
│   ├── RecoveryEmotionStage
│   ├── ReceptionRoomStage
│   ├── ManufacturingStage
│   └── GrowthChamberStage
├── ClueSetting                # Expand clue gifting attempts / inventory thresholds
└── SelectToGrow               # Cultivation chamber main mode
    ├── DoNothing
    ├── GrowAgain
    └── Any (displayed as "Specific Materials")
        ├── SelectToGrowItems   # Multi-select 18 materials; all selected by default
        ├── AutoExtractSeed
        ├── SortBy
        └── SortOrder
```

## Paths to Modify When Adding New Cultivation Materials

1. `assets/tasks/DijiangRewards.json` — Add the material name to `SelectToGrowItems.default_case`, then add a checkbox case under `SelectToGrowItems.cases`; the case must write Simplified Chinese, English, Japanese, Traditional Chinese, and Korean aliases to `GrowthChamberSelectTarget.attach`
2. `assets/locales/interface/{zh_cn,en_us,ja_jp,zh_tw,ko_kr}.json` — Add the material's `$item.*` display text
3. If game button/chamber copy changes — synchronize `Template/TextTemplate.json`, `Template/Location.json`

Do not modify `GrowthChamber.json` or add per-material `ColorMatch` or row-recognition overrides for a new material; the shared OCR whitelist flow reads the aliases from `attach`.

## Maintenance Tips

| Symptom | Priority Check |
| ---------------------------------- | ----------------------------------------------------------------- |
| Cannot enter central hub | `Entry.json`, SceneManager jump |
| A stage not executing | Corresponding stage switch under `StageTaskSetting` |
| Reception room not gifting clues | Whether `ClueSetting=No` default override matches advanced items |
| No replant after claiming reward | `SelectToGrow=GrowAgain`; next chain after reward claiming closes |
| Wrong material selected for growth | `SelectToGrowItems` selection, five-language `attach` values in each case, and the exact whitelist generated by `GrowthChamberInitSelectTarget` |
| No material is ever matched | Whether no material is selected and `a^` was generated; whether target aliases are complete; whether the initialization action succeeded |
| Material body exists but no base core is extracted | `AutoExtractSeed` and `GrowthChamberCheckTargetNotEmpty` linkage override |
| Unexpected sorting | `SortBy` / `SortOrder`; both apply only in "Specific Materials" mode |
| Other OCR recognition drift | Multilingual `expected` values in the three files under `Template/` |

Maintenance is divided into three layers: main flow layer (which chamber to go to) → stage business layer (what to do in the chamber) → interface configuration layer (which branches options modify).
