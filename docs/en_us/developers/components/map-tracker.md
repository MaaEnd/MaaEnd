# Development Guide - MapTracker Reference Document

## Introduction

This document describes how to use the common nodes related to **MapTracker**.

**MapTracker** is a computer vision-based **minimap tracking system** that can infer the player's position based on the minimap in the game and control the player to move according to specified waypoints.

### Key Concepts

1. **Map Name**: Each large map has a unique name in the game, e.g., "map01_lv001", where "map01" indicates the region is "Fourth Valley" and "lv001" indicates the sub-region is "Hub Area". Please check `/assets/resource/image/MapTracker/map` to get all map names and images (these images have been scaled to fit the minimap UI in the game with 720P resolution). The `map_name` must **exactly match** the filename (without the `.png` extension) in that directory.
2. **Coordinate System**: The coordinates used by MapTracker are the pixel coordinates $(x, y)$ of the above large map images, with the upper-left corner of the image as the origin $(0, 0)$.

> [!TIP]
>
> For deeper technical details, please read [this advanced document](./map-tracker%28advanced%29.md). It introduces advanced programming nodes and maintenance guidance for MapTracker.

## Node Descriptions

The following details the specific usage of the **common nodes** provided by MapTracker. These nodes are all Custom type nodes and need to specify `custom_action` or `custom_recognition` in the pipeline to use.

### Action: MapTrackerMove

🚶Controls the player to move along the specified waypoints.

> [!IMPORTANT]
>
> A **GUI tool** is provided in the repository to easily generate, import, and edit waypoints. Please refer to [Tool Instructions](#tool-instructions) to learn how to maximize the use of the tool to improve efficiency.

#### Node Parameters

Required parameters:

- `map_name`: The unique name of the map. E.g., "map01_lv001".

- `path`: A list of real-number waypoints consisting of several coordinates. The player will move to these coordinate points in sequence.

Optional parameters:

- `no_print`: Boolean value, default `false`. Whether to turn off UI message printing of pathfinding status. For better user experience, it is not recommended to turn off message printing for this node.

- `path_trim`: Boolean value, default `false`. When enabled, the nearest waypoint in the path will be selected as the actual starting point based on the current position when this action begins (the waypoints before that selected point will be automatically skipped); when disabled, movement will always start from the first waypoint.

- `fine_approach`: String, default `"FinalTarget"`. It controls when fine-approach will be enabled to ensure a super precise arrival. Valid values are:

    | Option Value    | Meaning                                                        | Recommended Scenario                                                       |
    | --------------- | -------------------------------------------------------------- | -------------------------------------------------------------------------- |
    | `"FinalTarget"` | Enable fine-approach only for the final target point (default) | Most scenarios                                                             |
    | `"AllTargets"`  | Enable fine-approach for every target point                    | When waypoint precision is critical (e.g., passing through narrow bridges) |
    | `"Never"`       | Disable fine-approach                                          | /                                                                          |

<details>
<summary>Advanced Optional Parameters (Expand)</summary>

- `no_ensure_initial_movement_state`: Boolean value, default `false`. Whether to skip the sprint-preparation step before starting the initial movement. When enabled, pathfinding starts immediately without actively resetting to a stable initial movement state.

- `no_ensure_final_orientation`: Boolean value, default `false`. Whether to disable adjusting the player's orientation upon reaching the final target point to ensure the camera faces the last direction of the path.

- `arrival_threshold`: Positive real number, default `2.5`. The distance threshold for judging arrival at the next target point, in pixel distance. A larger value makes it easier to be judged as arriving at the target point but may result in incomplete pathfinding; a smaller value requires more precise arrival at the target point but may make pathfinding difficult to complete.

- `arrival_timeout`: Positive integer, default `60000`. The time threshold for judging failure to reach the next target point, in milliseconds. If the next target point is not reached after this time, pathfinding fails immediately.

- `rotation_lower_threshold`: Real number between $(0, 180]$, default `7.5`. The direction angle deviation threshold for judging the need for fine-tuning the orientation, in degrees.

- `rotation_upper_threshold`: Real number between $(0, 180]$, default `60.0`. The direction angle deviation threshold for judging the need for large-scale orientation adjustment. At this time, the player will slow down to adjust orientation.

- `sprint_threshold`: Positive real number, default `10.0`. The distance threshold for performing the sprint action, in pixel distance. When the distance between the player and the next target point exceeds this value and the orientation is correct, the player will perform a sprint.

- `stuck_threshold`: Positive integer, default `2000`. The minimum duration for judging being stuck, in milliseconds. If the player does not actually move after this period of time, stuck mitigation actions will be triggered.

- `stuck_timeout`: Positive integer, default `10000`. The time threshold for judging failure to get out of the stuck state, in milliseconds. If the stuck state is not escaped after this time, pathfinding fails immediately.

- `stuck_mitigators`: List of strings, default `["MoveOrDeleteDevice", "Jump"]`. When the player is judged to be stuck, the actions in the list are executed sequentially to attempt to escape the stuck state. Doing nothing is not allowed; if this field is set to an empty list, the effect is the same as the default value. Available actions include:

    - `"Jump"`: Perform a jump action;
    - `"MoveOrDeleteDevice"`: Attempt to delete or move the device in front of the player.

- `map_name_match_rule`: String, default `"^%s(_tier_\\w+)?$"`. Allows maps that satisfy this expression to be used for pathfinding. The `%s` will be replaced by the `map_name` parameter (and automatically regex-escaped). Typical values are:
    - `^%s(_tier_\\w+)?$` (default): Allows the map itself and all its tiered maps to participate in pathfinding.
    - `^%s$`: Only allows the map itself to participate in pathfinding.

</details>

#### Example Usage

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerMove",
        "custom_action_param": {
            "map_name": "map02_lv002",
            "path": [
                [
                    688.0,
                    350.0
                ],
                [
                    679.5,
                    358.2
                ],
                [
                    670.0,
                    350.8
                ]
            ]
        }
    }
}
```

> [!TIP]
>
> Before executing this node, it is recommended to use the [MapTrackerAssertLocation](#recognition-maptrackerassertlocation) node to check whether the player's **initial position** meets the requirements to reach the first waypoint.

> [!WARNING]
>
> During the execution of this node, ensure that the player is **always in** the specified map, and adjacent waypoints **can be reached in a straight line**.

### Action: MapTrackerBigMapPick

🫳 Drags the big-map viewport until the target point appears, then can optionally click that point.

#### Node Parameters

Required parameters:

- `map_name`: The unique map name. For example, "map01_lv001".

- `target`: A list with 2 real numbers `[x, y]`, representing the target map coordinate.

Optional parameters:

- `on_find`: Action to perform after the target point is found. Default is `"Click"`. Available values:

    - `"Click"`: Click the target point (default).
    - `"Teleport"`: Perform teleportation (requires the point to be a teleport anchor).
    - `"DoNothing"`: Perform no action.

- `auto_open_map_scene`: Boolean, default `false`. Whether to automatically open the corresponding big-map scene before picking. This feature depends on SceneManager nodes. If disabled, make sure the player is already in the correct big-map scene.

- `no_zoom`: Boolean, default `false`. Whether to disable automatic zoom adjustment (which adjusts the big-map zoom to a suitable range). Disabling this may reduce the success rate of this node.

#### Example Usage

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerBigMapPick",
        "custom_action_param": {
            "map_name": "map02_lv002",
            "target": [
                585.8,
                825.5
            ],
            "on_find": "Teleport"
        }
    }
}
```

