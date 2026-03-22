# Development Guide - MapTracker Reference Document

## Introduction

This document describes how to use nodes related to MapTracker.

**MapTracker** is a computer vision-based **minimap tracking system** that can infer the player's position based on the minimap in the game and control the player to move according to specified waypoints.

### Key Concepts

1. **Map Name**: Each large map has a unique name in the game, e.g., "map01_lv001", where "map01" indicates the region is "Fourth Valley" and "lv001" indicates the sub-region is "Hub Area". Please check `/assets/resource/image/MapTracker/map` to get all map names and images (these images have been scaled to fit the minimap UI in the game with 720P resolution). The `map_name` must **exactly match** the filename (without the `.png` extension) in that directory.
2. **Coordinate System**: The coordinates used by MapTracker are the pixel coordinates $(x, y)$ of the above large map images, with the upper-left corner of the image as the origin $(0, 0)$.

## Node Descriptions

The following details the specific usage of the nodes provided by MapTracker. These nodes are all Custom type nodes and need to specify `custom_action` or `custom_recognition` in the pipeline to use.

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
    | `"Never"`       | Disable fine-approach                                          | When you have explicit camera-orientation requirements after arrival       |

<details>
<summary>Advanced Optional Parameters (Expand)</summary>

- `arrival_threshold`: Positive real number, default `2.5`. The distance threshold for judging arrival at the next target point, in pixel distance. A larger value makes it easier to be judged as arriving at the target point but may result in incomplete pathfinding; a smaller value requires more precise arrival at the target point but may make pathfinding difficult to complete.

- `arrival_timeout`: Positive integer, default `60000`. The time threshold for judging failure to reach the next target point, in milliseconds. If the next target point is not reached after this time, pathfinding fails immediately.

- `map_name_match_rule`: String, default `"^%s(_tier_\\w+)$"`. Used to generate `map_name_regex` for `MapTrackerInfer`. `%s` will be replaced by `map_name` (with regex escaping). The default rule matches map names like `map02_lv003_tier_xxx`.

- `rotation_lower_threshold`: Real number between $(0, 180]$, default `7.5`. The direction angle deviation threshold for judging the need for fine-tuning the orientation, in degrees.

- `rotation_upper_threshold`: Real number between $(0, 180]$, default `60.0`. The direction angle deviation threshold for judging the need for large-scale orientation adjustment. At this time, the player will slow down to adjust orientation.

- `sprint_threshold`: Positive real number, default `20.0`. The distance threshold for performing the sprint action, in pixel distance. When the distance between the player and the next target point exceeds this value and the orientation is correct, the player will perform a sprint.

- `stuck_threshold`: Positive integer, default `2500`. The minimum duration for judging being stuck, in milliseconds. If the player does not actually move after this period of time, automatic jumping will be triggered.
- `stuck_timeout`: Positive integer, default `10000`. The time threshold for judging failure to get out of the stuck state, in milliseconds. If the stuck state is not escaped after this time, pathfinding fails immediately.

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

- `precision`: Same meaning as the `precision` parameter in the [MapTrackerInfer](#recognition-maptrackerinfer) node.

- `threshold`: Same meaning as the `threshold` parameter in the [MapTrackerInfer](#recognition-maptrackerinfer) node.

- `fast_mode`: Boolean value, default `false`. Controls whether to enable fast matching mode to further improve recognition speed. Unless encountering performance bottlenecks, it is not recommended to enable this mode.

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

### Recognition: MapTrackerInfer

📍Gets the player's current map name, position coordinates, and orientation.

#### Node Parameters

Required parameters: None

Optional parameters:

- `map_name_regex`: A [regular expression](https://regexr.com/) used to filter map names. Only maps matching this regular expression will participate in recognition. For example:

    - `^map\\d+_lv\\d+$`: Default value. Matches all regular maps.
    - `^map\\d+_lv\\d+(_tier_\\d+)?$`: Matches all regular maps and tiered maps (Tier).
    - `^map01_lv001$`: Only matches "map01_lv001" (Fourth Valley - Hub Area).
    - `^map01_lv\\d+$`: Matches all sub-regions of "map01" (Fourth Valley).

- `print`: Boolean value, default `false`. Whether to enable UI message printing of recognition results.

<details>
<summary>Advanced Optional Parameters (Expand)</summary>

- `precision`: Real number between $(0, 1]$, default `0.5`. Controls the accuracy of matching. A larger value will match map features more strictly but may result in slow matching speed; a smaller value will greatly improve matching speed but may lead to incorrect results. When the number of maps to be matched is small (e.g., only one map), it is recommended to use a larger value to obtain more accurate results.

- `threshold`: Real number between $(0, 1]$, default `0.4`. Controls the confidence threshold for matching. Matching results below this value will not hit the recognition.

</details>

<br>

> [!TIP]
>
> MapTracker uses an integer between $[0, 360)$ to represent the player's **orientation**, in degrees. 0° indicates facing due north, with clockwise rotation as the increasing direction.

> [!WARNING]
>
> This node is designed for advanced programming, so it is not suitable for low-code development in the pipeline. If you need to judge whether the player's current position meets the conditions, please use the [MapTrackerAssertLocation](#recognition-maptrackerassertlocation) node.

### Recognition: MapTrackerBigMapInfer

🗺️ Infers the map coordinate of the current viewport region on the big map and the current map scale.

> [!WARNING]
>
> This node is designed for advanced programming, so it is not suitable for low-code development in the pipeline. For the exact cropping rule of the "current viewport region", refer to the implementation details in code.

#### Node Parameters

Please refer to the `MapTrackerBigMapInferParam` type definition in code.

## Tool Instructions

We provide a GUI tool script located at `/tools/map_tracker/map_tracker_editor.py`. It supports the following basic functions:

1. **Create Move Node**: Visually draw [MapTrackerMove](#action-maptrackermove) path points on a map.
2. **Create AssertLocation Node**: Draw a rectangle region on a map for [MapTrackerAssertLocation](#recognition-maptrackerassertlocation).
3. **Import from Pipeline JSON**: Load either of the two node types above from an existing pipeline JSON file, edit them, and save directly back to the file.

Simply install Python and the `opencv-python` package, then run the script with Python and follow the GUI instructions.

### Specific Usage of Path Editing

**Mouse Operations**: Left-click to add, move, or delete waypoints; right-click to drag the map; scroll the mouse wheel to zoom.

**Common Buttons**:

- The Save button is only available when editing an existing path. Clicking it will save the modifications back to the original JSON file where the pipeline is located.
- The Finish button ends editing. Then you can choose an export mode (for example, export JSON node text or a raw point list).
- The Record Realtime Path button tries to connect to the location service and records in-game coordinates over time. See below for how to enable the service.

**Positioning Service**:

To use real-time path recording, use the [Maa Pipeline Support](https://marketplace.visualstudio.com/items?itemName=nekosu.maa-support) VS Code extension to run the `MapTrackerTestLoop` node in `/assets/resource/pipeline/MapTracker.json`. Make sure Maa can capture the game window correctly (not blocked by other windows).

While that node is running, you can:

1. Click the Record Realtime Path button in the sidebar.
2. Move your character manually in-game; the tool records the traversed route automatically.
3. Return to the tool and click Stop Path Recording when finished.
4. Convert the recorded route in the operation area at the bottom into a path usable by MapTracker.

This workflow can significantly improve both efficiency and precision when creating path points.
