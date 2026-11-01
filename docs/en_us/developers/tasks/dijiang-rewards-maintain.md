# Development Manual - Base Rewards Maintenance Documentation

This document explains the file distribution and execution flow of `DijiangRewards`.  
The core design is "Control Nexus dispatching + sub-stage jump-back": each sub-stage is independent, and options only modify stage entry points or branches without altering the main flow skeleton.  
This document was updated on October 29, 2026.

## File Paths

| Path | Function |
| -------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `assets/interface.json` | Task mounting (`dijiang_ship` group; also in the `DailyFull` / `QuickDaily` presets) |
| `assets/tasks/DijiangRewards.json` | Task entry, stage switches, Reception Room and Growth Chamber options |
| `assets/resource/pipeline/DijiangRewards/Entry.json` | Enter the Dijiang ship Control Nexus |
| `assets/resource/pipeline/DijiangRewards/MainFlow.json` | Control Nexus dispatches sub-stages in sequence |
| `assets/resource/pipeline/DijiangRewards/FastCollect.json` | Control Nexus one-click collect products / clues |
| `assets/resource/pipeline/DijiangRewards/RecoveryEmotion.json` | Friend assistance to recover mood |
| `assets/resource/pipeline/DijiangRewards/ReceptionRoom.json` | Reception Room clue collection, exchange, gifting |
| `assets/resource/pipeline/DijiangRewards/Manufacturing.json` | Manufacturing Cabin harvest, restock, assist |
| `assets/resource/pipeline/DijiangRewards/GrowthChamber.json` | Growth Chamber claim rewards, grow again, select material for growth |
| `assets/resource/pipeline/DijiangRewards/Entry.json` → `NeedCredit` | Credit store linkage entry for obtaining credit points; enters the Reception Room flow |
| `assets/resource/pipeline/DijiangRewards/Template/Location.json` | Cabin interface positioning |
| `assets/resource/pipeline/DijiangRewards/Template/TextTemplate.json` | Button and state OCR templates |
| `assets/resource/pipeline/DijiangRewards/Template/Status.json` | Auxiliary recognition for red dots, quantities, inventory, etc. |
| `agent/go-service/common/attachregex/action.go` | Dynamically generates OCR whitelists from node `attach` data |
| `assets/locales/interface/*.json` | Task, option, and focus copy |

## Execution Flow

