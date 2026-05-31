# Development Manual - EnvironmentMonitoring Maintenance Documentation

This document explains the Pipeline organization, route data, terminal grouping, automatic generation mechanism, and integration method for new observation points of the `EnvironmentMonitoring` task.

The core characteristic of environment monitoring is **"Data-Driven + Template Batch Generation"**: The Pipeline JSON corresponding to each observation point is not manually written but batch-rendered into `assets/resource/pipeline/EnvironmentMonitoring/` using the [`@joebao/maa-pipeline-generate`](https://www.npmjs.com/package/@joebao/maa-pipeline-generate) tool, utilizing template/route configurations under `tools/pipeline-generate/EnvironmentMonitoring/` and zmdmap cache data under `tools/pipeline-generate/data/`. The focus of maintenance work is on **generation configuration and data cache**, not manually editing JSON.

> [!WARNING]
>
> Both `assets/resource/pipeline/EnvironmentMonitoring/{Station}/*.json` and `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json` are **generated artifacts**. Manually modifying these files will cause them to be overwritten upon the next regeneration. All maintenance should modify the generation configuration under `tools/pipeline-generate/EnvironmentMonitoring/`, or update the zmdmap cache under `tools/pipeline-generate/data/` via `pnpm fetch:zmdmap`.

## Overview

The core maintenance points for environment monitoring are as follows:

| Module                     | Path                                                                                                            | Purpose                                                                                                                                                                                                                                                    |
| -------------------------- | --------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Task Entry                 | `assets/tasks/EnvironmentMonitoring.json`                                                                       | Interface task definition (no configurable options, controllers = Win32-Front / Wlroots / ADB)                                                                                                                                                             |
| Main Flow Pipeline         | `assets/resource/pipeline/EnvironmentMonitoring.json`                                                           | Main entry node `EnvironmentMonitoringMain`, looping identification of two monitoring terminals                                                                                                                                                             |
| Terminal Grouping (Generated) | `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json`                                                 | Entry nodes and their respective observation point `next` lists for Suburban Monitoring Terminal / Marker Stone Monitoring Terminal (**Generated**)                                                                                                         |
| Terminal Jumps             | `assets/resource/pipeline/EnvironmentMonitoring/Locations.json`                                                 | `EnvironmentMonitoringGoTo*` and `Select*` nodes, entering the corresponding terminal from the main menu                                                                                                                                                   |
| Photo Taking Flow          | `assets/resource/pipeline/EnvironmentMonitoring/TakePhoto.json`                                                 | Entering photo mode, adjusting orientation, identifying the photo button, returning to the terminal after achieving the goal                                                                                                                                |
| Camera Swipe               | `assets/resource/pipeline/EnvironmentMonitoring/TakePhoto.json`                                                 | `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}` four-way orientation adjustment                                                                                                                                                                     |
| Common Buttons             | `assets/resource/pipeline/EnvironmentMonitoring/Button.json`                                                    | Common buttons specific to environment monitoring, such as `TrackMissionButton`                                                                                                                                                                             |
| Observation Point Nodes (Generated) | `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`                                            | **One JSON per observation point**, rendered from templates (**Generated**); `Id` is automatically generated by `data.mjs`, usually no manual writing needed                                                                                               |
| Observation Point Template | `tools/pipeline-generate/EnvironmentMonitoring/generator/template.json`                                         | Single observation point Pipeline template (text recognition, accept/go, teleport, pathfinding, photo taking)                                                                                                                                              |
| Terminal Template          | `tools/pipeline-generate/EnvironmentMonitoring/generator/terminals-template.json`                               | Terminal grouping node template                                                                                                                                                                                                                            |
| Route/Coordinate Data      | `tools/pipeline-generate/EnvironmentMonitoring/routes.json`                                                     | Route overrides matched by observation point `MissionId` (teleport point, map, path, camera swipe direction); `Name` is for human reading only, `Id` is the final template node ID, facilitating search for generated nodes/filenames                       |
| Route JSON Schema          | `tools/schema/environment_monitoring_routes.schema.json`                                                        | Field constraints for `routes.json` (required fields, enums, coordinate array shapes), automatically associated via `.vscode/settings.json`, providing IDE field completion and validation                                                                  |
| Route Sync Logic           | `tools/pipeline-generate/EnvironmentMonitoring/generator/sync-routes.mjs`                                       | Automatically syncs `routes.json` `MissionId` / `Name` / `Id` before generation, and sorts by `MissionId`                                                                                                                                                 |
| Route Parsing Logic        | `tools/pipeline-generate/EnvironmentMonitoring/generator/route-resolver.mjs`                                    | Parses `routes.json` entries into pathfinding recognition/action parameters required by the template, and uniformly handles unadapted degradation                                                                                                          |
| Terminal List Data         | `tools/pipeline-generate/EnvironmentMonitoring/generator/terminals-data.mjs`                                    | Generates each terminal's `next` from the row data of `data.mjs` and the automatically derived terminal list                                                                                                                                               |
| Game Data Snapshot         | `tools/pipeline-generate/data/kite_station_i18n.json`                                                           | Official monitoring terminal/commission data provided by `zmdmap` (multi-language names, `shotTargetName`), cached by `pnpm fetch:zmdmap`                                                                                                                  |
| Generator Configuration    | `tools/pipeline-generate/EnvironmentMonitoring/generator/config.json`                                           | Single observation point output configuration: `outputPattern: "${Station}/${Id}.json"`                                                                                                                                                                    |
| Terminal Generator Config  | `tools/pipeline-generate/EnvironmentMonitoring/generator/terminals-config.json`                                 | Terminal output configuration merged into a single file: `outputFile: "Terminals.json"`                                                                                                                                                                    |
| Multi-language Text        | `assets/locales/interface/*.json`                                                                               | `task.EnvironmentMonitoring.*` label / description (task level; observation point names use OCR)                                                                                                                                                           |
| Common Component Dependencies | `agent/go-service/maptracker/` / `3rdparty/maa-copilot`                                                         | `MapTrackerMove`, `MapTrackerAssertLocation`, `MapLocateAssertLocation`, `MapNavigateAction` (see details in [map-tracker.md](../components/map-tracker.md), [map-locator.md](../components/map-locator.md), [map-navigator.md](../components/map-navigator.md))                     |
| Scene Jump Dependencies    | `assets/resource/pipeline/SceneManager/`、`Interface/`                                                           | `SceneEnterWorldWuling*`, `SceneEnterMenuRegionalDevelopmentWulingEnvironmentMonitoring` (see details in [scene-manager.md](../scene-manager.md))                                                                                                         |

## Main Flow

Environment monitoring runs in the following hierarchical loop:

```text
EnvironmentMonitoringMain
  └─ EnvironmentMonitoringLoop                   (Identify monitoring terminal selection interface)
       ├─ [JumpBack]OutskirtsMonitoringTerminal  (Suburban Monitoring Terminal)
       │    └─ OutskirtsMonitoringTerminalLoop
       │         ├─ [JumpBack]{Id}Job × N        (Traverse all observation points under this terminal)
       │         └─ EnvironmentMonitoringFinish
       ├─ [JumpBack]MarkerStoneMonitoringTerminal(Marker Stone Monitoring Terminal)
       │    └─ MarkerStoneMonitoringTerminalLoop
       │         ├─ [JumpBack]{Id}Job × N
       │         └─ EnvironmentMonitoringFinish
       └─ EnvironmentMonitoringFinish
```

The internal chain for each observation point `{Id}Job` (rendered by `template.json`):

```text
{Id}Job                              (Identify this observation point list item)
  ├─ Accept{Id}                      (Commission available → Click to accept)
  └─ GoTo{Id}Mission                 (Commission accepted → Click to go)
       └─ {Id}TrackOrGoTo
            ├─ Track{Id}             (Click "Start Tracking" button if it exists)
            │    ├─ {Id}NotAdapted   (Route not adapted → Only prompt and end this observation point)
            │    └─ GoTo{Id}         (Route adapted → Continue to go)
            └─ AlreadyTracked{Id}    (Already tracking)
                 ├─ {Id}NotAdapted   (Route not adapted → Only prompt and end this observation point)
                 └─ GoTo{Id}         (Route adapted → Continue to go)
                      ├─ GoTo{Id}StartPos (MapTrackerAssertLocation / MapLocateAssertLocation in position → MapTrackerMove / MapNavigateAction)
                      └─ GoTo{Id}NotAtStartPos
                           └─ SubTask: ${EnterMap}            (Teleport)
                                └─ GoTo{Id}StartPos           (Check if already near the mission start position)
                                     └─ GoTo{Id}MapTrackerMove
                                          ├─ anchor: EnvironmentMonitoringBackToTerminal → ${GoToMonitoringTerminal}
                                          ├─ anchor: EnvironmentMonitoringAdjustCamera   → ${Id}AdjustCamera
                                          └─ next:   EnvironmentMonitoringTakePhoto
EnvironmentMonitoringTakePhoto       (Enter photo mode → Orient → Take photo)
  └─ [Anchor]EnvironmentMonitoringBackToTerminal
       └─ EnvironmentMonitoringGoTo{Outskirts|MarkerStone}MonitoringTerminal
```

> [!NOTE]
>
> The two keys of the `anchor` field are hardcoded placeholder names in the template, replaced at runtime with:
>
> - `EnvironmentMonitoringBackToTerminal` → The `EnvironmentMonitoringGoTo{Station}` node of the terminal to which the current observation point belongs (return to the correct terminal after taking the photo)
> - `EnvironmentMonitoringAdjustCamera` → `{Id}AdjustCamera` (Execute the camera swipe direction for this observation point)

## Naming Rules

### Observation Point Node ID (`Id`, Auto-generated)

`Id` is a generation field assembled by `data.mjs`, equivalent to the prefix for all observation point node names and output filenames:

```text
{PascalCase English Name}
```

For example:

```text
WaterTemperatureController        → Water temperature control device
EcologyNearTheFieldLogisticsDepot → Ecology around the field logistics depot
MysteriousCryptidGraffiti         → Mysterious cryptid graffiti
```

By default, `Id` is derived from the `name["en-US"]` of that task in `kite_station_i18n.json`, converted to PascalCase. The rules are in `buildDefaultId()` / `toPascalCase()` in `common.mjs`. If the English name is missing, it falls back to `missionId` / `entrustIdx`; if duplicates occur, `ensureUniqueId()` automatically appends a suffix.

When maintaining `routes.json`, you do not need to manually calculate `Id`. The route matching key is `MissionId`, and `Id` is automatically written back to `routes.json` during regeneration, equivalent to the node name prefix used by the final template, facilitating direct search for generated nodes and filenames.

> [!IMPORTANT]
>
> Do not use `Id` as display text. Display text uses zmdmap names / OCR; `Name` is the human-readable note in routes.json, and `Id` is only used for concatenating node names, filenames (`outputPattern: "${Station}/${Id}.json"`), and is automatically refreshed by the generator.

### Terminal Grouping (`Station`)

Derived by `buildStationName()` in `data.mjs` from `mission.kiteStation` (or fallback to `__terminalId`) mapped to `kite_station_i18n.json[terminalId].level.name["en-US"]` and converted to PascalCase. Currently, the repository contains only two groups:

| Chinese Name            | Station ID                      | Corresponding terminalId | `GoToMonitoringTerminal` Anchor                              |
| ----------------------- | ------------------------------- | ------------------------ | ------------------------------------------------------------ |
| Suburban Monitoring Terminal | `OutskirtsMonitoringTerminal`   | `kitestation_002_1`      | `EnvironmentMonitoringGoToOutskirtsMonitoringTerminal`       |
| Marker Stone Monitoring Terminal | `MarkerStoneMonitoringTerminal` | `kitestation_004_1`      | `EnvironmentMonitoringGoToMarkerStoneMonitoringTerminal`     |

If a new Station appears, **the generator side (`routes.json` + `data.mjs`) requires zero changes**: `MONITORING_TERMINAL_IDS` is automatically derived from `kite_station_i18n.json`, and the `GoToMonitoringTerminal` anchor name is concatenated using the `EnvironmentMonitoringGoTo{Station}` template. However, the following **hand-written linkage nodes** referenced by the generated Pipeline must be completed first; otherwise, MaaFramework runtime will report "referencing undefined task":

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

The default export of `data.mjs` is an array, where each element = the rendering context for one observation point (field names correspond to `${Xxx}` placeholders in `template.json`). `pnpm generate:EnvironmentMonitoring` first calls `sync-routes.mjs` to refresh the parent `routes.json`; subsequently, `data.mjs` only reads `routes.json` and `kite_station_i18n.json`, and assembles the final row via `route-resolver.mjs`:

| Field                                             | Source                                                                                                                                                                                                                          |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Station`                                         | English station name from `kite_station_i18n.json` (PascalCase)                                                                                                                                                                 |
| `Id`                                              | Auto-generated by default from the official English name in PascalCase; synchronized back to `routes.json`, equivalent to the node ID used by the final template                                                               |
| `MissionId` / `Name`                              | `MissionId` is the primary matching key in `routes.json`; `Name` comes from the Chinese name in `kite_station_i18n.json`, for human reading only                                                                               |
| `GoToMonitoringTerminal`                          | Determined by `Station`                                                                                                                                                                                                         |
| `EnterMap`                                        | `routes.json[*].EnterMap`, **must be a node name existing in SceneManager**                                                                                                                                                     |
| `MapName` / `MapAssert` / `MapPath` / `MapTarget` | `routes.json[*]`, corresponding to landing point verification and subsequent pathfinding parameters; `MapPath` generates `MapTrackerAssertLocation` + `MapTrackerMove`, `MapTarget` generates `MapLocateAssertLocation` + `MapNavigateAction` with `ZONE` declaration plus target coordinate point, one of the two must and only be chosen |
| `CameraSwipeDirection`                            | `routes.json[*]`, must be one of `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}`                                                                                                                                        |
| `CameraMaxHit`                                    | `routes.json[*].CameraMaxHit`, defaults to `2`; corresponds to the maximum hit count for the `${Id}AdjustCamera` swipe action                                                                                                  |
| `ExpectedText`                                    | Automatically expanded from the `mission.name` multi-language map in `kite_station_i18n.json` (5 languages, English converted to flexible regex)                                                                                |
| `InExpectedText`                                  | Automatically expanded from `mission.shotTargetName` in `kite_station_i18n.json`                                                                                                                                               |
| `TrackOrGoToNext` / `AfterTrackedNext`            | Automatically determined by `data.mjs` based on whether the route is complete: `TrackOrGoToNext` converges to `Track${Id}` / `AlreadyTracked${Id}`, `AfterTrackedNext` is `GoTo${Id}` when adapted, `${Id}NotAdapted` when not |

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

`terminals-data.mjs` scans all rows assembled by `data.mjs`, groups them by `Station`, chains each observation point's `[JumpBack]{Id}Job` to the `next` list of the corresponding terminal, and finishes with `EnvironmentMonitoringFinish`.

### Run Commands

```bash
# Recommended: Run from the repository root
pnpm generate:EnvironmentMonitoring

