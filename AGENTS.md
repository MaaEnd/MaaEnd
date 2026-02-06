# MaaEnd AI Agent 编码指南

欢迎参与 MaaEnd 的开发！本指南旨在帮助 AI Agent（如 GitHub Copilot, Cursor 等）快速理解项目结构及编码规范，以提供更高质量的代码建议。

## 项目概览

**MaaEnd** 是基于 [MaaFramework](https://github.com/MaaXYZ/MaaFramework) 开发的自动化工具。项目采用 **方案二（JSON + 自定义逻辑扩展）** 进行构建。

- **主体流程**：采用 Pipeline JSON 低代码实现，位于 [`assets/resource/pipeline`](assets/resource/pipeline) 。
- **复杂逻辑**：通过 Go 编写的 [`agent/go-service`](agent/go-service) 扩展实现。
- **配置入口**：[`assets/interface.json`](assets/interface.json) 定义了任务列表、控制器及 Agent 启动项。

## 项目结构关键路径

- [`assets/resource/pipeline/`](assets/resource/pipeline/): 所有的 Pipeline 任务逻辑。
- [`assets/resource/image/`](assets/resource/image/): 识别所需的图片资源（基准分辨率 720p）。
- [`agent/go-service/`](agent/go-service/): 自定义 Go Service 源码。
- [`assets/misc/locales/`](assets/misc/locales/): 国际化本地化文件（任务名称、UI 文本等）。
- [`docs/developers/development.md`](docs/developers/development.md:1): 核心开发手册，包含环境配置与规范。

## 编码规范

### 1. Pipeline 低代码规范

- **协议合规性**：所有 Pipeline JSON 字段必须严格遵循 [MaaFramework Pipeline 协议规范](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.1-PipelineProtocol.md)。在新增或修改节点时，务必核对字段名称、类型及取值范围。
- **状态驱动**：遵循“识别 -> 操作 -> 识别”的循环。严禁盲目使用 `pre_delay` 或 `post_delay`。
- **高命中率**：尽可能扩充 `next` 列表，确保在第一轮截图（一次心跳）内命中目标节点。
- **原子化操作**：每一步点击或交互都应基于明确的识别结果，不要假设点击后的状态。
- **分辨率基准**：所有坐标和图片必须以 **720p (1280x720)** 为基准。

### 2. Go Service 规范

- **职责分离**：Go Service 仅用于处理 Pipeline 难以实现的复杂图像算法或特殊交互逻辑。
- **流程控制**：禁止在 Go 中编写大规模的业务流程，流程控制应交由 Pipeline JSON 负责。
- **注册机制**：新的自定义动作/识别需在 `registerAll()` 中注册，具体实现参考各子包。

### 3. 资源维护与任务新增

- **接口定义合规性**：[`assets/interface.json`](assets/interface.json) 必须符合 [MaaFramework 项目接口 V2](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.3-ProjectInterfaceV2.md) 规范。
- **国际化同步**：新增任务时，必须在 [`assets/misc/locales/`](assets/misc/locales/) 下的相关语言 JSON 文件中添加对应的任务名称及描述。
- **配置同步**：`assets/interface.json` 的修改需要手动从 `install` 目录同步回源码（如果是通过工具修改）。

### 4. 代码格式化规范

- **Prettier 约束**：所有 JSON、YAML 文件必须遵循 [`.prettierrc`](.prettierrc) 的配置。
- **关键规则**：
  - 使用 **4 个空格** 缩进。
  - 数组格式受 `prettier-plugin-multiline-arrays` 插件影响，数组元素必须换行排列（阈值为 1）。
  - 提交前请务必执行格式化，确保代码风格统一。

## Code Review 重点

在审查代码（Review）时，请重点关注以下事项：

- **协议字段校验**：检查 Pipeline 和 Interface JSON 中的字段是否合法，是否存在拼写错误或使用了协议不支持的属性。参考相关协议文档。
- **禁止硬延迟**：检查是否出现了不必要的 `pre_delay`, `post_delay`, `timeout`。应优先考虑通过增加中间识别节点来优化流程。
- **截图效率**：检查 `next` 列表是否足够完善。理想情况下，应能覆盖当前操作后所有可能的预期画面，实现“一次心跳，立即命中”。
- **坐标合法性**：所有新定义的 `roi` 或 `target` 坐标必须基于 **1280x720** 分辨率。
- **代码格式化**：确保代码符合 [`.prettierrc`](.prettierrc) 规范，特别是 JSON 中的缩进格式。
- **国际化缺失**：检查新增任务是否在 `assets/misc/locales/` 文件夹中配置了多语言文本。
- **逻辑边界**：检查 Pipeline 是否处理了异常情况（如弹窗阻断）。每一步点击后都应有相应的识别验证。
- **Go 职责界限**：审查 Go Service 中的代码是否包含本应由 Pipeline 处理的业务逻辑。确保 Go 仅作为“工具”被 Pipeline 调用。
- **配置文件同步**：若修改了任务列表，务必确认 `assets/interface.json` 已正确更新。

## 相关文档链接

- [MaaFramework Pipeline 协议规范](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.1-PipelineProtocol.md)
- [MaaFramework 项目接口 V2](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.3-ProjectInterfaceV2.md)
- [MaaEnd 开发手册](docs/developers/development.md)
