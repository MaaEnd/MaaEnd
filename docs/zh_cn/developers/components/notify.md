# Notify（外部通知系统）

Notify 是 MaaEnd 的多渠道外部通知模块

任务失败、以及任意任务主动发起的自定义通知，统一向设置页启用的渠道（Webhook / Bark / ServerChan / Telegram / 可新增其他渠道）推送

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
            "task_title": "$notify.monthly_card.expired", // 以 $ 开头 = i18n key（查不到翻译显示去掉 $ 的 key）
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
| `task_title` / `task_body` | 标题/正文模板，支持模板变量（见下）；**以 `$` 开头的值视为 i18n key**（与 MXU 前端约定一致，查 `assets/locales/go-service/*.json` 与 `assets/locales/interface/*.json` 合并后的翻译表），查到翻译则用翻译，查不到则显示去掉 `$` 的 key 本身 |
| `task_notify_key` | 通知项 ID，**可选，推荐**：不写则不受通知项开关影响（默认启用）；写了需另外配置设置页独立开关 |

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

新增一个渠道（如 XYZ）共六步：

**1、新文件实现 `Channel` 接口 + `init` 注册一行**（仿照 `webhook.go` / `bark.go` / `serverchan.go`）：

```go
type xyzChannel struct{}

func init() { RegisterChannel(xyzChannel{}) }

func (xyzChannel) Name() string { return "xyz" }

func (xyzChannel) Enabled(config Config) bool { return config.XyzEnabled }

func (xyzChannel) Send(config Config, vars map[string]string) error {
    // 构造请求并发送，失败返回 error（调度层统一记日志）
    ...
}
```

**2、`Config` 加开关与参数字段**（`ParseConfig` 自动解析）：

```go
XyzEnabled bool   `json:"xyz_enabled"`
XyzKey     string `json:"xyz_key"`
XyzTitle   string `json:"xyz_title"` // 及渠道所需的其他参数
```

**3、设置页加开关**（`setting/Notify.json`，仿照 `NotifyWebhook`）+ 5 语言 i18n：

```json
"NotifyXyz": {
    "type": "switch",
    "label": "$option.NotifyXyz.label",
    "description": "$option.NotifyXyz.description",
    "default_case": "No",
    "cases": [
        {
            "name": "Yes",
            "option": ["NotifyXyzParams"],
            "pipeline_override": { "__NotifyConfig": { "attach": { "xyz_enabled": true } } }
        },
        {
            "name": "No",
            "pipeline_override": { "__NotifyConfig": { "attach": { "xyz_enabled": false } } }
        }
    ]
}
```

**4、设置页加参数配置项**（可仿照 `NotifyBarkParams` 的写法，`name` 需与步骤 2 字段对应）：

```json
"NotifyXyzParams": {
    "type": "input",
    "label": "",
    "inputs": [
        {
            "name": "Key",
            "label": "key",
            "pipeline_type": "string",
            "default": "",
            "description": "$option.NotifyXyzParams.Key.description"
        },
        {
            "name": "Title",
            "label": "title",
            "pipeline_type": "string",
            "default": ""
        }
    ],
    "pipeline_override": {
        "__NotifyConfig": {
            "attach": {
                "xyz_key": "{Key}",
                "xyz_title": "{Title}"
            }
        }
    }
}
```

**5、补全i18n**（可选但推荐，5 语言 `assets/locales/interface/*.json`）：

```json
"option.NotifyXyz.label": "XYZ 渠道",
"option.NotifyXyz.description": "XYZ 渠道说明",
"option.NotifyXyzParams.Key.description": "key 说明"
```

> [!note]
> input 参数项可以补 `description` 介绍用途（推荐），走 i18n

**6、补单元测试**（仿照 `notify_test.go` 现有用例）：

渠道发送用例用 httptest 本地服务器 + 端点注入（与 `TestSendBarkJSON` / `TestSendServerChanJSON` 同款），端点构造函数需做成包级变量（如 `barkEndpoint`）以便注入：

```go
func TestSendXyz(t *testing.T) {
    var got map[string]any
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        _ = json.NewDecoder(r.Body).Decode(&got)
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte(`{"code": 0}`))
    }))
    defer server.Close()

    orig := xyzEndpoint
    xyzEndpoint = func(string) (string, error) { return server.URL, nil }
    defer func() { xyzEndpoint = orig }()

    if !Send(Config{XyzEnabled: true, XyzKey: "key"}, map[string]string{}) {
        t.Fatalf("Send returned false")
    }
    if got["title"] != "..." {
        t.Errorf("payload mismatch: %v", got)
    }
}
```

同时把新渠道名加进注册表测试 `TestChannelRegistry` 的期望列表：

```go
wantNames := map[string]bool{"webhook": true, "bark": true, "serverchan": true, "xyz": true}
```

全部完成后 `Send()` 自动遍历注册表调用新渠道，失败通知 / 第三方自定义通知 **全部自动生效，无需改动调度与触发代码**

## 调用逻辑（Go 自动判定）

```
自定义任务调 NotifySendAction
        ↓ GO 自动判断
1  读 __NotifyConfig 所有开关状态
2  读当前节点 attach
3  总开关判断：allow_task_notify 为 false 则跳过
4  通知项判断：task_notify.<task_notify_key> 为 false 则跳过
5  内容解析：以 $ 开头的值查 i18n 翻译，查到用翻译、查不到显示去掉 $ 的 key；普通文本原样（再做模板变量替换）
6  Send() 向 __NotifyConfig 读到的已启用渠道推送
```

通知发送失败不影响游戏流程（动作始终返回成功）

## 实现文件

| 文件 | 职责 |
| --- | --- |
| `notify.go` | `Config` 解析、模板变量、`Send` 调度、`NotifySendAction` |
| `channel.go` | `Channel` 接口 + 注册表（新增渠道 = 新文件实现接口 + `init` 注册一行） |
| `http.go` | 超时 client、`postJSON`、错误脱敏 |
| `webhook.go` / `bark.go` / `serverchan.go` / `telegram.go` | 各渠道实现 |
| `telegram_proxy.go` | Telegram 代理：手动地址 / 复用 MXU 更新设置代理（读 `install/config/mxu-{项目名}.json`）、`proxyClient`（http/https） |
| `sink.go` | 失败事件监听、配置按 taskID 缓存与去重 |
| `register.go` | 动作与事件监听注册 |