# Only update the zmdmap cache
pnpm fetch:zmdmap

# If the zmdmap cache has already been updated, you can also render individually in the tools/pipeline-generate/EnvironmentMonitoring/generator/ directory:

# 0) Sync routes.json MissionId/Name/Id
node sync-routes.mjs

# 1) Render all observation point Pipelines
npx @joebao/maa-pipeline-generate

# 2) Render terminal entry
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

> [!NOTE]
>
> If `data.mjs` finds that an observation point has no entry in `routes.json` during rendering, or the entry exists but any required field is missing (`null` / empty string / empty array), it will `console.warn` and treat that observation point as **unadapted**. Unadapted observation points will still generate a Pipeline, but at runtime they will only accept and track the task, ending after the `${Id}NotAdapted` prompt, without executing teleportation or pathfinding.

## Key Dependencies

### Pathfinding Components

The "Teleport → Verify → Pathfind" three stages for observation points combine MapTracker and MapNavigator:

- `MapTrackerAssertLocation` / `MapLocateAssertLocation` (Recognition): Determines if currently within the `MapAssert` rectangle based on the minimap. `MapTrackerAssertLocation` is generated when using `MapPath`, `MapLocateAssertLocation` when using `MapTarget`.
- `MapTrackerMove` / `MapNavigateAction` (Action): Walks to the target point along the `MapPath` path, or generates a `ZONE` declaration plus target coordinate point according to `MapTarget` and goes to the target; supports the anchor mechanism to rewrite `EnvironmentMonitoringBackToTerminal` / `EnvironmentMonitoringAdjustCamera` during the process.

