# Development Guide - AutoFight Reference Document

## 1. AutoFight Introduction

**AutoFight** is an in-combat automation module in MaaEnd. After the user enters the game combat scene, it automatically performs operations such as basic attacks, skills, chain combos, ultimate skills, dodging, and target locking until the battle ends and exits.

### Core Concepts

- **Entry Recognition**: Determines whether the current scene is an "auto-combat ready" combat scene (energy bar visible, 4 operator skill icons ready, not in character level settlement, etc.) through custom recognition `AutoFightEntryRecognition`.
- **Main Loop**: After entering combat, enter `__AutoFightLoop`, and branch between "pause", "exit", and "execute" each frame; when a non-combat space (such as ultimate skill cutscene) is recognized, enter pause; when a settlement interface is recognized, exit; otherwise, execute one combat operation.
- **Execution Logic**: `AutoFightExecuteRecognition` in Go Service queues actions to be performed based on the current screen (enemies, energy, combos/ultimates, etc.), and `AutoFightExecuteAction` retrieves and executes actions in chronological order at action nodes (such as clicking basic attack, skill keys, dodge keys, etc.), with Pipeline's `__AutoFightAction*` nodes completing specific clicks/key presses.

### Implementation Division

- **Pipeline** (`assets/resource/pipeline/AutoFight/`): Defines all "single operation" recognition and action nodes (such as `__AutoFightRecognition*`, `__AutoFightAction*`), as well as the main loop structure (`MainLoop.json`).
- **Go Service** (`agent/go-service/autofight/autofight.go`): Implements four types of Custom recognition and execution actions for entry/exit/pause/execute. Internally calls the above Pipeline nodes through `ctx.RunRecognition` / `ctx.RunTask`, and maintains action queues and priorities (combo > ultimate > normal skills > basic attack/dodge).

### Anchor Mechanism

Entry nodes (such as `AutoFight`, `AutoFightRealtimeTask`) specify the replacement of the "basic attack anchor" through `anchor`: if configured `__AutoFightActionAttackAnchor` → `__AutoFightActionComboClick`, the basic attack node will first execute the combo key (E key); if replaced with an empty string, only the anchor placeholder is executed, without triggering the basic attack click. The task layer can override `__AutoFightActionAttackAnchor` through the option to switch between "fully automatic (with basic attack)" and "semi-automatic (no basic attack)".

## 2. AutoFight Usage

### Basic Usage

In the task Pipeline, use the "AutoFight interface node" as `[JumpBack]` or `next`. When business logic needs to enter combat and automatically fight, jump to the corresponding interface; the interface will first perform entry recognition, and after passing, enter the main loop until exiting combat.

### Interface Overview

| Interface Name          | Description                                                                                                                        |
| ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `AutoFight`             | Fully automatic combat: with basic attack anchor (default points to combo key), auto basic attack + skills + combos, etc.          |
| `AutoFightNoAttack`     | Semi-automatic combat: no basic attack, only execute skills/combos/ultimates/dodges, etc.                                          |
| `AutoFightRealtimeTask` | For exploration/real-time tasks: decide whether to basic attack by overriding `__AutoFightActionAttackAnchor` through task option. |

The above interfaces are defined in `AutoFightInterface.json`.

### Example: Mounting AutoFight in Real-time Tasks

In `RealtimeTask.json`, use `AutoFightRealtimeTask` as the `[JumpBack]` node. When in a combat scene, it will automatically enter the AutoFight process, and after the battle ends, JumpBack returns:

```jsonc
{
    "RealtimeTaskEntry": {
        "next": ["[JumpBack]AutoFightRealtimeTask", "SomeOtherRealtimeLogic"],
    },
}
```

## 3. AutoFight Interface Convention

### Only Use Interfaces from AutoFightInterface.json

**Please only use the interface nodes defined in `AutoFightInterface.json`**: `AutoFight`, `AutoFightNoAttack`, `AutoFightRealtimeTask`, etc.

### Prohibit Direct Reference to \_\_AutoFight\* Internal Nodes

The `__AutoFight*` nodes defined in `MainLoop.json`, `Action.json`, and `Recognition.json` under the `AutoFight` directory (such as `__AutoFightLoop`, `__AutoFightExecute`, `__AutoFightActionAttack`, etc.) belong to **internal implementation** and are used to support the recognition and action processes of the interfaces.

- **Do not** directly reference `__AutoFight*` nodes (such as `__AutoFightLoop`, `__AutoFightActionAttack`, etc.) in tasks or other Pipelines.
- The structure, names, and logic of these nodes may change with version updates.
- If "automatic combat" capability is needed, please use one of the three interfaces above.

## 4. Rotation Implementation and TODO

Rotation (skill rotation/timeline) refers to the scheduling logic of "what operation to perform at what timing" within combat. The current implementation **does not have an independent rotation data format**, and all are written in `agent/go-service/autofight/autofight.go`, belonging to implicit, hardcoded rules.

### Implemented Content

- **Action Queue Structure**: `fightAction` contains `executeAt` (execution time), `action` (action type), and `operator` (operator index 1–4, only used for skill types). The queue is sorted by `executeAt`, and when executing, only expired actions are retrieved and executed sequentially via `RunTask`.
- **Action Types**: Lock target, combo (E key), ultimate (KeyDown/KeyUp), normal skills (1–4 key rotation), basic attack, dodge.
- **Priority and Enqueue Logic** (inside `AutoFightExecuteRecognition`):
    - Enemy first appears on screen → enqueue "lock target", `executeAt = now + 1ms`.
    - Combo prompt available → enqueue "combo", `executeAt = now`.
    - Otherwise, if ultimate available → enqueue that operator's ultimate KeyDown + KeyUp after 1.5s, only take the first available operator.
    - Otherwise, if energy ≥ 1 → enqueue "normal skill", operators rotate 1→2→3→4→1 by `skillCycleIndex`, `executeAt = now`.
    - Attack side: if enemy attack is recognized → enqueue "dodge", `executeAt = now + 100ms`; otherwise enqueue "basic attack", `executeAt = now`.
- **Fixed Delays**: Ultimate long press 1500ms; dodge delays 100ms before triggering to match recognition results.

### Not Implemented / Limitations

- **No Rotation Configuration File**: Cannot describe "whose skill to release at what second" or customize rotations by stage/lineup through JSON/YAML, etc.
- **Priority and Branching Hardcoded in Code**: For example, combo has priority over ultimate, ultimate only takes the first available, etc. Changing logic requires changing Go code.
- **No Absolute Timeline**: Only has "delay relative to current moment", no absolute time rotation such as "N seconds after combat starts".
- **Normal Skill Rotation Fixed to 1→2→3→4**: Cannot configure skill rotations customized by operator or order.

### TODO

- [ ] **Rotation Format**: Design a rotation (timeline/skill rotation) data format for internal use, used to describe or configure the skill release order and timing in combat, to support configuring rotations by stage or lineup, user-customized skill order, etc.
