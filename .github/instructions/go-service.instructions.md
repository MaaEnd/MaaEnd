---
applyTo: "agent/go-service/**"
---

# Go Service 代码审查指引（GitHub Copilot）

本说明仅在对 `agent/go-service/` 下文件的请求中生效（含 Copilot 代码评审与编码智能体）。其他目录请参阅项目根目录的 `AGENTS.md`。

## 必须检查项

### 1. 职责边界

- **Go 仅作“工具”**：Go Service 只应实现 Pipeline 难以表达的复杂图像算法或特殊交互逻辑。
- **禁止在 Go 中写业务流程**：多步骤流程、分支判断、重试策略等应由 Pipeline JSON 编排。若出现“先识别 A → 再点击 B → 再识别 C”式的流程代码，应建议拆到 Pipeline，Go 只提供单步能力（如一次识别、一次点击）。
- 超时、重试、多步骤分支由 Pipeline 的 `next`、`timeout`、重试节点控制，不在 Go 里写死。

### 2. 注册与可被 Pipeline 调用

- **子包内**：新增的 CustomRecognizer、CustomAction、EventSink 必须在对应子包内注册。子包应单独使用 `register.go` 定义并维护 `Register()`，在该函数中完成本包所有组件的注册。
- **main 聚合**：各子包的 `Register()` 必须在 main 包的 `registerAll()` 中被调用，否则组件不会生效。
- **与 Pipeline/配置一致**：注册名称、参数需与 Pipeline 中 CustomRecognizer / CustomAction 的 `name`、`params` 一致。

### 3. 单文件管理与可读性

- **Maa 的 custom 组件应按单文件边界管理**：每个自定义识别器或动作的实现尽量集中在单独文件中，避免单文件行数爆炸（例如单文件超过数百行）。
- 同一子包内可按职责拆分为多个 `.go` 文件（如 `register.go`、按功能命名的实现文件），保持单文件职责清晰、行数可控，便于阅读与维护。

### 4. 命名与 Go 哲学

- **包名**：简短、小写、单词优先，符合 [Go 包命名惯例](https://go.dev/blog/package-names)；避免冗余前缀（如包名已为 `resell` 时不再使用 `resellXXX` 子包名除非确有层级必要）。
- **变量与类型名**：使用清晰、简洁的驼峰命名；避免冗余的“命名空间式”长前缀（如 `ResellServiceHandler` 在包 `resell` 下可简化为 `Handler` 或按职责命名）。
- 导出符号名应能表意，未导出实现细节保持简短即可。

### 5. 注释与文档

- **导出函数**：必须添加注释，说明用途、参数含义、返回值含义及主要错误情况；注释以符号名开头（便于 `go doc`）。
- **导出类型与全局变量**：必须添加注释，说明用途与适用场景。
- 未导出的函数、类型、变量在逻辑复杂或非显而易见时也应添加简要注释，避免可读性降低。

### 6. 日志风格（zerolog 结构化）

- **禁止纯字符串日志**：不得使用 `log.Printf`、`fmt` 拼接、`log.Println` 等旧式写法，不符合 zerolog 结构化日志规范；Review 时发现此类写法应要求改为 zerolog 链式调用。
- **必须使用 zerolog 链式风格**：先选级别（如 `log.Info()`、`log.Error()`），再链式挂字段（如 `.Err(err)`、`.Str("key", "val")`），最后以 `.Msg("xxx")` 收尾。
- **正确示例**：
  ```go
  log.Info().
      Msg("xxx")

  log.Error().
      Err(err).
      Msg("xxx")
  ```
- 错误、关键参数、识别结果等应通过链式字段传递（如 `.Err(err)`、`.Int("x", x)`），不要拼进 `Msg` 字符串里。

### 7. 代码质量与基准

- 错误应合理返回或记录，便于 Pipeline 或上层根据返回值/日志做分支处理。
- 图像或坐标处理需明确以 **720p (1280×720)** 为基准。

## 建议性检查

- 重复逻辑是否可抽取为共用函数或子包。
- 是否有多余的硬编码延迟（如 `time.Sleep`）；若仅为“等界面稳定”，应优先由 Pipeline 用识别节点驱动。

## 参考

- 项目整体规范与审查重点：根目录 **AGENTS.md**。
- 注册与子包结构：参考 `agent/go-service/` 下各子包的 `register.go` 及实现文件。
