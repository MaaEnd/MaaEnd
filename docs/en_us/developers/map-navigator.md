# Developer Guide - MapNavigator Path Navigation System

## Introduction

This document explains how to use **MapNavigator**-related nodes, and how to record, edit, and export navigation paths that can be used directly in Pipeline with the built-in GUI tool.

**MapNavigator** is MaaEnd's current high-precision automatic navigation Action module. It continuously obtains the player's current zone, global coordinates, and facing direction from the underlying localization capability, then drives the character point by point along the developer-provided `path`, executing sprint, jump, interaction, and zone-transition actions at key points.

### Scope and Limitations

MapNavigator is responsible for "**given a route, move the character there reliably**" and belongs to the Action layer.

- It does **not** handle business flow orchestration. When to start moving, where to stop, and how to handle unexpected situations should still be decided by the outer Pipeline.
- It does **not** generate business logic automatically. The path itself still needs to be recorded or edited by the developer first, then passed into `custom_action_param.path`.
- It does **not** decide whether "this route should be used right now." For entry-condition checks, you should first use recognition or scene-related nodes, and only then enter the navigation action.

### Relationship Between MapNavigator and the Recording Tool

The repository includes a dedicated GUI tool at `/tools/MapNavigator`.

Its intended workflow is extremely direct:

1. Launch the game, then open the tool.
2. Click Start Recording.
3. Walk the route once in-game.
4. Stop recording, then fine-tune points, delete points, or add actions in the GUI.
5. Click Copy, then paste the exported `path` into Pipeline `custom_action_param.path`.

In other words, **most routes do not need to be written by hand**. For developers, the recommended workflow is "record first, arrange later, paste at the end."

---

## Node Description

The following sections detail the usage of nodes provided by MapNavigator. The current interface is a MAA `Custom` Action: `MapNavigateAction`.

### custom_action: MapNavigateAction

Moves the character automatically along the given path and executes extra actions on path points.

#### Node Parameters

**Required Parameters (in practice, you should at least provide `path`)**:

- `path`: A list of path nodes. MapNavigator consumes them in order and continues navigation until the route finishes or fails midway.

**Common Optional Parameters (`custom_action_param`)**:

- `map_name`: String, default empty. Used as the initial zone context. If your `path` already contains `ZONE` declaration nodes, you usually do not need to set it.
- `path_trim`: Boolean, default `false`. When enabled, MapNavigator tries to attach the current position to a more suitable point on the route before navigation starts, instead of forcing the run from the very first point. This is useful when reusing the same route and the character's initial standing position may vary slightly.
- `arrival_timeout`: Positive integer, default `60000`. Maximum allowed time for a single target point before it is considered unreachable, in milliseconds.
- `sprint_threshold`: Positive real number, default `25.0`. Distance threshold used for automatic sprint judgment.
- `enable_rejoin`: Boolean, default `true`. Whether the navigator is allowed to try reconnecting to the route after a slight drift, minor blockage, or mid-route detachment.

Besides the fields above, the current implementation also contains several internal tuning parameters related to rejoin strategy and local driver details. Those are more like internal algorithm control knobs, and are not recommended as regular public-facing development interfaces. If you really need them, read the implementation first before relying on them.

#### `path` Data Structure

`path` is essentially an array, and each element represents one "path node." In normal use, you usually do not need to write these by hand. It is much more recommended to arrange them with the GUI tool at `/tools/MapNavigator`. Common forms are shown below.

**1. The most common coordinate point**

```json
[
    688,
    350
]
```

This represents a normal movement point. Once the character reaches this coordinate, navigation proceeds to the next point.

**2. A coordinate point with an action**

```json
[
    720,
    350,
    "SPRINT"
]
```

This means a `SPRINT` action should be executed upon reaching that point. Common actions currently include:

- `RUN`: A normal movement point.
- `SPRINT`: Trigger one sprint upon arrival.
- `JUMP`: Jump upon arrival.
- `FIGHT`: Attack once upon arrival.
- `INTERACT`: Interact upon arrival.
- `TRANSFER`: Reach the point precisely, then wait for an external mechanism to relocate the character to the next reachable segment.
- `PORTAL`: A cross-zone transition point. Once committed, it enters blind-walk mode and waits for the zone switch.

