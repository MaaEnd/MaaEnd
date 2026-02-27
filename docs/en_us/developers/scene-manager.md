# Development Guide - SceneManager Reference

## 1. Universal Jump Overview

**SceneManager** is the scene navigation module in MaaEnd, providing a "universal jump" mechanism.

### Core Concept

**Universal jump** means: **starting from any in-game screen, the system can automatically navigate to the target scene**.  
No matter whether the user is currently on the main menu, in the overworld, in some sub-menu, or even on a loading screen or pop-up, as long as the corresponding scene interface node is attached under `next`, the Pipeline will automatically handle:

- Recognizing and handling pop-ups (confirm / cancel)
- Waiting for loading to complete
- Stepping back or drilling down through intermediate scenes
- Eventually reaching the target scene

### How It Works

SceneManager uses MaaFramework's `[JumpBack]` mechanism to organize scene interfaces into a **hierarchical jump chain**:

- In the `next` list of each scene interface, there are both "direct success" recognition nodes and several "fallback" nodes.
- When the current path cannot recognize a matching node, it will `[JumpBack]` to a more basic scene interface; that interface is then responsible for entering the prerequisite scene, after which the attempt is retried.
- The bottom level is `SceneAnyEnterWorld` (enter any overworld). It is the starting point for most scene jumps.

For example, the `next` of `SceneEnterMenuProtocolPass` (enter the Protocol Pass menu) is:

- `__ScenePrivateWorldEnterMenuProtocolPass`: if already in the overworld, directly open Protocol Pass.
- `[JumpBack]SceneAnyEnterWorld`: if not in the overworld, enter the overworld first, then retry.

## 2. How to Use Universal Jump

### Basic Usage

In a Pipeline task, put the "target scene interface" as a `[JumpBack]` node in `next`.  
When a business node fails to recognize the expected screen, the framework will first perform a scene jump to reach the target scene, then return to the business logic and continue execution.

### Example: One-Click Claiming Protocol Pass Rewards

The following example shows how to use universal jump in a task that "enters the Protocol Pass menu from any screen and claims rewards".

```jsonc
{
    "DailyProtocolPassStart": {
        "pre_delay": 0,
        "post_delay": 0,
        "next": [
            "DailyProtocolPassInMenu",
            "[JumpBack]SceneEnterMenuProtocolPass"
        ]
    },
    "DailyProtocolPassInMenu": {
        "desc": "In Protocol Pass menu",
        "recognition": { ... },
        "next": [ 
            "DailyProtocolMissionsEnter",
             ... 
        ]
    },
    ...
}
```

**Execution Flow Explanation**:

1. The task entry is `DailyProtocolPassStart`, and its `next` contains `DailyProtocolPassInMenu` and `[JumpBack]SceneEnterMenuProtocolPass`.
2. If the current screen is already the Protocol Pass interface → it hits `DailyProtocolPassInMenu` and enters the business logic.
3. If the current screen is not the Protocol Pass interface → it hits `[JumpBack]SceneEnterMenuProtocolPass`, and the framework executes the "enter Protocol Pass menu" jump chain.
4. Inside `SceneEnterMenuProtocolPass`, it will call `SceneAnyEnterWorld` and others as needed, first entering the overworld and then opening the Protocol Pass menu.
5. After entering Protocol Pass, the Pipeline reruns from the task entry, and eventually hits `DailyProtocolPassInMenu`.

### Example: Entering Regional Development for Reselling

```jsonc
{
    "ResellMain": {
        "desc": "One-click resell entry",
        "pre_delay": 0,
        "post_delay": 500,
        "next": [
            "ResellStageCheckArea",
            "[JumpBack]SceneEnterMenuRegionalDevelopment"
        ]
    },
    "ResellStageCheckArea": {
        "desc": "Check current region",
        "recognition": { ... },
        "next": [ ... ]
    },
    ...
}
```

When `ResellStageCheckArea` fails to recognize (for example the current screen is the inventory, an event, etc.), it will automatically use `SceneEnterMenuRegionalDevelopment` to enter the Regional Development menu, then return to `ResellMain` and retry.

## 3. Conventions for Universal Jump Interfaces

### Only Use Interfaces from SceneInterface.json

**Only use the scene interface nodes defined in `assets/resource/pipeline/SceneInterface.json`.**  
These node names **do not start with `__ScenePrivate`**.

### Do Not Use __ScenePrivate Nodes

`SceneManager` files (such as `SceneCommon.json`, `SceneMenu.json`, `SceneWorld.json`, `SceneMap.json`, etc.) define `__ScenePrivate*` nodes as **internal implementation details** that support the actual jump logic of the interfaces.

- **Do not** reference `__ScenePrivate*` nodes directly in task Pipelines.
- The structure, names, and logic of these nodes may change in future versions.
- If you need some scene capability, first check whether there is a corresponding interface in `SceneInterface.json`. If not, please submit a feature request.

### Common Interface Overview

| Category | Interface Name                      | Description                                                                    |
| -------- | ----------------------------------- | ------------------------------------------------------------------------------ |
| World    | `SceneAnyEnterWorld`               | Enter any overworld (Valley / Wuling / Dijiang) from any screen.              |
| World    | `SceneEnterWorldDijiang`           | Enter Dijiang overworld.                                                       |
| World    | `SceneEnterWorldValleyIVTheHub`    | Enter Valley IV - The Hub overworld.                                           |
| World    | `SceneEnterWorldFactory`           | Enter overworld factory mode.                                                  |
| Map      | `SceneEnterMapDijiang`             | Enter Dijiang map screen.                                                      |
| Map      | `SceneEnterMapValleyIVTheHub`      | Enter Valley IV - The Hub map screen.                                          |
| Menu     | `SceneEnterMenuList`               | Enter main menu list.                                                          |
| Menu     | `SceneEnterMenuRegionalDevelopment`| Enter Regional Development menu.                                               |
| Menu     | `SceneEnterMenuEvent`              | Enter Event menu.                                                              |
| Menu     | `SceneEnterMenuProtocolPass`       | Enter Protocol Pass menu.                                                      |
| Menu     | `SceneEnterMenuBackpack`           | Enter inventory screen.                                                        |
| Menu     | `SceneEnterMenuShop`               | Enter shop screen.                                                             |
| Helper   | `SceneDialogConfirm`               | Click confirm button in dialogs.                                               |
| Helper   | `SceneDialogCancel`                | Click cancel button in dialogs.                                                |
| Helper   | `SceneNoticeRewardsConfirm`        | Click confirm button on rewards screens.                                       |
| Helper   | `SceneWaitLoadingExit`             | Wait for loading screen to disappear.                                          |

For the complete list of interfaces and detailed descriptions, please refer to the `desc` field of each node in `assets/resource/pipeline/SceneInterface.json`.
