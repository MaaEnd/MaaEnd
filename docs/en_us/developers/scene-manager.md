# Development Guide - SceneManager Reference

## 1. Universal Jump Overview

**SceneManager** is the scene navigation module in MaaEnd, providing a "universal jump" mechanism.

### Core Concept

**Universal jump** means: **starting from any in-game screen, the system can automatically navigate to the target scene**. No matter whether the user is currently on the main menu, in the overworld, in some sub-menu, or even on a loading screen or pop-up, as long as the corresponding scene interface node is attached under `next`, the Pipeline will automatically handle:

- Recognizing and handling pop-ups (confirm / cancel)
- Waiting for loading to complete
- Stepping back level by level, or entering a more basic scene first
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

In a Pipeline task, put the "target scene interface" as a `[JumpBack]` node in `next`. When a business node fails to recognize the expected screen, the framework will first perform a scene jump to reach the target scene, then return to the business logic and continue execution. Use a node referencing `InScene` universal scene recognition in `next` to verify that the target scene has been reached before entering business logic, preventing infinite loops caused by repeatedly entering the universal jump.

### Examples

For concrete usage examples, see `assets/resource/pipeline/Interface/Example/Scene.json` and `assets/resource/pipeline/Interface/Example/InScene.json`, which contain complete example nodes for both normal scene interfaces and teleport interfaces.

## 3. Conventions for Universal Jump Interfaces

### Only Use Scene Interfaces from the Interface Directory

**Only use the scene interface nodes defined in the `SceneXXX.json` files under `assets/resource/pipeline/Interface`.** These node names **do not start with `__ScenePrivate`**.

### Do Not Use \_\_ScenePrivate Nodes

Files in the `SceneManager` folder (such as `SceneCommon.json`, `SceneMenu.json`, `SceneWorld.json`, `SceneMap.json`, etc.) define `__ScenePrivate*` nodes as **internal implementation details** that support the actual jump logic of the interfaces.

- **Do not** reference `__ScenePrivate*` nodes directly in task Pipelines.
- The structure, names, and logic of these nodes may change in future versions.
- If you need some scene capability, first check whether there is a corresponding interface in the `SceneXXX.json` files under `Interface`. If not, please submit a feature request.

### Common Interface Overview

| Category | Interface Name | Description |
| -------- | ----------------------------------- | ---------------------------------------------------------------- |
| World | `SceneAnyEnterWorld` | Enter any overworld (Valley / Wuling / Dijiang) from any screen. |
| World | `SceneEnterWorldDijiang` | Enter Dijiang overworld. |
| World | `SceneEnterWorldValleyIVTheHub` | Enter Valley IV - The Hub overworld. |
| World | `SceneEnterWorldFactory` | Enter overworld factory mode. |
| Map | `SceneEnterMapDijiang` | Enter Dijiang map screen. |
| Map | `SceneEnterMapValleyIVTheHub` | Enter Valley IV - The Hub map screen. |
| Menu | `SceneEnterMenuList` | Enter main menu list. |
| Menu | `SceneEnterMenuMission` | Enter the Missions screen. |
| Menu | `SceneEnterMenuRegionalDevelopment` | Enter Regional Development menu. |
| Menu | `SceneEnterMenuEvent` | Enter Events menu. |
| Menu | `SceneEnterMenuProtocolPass` | Enter Protocol Pass menu. |
| Menu | `SceneEnterMenuBackpack` | Enter Backpack screen. |
| Menu | `SceneEnterMenuShop` | Enter shop screen. |
| Menu | `SceneEnterMenuIntelArchive` | Enter Intel Archives. |
| Menu | `SceneEnterMenuIntelArchiveIntelGathering` | Enter Intel Archives - Intel Gathering. |
| Menu | `SceneEnterMenuIntelArchiveAudioRecords` | Enter Intel Archives - Audio Records. |
| Menu | `SceneEnterMenuIntelArchiveFindings` | Enter Intel Archives - Findings. |
| Menu | `SceneEnterMenuIntelArchiveNexusFiles` | Enter Intel Archives - Nexus Files. |
| Menu | `SceneEnterMenuSetting` | Enter Settings (PC only). |
| Menu | `SceneEnterMenuSettingPerformanceGraphic` | Enter Settings - Performance & Graphics (PC only). |
| Menu | `SceneEnterMenuSettingAudio` | Enter Settings - Audio (PC only). |
| Menu | `SceneEnterMenuSettingControls` | Enter Settings - Controls (PC only). |
| Menu | `SceneEnterMenuSettingKeybinding` | Enter Settings - Key Binding (PC only). |
| Menu | `SceneEnterMenuSettingLanguage` | Enter Settings - Language (PC only). |
| Menu | `SceneEnterMenuSettingPlatformAccount` | Enter Settings - Platform & Account (PC only). |
| Menu | `SceneEnterMenuSettingOthers` | Enter Settings - Others (PC only). |
| Menu | `SceneEnterMenuHeadhunt` | Enter Operator Headhunt screen. |
| Menu | `SceneEnterMenuOperationalManualUmbralMonument` | Enter Operational Manual - Umbral Monument. |
| Helper | `SceneDialogConfirm` | Click confirm button in dialogs. |
| Helper | `SceneDialogCancel` | Click cancel button in dialogs. |
| Helper | `SceneNoticeRewardsConfirm` | Click confirm button on rewards screens. |
| Helper | `SceneNoticeRewardsUpgrade` | Close the Authority Level-up overlay by clicking the blank area. |
| Helper | `SceneWaitLoadingExit` | Wait for loading screen to disappear. |

