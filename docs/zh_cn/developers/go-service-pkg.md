# Go Service 公共包（`pkg/`）参考

`agent/go-service/pkg/` 放的是**跨业务复用**的工具包：不注册 Custom 节点，不承载完整业务流程，只提供「识别结果解包、表达式求值、OCR 数值、环境变量、i18n、资源读取」等通用能力。

业务包（如 `autostockpile/`、`ims/`）和公共 Custom（如 `common/expressionrecognition/`）应优先复用这里的实现，**不要在业务侧再抄一份**。

> [!TIP]
> 写 Custom 组件本身请先看 [Custom 动作与识别](./custom.md) 与 [Go Service 编码规范](./coding-standards.md#go-service-规范)。本文只讲 `pkg/` 里「被谁用、怎么用、不要重复造」。

## 包一览

按日常复用频率大致排序：

| 包 | 一句话 | 典型调用方 |
| ------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| [`i18n`](#i18n-多语言文案) | `T` / `RenderHTML`，按客户端语言取文案 | 几乎所有要给用户看提示的 Action |
| [`maafocus`](#maafocus-向客户端推-focus) | 把提示推到客户端 Focus UI | `FocusOCRAction`、战斗 / 据点交易等 |
| [`recogtarget`](#recogtarget-and-识别解包) | 从 Pipeline 节点（含 `And`）选出真正要用的子识别结果 | `ExpressionRecognition`、`FocusOCRAction` |
| [`boolexpr`](#boolexpr-条件计算) | `{占位符}` 替换 + 整型布尔表达式求值 | `ExpressionRecognition`、IMS R1 |
| [`ocrnum`](#ocrnum-ocr-数值解析) | OCR 文本 → `int`（支持 `1.38万` / `13.8K` 等） | `ExpressionRecognition`、`iconqty` |
| [`jsonclean`](#jsonclean-jsonc-清洗) | 去掉注释 / 尾逗号 / BOM，变成严格 JSON | `resource`、`i18n`、各类配置加载 |
| [`pienv`](#pienv-project-interface-环境) | 读取 `PI_*` 环境变量（语言、控制器、资源） | `i18n`、`iconqty`、启动期逻辑 |
| [`resource`](#resource-资源文件定位与读取) | 按相对路径找 `resource/` / `assets/` 下文件 | 商品目录、据点数据等 JSON 配置 |
| [`fsutil`](#fsutil-原子写文件) | 临时文件 + rename，避免半截写入 | debug / 持久化缓存 |
| [`levenshtein`](#levenshtein-编辑距离) | rune 级编辑距离 | OCR / 名称模糊匹配 |
| [`iconrecognition`](#iconrecognition--iconqty) | IconRecognition 参数与 detail 的公共结构 | IMS、囤货、送货等扫格逻辑 |
| [`iconqty`](#iconrecognition--iconqty) | 扫格 + 格内数量 OCR | IMS A2 / A3 |
| [`control`](#control-跨平台操控适配) | Win32 / ADB / macOS / Linux 统一操控接口 | 角色移动、镜头旋转等 |
| [`minicv`](#minicv-轻量-cv) | 模板匹配、圆检测等纯 Go CV | 需要本地匹配且不想走 Pipeline 模板节点时 |
| [`parentwatch`](#parentwatch-父进程监视) | 父进程退出则结束自身，避免孤儿 Agent | 仅 `main` 启动期 |

---

## `i18n`：多语言文案

路径：`agent/go-service/pkg/i18n/`

- 文案目录：`assets/locales/go-service/<lang>.json`（并会合并同级 `interface` locale）
- `i18n.T("key", args...)`：短文案
- `i18n.RenderHTML("registered.key", data)`：HTML 模板（须在包内 `htmlTemplates` 注册）

给用户看的提示优先走 i18n，再交给 `maafocus`，不要在 Go 里写死中文。

---

## `maafocus`：向客户端推 Focus

路径：`agent/go-service/pkg/maafocus/`

| 函数 | 场景 |
| --------------------------- | ------------------------------------------------ |
| `Print` | 普通 Focus 提示 |
| `PrintThrottle` | 每心跳都会跑的识别 / Action，避免同文案刷屏 |
| `PrintLargeContent` | 大段内容走 stdout，减轻 Maa 日志体积 |
| `PrintLargeContentTrimNewline` | 大段 HTML，压成单行避免客户端解析问题 |

---

## `recogtarget`：And 识别解包

路径：`agent/go-service/pkg/recogtarget/`

Pipeline 里常见两种「数字 / 文本来源」：

1. 纯 `OCR` 节点 —— `RunRecognition` 的 detail 本身就是 OCR 结果。
2. `And` 节点 —— detail 是 `CombinedResult`，真正要用的是 `box_index` 指向的那一项（往往是 OCR）。

业务代码若自己去翻 `CombinedResult`，很容易和扁平写法 / v2 写法、嵌套 And 名引用对不上。**凡是「跑某个识别节点，再取 OCR 文本或框」的逻辑，都应走本包。**

### 它解决什么

- 兼容扁平与 v2 节点 JSON：
  - 扁平：`"recognition":"And","all_of":[...],"box_index":n`
  - v2：`"recognition":{"type":"And","param":{"all_of":[...],"box_index":n}}`
- 按节点原生 `box_index` 从 `CombinedResult` 取子结果。
- 子项若仍是 And 节点名引用，会沿 `box_index` 链继续下钻（环路则报错）。

### 常用 API

| 函数 | 用途 |
| ---------------------- | ------------------------------------------------------------ |
| `ParseNodeJSON` | 解析节点 JSON → `Fields{Type, AllOf, BoxIndex}` |
| `ResolveAndBoxIndex` | 判断是否为 And，并返回合法 `box_index` |
| `SelectDetail` | **首选**：给定 `ctx` + 节点名 + detail，选出目标子结果（支持嵌套） |
| `SelectDetailFromJSON` | 已有节点 JSON 时选一层（嵌套 And 请用 `SelectDetail`） |
| `SelectedDetail` | 已知索引时直接取 `CombinedResult[i]` |
| `EffectiveType` | 沿 And 链解析「有效识别类型」（如最终是 `OCR`） |

### 推荐写法

```go
import (
    "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
    "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
)

detail, err := ctx.RunRecognition(nodeName, arg.Img)
if err != nil {
    return 0, err
}

selected, err := recogtarget.SelectDetail(ctx, nodeName, detail)
if err != nil {
    return 0, err
}

// 数字：交给 ocrnum；文本 / Box：直接读 selected
return ocrnum.Extract(selected)
```

参考实现：`common/expressionrecognition`（取数值）、`common/focusocr`（取 OCR 文本再 `maafocus.Print`）。

### 不要做的事

- 不要手写 `detail.CombinedResult[0]` 然后假设永远是 OCR。
- 不要为「OCR 或 And→OCR」再新造一套解析；扩展请改 `recogtarget`。
- 不要用业务自定义的「假 box_index」覆盖节点原生字段；解包语义与 Pipeline 节点定义保持一致。

---

## `boolexpr`：条件计算

路径：`agent/go-service/pkg/boolexpr/`

把「带占位符的表达式字符串」变成可求值的整型 / 布尔表达式。Pipeline 侧的 [`ExpressionRecognition`](./custom.md#expressionrecognition) 与 IMS R1 `ItemQuantitySatisfied` 都建立在它上面：前者占位符是识别节点名，后者是物品 ID。

### 两步流程

1. **`ResolvePlaceholders(expr, resolve)`**  
   用正则匹配 `{name}`，调用 `resolve(name) (int, error)` 得到整数，替换进表达式，并返回 `map[string]int` 便于打日志。
2. **`Evaluate(resolved)`**  
   用 Go 标准库 `go/parser` 解析 AST，在整型 / 布尔上求值。识别是否命中时，调用方必须断言结果为 `bool`。

### 支持的运算

| 类别 | 运算符 |
| ---- | -------------------------- |
| 算术 | `+` `-` `*` `/` `%` |
| 比较 | `<` `<=` `>` `>=` `==` `!=` |
| 逻辑 | `&&` `||` `!` |
| 分组 | `(...)` |

字面量超出平台 `int` 范围时会钳制到 `IntMax` / `IntMin` 并打 warn，求值继续而不是直接失败（与 `ocrnum` 溢出策略一致）。

### 推荐写法

```go
import "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/boolexpr"

resolved, values, err := boolexpr.ResolvePlaceholders(
    `{CurrentCredit}-{RefreshCost}<400`,
    func(name string) (int, error) {
        // 节点名 → OCR 整数；或物品 ID → 缓存数量
        return lookup(name)
    },
)
if err != nil {
    return false, err
}

result, err := boolexpr.Evaluate(resolved)
if err != nil {
    return false, err
}
matched, ok := result.(bool)
if !ok {
    return false, fmt.Errorf("expression must evaluate to bool")
}
_ = values // 打日志用
return matched, nil
```

### 和 Pipeline 的分工

| 需求 | 用什么 |
| -------------------------------- | ------------------------------------------------------------ |
| 多个 OCR 数值比大小 / 加减后判断 | Pipeline 节点 + `ExpressionRecognition`（内部已用本包） |
| IMS 缓存数量是否满足 | IMS R1 `ItemQuantitySatisfied`（占位符是物品 ID，不是节点名） |
| Go 里自己算一段同类表达式 | 直接调 `boolexpr`，不要再嵌一套表达式解析器 |

---

## `ocrnum`：OCR 数值解析

路径：`agent/go-service/pkg/ocrnum/`

把 OCR 文本里的数量读成 `int`：

- 英文量级：`k` / `m` / `b`（大小写不敏感）
- 中文 / 韩文：`万` `萬` `만`、`亿` `億` `억`
- 小数与千分位逗号的常见写法

| 函数 | 用途 |
| --------- | -------------------------------- |
| `Parse` | 纯文本 → `int` |
| `Extract` | 从 `RecognitionDetail` 取 OCR 再 `Parse` |

典型链路：`RunRecognition` → `recogtarget.SelectDetail` → `ocrnum.Extract`。

---

## 其它高复用包（简表 + 用法要点）

### `jsonclean`：JSONC 清洗

路径：`pkg/jsonclean/`

仓库里大量配置是 **JSONC**（`//`、`/* */`、尾逗号、可选 BOM）。`encoding/json` 吃不下时：

```go
json.Unmarshal(jsonclean.Clean(raw), &out)
```

行号尽量保留（注释行用换行占位），方便报错定位。几乎所有「读项目 JSON 配置」都应先过一遍。

### `pienv`：Project Interface 环境

路径：`pkg/pienv/`

启动时 Client 会注入 `PI_*`（见 Project Interface v2.5）。`Init()` / `Get()` 解析为单例：

- `ClientLanguage()` → 交给 `i18n`
- `ControllerType()` / `ControllerName()` → 区分 Win32 / Adb 等
- `ResourceName()`、版本号等

不要自己再 `os.Getenv("PI_...")` 散落解析。

### `resource`：资源文件定位与读取

路径：`pkg/resource/`

- `FindResource` / `ReadResource` / `ReadJsonResource`（内部已 `jsonclean`）
- 查找顺序：绝对 / 相对路径 → resource sink 基路径 → cwd 及上级的 `resource/`、`assets/`

读商品表、据点数据等时用本包，不要写死 `install/...` 绝对路径。

### `fsutil`：原子写文件

路径：`pkg/fsutil/`

`WriteFileAtomic(path, content, perm)`：同目录临时文件写完再 rename。适合 debug 落盘、可覆盖的缓存；**不保证掉电持久性**（不做目录 fsync）。

### `levenshtein`：编辑距离

路径：`pkg/levenshtein/`

`Distance(a, b int)`，按 **rune** 比较，适合中日韩 OCR 噪声下的名称近似匹配。阈值由业务自行决定。

### `iconrecognition` / `iconqty`

| 包 | 职责 |
| ----------------- | ------------------------------------------------------------ |
| `iconrecognition` | `Params` / `Detail` / `Match` / `GridType` 等公共结构，**禁止业务包再声明一份** |
| `iconqty` | 在格子上跑 IconRecognition，再按 `cell_box` OCR 数量（IMS 默认 ROI / 过滤器） |

详细协议与 Pipeline 用法见 [IconRecognition](./components/icon-recognition.md)、[IMS](./components/ims.md)。

### `control`：跨平台操控适配

路径：`pkg/control/`

`ControlAdaptor` 统一 Touch / Key / 镜头旋转 / 角色移动等；按平台有 Win32、ADB、macOS、Linux 实现。角色控制、自动战斗等「真正动手」的逻辑应走适配器，而不是直接调底层 Contorller API 散落各处。

### `minicv`：轻量 CV

路径：`pkg/minicv/`

纯 Go 模板匹配（含 SIMD 加速路径）、积分图、圆检测等。适合 Agent 内本地匹配；能用 Pipeline `TemplateMatch` 表达的优先仍写 Pipeline。

### `parentwatch`：父进程监视

路径：`pkg/parentwatch/`

仅在 `main` 启动时 `Start()` 一次：父进程退出则 `os.Exit(0)`，防止 MaaAgentServer 变孤儿。业务包不要依赖它。

---

## 选型速查

| 我想…… | 用 |
| -------------------------------------------- | -------------------------------- |
| 按当前语言提示用户 | `i18n` + `maafocus` |
| 跑识别节点后取 And 里真正的 OCR / Box | `recogtarget` |
| OCR 文本变整数 | `ocrnum` |
| `{A}+{B}<100` 这类条件 | Pipeline → `ExpressionRecognition`；Go 内 → `boolexpr` |
| 读带注释的 JSON 配置 | `jsonclean`（或经 `resource.ReadJsonResource`） |
| 区分 Adb / Win32 | `pienv.ControllerType()` |
| 扫背包格子并读数量 | `iconrecognition` + `iconqty` |
| 写文件不留半截内容 | `fsutil.WriteFileAtomic` |

## 相关文档

- [Custom 动作与识别](./custom.md)（`ExpressionRecognition`、`FocusOCRAction` 等）
- [编码规范 · Go Service](./coding-standards.md#go-service-规范)
- [组件指南](./components-guide.md)
- [IconRecognition](./components/icon-recognition.md)
- [IMS](./components/ims.md)
