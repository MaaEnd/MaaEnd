# Development Guide - AutoFight Reference Document

## 1. AutoFight Introduction

**AutoFight** is an in-combat automation module in MaaEnd. After the user enters the game combat scene, it automatically performs operations such as basic attacks, skills, link skills, ultimates, dodging, and target locking until the battle ends and exits.

### Core Concepts

- **Entry Recognition**: Determines whether the current scene is an "auto-combat ready" combat scene (energy bar visible, at least one operator's link skill ready, operator level not shown) through custom recognition `AutoFightEntryRecognition`.
- **Main Loop**: After entry recognition passes, the custom action `AutoFightMainAction` runs the main loop inside Go: after capturing each frame, it branches between "pause", "exit", and "execute"; when a non-combat space (such as ultimate skill cutscene) is recognized, enter pause; when the pause lasts 10 seconds or a node in `pause_exit_nodes` hits during the pause, exit; also exit when the operator level shows again in combat space (combat is over); otherwise, execute one combat operation.
- **Execution Logic**: `AutoFightMainAction` queues actions to be performed based on the current screen (enemies, energy, link skills/ultimates, HP, etc.) and the `attach` parameters, and at the end of each frame retrieves and executes the due actions in chronological order (such as skill keys, dodge keys, target locking, etc.), with Pipeline's `__AutoFightAction*` nodes completing specific clicks/key presses.

### Implementation Division

- **Pipeline** (`assets/resource/pipeline/AutoFight/`): Defines all "single operation" recognition and action nodes (such as `__AutoFightRecognition*`, `__AutoFightAction*`); the interface node `AutoFight` is in `assets/resource/pipeline/Interface/AutoFight.json`, and on ADB the touch actions and some recognition regions are overridden by `assets/resource_adb/pipeline/AutoFight/`.
- **Go Service** (`agent/go-service/autofight/`): `autofight.go` implements the entry recognition `AutoFightEntryRecognition` and the main loop action `AutoFightMainAction` (including pause/exit checks and the action queue); `ScreenAnalyzer` in `screenanalyzer.go` runs the ONNX detection node `__AutoFightRecognitionScreen` every frame and judges energy, link skills, ultimates, enemies and other states from the detections of the last few frames; `endaxistimeline.go` implements the Endaxis timeline. Internally calls the above Pipeline nodes through `ctx.RunRecognition` / `ctx.RunAction`.

### Parameters (attach)

At startup, `AutoFightMainAction` reads the current node's `attach` through `ctx.GetNodeJSON` as its parameters; the defaults are in `Interface/AutoFight.json`: `enable_attack` (basic attack), `enable_combo` (link skill), `enable_skill` / `reserve_skill_level` / `enable_break_accumulating_power` (combat skill), `enable_end_skill` (ultimate), `enable_dodge` / `enable_dodge_compat` (dodge), `enable_lock_target` (target locking), `enable_health_dangerous_switch` (switching out low-HP operators), `end_axis_timeline_code` / `skip_combo_cooldown` (timeline), plus `pause_exit_nodes`, which is not set by default (a list of node names; if any of them is recognized during a pause, the fight exits at once, e.g. the Trial of Swordmancy task passes `TrialOfSwordmancyFightFinish`). The task layer switches behavior by overriding fields of `AutoFight`'s `attach` through the option's `pipeline_override`; for example, the `AutoFightAttack` option in `assets/tasks/RealTimeTask.json` sets `enable_attack` to `false` to turn off basic attacks.

## 2. AutoFight Usage

### Basic Usage

In the task Pipeline, use the "AutoFight interface node" as `[JumpBack]` or `next`. When business logic needs to enter combat and automatically fight, jump to the corresponding interface; the interface will first perform entry recognition, and after passing, enter the main loop until exiting combat.

### Interface Overview

| Interface Name | Description |
| ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `AutoFight` | Auto combat: runs the main loop after entry recognition passes; whether basic attacks, link skills, combat skills, ultimates, dodging, target locking, etc. are enabled is controlled by the `attach` parameters. |

The above interface is defined in `assets/resource/pipeline/Interface/AutoFight.json`.

### Example: Mounting AutoFight in Real-time Tasks

In `assets/resource/pipeline/RealtimeTask.json`, `AutoFight` is listed in `custom_action_param.nodes` of `RealTimeTaskMain` and polled by the custom action `RealTimeTaskAction`. When in a combat scene, it will automatically enter the AutoFight process, and after the battle ends, polling continues (excerpt):

```jsonc
{
    "RealTimeTaskMain": {
        "action": "Custom",
        "custom_action": "RealTimeTaskAction",
        "custom_action_param": {
            "nodes": ["RealTimeAutoSkip", "AutoFight", "RealTimeSleep"],
        },
    },
}
```

## 3. AutoFight Interface Convention

### Only Use the Interface from Interface/AutoFight.json

**Please only use the interface node defined in `Interface/AutoFight.json`**: `AutoFight`; override its `attach` parameters when different behavior is needed.

### Prohibit Direct Reference to \_\_AutoFight\* Internal Nodes

The `__AutoFight*` nodes defined in `Action.json` and `Recognition.json` under the `AutoFight` directory (such as `__AutoFightRecognitionScreen`, `__AutoFightActionDodge`, `__AutoFightActionAttackTouchDown`, etc.) belong to **internal implementation** and are used to support the recognition and action processes of the interface.

- **Do not** directly reference `__AutoFight*` nodes (such as `__AutoFightRecognitionScreen`, `__AutoFightActionDodge`, etc.) in tasks or other Pipelines.
- The structure, names, and logic of these nodes may change with version updates.
- If "automatic combat" capability is needed, please use the `AutoFight` interface.

## 4. Timeline Implementation and TODO

The timeline (skill axis / time axis) refers to the scheduling logic of "what operation to perform at what timing" within combat. There are currently two schedulers: by default, the hardcoded rules in `agent/go-service/autofight/autofight.go` are used; when `attach.end_axis_timeline_code` holds a valid Endaxis timeline data code, combat skills and ultimates are instead dispatched along the timeline parsed by `agent/go-service/autofight/endaxistimeline.go`, and an invalid data code falls back to the default rules.

### Implemented Content

- **Action Queue Structure**: `fightAction` contains `executeAt` (execution time) and `action` (action type, with the operator number built into the type, such as `ActionSkill1`–`ActionSkill4`). The queue is sorted by `executeAt`; at the end of each frame, `drainActionQueue` takes out the due actions and runs the matching `__AutoFightAction*` nodes in turn via `RunAction`.
- **Action Types**: Lock target, link skill (E key), combat skill (keys 1–4), ultimate (long press of keys 1–4), dodge, switch operator (F1–F4), dash forward/back/left/right (WASD + dodge), wait 1 second, plus the basic attack re-press on ADB; the keys for link skills, combat skills and ultimates, and operator switching can be rebound in the global key option `KeymapFight` (`assets/tasks/setting/Keymap.json`). Basic attack is not queued: with `enable_attack` on, it is held down for the whole fight (`__AutoFightActionAttackTouchDown`) and released at the end.
- **Enqueue Logic** (inside the `AutoFightMainAction` loop, checked in this order every frame):
    - `enable_dodge` on and an enemy attack recognized → enqueue "dodge", `executeAt = now + 1ms`; when `enable_dodge_compat` is `true`, attack prompts centered in the middle of the screen are ignored, while ground attacks are still dodged.
    - With `enable_lock_target` on: if no enemy is locked, lock once at the start of combat, then lock again after 3 seconds without a lock, dash toward the enemy's direction (forward if there is no direction hint) and then lock after 6 seconds, and restart the timer after 9 seconds; if locked but there has been no dodge or dash for 5 seconds, dash forward once so the enemy does not get too far away. When it is off, the "enemy locked" conditions below always count as met.
    - `enable_health_dangerous_switch` on and the current operator's HP is dangerous → enqueue "switch operator" to the first operator with normal HP.
    - `enable_combo` on and a link skill prompt recognized → enqueue "link skill", `executeAt = now`.
    - `enable_end_skill` on, enemy locked and an ultimate ready → enqueue that operator's ultimate, only taking the first ready operator.
    - `enable_skill` on and energy ≥ 1 → when enemy charging is recognized and `enable_break_accumulating_power` is on, release a combat skill right away to break it; otherwise release only when energy is above `reserve_skill_level` and an enemy is locked; operators rotate 1→2→…→N→1 by `skillCycleIndex` (N is the team size), `executeAt = now`.
- **Timeline (Endaxis)**: `end_axis_timeline_code` is the string produced by "Copy Data Code" on the Endaxis site (`www.end-axis.com`), i.e. the project JSON compressed with gzip and then URL-safe base64 encoded. Once it is enabled, the link skill, ultimate and combat skill rules above no longer apply: when an enemy is locked and no plan is running, `SelectScenario` first waits until all link skills are ready (no waiting when `skip_combo_cooldown` is `true`), then picks from the data the first plan in which every operator with an ultimate has it ready and which contains combat skill or ultimate actions; it then dispatches combat skills and ultimates along the timeline, pausing the timer to wait while a condition is not met (ultimate not ready, not enough energy, no enemy locked); while the plan runs, link skills are released whenever a link skill prompt is recognized.
- **Fixed Delays**: Ultimate long press 1500ms; dodge, target locking, dashes and operator switching use `executeAt = now + 1ms`; a pause of more than 10 seconds exits; on ADB the basic attack is re-pressed every 5 seconds; after exiting, it keeps capturing for 3 seconds until the last actions finish.

### Not Implemented / Limitations

- **The Timeline Only Dispatches Combat Skills and Ultimates**: Only `ultimate` and `skill` / `battleSkill` actions in the Endaxis data are dispatched; other types are not dispatched and only count toward the plan's end time (the latest end frame of all actions); in timeline mode, link skills are still triggered by recognition.
- **Default Rules Hardcoded in Code**: For example, the ultimate only takes the first ready operator, combat skills are released by energy and `reserve_skill_level`, etc. Changing logic requires changing Go code.
- **Timeline Time Starts From the Selected Plan**: The timeline starts counting when `SelectScenario` selects a plan (frame 300 in Endaxis, converted at 60 fps), and the timer pauses while the front action cannot be executed yet, so it is not the same as "N seconds after combat starts".
- **Default Combat Skill Rotation Fixed to 1→2→…→N**: A custom release order requires the timeline.

### TODO

- [x] **Timeline Format**: The Endaxis timeline data code has been adopted (see above) and can configure the release order and timing of combat skills and ultimates in combat; automatically choosing different timelines by stage or lineup is not supported yet.
