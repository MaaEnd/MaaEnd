# Notify（外部通知系统）

Notify 是 MaaEnd 的多渠道外部通知模块

任务失败时，统一向设置页启用的渠道（Webhook / Bark / ServerChan / Telegram / Discord / 企业微信 / ntfy / Gotify / 钉钉 / 可新增其他渠道）推送通知

流程编排由 Pipeline 负责，Go 只负责开关判定、内容解析与渠道发送

实现位置：`agent/go-service/common/notify/`

## 模板变量

模板变量（`{{time}}` / `{{date}}` / `{{datetime}}` / `{{task_name}}` / `{{task_status}}` / `{{duration}}` / `{{controller}}` / `{{resource}}` / `{{title}}` / `{{body}}`）可在标题、正文、Webhook 请求体中使用，最终用户可见的完整说明见 [通知模板变量](../users/notify-variables.md)，此处不再重复维护

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

全部完成后 `Send()` 自动遍历注册表调用新渠道，失败通知 **自动生效，无需改动调度与触发代码**

## 富文本（Markdown）支持

Go 层**不做任何 markdown 解析/转义/渲染**，正文一律按纯文本发送（仅按各渠道官方长度上限截断）。需要富文本的渠道走其原生声明机制：

| 渠道 | 富文本方式 |
| --- | --- |
| 企微 / 钉钉 | `msgtype=markdown`（服务端原生渲染，代码仅透传字段） |
| Discord | `content` 客户端原生渲染 markdown 子集 |
| ServerChan | `desp` 服务端原生渲染 markdown |
| 其余渠道 | 纯文本 |

## 实现文件

| 文件 | 职责 |
| --- | --- |
| `notify.go` | 调度层：`ParseConfig`、`Send`（代理解析 + 遍历注册表分发） |
| `config.go` | 运行配置：`GlobalConfig`（失败通知开关与模板 + 全局代理）、`RuntimeConfig`、`decodeAttach` |
| `channel.go` | `Channel` / `ChannelFactory` 接口 + `SendContext` + 注册表 |
| `helper_vars.go` | 模板变量模块：`BuildVars` / `ReplaceVars` / `channelTitleBody` |
| `helper_length.go` | 内容长度截断辅助（`truncateRunes` / `truncateBytes`） |
| `helper_http.go` | 超时 client、`postJSON`、`readResponseBody`、错误脱敏 |
| `helper_proxy.go` | 全局代理模块：`resolveProxy`、`proxyClient`（http/https/socks5）、MXU 更新代理读取 |
| `channel_webhook.go` / `channel_bark.go` / `channel_serverchan.go` / `channel_telegram.go` / `channel_discord.go` / `channel_wecom.go` / `channel_ntfy.go` / `channel_gotify.go` / `channel_dingtalk.go` | 各渠道实现 |
| `sink.go` | 失败事件监听、配置按 taskID 缓存与去重 |
| `taskname.go` | `{{task_name}}` 显示名解析（扫描任务定义建立 entry→label 映射） || `register.go` | 动作与事件监听注册 |