Detailed parameters and coordinate recording methods are in [map-tracker.md](../components/map-tracker.md), [map-locator.md](../components/map-locator.md) and [map-navigator.md](../components/map-navigator.md).

### SceneManager

The `EnterMap` field must be filled with an existing teleport node name from SceneManager, e.g., `SceneEnterWorldWulingJingyuValley7`. If a new observation point is located at an unsupported teleport point, you need to first complete the corresponding `SceneEnterWorld*` and scene recognition nodes under `assets/resource/pipeline/SceneManager/` and `assets/resource/pipeline/Interface/` (refer to [scene-manager.md](../scene-manager.md)).

`data.mjs` determines whether to enter the pathfinding/photo-taking flow based on whether the `routes.json` entry is complete. Unadapted points will directly go to the `${Id}NotAdapted` branch. To make an observation point fully automated, you must provide all required fields in `routes.json`: `EnterMap` (the actual `SceneEnterWorld*` node) / `MapName` / `MapAssert` / `CameraSwipeDirection`, and choose one between `MapPath` and `MapTarget`; when a usable teleport point is temporarily unavailable, you can omit the entry and let it run in the degraded "accept and track only" flow.

### Main Menu Entry

The main entry node for environment monitoring, `EnvironmentMonitoringMain`, enters the terminal selection interface via `[JumpBack]SceneEnterMenuRegionalDevelopmentWulingEnvironmentMonitoring`. This node is maintained in `assets/resource/pipeline/Interface/InScene/Region.json`. When adding a new regional monitoring terminal, you need to confirm that the main menu entry can successfully reach the corresponding interface.

