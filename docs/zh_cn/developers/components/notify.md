# Notify（外部通知系统）

Notify 是 MaaEnd 的多渠道外部通知模块

任务失败、以及任意任务主动发起的自定义通知，统一向设置页启用的渠道（Webhook / Bark / ServerChan / Telegram / Discord / 可新增其他渠道）推送

流程编排由 Pipeline 负责，Go 只负责开关判定、内容解析与渠道发送

实现位置：`agent/go-service/common/notify/`

## 触发方式一览

| 触发方式 | 谁触发 | 需要配置 |
| --- | --- | --- |
| **全局任务失败通知** | 框架任务失败事件自动触发（`Sink`） | 设置页开启「任务失败通知」 |
| **自定义通知** | 任意任务的任意节点声明 `NotifySendAction` 动作 | 节点 attach 写内容；可选设置页通知项开关 |

两种最终都汇入同一个 `Send()` 调度，并向设置页启用的所有渠道推送

## 自定义通知：第三方任务接入

其他任务想主动发通知（如月卡到期、调查问卷出现），只需在节点上声明动作 + attach 内容：

```json
{
    "识别月卡到期": {
        "recognition": "OCR",
        "expected": ["月卡已过期"],
        "action": "Custom",
        "custom_action": "NotifySendAction",
        "attach": {
            "task_title": "$notify.monthly_card.expired", // 第三方 attach 支持 $ i18n：key 必须写入 assets/locales/go-service/*.json（补 5 语言），查不到翻译则显示去掉 $ 的 key
            "task_body": "请及时续费",                     // 普通文本原样发送
            "task_notify_key": "monthly_card"             // 通知项 ID（可选，用于独立开关）
        }
    }
}
```

任务只需在 **合适的时机** 执行该节点，后续开关判定、内容解析、渠道发送全部自动，无需任何 Go 代码

### attach 参数

| 字段 | 说明 |
| --- | --- |
| `task_title` / `task_body` | 标题/正文模板，支持模板变量（见下）。**第三方节点 attach 编写的值**以 `$` 开头时视为 i18n key，**必须使用 `assets/locales/go-service/*.json` 存储翻译**（不要放 interface），查到翻译则用翻译，查不到则显示去掉 `$` 的 key 本身；**玩家在设置页填写的值**为普通文本，不解析 `$` |
| `task_notify_key` | 通知项 ID，**可选，推荐**：不写则不受通知项开关影响（默认启用）；写了需另外配置设置页独立开关 |

> [!note]
> **`$` i18n 翻译存放位置（强制约定）**：`task_title` / `task_body` 中以 `$` 开头的 i18n key（如 `$notify.monthly_card.expired`），翻译**必须写入 `assets/locales/go-service/*.json`**，并补齐 5 语言。不要写入 `assets/locales/interface/*.json`——interface 目录仅用于界面文案（任务名 `$task.*.label`、设置项 `$option.*.*`）。key 查不到翻译时显示去掉 `$` 的 key 本身

### 设置页加通知项开关（可选）

想让用户能单独关闭你的通知项，按以下三步（照抄 `NotifyItemManualNotifySend` 的现有实现即可）：

**1、在 `NotifyAllowTask` 的 Yes case `option` 数组里加名字**（`assets/tasks/setting/Notify.json`）：

```json
"option": [
    "NotifyItemsHelp",
    "NotifyItemManualNotifySend",
    "NotifyItemMonthlyCard"
]
```

**2、在文件底部 `option` 对象中补完整定义**（与 `NotifyItemManualNotifySend` 平级）：

```json
"NotifyItemMonthlyCard": {
    "type": "switch",
    "label": "$option.NotifyItemMonthlyCard.label",
    "description": "$option.NotifyItemMonthlyCard.description",
    "default_case": "Yes",
    "cases": [
        {
            "name": "Yes",
            "pipeline_override": { "__NotifyConfig": { "attach": { "task_notify.monthly_card": true } } }
        },
        {
            "name": "No",
            "pipeline_override": { "__NotifyConfig": { "attach": { "task_notify.monthly_card": false } } }
        }
    ]
}
```

**3、补 i18n**：5 语言 `assets/locales/interface/*.json` 加 `option.NotifyItemMonthlyCard.{label, description}`

> [!note]
> 未配置开关的通知项默认启用，每个通知项各占一个独立的 `task_notify.<id>` 键（如 `task_notify.monthly_card`），设置页多个开关各写各的键、互不影响；不要把多个开关塞进同一个键里，不然后写的会把先写的盖掉

## 模板变量

> [!note]
> 此内容与用户 UI 内 `设置->通知渠道->通知使用说明->模板变量` 中填写的内容相同，更改模板变量 Go 文件后应同步两处

标题 / 正文 / Webhook 请求体中可用（`{{title}}` / `{{body}}` 可在渠道标题正文中引用通知项预填内容）：

