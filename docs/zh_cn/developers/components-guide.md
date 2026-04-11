# 组件指南

本文介绍 MaaEnd 的项目架构和可复用组件。开始写新节点前，请先查阅本文确认是否已有现成能力。

## 项目架构

MaaEnd 基于 [MaaFramework](https://github.com/MaaXYZ/MaaFramework)，主体流程采用 [Pipeline JSON 低代码](/assets/resource/pipeline)，复杂逻辑通过 [go-service](/agent/go-service) 编码实现。若有意加入开发，可以先阅读 [MaaFramework 相关文档](https://maafw.com/)，也可以查看 [MaaFramework 教学视频](https://www.bilibili.com/video/BV1yr421E7MW)（较旧，以文档为主）。

项目可以理解为四层：

1. `assets/interface.json` — 定义项目入口、控制器、资源、任务导入列表、Agent 启动项。
2. `assets/tasks/**/*.json` — 定义任务在 UI 里的展示、入口节点、可选项。
3. `assets/resource/pipeline/**/*.json` — 定义"识别什么、点哪里、下一步去哪"。**日常开发最常改的一层。**
4. `agent/go-service/**` — 仅放 Pipeline 难以表达的复杂逻辑（复杂识别、计算、遍历、特殊交互）。

一条任务的执行路径：

`界面任务(Task) → 进入 Pipeline 节点 → 识别/操作循环 → 必要时调用 Go 自定义逻辑`

**大多数功能修改优先落在 Task + Pipeline，不是先写 Go。**

## 判断"这次改动该改哪"

| 改动类型                         | 该看哪里                              |
| -------------------------------- | ------------------------------------- |
| 界面文案、任务名、选项文案       | `assets/locales/interface/zh_cn.json` |
| 任务编排、入口节点、UI 选项      | `assets/tasks/**/*.json`              |
| 识别、点击、跳转、等待、流程细节 | `assets/resource/pipeline/**/*.json`  |
| 复杂逻辑（算法、遍历、计算）     | `agent/go-service/**`                 |

## Pipeline 可复用节点

以下节点可以直接在 Pipeline 中调用，建议所有开发者优先掌握：

| 节点         | 说明                                   | 文档                                     |
| ------------ | -------------------------------------- | ---------------------------------------- |
| 通用按钮     | 白色/黄色确认、取消、关闭、传送等      | [common-buttons.md](./common-buttons.md) |
| SceneManager | 万能跳转：从任意界面自动导航到目标场景 | [scene-manager.md](./scene-manager.md)   |

## Custom 可复用节点

以下节点基于 Go/C++ 实现，具有高业务化特点。根据[编码规范](./coding-standards.md#go-service-规范)，不应在非必要情况下使用。

| 节点                                            | 说明                             | 文档                                                                       |
| ----------------------------------------------- | -------------------------------- | -------------------------------------------------------------------------- |
| SubTask / ClearHitCount / ExpressionRecognition | 子任务调度、计数清理、表达式识别 | [custom.md](./custom.md)                                                   |
| AutoFight                                       | 战斗内自动操作                   | [components/auto-fight.md](./components/auto-fight.md)                     |
| CharacterController                             | 角色视角旋转、移动、朝向目标     | [components/character-controller.md](./components/character-controller.md) |
| QuantizedSliding                                | 离散数量滑条调节                 | [components/quantized-sliding.md](./components/quantized-sliding.md)       |
| MapLocator                                      | AI + CV 小地图定位               | [components/map-locator.md](./components/map-locator.md)                   |
| MapTracker                                      | 小地图追踪与路径移动             | [components/map-tracker.md](./components/map-tracker.md)                   |
| MapNavigator                                    | 高精度自动导航 + GUI 录制工具    | [components/map-navigator.md](./components/map-navigator.md)               |

## 任务维护文档

以下任务有专门的维护文档。**修改这些任务前必须先阅读对应文档**：

| 任务                      | 文档                                                                     |
| ------------------------- | ------------------------------------------------------------------------ |
| AutoStockpile 自动囤货    | [tasks/auto-stockpile-maintain.md](./tasks/auto-stockpile-maintain.md)   |
| DijiangRewards 基建任务   | [tasks/dijiang-rewards-maintain.md](./tasks/dijiang-rewards-maintain.md) |
| CreditShopping 信用点商店 | [tasks/credit-shopping-maintain.md](./tasks/credit-shopping-maintain.md) |

## 外部文档

- [MaaFramework 官方文档](https://maafw.com/)
- [Pipeline 协议](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/zh_cn/3.1-%E4%BB%BB%E5%8A%A1%E6%B5%81%E6%B0%B4%E7%BA%BF%E5%8D%8F%E8%AE%AE.md)
- [项目接口 V2](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/zh_cn/3.3-ProjectInterfaceV2%E5%8D%8F%E8%AE%AE.md)
