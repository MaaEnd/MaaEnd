# Notify

多渠道通知 Go Service：任务失败、发送通知任务、以及任意任务主动发起的自定义通知，统一向设置页启用的渠道（Webhook / Bark / ServerChan / Telegram / Discord / 企业微信 / ntfy / 可新增其他渠道）推送。开关判定、内容解析与渠道发送由本包负责，触发时机与流程由 Pipeline 控制。

完整接入说明见 `docs/zh_cn/developers/components/notify.md`

## 文件与职责

模块划分：调度层薄化 + 渠道自包含 + 共享能力抽成公共模块，渠道之间互不耦合。

| 文件 | 职责 |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `notify.go` | 调度层：`ParseConfig`（解析运行配置）、`Send`（统一解析代理→遍历注册表分发）、`NotifySendAction` 自定义动作、`resolveTitleBody`/`resolveNotifyText`（仅第三方 attach 的 `$` i18n 解析）、`taskNotifySkipped`（通知项开关判断） |
| `config.go` | 运行配置：`GlobalConfig`（系统开关 on_fail/allow_task_notify/task_notify.* + 标题正文模板 + 全局代理）、`RuntimeConfig`（全局 + 原始 attach 供渠道工厂 Create）、`decodeAttach`（attach → 结构体，未知键忽略）、`MergeAttach`（节点内容覆盖全局内容） |
| `channel.go` | `Channel` 接口（`Enabled` / `UseProxy` / `Send`）+ `ChannelFactory` 接口（`Name` / `Create`）+ `SendContext` + 注册表（`RegisterChannel`，渠道文件 `init` 注册零值工厂一行） |
| `vars.go` | 模板变量模块：`BuildVars` / `ReplaceVars` / `channelTitleBody` / `firstNonEmpty` / `addIfPresent` |
| `http.go` | 共享 HTTP：`httpClient`（10s 超时）、`postJSON(client,...)`（响应体 1MB 上限、业务 code 校验）、`checkStatus`、`sanitizeError`（错误中 URL 整体脱敏，防凭据泄漏） |
| `proxy.go` | 全局代理模块：`resolveProxy`（手动地址 / 复用更新设置代理二选一）、`proxyClient`（仅标准库，http/https；socks5 明确报错）、更新设置代理读取（`install/config/mxu-{项目名}.json` 的 `settings.proxy.url`）、按地址缓存 client |
| `channel_webhook.go` | 渠道模块：Webhook，自定义方法/请求头/请求体，全模板变量；`ParseHeaders`（JSON 优先、文本回退） |
| `channel_bark.go` | 渠道模块：Bark，官方全部参数（非空才携带、均做变量替换）；`channel_bark_devicekeys` 逗号分隔时走 `/push` 批量推送 |
| `channel_serverchan.go` | 渠道模块：ServerChan，SC3（`sctp` 前缀按官方正则 `/^sctp(\d+)t/` 提取 uid）/ Turbo 双端点自动分流；`pipeSeparated` |
| `channel_telegram.go` | 渠道模块：Telegram Bot，`sendMessage` 推送（标题+正文拼合，`chat_id` 逗号分隔多播）；`postTelegram` 校验 `ok` 布尔响应；API 地址留空用官方，填写第三方服务地址自动拼接 `/bot{token}/sendMessage` |
| `channel_discord.go` | 渠道模块：Discord Webhook，`content` 标题+正文拼合，可选 `username`/`avatar_url` 覆盖；响应按 HTTP 状态判断（204 即成功） |
| `channel_wecom.go` | 渠道模块：企业微信群机器人，`msgtype`（text/markdown/markdown_v2）+ `content` 标题+正文拼合；`postWeCom` 校验 `errcode`（企微失败也返回 HTTP 200，故单独校验） |
| `channel_ntfy.go` | 渠道模块：ntfy，标题用 `Title` header、正文用请求 body（标题正文分离）；可选优先级/标签/`Bearer` token；响应按 HTTP 状态判断 |
| `channel_gotify.go` | 渠道模块：Gotify，`X-Gotify-Key` 鉴权、JSON body 标题+正文拼合，可选优先级（0-10，空用应用默认）；响应按 HTTP 状态判断 |
| `sink.go` | 事件监听：`ConfigSink`（节点事件缓存配置，按 taskID 隔离）、`Sink`（任务失败事件发通知，按 taskID 去重、失败后清理缓存）、`controllerStartTime`（`{{duration}}` 起点）、`splitList` |
| `taskname.go` | `{{task_name}}` 显示名解析：扫描 `tasks/*.json` 建立 `entry → i18n label` 映射（`sync.Once` 缓存），`resolveTaskName` 解析失败回退入口名 |
| `register.go` | `Register()`：注册 `NotifySendAction` 动作 + `Sink` / `ConfigSink` 事件监听，供上层 `go-service` 统一加载 |

## 测试

