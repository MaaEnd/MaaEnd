# Development Manual - MapNavigator Path Navigation System

## Introduction

This document explains how to use the **MapNavigator** related nodes and how to leverage the built-in GUI tool in the repository to record, edit, and export navigation paths that can be directly used in a Pipeline.

**MapNavigator** is MaaEnd's current high-precision automatic navigation Action module. It relies on the underlying MapLocator's continuous localization to obtain the character's current area, global coordinates, and orientation. Then, it drives the character to move to the target location, executing actions like sprinting, jumping, interaction, and map transition at key points.

### Two Workflows

MapNavigator offers two ways to specify "where to go". **Determining which one your scenario belongs to first can save a great deal of work**:

| Workflow                     | What you need to provide   | Applicable scenario                                                                                         |
| ---------------------------- | -------------------------- | ----------------------------------------------------------------------------------------------------------- |
| **Target-based pathfinding** | A single target coordinate | The target point is inherently walkable, with no special mechanisms along the way                           |
| **Recorded path**            | The full waypoint sequence | The route involves interactions, map transitions, jump platforms, external teleports, and similar semantics |

#### Target-based: `NAVMESH`

As long as the target point is inherently reachable without interactions, map transitions, or special mechanisms, **filling in a single `target` is enough**:

```json
{
    "GotoTarget": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapNavigateAction",
        "custom_action_param": {
            "path": [
                {
                    "action": "NAVMESH",
                    "target": [
                        720,
                        630
                    ]
                }
            ]
        }
    }
}
```