## Adding New Observation Points

New observation points typically come from game updates, reflected as an additional `mission` in `kite_station_i18n.json`. The maintenance process:

> [!TIP]
>
> If you are using a client supporting AI Skills (like Claude Code or GitHub Copilot), you can directly invoke the **`environment-monitoring-add-route` skill**. It will automatically detect missing entries and help you fill in `routes.json` through interactive Q&A, saving the manual table lookup steps.

### 1. Update Game Data

Run `pnpm fetch:zmdmap`. It will download and cache the latest `tools/pipeline-generate/data/kite_station_i18n.json` from the zmdmap API.

### 2. Check Route Adaptation Status

Compare the `entrustTasks` in `kite_station_i18n.json` with the entries in `routes.json` to confirm the status of each observation point. The matching method is `missionId` against `MissionId` in `routes.json`, not `Name` or `Id`:

- **Unadapted**: `routes.json` does not have this observation point, or the entry exists but is missing any required field (including `null` / empty string / empty array) → Will only accept and track after generation.
- **Ready to Adapt**: Need to make this observation point automatically go and take photos → Proceed to step 3 to complete the real route.

> [!IMPORTANT]
>
> If you do not plan to adapt a certain observation point, simply do not add the entry in `routes.json`; do not write placeholder values like `"SceneAnyEnterWorld"` / `[0,0,1,1]`.

