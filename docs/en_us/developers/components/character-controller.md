# Development Manual - CharacterController Reference

## Introduction

This document explains how to use nodes related to CharacterController.

**CharacterController** provides a set of custom Actions for **controlling game characters**, including features like rotating the view, moving forward/backward, circle-walk search for targets, and automatically moving toward a target. These nodes are often used with MapTracker for more precise character control.

> [!IMPORTANT]
>
> Camera/view nodes (Yaw / Pitch / MoveToTarget, etc.) depend on keyboard/mouse input and **must run in the foreground mode (Seize)**, otherwise input events cannot be correctly delivered to the game.  
> Axis movement nodes (`CharacterControllerForwardAxisAction` and the `__CharacterControllerAxisLongPress*Action` nodes used by `CharacterSearchAction`) work on **ADB** via joystick `LongPress` overrides in [`resource_adb/.../CharacterController/Action.json`](../../../assets/resource_adb/pipeline/Common/__Private/CharacterController/Action.json); Seize is not required for those.

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

---

### Action: CharacterSearchAction

🔍 When an interact point cannot be found, walks a fixed circle path to fine-tune position and repeatedly recognizes target nodes. Returns success if any `wait_nodes` hits; returns failure after the full path with no hit, or when the task is Stopping. **Recognizes once at the starting position first**, then enters the move → wait → recognize loop.

Each step runs `__CharacterControllerAxisLongPress*Action`: default resource uses WASD `LongPressKey`; the ADB resource remaps them to virtual-joystick `LongPress` (see IMPORTANT above).

#### Node Parameters

Required parameters:

- `wait_nodes`: String array. Pipeline node names to search for; any hit means success.

#### Circle Path

Each step is fixed at 100ms (same as `CharacterControllerForwardAxisAction` with `axis: 1`); after every step, wait then recognize. Direction mapping: forward/up = W (or joystick up), back/down = S (or joystick down), left = A (or joystick left), right = D (or joystick right).

```text
start recognize → F F | L L | S S S S | D D D D | W W W W | A A
                        ^after each step: wait → screencap → recognize wait_nodes
```

18 move steps total, up to 19 recognition attempts (including the start).

## Complete Example

For a complete usage example, please refer to `assets/resource/pipeline/Interface/Example/CharacterController.json`.
