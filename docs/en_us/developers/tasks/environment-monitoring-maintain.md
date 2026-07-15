# Development Manual - EnvironmentMonitoring Maintenance Documentation

This document explains the Pipeline organization, route data, terminal grouping, automatic generation mechanism, and integration method for new observation points for the `EnvironmentMonitoring` task.

The core characteristic of EnvironmentMonitoring is **"Data-Driven + Template Batch Generation"**: The Pipeline JSON corresponding to each observation point is not manually written but is batch-rendered into `assets/resource/pipeline/EnvironmentMonitoring/` using the [`@joebao/maa-pipeline-generate`](https://www.npmjs.com/package/@joebao/maa-pipeline-generate) tool, combining template/route configurations from `tools/pipeline-generate/EnvironmentMonitoring/` and zmdmap cache data from `tools/pipeline-generate/data/`. The focus of maintenance work is on **generation configuration and data caching**, not manually editing JSON.

> [!WARNING]
>
> `assets/resource/pipeline/EnvironmentMonitoring/{Station}/*.json` and `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json` are **generated artifacts**. Manually modifying these files will be overwritten during the next regeneration. All maintenance should be done in the generation configuration under `tools/pipeline-generate/EnvironmentMonitoring/`, or by updating the zmdmap cache under `tools/pipeline-generate/data/` via `pnpm fetch:zmdmap`.

## Overview

The core maintenance points for EnvironmentMonitoring are as follows:

| Module                              | Path                                                                              | Function                                                                                                                                                                                                                                                                           |
| ----------------------------------- | --------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Task Entry                          | `assets/tasks/EnvironmentMonitoring.json`                                         | Interface task definition (no configurable options, controller = Win32-Front / Wlroots / ADB)                                                                                                                                                                                      |
| Main Flow Pipeline                  | `assets/resource/pipeline/EnvironmentMonitoring.json`                             | Main entry node `EnvironmentMonitoringMain`, loops to identify the two monitoring terminals                                                                                                                                                                                        |
| Terminal Grouping (Generated)       | `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json`                   | Entry nodes for Outskirts Monitoring Terminal / Marker Stone Monitoring Terminal and their respective observation point `next` lists (**generated**)                                                                                                                               |
| Terminal Jump                       | `assets/resource/pipeline/EnvironmentMonitoring/Locations.json`                   | `EnvironmentMonitoringGoTo*` and `Select*` nodes, enter the corresponding terminal from the main menu                                                                                                                                                                              |
| Photo Taking Flow                   | `assets/resource/pipeline/EnvironmentMonitoring/TakePhoto.json`                   | Enter photo mode, adjust orientation, identify the photo button, return to terminal after achieving the goal                                                                                                                                                                       |
| Camera Swipe                        | `assets/resource/pipeline/EnvironmentMonitoring/TakePhoto.json`                   | `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}` four-direction orientation adjustment                                                                                                                                                                                       |
| Common Buttons                      | `assets/resource/pipeline/EnvironmentMonitoring/Button.json`                      | EnvironmentMonitoring-specific common buttons like `TrackMissionButton`                                                                                                                                                                                                            |
| Observation Point Nodes (Generated) | `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`              | **One JSON per observation point**, rendered from templates (**generated**); `Id` is automatically generated by `data.mjs`, usually no manual writing needed                                                                                                                       |
| Observation Point Template          | `tools/pipeline-generate/EnvironmentMonitoring/generator/template.json`           | Single observation point Pipeline template (text recognition, accept/go, teleport, pathfinding, photo taking)                                                                                                                                                                      |
| Terminal Template                   | `tools/pipeline-generate/EnvironmentMonitoring/generator/terminals-template.json` | Terminal grouping node template                                                                                                                                                                                                                                                    |
| Route/Coordinate Data               | `tools/pipeline-generate/EnvironmentMonitoring/routes.json`                       | Route overrides matched by observation point `MissionId` (teleport points, map, path, camera swipe direction); `Name` is for human reading only, `Id` is the final template node ID, convenient for searching generated nodes/file names                                           |
| Route JSON Schema                   | `tools/schema/environment_monitoring_routes.schema.json`                          | Field constraints for `routes.json` (required fields, enums, coordinate array shapes), automatically associated via `.vscode/settings.json`, providing IDE field completion and validation                                                                                         |
| Failure Collector Parameter Schema  | `tools/schema/components/failure_collector.schema.json`                           | Parameter constraints for generic Failure Collector Custom Actions; action names are registered in `tools/schema/custom.action.schema.json`                                                                                                                                        |
| Route Sync Logic                    | `tools/pipeline-generate/EnvironmentMonitoring/generator/sync-routes.mjs`         | Automatically syncs `MissionId` / `Name` / `Id` in `routes.json` before generation and sorts by `MissionId`                                                                                                                                                                        |
| Route Resolution Logic              | `tools/pipeline-generate/EnvironmentMonitoring/generator/route-resolver.mjs`      | Parses `routes.json` entries into pathfinding recognition/action parameters required by the template and uniformly handles unadapted fallbacks                                                                                                                                     |
| Terminal List Data                  | `tools/pipeline-generate/EnvironmentMonitoring/generator/terminals-data.mjs`      | Generates each terminal's `next` from the row data of `data.mjs` and the automatically derived terminal list                                                                                                                                                                       |
| Game Data Snapshot                  | `tools/pipeline-generate/data/kite_station_i18n.json`                             | Official monitoring terminal/quest data provided by `zmdmap` (multilingual names, `shotTargetName`), cached by `pnpm fetch:zmdmap`                                                                                                                                                 |
| Generator Config                    | `tools/pipeline-generate/EnvironmentMonitoring/generator/config.json`             | Single observation point output configuration: `outputPattern: "${Station}/${Id}.json"`                                                                                                                                                                                            |
| Terminal Generator Config           | `tools/pipeline-generate/EnvironmentMonitoring/generator/terminals-config.json`   | Terminal output configuration merged into a single file: `outputFile: "Terminals.json"`                                                                                                                                                                                            |
| Multilingual Text                   | `assets/locales/interface/*.json`                                                 | `task.EnvironmentMonitoring.*` label / description (task-level; observation point names use OCR)                                                                                                                                                                                   |
| Common Component Dependencies       | `agent/go-service/maptracker/` / `3rdparty/maa-copilot`                           | `MapTrackerMove`, `MapTrackerGoal`, `MapTrackerAssertLocation`, `MapLocateAssertLocation`, `MapNavigateAction` (see details in [map-tracker.md](../components/map-tracker.md), [map-locator.md](../components/map-locator.md), [map-navigator.md](../components/map-navigator.md)) |
| Scene Transition Dependencies       | `assets/resource/pipeline/SceneManager/`、`Interface/`                            | `SceneEnterWorldWuling*`, `SceneEnterMenuRegionalDevelopmentWulingEnvironmentMonitoring` (see details in [scene-manager.md](../scene-manager.md))                                                                                                                                  |

## Main Flow

EnvironmentMonitoring runs in the following hierarchical loop at runtime:

```text
EnvironmentMonitoringMain
  └─ EnvironmentMonitoringLoop                   (Identifies monitoring terminal selection interface)
       ├─ [JumpBack]OutskirtsMonitoringTerminal  (Outskirts Monitoring Terminal)
       │    └─ OutskirtsMonitoringTerminalLoop
       │         ├─ [JumpBack]{Id}Job × N        (Iterates through all observation points under this terminal)
       │         └─ EnvironmentMonitoringTerminalFinish
       ├─ [JumpBack]MarkerStoneMonitoringTerminal (Marker Stone Monitoring Terminal)
       │    └─ MarkerStoneMonitoringTerminalLoop
       │         ├─ [JumpBack]{Id}Job × N
       │         └─ EnvironmentMonitoringTerminalFinish
       └─ EnvironmentMonitoringFinish
```

The internal chain for each observation point `{Id}Job` (rendered by `template.json`):

```text
{Id}Job                              (Identifies this observation point list item)
  ├─ Accept{Id}                      (Quest can be accepted -> Click to accept)
  └─ GoTo{Id}Mission                 (Quest already accepted -> Click to go)
       └─ {Id}TrackOrGoTo
            ├─ Track{Id}             (If "Start Tracking" button exists, click it)
            │    ├─ {Id}NotAdapted   (Route not adapted -> Only prompt and end this observation point)
            │    └─ GoTo{Id}         (Route adapted -> Continue to go)
            └─ AlreadyTracked{Id}    (Already tracking)
                 ├─ {Id}NotAdapted   (Route not adapted -> Only prompt and end this observation point)
                 └─ GoTo{Id}         (Route adapted -> Continue to go)
                      ├─ GoTo{Id}StartPos (MapTrackerAssertLocation / MapLocateAssertLocation in position -> MapTrackerMove / MapTrackerGoal / MapNavigateAction)
                      └─ GoTo{Id}NotAtStartPos
                           └─ SubTask: ${EnterMap}            (Teleport)
                                └─ GoTo{Id}StartPos           (Check if near the task start position)
                                     └─ GoTo{Id}Move
                                          ├─ anchor: EnvironmentMonitoringBackToTerminal → ${GoToMonitoringTerminal}
                                          ├─ anchor: EnvironmentMonitoringAdjustCamera   → ${Id}AdjustCamera
                                          └─ next:   EnvironmentMonitoringTakePhoto
EnvironmentMonitoringTakePhoto       (Enter photo mode -> orientation -> take photo)
  └─ [Anchor]EnvironmentMonitoringBackToTerminal
       └─ EnvironmentMonitoringGoTo{Outskirts|MarkerStone}MonitoringTerminal
```

Each `{Id}Job` still identifies its observation point list item, then uses the generic `FailureCollectorRunTask` action to execute the `{Id}Execute` route. The generator synchronizes zmdmap's five-language `mission.name` data into `task.EnvironmentMonitoring.route.{Id}.label`. If any node inside the route fails, the wrapper Action records `{Id}Failed`, runs `recovery_task` to return to the current monitoring terminal, and reports success outward so the remaining routes continue. After all terminals have been processed, `EnvironmentMonitoringFinish` uses `FailureCollectorFinish` to call those notification nodes in failure order, then returns overall failure. The Agent does not directly print user-facing messages.

> [!NOTE]
>
> The two keys for the `anchor` field are hardcoded placeholder names in the template, replaced at runtime with:
>
> - `EnvironmentMonitoringBackToTerminal` → The `EnvironmentMonitoringGoTo{Station}` node of the terminal the current observation point belongs to (returns to the correct terminal after photo)
> - `EnvironmentMonitoringAdjustCamera` → `{Id}AdjustCamera` (executes the camera swipe direction for this observation point)

## Naming Rules

### Observation Point Node ID (`Id`, Auto-generated)

`Id` is a generated field assembled by `data.mjs`, equivalent to the prefix for all observation point node names and output file names:

```text
{PascalCase English Name}
```

For example:

```text
WaterTemperatureController        -> Water Temperature Control Device
EcologyNearTheFieldLogisticsDepot -> Ecology near the Field Logistics Depot
MysteriousCryptidGraffiti         -> Mysterious Cryptid Graffiti
```

By default, `Id` is derived by PascalCase conversion of the `name["en-US"]` for that task from `kite_station_i18n.json`, with rules in `common.mjs`'s `buildDefaultId()` / `toPascalCase()`. If the English name is missing, it falls back to `missionId` / `entrustIdx`; if duplicates occur, `ensureUniqueId()` automatically appends a suffix.

When maintaining `routes.json`, you don't need to manually calculate `Id`. The route matching key is `MissionId`. `Id` will be automatically written to `routes.json` during regeneration, equivalent to the node name prefix used by the final template, convenient for directly searching generated nodes and file names.

> [!IMPORTANT]
>
> Do not treat `Id` as display text. Display text uses zmdmap names / OCR; `Name` is a human-readable note in routes.json, `Id` is only used for concatenating node names and file names (`outputPattern: "${Station}/${Id}.json"`), and is automatically refreshed by the generator.

### Terminal Grouping (`Station`)

Derived by `data.mjs`'s `buildStationName()` from the PascalCase of the `kite_station_i18n.json[terminalId].level.name["en-US"]` corresponding to `mission.kiteStation` (or falling back to `__terminalId`). Currently, there are only two groups in the repository:

| Chinese Name | Station ID                      | Corresponding terminalId | `GoToMonitoringTerminal` Anchor                          |
| ------------ | ------------------------------- | ------------------------ | -------------------------------------------------------- |
| 城郊监测终端 | `OutskirtsMonitoringTerminal`   | `kitestation_002_1`      | `EnvironmentMonitoringGoToOutskirtsMonitoringTerminal`   |
| 首墩监测终端 | `MarkerStoneMonitoringTerminal` | `kitestation_004_1`      | `EnvironmentMonitoringGoToMarkerStoneMonitoringTerminal` |

If a new Station appears, **the generator side (`routes.json` + `data.mjs`) requires zero changes**: `MONITORING_TERMINAL_IDS` is automatically derived from `kite_station_i18n.json`, and the `GoToMonitoringTerminal` anchor name is concatenated according to the `EnvironmentMonitoringGoTo{Station}` template. However, the following **hand-written linked nodes** referenced by the generated Pipeline must be completed first, otherwise MaaFramework will report "undefined task referenced" at runtime:

1. `assets/resource/pipeline/EnvironmentMonitoring/Locations.json`: Add new `EnvironmentMonitoringGoTo{Station}MonitoringTerminal` and `EnvironmentMonitoringSelect{Station}MonitoringTerminal` nodes.
2. `assets/resource/pipeline/EnvironmentMonitoring.json`'s `EnvironmentMonitoringLoop.next`: Add `[JumpBack]{Station}MonitoringTerminal`.
3. If there are new text recognition nodes (e.g., `EnvironmentMonitoringCheck{Station}MonitoringTerminalText`, `EnvironmentMonitoringIn{Station}MonitoringTerminal`), complete them in the Pipeline (hand-written).

## Automatic Generation Mechanism

### Single Observation Point: `config.json`

```json
{
    "template": "template.json",
    "data": "data.mjs",
    "outputDir": "../../../../assets/resource/pipeline/EnvironmentMonitoring",
    "outputPattern": "${Station}/${Id}.json",
    "format": true,
    "merged": false
}
```

The default export of `data.mjs` is an array, where each element = the rendering context for one observation point (field names correspond to the `${Xxx}` placeholders in `template.json`). `pnpm generate:EnvironmentMonitoring` first calls `sync-routes.mjs` to refresh the parent `routes.json`; subsequently, `data.mjs` only reads `routes.json` and `kite_station_i18n.json`, and assembles the final rows via `route-resolver.mjs`:

| Field                                                                           | Source                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| ------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `Station`                                                                       | English station name from `kite_station_i18n.json` (PascalCase)                                                                                                                                                                                                                                                                                                                                                                                                                |
| `Id`                                                                            | Automatically generated by default from the official English name in PascalCase; will be synced back to `routes.json`, equivalent to the node ID used by the final template                                                                                                                                                                                                                                                                                                    |
| `MissionId` / `Name`                                                            | `MissionId` is the primary matching key in `routes.json`; `Name` comes from the Chinese name in `kite_station_i18n.json`, for human reading only                                                                                                                                                                                                                                                                                                                               |
| `GoToMonitoringTerminal`                                                        | Determined by `Station`                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `EnterMap`                                                                      | `routes.json[*].EnterMap`, **must be a node name existing in SceneManager**                                                                                                                                                                                                                                                                                                                                                                                                    |
| `MapName` / `MapAssert` / `MapPath` / `MapTarget` / `MapTargetTier` / `MapGoal` | `routes.json[*]`, corresponding to the initial location check and subsequent pathfinding parameters; `MapPath` generates `MapTrackerAssertLocation` + `MapTrackerMove`, `MapTarget` generates `MapLocateAssertLocation` + `MapNavigateAction` with the `NAVMESH` target point, `MapTargetTier` optionally generates `target_tier`, `MapGoal` generates `MapTrackerAssertLocation` + `MapTrackerGoal`; only one of `MapPath` / `MapTarget` / `MapGoal` can and must be selected |
| `CameraSwipeDirection`                                                          | `routes.json[*]`, must be one of `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}`                                                                                                                                                                                                                                                                                                                                                                                        |
| `CameraMaxHit`                                                                  | `routes.json[*].CameraMaxHit`, defaults to `2`; corresponds to the maximum hit count for the `${Id}AdjustCamera` swipe action                                                                                                                                                                                                                                                                                                                                                  |
| `OcrReplace`                                                                    | Passed through from `routes.json[*].Replace` to `Check${Id}Text.replace` and `In${Id}Mission.replace`; used to configure task-specific OCR replacement pairs for the task list and mission detail page, without affecting route adaptation checks                                                                                                                                                                                                                              |
| `ExpectedText`                                                                  | Automatically expanded from the `mission.name` multilingual map in `kite_station_i18n.json` (5 languages, English converted to flexible regex)                                                                                                                                                                                                                                                                                                                                 |
| `InExpectedText`                                                                | Automatically expanded from the `mission.shotTargetName` in `kite_station_i18n.json`                                                                                                                                                                                                                                                                                                                                                                                           |
| `TrackOrGoToNext` / `AfterTrackedNext`                                          | Automatically determined by `data.mjs` based on whether the route is complete: `TrackOrGoToNext` converges to `Track${Id}` / `AlreadyTracked${Id}`, `AfterTrackedNext` is `GoTo${Id}` when adapted, `${Id}NotAdapted` when not adapted                                                                                                                                                                                                                                         |
| `AfterTeleportDescription` / `AfterTeleportNext`                                | Automatically determined by `data.mjs` based on the navigation type: `MapPath` returns to `GoTo${Id}StartPos` to verify the landing point after teleporting, while `MapTarget` / `MapGoal` proceed directly to `GoTo${Id}Move`                                                                                                                                                                                                                                                 |

### Terminal Grouping: `terminals-config.json`

```json
{
    "template": "terminals-template.json",
    "data": "terminals-data.mjs",
    "outputDir": "../../../../assets/resource/pipeline/EnvironmentMonitoring",
    "outputFile": "Terminals.json",
    "format": true,
    "merged": true
}
```

`terminals-data.mjs` scans all rows assembled by `data.mjs`, groups them by `Station`, links each observation point's `[JumpBack]{Id}Job` into the corresponding terminal's `next` list, and ends with `EnvironmentMonitoringTerminalFinish`. Each `{Id}Job` handles route failures through its `FailureCollectorRunTask` wrapper Action; after both terminals finish, the main flow uses `EnvironmentMonitoringFinish` to summarize the result.

### Run Commands

```bash
# Recommended: Run from the repository root
pnpm generate:EnvironmentMonitoring

# Only update zmdmap cache
pnpm fetch:zmdmap

# If you have already updated the zmdmap cache, you can also render individually in the tools/pipeline-generate/EnvironmentMonitoring/generator/ directory:

# 0) Sync MissionId/Name/Id in routes.json
node sync-routes.mjs

# 1) Render all observation point Pipelines
npx @joebao/maa-pipeline-generate

# 2) Render terminal entries
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

> [!NOTE]
>
> If `data.mjs` encounters an observation point without a `routes.json` entry during rendering, or the entry exists but any required field is missing (`null` / empty string / empty array), it will `console.warn` and treat the observation point as **unadapted**. Unadapted observation points will still generate a Pipeline, but at runtime will only accept and track the quest, ending after the `${Id}NotAdapted` prompt, without executing teleportation or pathfinding.

## Key Dependencies

### Pathfinding Components

The teleport and pathfinding flow for observation points combines MapTracker and MapNavigator according to the route type:

- `MapTrackerAssertLocation` / `MapLocateAssertLocation` (Recognition): Judges whether the current position is within the `MapAssert` rectangle based on the minimap. Uses `MapTrackerAssertLocation` when using `MapPath` / `MapGoal`, and `MapLocateAssertLocation` when using `MapTarget`.
- `MapTrackerMove` / `MapTrackerGoal` / `MapNavigateAction` (Action): Walks along the `MapPath` route to the target point, automatically plans and goes to the target according to `MapGoal` using `MapTrackerGoal`, or generates a `NAVMESH` target point and goes to the target according to `MapTarget`; `MapTargetTier` will be passed through as `target_tier`, used when the target coordinates and start point are not in the same tier; supports the anchor mechanism to rewrite `EnvironmentMonitoringBackToTerminal` / `EnvironmentMonitoringAdjustCamera` during the process.
- After teleporting, only `MapPath` routes verify `MapAssert` again. `MapTarget` / `MapGoal` routes rely on NavMesh to proceed from a nearby teleport landing point and start pathfinding immediately.

For detailed parameters and coordinate recording methods, see [map-tracker.md](../components/map-tracker.md), [map-locator.md](../components/map-locator.md), and [map-navigator.md](../components/map-navigator.md).

### SceneManager

The `EnterMap` field must be filled with the name of an existing teleport node in SceneManager, such as `SceneEnterWorldWulingJingyuValley7`. If a new observation point is located at a teleport point not yet supported, the corresponding `SceneEnterWorld*` and scene recognition nodes need to be completed first in `assets/resource/pipeline/SceneManager/` and `assets/resource/pipeline/Interface/` (see [scene-manager.md](../scene-manager.md)).

`data.mjs` determines whether to enter the pathfinding/photo-taking process based on whether the `routes.json` entry is complete. Unadapted points will directly take the `${Id}NotAdapted` branch. To make an observation point fully automated, you must provide the complete `EnterMap` (the actual `SceneEnterWorld*` node) / `MapName` / `MapAssert` / `CameraSwipeDirection` in `routes.json`, and select one from `MapPath` / `MapTarget` / `MapGoal`; when there is no available teleport point for the time being, you can omit the entry first, allowing it to run in the degraded "only accept and track" process.

### Main Menu Entry

The EnvironmentMonitoring main entry node `EnvironmentMonitoringMain` enters the terminal selection interface via `[JumpBack]SceneEnterMenuRegionalDevelopmentWulingEnvironmentMonitoring`. This node is maintained in `assets/resource/pipeline/Interface/InScene/Region.json`. When adding new regional monitoring terminals, ensure the main menu entry can enter the corresponding interface.

## Adding a New Observation Point

New observation points generally come from game updates, appearing as an additional `mission` in `kite_station_i18n.json`. The maintenance process:

> [!TIP]
>
> If you are using a client that supports AI Skills (like Claude Code or GitHub Copilot), you can directly call the **`environment-monitoring-add-route` skill**, which will automatically detect missing entries and help you fill in `routes.json` through interactive Q&A, saving the steps of manual table lookup.

### 1. Update Game Data

Run `pnpm fetch:zmdmap`, which will download and cache the latest `tools/pipeline-generate/data/kite_station_i18n.json` from the zmdmap API.

### 2. Check Route Adaptation Status

Compare the `entrustTasks` in `kite_station_i18n.json` with the entries in `routes.json` to confirm the status of each observation point. The matching method is `missionId` against `MissionId` in `routes.json`, not `Name` or `Id`:

- **Unadapted**: `routes.json` has no entry for this observation point, or the entry exists but is missing any required field (including `null` / empty string / empty array) → After generation, it will only accept and track.
- **Ready to adapt**: Needs to make this observation point automatically go and take a photo → Proceed to step 3 to complete the real route.

> [!IMPORTANT]
>
> If you do not intend to adapt a certain observation point, simply do not add the entry in `routes.json`; do not write placeholder values like `"SceneAnyEnterWorld"` / `[0,0,1,1]`.

### 3. Add/Complete Entries in `routes.json`

`tools/pipeline-generate/EnvironmentMonitoring/routes.json`:

```jsonc
{
    "MissionId": "m1m30",                    // Must match the missionId in kite_station_i18n.json
    "Name": "My New Observation Point",      // Chinese name, for human reading only
    "Id": "MyNewObservationPoint",           // Final template node ID, for human searching nodes/file names only
    "EnterMap": "SceneEnterWorldWulingXxx",  // Teleport node existing in SceneManager
    "MapName": "map02_lv001",                // Map identifier: MapPath uses MapTracker map_name; MapGoal uses exact MapTracker map_name that can load NavMesh; MapTarget uses MapLocate zone_id
    "MapAssert": [x, y, w, h],               // Initial location rectangle; only MapPath verifies it again after teleporting
    "MapPath": [[x1, y1], [x2, y2]],         // Pathfinding path (minimap coordinates), select one from MapTarget / MapGoal
    // "MapTarget": [x, y],                  // NAVMESH target point for MapNavigateAction
    // "MapTargetTier": "ValleyIV_L1_171",   // Optional; target_tier where the MapTarget coordinates are located, fill when target and start point are not in the same tier
    // "MapGoal": [x, y],                    // MapTrackerGoal target point, will automatically use MapTrackerGoal during generation
    "CameraSwipeDirection": "EnvironmentMonitoringSwipeScreenUp", // Orientation adjustment direction
    // "CameraMaxHit": 2,  // Optional; maximum swipe hit count, defaults to 2; can be increased slightly if the target is difficult to align
    // "Replace": [["売", "壳"]] // Optional; OCR replacement pairs for the task list and mission detail page
}
```

> [!IMPORTANT]
>
> `routes.json` is strict JSON: double quotes, no inline comments, no trailing commas. The `//` in the code block above is only for documentation; writing it into a real file will cause JSON parsing failure.
>
> `MissionId` is the matching key for `data.mjs`, which will exactly match the `missionId` in `kite_station_i18n.json`. `Name` is for human reading only, `Id` is for human searching of generated nodes/file names only; if inconsistent with the current zmdmap data, the generator will directly refresh it to the current correct value, without affecting matching.

> When regenerating EnvironmentMonitoring, `sync-routes.mjs` will first automatically refresh `MissionId` / `Name` / `Id` based on zmdmap data and sort by `MissionId`. When writing entries manually, `MissionId` must be filled; if a new task exists in zmdmap but `routes.json` has no corresponding entry, the generator will automatically append an unadapted placeholder entry containing only `MissionId` / `Name` / `Id`, making it convenient for maintainers to see routes that need completion.

### 4. Record Coordinates and Paths

Refer to the GUI tool in [map-navigator.md](../components/map-navigator.md) to record `MapAssert` / `MapPath`. Copy the `NAVMESH` target point from MapNavigateAction and fill it into `MapTarget`. If a cross-tier target is needed, fill the `target_tier` into `MapTargetTier`. Alternatively, copy the MapTrackerGoal target point and fill it into `MapGoal`, and confirm in the game:

- `MapName` is consistent with the tool used: For `MapPath` routes, fill in the MapTracker `map_name` (e.g., `map02_lv001` / regex); for `MapGoal` routes, fill in the exact MapTracker `map_name` that can load NavMesh (e.g., `map02_lv001`); for `MapTarget` routes, fill in the MapLocate `zone_id` (e.g., `Wuling_Base`); optional `MapTargetTier` fills the MapNavigator `target_tier` region name. Do not mix the two sets of identifiers.

- Which direction the camera needs to swipe for the photo (determines `CameraSwipeDirection`).
- Whether the standing position allows `EnvironmentMonitoringTakePhoto` to successfully execute `EnvironmentMonitoringEnterCameraMode` (auto-orient to target); if not, it will automatically fall back to `EnvironmentMonitoringTakePhotoDirectly` + manual swipe `${Id}AdjustCamera`.

### 5. Regenerate Pipeline

```bash
# Run from the repository root
pnpm generate:EnvironmentMonitoring

# Or execute separately in the generator directory
cd tools/pipeline-generate/EnvironmentMonitoring/generator
node sync-routes.mjs
npx @joebao/maa-pipeline-generate
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

Confirm the generation of the two types of files:

- `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`
- `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json` (contains `[JumpBack]{Id}Job` in `{Station}MonitoringTerminalLoop.next`)

Here `{Id}` is the node ID in the generation result. Usually, you can confirm by looking directly at the generated file name; no need to manually calculate in advance when maintaining `routes.json`.

## Modifying Existing Observation Point Routes

Only adjust the route/orientation (without changing the English name):

1. Modify the corresponding entry in `tools/pipeline-generate/EnvironmentMonitoring/routes.json`.
2. Regenerate. In normal cases, you can run `pnpm generate:EnvironmentMonitoring` directly in the repository root; if you confirm the terminal list has not changed, you can also run `node sync-routes.mjs && npx @joebao/maa-pipeline-generate` only in the `tools/pipeline-generate/EnvironmentMonitoring/generator/` directory, without regenerating `Terminals.json`.
3. Commit `routes.json` and the regenerated `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`.

If the official English name of the observation point changes, the generated `Id` / file name will also change; after regeneration, the `Id` in `routes.json` will be synced to the new final template ID.

## Self-Check List

Before submission, at least check:

1. Are the fields for new/modified entries in `tools/pipeline-generate/EnvironmentMonitoring/routes.json` complete?
2. Does the `MissionId` for new entries in `routes.json` match the `missionId` in `kite_station_i18n.json`; `Id` is automatically refreshed by the generator.
3. Have the `EnterMap`, `MapAssert`, `CameraSwipeDirection` for adapted entries been filled with real values, and has one of `MapPath` / `MapTarget` / `MapGoal` been selected and filled?
4. In the regenerated `Terminals.json`, does each `{Station}MonitoringTerminalLoop.next` contain all new `[JumpBack]{Id}Job`, and end with `EnvironmentMonitoringTerminalFinish`?
5. Does the `Scene*` node referenced by `EnterMap` actually exist in `assets/resource/pipeline/SceneManager/` and `Interface/`?
6. Is `CameraSwipeDirection` one of the four: `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}`?
7. **No manual modifications** were made to `assets/resource/pipeline/EnvironmentMonitoring/{Station}/*.json` or `Terminals.json` (manual modifications will be overwritten by the next generation; if special nodes are truly needed, they should be extended in `template.json` / `terminals-template.json`).
8. JSON files follow the `.prettierrc` format (the generator has `format: true`, but running `pnpm prettier --write` once before submission is safer).

## Common Pitfalls

- **Manually modifying generated artifacts**: Directly editing `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json` or `Terminals.json` will cause changes to be lost during the next regeneration. The correct approach is to modify the generation configuration / update the zmdmap cache and then regenerate.
- **`MissionId` mismatch with game data**: The `MissionId` in the `routes.json` entry is the matching key; `Name` / `Id` are only for human reading and searching. If `MissionId` matching fails, the generator will prompt that the entry is unused, and the corresponding observation point will be treated as unadapted (only accept and track).
- **Using `Id` as the matching key**: `Id` is only the final template node ID, convenient for searching generated nodes/file names; matching still only looks at `MissionId`.
- **`Id` drifts from `kite_station_i18n.json` English name**: When the game side changes the English name, the automatically calculated `Id` will change, possibly causing generated file renaming or residual old files; after regeneration, the `Id` in `routes.json` will be synced.
- **`EnterMap` references a non-existent Scene node**: Generation itself does not validate Scene references, and at runtime it will get stuck in an infinite loop at `GoTo{Id}NotAtStartPos`.
- **`MapPath` / `MapTarget` / `MapGoal` passes through unlocked areas / battles / interactive objects**: MapTracker and MapNavigateAction do not handle battles, story sequences, map transitions, or mechanism interactions; routes can only select pure traversal segments.
- **New `Station` but `Locations.json` / `EnvironmentMonitoringLoop.next` not synced**: The new terminal cannot be recognized and entered, so all observation points cannot run.
- **`anchor` placeholder name consistency**: The key name `EnvironmentMonitoringBackToTerminal` for the `anchor` in `template.json` must exactly match the `[Anchor]EnvironmentMonitoringBackToTerminal` in `TakePhoto.json`; otherwise, the anchor mechanism fails.
- **"Generation success ≠ Fully adapted"**: Observation points without a `routes.json` entry, or with an entry but missing required fields, will generate as degraded flows, only accepting and tracking, without going to take photos. Full automation requires completing the real `EnterMap`, `MapName`, `MapAssert`, `CameraSwipeDirection`, and selecting one from `MapPath` / `MapTarget` / `MapGoal`.
