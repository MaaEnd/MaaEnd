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

### Examples

For concrete usage examples, see `assets/resource/pipeline/Interface/Example/Scene.json`, which contains complete example nodes for both normal scene interfaces and teleport interfaces.

## 3. Conventions for Universal Jump Interfaces

### Only Use Scene Interfaces from the Interface Directory

**Only use the scene interface nodes defined in the `SceneXXX.json` files under `assets/resource/pipeline/Interface`.**  
These node names **do not start with `__ScenePrivate`**.

### Do Not Use \_\_ScenePrivate Nodes

`SceneManager` files (such as `SceneCommon.json`, `SceneMenu.json`, `SceneWorld.json`, `SceneMap.json`, etc.) define `__ScenePrivate*` nodes as **internal implementation details** that support the actual jump logic of the interfaces.

- **Do not** reference `__ScenePrivate*` nodes directly in task Pipelines.
- The structure, names, and logic of these nodes may change in future versions.
- If you need some scene capability, first check whether there is a corresponding interface in the `SceneXXX.json` files under `Interface`. If not, please submit a feature request.

### Common Interface Overview

| Category | Interface Name                      | Description                                                      |
| -------- | ----------------------------------- | ---------------------------------------------------------------- |
| World    | `SceneAnyEnterWorld`                | Enter any overworld (Valley / Wuling / Dijiang) from any screen. |
| World    | `SceneEnterWorldDijiang`            | Enter Dijiang overworld.                                         |
| World    | `SceneEnterWorldValleyIVTheHub`     | Enter Valley IV - The Hub overworld.                             |
| World    | `SceneEnterWorldFactory`            | Enter overworld factory mode.                                    |
| Map      | `SceneEnterMapDijiang`              | Enter Dijiang map screen.                                        |
| Map      | `SceneEnterMapValleyIVTheHub`       | Enter Valley IV - The Hub map screen.                            |
| Menu     | `SceneEnterMenuList`                | Enter main menu list.                                            |
| Menu     | `SceneEnterMenuRegionalDevelopment` | Enter Regional Development menu.                                 |
| Menu     | `SceneEnterMenuEvent`               | Enter Event menu.                                                |
| Menu     | `SceneEnterMenuProtocolPass`        | Enter Protocol Pass menu.                                        |
| Menu     | `SceneEnterMenuBackpack`            | Enter inventory screen.                                          |
| Menu     | `SceneEnterMenuShop`                | Enter shop screen.                                               |
| Helper   | `SceneDialogConfirm`                | Click confirm button in dialogs.                                 |
| Helper   | `SceneDialogCancel`                 | Click cancel button in dialogs.                                  |
| Helper   | `SceneNoticeRewardsConfirm`         | Click confirm button on rewards screens.                         |
| Helper   | `SceneWaitLoadingExit`              | Wait for loading screen to disappear.                            |

For the complete list of interfaces and detailed descriptions, please refer to the `desc` field of each node in the `SceneXXX.json` files under `assets/resource/pipeline/Interface`.