No pre-recording of the route, no intermediate points, and no `zone_id` are needed — at runtime the area is inferred from the current localization, and an executable path is planned on the BaseNav triangle graph. When the target point is on a layered map, just add one more [`target_tier`](#cross-tier-target-target_tier) field.

This form is already used in production routes such as Auto Collect and Environment Monitoring.

#### Recorded: `path`

When the route itself contains semantics the navigator cannot infer on its own — interacting at a certain point, passing through a map transition point, using a jump platform, or waiting for an external teleport — you need a recorded path. The accompanying GUI tool `/tools/MapNavigator` lets you walk the route once in-game to record it automatically, then fine-tune it, add actions, and copy it with one click.

The two forms can be mixed within the same `path` array: use `NAVMESH` to cover long stretches of plain travel, and coordinate points to handle the local segments that need precise semantics.

> [!NOTE]
>
> The GUI's A\* preview and the runtime pathfinder read the same `base.nav.gz` and follow the same planning logic, so the route previewed in the GUI matches the route the runtime plans, and you can judge from the preview whether a target point can be reached. Note that this refers to the **planning result** being consistent; actual execution is still affected by localization, terrain, and in-game conditions, so the outer Pipeline still needs failure fallback.

### Boundary Description

MapNavigator is responsible for "**stably leading the person there once the target is given**", belonging to the Action layer.

- It **does not** handle business process orchestration. Decisions like when to start walking, what constitutes success upon arrival, or how to handle unexpected situations en route should still be determined by the outer Pipeline.
- It **does not** infer business semantics. Plain-travel paths can be planned automatically by `NAVMESH`, but semantics like interaction, map transition, jump platforms, and external teleports must be explicitly annotated by the developer in `path`.
- It **does not** make judgments like "should this path be taken this time". For such entry condition judgments, it is recommended to first confirm using recognition or scene nodes before entering the navigation action.

### Relationship Between MapNavigator and Recording Tool

A dedicated GUI tool is provided within the repository: `/tools/MapNavigator`.

Its design goal is very direct:

1. Start the game and open the tool.
2. Click to start recording directly.
3. Walk through the actual path in the game.
4. After stopping the recording, fine-tune, delete points, and add actions in the GUI.
5. Click to copy, and paste the exported `path` into the Pipeline's `custom_action_param.path`.

This means that **recording a route does not require manually writing coordinates**: walk it once, fine-tune, and paste. What mainly needs recording are routes containing interactions, map transitions, and mechanisms; for stretches of plain travel, giving `NAVMESH` a target coordinate skips recording entirely (see [Two Workflows](#two-workflows)).

---

## Node Description

Below is a detailed introduction to the node usage provided by MapNavigator. The current interface is based on MAA `Custom` Action: `MapNavigateAction`.

### custom_action: MapNavigateAction

Controls the character to move automatically along a given path and execute additional actions at waypoints.

#### Node Parameters

**Required Parameters (at least provide `path` recommended)**:

- `path`: List of path points. MapNavigator will consume these nodes sequentially and navigate continuously until the path ends or fails midway.

**Common Optional Parameters (`custom_action_param`)**:

- `map_name`: String, empty by default. Used as the initial area context. If the `path` already contains a `ZONE` declaration node, this usually does not need to be filled additionally.
- `arrival_timeout`: Positive integer, `60000` by default. Maximum allowed time in milliseconds for a single target point to remain unreached before being considered failed.
- `sprint_threshold`: Positive real number, `25.0` by default. The "length of continuously runnable segment ahead" threshold used for automatic sprint judgment, rather than just looking at the straight-line distance to the current point.
- Other unknown top-level fields: Currently ignored silently without causing errors.

#### `path` Data Structure

`path` is essentially an array where each element represents a "path node". Typically, you don't need to manually write this content; using the accompanying GUI tool `/tools/MapNavigator` for orchestration is more recommended. Common usage is as follows.

##### **1. Most Common Coordinate Point**

```json
[
    688,
    350
]
```

Represents an ordinary movement point, meaning proceeding to the next point upon reaching this coordinate.

##### **2. Coordinate Point with Action**

```json
[
    720,
    350,
    "SPRINT"
]
```

Indicates that a `SPRINT` action should be executed upon reaching this point. Current common actions include:

- `RUN`: Ordinary movement point.
- `SPRINT`: Perform a sprint once upon arrival.
- `JUMP`: Jump upon arrival.
- `FIGHT`: Attack once upon arrival.
- `INTERACT`: Interact upon arrival.
- `TRANSFER`: Stop upon arrival, wait for external force to move the character to the next path segment, then continue navigation from subsequent points.
- `PORTAL`: Cross-area transition point, upon triggering, enter blind walk to wait for area switch.
- `HEADING`: Adjust the camera to a specified orientation, then press `W` once.
- `COLLECT`: Collection point, upon precise arrival, synchronously trigger AutoCollect OCR + click, without exiting NaviController. See [Collection Semantics](#collection-semantics-collect--dig).
- `DIG`: Digging point, same as `COLLECT`, but triggers a digging subtask. See [Collection Semantics](#collection-semantics-collect--dig).

##### **3. Strict Arrival Point**

```json
[
    700,
    350,
    "INTERACT",
    true
]
```

The trailing `true` enables strict arrival for this point. For certain interactions, jumps, teleports, or map transitions that genuinely require strict arrival at key points, it is recommended to use strict arrival or directly use the corresponding action point. This is because the underlying system already processes these critical actions with stricter arrival semantics (slower arrival, stricter confirmation of arrival radius threshold).

##### **4. Zone Declaration Node**

```json
{
    "action": "ZONE",
    "zone_id": "Wuling_Base"
}
```

This is a **non-coordinate control node** used to declare "which area the subsequent path should be in". It itself does not execute displacement but only provides area **verification** context for subsequent path points.

##### **5. Orientation Control Node `HEADING`**

```json
{
    "action": "HEADING",
    "angle": 90
}
```

Or:

```json
{
    "action": "HEADING",
    "target": [
        688,
        350
    ]
}
```

Non-coordinate node. During execution, after adjusting the camera orientation, lightly press `W` once to advance and make the orientation take effect. `angle` specifies the orientation angle directly; `target` calculates the orientation based on "current position -> target coordinates" and then reuses the same `HEADING` action flow.

##### **6. BaseNav Semantic Node `NAVMESH`**

```json
{
    "action": "NAVMESH",
    "target": [
        720,
        630
    ]
}
```

This is a **BaseNav semantic pathfinding node**. It does not carry `zone_id`, `navmesh_zone`, or `path`; it only provides the target point `target`. The remaining information is automatically inferred at runtime based on current localization.

The operational flow of `NAVMESH` is:

1. At runtime, prioritize loading `assets/resource/model/map/navmesh/base.nav.gz`; fall back to `base.nav` if it doesn't exist.
2. Infer the BaseNav zone based on the current localization area.
3. Snap the landing point according to the current floor height, then execute A\* on the `.nav` triangle graph, only traversing BaseNav's own edges.
4. Expand the planning result into ordinary `RUN` waypoints, which are then handed over to the movement execution chain.

Step 3's floor snapping is worth calling out separately: on multi-floor maps, triangle faces from several floors are stacked at the same planar coordinate. Without distinguishing height, the start or end point may be snapped to the wrong floor, which shows up as the character clipping through walls or the path being unreachable. BaseNav bakes the floor height for each zone into the data pack and filters candidate faces by height band during planning, so the landing point in multi-floor areas is deterministic.

In the GUI, clicking `Load BaseNav` makes the tool enter the same BaseNav preview logic; clicking `Copy NAVMESH` copies this type of node to the clipboard.
`NAVMESH` is suitable for scenarios that require "automatically finding a triangle graph path from the current position to the target point" without needing to manually record an entire path segment beforehand.

**As long as the original path is inherently reachable without interactions, map transitions, or special mechanisms, `NAVMESH` only needs a `target` to directly lead the character to the target location**. No pre-recording of the entire route is needed, nor is there a need to add intermediate points, adjust coordinates, or manually splice the path for this target point. In the GUI, simply click out the target, and at runtime, an executable path will be planned directly based on the BaseNav triangle graph.

###### Cross-tier Target: `target_tier`

When `target_tier` is not specified, `target` defaults to **base (base map) coordinates**—this is the default behavior described above, with no change in functionality.

When the target point is on a specific **tier (layered map)**, each tier is a **mutually independent coordinate system**: the same numbers `[123, 456]` on the base and on a tier are completely different physical locations. In this case, simply add a `target_tier` field to the node, declaring which layer's coordinate system the `target` is filled according to:

```json
{
    "action": "NAVMESH",
    "target": [
        81.77,
        108.72
    ],
    "target_tier": "ValleyIV_L1_171"
}
```

- `target`: The coordinate **directly clicked out in the GUI after switching to that tier's base map**, without needing to manually convert it to base coordinates.
- `target_tier`: The **area name** of that layer, i.e., the name part after `:` in the `id:name` of the tier dropdown in the GUI.
- At runtime, the affine transformation baked into the `.nav` for that tier is used to automatically project `target` back to the base coordinate system (using the same mirroring logic as automatic normalization of the starting point localization), and snap the landing point according to that tier's floor height.
- This is the only thing needed to go to a tier: **a single node with `target` + `target_tier` is enough**. No additional `ZONE` node is needed, no intermediate points need to be added, and no manual coordinate adjustment is required.
- The field also supports camelCase writing `targetTier`; filling in a non-existent layer name will be logged as a warning and treated as base coordinates.

#### Return Behavior

`MapNavigateAction` is an Action node; it does not have a stable structured recognition output like Recognition. Its results are mainly reflected as:

- If navigation successfully completes the entire path, the current Action returns success.
- If a quick failure condition is triggered during the process (continuous lack of progress timeout / continuous deviation timeout), the current Action returns failure.

Therefore, in a Pipeline, it is generally regarded as an atomic action that either **completes the entire path or the entire node fails**.

#### Example Usage

Below is the most common usage pattern; simply paste the `path` copied from the recording tool:

```json
{
    "DebugNavi": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapNavigateAction",
        "custom_action_param": {
            "path": [
                {
                    "action": "ZONE",
                    "zone_id": "Wuling_Base"
                },
                [
                    405,
                    1592
                ],
                [
                    400,
                    1583
                ],
                [
                    380,
                    1567,
                    "SPRINT"
                ],
                [
                    331,
                    1578,
                    true
                ]
            ]
        }
    }
}
```

```json
{
    "MyNavigateNode": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapNavigateAction",
        "custom_action_param": {
            "arrival_timeout": 45000,
            "path": [
                {
                    "action": "ZONE",
                    "zone_id": "Wuling_Base"
                },
                [
                    405,
                    1592
                ],
                [
                    331,
                    1578,
                    "INTERACT",
                    true
                ]
            ]
        }
    }
}
```

> [!TIP]
>
> In actual development, it is recommended to use `MapNavigateAction` after a node that has confirmed the entry state. First confirm that the character is indeed in the expected scene, area, and near the expected orientation before starting the entire navigation, which will significantly increase the success rate.

> [!WARNING]
>
> Path points should preferably satisfy the requirement of "being able to move coherently to the next point". Do not expect the navigator to pass through models, circumvent particularly complex obstacles, or automatically understand business mechanisms. Special segments like map transitions, jump platforms, falls, and ascent mechanisms should be explicitly split into `PORTAL` / `TRANSFER` / business node combinations for handling.

---

## Tool Description

We provide a dedicated GUI tool for MapNavigator, located at `/tools/MapNavigator`, with the entry point being `main.py`.

It supports:

1. Direct connection to the current game window to record actual movement trajectories.
2. Automatic addition of `ZONE` / `PORTAL` semantics based on area transitions.
3. Deleting points, dragging points, changing coordinate point actions, and modifying strict arrival in the GUI.
4. Importing existing JSON / JSONC, recursively searching for recognizable `path` data and continuing editing.
5. One-click copying of canonical `path` that can be directly pasted into `custom_action_param.path`.
6. Through an independent `Assert mode` to manually select the base map and frame rectangular areas, exporting `MapLocateAssertLocation` nodes.
7. Entering BaseNav A\* mode, loading `.nav.gz` / `.nav`, previewing paths on the red triangle face overlay, and copying `NAVMESH` nodes.

An additional note is that the current GUI editor primarily round-trips path points with coordinates and `ZONE` declarations derived from area information.
Non-coordinate control nodes like `HEADING` and semantic pathfinding nodes like `NAVMESH` are not regular point editing objects in the GUI. It is recommended to manually add back or maintain `HEADING` after exporting the `path`, while `NAVMESH` can be directly generated using `Copy NAVMESH`.

### Running Method

#### 1) Standard Python

```powershell
cd tools\MapNavigator
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements.txt
python main.py
```

#### 2) uv

```powershell
cd tools\MapNavigator
uv run main.py
```

### Pre-run Preparation

Before starting to record, please confirm:

1. The project development environment has been configured according to the development manual, especially that `install/agent/cpp-algo.exe` and `install/maafw` are usable.
2. The Python dependencies `maafw`, `Pillow`, and `pynput` are installed.
3. **Windows**: The tool needs to be run with **administrator privileges**; otherwise, the G/X hotkeys may not be captured by the system when the game (an administrator process) is in the foreground. `main.py` will automatically detect this and prompt a UAC elevation request at startup.
4. **macOS**: On the first run, you need to authorize the current terminal or Python interpreter in **System Settings → Privacy & Security → Input Monitoring**, otherwise global hotkeys will not work.
5. If using `Win32` connection, the game is already started, and the window is **not minimized**.
6. If using `ADB` connection, `adb` is available, and the target emulator/device appears in the device list.
7. The current character is standing near the starting point of the route you want to record.

### Recommended Workflow

The following flow is the complete usage for recorded paths. If the target point is reachable by plain travel alone, giving `NAVMESH` a coordinate is enough and you do not need this flow (see [Two Workflows](#two-workflows)).

#### Step 1: Open the Tool and Start Recording

After running `tools/MapNavigator/main.py`, first select the controller to be used for this recording in the top `Connection` area, then click **`Start Recording`** in the upper-left corner of the GUI.

- When recording the PC version, select `Win32 Window`, modifying the window title if necessary.
- When recording an emulator/real device, select `ADB Device`, configure the `adb` path, refresh the device list, and select the target.

The tool will automatically:

1. Launch the local Agent.
2. Establish a controller based on the selected connection method.
3. Call the underlying localization recognition to continuously read the current coordinates and area.
4. Sample your actual walked route into a raw trajectory.

If the current environment is incomplete, the Win32 window is not found, or the ADB device is not connected, the tool will report an error directly without generating an invalid trajectory.

#### Step 2: Switch Back to the Game and Walk Through Manually

After recording starts, switch back to the game and simply **walk through once as you wish the character to automatically execute in the future**.

During recording, you can use the following shortcut keys:

| Shortcut Key | Function                                                                                                                        |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------- |
| `G`          | 📋 **Copy the current coordinates to the clipboard** in `[x, y]` format (does not affect recorded data, can be pressed anytime) |
| `X`          | 📌 **Force insert a strict arrival (strict) path point** at the current precise location into the recorded data                 |

> [!TIP]
>
> The `G` key is used to quickly record coordinates of interest without interrupting the recording process. The `X` key is used to mark key locations (interaction points, map transition points, etc.) to ensure that the coordinate is definitely recorded and marked as a strict arrival point.

It should be noted that points with stronger business semantics like `FIGHT`, `TRANSFER`, `HEADING` **will not be automatically determined during the recording phase**. The usual practice is to manually change the corresponding point to the target action in the GUI after stopping the recording.

Therefore, the simplest usage is:

1. Click start recording.
2. Go run the map normally in the game.
3. Press `X` at key locations to force mark points (e.g., interaction trigger points, jump platform landing points).
4. Come back and click stop after finishing.

#### Step 3: Stop Recording and Observe the Automatically Organized Results

After clicking **`Stop Recording`**, the tool will perform a round of organization on the raw trajectory, including:

- Unifying the canonical format of coordinates, actions, `strict`, and `zone`.
- Automatically adding `PORTAL` semantics at cross-area boundaries.
- Splitting the view by current area for browsing.

What you see is a navigation route that has been normalized and can be further edited and exported.

#### Step 4: Orchestrate the Path in the GUI

Next, directly handle the details in the GUI.

**View Operations:**

- Mouse wheel: Zoom in/out.
- Right mouse button drag: Pan the view.
- Left mouse button click on empty space: Insert a new point.
- Left mouse button click on an existing point: Select that point.
- Left mouse button drag on an existing point: Fine-tune coordinates.

**Area Switching:**

- The top `◀ / ▶` buttons are used to switch between different areas for viewing.
- If the route crosses areas, the tool will display each area separately for easy inspection of whether the area transitions are reasonable.

**Point Property Editing:**

- The top action dropdown can set the action for the current point.
- `Set Single`: Change the current point's action to the selected action in the dropdown.
- `Append`: Append an action semantic after the current point.
- `Undo One`: Remove the last action in the current point's action chain.
- `Strict`: Mark the current point as a strict arrival point.
- `🗑`: Delete the currently selected point.

The current action dropdown targets coordinate point actions, commonly edited to `RUN / SPRINT / JUMP / FIGHT / INTERACT / PORTAL / TRANSFER / COLLECT / DIG`.
Non-coordinate control nodes like `HEADING` are not part of this GUI action chain.

**Undo/Redo:**

- `Ctrl+Z`: Undo.
- `Ctrl+Y`: Redo.
- `C`: Copy the coordinates of the currently selected point to the clipboard (format is `[x, y]`, supports copying multiple selected points line by line).

Usually, the only fine-tuning you really need to do is:

1. Change key interaction points to `INTERACT` and check `Strict` (points recorded with the X key are already strict arrival by default).
2. Change points that require jumping, sprinting, external teleportation, or map transitions to the corresponding action (e.g., `JUMP` / `SPRINT` / `TRANSFER` / `PORTAL`).
3. Check whether the two points before and after an area transition fall in reasonable locations.

#### Step 5: Copy `path` and Paste into Pipeline

After confirming the route is correct, click **`Copy Path`**.

What the tool copies to the clipboard is **only the `path` body**, not the complete node JSON. That is, you can directly paste it into:

```json
"custom_action_param": {
    "path": [
        ...
    ]
}
```

This is also why it is recommended to finish all orchestration in the GUI before copying, because the exported content is already in the canonical format that MapNavigator can directly consume.

### Importing Existing Paths for Editing

If you have already written a path in another Pipeline, or a colleague has given you a piece of JSON / JSONC, you can also click **`Import JSON`**.

The tool will recursively scan the file for recognizable `path` data and automatically load the candidate route with the most points. If the source data lacks zone information, the GUI will prompt you to assign areas to each route segment before continuing with editing and exporting.

This is very suitable for the following scenarios:

- Migrating old paths to the new navigation module.
- Reusing existing routes in multi-person collaboration.
- Modifying previous routes.

### Assert Mode

When you need not to "record a path" but to "determine whether the character currently falls within a certain rectangular area", you can directly use the `Assert Mode` at the top of the tool.

Usage:

1. Check `Assert Mode`.
2. Select the target `zone` from the dropdown.
3. Drag out a rectangle on the base map.
4. Click `Copy Assert` to copy the complete `MapLocateAssertLocation` node to the clipboard.

This mode does not modify the current path data; it merely borrows the same map rendering capabilities to quickly generate area determination nodes.

---

## Actual Development Suggestions

1. Record whenever possible; try not to manually craft the entire path. Walking through once in reality is usually more accurate than filling coordinates by feeling. If the precision of points hit by running and sprinting feels insufficient, try walking slowly.
2. Starting point stability. Before recording, tidy up the character's position and viewpoint to reduce the cost of subsequent point correction.
3. Special action points should be fewer but more precise; do not indiscriminately scatter them along the path. Especially for points like `INTERACT`, `TRANSFER`, `PORTAL`, `HEADING`, they should only be placed where they genuinely need to be triggered. `HEADING` also requires attention as a control node; it is usually more stable to manually maintain it after GUI export.
4. For cross-area routes, always check the map transition points. Automatic addition of `PORTAL` is only for semantic supplementation, meaning not all cross-area boundaries are inherently reasonable.
5. The outer Pipeline should still perform entry verification and failure fallback. Navigation is not the business flow itself; do not place all exception handling on a single `MapNavigateAction`.

---

## Collection Semantics COLLECT / DIG

### Concept

`COLLECT` and `DIG` are MapNavigator's **native collection/digging semantic points**. The path author only needs to write the collection coordinates as `[x, y, "COLLECT"]` or `[x, y, "DIG"]` in the `path` array. After the navigator arrives precisely, it will automatically stop, synchronously trigger the corresponding pipeline subtask to complete collection/digging, and then continue to the next path segment, **without exiting NaviController throughout**.

This improvement over the old `anchor` chain writing method includes:

- No re-establishment of connection, re-bootstrapping, or resetting of sprint start grace period for each collection.
- The entire segment near collection points automatically prohibits sprinting, preventing overshooting the target.
- Multiple collection points are merged in a single Pipeline node, without needing to split them into multiple `GotoFindN` nodes.

### Writing Method

In `custom_action_param.path`, change the third element of the target coordinates that need collection/digging to the corresponding action string:

```json
"path": [
    { "action": "ZONE", "zone_id": "Wuling_Base" },
    [707, 838],
    [720, 832],
    [741, 802, "COLLECT"],
    [744, 800, "COLLECT"],
    [739, 792, "COLLECT"]
]
```

- `[x, y, "COLLECT"]`: Triggers OCR recognition + automatic click collection (`AutoCollectClickStart`) upon reaching this point.
- `[x, y, "DIG"]`: Triggers unconditional click digging (`AutoCollectDigStart`) upon reaching this point.
- Any number of `COLLECT` and `DIG` points can be mixed within the same `MapNavigateAction` node.
- **No need** to write `anchor` on the node or point `next` to `AutoCollectClickStart`.

### Files Path Authors Need to Care About

| File                                                          | Responsibility                                                                       | When Changes Are Needed                                          |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------ | ---------------------------------------------------------------- |
| `assets/resource/pipeline/AutoCollect/AutoCollectRoute*.json` | Path definitions, containing `MapNavigateAction` nodes and collection coordinates    | Add new routes, adjust coordinates, add/remove collection points |
| `assets/resource/pipeline/AutoCollect/AutoCollectClick.json`  | OCR and click subtask triggered by `COLLECT`, entry point is `AutoCollectClickStart` | Add or delete OCR-recognized collection object names             |
| `assets/resource/pipeline/AutoCollect/AutoCollectDig.json`    | Digging subtask triggered by `DIG`, entry point is `AutoCollectDigStart`             | When digging interaction logic changes                           |
| `assets/resource/pipeline/AutoCollect.json`                   | Route iteration, failure collection, and backpack storage before/after collection    | Add route entries or adjust the overall flow                     |

**In most cases, path authors only need to modify `AutoCollectRoute*.json`.**

The overall Auto Collect flow uses `AutoCollectLoop` to invoke route wrapper nodes in order. Each wrapper uses the generic `FailureCollectorRunTask` to execute an enabled route; if any node inside the route fails, the wrapper Action records that route's `{Route}Failed` Pipeline node and returns success so Pipeline continues with the next route. After all routes and the final backpack storage step finish, `AutoCollectFinish` calls those nodes in failure order to output the localized `$option.*.label`, then makes the Auto Collect task return failure.

### Parts Path Authors Do Not Need to Touch

The following files are maintained by cpp-algo developers; path authors do not need to modify them:

- `agent/cpp-algo/source/MapNavigator/navi_domain_types.h`: `ActionType` enum, `COLLECT`/`DIG` declared here.
- `agent/cpp-algo/source/MapNavigator/navi_config.h`: Subtask entry names, `pipeline_override`, wait time after collection, and other constants.
- `agent/cpp-algo/source/MapNavigator/semantic_nodes.cpp`: Execution logic after arriving at the collection point.

### Boundary Description

**Old Writing Method Deprecated**

The old `anchor: { "AutoCollectClickAfter": "..." }` + `next: ["AutoCollectClickStart"]` chain-splitting writing method is deprecated and should no longer appear in new routes.

**`AutoCollectClickEnd`'s `next` Cannot Be Changed**

The `next` in `AutoCollectClickEnd` within `AutoCollectClick.json` points to `[Anchor]AutoCollectClickAfter` to maintain compatibility with old anchor chain calls. When called from a `MaaContextRunTask` subtask, the cpp-algo layer temporarily nullifies this `next` via `pipeline_override`, allowing the subtask to exit cleanly. Path authors **should not modify** this field, as it may affect other routes still using the old writing method.

**Sprint Control is Runtime-Managed**

For all `COLLECT`, `DIG`, and strict arrival points, the sprint on the entire preceding segment is hard-disabled by cpp-algo at the `NavigationStateMachine` level. Path authors cannot and do not need to control this behavior in the path JSON.

### Complete Steps for Adding a New Collection Route

1. Create a new `AutoCollectRouteN.json` under `assets/resource/pipeline/AutoCollect/`, referencing existing routes to write the basic skeleton of four nodes: `Start` → `AssertLocation` → `Goto` → `End`.
2. Use the MapNavigator tool to record the path. In the GUI, change the action of the collection target points to `Collect` or `Dig`, copy the `path`, and paste it into the `custom_action_param.path` of the `Goto` node.
3. Register the new route entry in `interface.json` / the task entry JSON.
4. No changes are needed to `AutoCollectClick.json`, `AutoCollectDig.json`, or any cpp-algo source files.