**3. Strict-arrival point**

```json
[
    700,
    350,
    "INTERACT",
    true
]
```

The trailing `true` means strict arrival is enabled for that point. For certain key points that really require precise arrival, such as interaction, jump, teleport, or zone-transition points, it is recommended to use strict arrival or directly use the corresponding action point, because the underlying logic already applies stricter arrival semantics there, such as slower approach and tighter arrival-radius confirmation.

**4. Zone declaration node**

```json
{
    "action": "ZONE",
    "zone_id": "Wuling_Base"
}
```

This is a **positionless control node** used to declare which zone the following path should belong to. It does not move the character by itself, but it provides zone-validation context for the subsequent path points.

#### Return Behavior

`MapNavigateAction` is an Action node, so it does not expose a stable structured recognition output like a Recognition node does. In practice, its result is mainly reflected as:

- If the entire route is completed successfully, the Action returns success.
- If severe timeout, rejoin failure, long-term localization loss, or similar problems occur midway, the Action returns failure.

So in Pipeline, it is generally best treated as an atomic action: "**either the whole path finishes, or the node fails**."

#### Usage Example

The most common usage is simply to paste the `path` copied from the recording tool:

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

If you want the navigator to resume from a closer position when re-entering the same route repeatedly, you can also add common optional parameters:

