# Development Manual - CharacterController Reference

## Introduction

This document explains how to use nodes related to CharacterController.

**CharacterController** provides a set of custom Actions for **controlling game characters**, including features like rotating the view, moving forward/backward, circle-walk search for targets, and automatically moving toward a target. These nodes are often used with MapNavigator for more precise character control.

> [!IMPORTANT]
>
> On Win32, camera/view nodes (Yaw / Pitch / MoveToTarget, etc.) depend on keyboard/mouse input and **must run in the foreground mode (Seize)**, otherwise input events cannot be correctly delivered to the game.  
> On **ADB**, [`resource_adb/.../CharacterController/Action.json`](../../../../assets/resource_adb/pipeline/Common/Private/CharacterController/Action.json) remaps the axis movement nodes (`CharacterControllerForwardAxisAction` and the `__CharacterControllerAxisLongPress*Action` nodes used by `CharacterSearchAction`) to joystick `LongPress`, the view drag to a touch swipe, and the Shift sprint to a sprint-button tap; Seize is not required for any of them.

## ADB Input Capability and Task Protection

An `AdbShell` single-pointer `Swipe` cannot provide the multiple contacts required by auto combat. Successful movement does not prove combat input works.

Protocol Space, Essence Farm, Auto Collect, Environment Monitoring, Visit Friends, Gift to Operator and the Fully Automatic Delivery subflow run the parameterless `TouchInputCheck` before their business steps, protecting auto-combat and map-navigation dependencies through these entries. Unscheduled days still skip normally, and the new entry check does not globally disable other delivery modes. `AutoFightMainAction` checks again before its combat loop. The current Go API's `GetInfo()` reports input-method configuration rather than the selected backend. On ADB, the check therefore releases attack contact `4` once, without pressing, clicking, or moving. `AdbShell` cannot perform this touch API and rejects it. This preflight verifies the necessary touch API; it does not replace in-game multi-touch validation.

Eco-Farm checks at `__AutoEcoFarmFilterClosed` before following the anchor to find a facility; disabled branches still skip. Delivery checks at `AutoDelivery` before resolving a mission or locating a recycle bin on the map. Both reject unsupported input before entering the C++ `MapFind` recognizer, avoiding a failed nested zoom followed by continued outer localization. A check in the action of a `MapFind` node would be too late because recognition runs first.

Shared map zoom, the ADB camera wheel and cursor movement in delivery dialogs use `TouchInputAction` before forwarding their original action. Its only parameter, `node`, names that action node; the original recognition box and outer wait settings are preserved. Held-key candidates in shared menus and legacy flows also have this protection in the ADB resource layer. They are checked only when executed; tasks referencing shared menus are not globally disabled. Protection does not establish full business-flow acceptance. Both the runtime log and Go log explain that ADB fallback or unavailable held-touch input was detected, the task cannot complete, and the emulator should be checked, restarted and reconnected. The API cannot confirm the selected backend name, so an arbitrary touch error is not presented as a confirmed backend switch.

Capability rejection is recorded for the current task ID and clears the rejected node's `on_error` in the current context. `SubTask` must propagate this rejection even when configured to ignore subtask failures; `FailureCollector` must propagate rejection from its main task, recovery route or failure notification. Dependent steps, reward claims and retries in this task stop. `RepeatUntilFoundAction` and `RepeatUntilNotFoundAction` check rejection after their inner actions and recognition, and `AutoCtrlClickAction` propagates it before returning, so an outer success result or recovery branch cannot swallow rejection. Ordinary route failures retain their existing handling. Task lifecycle events clear rejection state without calling `PostStop`, which would report success. Later independent tasks remain runnable but must verify their own scene preconditions. Direct C++ navigation calls that bypass these Pipeline entries are outside the protected business wiring.

Failed input actions end the current combat callback. Ordinary input failures follow the parent's existing recovery or continuation policy; their message does not promise task-wide interruption or no further retries. The main combat callback attempts bounded release of its requested contacts on exit, preserves the first error if release fails, and clears pending actions so the next battle cannot inherit an old queue. Capability rejections still propagate at task scope as described above. Reconnect after restoring the device or touch backend; rerunning a task with the same degraded controller is insufficient.

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
- `far_target_width`: Positive integer. When the recognition box width is less than this value, the target is considered too far away, and the character moves forward directly (200 ms forward, then 3 sprints), skipping rotation and alignment logic. This judgment is not enabled if not set.

#### Behavior Description

Each time it is called, one of the following logics is executed based on the current frame's recognition result:

| Condition | Action Taken |
| -------------------------------------------------------------------------- | ----------------- |
| Recognition box width < `far_target_width` (and `far_target_width` is set) | Move forward, then sprint 3 times |
| Target is left of screen center (exceeds `align_threshold`) | Rotate view left |
| Target is right of screen center (exceeds `align_threshold`) | Rotate view right |
| Target is aligned, but Y-coordinate > 480 (target in lower half, passed) | Move backward |
| Target is aligned, and Y-coordinate ≤ 480 (target in upper half) | Move forward |

---

### Action: CharacterSearchAction

🔍 When an interact point cannot be found, walks a fixed circle path to fine-tune position and repeatedly recognizes target nodes. Returns success if any `wait_nodes` hits; returns failure after the full path with no hit, or when the task is Stopping. **Recognizes once at the starting position first**; if that misses, it enters the move → wait → recognize loop.

Each step runs `__CharacterControllerAxisLongPress*Action`: default resource uses WASD `LongPressKey`; the ADB resource remaps them to virtual-joystick `LongPress` (see IMPORTANT above).

#### Node Parameters

Required parameters:

- `wait_nodes`: String array. Pipeline node names to search for; any hit means success.

#### Circle Path

Each step is fixed at 100ms (same as `CharacterControllerForwardAxisAction` with `axis: 1`); after every step, wait a fixed interval, then recognize. Direction mapping: forward/up = W (or joystick up), back/down = S (or joystick down), left = A (or joystick left), right = D (or joystick right).

```text
start recognize → W W | A A | S S S S | D D D D | W W W W | A A
                        ^after each step: wait → screencap → recognize wait_nodes
```

18 move steps total, up to 19 recognition attempts (including the start).

## Complete Example

For a complete usage example, please refer to `assets/resource/pipeline/Interface/Example/CharacterController.json`.