| 文件 | 覆盖 |
| -------- | ---------------------------------------------------------------------------------------------- |
| `notify_test.go` | 配置解析、模板变量、渠道发送（httptest + 端点注入，含 Telegram `ok` 响应校验）、业务码校验、错误脱敏、失败事件链路、去重与并发 |
| `proxy_test.go` | 代理解析（手动 / 复用 MXU 更新代理）、MXU 配置读取、`proxyClient`（http 支持 / socks5 报错）、全局代理发送链路 |
| `channel_discord_test.go` | Discord 发送（content/username/avatar_url 拼合与省略）、错误路径（空/非法 URL、空内容、HTTP 500）、端点校验 |
| `channel_wecom_test.go` | 企业微信发送（msgtype/text.content 拼合、markdown 分支）、错误路径（空/非法 URL、空内容、errcode!=0、HTTP 500）、端点校验 |
| `channel_ntfy_test.go` | ntfy 发送（Title/Priority/Tags header + body 拼合、Bearer token）、错误路径（空/非法 URL、空内容、HTTP 401）、端点校验 |
| `channel_gotify_test.go` | Gotify 发送（X-Gotify-Key header + JSON body 的 message/title/priority 拼合与省略）、错误路径（空/非法 URL、空内容、HTTP 401）、端点校验 |
| `channel_test.go` | 注册表完整性、重复注册忽略 |
| `send_test.go` | `Send` 不污染调用方 vars |
| `taskname_test.go` | 任务定义扫描、显示名解析端到端与回退 |

新增渠道时必须补对应发送测试（httptest + 端点注入），并把新渠道名加入 `TestChannelRegistry` 期望列表

## 新增渠道（最小步骤）

渠道 = 自包含模块，新增一个渠道（如 XYZ）不动其他任何文件。照抄 `channel_discord.go` 改前缀即可：

1. **新文件 `channel_xyz.go`**：一个类型同时作工厂（零值）与实例（cfg 字段），配置类型化、无需断言：

```go
// xyzConfig 私有配置（attach 顶层 channel_xyz_* 键）
type xyzConfig struct {
    Enabled  bool   `json:"channel_xyz_enabled"`
    UseProxy bool   `json:"channel_xyz_use_proxy"` // 是否走全局代理（配合全局 use_proxy 主开关）
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

func (c xyzChannel) Enabled() bool { return c.cfg.Enabled }

func (c xyzChannel) UseProxy() bool { return c.cfg.UseProxy }

func (c xyzChannel) Send(ctx *SendContext) error {
    config := c.cfg                 // 类型安全，直接读写
    vars := ctx.Vars
    // 构造 payload 并发送；响应按 HTTP 状态 / 业务 code 判断，失败返回 error
    return postJSON(ctx.Client, xyzEndpoint(config.Key), payload, 0)
}
```

2. **设置页加开关 + 参数**（`assets/tasks/setting/Notify.json`，仿照 `NotifyDiscord` / `NotifyDiscordParams`），写入 `channel_xyz_enabled` / `channel_xyz_key` 等 attach 键
3. **5 语言 i18n**（`assets/locales/interface/*.json` 加 `option.NotifyXyz.*` 键）
4. **补发送测试**（httptest + 端点注入，仿照 `channel_discord_test.go`），并把 `"xyz"` 加入 `TestChannelRegistry`

> 渠道内只管自己的配置与请求；代理（`ctx.Client`）、模板变量（`ctx.Vars`）、标题/正文拼合（`channelTitleBody`）都由调度层与公共模块提供

> [!note]
> 修改本包代码后，务必同步更新 `docs/zh_cn/developers/components/notify.md` 中与本文档中的对应内容（模板变量、attach 键、接入步骤、渠道说明等）

## 关键机制

1. **统一调度 + 渠道解耦**：`Send()` 遍历注册表，向所有启用渠道推送；渠道 = 自包含模块（私有配置 + 工厂 Create + Enabled/Send），不依赖其他渠道、不感知代理与通知项内容解析，配置是实例的类型化字段（无 `any`、无断言）
2. **attach 顶层键合并**：所有配置经 `__NotifyConfig.attach` 顶层键注入，多个设置项各写各的键互不覆盖（通知项开关用 `task_notify.<id>` 前缀平铺键）
3. **两级代理**：代理是系统级配置（`use_proxy` 主开关 + `use_update_proxy` / `proxy_url` 地址），`Send()` 统一解析一次；每个渠道另有独立开关（`channel_<channel>_use_proxy`，经 `Channel.UseProxy()` 声明），主开关开启且渠道开关开启时才走代理，渠道零代理代码
4. **任务名显示名**：`{{task_name}}` 经 `resolveTaskName` 输出 i18n 显示名（如 🔑自动切换账号），从任务入口名反查任务定义；解析失败回退入口名
5. **事件与动作分离**：失败通知走 `Sink`（事件驱动，无 Context 时用缓存配置），自定义通知走 `NotifySendAction`（动作内读 `__NotifyConfig` + 当前节点 attach 合并）