| 变量 | 说明 |
| --- | --- |
| `{{time}}` | 时间 HH:mm:ss |
| `{{date}}` | 日期 yyyy-MM-dd |
| `{{datetime}}` | 日期时间 yyyy-MM-dd HH:mm:ss |
| `{{task_name}}` | 任务显示名（如 🔑自动切换账号；自动从任务入口解析；入口反查取不到时回退当前节点名再解析，仍查不到则显示原文） |
| `{{task_status}}` | 任务状态（如 失败） |
| `{{duration}}` | 执行耗时（如 1m23s） |
| `{{controller}}` | 控制器名 |
| `{{resource}}` | 资源包名 |
| `{{title}}` | 通知项标题（渠道配置中可引用，渠道配置优先级高于任务配置的内容） |
| `{{body}}` | 通知项正文（渠道配置中可引用，渠道配置优先级高于任务配置的内容） |

## 自定义渠道：新增其他通知渠道

渠道是自包含模块，新增一个渠道（如 XYZ）不动其他任何文件，照抄 `channel_discord.go` 改前缀即可

**1、新文件 `channel_xyz.go`**：一个类型同时作工厂（零值注册）与实例（cfg 字段），配置类型化、无需断言

```go
// xyzConfig 私有配置（attach 顶层 channel_xyz_* 键）
type xyzConfig struct {
    Enabled  bool   `json:"channel_xyz_enabled"`
    UseProxy bool   `json:"channel_xyz_use_proxy"` // 是否走全局代理（配合 use_proxy 主开关）
    Key      string `json:"channel_xyz_key"`
    Title    string `json:"channel_xyz_title"`
}

type xyzChannel struct {
    cfg xyzConfig
}

func init() { RegisterChannel(xyzChannel{}) }

var _ ChannelFactory = xyzChannel{}
var _ Channel = xyzChannel{}

func (xyzChannel) Name() string { return "xyz" }

func (xyzChannel) Create(attach map[string]any) (Channel, error) {
    var cfg xyzConfig
    if err := decodeAttach(attach, &cfg); err != nil {
        return nil, err
    }
    return xyzChannel{cfg: cfg}, nil
}

func (c xyzChannel) Enabled() bool  { return c.cfg.Enabled }
func (c xyzChannel) UseProxy() bool { return c.cfg.UseProxy }

func (c xyzChannel) Send(ctx *SendContext) error {
    config := c.cfg
    vars := ctx.Vars
    // 构造 payload 并发送，响应按 HTTP 状态 / 业务 code 判断，失败返回 error
    return postJSON(ctx.Client, xyzEndpoint(config.Key), payload, 0)
}
```

**2、设置页加开关 + 参数**（`setting/Notify.json`，仿照 `NotifyDiscord` / `NotifyDiscordParams`），写入 `channel_xyz_enabled` / `channel_xyz_key` 等 attach 键

**3、5 语言 i18n**（`assets/locales/interface/*.json` 加 `option.NotifyXyz.*` 键）

**4、补发送测试**（httptest + 端点注入，仿照 `channel_discord_test.go`），并把 `"xyz"` 加入 `TestChannelRegistry` 期望列表

> 渠道内只管自己的配置与请求；代理（`ctx.Client`）、模板变量（`ctx.Vars`）、标题/正文拼合（`channelTitleBody`）都由调度层与公共模块提供

全部完成后 `Send()` 自动遍历注册表调用新渠道，失败通知 / 第三方自定义通知 **全部自动生效，无需改动调度与触发代码**

## 调用逻辑（Go 自动判定）

```
自定义任务调 NotifySendAction
        ↓ GO 自动判断
1  读 __NotifyConfig 所有开关状态
2  读当前节点 attach
3  总开关判断：allow_task_notify 为 false 则跳过
4  通知项判断：task_notify.<task_notify_key> 为 false 则跳过
5  内容解析：第三方 attach 以 $ 开头的值查 i18n 翻译，查到用翻译、查不到显示去掉 $ 的 key；玩家 UI 与普通文本原样（再做模板变量替换）
6  Send() 向 __NotifyConfig 读到的已启用渠道推送
```

通知发送失败不影响游戏流程（动作始终返回成功）

## 实现文件

| 文件 | 职责 |
| --- | --- |
| `notify.go` | 调度层：`ParseConfig`、`Send`（代理解析 + 遍历注册表分发）、`NotifySendAction` |
| `config.go` | 运行配置：`GlobalConfig`（系统开关 + 标题正文模板 + 全局代理）、`RuntimeConfig`、`decodeAttach`、`MergeAttach` |
| `channel.go` | `Channel` / `ChannelFactory` 接口 + `SendContext` + 注册表 |
| `vars.go` | 模板变量模块：`BuildVars` / `ReplaceVars` / `channelTitleBody` |
| `http.go` | 超时 client、`postJSON`、错误脱敏 |
| `proxy.go` | 全局代理模块：`resolveProxy`、`proxyClient`、MXU 更新代理读取 |
| `channel_webhook.go` / `channel_bark.go` / `channel_serverchan.go` / `channel_telegram.go` / `channel_discord.go` / `channel_wecom.go` / `channel_ntfy.go` / `channel_gotify.go` | 各渠道实现 |
| `sink.go` | 失败事件监听、配置按 taskID 缓存与去重 |
| `register.go` | 动作与事件监听注册 |
