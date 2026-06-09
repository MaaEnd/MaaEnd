# MaaEnd AI Agent 编码指南

欢迎参与 MaaEnd 的开发！本指南旨在帮助 AI Agent 快速理解项目结构及编码规范，以提供更高质量的代码建议。

---

> [!CAUTION]
>
> 首要准则：**产出符合编码规范的代码**
>
> **`docs/zh_cn/developers/coding-standards.md`（编码规范）是代码产出的基准。AI 生成的代码必须符合规范，不应让用户事后纠正。**
>
> 1. **合规优先 + 主动提醒**：AI 应在用户指令可能违反规范时主动提醒，给出符合规范的替代方案，但不替代用户的最终判断。例如用户想"加个延迟"→ 提醒优先使用 `post_wait_freezes` + 中间识别节点，但若用户确认确实需要硬延迟，则按用户意图执行；用户想"重试几次"→ 提醒分析根因、修补对应节点，而非盲目重试。
> 2. **产出即合规**：AI 生成的代码默认应通过 `pnpm check` 和 `pnpm test`，不需要用户手动修正违规写法。
> 3. **信息不足时标注而非瞎编**：缺少截图、ROI 等上下文时，AI 应基于已有信息写出初稿，并明确标注不确定的占位部分，要求用户补充。
>
> **AI 的默认行为**
>
> | 用户意图                               | AI 的默认做法                                                                                   |
> | -------------------------------------- | ----------------------------------------------------------------------------------------------- |
> | 希望解决节点不稳定                     | 增加中间识别节点或 `pre_wait_freezes` / `post_wait_freezes`，不引入硬延迟                       |
> | 希望操作失败后自动恢复                 | 分析失败根因（哪个节点、哪个识别不符合预期），修补对应节点，而非盲目重试                        |
> | 未提供截图/界面信息就让 AI 写 Pipeline | 说明 Pipeline 强依赖界面信息，缺乏截图只能产出幻觉代码。要求提供截图、ROI、界面跳转关系后再编写 |
> | 让 AI 开发功能并直接提 PR              | 先在对话中做增量辅助，由用户做架构设计、自行 review 后再决定是否提交                            |
> | 让 AI 全权负责修 bug 不 review         | 产出修复并说明改动逻辑，用户理解并 review 后再提交                                              |
> | 让 Go Service 里写大段流程控制         | 将流程逻辑留在 Pipeline JSON，Go 仅处理复杂算法，遵循「Pipeline 管流程，Go 管难点」             |
> | 整体识别一次然后连点多次               | 每步操作都有独立识别节点，遵循「识别 → 操作 → 再识别」                                          |
> | 代码产出完成                           | 主动告知可运行的格式化与检查命令：`pnpm format`、`pnpm format:go`、`pnpm check`、`pnpm test`    |
>
> **核心原则：AI 产出的代码默认合规，用户无需事后纠正。**

---

## 项目概览