### 3. Add/Complete Entry in `routes.json`

`tools/pipeline-generate/EnvironmentMonitoring/routes.json`:

```jsonc
{
    "MissionId": "m1m30",                    // Must match the missionId in kite_station_i18n.json
    "Name": "My New Observation Point",      // Chinese name, for human reading only
    "Id": "MyNewObservationPoint",           // Final template node ID, for human search of node/filename only
    "EnterMap": "SceneEnterWorldWulingXxx", // Teleport node existing in SceneManager
    "MapName": "map02_lv001",               // Map identifier: MapPath uses MapTracker map_name, MapTarget uses MapLocate zone_id
    "MapAssert": [x, y, w, h],              // Target rectangle (minimap coordinates)
    "MapPath": [[x1, y1], [x2, y2]],        // Pathfinding path (minimap coordinates), choose one with MapTarget
    // "MapTarget": [x, y],             // MapNavigateAction target point, generates ZONE declaration when path is generated
    "CameraSwipeDirection": "EnvironmentMonitoringSwipeScreenUp" // Orientation adjustment direction
    // "CameraMaxHit": 2,  // Optional; maximum swipe hit count, default 2; can be appropriately increased if the photo target is hard to align
}
```

> [!IMPORTANT]
>
> `routes.json` is strict JSON: double quotes, no inline comments allowed, no trailing commas allowed. The `//` in the code block above is only for documentation; writing it into the actual file will cause JSON parsing to fail.
>
> `MissionId` is the matching key for `data.mjs`, precisely matching `missionId` in `kite_station_i18n.json`. `Name` is for human reading only, `Id` is for human search of generated node/filename only; if inconsistent with current zmdmap data, the generator will directly refresh it to the current correct value, not affecting matching.

