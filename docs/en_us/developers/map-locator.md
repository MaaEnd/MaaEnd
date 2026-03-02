# Developer Guide - MapLocator Minimap Localization System

## Introduction

This document explains how to use nodes related to **MapLocator**.

**MapLocator** is a next-generation minimap localization system developed in native C++, deeply integrating AI and traditional computer vision algorithms. It can robustly output the map area the player is currently in (usually the map name/ZoneID), the global pixel coordinates $(x, y)$, and the current rotation angle, even under severe visual interference like heavy skill effects. This system is designed for extreme scenarios involving heavy UI occlusion and intense lighting changes, achieving millisecond-level precision localization with extremely low system overhead.

### Scope and Limitations

MapLocator acts strictly as the "bright eyes of the system"—providing high-frequency, high-precision **coordinate and rotation recognition**, and belongs to the Recognition layer. To achieve actions like "making the character run to a specified coordinate", you need to integrate a pathfinding algorithm (e.g., A\*) in the outer layer and issue control codes for physical operations (like mouse rotation, keyboard movement) through the Action layer. This module does not automatically take over game control.

### MapLocator Core Architecture

1. **Native Compute Advantage and OpenCV Integration**
   This system runs as an independent `cpp-algo` process component connected to the Pipeline. Thanks to the native C++ and OpenCV underlying image pipeline and highly optimized memory processing, it maintains extremely low runtime latency even with the inclusion of YOLO model inference.
2. **Robust Dynamic Environment Awareness with YOLO Pre-filtering**
   The system deploys an independently trained YOLOv26-small model as a pre-validation stream for scene recognition. Based on the confidence score, the model can directly confirm whether a valid minimap area (Minimap ROI) exists in the current frame, quickly filtering out disturbances caused by abnormal scenes such as full-screen menus or heavy particle occlusion, significantly reducing false positive rates.
3. **Gradient-Domain ZNCC Matching and Interference Resistance**
   Built into the system is a gradient feature extraction and ZNCC (Zero-mean Normalized Cross-Correlation) template matching mechanism optimized directly for multi-layer Alpha rendering (semi-transparent UI stacking). It maintains high matching robustness relying on edge features and contour weights when encountering flashing skill effects or drastic UI changes.
4. **Tracking Optimization via Internal MotionTracker Engine**
   The system internally records and analyzes the historical movement speed of the character. When proceeding to the next frame recognition, the algorithm will not blindly perform a global search. Instead, it estimates a reasonable movement radius (Search Bounds) for the character at that moment based on the instantaneous speed calculated from previous frames. This narrows the template matching scope, greatly boosting computation speed, and avoids erroneously matching distant but remarkably similar color blocks.

---

## Node Description

The following sections detail the usage of the nodes provided by MapLocator. This node is designed as a `Custom` type in MAA and can be directly embedded into the Pipeline. It is highly recommended that the subsequent control/Action layer directly capture the high-precision numerical values from its output (`out_detail`) to determine movement logic.

### custom_recognition: MapLocateRecognition

Retrieves the large map zone name (ZoneID or mapName) where the player is currently located, along with exact coordinates and rotation.

#### Node Parameters

**Run Mechanism**: Calling the `MapLocateRecognition` node will process **one and only one frame (the current valid screenshot)** of minimap calculation and return the coordinates. If you need continuous, real-time tracking like a "radar," you must wrap it using the Pipeline's loop mechanism (e.g., using `next` to loop the node itself or nesting it in a loop node) to invoke it at a high frequency and provide a continuous stream of data.

**Required Parameters**: **None**

**Optional Parameters (`custom_recognition_param`)**:
Supports passing advanced parameter settings in JSON string format to override defaults and specifically fine-tune the locator for extreme or specialized scenarios.

- `loc_threshold`: A floating-point number in the range $[0, 1]$, default is `0.55`. Controls the most lenient primary score threshold for image matching features. If the environment is exceedingly complex and causes frequent unexpected tracking losses, you can appropriately lower this (e.g., `0.45`); in normal situations, it's recommended to keep the default or increase it slightly to guarantee precision.
- `yolo_threshold`: A floating-point number in the range $[0, 1]$, default is `0.70`. The minimum confidence threshold for YOLO to judge map UI categories. A value too low may misidentify UIs similar to mini-maps (like circular menus).
- `force_global_search`: Boolean, default is `false`. For extreme optimization, daily tracking only follows the area matching the player's last location (Local Search). However, when experiencing massive region transitions, respawn teleports, or recovering from a long screen freeze, you should set this to `true` to force a full-map global scan to lock the position.
- `max_lost_frames`: Integer, default is `3`. Defines how many continuous frames the system is allowed to fail a valid result detection before officially declaring "Tracking Lost". Increasing this value enhances transition capabilities through brief UI blockages but simultaneously extends the false-lock duration.

#### Return Value Structure (Out Detail)

Upon successful localization or termination of detection, the node will output complete status feedback in a JSON structure to the `out_detail` pipeline and the console. This can be used for complex business flow routing or error handling:

- `status`: [Internal status code enumeration]
    - `0 (Success)`: Successfully localized.
    - `1 (TrackingLost)`: Tracking dropped (global burst search also yielded no results).
    - `2 (ScreenBlocked)`: Image overwhelmed/occluded by unknown, massive obstructions.
    - `3 (Teleported)`: Coordinate displacement between two frames severely exceeded human/vehicle movement limits, deduced as a forced teleportation.
    - `4 (YoloFailed)`: The initial YOLO model confirmed the current game screenshot does not contain a minimap recognition area.
- `mapName`: (On Success) The large map area name with the highest localization matching degree (e.g., "map001_lv001").
- `x`: (On Success) The global pixel coordinate on the X-axis (horizontal).
- `y`: (On Success) The global pixel coordinate on the Y-axis (vertical).
- `rot`: (On Success) The true yaw angle computed output (high precision, usually $0^\circ \sim 360^\circ$ with North as zero).
- `locConf`: The final hit confidence score, often used as a reference when fine-tuning parameters.
- `latencyMs`: The total natural milliseconds consumed by the node's algorithm execution, useful for performance monitoring.

#### Usage Examples

**Minimal Invocation (Recommended for basic usage):**
Thanks to a robust internal adaptive initialization flow, in most cases, you can simply call `MapLocateRecognition` directly as shown below, without passing any extra parameters. The system will automatically use YOLO inference to locate the associated map and carry out fully automated long-term tracking:

```json
{
    "MyLocateTask": {
        "recognition": "Custom",
        "custom_recognition": "MapLocateRecognition",
        "action": "DoNothing"
    }
}
```

**Advanced Invocation (Passing extra parameters):**
When executing a long-distance respawn teleport, or when you need to skip the previous calculation and forcefully specify coordinate tracking in a designated map, you can use `custom_recognition_param` to override the parameters:

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