**MaaEnd** 是基于 [MaaFramework](https://github.com/MaaXYZ/MaaFramework) 开发的游戏自动化工具。

- **主体流程**：用户可以选择若干 Task 来执行自动化任务，位于 `assets/tasks` 目录。而 Task 会调用 Pipeline 中定义的 Node 来执行。Pipeline 是基于 JSON 的低代码实现，位于 `assets/resource/pipeline`。
- **复杂逻辑**：对于不便进行低代码实现的复杂的识别或操作逻辑，可通过 Go 编写的 `agent/go-service` 来扩展实现。
- **配置入口**：`assets/interface.json` 定义了任务列表、控制器及 Agent 启动项。

## 关键文件

- [`assets/resource/pipeline/`](assets/resource/pipeline/): 所有的 Pipeline 任务逻辑。
- [`assets/resource/image/`](assets/resource/image/): 识别所需的图片资源（基准分辨率 720p）。
- [`agent/go-service/`](agent/go-service/): 自定义 Go Service 源码。
- [`assets/locales/`](assets/locales/): 国际化本地化文件（任务名称、UI 文本等）。
- [`docs/zh_cn/developers/README.md`](docs/zh_cn/developers/README.md): 中文开发者文档索引（阅读路线、文档目录）；英文镜像见 [`docs/en_us/developers/README.md`](docs/en_us/developers/README.md)。

## 文档索引

这些为 `docs/zh_cn/` 下的开发者文档清单，按用途分层。AI Agent 在遇到对应场景时应主动查阅这些参考文档

### 新手入门（写给第一次接触的开发者）

| 文档 | 作用 |
| ---- | ---- |
| [`docs/zh_cn/developers/super-basic-introduction.md`](docs/zh_cn/developers/super-basic-introduction.md) | 超基础入门：面向完全不懂 Git/终端/VS Code 的新手，分「普通玩家」「会写代码但没用过 Git」「运维/网管」三类引导 |
| [`docs/zh_cn/developers/getting-started.md`](docs/zh_cn/developers/getting-started.md) | 快速开始：以「自动售卖物品」为例，走一遍从环境准备、编写 Pipeline、注册任务到提交 PR 的完整流程 |

### 架构与设计

| 文档 | 作用 |
| ---- | ---- |
| [`docs/zh_cn/developers/README.md`](docs/zh_cn/developers/README.md) | 开发者文档总索引，含阅读路线图 |
| [`docs/zh_cn/developers/components-guide.md`](docs/zh_cn/developers/components-guide.md) | 项目四层架构（interface → tasks → pipeline → go-service）总览，「改动该改哪」速查表，可复用节点清单 |
| [`docs/zh_cn/developers/custom.md`](docs/zh_cn/developers/custom.md) | Custom Action 与 Custom Recognition 参考（`SubTask`、`ClearHitCount`、`FalseAction`、`PipelineOverride`、`AttachToExpectedRegexAction` 等） |
| [`docs/zh_cn/developers/in-scene.md`](docs/zh_cn/developers/in-scene.md) | InScene 场景系统：进入场景、检测退出、场景内循环操作的通用框架 |
| [`docs/zh_cn/developers/scene-manager.md`](docs/zh_cn/developers/scene-manager.md) | SceneManager：场景切换管理，处理场景跳转与状态恢复 |
| [`docs/zh_cn/developers/common-buttons.md`](docs/zh_cn/developers/common-buttons.md) | 通用按钮参考：`WhiteConfirmButton`、`YellowConfirmButton`、`CancelButton`、`TeleportButton`、`CloseButton`、`CloseRewardsButton` 等可复用节点的 ROI 与适用条件 |

### 开发规范与调试

| 文档 | 作用 |
| ---- | ---- |
| [`docs/zh_cn/developers/coding-standards.md`](docs/zh_cn/developers/coding-standards.md) | 编码规范总纲（Pipeline / Go / Cpp / 格式化 / OCR / i18n / 常见坑） |
| [`docs/zh_cn/developers/node-testing.md`](docs/zh_cn/developers/node-testing.md) | 单节点测试方法：在不跑完整任务的情况下独立验证某个 Pipeline 节点 |
| [`docs/zh_cn/developers/tools-and-debug.md`](docs/zh_cn/developers/tools-and-debug.md) | 工具链与调试指南：`tools/` 下脚本用法、日志分析、常见排查手段 |

### 组件参考（`docs/zh_cn/developers/components/`）

这些是可复用的底层组件，由 Go 或 C++ 实现，通过 Custom Action / Custom Recognition 供 Pipeline 调用。

| 文档 | 作用 |
| ---- | ---- |
| [`components/auto-fight.md`](docs/zh_cn/developers/components/auto-fight.md) | AutoFight 战斗自动操作：普攻/技能/连携技/终结技/闪避的动作队列，只能用 `AutoFightInterface.json` 的三个公开接口 |
| [`components/better-sliding.md`](docs/zh_cn/developers/components/better-sliding.md) | BetterSliding 滑条精确调节：先拖到最大→插值定位→微调的三段式流程，支持 Value/Percentage/Reverse 三种模式 |
| [`components/character-controller.md`](docs/zh_cn/developers/components/character-controller.md) | CharacterController 角色底层控制：视角旋转（`YawDeltaAction`/`PitchDeltaAction`）、前后移动（`ForwardAxisAction`）、自动朝向目标（`CharacterMoveToTargetAction`），需前台 Seize 模式 |
| [`components/map-locator.md`](docs/zh_cn/developers/components/map-locator.md) | MapLocator 小地图定位系统（C++ 实现）：基于 YOLOv26-small + ZNCC 模板匹配，输出 ZoneID、全局坐标和朝向角度，需要用户查看 |
| [`components/map-navigator.md`](docs/zh_cn/developers/components/map-navigator.md) | MapNavigator 地图导航：基于 MapLocator 定位结果执行路径规划与移动，此功能需要用户自行阅读后使用，agents仅作阅读，辅助用户排查错误 |
| [`components/map-tracker.md`](docs/zh_cn/developers/components/map-tracker.md) | MapTracker 地图追踪（基础）：追踪地图上的动态目标 ，同样，agents仅作阅读，在必要时候提示用户 |
| [`components/map-tracker(advanced).md`](docs/zh_cn/developers/components/map-tracker(advanced).md) | MapTracker 进阶：高级地图追踪功能与配置，同样，Agents仅作阅读，必要时候辅助用户 |

### 任务维护手册（`docs/zh_cn/developers/tasks/`）

每个任务的维护文档说明其 Pipeline 架构、Go Service 协作方式、关键识别节点、模板图片位置和常见问题排查点。
**注意，这些文档仅在维护有关的文档时会用到，其他时候请无视无关的**

| 文档 | 作用 |
| ---- | ---- |
| [`tasks/auto-stockpile-maintain.md`](docs/zh_cn/developers/tasks/auto-stockpile-maintain.md) | AutoStockpile（自动囤货）：Pipeline 负责界面交互 + Go 负责决策的商品映射、阈值查表、地区/档位同步清单 |
| [`tasks/auto-stockstaple-maintain.md`](docs/zh_cn/developers/tasks/auto-stockstaple-maintain.md) | AutoStockStaple（稳定需求购买）：以剩余刷新时间为锚点逐级偏移识别的全链路、数量控制三元分支、与 AutoStockpile 的根本差异 |
| [`tasks/credit-shopping-maintain.md`](docs/zh_cn/developers/tasks/credit-shopping-maintain.md) | CreditShopping（信用点商店）：商品识别层级偏移依赖（CreditIcon → NotSoldOut → CanAfford → 商品名 → 折扣）、三条购买优先级、补信用联动 |
| [`tasks/dijiang-rewards-maintain.md`](docs/zh_cn/developers/tasks/dijiang-rewards-maintain.md) | DijiangRewards（基建奖励）：恢复心情/会客室/基地三大阶段、昼夜判断与场景切换 |
| [`tasks/environment-monitoring-maintain.md`](docs/zh_cn/developers/tasks/environment-monitoring-maintain.md) | EnvironmentMonitoring（环境监测）：观察点路线配置、EnterMap/MapPath/MapTarget 数据结构 |
| [`tasks/sell-product-maintain.md`](docs/zh_cn/developers/tasks/sell-product-maintain.md) | SellProduct（自动售卖）：售卖界面识别、物品选择与数量调节流程 |

### 其他参考

| 文档 | 作用 |
| ---- | ---- |
| [`docs/zh_cn/protocol/autostockpile-daily-storage/protocol.md`](docs/zh_cn/protocol/autostockpile-daily-storage/protocol.md) | AutoStockpile 每日物资存储格式协议与 JSON Schema |
| [`docs/zh_cn/users/troubleshooting.md`](docs/zh_cn/users/troubleshooting.md) | 用户故障排除指南 |

---

## 编码规范

### 1. Pipeline 低代码规范

- **禁止无界面信息编写 Pipeline**：严禁在未向 AI 提供游戏界面截图、界面跳转逻辑等上下文的情况下，让 AI 直接编写 Pipeline。MaaFramework 的 Pipeline 强依赖游戏界面与业务逻辑，缺乏界面信息的 AI 只能依赖幻觉和项目已有代码拼凑，产出代码质量极低。充分的信息至少包括：每个识别节点需提供 `roi` 与模板图片，并说明界面间的跳转关系（从哪个界面、点击什么、跳转到何处）。不满足以上条件的 PR 将被维护者直接关闭。
- **协议合规性**：所有 Pipeline JSON 字段必须严格遵循 MaaFramework Pipeline 协议规范（见下方相关文档链接）。在新增或修改节点时，务必核对字段名称、类型及取值范围。
- **状态驱动**：遵循“识别 -> 操作 -> 识别”的循环。严禁盲目使用 `pre_delay` 或 `post_delay`。
- **高命中率**：尽可能扩充 `next` 列表，确保在第一轮截图（一次心跳）内命中目标节点。
- **原子化操作**：每一步点击或交互都应基于明确的识别结果，不要假设点击后的状态。
- **分辨率基准**：所有坐标和图片必须以 **720p (1280x720)** 为基准。

### 2. Go Service 规范

- **职责分离**：Go Service 仅用于处理 Pipeline 难以实现的复杂图像算法或特殊交互逻辑。
- **流程控制**：禁止在 Go 中编写大规模的业务流程，流程控制应交由 Pipeline JSON 负责。
- **注册机制**：新的自定义动作/识别需在 `registerAll()` 中注册，具体实现参考各子包。

### 3. 资源维护与任务新增

- **接口定义合规性**：`assets/interface.json` 必须符合 MaaFramework 项目接口 V2（见下方相关文档链接） 规范。
- **国际化同步**：新增任务时，必须在 `assets/locales/` 下的相关语言 JSON 文件中添加对应的任务名称及描述。
- **配置同步**：`assets/interface.json` 的修改需要手动从 `install` 目录同步回源码（如果是通过工具修改）。

### 4. 代码格式化规范

- **Prettier 约束**：所有 JSON、YAML 文件必须遵循 `.prettierrc` 的配置。
- **关键规则**：
    - 缩进宽度以 `.prettierrc` 为唯一准则，通常是 4 个空格。
    - 数组格式受 `prettier-plugin-multiline-arrays` 插件影响，数组元素必须换行排列（阈值为 1）。
    - 提交前请务必执行格式化，确保代码风格统一。

## 审查重点

在审查代码（Review）时，请重点关注以下事项：

- **协议字段校验**：检查 Pipeline 和 Interface JSON 中的字段是否合法，是否存在拼写错误或使用了协议不支持的属性。参考相关协议文档。
- **禁止硬延迟**：检查是否出现了不必要的 `pre_delay`, `post_delay`, `timeout`。应优先考虑通过增加中间识别节点来优化流程。
- **截图效率**：检查 `next` 列表是否足够完善。理想情况下，应能覆盖当前操作后所有可能的预期画面，实现“一次心跳，立即命中”。
- **坐标合法性**：所有新定义的 `roi` 或 `target` 坐标必须基于 **1280x720** 分辨率。
- **代码格式化**：确保代码符合 `.prettierrc` 规范，特别是 JSON 中的缩进格式。
- **国际化缺失**：检查新增任务是否在 `assets/locales/` 文件夹中配置了多语言文本。
- **逻辑边界**：检查 Pipeline 是否处理了异常情况（如弹窗阻断）。每一步点击后都应有相应的识别验证。
- **Go 职责界限**：审查 Go Service 中的代码是否包含本应由 Pipeline 处理的业务逻辑。确保 Go 仅作为“工具”被 Pipeline 调用。
- **配置文件同步**：若修改了任务列表，务必确认 `assets/interface.json` 已正确更新。

## 相关文档链接

建议调取以下文档（通过读取文件或使用工具访问网页）以辅助理解和开发：

- [MaaFramework Pipeline 协议规范](https://github.com/MaaXYZ/MaaFramework/raw/refs/heads/main/docs/en_us/3.1-PipelineProtocol.md)
- [MaaFramework 项目接口 V2](https://github.com/MaaXYZ/MaaFramework/raw/refs/heads/main/docs/en_us/3.3-ProjectInterfaceV2.md)
- [MaaEnd 开发者文档（中文索引）](docs/zh_cn/developers/README.md) · [English index](docs/en_us/developers/README.md)