```json
{
    "MyNavigateNode": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapNavigateAction",
        "custom_action_param": {
            "path_trim": true,
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
> In actual development, it is recommended to place `MapNavigateAction` after a node that has already confirmed the entry state. Confirm that the character is indeed in the expected scene, zone, and roughly correct orientation before starting a full navigation segment. This improves success rate significantly.

> [!WARNING]
>
> Adjacent path points should still be reasonably traversable one after another. Do not expect the navigator to clip through geometry, route around highly complex obstacles, or understand business-specific mechanisms automatically. For special segments such as portal transitions, jump pads, falling, or lift-like mechanisms, explicitly split them using `PORTAL`, `TRANSFER`, or separate business nodes.

---

## Tool Guide

We provide a dedicated GUI tool for MapNavigator at `/tools/MapNavigator`, with `main.py` as the entry point.

It supports:

1. Connecting directly to the current game window and recording real movement traces.
2. Automatically adding `ZONE` and `PORTAL` semantics based on zone changes.
3. Deleting points, dragging points, changing actions, and editing strict-arrival settings in the GUI.
4. Importing existing JSON / JSONC files, recursively searching recognizable `path` data, and continuing editing.
5. One-click copying of the canonical `path` that can be pasted directly into `custom_action_param.path`.

### Running the Tool

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

### Before You Start

Before recording, make sure:

1. The project development environment has already been set up according to the development guide, especially that `install/agent/cpp-algo.exe` and `install/maafw` are usable.
2. The Python dependencies `maafw` and `Pillow` are installed.
3. The game is already running and the window is **not minimized**.
4. The character is already standing near the route starting point you want to record.

### Recommended Workflow

This is the most recommended and least painful way to use MapNavigator in practice.

#### Step 1: Open the tool and start recording

Run `tools/MapNavigator/main.py`, then click **`Start Recording`** in the top-left of the GUI.

The tool will automatically:

1. Launch the local Agent.
2. Search for the current game window.
3. Call the underlying localization logic continuously to read coordinates and zone information.
4. Sample the route you actually walked into a raw trajectory.

If the environment is incomplete or the game window cannot be found, the tool reports an error directly instead of producing invalid path data.

#### Step 2: Switch back to the game and walk the route once manually

After recording starts, go back to the game and simply **walk the route the way you want the character to execute it later**.

During recording, the tool reads part of the keyboard state and records action points automatically:

- With no special key pressed, it records `RUN`.
- Pressing `Space` records `JUMP`.
- Pressing `F` records `INTERACT`.
- Holding `Shift` or the right mouse button records `SPRINT`.

One important note: points with stronger business semantics such as `FIGHT` and `TRANSFER` are **not inferred automatically during recording**. The usual workflow is to stop recording first, then manually change those points to the desired action in the GUI.

So the most basic workflow is simply:

1. Click Start Recording.
2. Go into the game and run the route normally.
3. Press Space when a jump is needed.
4. Press F when interaction is needed.
5. Come back and click Stop when finished.

That is exactly the primary workflow the tool was designed for.

#### Step 3: Stop recording and review the automatically cleaned-up result

After clicking **`Stop Recording`**, the tool performs one round of cleanup on the raw trace, including:

- Compressing overly dense path points.
- Keeping important turning points and action points.
- Automatically adding `PORTAL` semantics on cross-zone boundaries.
- Splitting the view by the current zone for easier browsing.

Under normal circumstances, what you see is no longer raw "one point per frame" data, but an editable and exportable navigation route.

#### Step 4: Arrange the path in the GUI

At this point, you can handle the remaining details directly in the GUI.

**View operations:**

- Mouse wheel: zoom.
- Right mouse drag: pan the view.
- Left click on blank space: insert a new point.
- Left click on an existing point: select it.
- Left drag on an existing point: fine-tune its coordinate.

**Zone switching:**

- The `◀ / ▶` buttons at the top are used to switch between zones.
- If the route crosses zones, the tool displays each zone as a separate segment, making it easier to verify whether the transition before and after the zone boundary is reasonable.

**Point property editing:**

- The action dropdown at the top lets you set the action for the current point.
- `Set`: replace the current point's action with the selected one.
- `Append`: append another action semantic to the current point.
- `Pop`: remove the last action semantic from the current point's action chain.
- `Strict`: mark the current point as a strict-arrival point.
- `🗑`: delete the currently selected point.

**Undo / Redo:**

- `Ctrl+Z`: undo.
- `Ctrl+Y`: redo.

In practice, these are usually the only edits you really need:

1. Delete points that are too dense and not meaningful.
2. Change key interaction points to `INTERACT` or enable `Strict`.
3. Change points that need jumping, sprinting, or transfer behavior into the corresponding action.
4. Check whether the points before and after a zone transition are placed reasonably.

#### Step 5: Copy the `path` and paste it into Pipeline

Once the route looks correct, click **`Copy Path`**.

What the tool copies to the clipboard is **the `path` body only**, not a full node JSON object. That means you can paste it directly into:

```json
"custom_action_param": {
    "path": [
        ...
    ]
}
```

This is also why it is recommended to finish all editing in the GUI before copying, because the exported content is already the canonical format that MapNavigator can consume directly.

### Import and Edit Existing Paths

If you already wrote a path in another Pipeline, or a teammate gives you a JSON / JSONC file, you can also click **`Import JSON`**.

The tool recursively scans the file for recognizable `path` data and automatically loads the candidate route with the most points. If the source data lacks zone information, the GUI will ask you to assign a zone for each route segment before continuing with editing and export.

This is especially useful for:

- Migrating old paths to the new navigation module.
- Reusing existing routes in collaborative development.
- Modifying a previously created route.

---

## Practical Development Advice

1. Record when possible. Try not to hand-write an entire path. Actually walking the route once is usually more accurate than filling coordinates in by feel. If the precision of path points recorded while running and sprinting feels insufficient, just walk more slowly.
2. Keep the starting point stable. Before recording, stabilize the character's position and camera as much as possible. This reduces later editing work.
3. Use special action points sparingly and precisely. Especially for `INTERACT`, `TRANSFER`, and `PORTAL`, only place them where they are truly needed.
4. Always inspect zone-transition routes carefully. Automatically adding `PORTAL` only helps supplement semantics; it does not mean every cross-zone boundary is naturally valid.
5. The outer Pipeline still needs proper entry checks and failure fallback. Navigation is not your business flow itself, so do not push all exception handling into a single `MapNavigateAction`.