1. Enter the Dijiang ship Control Nexus from the task entry (`Entry.json`).
2. At the Control Nexus, attempt each sub-stage in a fixed sequence (`MainFlow.json`); after completing one stage, return to the Control Nexus to continue to the next:
    - (Optional) [One-click collect](#one-click-collect) products and clues
    - [Recover mood](#recover-mood)
    - [Reception Room](#reception-room)
    - Manufacturing Cabin: claim output → restock → assist → exit
    - [Growth Chamber](#growth-chamber-options) (most option overrides)
3. End the task when no stages are triggered.

Each stage can be individually toggled via `StageTaskSetting`; by default, the recommended full process is followed.

## Sub-stage Descriptions

### One-click Collect

Implemented in `FastCollect.json`, directly clicks "products" and "clues" shortcuts at the Control Nexus for collection without entering the corresponding cabin.  
Controlled by the `StageTaskSetting` → `FastCollect` switch, disabled by default. When enabled, under `FastCollect` the user is able to choose to enable `FastCollectClue` and `FastCollectProduction`, by default enabled, When disabled skips the `FastCollect` task

### Recover Mood

`RecoveryEmotionCabins` defaults to all five cabins; deselecting all skips this stage. Following the game's ascending mood order, each page checks the fixed cabin labels of 12 operators from left to right, then top to bottom. Control Nexus, Reception Room, and Growth Chamber match by their color or full name. Manufacturing Cabins require yellow and the full cabin name containing I/II, checking II first. Unassigned cards are skipped while scanning continues.

The available assist points determine whether one or two operators are selected. After each click, a fixed offset from the current cabin label locates the top-right yellow selection marker. A match continues selection or proceeds to confirmation; otherwise, scanning continues with the next operator. The list scrolls only after the current page is exhausted. Each swipe holds the starting point, initiates dragging, moves past two rows, and holds the endpoint for 1 second. The right-side scrollbar's top position is compared before and after each scroll, treating a vertical shift of up to 3 px as unchanged. Three consecutive unchanged positions end the search; a larger shift resumes scanning on the new page and resets the count. Selected operators are confirmed before returning to the Control Nexus. If no first operator is found, the assist window is closed, the pipeline returns to the Control Nexus, and a not-found notice is shown.

### Reception Room

Upon entering the Reception Room, attempt in order: handle exchange completion popup → collect clues → receive clues → place/replace clues → (optional) start clue exchange → exit.

When clue inventory is full, follow the [clue gifting](#clue-gifting) branch, which is not an independent top-level stage.  
Whether to actively "start clue exchange" is controlled by the task-level `AutoStartExchange` option, enabled by default and independent of `ClueSetting`.

CustomizeClueSending, disabled by default, allows the user to choose which clues to send, the ones not choosen are kept. The gifting logic follows the old implementation of `ReceptionRoomSendCluesClickSend`

### Manufacturing Cabin

Upon entry: claim output → restock → use assist → exit. Maintenance focus is on button recognition stability, with fewer option overrides.

### Growth Chamber

Default behavior: claim mature rewards → normal material selection for growth → exit. "Grow Again" is disabled by default and must be explicitly enabled by an option.

Detail page loop: claim reward → (optional) grow again → (optional) enter material selection list → find target → confirm growth or extract seeds → return to detail page to continue.  
Material selection logic is almost entirely overridden by [Growth Chamber options](#growth-chamber-options), which is the maintenance focus.

## Special Handling

### Clue Gifting

Implemented in `ReceptionRoom.json`. When clues overflow, enter the gifting process: identify clue type and inventory quantity → select clues that meet the threshold → combine with friend's missing color or send button to complete gifting.

| Configuration | Behavior |
| -------------------------------------- | ------------------------------------------------------------------------------------- |
| `ClueSetting=No` (default) | Maximum of 3 gifts per session; send only if each clue inventory ≥ 3 (retain 2) |
| `ClueSetting=Yes` | Expand `SendCluesDirect`, `ClueSend`, `ClueStockLimit` for customizing entry / attempts / inventory thresholds |
| `QuickGiveDuplicateClues=No` (default) | Do not click in-game "Give Away All Duplicates"; send one by one by stock threshold |
| `QuickGiveDuplicateClues=Yes` | Prefer the one-click "Give Away All Duplicates" action after entering the Send Clues screen |

The attempt limit modifies the `max_hit` of the gifting loop; the inventory threshold modifies the quantity OCR regex.

Both entry points (`ClueSend.MaxClueSend` and `CreditShoppingClueSend.MaxClueSendWhenInsufficient`) write the same `ReceptionRoomSendCluesSendMain.max_hit`, and their accepted range is `0`–`20` (matching the per-clue stock limit of 20). `0` means never send.

Note that `max_hit` counts gifting-loop iterations, including rounds where none of the seven clue types meets the threshold, so it is not equal to the number of clues actually sent.

### Growth Chamber Options

Implemented in `GrowthChamber.json` + `pipeline_override` of `DijiangRewards.json`.

#### `SelectToGrow`: Overall Growth Direction

| Mode | Actual Behavior |
| ----------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `DoNothing` (Do Nothing) | Only claim mature rewards, do not enter material selection |
| `GrowAgain` (Grow Again) | Disable normal growth; after reward claiming closes, attempt "Grow Again" and confirm (since [#2003](https://github.com/MaaEnd/MaaEnd/pull/2003), triggered after reward claiming closes, not directly from detail page) |
| `Any` (displayed as "Specified Materials") | Expands material multi-select, seed extraction, and sorting sub-options, then finds an available target among the selected materials |

#### `SelectToGrowItems`: Select Growth Materials

`SelectToGrowItems` is a checkbox option that supports selecting any combination of 18 materials. All materials are selected by default.

Each material case writes its aliases in all five supported languages to `GrowthChamberSelectTarget.attach` through `pipeline_override`. After entering the material list, `GrowthChamberInitSelectTarget` calls `AttachToExpectedRegexAction` to merge aliases from the selected cases and generate an exact OCR whitelist in the form `^(alias1|alias2|...)$` for `GrowthChamberSelectTarget.expected` at runtime. If no material is selected, the whitelist is empty and the action generates the never-matching regex `a^`, preventing other materials from being selected accidentally.

#### `AutoExtractSeed`: What to Do When Seeds Are Missing

This is a separate switch under the "Specified Materials" mode and is independent of the material checkbox selection.

| Configuration | Actual Behavior |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Yes | Accept targets with "has seeds" or "has the material to extract seeds from"; follow extraction branch when seeds are missing |
| No | Filter tightened to must have seeds; if accidentally entering extraction entry, retreat to list to continue search (also serves as fallback for accidental return after continuous planting) |

#### `SortBy` / `SortOrder`

Only apply to the "Specified Materials" mode. They affect candidate list order but do not change the selected material set.

When maintaining Growth Chamber issues, first confirm the current `SelectToGrow` mode, the `SelectToGrowItems` selection, the runtime whitelist, sorting settings, and whether `AutoExtractSeed` has changed the acceptable target range.

### Option Hierarchy

```text
DijiangRewards
├── StageTaskSetting           # Expand stage sub-switches
│   ├── FastCollect            # One-click collect
│   │   └── FastCollectClue         # Sub-option that decides whether to collect clue
│   │   └── FastCollectProduction   # Sub-option that decides whether to collect production
│   ├── RecoveryEmotionStage
│   │   └── RecoveryEmotionCabins  # Cabins to include; all five by default
│   ├── ReceptionRoomStage
│   ├── ManufacturingStage
│   └── GrowthChamberStage
├── AutoStartExchange          # Whether the Reception Room actively starts clue exchange; on by default
│   └── WaitExchangeBeforeStart  # Wait when all clues are placed; Yes expands WaitExchangeBeforeStartThreshold
├── ClueSetting                # Expands the sending rules (direct send / send excess / stock limit / clue types)
│   ├── SendCluesDirect
│   ├── ClueSend        # Max clue send limit
│   ├── ClueStockLimit  # Max clue keep limit
│   ├── QuickGiveDuplicateClues # Give Away All Duplicates; off by default
│   ├── CustomSendClues
│   │   └── Checkbox
│   │       ├── ClueOne
│   │       ├── ClueTwo
│   │       ├── ClueThree
│   │       ├── ClueFour
│   │       ├── ClueFive
│   │       ├── ClueSix
│   │       └── ClueSeven
└── SelectToGrow               # Growth Chamber main mode
    ├── DoNothing
    ├── GrowAgain
    └── Any（界面显示「指定材料」）
        ├── SelectToGrowItems   # Multi-select 18 materials; all selected by default
        ├── AutoExtractSeed
        ├── SortBy
        └── SortOrder
```

## Paths to Modify When Adding New Cultivation Materials

1. `assets/tasks/DijiangRewards.json` — Add the material name to `SelectToGrowItems.default_case`, then add a checkbox case under `SelectToGrowItems.cases`; the case must write Simplified Chinese, English, Japanese, Traditional Chinese, and Korean aliases to `GrowthChamberSelectTarget.attach`
2. `assets/locales/interface/{zh_cn,en_us,ja_jp,zh_tw,ko_kr}.json` — Add the material's `$item.*` display text
3. If game button/cabin copy changes — synchronize `Template/TextTemplate.json`, `Template/Location.json`

There is no need to modify `GrowthChamber.json` or add per-material `ColorMatch` or row-recognition overrides for a new material; the shared OCR whitelist flow reads the aliases from `attach`.

## Maintenance Tips

| Symptom | Priority Check |
| ---------------------------------- | ----------------------------------------------------------------- |
| Cannot enter Control Nexus | `Entry.json`, SceneManager jump |
| A stage not executing | Corresponding stage switch under `StageTaskSetting` |
| Reception Room not gifting clues | Whether `ClueSetting=No` default override matches advanced items |
| No Grow Again after claiming reward | `SelectToGrow=GrowAgain`; next chain after reward claiming closes |
| Wrong material selected for growth | `SelectToGrowItems` selection, five-language `attach` values in each case, and the exact whitelist generated by `GrowthChamberInitSelectTarget` |
| No material is ever matched | Whether no material is selected and `a^` was generated; whether target aliases are complete; whether the initialization action succeeded |
| Material body exists but no seeds are extracted | `AutoExtractSeed` and disabling `GrowthChamberFindTargetByPlant` via task option |
| Unexpected sorting | `SortBy` / `SortOrder`; both apply only in "Specified Materials" mode |
| Other OCR recognition drift | Multilingual `expected` values in the three files under `Template/` |

Maintenance is divided into three layers: main flow layer (which cabin to go to) → stage business layer (what to do in the cabin) → interface configuration layer (which branches options modify).
