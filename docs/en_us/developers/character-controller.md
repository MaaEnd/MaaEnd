# Development Guide - CharacterController Reference Document

## Introduction

This document describes how to use nodes related to CharacterController.

**CharacterController** provides a set of custom Actions for **controlling the game character**, including view rotation, forward/backward movement, and automatic movement toward a recognized target. These nodes are typically used alongside MapTracker for precise character control.

## Node Descriptions

The following details the specific usage of the nodes provided by CharacterController. These nodes are all Custom type nodes and need to specify `custom_action` in the pipeline to use.

---

### Action: CharacterControllerYawDeltaAction

↔️ Rotates the player's view horizontally (Yaw).

#### Node Parameters

Required parameters:

- `delta`: Integer, rotation angle in degrees. Positive values rotate right, negative values rotate left. Automatically taken modulo 360.

---

### Action: CharacterControllerPitchDeltaAction

↕️ Rotates the player's view vertically (Pitch).

#### Node Parameters

Required parameters:

- `delta`: Integer, rotation angle in degrees. Positive values rotate downward, negative values rotate upward. Automatically taken modulo 360.

---

### Action: CharacterControllerForwardAxisAction

🚶 Controls the character to move forward or backward.

#### Node Parameters

Required parameters:

- `axis`: Integer. Positive values move forward, negative values move backward, `0` means no movement. The actual movement duration is `|axis| × 100` milliseconds.

---

### Action: CharacterMoveToTargetAction

🎯 Automatically adjusts orientation and moves toward a recognized target. Each invocation performs one adjustment step (rotate or move forward/backward). This node should be called repeatedly in a loop until the target is reached.

#### Node Parameters

Required parameters:

- `recognition`: String, specifies the recognition method used. Currently only `"NeuralNetworkDetect"` is supported.

Optional parameters:

- `align_threshold`: Positive integer, default `120`. The horizontal pixel tolerance for centering on the target. When the horizontal offset between the target center and the screen center is less than this value, the target is considered aligned and the node switches to forward/backward movement.

#### Behavior Description

On each invocation, one of the following actions is taken based on the current frame's recognition result:

| Condition | Action |
|---|---|
| Target is to the left of screen center (beyond `align_threshold`) | Rotate view left |
| Target is to the right of screen center (beyond `align_threshold`) | Rotate view right |
| Target is aligned, but Y coordinate > 480 (target in lower half of screen, already passed) | Step backward |
| Target is aligned, and Y coordinate ≤ 480 (target in upper half of screen) | Step forward |

> [!NOTE]
>
> The `recognition` field must match the node's own recognition method. Currently only `NeuralNetworkDetect` is supported.

## Full Example

For a complete usage example, see `assets/resource/pipeline/Interface/Example/CharacterController.json`.
