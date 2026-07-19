# Developer Guide - MapLocator Minimap Localization System

MapLocator is a native C++ minimap localization system that combines AI inference with traditional computer vision algorithms. It outputs the map area the character is currently in (ZoneID), the global pixel coordinates `(x, y)`, and the orientation angle.

It is also the position source for [MapNavigator](./map-navigator.md) — every position judgment made during navigation comes from MapLocator's per-frame recognition.

- [MapLocateRecognition](#maplocaterecognition)
- [MapLocateAssertLocation](#maplocateassertlocation)
- [How Localization Works](#how-localization-works)

MapLocator belongs to the Recognition layer and is only responsible for coordinate and orientation recognition; it does not take over game control. To make the character move to a specified coordinate, use [MapNavigator](./map-navigator.md), which already implements pathfinding and movement control on top of MapLocator; alternatively, you can write your own control logic based on the `out_detail` output.

---

## MapLocateRecognition

Retrieves the zone name (ZoneID) where the character is currently located, along with exact coordinates and rotation.

Each call processes a single frame. The locator keeps its tracking state across calls: a regular call only searches locally near the previous position, and tracking is declared lost after `max_lost_frames` consecutive frames without a valid result, falling back to a global search. For continuous tracking, invoke this node repeatedly at a high frequency through the Pipeline's loop mechanism (e.g., linking `next` back to itself).

### Node Parameters

No required parameters. Optional parameters (`custom_recognition_param`):

| Parameter             | Default | Description                                                                                                                                                              |
| --------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `loc_threshold`       | `0.55`  | Lower bound of the template matching score. Lower it appropriately (e.g., `0.45`) when heavy interference causes frequent localization loss                              |
| `yolo_threshold`      | `0.70`  | Minimum confidence for YOLO to accept a minimap area. Setting it too low may misidentify other disc-shaped UIs as the minimap                                            |
| `force_global_search` | `false` | Set to `true` after cross-region teleports, respawns, or long screen switches to abandon tracking and re-lock via a full-map search                                      |
| `max_lost_frames`     | `3`     | Consecutive frames without a valid result before tracking is declared lost. Raising it tolerates brief occlusion but also lengthens how long an incorrect state persists |
| `expected_zone_id`    | Empty   | When non-empty, only localization results in this zone are accepted. Useful when the character's area is already known                                                   |

### Return Value (out_detail)

| Field       | Description                                                        |
| ----------- | ------------------------------------------------------------------ |
| `status`    | Status code, see the table below                                   |
| `message`   | Failure reason or debug information                                |
| `mapName`   | (On success) The localized zone name, e.g., `map01_lv001`          |
| `x` / `y`   | (On success) Global pixel coordinates                              |
| `rot`       | (On success) Orientation yaw angle, 0°–360°, north as zero         |
| `locConf`   | Confidence score of this hit, for reference when tuning parameters |
| `latencyMs` | Time consumed by this calculation (milliseconds)                   |

`status` values:

| Value | Enum            | Meaning                                                                                              |
| ----- | --------------- | ---------------------------------------------------------------------------------------------------- |
| `0`   | `Success`       | Localization succeeded                                                                               |
| `1`   | `TrackingLost`  | Tracking lost; falling back to a global search still produced no match                               |
| `2`   | `ScreenBlocked` | The frame is occluded over a large area; no valid features can be extracted                          |
| `3`   | `Teleported`    | Displacement between two frames exceeds the normal movement speed limit, judged as a forced teleport |
| `4`   | `YoloFailed`    | YOLO pre-filtering determined that the current frame contains no minimap area                        |

### Examples

Minimal invocation (in most cases no parameters are needed; the node determines the zone through YOLO and enters tracking automatically):

```json
{
    "MyLocateTask": {
        "recognition": "Custom",
        "custom_recognition": "MapLocateRecognition",
        "action": "DoNothing"
    }
}
```

Overriding parameters (e.g., forcing a global search after a long-distance teleport):

```json
{
    "MyLocateTask": {
        "recognition": "Custom",
        "custom_recognition": "MapLocateRecognition",
        "custom_recognition_param": {
            "loc_threshold": 0.55,
            "yolo_threshold": 0.7,
            "force_global_search": true
        },
        "action": "DoNothing"
    }
}
```

---

## MapLocateAssertLocation

Checks whether the character is currently inside a specified rectangle within a given `zone_id`.

Unlike the single-frame check of `MapLocateRecognition`, this node performs a **settled determination**: it resets the tracking state, forces a global search, then polls at 250ms intervals (up to 60 frames, about 15 seconds). It requires 3 consecutive frames with successful localization, a matching zone, and positions stable within a 12px radius, and finally checks the centroid coordinate against the rectangle. A call may therefore block for several seconds — it is not instantaneous.

### Node Parameters

Required parameters (`custom_recognition_param`):

| Parameter | Description                                                           |
| --------- | --------------------------------------------------------------------- |
| `zone_id` | Target zone name; must exactly match the localized zone name          |
| `target`  | Array of 4 numbers `[x, y, w, h]`: rectangle top-left corner and size |

Optional parameters (`custom_recognition_param`):

| Parameter        | Default | Description                                                                                                                      |
| ---------------- | ------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `loc_threshold`  | `0.70`  | Lower bound of the template matching score, same meaning as above; note the default is higher than for single-frame localization |
| `yolo_threshold` | `0.70`  | Same meaning as above                                                                                                            |

The assertion always forces a global search and only accepts localization results inside `zone_id`; no search-scope configuration is needed.

### Return Value (out_detail)

| Field       | Description                                                      |
| ----------- | ---------------------------------------------------------------- |
| `status`    | Localization status code, same meaning as `MapLocateRecognition` |
| `matched`   | `true` only when both the zone and the rectangle match           |
| `inTarget`  | Equivalent to `matched`                                          |
| `message`   | Localization log or failure reason                               |
| `zoneId`    | The target zone name required by this assertion                  |
| `x` / `y`   | (On success) Centroid coordinates of the stable window           |
| `rot`       | (On success) Orientation yaw angle                               |
| `locConf`   | Confidence score of this hit                                     |
| `latencyMs` | Time consumed by this calculation (milliseconds)                 |
| `target`    | Echoes the `[x, y, w, h]` rectangle used for this assertion      |

### Example

```json
{
    "WulingBaseAssert": {
        "recognition": "Custom",
        "custom_recognition": "MapLocateAssertLocation",
        "custom_recognition_param": {
            "zone_id": "Wuling_Base",
            "target": [
                605,
                878,
                60,
                20
            ]
        },
        "action": "DoNothing"
    }
}
```

> [!TIP]
>
> This node works well as an entry guard before `MapNavigateAction`: confirm the character is in the expected area first, then start the navigation. The companion tool's `Assert Mode` can export this node by dragging a rectangle directly; see [MapNavigator](./map-navigator.md).

---

## How Localization Works

This section is for readers who want to understand the internals; it is not required for everyday use.

1. **Native C++ image pipeline**: integrated into the Pipeline as an independent `cpp-algo` process. Image processing is built on C++ / OpenCV and optimized for memory copying, keeping latency at millisecond level even with YOLO inference included.
2. **YOLO pre-filtering**: judges by confidence whether a valid minimap area exists in the current frame, filtering out abnormal frames such as full-screen menus and effect occlusion.
3. **Gradient-domain ZNCC matching**: gradient features are extracted for semi-transparent UI stacking scenarios, paired with ZNCC (Zero-mean Normalized Cross-Correlation) template matching. Matching relies mainly on edge and contour features, staying stable when skill effects flash or the UI changes.
4. **MotionTracker motion prediction**: infers the search range for the current frame from historical movement speed instead of searching globally every frame, which improves speed and avoids matching distant areas that look similar but are not actually reachable.

> [!IMPORTANT]
>
> The per-frame recognition result is the only source of position truth. When localization is briefly lost, you **should not** derive a "virtual position" from motion prediction to fill the gap — this lets the upper layer keep acting without any actual observation, and the error accumulates with no way to self-correct. The correct approach is to rely on the `max_lost_frames` transition and the global search recovery mechanism.
