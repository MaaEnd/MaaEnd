# Notify

多渠道通知 Go Service：任务失败、发送通知任务、以及任意任务主动发起的自定义通知，统一向设置页启用的渠道（Webhook / Bark / ServerChan）推送。开关判定、内容解析与渠道发送由本包负责，触发时机与流程由 Pipeline 控制。

完整接入说明见 `docs/zh_cn/developers/components/notify.md`

## 文件与职责

| 文件 | 职责 |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `notify.go` | 核心：`Config`（attach 顶层键配置，一次解析全包消费）、`ParseConfig`、模板变量（`BuildVars` / `ReplaceVars`）、`Send` 调度、`NotifySendAction` 自定义动作、`mergeConfig`（节点内容优先合并）、`resolveNotifyText`（`$` 开头视为 i18n key，查不到回退去掉 `$` 的 key）、`taskNotifySkipped`（通知项开关判断） |
| `channel.go` | `Channel` 接口（`Name` / `Enabled` / `Send`）+ 注册表（`RegisterChannel`，渠道文件 `init` 注册一行）+ 公共辅助（`addIfPresent` / `firstNonEmpty` / `channelTitleBody`） |
| `http.go` | 公共 HTTP：`httpClient`（10s 超时）、`postJSON`（响应体 1MB 上限、业务 code 校验）、`checkStatus`、`sanitizeError`（错误中 URL 整体脱敏，防凭据泄漏） |
| `webhook.go` | Webhook 渠道：自定义方法/请求头/请求体，全模板变量；`ParseHeaders`（JSON 优先、文本回退） |
| `bark.go` | Bark 渠道：官方全部参数（非空才携带、均做变量替换）；`bark_devicekeys` 逗号分隔时走 `/push` 批量推送；`barkEndpoint` / `barkBatchEndpoint`（包级变量，测试注入） |
| `serverchan.go` | ServerChan 渠道：SC3（`sctp` 前缀按官方正则 `/^sctp(\d+)t/` 提取 uid，畸形 sendkey 拒绝构造端点）/ Turbo 双端点自动分流；`pipeSeparated`（逗号输入转 `\|`） |
| `sink.go` | 事件监听：`ConfigSink`（节点事件缓存配置，按 taskID 隔离）、`Sink`（任务失败事件发通知，按 taskID 去重、失败后清理缓存）、`controllerStartTime`（`{{duration}}` 起点）、`splitList` |
| `taskname.go` | `{{task_name}}` 显示名解析：扫描 `tasks/*.json` 建立 `entry → i18n label` 映射（`sync.Once` 缓存），`resolveTaskName` 解析失败回退入口名 |
| `register.go` | `Register()`：注册 `NotifySendAction` 动作 + `Sink` / `ConfigSink` 事件监听，供上层 `go-service` 统一加载 |

## 测试

| 文件 | 覆盖 |
| -------- | ---------------------------------------------------------------------------------------------- |
| `notify_test.go` | 配置解析、模板变量、渠道发送（httptest + 端点注入）、业务码校验、错误脱敏、失败事件链路、去重与并发 |
| `channel_test.go` | 注册表完整性、重复注册忽略 |
| `send_test.go` | `Send` 不污染调用方 vars |
| `taskname_test.go` | 任务定义扫描、显示名解析端到端与回退 |

新增渠道时必须补对应发送测试（httptest + 端点注入），并把新渠道名加入 `TestChannelRegistry` 期望列表

> [!note]
> 修改本包代码后，务必同步更新 `docs/zh_cn/developers/components/notify.md` 中与本文档中的对应内容（模板变量、attach 键、接入步骤、渠道说明等）

## 关键机制

1. **统一调度**：`Send()` 遍历注册表，向所有启用渠道推送；失败通知 / 发送通知任务 / 第三方自定义通知全部汇入同一入口，无任务级选渠道
2. **attach 顶层键合并**：所有配置经 `__NotifyConfig.attach` 顶层键注入，多个设置项各写各的键互不覆盖（通知项开关用 `task_notify.<id>` 前缀平铺键）
3. **任务名显示名**：`{{task_name}}` 经 `resolveTaskName` 输出 i18n 显示名（如 🔑自动切换账号），从任务入口名反查任务定义；解析失败回退入口名
4. **事件与动作分离**：失败通知走 `Sink`（事件驱动，无 Context 时用缓存配置），自定义通知走 `NotifySendAction`（动作内读 `__NotifyConfig` + 当前节点 attach 合并）