### Recognition: MapTrackerAssertLocation

✅Judges whether the player's current map name and position coordinates meet any of the expected conditions.

#### Node Parameters

Required parameters:

- `expected`: A list consisting of one or more conditions. Each condition object needs to contain the following fields:
    - `map_name`: The unique name of the expected map.
    - `target`: A list of 4 real-numbers `[x, y, w, h]`, representing the rectangular area where the expected coordinates are located.

<details>
<summary>Advanced Optional Parameters (Expand)</summary>

- `precision`: Same meaning as the `precision` parameter in the [MapTrackerInfer](./map-tracker%28advanced%29.md#recognition-maptrackerinfer) node.

- `threshold`: Same meaning as the `threshold` parameter in the [MapTrackerInfer](./map-tracker%28advanced%29.md#recognition-maptrackerinfer) node.

</details>

#### Example Usage

```json
{
    "MyNodeName": {
        "recognition": "Custom",
        "custom_recognition": "MapTrackerAssertLocation",
        "custom_recognition_param": {
            "expected": [
                {
                    "map_name": "map02_lv002",
                    "target": [
                        670,
                        350,
                        20,
                        20
                    ]
                }
            ]
        },
        "action": "DoNothing"
    }
}
```

## Tool Instructions

We provide a GUI tool script located at `/tools/map_tracker/map_tracker_editor.py`. It supports the following basic functions:

- **Create Move Node**: Visually draw [MapTrackerMove](#action-maptrackermove) path points on a map.
- **Create AssertLocation Node**: Draw a rectangle region on a map for [MapTrackerAssertLocation](#recognition-maptrackerassertlocation).
- **Import from Pipeline JSON**: Load the two node types above from an existing pipeline JSON file, edit them, and save directly back to the file.

### Environment Setup and Launch

Prepare a **Python runtime environment** and install the dependencies with the following command:

```bash
pip install opencv-python maafw
```

Then run the program with Python. The working directory must be the project root:

```bash
python tools/map_tracker/map_tracker_editor.py
```

### How to Use

🖱**Mouse operations**: Left click can add, move, or select path points; right click can pan the map; the mouse wheel can be used to zoom.

📷**Path recording**: In the path editing page, two recording modes are available: **Loop** (continuous recording) and **Once** (single-point recording). In Loop mode, pressing the record button will continuously record the player's path points; in Once mode, each press of the record button records only one path point.

> [!NOTE]
>
> To use path recording, make sure you have successfully set up the full environment according to the project's quick start guide.
>
> Path recording supports both Win32 and ADB controllers, with Win32 taking priority. The program will automatically detect the currently available game window and connect to it, so no manual selection is required.

↕️**Tier switching**: Some maps support tiers. You can view the different tiers in the Tiers List panel on the left.

👀**Point properties**: Click a path point to view its coordinate information, and you can delete it or copy its coordinates.

✅**Finish editing**: On the sidebar of any editing page, click the Finish button to choose an export method.

> [!TIP]
>
> If you are editing in "Import from existing node" mode, you can also click the Save button directly to save your changes to the file in one step!
