# Development Manual - Infrastructure Task Maintenance Documentation

This document explains the file distribution and execution flow of `DijiangRewards`.
The core design is "central hub dispatching + sub-stage callback": each sub-stage is independent, and options only modify stage entry points or branches without altering the main flow skeleton.
This document was updated on June 9, 2026 (synchronized with the recovery of the mood-based operator selection logic adjustment).

## File Paths

| Path                                                                 | Function                                                                   |
| -------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `assets/interface.json`                                              | Task mounting (`dijiang_ship` / `daily` group)                             |
| `assets/tasks/DijiangRewards.json`                                   | Task entry, stage switches, reception room and cultivation chamber options |
| `assets/resource/pipeline/DijiangRewards/Entry.json`                 | Enter Dijiang ship central hub                                             |
| `assets/resource/pipeline/DijiangRewards/MainFlow.json`              | Central hub dispatches sub-stages in sequence                              |
| `assets/resource/pipeline/DijiangRewards/FastCollect.json`           | Central hub one-click collect products / clues                             |
| `assets/resource/pipeline/DijiangRewards/RecoveryEmotion.json`       | Friend assistance to recover mood                                          |
| `assets/resource/pipeline/DijiangRewards/ReceptionRoom.json`         | Reception room clue collection, exchange, gifting                          |
| `assets/resource/pipeline/DijiangRewards/Manufacturing.json`         | Manufacturing chamber harvest, restock, assist                             |
| `assets/resource/pipeline/DijiangRewards/GrowthChamber.json`         | Cultivation chamber claim rewards, replant, select material for growth     |
| `assets/resource/pipeline/DijiangRewards/NeedCredit.json`            | Credit store linkage sub-process for obtaining credit points               |
| `assets/resource/pipeline/DijiangRewards/Template/Location.json`     | Chamber interface positioning                                              |
| `assets/resource/pipeline/DijiangRewards/Template/TextTemplate.json` | Button and state OCR templates                                             |
| `assets/resource/pipeline/DijiangRewards/Template/Status.json`       | Auxiliary recognition for red dots, quantities, inventory, etc.            |
| `assets/locales/interface/*.json`                                    | Task, option, and focus copy                                               |

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

| Configuration              | Behavior                                                                              |
| -------------------------- | ------------------------------------------------------------------------------------- |
| `ClueSetting=No` (default) | Maximum of 3 gifts per session; send only if each clue inventory ≥ 3 (retain 2)       |
| `ClueSetting=Yes`          | Expand `ClueSend`, `ClueStockLimit` for customizing attempts and inventory thresholds |

The attempt limit modifies the `max_hit` of the gifting loop; the inventory threshold modifies the quantity OCR regex.

### Cultivation Chamber Options

Implemented in `GrowthChamber.json` + `pipeline_override` of `DijiangRewards.json`.

#### `SelectToGrow`: Overall Growth Direction

| Mode              | Actual Behavior                                                                                                                                                                                                |
| ----------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| No Growth         | Only claim mature rewards, do not enter material selection                                                                                                                                                     |
| Replant           | Disable normal growth; after reward claiming closes, attempt "replant" and confirm ([#2003](https://github.com/MaaEnd/MaaEnd/pull/2003) triggered after reward claiming closes, not directly from detail page) |
| Any Material      | Whitelist includes all materials; expand sorting and base core extraction sub-options; sort first then pick the first available target in the list                                                             |
| Specific Material | Whitelist narrowed to the multilingual name of that material; only expand base core extraction; row recognition bound to the entire target row, reducing quantity OCR jitter                                   |

#### `AutoExtractSeed`: What to Do When Base Core is Missing

Only appears in "Any Material" or "Specific Material" modes.

| Configuration | Actual Behavior                                                                                                                                                                                  |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Yes           | Accept targets with "base core" or "can extract base core"; follow extraction branch when base core is missing                                                                                   |
| No            | Filter tightened to must have base core; if accidentally entering extraction entry, retreat to list to continue search (also serves as fallback for accidental return after continuous planting) |

#### `SortBy` / `SortOrder`

Only appear in "Any Material" mode, only affect candidate list order, do not change the semantics of "who to find".

When maintaining cultivation chamber issues, first confirm three things: current `SelectToGrow` mode, whether sorting is enabled, and whether `AutoExtractSeed` has changed the acceptable target range.

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
    ├── Any → AutoExtractSeed, SortBy, SortOrder
└── Specific Material → AutoExtractSeed
```

## Paths to Modify When Adding New Cultivation Materials

1. `assets/tasks/DijiangRewards.json` — `SelectToGrow` add new case: `GrowthChamberSelectTarget.expected` + row recognition override
2. `assets/locales/interface/*.json` — Material name copy
3. If game button/chamber copy changes — synchronize `Template/TextTemplate.json`, `Template/Location.json`

## Maintenance Tips

| Symptom                            | Priority Check                                                    |
| ---------------------------------- | ----------------------------------------------------------------- |
| Cannot enter central hub           | `Entry.json`, SceneManager jump                                   |
| A stage not executing              | Corresponding stage switch under `StageTaskSetting`               |
| Reception room not gifting clues   | Whether `ClueSetting=No` default override matches advanced items  |
| No replant after claiming reward   | `SelectToGrow=GrowAgain`; next chain after reward claiming closes |
| Wrong material selected for growth | `SelectToGrow` whitelist; `SortBy`/`SortOrder` (Any mode)         |
| Has base core but not extracting   | `AutoExtractSeed` and `CheckTargetNotEmpty` linkage override      |
| OCR recognition drift              | Multilingual `expected` in three files under `Template/`          |

Maintenance is divided into three layers: main flow layer (which chamber to go to) → stage business layer (what to do in the chamber) → interface configuration layer (which branches options modify).
