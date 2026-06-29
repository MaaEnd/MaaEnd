# Developer Manual - MapTracker Reference Documentation

## Introduction

This document describes how to use common nodes related to **MapTracker**.

**MapTracker** is a **minimap tracking system** based entirely on computer vision. It can infer the player's location based on the in-game minimap and can control the player to move along a specified set of waypoints.

### Key Concepts

1.  **Map Name**: Each major map in the game has a unique name, for example, "map01_lv001". Here, "map01" indicates the area is "Valley IV", and "lv001" indicates the sub-area is "Hub Zone". Please refer to `/assets/resource/image/MapTracker/map` to obtain all map names and images (these images have been scaled to fit the minimap UI in a 720P resolution game). `map_name` must **exactly match** the filename in this directory (excluding the `.png` extension).
2.  **Coordinate System**: The coordinates used by MapTracker are the pixel coordinates $(x, y)$ of the aforementioned major map image, with the top-left corner of the image as the origin $(0, 0)$.

> [!TIP]
>
> To learn more about the technical details, please read [this advanced documentation](./map-tracker%28advanced%29.md). It explains the usage of advanced programming nodes and maintenance methods for MapTracker.

## Node Descriptions

The following sections detail the specific usage of **common nodes** provided by MapTracker. These nodes are all of the Custom type and require specifying `custom_action` or `custom_recognition` within a pipeline to use them.

### Action: MapTrackerMove

🚶 Controls the player to move along specified waypoints.