## Dijiang Depot Interfaces

The Dijiang Depot public Pipeline provides depot screen recognition, navigation, region switching, and item category selection.

### Base and Region Interfaces

| Target State | Navigation Interface | Validation Interface |
| ------------------ | ----------------------------------------------- | ------------------------------ |
| Any supported region | `SceneEnterMenuBackpackWithDepot` | `InDijiangDepot` |
| Valley IV | `SceneEnterMenuBackpackWithDepotValleyIV` | `InDijiangDepotValleyIV` |
| Wuling | `SceneEnterMenuBackpackWithDepotWuling` | `InDijiangDepotWuling` |

### Item Categories

The region-independent category interfaces enter the Dijiang Depot from any screen and select the target category without changing the region active after entering the depot.

#### Region-Independent Categories

| Category | Navigation Interface | Validation Interface |
| ---------------- | ---------------------------------------------------- | -------------------------------- |
| All | `SceneEnterMenuBackpackWithDepotAll` | `InDijiangDepotAll` |
| Minerals | `SceneEnterMenuBackpackWithDepotOre` | `InDijiangDepotOre` |
| Plants | `SceneEnterMenuBackpackWithDepotPlant` | `InDijiangDepotPlant` |
| Products | `SceneEnterMenuBackpackWithDepotProduct` | `InDijiangDepotProduct` |
| Gatherables | `SceneEnterMenuBackpackWithDepotDoodad` | `InDijiangDepotDoodad` |
| Progression | `SceneEnterMenuBackpackWithDepotNurturance` | `InDijiangDepotNurturance` |
| Usables | `SceneEnterMenuBackpackWithDepotUsable` | `InDijiangDepotUsable` |
| Production | `SceneEnterMenuBackpackWithDepotProducer` | `InDijiangDepotProducer` |
| Personal Device | `SceneEnterMenuBackpackWithDepotPortableDevice` | `InDijiangDepotPortableDevice` |

#### Valley IV

| Category | Navigation Interface | Validation Interface |
| ---------------- | ---------------------------------------------------------- | ------------------------------------------- |
| All | `SceneEnterMenuBackpackWithDepotValleyIVAll` | `InDijiangDepotValleyIVAll` |
| Minerals | `SceneEnterMenuBackpackWithDepotValleyIVOre` | `InDijiangDepotValleyIVOre` |
| Plants | `SceneEnterMenuBackpackWithDepotValleyIVPlant` | `InDijiangDepotValleyIVPlant` |
| Products | `SceneEnterMenuBackpackWithDepotValleyIVProduct` | `InDijiangDepotValleyIVProduct` |
| Gatherables | `SceneEnterMenuBackpackWithDepotValleyIVDoodad` | `InDijiangDepotValleyIVDoodad` |
| Progression | `SceneEnterMenuBackpackWithDepotValleyIVNurturance` | `InDijiangDepotValleyIVNurturance` |
| Usables | `SceneEnterMenuBackpackWithDepotValleyIVUsable` | `InDijiangDepotValleyIVUsable` |
| Production | `SceneEnterMenuBackpackWithDepotValleyIVProducer` | `InDijiangDepotValleyIVProducer` |
| Personal Device | `SceneEnterMenuBackpackWithDepotValleyIVPortableDevice` | `InDijiangDepotValleyIVPortableDevice` |

#### Wuling