> When regenerating EnvironmentMonitoring, `sync-routes.mjs` will first automatically refresh `MissionId` / `Name` / `Id` based on zmdmap data, and sort by `MissionId`. When writing entries manually, you must fill in `MissionId`; if a new task exists in zmdmap but `routes.json` has no corresponding entry, the generator will automatically append an unadapted placeholder entry containing only `MissionId` / `Name` / `Id` for maintainers to see pending routes.

### 4. Record Coordinates and Paths

Refer to the GUI tool in [map-navigator.md](../components/map-navigator.md) to record `MapAssert` / `MapPath`, or copy the MapNavigateAction target point into `MapTarget`, and confirm in-game:

- `MapName` matches the tool used: For `MapPath` routes, fill in MapTracker's `map_name` (e.g., `map02_lv001` / regex); for `MapTarget` routes, fill in MapLocate / MapNavigator's `zone_id` (e.g., `Wuling_Base`). Do not mix the two sets of identifiers.
- Which direction the camera needs to swipe when taking photos (determines `CameraSwipeDirection`).
- Whether the position allows `EnvironmentMonitoringTakePhoto` to succeed via `EnvironmentMonitoringEnterCameraMode` (auto-orienting to the target); if not, it will automatically fallback to `EnvironmentMonitoringTakePhotoDirectly` + manual swipe `${Id}AdjustCamera`.

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

Confirm the two types of generated files:

- `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`
- `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json` (`{Station}MonitoringTerminalLoop.next` contains `[JumpBack]{Id}Job`)

Here `{Id}` is the node ID in the generation result. Usually you can confirm directly by looking at the generated filename; no need to manually calculate in advance when maintaining `routes.json`.

## Modifying Existing Observation Point Routes

Only adjusting the route/orientation (without changing the English name):

