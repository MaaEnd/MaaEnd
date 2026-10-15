# Component Guide

This article introduces the project architecture and reusable components of MaaEnd. Before writing a new node, please consult this article to confirm if existing capabilities are already available.

## Project Architecture

MaaEnd is based on [MaaFramework](https://github.com/MaaXYZ/MaaFramework). The main process uses [Pipeline JSON low-code](/assets/resource/pipeline), while complex logic is implemented through [go-service](/agent/go-service). If you intend to join development, you can first read the [MaaFramework related documentation](https://maafw.com/), or watch the [MaaFramework tutorial videos](https://www.bilibili.com/video/BV1yr421E7MW) (which are older, documentation is primary).

The project can be understood as four layers:

1. `assets/interface.json` — Defines the project entry point, controllers, resources, task import lists, and Agent startup items.
2. `assets/tasks/**/*.json` — Defines how tasks are displayed in the UI, entry nodes, and options.
3. `assets/resource/pipeline/**/*.json` — Defines "what to recognize, where to click, and where to go next". **This is the layer most frequently modified during daily development.**
4. `agent/go-service/**` — Only contains complex logic that is difficult to express with Pipelines (complex recognition, calculations, traversal, special interactions). Utilities reused across features are in `agent/go-service/pkg/`; see [Go Service `pkg/` Shared Packages](./go-service-pkg.md).

The execution path for a task:

`Interface Task (Task) → Enter Pipeline Node → Recognition/Operation Loop → Call Go custom logic if necessary`

**Prioritize writing most feature modifications in Task + Pipeline, not Go first.**

## Determining "Which Part to Modify This Time"

| Change Type | Where to Look |
| -------------------------------------------------------- | ------------------------------------- |
| Interface text, task names, option text | `assets/locales/interface/*.json` (all languages) |
| Task orchestration, entry nodes, UI options | `assets/tasks/**/*.json` |
| Recognition, clicking, jumping, waiting, process details | `assets/resource/pipeline/**/*.json` |
| Complex logic (algorithms, traversal, calculations) | `agent/go-service/**`; single recognition algorithms (OpenCV / ONNX) live in `agent/cpp-algo/**` |
| Utilities shared across features (And unwrapping, expressions, OCR numbers, i18n…) | `agent/go-service/pkg/**` ([docs](./go-service-pkg.md)) |

## Pipeline Reusable Nodes

The following nodes can be called directly within Pipelines. It is recommended that all developers master them first:

| Node | Description | Documentation |
| -------------- | ------------------------------------------------------------------------------------ | ---------------------------------------- |
| Common Buttons | White/yellow confirm, cancel, close, teleport, etc. | [common-buttons.md](./common-buttons.md) |
| InScene | Universal scene recognition: determines the current screen scene | [in-scene.md](./in-scene.md) |
| SceneManager | Universal navigation: automatically navigates to the target scene from any interface | [scene-manager.md](./scene-manager.md) |

## Custom Reusable Nodes

The following nodes are implemented based on Go/C++ and have high business-specific features. According to the [coding standards](./coding-standards.md#go-service-standards), they should not be used unnecessarily.

| Node | Description | Documentation |
| ----------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| SubTask / ClearHitCount / ExpressionRecognition | Sub-task scheduling, count clearing, expression recognition | [custom.md](./custom.md) |
| AutoDelivery | A single entry handles depot pickup and delivery at the destination, and provides stage exit anchors | [components/auto-delivery.md](./components/auto-delivery.md) |
| AutoFight | Automatic operation during combat | [components/auto-fight.md](./components/auto-fight.md) |
| CharacterController | Character view rotation, movement, facing targets | [components/character-controller.md](./components/character-controller.md) |
| BetterSliding | Discrete quantity slider adjustment | [components/better-sliding.md](./components/better-sliding.md) |
| MapLocator | AI + CV minimap positioning | [components/map-locator.md](./components/map-locator.md) |
| MapNavigator | Automatic pathfinding: reaches a target coordinate without recording; routes with interactions or map transitions can be recorded | [components/map-navigator.md](./components/map-navigator.md) |
| WorldMap | World map coordinate recognition: given a zone and a base map coordinate, confirms the icon and hands back its screen position | [components/world-map.md](./components/world-map.md) |
| RecoGrid / GridTracker / EssenceGrid | C++ single-frame grid recognition, cross-frame tracking, and Essence integration | [components/recogrid-engine.md](./components/recogrid-engine.md) |
| IconRecognition | C++ item icon recognition, fixed ROI, and Maa Custom Recognition | [components/icon-recognition.md](./components/icon-recognition.md) |
| IMS | Item Management System: A2 sync / A1 delta / A3 add, R1 quantity / R2 readiness | [components/ims.md](./components/ims.md) |

## Task Maintenance Documentation

The following tasks have dedicated maintenance documentation. **You must read the corresponding documentation before modifying these tasks**:

| Task | Documentation |
| ------------------------------------- | ------------------------------------------------------------------------ |
| AutoStockpile (Auto Stockpile) | [tasks/auto-stockpile-maintain.md](./tasks/auto-stockpile-maintain.md) |
| DijiangRewards (Base Rewards) | [tasks/dijiang-rewards-maintain.md](./tasks/dijiang-rewards-maintain.md) |
| CreditShopping (Credit Shop) | [tasks/credit-shopping-maintain.md](./tasks/credit-shopping-maintain.md) |
| AutoStockStaple (Buy Staple Goods) | [tasks/auto-stockstaple-maintain.md](./tasks/auto-stockstaple-maintain.md) |
| EnvironmentMonitoring (Environment Monitoring) | [tasks/environment-monitoring-maintain.md](./tasks/environment-monitoring-maintain.md) |
| OutpostTrading (Outpost Trading) | [tasks/outpost-trading-maintain.md](./tasks/outpost-trading-maintain.md) |
| SeizeDeliveryJobs (Accept Commission & Delivery) | [tasks/seize-delivery-jobs-maintain.md](./tasks/seize-delivery-jobs-maintain.md) |
| GiftOperator (Gift to Operator) | [tasks/gift-operator-maintain.md](./tasks/gift-operator-maintain.md) |
| DeliveryJobs (Delivery Jobs) | [tasks/delivery-jobs-maintain.md](./tasks/delivery-jobs-maintain.md) |

## External Documentation

- [MaaFramework Official Documentation](https://maafw.com/)
- [Pipeline Protocol](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.1-PipelineProtocol.md)
- [Project Interface V2](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.3-ProjectInterfaceV2.md)
