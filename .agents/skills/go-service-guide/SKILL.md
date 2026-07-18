---
name: go-service-guide
description: MaaEnd go-service 编写指南。为 agent/go-service/ 下的 Go 代码提供架构、注册、命名、日志、注释等编码规范和模式参考。在编写、修改或审查 Go 自定义识别器、动作、EventSink，或需要了解 go-service 项目结构与 MaaFramework Go 集成方式时使用。
---

# MaaEnd Go Service 编写指南

## 架构定位

Go Service 仅处理 Pipeline 无法覆盖的复杂逻辑（图像算法、状态机、外部数据等）。禁止在 Go 中编写大规模业务流程——流程控制由 Pipeline JSON 负责。

所有坐标与图像以 **720p (1280×720)** 为基准。

## 目录结构

```
agent/go-service/
├── main.go                     # 入口：初始化、registerAll、启动 AgentServer
├── register.go                 # registerAll() 聚合各子包 Register()
├── logger.go                   # zerolog 初始化
├── pkg/                        # 公共工具包（pienv、resource、minicv、i18n、control）
├── common/                     # 通用 Custom 组件（subtask、clearhitcount 等）
├── taskersink/                 # TaskerEventSink / ContextEventSink 实现
└── <business>/                 # 业务子包（resell、essencefilter、autofight 等）
    ├── register.go             # Register() —— 本包所有组件注册
    └── *.go                    # 按职责拆分的实现文件
```

## 注册机制

### 子包 Register()

每个子包必须有 `register.go`，只暴露一个 `Register()` 函数，在其中完成本包所有组件注册。

```go
package mypkg

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
    maa.AgentServerRegisterCustomAction("MyAction", &MyAction{})
    maa.AgentServerRegisterCustomRecognition("MyRecognition", &MyRecognition{})
}
```

注册名称和参数必须与 Pipeline JSON 中 `custom_action` / `custom_recognition` 的 `name`、`param` 一致。

### main 聚合

子包的 `Register()` 必须在 `register.go` 的 `registerAll()` 中调用：

```go
func registerAll() {
    mypkg.Register()
    // ...
}
```

遗漏调用 = 组件不生效。

## 编译期接口校验

所有注册类型必须在**定义该类型的文件**中包含编译期校验，不要集中放在 `register.go`：

```go
var _ maa.CustomActionRunner      = &MyAction{}
var _ maa.CustomRecognitionRunner = &MyRecognition{}
var _ maa.TaskerEventSink         = &MySink{}
var _ maa.ContextEventSink        = &MySink{}
```

## 文件管理

- 一个 Custom 组件的实现尽量集中在单个文件。
- 同包内可按职责拆分多个 `.go`（`register.go` + 功能文件），保持单文件行数可控。
- 参数结构体（`xxxParam`）放在实现文件中，紧跟类型定义。

## 命名

- **包名**：简短、小写、单词优先（[Go 包命名惯例](https://go.dev/blog/package-names)）；包名已表达语义时不加冗余前缀。
- **类型/变量**：清晰驼峰；导出名能表意，未导出名保持简短。

## 日志（zerolog）

统一 zerolog，禁止 `log.Printf` / `log.Println`。

```go
log.Info().
    Str("component", "MyComponent").
    Str("step", "Step1").
    Msg("short description")

log.Error().
    Err(err).
    Str("component", "MyComponent").
    Msg("what failed")
```

- 上下文（组件名、步骤、场景）用链式字段，禁止拼进 `Msg`。
- 错误、参数、识别结果一律用链式字段（`.Err(err)`、`.Int("x", x)`）。

## 注释

- **导出符号**：必须添加注释，以符号名开头（便于 `go doc`），说明用途、参数、返回值。
- **未导出但复杂的逻辑**：初始化、多分支错误处理、算法步骤等应有简要注释。
- 判断标准：读者能否在不读实现的情况下理解何时/为何被调用。

## CustomAction 模板

```go
package mypkg

import (
    "encoding/json"

    maa "github.com/MaaXYZ/maa-framework-go/v4"
    "github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &MyAction{}

type myActionParam struct {
    Target string `json:"target"`
}

// MyAction does X when Pipeline calls custom_action "MyAction".
type MyAction struct{}

func (a *MyAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
    var params myActionParam
    if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
        log.Error().
            Err(err).
            Str("component", "MyAction").
            Msg("failed to parse params")
        return false
    }

    // ... 业务逻辑 ...

    return true
}
```

## CustomRecognition 模板

```go
package mypkg

import (
    "encoding/json"

    maa "github.com/MaaXYZ/maa-framework-go/v4"
    "github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &MyRecognition{}

type myRecognitionParam struct {
    Threshold float64 `json:"threshold"`
}

// MyRecognition performs X recognition.
type MyRecognition struct{}

func (r *MyRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
    var params myRecognitionParam
    if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
        log.Error().
            Err(err).
            Str("component", "MyRecognition").
            Msg("failed to parse params")
        return nil, false
    }

    // ... 识别逻辑，使用 arg.Img ...

    matched := true // 判断是否命中
    if !matched {
        return nil, false
    }

    return &maa.CustomRecognitionResult{
        Box:    arg.Roi,
        Detail: "...",
    }, true
}
```

## EventSink 模板

```go
package mypkg

import maa "github.com/MaaXYZ/maa-framework-go/v4"

var _ maa.TaskerEventSink = &MySink{}

// MySink does X on task lifecycle events.
type MySink struct{}

func (s *MySink) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
    if event != maa.EventStatusStarting {
        return
    }
    // ...
}
```

如需同时监听 Context 事件，实现 `maa.ContextEventSink` 并通过 `maa.AgentServerAddContextSink` 注册。未使用的回调方法写空实现。

## 错误处理

- 错误合理返回或记录，便于上层分支处理。
- 避免静默吞掉错误。

## 审查清单

- [ ] 注册名与 Pipeline `name` / `param` 一致
- [ ] `Register()` 已在 `registerAll()` 中调用
- [ ] 编译期接口校验在类型定义文件中
- [ ] zerolog 链式写法，无 `log.Printf`，上下文不拼进 Msg
- [ ] 导出符号有注释
- [ ] 无大规模流程代码——流程由 Pipeline 驱动
- [ ] 坐标/图像基于 720p
- [ ] 无多余 `time.Sleep`（有明确用途注释的除外）
- [ ] 重复逻辑考虑抽取为共用函数或子包

## 参考

- 项目整体规范：根目录 `AGENTS.md`
- 注册示例：`agent/go-service/register.go` + 各子包 `register.go`
- Custom 节点文档：`docs/zh_cn/developers/custom.md`
- Pipeline 协议：[MaaFramework PipelineProtocol](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.1-PipelineProtocol.md)
- Go binding：`vendor/github.com/MaaXYZ/maa-framework-go/v4/`
