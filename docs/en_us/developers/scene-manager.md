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

## Protocol Teleport Point Interfaces

### Valley IV

| Interface Name                                     | Description                                                                                           |
| -------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `SceneEnterMapValleyIVTheHub`                      | Enter the Valley IV - The Hub map screen from any screen.                                             |
| `SceneEnterMapValleyIVValleyPass`                  | Enter the Valley IV - Valley Pass map screen from any screen.                                         |
| `SceneEnterMapValleyIVOriginiumSciencePark`        | Enter the Valley IV - Originium Science Park map screen from any screen.                              |
| `SceneEnterMapValleyIVAburreyQuarry`               | Enter the Valley IV - Aburrey Quarry map screen from any screen.                                      |
| `SceneEnterMapValleyIVOriginLodespring`            | Enter the Valley IV - Origin Lodespring map screen from any screen.                                   |
| `SceneEnterMapValleyIVPowerPlateau`                | Enter the Valley IV - Power Plateau map screen from any screen.                                       |
| `SceneEnterWorldValleyIVTheHub`                    | Enter the Valley IV - The Hub overworld from any screen.                                              |
| `SceneEnterWorldValleyIVTheHub1`                   | Enter the Valley IV - The Hub (East of Old Water Supply Facility) overworld from any screen.          |
| `SceneEnterWorldValleyIVTheHub2`                   | Enter the Valley IV - The Hub (Rock Hill Passage) overworld from any screen.                          |
| `SceneEnterWorldValleyIVOriginiumSciencePark0`     | Enter the Valley IV - Originium Science Park (Origin Lodespring Entrance) overworld from any screen.  |
| `SceneEnterWorldValleyIVOriginiumSciencePark1`     | Enter the Valley IV - Originium Science Park (Originium Experimental Park) overworld from any screen. |
| `SceneEnterWorldValleyIVOriginiumSciencePark2`     | Enter the Valley IV - Originium Science Park (Gentle Mountain Slope) overworld from any screen.       |
| `SceneEnterWorldValleyIVOriginiumSciencePark3`     | Enter the Valley IV - Originium Science Park (Highway 5) overworld from any screen.                   |
| `SceneEnterWorldValleyIVOriginiumSciencePark4`     | Enter the Valley IV - Originium Science Park (Alcohol Compound Factory) overworld from any screen.    |
| `SceneEnterWorldValleyIVOriginiumScienceParkInfra` | Enter the Valley IV - Originium Science Park (Infrastructure Outpost) overworld from any screen.      |
| `SceneEnterWorldValleyIVOriginLodespring0`         | Enter the Valley IV - Origin Lodespring (Originium Tunnel) overworld from any screen.                 |
| `SceneEnterWorldValleyIVOriginLodespring1`         | Enter the Valley IV - Origin Lodespring (Transport Area) overworld from any screen.                   |
| `SceneEnterWorldValleyIVOriginLodespring2`         | Enter the Valley IV - Origin Lodespring (Old Mining Area) overworld from any screen.                  |
| `SceneEnterWorldValleyIVOriginLodespring3`         | Enter the Valley IV - Origin Lodespring (Mining Medical Station) overworld from any screen.           |
| `SceneEnterWorldValleyIVOriginLodespring4`         | Enter the Valley IV - Origin Lodespring (Landbreaker Camp) overworld from any screen.                 |
| `SceneEnterWorldValleyIVPowerPlateau0`             | Enter the Valley IV - Power Plateau (Power Station Communication Station) overworld from any screen.  |
| `SceneEnterWorldValleyIVPowerPlateau1`             | Enter the Valley IV - Power Plateau (Highland Main Road) overworld from any screen.                   |
| `SceneEnterWorldValleyIVPowerPlateau2`             | Enter the Valley IV - Power Plateau (Bonegrinder Camp) overworld from any screen.                     |
| `SceneEnterWorldValleyIVPowerPlateau3`             | Enter the Valley IV - Power Plateau (Evacuation Zone) overworld from any screen.                      |

### Wuling

