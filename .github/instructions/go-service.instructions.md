---
applyTo: "agent/go-service/**"
---

本说明仅在对 `agent/go-service/` 下的文件的请求中生效（含 Copilot 代码评审与编码智能体）。其他目录请参阅项目根目录的 `AGENTS.md`。

<RequiredChecks>

<RegistrationAndPipeline>

- **子包内**：新增的 CustomRecognizer、CustomAction、EventSink 必须在对应子包内注册。子包应单独使用 `register.go` 定义并维护 `Register()`，在该函数中完成本包所有组件的注册。
- **main 聚合**：各子包的 `Register()` 必须在 main 包的 `registerAll()` 中被调用，否则组件不会生效。
- **与 Pipeline/配置一致**：注册名称、参数需与 Pipeline 中 CustomRecognizer / CustomAction 的 `name`、`params` 一致。

</RegistrationAndPipeline>

<FileManagementAndReadability>

- **Maa 的 custom 组件应按单文件边界管理**：每个自定义识别器或动作的实现尽量集中在单独文件中，避免单文件行数爆炸。
- 同一子包内可按职责拆分为多个 `.go` 文件（如 `register.go`、按功能命名的实现文件），保持单文件职责清晰、行数可控，便于阅读与维护。
- **每个实现体必须有编译期接口校验**：所有注册的 CustomRecognitionRunner、CustomActionRunner、ResourceEventSink、TaskerEventSink 等实现类型，都必须在某处（见下一条）包含 `var _ 接口 = &类型{}` 的编译期校验，确保实现与框架接口一致；审查时若发现某注册组件缺少对应校验，应要求补上。
- **接口实现校验应靠近类型定义**：用于编译期校验类型实现某接口的写法（如 `var _ maa.CustomRecognitionRunner = &RealTimeAutoFightEntryRecognition{}`）应写在**定义该类型的文件**中，不要集中放在 `register.go`。这样在阅读实现时即可确认接口契约；审查时若发现校验与类型定义分离，应要求将校验挪到定义附近。

</FileManagementAndReadability>

<NamingAndGoPhilosophy>

- **包名**：简短、小写、单词优先，符合 [Go 包命名惯例](https://go.dev/blog/package-names)；避免冗余前缀（如包名已为 `resell` 时不再使用 `resellXXX` 子包名除非确有层级必要）。
- **变量与类型名**：使用清晰、简洁的驼峰命名；避免冗余的“命名空间式”长前缀（如 `ResellServiceHandler` 在包 `resell` 下可简化为 `Handler` 或按职责命名）。
- 导出符号名应能表意，未导出实现细节保持简短即可。

</NamingAndGoPhilosophy>

<CommentsAndDocumentation>

- **导出函数**：必须添加注释，说明用途、参数含义、返回值含义及主要错误情况；注释以符号名开头（便于 `go doc`）。
- **导出类型与全局变量**：必须添加注释，说明用途与适用场景。
- **未导出但复杂逻辑的注释**：未导出的函数、类型、变量在逻辑复杂或非显而易见时也应添加简要注释。例如：初始化或配置加载（如 `initLogger`、资源路径解析）、多分支错误处理、与外部行为强相关的约定（如工作目录、文件布局）、算法或状态机步骤等，应有一句说明用途或前提，避免可读性降低；审查时可根据“读者能否在不读实现的情况下理解何时/为何被调用”判断是否需要补注释。

</CommentsAndDocumentation>

<LogStyleZerolog>

- **统一使用 zerolog**：禁止 `log.Printf`、`log.Println` 等旧式写法；一律改为 zerolog 链式调用。
- **链式写法**：级别（`log.Info()` / `log.Error()`）→ 链式挂字段（`.Err(err)`、`.Str("key", "val")` 等）→ 以 `.Msg("简短描述")` 收尾。
- **上下文用字段，不拼进 Msg**：组件名、步骤、场景等用链式字段（如 `.Str("component", "EssenceFilter")`、`.Str("step", "Step2")`）。禁止在 `Msg` 里用前缀（如 `<Component>`、`[Step2]`）或长句（如「Step2 ok: …」「MatchEssenceSkills: …」），否则无法按字段检索；审查时要求改为「字段 + 简短 Msg」。
- **错误、参数、识别结果**：一律用链式字段（`.Err(err)`、`.Int("x", x)` 等），不拼进 `Msg` 字符串。
- **示例**：

    ```go
    log.Info().
        Str("component", "EssenceFilter").
        Str("step", "Step2").
        Msg("matcher config loaded")

    log.Error().
        Err(err).
        Msg("xxx")
    ```

</LogStyleZerolog>

<CodeQualityAndBaseline>

- 错误应合理返回或记录，便于上层根据返回值/日志做分支处理。
- 图像或坐标处理需明确以 **720p (1280×720)** 为基准。

</CodeQualityAndBaseline>

</RequiredChecks>

<SuggestedChecks>

- 重复逻辑是否可抽取为共用函数或子包。
- 是否有多余的硬编码延迟（如 `time.Sleep`）；若仅为“等界面稳定”，应优先由 Pipeline 用识别节点驱动。若代码中已有注释说明用途（如手势间隔、与框架/设备回调的配合等），则无需提示改由 Pipeline 驱动。

</SuggestedChecks>

<Reference>

- 项目整体规范与审查重点：根目录 **AGENTS.md**。
- 注册与子包结构：参考 `agent/go-service/` 下各子包的 `register.go` 及实现文件。

</Reference>
