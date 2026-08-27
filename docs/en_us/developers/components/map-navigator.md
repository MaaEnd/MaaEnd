# Development Manual - MapNavigator Path Navigation System

## Introduction

This document explains how to use the **MapNavigator** related nodes and how to leverage the built-in GUI tool in the repository to record, edit, and export navigation paths that can be directly used in a Pipeline.

**MapNavigator** is MaaEnd's current high-precision automatic navigation Action module. It relies on the underlying MapLocator's continuous localization to obtain the character's current area, global coordinates, and orientation. Then, it drives the character to move to the target location, executing actions like sprinting, jumping, interaction, and map transition at key points.

### Two Workflows

MapNavigator offers two ways to specify "where to go". **Determining which one your scenario belongs to first can save a great deal of work**:

| Workflow | What you need to provide | Applicable scenario |
| ---------------------------- | -------------------------- | ----------------------------------------------------------------------------------------------------------- |
| **Target-based pathfinding** | A single target coordinate | The target point is inherently walkable, with no special mechanisms along the way |
| **Recorded path** | The full waypoint sequence | The route involves interactions, map transitions, jump platforms, external teleports, and similar semantics |

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

No pre-recording of the route, no intermediate points, and no `zone_id` are needed — at runtime the area is inferred from the current localization, and an executable path is planned on the BaseNav triangle graph. When the target point is on a layered map, just add one more [`target_tier`](#tier-coordinate-frame-target_tier) field.

This form is already used in production routes such as Auto Collect and Environment Monitoring.

In the GUI's A\* tab, paste a JSON coordinate pair such as `[724.98, 1596.8]` into either the X or Y field to fill both values automatically, then click **Mark Point** to show the target preview.

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
- `interact_text`: String or array of strings, empty by default. Route-wide default for the interact prompt text, see [Async Interaction](#async-interaction-interact). The field also supports camelCase `interactText`; text written on a waypoint wins, and the route-wide value only fills in the `INTERACT` points that carry none of their own. An empty string or an empty array is rejected outright (the whole parameter parse fails) rather than treated as absent, because empty text matches anything on the recognition side.
- `interact_scan`: String, empty by default. Route-wide default for the walking pre-filter node, see [Replacing the Icon Pre-filter](#replacing-the-icon-pre-filter). The field also supports camelCase `interactScan`; a node named on a waypoint wins, and an empty string counts as absent (falling back to the shipped one).
- `interact_rec`: Boolean, `false` by default. Whether this route's `INTERACT` points recognize the prompt without pressing anything, see [Recognize Only, Never Press](#recognize-only-never-press-rec-mode). The field also supports camelCase `interactRec`. Unlike the two above it does not follow "the waypoint wins": writing `true` here turns the whole route's `INTERACT` points on, and a waypoint cannot turn itself back off — a boolean cannot tell "written `false`" from "not written", so this field only ever switches points on. Leave it off the route root when some points should still press.
- `enable_bootstrap_navmesh`: Boolean, `true` by default. Whether the run may plan a navmesh route at startup to join the recorded path. Set it to `false` to skip that planning and walk the recorded points directly — the escape hatch for stacked terrain (platforms, catwalks, rooftops) where the startup plan detours badly.
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

When a coordinate was authored directly on a tier basemap, use the object form to declare that point's coordinate frame explicitly:

```json
{
    "action": "RUN",
    "target": [
        243.49,
        177.53
    ],
    "target_tier": "Wuling_L4_328"
}
```

`target_tier` only describes the coordinate frame of this node. It does not switch zones, update following nodes, or replace the `ZONE` / `PORTAL` semantics required for an actual area transition. Existing arrays and object points without `target_tier` retain their previous behavior.

The object form is also how an `INTERACT` point declares the prompt text it came for (and, when needed, `interact_scan`). The array form `[x, y, "INTERACT"]` has no slot for these fields, so async interaction requires the object form:

```json
{
    "action": "INTERACT",
    "target": [
        331,
        1578
    ],
    "interact_text": "登记"
}
```

Only `INTERACT` points can take these fields — on any other action they are ignored, with one `carries interact fields without an INTERACT action` line in the log.

- `RUN`: Ordinary movement point.
- `SPRINT`: Perform a sprint once upon arrival.
- `JUMP`: Jump upon arrival.
- `FIGHT`: Attack once upon arrival.
- `INTERACT`: Interact upon arrival. With `interact_text` it becomes an async interaction instead, see [Async Interaction](#async-interaction-interact).
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

###### Tier Coordinate Frame: `target_tier`

For `NAVMESH`, omitting `target_tier` means that `target` is already in **base-map coordinates**. For ordinary coordinate waypoints, omitting it preserves the historical coordinate interpretation without adding a conversion.

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
- Positioned ordinary actions (`RUN / SPRINT / JUMP / FIGHT / INTERACT / PORTAL / TRANSFER / COLLECT / DIG`) and target-based `HEADING` accept the same `target` + `target_tier` object form. Unlike `NAVMESH`, this declaration does not imply navigation or a zone transition; it only projects that one coordinate before execution.
- The field also supports camelCase `targetTier`. An unknown tier on `NAVMESH` keeps the compatibility behavior of logging a warning and treating the target as base coordinates. An explicitly tagged ordinary point fails instead of silently moving toward the wrong location.

###### Overlapping Deck Target: `target_deck_y`

The game is three-dimensional, the map is not: a walkway, a bridge and a rooftop can all sit on the same `target`. Without `target_deck_y` the planner treats **every** walkable surface in the target cell as a goal and stops at whichever it reaches first. Stacked surfaces usually belong to the same connected component so nothing fails, and the two-dimensional arrival check passes either way — which makes landing on the wrong deck completely silent.

`target_deck_y` declares which walkable surface the `target` sits on. The value is that surface's **world height**:

```json
{
    "action": "NAVMESH",
    "target": [
        358.8,
        238.8
    ],
    "target_deck_y": 265.37
}
```

- Read the number from the MapNavigator GUI: click the target and the sidebar lists the overlapping decks; selecting one highlights that deck on the canvas, and the "Select" button writes the value into the field. Do not estimate it by hand.
- A deck matches when it is the nearest surface and within 2px (measured spacing between adjacent decks is 8–9px). When nothing matches, the route fails with "target deck unreachable" — it does not fall back to the cell-level search and is not absorbed by the blind-walk fallback. Failing loudly beats silently walking onto another deck.
- Points with a single surface do not need it; the GUI hides the list when there is no overlap.
- The field also supports camelCase writing `targetDeckY`, and can be combined with `target_tier`.

A declaration applies to its own waypoint only: **it pins which deck that leg stops on, while which deck the start stands on is worked out by the planner from the start's own height**. A leg can therefore go from a rooftop down to a walkway or the other way round with no declaration on the start side — and re-planning starts from the live two-dimensional localization, which has no deck to begin with.

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
3. Deleting points, dragging points, changing coordinate point actions, modifying strict arrival, and declaring an optional per-point coordinate tier in the GUI.
4. Selecting existing project Pipeline nodes from `assets`, or reading JSON from the current clipboard under `More`.
5. One-click copying of canonical `path` that can be directly pasted into `custom_action_param.path`.
6. Through an independent `Assert mode` to manually select the base map and frame rectangular areas, exporting `MapLocateAssertLocation` nodes.
7. Entering BaseNav A\* mode, loading `.nav.gz` / `.nav`, previewing paths on the red triangle face overlay, and copying `NAVMESH` nodes.

An additional note is that the current GUI editor round-trips coordinate path points, their optional `target_tier`, and `ZONE` declarations derived from area information. Untagged points keep the legacy array export, while tagged points use the `target` object form.
Non-coordinate control nodes like `HEADING` and semantic pathfinding nodes like `NAVMESH` are not regular point editing objects in the GUI. It is recommended to manually add back or maintain `HEADING` after exporting the `path`, while `NAVMESH` can be directly generated using `Copy NAVMESH`.

### Running Method

```powershell
cd tools\MapNavigator
uv run main.py
```

### Pre-run Preparation

Before starting to record, please confirm:

1. The project development environment has been configured according to the development manual, especially that `install/agent/cpp-algo.exe` and `install/maafw` are usable.
2. uv is installed; `uv run main.py` prepares Python and dependencies automatically from the PEP 723 metadata.
3. **Windows**: The tool needs to be run with **administrator privileges**; otherwise, the G/X hotkeys may not be captured by the system when the game (an administrator process) is in the foreground. `main.py` will automatically detect this and prompt a UAC elevation request at startup.
4. **macOS**: On the first run, authorize the current terminal or the uv-managed Python interpreter in **System Settings → Privacy & Security → Input Monitoring**; otherwise, global hotkeys will not work.
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

| Shortcut Key | Function |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------- |
| `G` | 📋 **Copy the current coordinates to the clipboard** in `[x, y]` format (does not affect recorded data, can be pressed anytime) |
| `X` | 📌 **Force insert a strict arrival (strict) path point** at the current precise location into the recorded data |

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
- `Coordinate Tier`: Declare which tier basemap the selected point's coordinate was authored on. Leaving it empty keeps the legacy coordinate behavior; it does not modify `ZONE`.
- `🗑`: Delete the currently selected point.

The current action dropdown targets coordinate point actions, commonly edited to `RUN / SPRINT / JUMP / FIGHT / INTERACT / PORTAL / TRANSFER / COLLECT / DIG`.
Non-coordinate control nodes like `HEADING` are not part of this GUI action chain.

**Undo/Redo:**

- `Ctrl+Z`: Undo.
- `Ctrl+Y`: Redo.
- `C`: Copy the coordinates of the currently selected point to the clipboard (format is `[x, y]`, supports copying multiple selected points line by line).

Usually, the only fine-tuning you really need to do is:

1. Change key interaction points to `INTERACT` and check `Strict` (points recorded with the X key are already strict arrival by default). When the prompt text has to be confirmed before pressing, add `interact_text` to that point after exporting — see [Async Interaction](#async-interaction-interact).
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

### Importing Project Nodes

All three route-making modes use **`Select Project Node`** as their primary import entry. The path editor loads `MapNavigateAction` nodes for continued editing. A\* keeps coordinates on the same basemap as ordered targets. Whether one or several targets are imported, click the map once to add a manual start; the tool then plans `manual start -> imported point 1 -> imported point 2 -> ...` automatically. A lone target remains directly copyable before a start is added. Assert mode lists `MapLocateAssertLocation` nodes by default and can switch to reference routes. The selector searches by resource path, node name, or zone, so files containing several Pipeline nodes are not imported ambiguously. Path imports validate action semantics strictly: an unknown action rejects the import and is never silently downgraded.

Each mode provides a direct **`Read Clipboard`** button. The generic recursive importer recognizes a complete Pipeline, a single `MapNavigateAction` / `MapLocateAssertLocation` node, a `{"path": [...]}` object, or a bare `path` array. If path data lacks zone information, the GUI prompts for a zone assignment before loading it. Clipboard access is initiated by the button click, and the browser may request permission the first time.

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

| File | Responsibility | When Changes Are Needed |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------ | ---------------------------------------------------------------- |
| `assets/resource/pipeline/AutoCollect/AutoCollectRoute*.json` | Path definitions, containing `MapNavigateAction` nodes and collection coordinates | Add new routes, adjust coordinates, add/remove collection points |
| `assets/resource/pipeline/AutoCollect/AutoCollectClick.json` | OCR and click subtask triggered by `COLLECT`, entry point is `AutoCollectClickStart` | Add or delete OCR-recognized collection object names |
| `assets/resource/pipeline/AutoCollect/AutoCollectDig.json` | Digging subtask triggered by `DIG`, entry point is `AutoCollectDigStart` | When digging interaction logic changes |
| `assets/resource/pipeline/AutoCollect.json` | Route iteration, failure collection, and backpack storage before/after collection | Add route entries or adjust the overall flow |

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

---

## Async Interaction INTERACT

### Concept

`INTERACT` carries two meanings, decided by whether the waypoint has `interact_text`:

- **Without it**: press the interact key once upon arrival. This is the historical behavior; existing routes are unaffected.
- **With it**: the point becomes an **async interaction**. On the last stretch toward that point, a background pre-filter watches for the interact prompt; when the prompt shows up the navigator stops and hands over to a Pipeline subtask, which OCRs the prompt and presses the interact key only when the text matches `interact_text`. The prompt showing up is the game's own confirmation that the character is in range, so that press completes the point — the little distance left is not walked.

The reason the route has to supply the text: there are far too many kinds of interactable, and the prompt wording differs per business, so no single shared table can enumerate them. Collectible names *are* a shared table, which is why `COLLECT` keeps its text in the Pipeline node; interact text has to be injected by whichever route uses it.

What a route may customize is those two recognitions — when to stop, and whether this is the one — plus whether a match presses anything:

| Field | Replaces | Left out |
| --------------- | ----------------------------------------------------------- | ----------------------------------------------- |
| `interact_scan` | The icon pre-filter that decides **when to stop** while walking | The shipped one, looking for the default icon |
| `interact_text` | The OCR text that decides **whether this is it** once stopped | The point is not async; it interacts on arrival |
| `interact_rec` | Whether a match **presses anything** | It presses |

What gets pressed is not customizable — it is always the interact key (F on Windows, a key code on macOS, a tap on the recognized prompt on touch controllers). To keep navigation from pressing at all, set `interact_rec`, see [Recognize Only, Never Press](#recognize-only-never-press-rec-mode). Everything past the UI the interaction opens belongs to the outer Pipeline; navigation returns once the point is done. See [Replacing the Icon Pre-filter](#replacing-the-icon-pre-filter).

### Writing Method

Write the interaction point in the object form and add `interact_text`:

```json
"path": [
    { "action": "ZONE", "zone_id": "Wuling_Base" },
    [405, 1592],
    { "action": "INTERACT", "target": [331, 1578], "interact_text": "登记" }
]
```

- `interact_text` accepts a string or an array of strings; any one of them matching is enough to press.
- The text is matched against the OCR result as a **regular expression**, the same semantics as Pipeline's `expected`. Copy the in-game wording as-is; escape regex metacharacters yourself if the wording contains any.
- When a whole route belongs to one business, put `interact_text` at the top level of `custom_action_param` as the default, see [Node Parameters](#node-parameters).
- When navigation should only walk there and recognize the prompt, leaving the press to the outer Pipeline, add `interact_rec`, see [Recognize Only, Never Press](#recognize-only-never-press-rec-mode).
- A single route may mix interaction points from several businesses, and mix them with `COLLECT` / `DIG`, in any number.
- **No trailing `true` is needed**: `INTERACT` is already handled with strict arrival semantics.

In the GUI: walk up to the interactable normally while recording, change that point's action to `INTERACT` after stopping, then add `interact_text` to it by hand after exporting the `path` (the editor does not emit this field yet).

### Writing Text That Survives OCR

OCR is not always reliable, which is why this field is **a set of** regular expressions rather than one literal line. List every variant:

```json
{
    "action": "INTERACT",
    "target": [331, 1578],
    "interact_text": [
        "^登记$", // anchored short verb, so other words in the ROI cannot latch on
        "^登記$", // traditional
        "(?i)^Register$", // (?i) ignores case
        "(?i)^Sign\\s*In$" // \\s* absorbs spaces OCR splits in
    ]
}
```

Rules of thumb:

- **Anchor first.** Matching is "contains", not whole-string equality, so for a short verb prompt like 登记 write `^登记$`; an unanchored `登记` matches any text in the same ROI that contains those characters.
- **Prefer too narrow over too wide.** A miss only wastes one visit to the point (navigation does not fail, see below), whereas a false match presses the key at the wrong thing.
- **List variants one per line instead of expecting one pattern to cover every language.** Write simplified, traditional, English and Japanese separately; the syntax is Perl-flavoured, so inline flags such as `(?i)` are available. Remember that a backslash is doubled in JSON.
- **Leading and trailing spaces need no handling** — the recognized text is trimmed before matching; `\\s*` is only for spaces split into the middle of the text.
- **JSONC comments work.** Note which language or which UI each line covers, and keep the reason when a line is disabled.
- **A malformed regex does not fail silently.** The injected text is validity-checked first, and an invalid pattern makes the whole subtask refuse to dispatch, logging `regex invalid`, `failed to override_pipeline` and `Prompt subtask failed to dispatch` in sequence; the point simply does not press, and navigation still does not fail.
- A ready-made model is `AutoPickInteractive` in `assets/resource/pipeline/RealTimeTask/AutoPick.json`: it mixes item names with verbs like `^采集$` and `^打开$` in one table, and records why a few lines are left out.

Reuse boundary: within one route a shared table goes at the top level of `custom_action_param` (see [Node Parameters](#node-parameters)); reuse across routes currently means writing the table once per route JSON.

### Replacing the Icon Pre-filter

On the last stretch toward an interaction point, a background pass looks for the prompt icon at a fixed cadence and stops only on a hit. This step only decides whether stopping is worth it — after the stop, `interact_text` has to confirm before anything is pressed — so it can afford to be loose.

The shipped one is an ordinary Pipeline node, `MapNavigatorInteractScan`, with its `roi` / `template` / `threshold` written right there. When the prompt icon looks different, or does not appear in the default region, copy it, adjust it, and name it with `interact_scan`:

```json
"MyBusinessInteractScan": {
    "recognition": {
        "type": "TemplateMatch",
        "param": {
            "roi": [755, 330, 297, 312],
            "template": "MyBusiness/InteractHint.png",
            "threshold": 0.75
        }
    }
}
```

```json
{
    "action": "INTERACT",
    "target": [331, 1578],
    "interact_text": "登记",
    "interact_scan": "MyBusinessInteractScan"
}
```

- This node is **never dispatched**; MapNavigator only reads those three parameters out of it. It therefore needs no `action`, no `next`, and does not have to hang off any chain.
- `recognition.type` must be `TemplateMatch`, `template` must be exactly one image, and `threshold` exactly one number (defaulting to `0.75`). `roi` must be absolute coordinates in the 1280×720 base frame — the offset-from-previous-node form is not supported, since the pre-filter runs independently per frame and has no "previous node".
- `template` resolves as a path under `image/`, the same as anywhere else in the Pipeline, and a platform overlay's same-named image wins as usual — so swapping the image for touch backends only means adding one under `resource_adb`.
- If any of the above does not hold, the log carries one `Prompt scan node ...` line and **the points naming it fall back to "recognize once upon precise arrival"**. It does not take the route down with it and it does not silently press the wrong thing.
- At most 4 pre-filters are armed per route (one background thread each). One node named by several points counts once. Anything beyond that reports `Prompt scan node dropped at the cap`, and those points fall back to arrival recognition too.
- **A pre-filter needs `interact_text` alongside it.** A point naming only `interact_scan` falls back to the plain meaning (press the interact key on arrival) with the pre-filter inert, and logs one `names a prompt scan node without any interact text` line. The text may live on the point or be inherited from the route root.
- A loose threshold cannot stall navigation: one pre-filter hit completes the point, so a pre-filter that fires on anything only spends its own points one by one and the route still drains forward. Those points do not actually interact, though, so set the threshold from the icon itself.

### Recognize Only, Never Press: rec Mode

An `INTERACT` point carrying `"interact_rec": true` keeps the whole sequence — the pre-filter while walking, the stop when the prompt appears, the OCR confirmation once stopped — and **drops only the final key press**. The arrival radius stays tightened to the async-interaction value, and a prompt hit still completes the point.

When this is what you want: the prompt is up but *which* entry to take is the business's decision, while the interact key can only ever take the default one. The typical shape is one interactable carrying several rows (claim / discard and the like) — the key always takes the first row, so the business's own "which row" switch never gets to matter. Let navigation just get there and recognize the prompt, hand the choice back to the outer Pipeline, and that switch is in charge again.

```json
{
    "action": "INTERACT",
    "target": [331, 1578],
    "interact_text": "领取",
    "interact_rec": true
}
```

- **It needs `interact_text` alongside it**, for the same reason `interact_scan` does: a point whose text never resolved cannot enter the async path at all and falls back to the plain meaning (press the interact key on arrival), which is exactly what rec mode exists to avoid. To keep that degradation from pressing anyway, the plain path honours this field too — it skips the press and logs one `INTERACT in rec mode, skipping the key press` line.
- **The recognition after stopping still runs**; only the recognition node's own key action is swapped for `DoNothing`. So `interact_text` is authored exactly as before, and whether it matched still shows up in the log.
- **Navigation emits no click at all.** Whether the interaction happened is entirely the outer Pipeline's to validate — that was already async interaction's contract; rec mode just follows it through.
- When a whole route belongs to one business, `interact_rec` can go at the top level of `custom_action_param`, see [Node Parameters](#node-parameters) — bearing in mind it only ever switches points on.

### Arrival Determination and Movement

Async interaction points use the same set of values as collection points, all runtime-managed, with nothing for the path author to control:

- The arrival radius is tightened to a smaller value, relaxing back to the ordinary radius only when the point stays out of reach for a long time.
- Automatic sprint is disabled for the whole segment approaching such a point, preventing overshoot.
- The last stretch automatically switches from running to walking, so the character is less likely to run past the prompt's trigger range.

The pre-filter only runs **while that point is the one being walked to and the last stretch has been entered**, at a fixed cadence, and a tick where the background reported nothing costs nothing; a route with no async interaction points is therefore unaffected. Passing other interactables further away cannot be mistaken for this point's prompt.

### Fallback and Failure Semantics

- If the prompt never shows up on the way, **the authoritative recognition still runs once upon precise arrival** — a missed pre-filter does not skip the point. Every async interaction point therefore gets exactly one authoritative recognition: a hit on the way completes it, and otherwise arrival makes it up.
- **Async interaction never fails navigation.** The key press only happens when OCR matches the injected text, and never at all in rec mode; navigation itself does not verify that the interaction actually took place, and does not error out because no prompt was recognized. Whether the interaction succeeded has to be validated by the outer Pipeline (for example, the UI that should appear afterwards).

When the interaction opens a UI (a registration desk, a commission board), **make that point the last one of the route**: once the UI is up the character stops moving and the minimap is covered, so later points cannot be walked. This is the same author-side discipline as plain `INTERACT` without `interact_text` — async or not makes no difference here.

The `MapNavigatorInteract` node ships with a placeholder text that can never match, so a route that injects no `interact_text` recognizes nothing there and cannot press by mistake.

### Files Path Authors Need to Care About

| File | Responsibility | When Changes Are Needed |
| ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| The business's own route JSON | The `MapNavigateAction` node, interaction coordinates and `interact_text` | Add interaction points, change wording |
| `assets/resource/pipeline/MapNavigator/Interact.json` | Async interaction subtask, entry point `MapNavigatorInteractStart`, holds the prompt ROI and the key action; the shipped pre-filter `MapNavigatorInteractScan` lives here too | When the prompt region, the default interact key or the shipped pre-filter changes |
| `assets/tasks/setting/Keymap.json` | Interact key rebinding; `KeymapInteract` applies to this node as well | Add or remove nodes affected by rebinding |
| `assets/resource_macos/pipeline/MacOSKeyMap.json` | macOS key-code overrides, listed alongside the other interact-key nodes | Add or remove nodes that press the interact key |
| `assets/resource_adb/pipeline/MapNavigator/Interact.json` | On ADB / cloud / PlayCover, taps the recognized prompt box instead (a touchscreen has no F key) | When the touchscreen action changes |

**In most cases, path authors only need to modify their own route JSON.**

### Parts Path Authors Do Not Need to Touch

The following files are maintained by cpp-algo developers; path authors do not need to modify them:

- `agent/cpp-algo/source/MapNavigator/async_prompt_action.h` / `.cpp`: the shared implementation of prompt-driven actions, used by both `COLLECT` and async `INTERACT`.
- `agent/cpp-algo/source/MapNavigator/prompt_scan_profile.h` / `.cpp`: reading the pre-filter parameters out of a Pipeline node, and the validation chain around it.
- `agent/cpp-algo/source/MapNavigator/navi_config.h`: subtask node names, pre-filter cadence and last-resort constants, arrival values and others.
- `agent/cpp-algo/source/MapNavigator/navi_param_parser.cpp`: parsing of `interact_text` / `interact_scan` / `interact_rec` and propagation of the route-wide defaults.
- `agent/cpp-algo/source/MapNavigator/semantic_nodes.cpp`: the fallback execution logic upon arrival.