| Interface Name                         | Description                                                                             |
| -------------------------------------- | --------------------------------------------------------------------------------------- |
| `SceneEnterMapWulingWulingCity`        | Enter the Wuling - Wuling City map screen from any screen.                              |
| `SceneEnterMapWulingJingyuValley`      | Enter the Wuling - Jingyu Valley map screen from any screen.                            |
| `SceneEnterMapWulingQingboStockade`    | Enter the Wuling - Qingbo Stockade map screen from any screen.                          |
| `SceneEnterWorldWulingJingyuValley0`   | Enter Wuling - Jingyu Valley (Ecological Experiment Station) overworld from any screen. |
| `SceneEnterWorldWulingJingyuValley1`   | Enter Wuling - Jingyu Valley (Caidao) overworld from any screen.                        |
| `SceneEnterWorldWulingJingyuValley2`   | Enter Wuling - Jingyu Valley (Outside Treasure Cave) overworld from any screen.         |
| `SceneEnterWorldWulingJingyuValley3`   | Enter Wuling - Jingyu Valley (Outer Qingbo Stockade) overworld from any screen.         |
| `SceneEnterWorldWulingJingyuValley4`   | Enter Wuling - Jingyu Valley (Inside Treasure Cave) overworld from any screen.          |
| `SceneEnterWorldWulingJingyuValley5`   | Enter Wuling - Jingyu Valley (Tianwang Flat) overworld from any screen.                 |
| `SceneEnterWorldWulingJingyuValley6`   | Enter Wuling - Jingyu Valley (Tuobi Mountain) overworld from any screen.                |
| `SceneEnterWorldWulingJingyuValley7`   | Enter Wuling - Jingyu Valley (Lost Forest) overworld from any screen.                   |
| `SceneEnterWorldWulingJingyuValley8`   | Enter Wuling - Jingyu Valley (Zhailing Islet) overworld from any screen.                |
| `SceneEnterWorldWulingJingyuValley9`   | Enter Wuling - Jingyu Valley (South Mountain) overworld from any screen.                |
| `SceneEnterWorldWulingJingyuValley10`  | Enter Wuling - Jingyu Valley (Shuize Ravine) overworld from any screen.                 |
| `SceneEnterWorldWulingWulingCityCore`  | Enter Wuling - Wuling City Core overworld from any screen.                              |
| `SceneEnterWorldWulingWulingCity0`     | Enter Wuling - Wuling City (Observation Station) overworld from any screen.             |
| `SceneEnterWorldWulingWulingCity1`     | Enter Wuling - Wuling City (Boundary Stone Flat) overworld from any screen.             |
| `SceneEnterWorldWulingWulingCity2`     | Enter Wuling - Wuling City (Pending Repair Area) overworld from any screen.             |
| `SceneEnterWorldWulingWulingCity3`     | Enter Wuling - Wuling City (Fangxing Avenue) overworld from any screen.                 |
| `SceneEnterWorldWulingWulingCity4`     | Enter Wuling - Wuling City (Tianshi Academy) overworld from any screen.                 |
| `SceneEnterWorldWulingWulingCity5`     | Enter Wuling - Wuling City (Tianjing Courtyard) overworld from any screen.              |
| `SceneEnterWorldWulingWulingCity6`     | Enter Wuling - Wuling City (Reserve Station Upper Left) overworld from any screen.      |
| `SceneEnterWorldWulingWulingCity7`     | Enter Wuling - Wuling City (Sanku Gang) overworld from any screen.                      |
| `SceneEnterWorldWulingWulingCity8`     | Enter Wuling - Wuling City (Reserve Station Lower Right) overworld from any screen.     |
| `SceneEnterWorldWulingQingboStockade0` | Enter Wuling - Qingbo Stockade (Dingtian Ridge) overworld from any screen.              |
| `SceneEnterWorldWulingQingboStockade1` | Enter Wuling - Qingbo Stockade (Plank Bridge Path) overworld from any screen.           |
| `SceneEnterWorldWulingQingboStockade2` | Enter Wuling - Qingbo Stockade (Main Stockade Southwest) overworld from any screen.     |
| `SceneEnterWorldWulingQingboStockade3` | Enter Wuling - Qingbo Stockade (Ancestral Spring) overworld from any screen.            |
| `SceneEnterWorldWulingQingboStockade4` | Enter Wuling - Qingbo Stockade (Main Stockade Southeast) overworld from any screen.     |

For the complete list of interfaces and detailed descriptions, please refer to the `desc` field of each node in the `SceneXXX.json` files under `assets/resource/pipeline/Interface`.
