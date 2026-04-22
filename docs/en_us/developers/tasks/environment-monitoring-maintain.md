# Developer Manual — EnvironmentMonitoring Maintenance

This document describes the Pipeline organization, route data, terminal grouping, automatic generation mechanism, and how to onboard new observation points for the `EnvironmentMonitoring` task.

The core characteristic of environment monitoring is **"data-driven + template batch generation"**: the Pipeline JSON for each observation point is not written by hand; instead, it is batch-rendered from the templates and data under `tools/pipeline-generate/EnvironmentMonitoring/` to `assets/resource/pipeline/EnvironmentMonitoring/` using the [`@joebao/maa-pipeline-generate`](https://www.npmjs.com/package/@joebao/maa-pipeline-generate) tool. Maintenance effort centers on **data files**, not hand-editing JSON.

> [!WARNING]
>
> `assets/resource/pipeline/EnvironmentMonitoring/{Station}/*.json` and `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json` are **generated artifacts**. Edits to these files will be overwritten the next time generation runs. All maintenance must go through the source data under `tools/pipeline-generate/EnvironmentMonitoring/`.

## Overview

The core maintenance points for environment monitoring are:

| Module                  | Path                                                                                | Purpose                                                                                                                                          |
| ----------------------- | ----------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| Task entry              | `assets/tasks/EnvironmentMonitoring.json`                                           | Interface task definition (no configurable options; controller = Win32-Front / Wlroots / ADB)                                                    |
| Main-flow Pipeline      | `assets/resource/pipeline/EnvironmentMonitoring.json`                               | Top-level entry node `EnvironmentMonitoringMain`, loops over the two monitoring terminals                                                        |
| Terminal groups (gen.)  | `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json`                     | Entry nodes for Outskirts / MarkerStone terminals and their observation-point `next` lists (**generated**)                                       |
| Terminal navigation     | `assets/resource/pipeline/EnvironmentMonitoring/Locations.json`                     | `EnvironmentMonitoringGoTo*` and `Select*` nodes that navigate from the main menu into the respective terminal                                   |
| Photo-taking flow       | `assets/resource/pipeline/EnvironmentMonitoring/TakePhoto.json`                     | Enters photo mode, adjusts camera facing, identifies the shutter button, returns to the terminal after completion                                |
| Camera swipe            | `assets/resource/pipeline/EnvironmentMonitoring/TakePhoto.json`                     | `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}` four-direction facing adjustment                                                          |
| Shared buttons          | `assets/resource/pipeline/EnvironmentMonitoring/Button.json`                        | Environment-monitoring-specific shared buttons such as `TrackMissionButton`                                                                      |
| Observation-point nodes | `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`                | **One JSON per observation point**, rendered from the template (**generated**)                                                                   |
| Point template          | `tools/pipeline-generate/EnvironmentMonitoring/template.jsonc`                      | Single-observation-point Pipeline template (text recognition, accept/go-to, teleport, pathfinding, photo)                                       |
| Terminal template       | `tools/pipeline-generate/EnvironmentMonitoring/terminals-template.jsonc`            | Terminal-group node template                                                                                                                     |
| Route / coordinate data | `tools/pipeline-generate/EnvironmentMonitoring/routes.mjs`                          | `ROUTE_CONFIG`: teleport target, map name, path, camera-swipe direction for each observation point                                               |
| Terminal list data      | `tools/pipeline-generate/EnvironmentMonitoring/terminals-data.mjs`                  | Terminal ID list; chains each observation-point Job node into the corresponding terminal's `next`                                                |
| Game data snapshot      | `tools/pipeline-generate/EnvironmentMonitoring/kite_station.json`                   | Official terminal/commission data from `zmdmap` (multi-language names, `shotTargetName`)                                                         |
| Generator config        | `tools/pipeline-generate/EnvironmentMonitoring/config.json`                         | Per-point output config: `outputPattern: "${Station}/${Id}.json"`                                                                                |
| Terminal gen. config    | `tools/pipeline-generate/EnvironmentMonitoring/terminals-config.json`               | Merged terminal output config: `outputFile: "Terminals.json"`                                                                                    |
| Locale strings          | `assets/locales/interface/*.json`                                                   | `task.EnvironmentMonitoring.*` label / description (task level; observation-point names use OCR)                                                 |
| MapTracker dependency   | `agent/go-service/map-tracker/`                                                     | `MapTrackerMove`, `MapTrackerAssertLocation` (see [map-tracker.md](../components/map-tracker.md))                                                |
| SceneManager dependency | `assets/resource/pipeline/SceneManager/`, `Interface/`                              | `SceneEnterWorldWuling*`, `SceneEnterMenuRegionalDevelopmentWulingEnvironmentMonitoring` (see [scene-manager.md](../scene-manager.md))            |

## Main flow

At runtime, environment monitoring iterates in the following hierarchy:

```text
EnvironmentMonitoringMain
  └─ EnvironmentMonitoringLoop                    (recognizes terminal selection screen)
       ├─ [JumpBack]OutskirtsMonitoringTerminal   (Outskirts Monitoring Terminal)
       │    └─ OutskirtsMonitoringTerminalLoop
       │         ├─ [JumpBack]{Id}Job × N         (iterates all observation points under this terminal)
       │         └─ EnvironmentMonitoringFinish
       ├─ [JumpBack]MarkerStoneMonitoringTerminal  (MarkerStone Monitoring Terminal)
       │    └─ MarkerStoneMonitoringTerminalLoop
       │         ├─ [JumpBack]{Id}Job × N
       │         └─ EnvironmentMonitoringFinish
       └─ EnvironmentMonitoringFinish
```

The chain inside each observation-point `{Id}Job` (rendered from `template.jsonc`):

```text
{Id}Job                               (recognizes this observation-point list item)
  ├─ Accept{Id}                       (commission available → click accept)
  └─ GoTo{Id}Mission                  (commission already accepted → click go-to)
       └─ {Id}TrackOrGoTo
            ├─ Track{Id}              (if "Start Tracking" button present → click it)
            └─ GoTo{Id}              (SubTask: SceneAnyEnterWorld → back to open world)
                 ├─ GoTo{Id}StartPos  (MapTrackerAssertLocation confirms position → MapTrackerMove)
                 └─ GoTo{Id}NotAtStartPos
                      └─ SubTask: ${EnterMap}             (teleport)
                           ├─ GoTo{Id}RecheckStartPos     (re-check position after teleport)
                           └─ GoTo{Id}ReEnterMap          (second teleport → FinalCheck)
                                └─ GoTo{Id}MapTrackerMove
                                     ├─ anchor: EnvironmentMonitoringBactToTerminal → ${GoToMonitoringTerminal}
                                     ├─ anchor: EnvironmentMonitoringAdjustCamera   → ${Id}AdjustCamera
                                     └─ next:   EnvironmentMonitoringTakePhoto
EnvironmentMonitoringTakePhoto        (enters photo mode → adjusts facing → takes photo)
  └─ [Anchor]EnvironmentMonitoringBactToTerminal
       └─ EnvironmentMonitoringGoTo{Outskirts|MarkerStone}MonitoringTerminal
```

> [!NOTE]
>
> The two `anchor` keys are hard-coded placeholder names in the template (`EnvironmentMonitoringBactToTerminal` spelling is intentionally preserved from history—do not correct it). At runtime they are replaced with:
>
> - `EnvironmentMonitoringBactToTerminal` → the `EnvironmentMonitoringGoTo{Station}` node for the terminal this observation point belongs to (returns to the correct terminal after shooting)
> - `EnvironmentMonitoringAdjustCamera` → `{Id}AdjustCamera` (executes the camera-swipe direction for this observation point)

## Naming conventions

### Observation-point ID (`Id`)

`ROUTE_CONFIG[*].Id` in `routes.mjs`; serves as the prefix for all generated node names:

```text
{PascalCase English name}
```

Examples:

```text
WaterTemperatureController        → 净水温控装置
EcologyNearTheFieldLogisticsDepot → 储备站周围的生态环境
MysteriousCryptidGraffiti         → 谜之生物的涂鸦
```

`Id` is derived by default from the `name["en-US"]` of the corresponding task in `kite_station.json`, PascalCase'd via `buildDefaultId()` / `toPascalCase()` in `data.mjs`. **When an explicit `Id` is provided in `ROUTE_CONFIG`, that value takes precedence**—this is the only way to decouple `Id` from the game's official English name.

> [!IMPORTANT]
>
> Do not use `Id` as display text. Display text comes from `Name` (Chinese) or OCR; `Id` is only used for constructing node names and file names (`outputPattern: "${Station}/${Id}.json"`). `Id` must be a valid identifier (only `[A-Za-z0-9]`) and must match the `[JumpBack]{Id}Job` entries in the `next` list exactly.

### Terminal group (`Station`)

Derived in `data.mjs` by `buildStationName()` from `mission.kiteStation` (or falling back to `__terminalId`), mapped to `kite_station.json[terminalId].level.name["en-US"]` and PascalCase'd. The current repository contains two groups:

| Chinese name | Station ID                      | terminalId          | `GoToMonitoringTerminal` anchor                          |
| ------------ | ------------------------------- | ------------------- | -------------------------------------------------------- |
| 城郊监测终端 | `OutskirtsMonitoringTerminal`   | `kitestation_002_1` | `EnvironmentMonitoringGoToOutskirtsMonitoringTerminal`   |
| 首墩监测终端 | `MarkerStoneMonitoringTerminal` | `kitestation_004_1` | `EnvironmentMonitoringGoToMarkerStoneMonitoringTerminal` |

When a new Station appears, **the generator side (`routes.mjs` + `data.mjs`) requires zero changes**: `MONITORING_TERMINAL_IDS` is derived automatically from `kite_station.json`, and the `GoToMonitoringTerminal` anchor name is assembled via the `EnvironmentMonitoringGoTo{Station}` template. However, the following **hand-written linking nodes** referenced by the generated Pipeline must be added first, otherwise MaaFramework will report "referenced undefined task" at runtime:

1. `assets/resource/pipeline/EnvironmentMonitoring/Locations.json`: add `EnvironmentMonitoringGoTo{Station}MonitoringTerminal` and `EnvironmentMonitoringSelect{Station}MonitoringTerminal` nodes.
2. `EnvironmentMonitoringLoop.next` in `assets/resource/pipeline/EnvironmentMonitoring.json`: add `[JumpBack]{Station}MonitoringTerminal`.
3. If new text-recognition nodes are needed (e.g. `EnvironmentMonitoringCheck{Station}MonitoringTerminalText`, `EnvironmentMonitoringIn{Station}MonitoringTerminal`), add them by hand in the Pipeline.

## Automatic generation

### Per-point: `config.json`

```json
{
    "template": "template.jsonc",
    "data": "data.mjs",
    "outputDir": "../../../assets/resource/pipeline/EnvironmentMonitoring",
    "outputPattern": "${Station}/${Id}.json",
    "format": true,
    "merged": false
}
```

`data.mjs`'s default export is an array; each element is the render context for one observation point (field names map to `${Xxx}` placeholders in `template.jsonc`). It reads the manually maintained `ROUTE_CONFIG` / `ROUTE_DEFAULTS` from `routes.mjs` and assembles each row together with `kite_station.json`:

| Field                               | Source                                                                                  |
| ----------------------------------- | --------------------------------------------------------------------------------------- |
| `Station`                           | English terminal name from `kite_station.json` (PascalCase)                             |
| `Id`                                | `ROUTE_CONFIG[*].Id` if provided; otherwise official English name PascalCase'd          |
| `Name`                              | `name["zh-CN"]` from `kite_station.json`, special characters stripped                   |
| `GoToMonitoringTerminal`            | Determined by `Station`                                                                  |
| `EnterMap`                          | `ROUTE_CONFIG[*].EnterMap`; **must be an existing SceneManager node name**               |
| `MapName` / `MapTarget` / `MapPath` | `ROUTE_CONFIG[*]`; maps to `MapTrackerMove` / `MapTrackerAssertLocation` parameters      |
| `CameraSwipeDirection`              | `ROUTE_CONFIG[*]`; must be one of `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}` |
| `CameraMaxHit`                      | `ROUTE_CONFIG[*].CameraMaxHit`; defaults to `ROUTE_DEFAULTS.CameraMaxHit` (`2`); corresponds to the max-hit count for `${Id}AdjustCamera` swipe |
| `ExpectedText`                      | Expanded automatically from `mission.name` multi-language map in `kite_station.json` (5 languages, English converted to a flexible regex) |
| `InExpectedText`                    | Expanded from `mission.shotTargetName` in `kite_station.json`                            |

### Terminal groups: `terminals-config.json`

```json
{
    "template": "terminals-template.jsonc",
    "data": "terminals-data.mjs",
    "outputDir": "../../../assets/resource/pipeline/EnvironmentMonitoring",
    "outputFile": "Terminals.json",
    "format": true,
    "merged": true
}
```

`terminals-data.mjs` scans all rows assembled by `data.mjs`, groups them by `Station`, chains each observation point's `[JumpBack]{Id}Job` into the corresponding terminal's `next` list, and appends `EnvironmentMonitoringFinish` at the end.

### Run commands

```bash
# Install dependency (first time only)
npm i -g @joebao/maa-pipeline-generate
# Or as a one-off: npx @joebao/maa-pipeline-generate

# Run in the tools/pipeline-generate/EnvironmentMonitoring/ directory:

# 1) Render all observation-point Pipelines
npx @joebao/maa-pipeline-generate

# 2) Render terminal entry nodes
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

> [!NOTE]
>
> If an observation point is missing fields at render time, `data.mjs` falls back to `ROUTE_DEFAULTS` (`SceneAnyEnterWorld` + placeholder coordinates) and emits a `console.warn`. **Pipelines rendered with placeholder values pass lint but cannot reach the observation point at runtime.** After adding a new point, always confirm that all `ROUTE_CONFIG` fields are filled in to avoid leaving placeholder entries behind.

## Key dependencies

### MapTracker

The three phases "teleport → recheck → pathfind" for each observation point all depend on `agent/go-service/map-tracker/`:

- `MapTrackerAssertLocation` (recognizer): determines whether the current minimap position is within the `MapTarget` rectangle.
- `MapTrackerMove` (action): walks along `MapPath` to the target, with anchor-rewrite support for `EnvironmentMonitoringBactToTerminal` / `EnvironmentMonitoringAdjustCamera`.

For detailed parameters and coordinate recording, see [map-tracker.md](../components/map-tracker.md) and [map-navigator.md](../components/map-navigator.md).

### SceneManager

The `EnterMap` field must be an existing teleport node name in SceneManager, e.g. `SceneEnterWorldWulingJingyuValley7`. If a new observation point is in a yet-unsupported teleport location, the corresponding `SceneEnterWorld*` and scene-recognition nodes must first be added under `assets/resource/pipeline/SceneManager/` and `assets/resource/pipeline/Interface/` (see [scene-manager.md](../scene-manager.md)).

Special fallback: `SceneAnyEnterWorld` means "skip teleport, return to the current world directly." When no suitable teleport exists for an observation point (e.g. the "Rainbow" point that lacks a Qingyun Cave teleport in this branch), `SceneAnyEnterWorld` can be used temporarily combined with a precise `MapTarget` / `MapPath` so the character walks there; mark it with a `// TODO:` comment in `routes.mjs`.

### Main menu entry

The main entry node `EnvironmentMonitoringMain` enters the terminal selection screen via `[JumpBack]SceneEnterMenuRegionalDevelopmentWulingEnvironmentMonitoring`. That node is maintained in `assets/resource/pipeline/Interface/SceneInMenu.json`. When adding a new regional monitoring terminal, confirm that the main menu entry can navigate into the corresponding screen.

## Adding a new observation point

New observation points generally come from game updates, reflected as additional `mission` entries in `kite_station.json`. Maintenance flow:

> [!TIP]
>
> If you are using a client that supports AI Skills (such as Claude Code or GitHub Copilot), you can invoke the **`environment-monitoring-add-route` skill** directly. It will automatically detect missing entries and guide you through filling in `ROUTE_CONFIG` field-by-field via interactive prompts, saving you from manual look-ups.

### 1. Update game data

Replace `tools/pipeline-generate/EnvironmentMonitoring/kite_station.json` with the latest version (source: `zmdmap`).

### 2. Identify missing entries

Compare `entrustTasks` in `kite_station.json` against entries in `ROUTE_CONFIG` and confirm:

- **Missing config**: the observation point is absent from `ROUTE_CONFIG` entirely → proceed to step 3.
- **Placeholder pending completion**: the entry exists in `ROUTE_CONFIG` but `EnterMap` / `MapPath` etc. are still default values → proceed to step 4.

### 3. Add an entry to `ROUTE_CONFIG`

In `tools/pipeline-generate/EnvironmentMonitoring/routes.mjs` → `ROUTE_CONFIG`:

```javascript
{
    Id: "MyNewObservationPoint",         // PascalCase; used as node prefix and file name
    Name: "我的新观察点",                 // must match zh-CN name in kite_station.json (after stripping special characters)
    EnterMap: "SceneEnterWorldWulingXxx",// existing teleport node in SceneManager
    MapName: "map02_lv001",              // MapTracker minimap identifier
    MapTarget: [x, y, w, h],             // target rectangle (minimap coordinates)
    MapPath: [[x1, y1], [x2, y2], ...],  // pathfinding route (minimap coordinates)
    CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp", // facing-adjustment direction
    // CameraMaxHit: 2,  // optional; max swipe count, default 2; increase if the target is hard to frame
}
```

> [!IMPORTANT]
>
> `Name` is the matching key used internally by `data.mjs`'s `normalizeMissionName()`; it is compared against `mission.name["zh-CN"]` in `kite_station.json` with symbols stripped and lowercased. If the match fails, the override in `ROUTE_CONFIG` will not take effect and the default values will be used as fallback.

### 4. Record coordinates and path

Use the GUI tool described in [map-navigator.md](../components/map-navigator.md) to record `MapTarget` / `MapPath`, and verify in-game:

- Which direction the camera needs to swipe when taking the photo (determines `CameraSwipeDirection`).
- Whether the standing position allows `EnvironmentMonitoringTakePhoto` to follow the `EnvironmentMonitoringEnterCameraMode` path (auto-face target) successfully; if not, it automatically falls back to `EnvironmentMonitoringTakePhotoDirectly` + manual swipe `${Id}AdjustCamera`.

### 5. Regenerate the Pipeline

```bash
cd tools/pipeline-generate/EnvironmentMonitoring
npx @joebao/maa-pipeline-generate
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

Verify the two categories of generated files:

- `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`
- `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json` (`{Station}MonitoringTerminalLoop.next` contains `[JumpBack]{Id}Job`)

## Modifying an existing observation-point route

Adjusting only the route/facing (no change to the English name):

1. Edit `ROUTE_CONFIG[i]` in `tools/pipeline-generate/EnvironmentMonitoring/routes.mjs`.
2. Regenerate (only `npx @joebao/maa-pipeline-generate` is needed; re-generating `Terminals.json` is unnecessary when the terminal list is unchanged).
3. Commit `routes.mjs` together with the regenerated `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`.

If the observation point's official English name changes and causes the `Id` to drift:

1. Explicitly add `Id: "ExistingId"` in `ROUTE_CONFIG` to pin the old ID (prevents all `[JumpBack]{Id}Job` entries in `next` chains from breaking).
2. Regenerate.

## Pre-submit checklist

Before committing, at minimum verify:

1. New/modified entries in `ROUTE_CONFIG` in `tools/pipeline-generate/EnvironmentMonitoring/routes.mjs` have all required fields.
2. `EnterMap`, `MapTarget`, `MapPath`, and `CameraSwipeDirection` in new `ROUTE_CONFIG` entries all hold real values (not `ROUTE_DEFAULTS` placeholders).
3. The regenerated `Terminals.json` has `[JumpBack]{Id}Job` for every new point in both `{Station}MonitoringTerminalLoop.next` lists, ending with `EnvironmentMonitoringFinish`.
4. The `Scene*` node referenced by `EnterMap` actually exists under `assets/resource/pipeline/SceneManager/` and `Interface/`.
5. `CameraSwipeDirection` is one of `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}`.
6. **No hand-edits** to `assets/resource/pipeline/EnvironmentMonitoring/{Station}/*.json` or `Terminals.json` (hand-edits are overwritten on the next generation run; if special nodes are truly needed, extend `template.jsonc` / `terminals-template.jsonc`).
7. JSON files conform to `.prettierrc` formatting (the generator has `format: true`, but running `pnpm prettier --write` before committing is even safer).

## Common pitfalls

- **Hand-editing generated artifacts**: Directly editing `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json` or `Terminals.json` will lose changes on the next generation run. Edit source data and regenerate.
- **`Name` does not match game text**: `ROUTE_CONFIG[i].Name` is only used internally in `data.mjs` to match `mission.name["zh-CN"]` in `kite_station.json`. It is not display text or an OCR expectation. A mismatch emits a `console.warn` and falls back to the placeholder.
- **`Id` drifts from `kite_station.json` English name**: When the game renames an item in English, the auto-computed `Id` changes, invalidating old `[JumpBack]{Id}Job` entries in `Terminals.json`. Add `ROUTE_CONFIG[i].Id` explicitly to pin the old ID.
- **`EnterMap` references a non-existent Scene node**: The generator does not validate this; at runtime the task will loop indefinitely at `GoTo{Id}NotAtStartPos`.
- **`MapPath` passes through locked areas / combat / interactables**: MapTracker does not handle combat or cutscenes; paths must only traverse freely walkable sections.
- **New `Station` added but `Locations.json` / `EnvironmentMonitoringLoop.next` not updated**: the new terminal cannot be recognized or entered, so all its observation points are unreachable.
- **`anchor` placeholder name spelling**: `EnvironmentMonitoringBactToTerminal` is the historical spelling (missing a `k`—intentional, not a bug). It must stay consistent with `[Anchor]EnvironmentMonitoringBactToTerminal` in `TakePhoto.json`. Do not "fix" it to `Back`.
- **"Passes generation ≠ passes runtime"**: `ROUTE_DEFAULTS` prevents generation-stage errors, but `EnterMap=SceneAnyEnterWorld` + `MapPath=[[0,0]]` will never reach the target at runtime. Before committing, manually verify that no placeholder entries remain in `ROUTE_CONFIG` (entries with `EnterMap` set to `SceneAnyEnterWorld` and no `// TODO:` comment should raise a flag).