| Category | Navigation Interface | Validation Interface |
| ---------------- | -------------------------------------------------------- | ----------------------------------------- |
| All | `SceneEnterMenuBackpackWithDepotWulingAll` | `InDijiangDepotWulingAll` |
| Minerals | `SceneEnterMenuBackpackWithDepotWulingOre` | `InDijiangDepotWulingOre` |
| Plants | `SceneEnterMenuBackpackWithDepotWulingPlant` | `InDijiangDepotWulingPlant` |
| Products | `SceneEnterMenuBackpackWithDepotWulingProduct` | `InDijiangDepotWulingProduct` |
| Gatherables | `SceneEnterMenuBackpackWithDepotWulingDoodad` | `InDijiangDepotWulingDoodad` |
| Progression | `SceneEnterMenuBackpackWithDepotWulingNurturance` | `InDijiangDepotWulingNurturance` |
| Usables | `SceneEnterMenuBackpackWithDepotWulingUsable` | `InDijiangDepotWulingUsable` |
| Production | `SceneEnterMenuBackpackWithDepotWulingProducer` | `InDijiangDepotWulingProducer` |
| Personal Device | `SceneEnterMenuBackpackWithDepotWulingPortableDevice` | `InDijiangDepotWulingPortableDevice` |

## TP Point Interfaces

Every TP point entry is named `SceneEnterWorld<Region><SubArea><Place>`, where the place is the English name of the area the point sits in. For example, `SceneEnterWorldWulingYinglungPassYSTFMainCamp` is Wuling - Yinglung Pass - YSTF Main Camp; when several points in one sub-area share a place name, `North` / `South` / `East` / `West` tells them apart. The full list with descriptions is in `assets/resource/pipeline/Interface/SceneTeleportAnchor.json`, generated by `pnpm generate:SceneTeleport`; some Valley IV entries in Originium Science Park, Origin Lodespring and Power Plateau are still written in `Interface/SceneValleyIV.json`.

The tables below list the sub-area map entries and other teleport entries such as cores.

### Valley IV

| Interface Name | Description |
| -------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `SceneEnterMapValleyIVTheHub` | Enter the Valley IV - The Hub map screen from any screen. |
| `SceneEnterMapValleyIVValleyPass` | Enter the Valley IV - Valley Pass map screen from any screen. |
| `SceneEnterMapValleyIVOriginiumSciencePark` | Enter the Valley IV - Originium Science Park map screen from any screen. |
| `SceneEnterMapValleyIVAburreyQuarry` | Enter the Valley IV - Aburrey Quarry map screen from any screen. |
| `SceneEnterMapValleyIVOriginLodespring` | Enter the Valley IV - Origin Lodespring map screen from any screen. |
| `SceneEnterMapValleyIVPowerPlateau` | Enter the Valley IV - Power Plateau map screen from any screen. |
| `SceneEnterWorldValleyIVTheHub` | Enter the Valley IV - The Hub overworld from any screen. |
| `SceneEnterWorldValleyIVOriginiumScienceParkInfra` | Enter the Valley IV - Originium Science Park (Infra-Station) overworld from any screen. |

### Wuling

| Interface Name | Description |
| --------------------------------------------- | ------------------------------------------------------------------------ |
| `SceneEnterMapWulingWulingCity` | Enter the Wuling - Wuling City map screen from any screen. |
| `SceneEnterMapWulingJingyuValley` | Enter the Wuling - Jingyu Valley map screen from any screen. |
| `SceneEnterMapWulingQingboStockade` | Enter the Wuling - Qingbo Stockade map screen from any screen. |
| `SceneEnterMapWulingMarkerStone` | Enter the Wuling - Marker Stone map screen from any screen. |
| `SceneEnterMapWulingTestArea` | Enter the Wuling - Test Area map screen from any screen. |
| `SceneEnterMapWulingSwordVaultDale` | Enter the Wuling - Sword Vault Dale map screen from any screen. |
| `SceneEnterMapWulingYinglungPass` | Enter the Wuling - Yinglung Pass map screen from any screen. |
| `SceneEnterMapWulingNorthWulingExclusionZone` | Enter the Wuling - North Wuling Exclusion Zone map screen from any screen. |
| `SceneEnterMapWulingSnowyForest` | Enter the Wuling - Snowy Forest map screen from any screen. |
| `SceneEnterMapWulingSisPainting` | Enter the Wuling - Si's Painting map screen from any screen. |
| `SceneEnterWorldWulingWulingCityCore` | Enter Wuling - Wuling City Core overworld from any screen. |
| `SceneEnterWorldWulingMarkerStoneCore` | Enter Wuling - Marker Stone Core overworld from any screen. |

For the complete list of interfaces and detailed descriptions, please refer to the `desc` field of each node in the `SceneXXX.json` files under `assets/resource/pipeline/Interface`.
