# Component Guide

This article introduces the project architecture and reusable components of MaaEnd. Before writing a new node, please consult this article to confirm if existing capabilities are already available.

## Project Architecture

MaaEnd is based on [MaaFramework](https://github.com/MaaXYZ/MaaFramework). The main process uses [Pipeline JSON low-code](/assets/resource/pipeline), while complex logic is implemented through [go-service](/agent/go-service). If you intend to join development, you can first read the [MaaFramework related documentation](https://maafw.com/), or watch the [MaaFramework tutorial videos](https://www.bilibili.com/video/BV1yr421E7MW) (which are older, documentation is primary).

The project can be understood as four layers:

1. `assets/interface.json` — Defines the project entry point, controllers, resources, task import lists, and Agent startup items.
2. `assets/tasks/**/*.json` — Defines how tasks are displayed in the UI, entry nodes, and options.
3. `assets/resource/pipeline/**/*.json` — Defines "what to recognize, where to click, and where to go next". **This is the layer most frequently modified during daily development.**
4. `agent/go-service/**` — Only contains complex logic that is difficult to express with Pipelines (complex recognition, calculations, traversal, special interactions).

The execution path for a task:

`Interface Task (Task) → Enter Pipeline Node → Recognition/Operation Loop → Call Go custom logic if necessary`

**Prioritize writing most feature modifications in Task + Pipeline, not Go first.**

## Determining "Which Part to Modify This Time"

| Change Type                                              | Where to Look                         |
| -------------------------------------------------------- | ------------------------------------- |
| Interface text, task names, option text                  | `assets/locales/interface/zh_cn.json` |
| Task orchestration, entry nodes, UI options              | `assets/tasks/**/*.json`              |
| Recognition, clicking, jumping, waiting, process details | `assets/resource/pipeline/**/*.json`  |
| Complex logic (algorithms, traversal, calculations)      | `agent/go-service/**`                 |

## Pipeline Reusable Nodes

The following nodes can be called directly within Pipelines. It is recommended that all developers master them first:

| Node           | Description                                                                          | Documentation                            |
| -------------- | ------------------------------------------------------------------------------------ | ---------------------------------------- |
| Common Buttons | White/yellow confirm, cancel, close, teleport, etc.                                  | [common-buttons.md](./common-buttons.md) |
| InScene        | Universal scene recognition: determines the current screen scene                     | [in-scene.md](./in-scene.md)             |
| SceneManager   | Universal navigation: automatically navigates to the target scene from any interface | [scene-manager.md](./scene-manager.md)   |

## Custom Reusable Nodes

The following nodes are implemented based on Go/C++ and have high business-specific features. According to the [coding standards](./coding-standards.md#go-service-standards), they should not be used unnecessarily.

| Node                                            | Description                                                 | Documentation                                                              |
| ----------------------------------------------- | ----------------------------------------------------------- | -------------------------------------------------------------------------- |
| SubTask / ClearHitCount / ExpressionRecognition | Sub-task scheduling, count clearing, expression recognition | [custom.md](./custom.md)                                                   |
| AutoFight                                       | Automatic operation during combat                           | [components/auto-fight.md](./components/auto-fight.md)                     |
| CharacterController                             | Character view rotation, movement, facing targets           | [components/character-controller.md](./components/character-controller.md) |
| BetterSliding                                   | Discrete quantity slider adjustment                         | [components/better-sliding.md](./components/better-sliding.md)             |
| MapLocator                                      | AI + CV minimap positioning                                 | [components/map-locator.md](./components/map-locator.md)                   |
| MapTracker                                      | Minimap tracking and path movement                          | [components/map-tracker.md](./components/map-tracker.md)                   |
| MapNavigator                                    | High-precision automatic navigation + BaseNav A\* preview   | [components/map-navigator.md](./components/map-navigator.md)               |
| RecoGrid Engine                                 | C++ grid recognition and rolling cumulative scanning engine | [components/recogrid-engine.md](./components/recogrid-engine.md)           |

## Task Maintenance Documentation

The following tasks have dedicated maintenance documentation. **You must read the corresponding documentation before modifying these tasks**:

| Task                                  | Documentation                                                            |
| ------------------------------------- | ------------------------------------------------------------------------ |
| AutoStockpile (Automatic Stockpiling) | [tasks/auto-stockpile-maintain.md](./tasks/auto-stockpile-maintain.md)   |
| DijiangRewards (Base Building Tasks)  | [tasks/dijiang-rewards-maintain.md](./tasks/dijiang-rewards-maintain.md) |
| CreditShopping (Credit Point Store)   | [tasks/credit-shopping-maintain.md](./tasks/credit-shopping-maintain.md) |

## External Documentation

- [MaaFramework Official Documentation](https://maafw.com/)
- [Pipeline Protocol](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/zh_cn/3.1-%E4%BB%BB%E5%8A%A1%E6%B5%81%E6%B0%B4%E7%BA%BF%E5%8D%8F%E8%AE%AE.md)
- [Project Interface V2](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/zh_cn/3.3-ProjectInterfaceV2%E5%8D%8F%E8%AE%AE.md)