1. Modify the corresponding entry in `tools/pipeline-generate/EnvironmentMonitoring/routes.json`.
2. Regenerate. Normally, you can directly run `pnpm generate:EnvironmentMonitoring` from the repository root; if you are sure the terminal list has not changed, you can also just run `node sync-routes.mjs && npx @joebao/maa-pipeline-generate` in the `tools/pipeline-generate/EnvironmentMonitoring/generator/` directory without regenerating `Terminals.json`.
3. Commit `routes.json` and the regenerated `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`.

If the official English name of the observation point changes, the generated `Id` / filename will also change; after regeneration, the `Id` in `routes.json` will synchronize to the new final template ID.

## Self-Check Checklist

Before submitting, at least check:

1. Whether the newly added/modified entries in `tools/pipeline-generate/EnvironmentMonitoring/routes.json` have all fields complete.
2. Whether the `MissionId` of new entries in `routes.json` can match `missionId` in `kite_station_i18n.json`; `Id` is automatically refreshed by the generator.
3. Whether the `EnterMap`, `MapAssert`, `CameraSwipeDirection` of adapted entries have all been filled with real values, and `MapPath` / `MapTarget` have been filled by choosing one.
4. Whether each `{Station}MonitoringTerminalLoop.next` in the regenerated `Terminals.json` contains all new `[JumpBack]{Id}Job`, and ends with `EnvironmentMonitoringFinish`.
5. Whether the `Scene*` nodes referenced by `EnterMap` actually exist in `assets/resource/pipeline/SceneManager/` and `Interface/`.
6. Whether `CameraSwipeDirection` is one of `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}`.
7. **Did not manually edit** `assets/resource/pipeline/EnvironmentMonitoring/{Station}/*.json` or `Terminals.json` (manual edits will be overwritten by the next generation; if special nodes are truly needed, they should be extended in `template.json` / `terminals-template.json`).
8. JSON files follow `.prettierrc` format (generator has `format: true`, but running `pnpm prettier --write` once before submission is more stable).

## Common Pitfalls

- **Manually editing generated artifacts**: Directly editing `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json` or `Terminals.json` will cause changes to be lost upon the next regeneration. The correct approach is to modify the generation configuration / update the zmdmap cache and then regenerate.
- **`MissionId` does not match game data**: The `MissionId` in the `routes.json` entry is the matching primary key; `Name` / `Id` are only for human reading and searching. When `MissionId` matching fails, the generator will prompt that the entry is unused, and the corresponding observation point will be treated as unadapted (accept and track only).
- **Using `Id` as the matching key**: `Id` is only the final template node ID, facilitating search for generated nodes/filenames; matching still only looks at `MissionId`.
- **`Id` drifts with `kite_station_i18n.json` English name**: When the game side changes the English name, the automatically calculated `Id` will change, potentially causing generated file renaming or residual old files; after regeneration, the `Id` in `routes.json` will synchronize.
- **`EnterMap` references a non-existent Scene node**: Generation itself does not validate Scene references; at runtime, it will get stuck in an infinite loop at `GoTo{Id}NotAtStartPos`.
- **`MapPath` / `MapTarget` passes through unlocked areas / combat / interactive objects**: MapTracker and MapNavigateAction do not handle combat, story sequences, map transitions, or mechanism interactions; paths can only be on purely passable sections.
- **`Station` added but `Locations.json` / `EnvironmentMonitoringLoop.next` not synchronized**: New terminals cannot be recognized or entered, and none of their observation points will run.
- **`anchor` placeholder name consistency**: The key name `EnvironmentMonitoringBackToTerminal` for `anchor` in `template.json` must exactly match `[Anchor]EnvironmentMonitoringBackToTerminal` in `TakePhoto.json`; otherwise, the anchor mechanism will fail.
- **"Generation success ≠ fully adapted"**: Observation points without a `routes.json` entry, or with an entry but missing required fields, will generate a degraded flow that only accepts and tracks, without going to take photos. Full automation requires completing the real `EnterMap`, `MapName`, `MapAssert`, `CameraSwipeDirection`, and choosing one between `MapPath` and `MapTarget`.