> [!IMPORTANT]
>
> A **GUI tool** is provided in the repository, which can conveniently generate, import, and edit waypoints. Please refer to the [tool instructions](#tool-description) to learn how to maximize efficiency by using the tool.

#### Node Parameters

Required parameters:

- `map_name`: The unique name of the map. For example, "map01_lv001".
- `path`: A list of waypoints composed of several real-number coordinates. The player will move to these coordinate points sequentially.

Optional parameters:

- `no_print`: Boolean, default `false`. Whether to disable printing the pathfinding status UI message. To improve user experience, it is not recommended to disable this node's message printing.
- `path_trim`: Boolean, default `false`. Whether to select the waypoint closest to the character as the actual starting point when pathfinding begins (waypoints before this point will be automatically skipped); otherwise, it always starts moving from the first waypoint.
- `fine_approach`: String, default `"FinalTarget"`. Controls when to enable fine approach (reaching the target point with extreme precision). Optional values:

    | Option Value    | Meaning                                                       | Applicable Scenario                                                                          |
    | --------------- | ------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
    | `"FinalTarget"` | Enable fine approach only at the final target point (default) | Most scenarios                                                                               |
    | `"AllTargets"`  | Enable fine approach at every target point                    | When extremely high precision is required for transit points (e.g., crossing narrow bridges) |
    | `"Never"`       | Disable fine approach                                         | /                                                                                            |

- `on_finish`: Pipeline node object, defaults to not filled. Executes this Pipeline node once after successful pathfinding. For an example, refer to the Tip section of [MapTrackerToward](#action-maptrackertoward). The `pre_delay` and `post_delay` of the filled node default to `0` milliseconds if omitted.

<details>
<summary>Advanced Optional Parameters (Expand)</summary>

- `no_ensure_initial_movement_state`: Boolean, default `false`. Whether to skip the "sprint" preparation action before the first move begins. If enabled, it will proceed directly to the pathfinding process without actively resetting to a stable initial movement state.
- `no_ensure_final_orientation`: Boolean, default `false`. Whether to disable adjusting the player's orientation when reaching the last target point to ensure the camera faces the last direction of the path.
- `arrival_threshold`: Positive real number, default `2.5`. The distance threshold for judging arrival at the next target point, in pixel distance. A larger value makes it easier to be judged as having reached the target point, but may cause incomplete pathfinding; a smaller value requires more precise arrival at the target point, but may cause pathfinding difficulty.
- `arrival_timeout`: Positive integer, default `60000`. The time threshold for judging inability to reach the next target point, in milliseconds. If the next target point is not reached within this time, pathfinding fails immediately.
- `rotation_lower_threshold`: A real number in the range $(0, 180]$, default `7.5`. The directional angle deviation threshold for judging that fine-tuning of orientation is needed, in degrees.
- `rotation_upper_threshold`: A real number in the range $(0, 180]$, default `60.0`. The directional angle deviation threshold for judging that a major adjustment of orientation is needed, in degrees. In this case, the player will turn at a slower speed.
- `sprint_threshold`: Positive real number, default `10.0`. The distance threshold for performing a sprint action, in pixel distance. When the distance between the player and the next target point exceeds this value and the orientation is correct, the player will perform a sprint.
- `stuck_threshold`: Positive integer, default `2000`. The minimum duration for judging being stuck, in milliseconds. If the player still has no actual movement after this time, a stuck mitigation action will be triggered.
- `stuck_timeout`: Positive integer, default `10000`. The time threshold for judging inability to escape the stuck state, in milliseconds. If the stuck state is not escaped within this time, pathfinding fails immediately.
- `stuck_mitigators`: String list, default `["MoveOrDeleteDevice", "Jump"]`. When the player is judged to be stuck, the operations in the list are executed sequentially to attempt to escape the stuck state. Not allowing no operation; if this field is set to an empty list, the effect is the same as the default value. Available operations include:
    - `"Jump"`: Perform a jump action;
    - `"MoveOrDeleteDevice"`: Attempt to delete or move a device in front.

- `map_name_match_rule`: String, default `"^%s(_tier_\\w+)?$"`. Allows maps satisfying this expression to be used for pathfinding. `%s` will be replaced with the `map_name` parameter (and automatically regex-escaped). Typical values:
    - `^%s(_tier_\\w+)?$` (default): Allows this map and all its tiered maps to participate in pathfinding;
    - `^%s$`: Allows only this map to participate in pathfinding.

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
> Before executing this node, it is recommended to use the [MapTrackerAssertLocation](#recognition-maptrackerassertlocation) node to check if the player's **initial location** meets the requirements, in order to reach the first waypoint.

> [!WARNING]
>
> During the execution of this node, ensure the player is **always** in the specified map, and that **straight-line travel** is possible between adjacent waypoints.

### Action: MapTrackerGoal

🧭 Automatically plans a path based on NavMesh and controls the player to move to a specified target.

#### Working Principle

This node first identifies the player's current location, then reads the NavMesh road network data, temporarily connects the current location and the target point to the road network, plans a path using the Dijkstra algorithm, and finally delegates execution to [MapTrackerMove](#action-maptrackermove).

If a zipline policy is actively specified, it will also automatically scan for zipline points on the major map before pathfinding and incorporate ziplines into the pathfinding consideration.

#### Node Parameters

Required parameters:

- `map_name`: The unique name of the map. For example, "map02_lv002".
- `target` or `entity_id`: Choose one.
    - `target`: A list of 2 real numbers `[x, y]`, representing the target coordinate point.
    - `entity_id`: The entity ID associated with the NavMesh vertex.

Optional parameters:

- `zipline_policy`: String, default `"Never"`. Controls the aggressiveness of using ziplines. Optional values:

    | Option Value   | Meaning                                   | Applicable Scenario                                        |
    | -------------- | ----------------------------------------- | ---------------------------------------------------------- |
    | `"Never"`      | Never use ziplines (default)              | Most scenarios                                             |
    | `"Lazy"`       | Use ziplines only in extreme cases        | When needing to cross impassable areas like water          |
    | `"Active"`     | Actively use ziplines like a human player | When there are many impassable areas and the route is long |
    | `"Aggressive"` | Use ziplines very aggressively            | Generally not recommended                                  |

- Other parameters: Supports supplementing parameters of [MapTrackerMove](#action-maptrackermove), which will be passed through to the final movement process, such as `fine_approach`, `arrival_timeout`, `stuck_mitigators`, etc.

> [!TIP]
>
> If both `target` and `entity_id` are provided, the node will prioritize using `target` and will not throw an error.

#### Example Usage

Using coordinates as the target:

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerGoal",
        "custom_action_param": {
            "map_name": "map02_lv002",
            "target": [
                670.0,
                350.8
            ]
        }
    }
}
```

Using entity ID as the target:

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerGoal",
        "custom_action_param": {
            "map_name": "map02_lv002",
            "entity_id": 22800173539
        }
    }
}
```

> [!TIP]
>
> Entity information can be found in the [assets/data/ZmdMap/maaend_entities.json](/assets/data/ZmdMap/maaend_entities.json) file and cross-referenced using the [ZmdMap website](https://zmdmap.com).

> [!WARNING]
>
> During the execution of this node, ensure the player is **always** in the specified map, and that the target point is reachable via the corresponding NavMesh road network.

### Action: MapTrackerToward

➡️ Adjusts the player's orientation to face a specified angle or map point.

#### Node Parameters

Required parameters:

- `angle` or `target`: Choose one.
    - `angle`: Real number. The expected orientation angle, in degrees. Suitable for situations requiring a fixed angle value, offering the best robustness. 0° indicates due north, with clockwise rotation as the increasing direction. It can also be set to a negative number, representing a counterclockwise rotation angle.
    - `target`: A list of 2 real numbers `[x, y]`, representing the expected map coordinate point to face. Suitable for situations where the angle is not fixed or when needing to face a specific point. When choosing this parameter, the `map_name` parameter must also be provided.

Optional parameters:

- `map_name`: The unique name of the map. Required only in `target` mode; not needed in `angle` mode.

<details>
<summary>Advanced Optional Parameters (Expand)</summary>

- `rotation_threshold`: A real number in the range $(0, 180)$, default `12.0`. The directional angle deviation threshold for judging that the target orientation has been reached, in degrees.
- `map_name_match_rule`: Same meaning as the `map_name_match_rule` parameter in the [MapTrackerMove](#action-maptrackermove) node.

</details>

#### Example Usage

Face a specified angle (due east):

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerToward",
        "custom_action_param": {
            "angle": 90.0
        }
    }
}
```

Face a specified map point:

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerToward",
        "custom_action_param": {
            "map_name": "map02_lv002",
            "target": [
                670.0,
                350.8
            ]
        }
    }
}
```

> [!TIP]
>
> If you want to call this node immediately after successful pathfinding movement to adjust the player's orientation, a convenient way is to directly provide an `on_finish` parameter in [MapTrackerMove](#action-maptrackermove):
>
> ```json
> "on_finish": {
>     "action": "Custom",
>     "custom_action": "MapTrackerToward",
>     "custom_action_param": {
>         "angle": 90.0
>     }
> }
> ```

### Action: MapTrackerZipline

🎢 Makes the player on a zipline stand turn towards the next specified zipline stand, and automatically executes the zipline movement after alignment.

#### Node Parameters

Required parameters:

- `map_name`: The unique name of the map.
- `target`: The map coordinates `[x, y]` of the next zipline stand.

<details>
<summary>Advanced Optional Parameters (Expand)</summary>

- `rotation_threshold`: A positive real number in the range $(0, 180)$, default `9.0`. The directional angle deviation threshold for judging that the target zipline point orientation has been reached, in degrees.
- `timeout`: Positive integer, default `15000`. The timeout duration for turning towards the target zipline stand and for executing the zipline movement operation, in milliseconds.
- `map_name_match_rule`: Same meaning as the `map_name_match_rule` parameter in the [MapTrackerMove](#action-maptrackermove) node.

</details>

#### Example Usage

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerZipline",
        "custom_action_param": {
            "map_name": "map02_lv002",
            "target": [
                114.0,
                514.0
            ]
        }
    }
}
```

> [!TIP]
>
> This node operates entirely **on the zipline stand**. That is, when the node is called, the player is required to already be on a zipline stand; after the node executes, the player will not automatically leave the zipline stand.

> [!WARNING]
>
> If the target zipline stand is unreachable (zipline stand not powered, zipline stand does not exist, obstacles blocking), this node will immediately return failure.

### Recognition: MapTrackerAssertLocation

✅ Judges whether the player's current map name and location coordinates meet any of the expected conditions.

#### Node Parameters

Required parameters:

- `expected`: A list composed of one or more conditions. Each condition object needs to include the following fields:
    - `map_name`: The unique name of the expected map.
    - `target`: A list of 4 real numbers `[x, y, w, h]`, representing the rectangular region where the expected coordinates are located.

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

### Recognition: MapTrackerBigMapFindImage

🔍 Finds the location of a specified icon in the big map interface via template matching.

#### Node Parameters

Required parameters:

- `template`: Path to the template image. Note that this path is relative to the `assets/resource` directory, for example, `image/MapTracker/BigMapIcons/Pointer.png` (player pointer icon).
- `expected`: Boolean, non-negative integer, or condition object. Controls the conditions for a hit recognition, with specific meanings as follows:
    - If it is the boolean `true`, it means finding at least one match result is sufficient for a hit recognition;
    - If it is the boolean `false`, it means finding no match results is required for a hit recognition;
    - If it is a non-negative integer `n`, it means matching exactly `n` results is required for a hit recognition;
    - If it is an object `{"map_name": "...", "target": [x, y, w, h]}`, it means having at least one match result within the specified map's rectangular coordinate region is required for a hit recognition.

Optional parameters:

- `threshold`: A real number in the range $(0, 1]$, default `0.5`. Match confidence threshold. Match results below this value will be ignored.
- `green_mask`: Boolean, default `false`. Whether to enable green masking for the template image.
- `with_rotation`: Boolean, default `false`. Whether to enable arbitrary angle matching, suitable for situations requiring matching rotated icons (e.g., player pointer).
- `zoom_value`: A real number in the range $[0, 1]$, default `0`. Adjust the big map zoom slider to this position before starting the match. If set to `0` (default), it means no adjustment of the zoom slider will be made.
- `map_name_regex`: String, defaults to not filled. Restricts the range of candidate maps for big map inference. Only set when map misidentification may occur, for example, `"^map02_lv002$"` will lock the inference to only occur within "map02_lv002".

<details>
<summary>Advanced Optional Parameters (Expand)</summary>

- `max_matches`: Integer, default `32`. Controls the maximum number of match results. This parameter generally does not need adjustment.
- `must_see_points`: A list of waypoints composed of several real-number coordinates, defaults to not filled. Specifies map coordinate points that the map viewport must cover during the matching process. If this parameter is filled, the map viewport will be automatically dragged during matching until all specified coordinate points have appeared in the viewport. This parameter is suitable for large-scale matching across a large area but will significantly increase matching time.

</details>

#### Example Usage

The following demonstrates how to determine whether a "blue task location marker" is within a certain area of the map:

```json
{
    "MyFindImageNode": {
        "recognition": "Custom",
        "custom_recognition": "MapTrackerBigMapFindImage",
        "custom_recognition_param": {
            "template": "image/SeizeDeliveryJobs/BlueTaskLocation.png",
            "expected": {
                "map_name": "map02_lv005",
                "target": [
                    114,
                    514,
                    19,
                    19
                ]
            },
            "green_mask": true,
            "zoom_value": 0.25
        },
        "action": "DoNothing"
    }
}
```

> [!TIP]
>
> Of course, calling this node on the Go side can provide richer return information. Please refer to this node's Go code for the specific format of the returned results.

### Action: MapTrackerBigMapPick

🫳 Drags the view in the big map interface until the specified point appears, allowing subsequent click operations.

#### Node Parameters

Required parameters:

- `map_name`: The unique name of the map. For example, "map01_lv001".
- `target`: A list of 2 real numbers `[x, y]`, representing the target coordinate point.

Optional parameters:

- `on_find`: The operation to perform after finding the target point. Default is `"Click"`. Optional values are:
    - `"Click"`: Click the target point (default);
    - `"Teleport"`: Perform a teleportation operation (requires the target point to be a teleport waypoint);
    - `"DoNothing"`: Perform no operation.
- `auto_open_map_scene`: Boolean, default `false`. Whether to automatically open the corresponding big map interface beforehand. This function relies on the SceneManager series of nodes. If this function is not enabled, please confirm that the player is currently in the corresponding big map interface.
- `zoom_value`: Controls the automatic zoom adjustment behavior before searching for the target point. For details, refer to the `zoom_value` parameter of the [MapTrackerBigMapZoom](#action-maptrackerbigmapzoom) node. If not filled, the default value is 0.725.

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

### Action: MapTrackerBigMapZoom

🔍 Adjusts the zoom slider to a specified position in the big map interface.

#### Node Parameters

Required parameters:

- `zoom_value`: A real number in the range $[0, 1]$. If set to `0` or not filled, it means disabling zoom adjustment (nothing will happen). Other non-zero values indicate the click position of the big map zoom slider; closer to `0` means closer to the nearest view (maximum zoom), and `1` is the farthest view (minimum zoom).

#### Example Usage

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerBigMapZoom",
        "custom_action_param": {
            "zoom_value": 0.7
        }
    }
}
```

## Tool Description

We provide a GUI tool script located at `/tools/map_tracker/map_tracker_editor.py`. It supports the following basic functions:

- **Create Move Node**: Visually draw [MapTrackerMove](#action-maptrackermove) waypoints on the map.
- **Create AssertLocation Node**: Select a rectangular area on the map for [MapTrackerAssertLocation](#recognition-maptrackerassertlocation).
- **Edit Existing Node (Import from Pipeline JSON)**: Load the above two types of nodes from an existing pipeline JSON file, make modifications, and save directly to the file!

### Environment Setup and Opening Method

Prepare a **Python runtime environment** and **install the dependency libraries** using the following command:

```bash
pip install opencv-python maafw
```

Then run the program using Python (the working directory needs to be the project root directory):

```bash
python tools/map_tracker/map_tracker_editor.py
```

### Usage Introduction

🖱**Mouse Operations**: The left button can add, move, or select waypoints; the right button can drag the map; the scroll wheel can be used for zooming.

📷**Path Recording**: In the path editing page, there are two modes for recording paths: **Loop (continuous recording) and Once (single-point recording) modes**. In Loop mode, pressing the record button will continuously record the player's waypoints; in Once mode, pressing the record button each time will only record one waypoint.

> [!NOTE]
>
> To use the path recording function, you need to ensure that you have successfully set up the entire environment according to the project's quick start guide.
>
> The path recording function supports both Win32 and ADB controllers (Win32 is prioritized). The program will automatically detect the currently available game window and connect automatically, without manual selection.

↕️**Layer Switching**: Some maps have layer functionality. You can view maps of different layers in the Tiers List panel on the left.

👀**Point Property Viewing**: Clicking on a waypoint allows you to view its coordinate information and perform operations like deleting or copying coordinates.

✅**Finish Editing**: In the sidebar of any editing page, clicking the Finish button allows you to choose the export method.

> [!TIP]
>
> If you are editing in the "Edit Existing Node" mode, you can also directly click the Save button to save the changes to the file with one click
