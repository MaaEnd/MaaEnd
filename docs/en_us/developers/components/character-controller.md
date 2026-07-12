# Development Manual - CharacterController Reference

## Introduction

This document explains how to use nodes related to CharacterController.

**CharacterController** provides a set of custom Actions for **controlling game characters**, including features like rotating the view, moving forward/backward, and automatically moving toward a target. These nodes are often used with MapTracker for more precise character control.

> [!IMPORTANT]
>
> All CharacterController nodes depend on keyboard/mouse input and **must run in the foreground mode (Seize)**, otherwise input events cannot be correctly delivered to the game. Ensure the controller uses the `Seize` connection method in `interface.json` or user configuration.

## Node Descriptions

Below are detailed descriptions of the specific usage of nodes provided by CharacterController. These nodes are of the Custom type and need to specify `custom_action` in the pipeline to use.

---

### Action: CharacterControllerYawDeltaAction

↔️ Rotates the player's view horizontally (yaw angle).

#### Node Parameters

Required parameters:

- `delta`: Integer, rotation angle in degrees. Positive values rotate right, negative values rotate left. Automatically takes modulo 360.

---

### Action: CharacterControllerPitchDeltaAction

↕️ Rotates the player's view vertically (pitch angle).

#### Node Parameters

Required parameters:

- `delta`: Integer, rotation angle in degrees. Positive values rotate down, negative values rotate up. Automatically takes modulo 360.

---

### Action: CharacterControllerForwardAxisAction

🚶 Controls the character's movement forward/backward.

#### Node Parameters

Required parameters:

- `axis`: Integer. Positive values move forward, negative values move backward, `0` means no movement. The actual movement duration is `|axis| × 100` milliseconds.

---

### Action: CharacterMoveToTargetAction

🎯 Automatically adjusts orientation and moves toward a target based on recognition results. Each call performs one adjustment step (rotation or forward/backward movement). It needs to be called repeatedly in a loop node until reaching the target.

#### Node Parameters

Optional parameters:

- `align_threshold`: Positive integer, default `120`. The pixel tolerance range for horizontal centering. When the horizontal offset between the target center and the screen center is less than this value, it is considered aligned, and the action switches to forward/backward movement.
- `far_target_width`: Positive integer. When the recognition box width is less than this value, the target is considered too far away, and the character moves forward directly, skipping rotation and alignment logic. This judgment is not enabled if not set.

#### Behavior Description

Each time it is called, one of the following logics is executed based on the current frame's recognition result:

| Condition                                                                  | Action Taken      |
| -------------------------------------------------------------------------- | ----------------- |
| Recognition box width < `far_target_width` (and `far_target_width` is set) | Move forward      |
| Target is left of screen center (exceeds `align_threshold`)                | Rotate view left  |
| Target is right of screen center (exceeds `align_threshold`)               | Rotate view right |
| Target is aligned, but Y-coordinate > 480 (target in lower half, passed)   | Move backward     |
| Target is aligned, and Y-coordinate ≤ 480 (target in upper half)           | Move forward      |

## Complete Example

For a complete usage example, please refer to `assets/resource/pipeline/Interface/Example/CharacterController.json`.
