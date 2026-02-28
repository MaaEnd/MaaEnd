# Development Guide - MapTracker Reference Document

## Introduction

This document describes how to use nodes related to MapTracker.

**MapTracker** is a computer vision-based **minimap tracking system** that can infer the player's position based on the minimap in the game and control the player to move according to specified waypoints.

### Key Concepts

1. **Map Name**: Each large map has a unique name in the game, e.g., "map001_lv001", where "map001" indicates the region is "Fourth Valley" and "lv001" indicates the sub-region is "Hub Area". Please check `/assets/resource/image/MapTracker/map` to get all map names and images (these images have been scaled to fit the minimap UI in the game with 720P resolution).
2. **坐标系统***Coordinate System**: The coordinates used by MapTracker are the pixel coordinates $(x, y)$ of the above large map images, with the upper-left corner of the image as the origin $(0, 0)$.

## Node Descriptions

The following details the specific usage of the nodes provided by MapTracker. These nodes are all Custom type nodes and need to specify `custom_action` or `custom_recognition` in the pipeline to use.

### Action: MapTrackerMove

🚶Controls the player to move along the specified waypoints.

> [!IMPORTANT]
>
> A **GUI tool** is provided in the repository to easily generate, import, and edit waypoints. Please refer to [Tool Instructions](#tool-instructions) to learn how to maximize the use of the tool to improve efficiency.

#### Node Parameters

Required parameters:

- `map_name`: The unique name of the map. E.g., "map001_lv001".

- `path`: A list of waypoints consisting of several coordinates. The player will move to these coordinate points in sequence.

Optional parameters:

- `no_print`: Boolean value, default `false`. Whether to turn off UI message printing of pathfinding status. For better user experience, it is not recommended to turn off message printing for this node.

<details>
<summary>Advanced Optional Parameters (Expand)</summary>

- `path_trim`: Boolean value, default `false`. When enabled, before moving, it first calculates the closest distance from the current position to each point in the path, takes the closest waypoint as the new starting point, and the previous waypoints will be skipped automatically; when disabled, it always starts from the first waypoint.
- `arrival_threshold`: Positive real number, default `3.5`. The distance threshold for judging arrival at the next target point, in pixel distance. A larger value makes it easier to be judged as arriving at the target point but may result in incomplete pathfinding; a smaller value requires more precise arrival at the target point but may make pathfinding difficult to complete.
- `arrival_timeout`: Positive integer, default `60000`. The time threshold for judging failure to reach the next target point, in milliseconds. If the next target point is not reached after this time, pathfinding fails immediately.
- `rotation_lower_threshold`: Real number between $(0, 180]$, default `8.0`. The direction angle deviation threshold for judging the need for fine-tuning the orientation, in degrees.
- `rotation_upper_threshold`: Real number between $(0, 180]$, default `60.0`. The direction angle deviation threshold for judging the need for large-scale orientation adjustment. At this time, the player will stop, gradually adjust the orientation, and then continue moving.
- `rotation_speed`: Positive real number, default `2.0`. The rotation speed multiplier when the player adjusts the orientation, in pixels per degree. A larger value adjusts the orientation faster but may cause over-adjustment and repeated adjustments; a smaller value adjusts the orientation smoothly but may cause delayed adjustment.
- `rotation_timeout`: Positive integer, default `30000`. The time threshold for judging failure to adjust the orientation, in milliseconds. If the orientation is not adjusted properly after this time, pathfinding fails immediately.
- `sprint_threshold`: Positive real number, default `25.0`. The distance threshold for performing the sprint action, in pixel distance. When the distance between the player and the next target point exceeds this value and the orientation is correct, the player will perform a sprint.
- `stuck_threshold`: Positive integer, default `1500`. The minimum duration for judging being stuck, in milliseconds. If the player does not actually move after this period of time, automatic jumping will be triggered.
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
                    688,
                    350
                ],
                [
                    679,
                    358
                ],
                [
                    670,
                    350
                ]
            ]
        }
    }
}
```

> [!TIP]
> Before executing this node, it is recommended to use the [MapTrackerAssertLocation](#recognition-maptrackerassertlocation) node to check whether the player's **initial position** meets the requirements to reach the first waypoint.

> [!WARNING]
>
> During the execution of this node, ensure that the player is **always in** the specified map, and adjacent waypoints **can be reached in a straight line**.

### Recognition: MapTrackerInfer

📍Gets the player's current map name, position coordinates, and orientation.

#### Node Parameters

Required parameters: None

Optional parameters:

- `map_name_regex`: A [regular expression](https://regexr.com/) used to filter map names. Only maps matching this regular expression will participate in recognition. For example:

    - `^map\\d+_lv\\d+$`: Default value. Matches all regular maps.
    - `^map\\d+_lv\\d+(_tier_\\d+)?$`: Matches all regular maps and tiered maps (Tier).
    - `^map001_lv001$`: Only matches "map001_lv001" (Fourth Valley - Hub Area).
    - `^map001_lv\\d+$`: Matches all sub-regions of "map001" (Fourth Valley).

- `print`: Boolean value, default `false` . Whether to enable UI message printing of recognition results.

<details>
<summary>Advanced Optional Parameters (Expand)</summary>

- `precision`: Real number between $(0, 1]$, default `0.4`. Controls the accuracy of matching. A larger value will match map features more strictly but may result in slow matching speed; a smaller value will greatly improve matching speed but may lead to incorrect results. When the number of maps to be matched is small (e.g., only one map), it is recommended to use a larger value to obtain more accurate results.

- `threshold`: Real number between $(0, 1]$, default `0.4` Controls the confidence threshold for matching. Matching results below this value will not hit the recognition.

</details>

#### Example Usage

```json
{
    "MyNodeName": {
        "recognition": "Custom",
        "custom_recognition": "MapTrackerInfer",
        "custom_recognition_param": {
            "map_name_regex": "^map\\d+_lv\\d+$"
        },
        "action": "DoNothing"
    }
}
```

> [!TIP]
>
> MapTracker uses an integer between $[0, 360)$ to represent the player's **orientation**, in degrees. 0° indicates facing due north, with clockwise rotation as the increasing direction.

> [!WARNING]
>
> This node is not suitable for low-code development in the pipeline. If you need to judge whether the player's current position meets the conditions, please use the [MapTrackerAssertLocation](#recognition-maptrackerassertlocation) node.

### Recognition: MapTrackerAssertLocation

✅Judges whether the player's current map name and position coordinates meet any of the expected conditions.

#### Node Parameters

Required parameters:

- `expected`: A list consisting of one or more conditions. Each condition object needs to contain the following fields:
    - `map_name`: The unique name of the expected map.
    - `target`: A list of 4 integers `[x, y, w, h]`, representing the rectangular area where the expected coordinates are located.

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
                    "map_name": "map002_lv002",
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

1. **Create Path**: Select a map, then create waypoints on the map with the mouse, and finally export the waypoint list or node JSON.
2. **Edit Path**: Load waypoints from an existing pipeline JSON file, modify them, and save or export them again.

Simply install Python and the `opencv-python` library, then run the above tool script with Python. After running, follow the instructions in the console to operate.

### Specific Usage of Path Editing

**Mouse Operations**: Left-click to add, move, or delete waypoints; right-click to drag the map; scroll the mouse wheel to zoom.

**Common Buttons**:

- The Save button is only available when editing an existing path. Clicking it will save the modifications back to the original JSON file where the pipeline is located.
- The Finish button will end the editing (equivalent to closing the window). At this time, the console will ask you to enter an export mode. There are multiple export modes to choose from, such as exporting JSON nodes or waypoint lists.
- The Get Realtime Location button will attempt to connect to the positioning service and identify the current coordinates in the game. See the instructions below for how to enable the positioning service.

**Positioning Service**:

To use the real-time positioning function, use the [Maa Pipeline Support](https://marketplace.visualstudio.com/items?itemName=nekosu.maa-support) VS Code extension to "execute" the `MapTrackerTestLoop` node located in `/assets/resource/pipeline/MapTracker.json` . Ensure that the game window can be correctly captured by Maa and that the node can run normally.

Then you can use the Get Realtime Location button to get the player's current coordinates in the game.
