package notify

import (
	"encoding/json"
	"maps"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const defaultConfigNode = "__NotifyConfig"

// Config 是 __NotifyConfig 节点 attach 的解析结果，由用户在设置页配置、
// 经 global_option 的 pipeline_override 注入节点 attach 后由本包读取。
// 注意：各字段均为 attach 的顶层 key——MaaFramework 的 override 对 attach
// 按顶层 key 合并，多个设置项各自写入一个顶层 key 才不会互相覆盖。
type Config struct {
	WebhookEnabled bool   `json:"webhook_enabled"`
	WebhookURL     string `json:"webhook_url"`
	WebhookMethod  string `json:"webhook_method"`
	WebhookHeaders string `json:"webhook_headers"`
	WebhookBody    string `json:"webhook_body"`

	BarkEnabled bool   `json:"bark_enabled"`
	BarkKey     string `json:"bark_key"`
	BarkTitle   string `json:"bark_title"` // 渠道级标题，优先级高于通知项；支持 {{title}} 引用通知项预填，留空回退通知项
	BarkBody    string `json:"bark_body"`  // 渠道级正文，同标题语义；支持 {{body}} 引用通知项预填

	// Bark 官方参数（https://bark.day.app/#/tutorial）
	BarkSubtitle   string `json:"bark_subtitle"`
	BarkGroup      string `json:"bark_group"`
	BarkLevel      string `json:"bark_level"` // critical | active | timeSensitive | passive
	BarkSound      string `json:"bark_sound"`
	BarkIcon       string `json:"bark_icon"`
	BarkImage      string `json:"bark_image"`
	BarkURL        string `json:"bark_url"`
	BarkBadge      string `json:"bark_badge"`
	BarkMarkdown   string `json:"bark_markdown"`
	BarkCopy       string `json:"bark_copy"`
	BarkIsArchive  string `json:"bark_isarchive"`
	BarkTTL        string `json:"bark_ttl"`
	BarkDeviceKeys string `json:"bark_devicekeys"`
	BarkVolume     string `json:"bark_volume"`
	BarkCall       string `json:"bark_call"`
	BarkAutoCopy   string `json:"bark_autocopy"`
	BarkCiphertext string `json:"bark_ciphertext"`
	BarkAction     string `json:"bark_action"`
	BarkID         string `json:"bark_id"`
	BarkDelete     string `json:"bark_delete"`

	ServerChanEnabled bool   `json:"serverchan_enabled"`
	ServerChanKey     string `json:"serverchan_key"`
	ServerChanTags    string `json:"serverchan_tags"`    // SC3：标签列表，界面按逗号输入，发送时转 |
	ServerChanShort   string `json:"serverchan_short"`   // 消息卡片简短描述
	ServerChanNoIP    bool   `json:"serverchan_noip"`    // SCT：隐藏调用 IP
	ServerChanChannel string `json:"serverchan_channel"` // SCT：消息通道，界面按逗号输入，发送时转 |
	ServerChanOpenID  string `json:"serverchan_openid"`  // SCT：抄送 openid，官方接口即逗号分隔
	ServerChanTitle   string `json:"serverchan_title"`   // 渠道级标题，优先级高于通知项；支持 {{title}} 引用通知项预填，留空回退通知项
	ServerChanBody    string `json:"serverchan_body"`    // 渠道级正文，同标题语义；支持 {{body}} 引用通知项预填

	// Telegram Bot 渠道
	TelegramEnabled             bool   `json:"telegram_enabled"`
	TelegramToken               string `json:"telegram_token"`                // Bot token（@BotFather 创建，仅拼接进 URL，不写入日志）
	TelegramChatID              string `json:"telegram_chat_id"`              // 接收 chat_id，逗号分隔支持多个
	TelegramTitle               string `json:"telegram_title"`                // 渠道级标题，支持 {{title}} 引用通知项预填，留空回退通知项
	TelegramBody                string `json:"telegram_body"`                 // 渠道级正文，同标题语义；支持 {{body}} 引用通知项预填
	TelegramParseMode           string `json:"telegram_parse_mode"`           // 空=纯文本；HTML / Markdown / MarkdownV2
	TelegramDisableNotification bool   `json:"telegram_disable_notification"` // 静默推送（disable_notification=true，不响铃）

	// Telegram 代理（部分网络环境无法直连 api.telegram.org）
	TelegramUseProxy       bool   `json:"telegram_use_proxy"`        // 是否使用代理访问 Telegram API
	TelegramUseUpdateProxy bool   `json:"telegram_use_update_proxy"` // 复用「更新设置」里配置的代理（读取 install/config/mxu-*.json）
	TelegramProxyURL       string `json:"telegram_proxy_url"`        // 手动代理地址（http:// 或 https://）

	// 失败通知
	OnFail bool `json:"on_fail"`

	FailTitle string `json:"fail_title"`
	FailBody  string `json:"fail_body"`

	// 自定义通知内容（经 attach 顶层键合并写入；NotifyTask 由设置页 option 注入，
	// 其他任务可在调用节点 attach 直接编写）。支持两种写法：普通文本，或 "$" 开头的
	// i18n key（与 MXU 前端约定一致，如 "$notify.monthly_card.expired"）。
	TaskTitle string `json:"task_title"` // 标题模板：普通文本，或 $ 开头的 i18n key
	TaskBody  string `json:"task_body"`  // 正文模板：普通文本，或 $ 开头的 i18n key

	// AllowTaskNotify 设置页总开关（收纳开关）：是否允许任务/节点通过 NotifySendAction 发送自定义通知。
	// nil=未配置（默认允许，不破坏旧行为）；设置页关闭时写入 false，同时收起所有通知项分项开关。
	AllowTaskNotify *bool `json:"allow_task_notify"`

	// TaskNotifyKey 通知项标识（调用节点 attach 写入，如 "monthly_card"）：配合设置页的
	// 通知项分项开关（attach 顶层键 "task_notify.<id>"）决定该通知项是否被用户关闭。
	TaskNotifyKey string `json:"task_notify_key"`

	// TaskNotifyToggles 通知项开关表：键为通知项 ID，值为设置页配置的开关。
	// 由 ParseConfig 从 attach 顶层 "task_notify.<id>" 键收集；未配置的通知项默认启用。
	TaskNotifyToggles map[string]bool `json:"-"`
}

// ParseConfig 从节点 JSON（含 attach）解析通知配置，并收集 "task_notify.<id>" 通知项开关。
func ParseConfig(nodeJSON string) (Config, error) {
	var node struct {
		Attach json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(nodeJSON), &node); err != nil {
		return Config{}, err
	}
	var config Config
	if len(node.Attach) > 0 {
		if err := json.Unmarshal(node.Attach, &config); err != nil {
			return Config{}, err
		}
		config.TaskNotifyToggles = parseTaskNotifyToggles(node.Attach)
	}
	return config, nil
}

// parseTaskNotifyToggles 从 attach 顶层键中提取 "task_notify.<id>" 形式的通知项开关。
// attach 合并按顶层 key 进行，每个通知项独立一个键，互不覆盖。
func parseTaskNotifyToggles(attach json.RawMessage) map[string]bool {
	var raw map[string]any
	if err := json.Unmarshal(attach, &raw); err != nil {
		return nil
	}
	toggles := make(map[string]bool)
	for key, value := range raw {
		id, ok := strings.CutPrefix(key, "task_notify.")
		if !ok || id == "" {
			continue
		}
		if v, ok := value.(bool); ok {
			toggles[id] = v
		}
	}
	return toggles
}

// Enabled 返回配置中是否有至少一个已注册且启用的渠道。
// 遍历注册表而非手写 || 链，新增渠道自动纳入判定。
func (c *Config) Enabled() bool {
	for _, name := range channelOrder {
		if channels[name].Enabled(*c) {
			return true
		}
	}
	return false
}

// BuildVars 构造内置模板变量。taskName 为任务入口名（entry），status 为本地化状态文本（如 失败/Failure）。
// startTime 为零值时跳过 duration 变量。
func BuildVars(taskName, status string, now, startTime time.Time) map[string]string {
	vars := map[string]string{
		"time":        now.Format("15:04:05"),
		"date":        now.Format("2006-01-02"),
		"datetime":    now.Format("2006-01-02 15:04:05"),
		"task_name":   taskName,
		"task_status": status,
	}
	if !startTime.IsZero() {
		vars["duration"] = now.Sub(startTime).Truncate(time.Second).String()
	}
	if name := pienv.ControllerName(); name != "" {
		vars["controller"] = name
	}
	if name := pienv.ResourceName(); name != "" {
		vars["resource"] = name
	}
	return vars
}

// ReplaceVars 把模板中的 {{key}} 替换为 vars 中的值，未识别的变量原样保留。
// 多轮替换直到收敛，避免 map 迭代顺序随机导致交叉引用结果不确定。
func ReplaceVars(template string, vars map[string]string) string {
	for i := 0; i < 10; i++ {
		prev := template
		for key, value := range vars {
			template = strings.ReplaceAll(template, "{{"+key+"}}", value)
		}
		if template == prev {
			return template
		}
	}
	return template
}

// Send 遍历注册表中所有已启用的渠道发送通知，任一渠道发送成功即返回 true；
// 全部失败返回 false（不影响调用方流程，仅记录日志）。
// 标题/正文优先级：渠道配置（bark_title 等，支持 {{title}}/{{body}} 引用通知项预填内容）> 通知项模板（vars["title"]/["body"]）> 默认标题。
// Webhook 不读取标题/正文，可在请求体里用 {{title}}/{{body}} 引用本次通知的标题/正文。
func Send(config Config, vars map[string]string) bool {
	if !config.Enabled() {
		log.Debug().Str("component", "Notify").Msg("no enabled channel, skip notify")
		return true
	}

	// 复制 vars：下方会把解析后的 title/body 写回供渠道引用，
	// clone 避免隐式修改调用方 map（sink 中 go Send 与调用方并发使用同一 map）。
	vars = maps.Clone(vars)
	if vars == nil {
		vars = map[string]string{}
	}

	// 通知项预填内容：此时 vars 尚无 title/body，通知项模板里写 {{title}}/{{body}} 不会被替换（天然无效）
	prefillTitle := ReplaceVars(firstNonEmpty(vars["title"]), vars)
	prefillBody := ReplaceVars(firstNonEmpty(vars["body"]), vars)
	// 写回变量，供渠道标题/正文与 Webhook 请求体 {{title}}/{{body}} 引用
	vars["title"] = firstNonEmpty(prefillTitle, i18n.T("notify.default_title"))
	vars["body"] = firstNonEmpty(prefillBody, i18n.T("notify.default_body"))

	sent := false
	for _, name := range channelOrder {
		ch := channels[name]
		if !ch.Enabled(config) {
			continue
		}
		if err := ch.Send(config, vars); err != nil {
			log.Error().Err(err).Str("component", "Notify").Str("channel", name).Msg("notify failed")
		} else {
			sent = true
		}
	}
	return sent
}

// NotifySendAction 供 Pipeline 手动触发通知：渠道配置从 __NotifyConfig 读取，
// 标题/正文支持两种写法——NotifyTask 由设置页 option 注入全局节点，
// 其他任务可在调用节点 attach 直接编写（本地优先）；内容以 "$" 开头时视为 i18n key
// （查不到翻译则显示去掉 $ 的 key，与 MXU 前端 resolveI18nText 约定一致）。
// 通知失败不会导致 Pipeline 节点失败。
type NotifySendAction struct{}

var _ maa.CustomActionRunner = &NotifySendAction{}

func (a *NotifySendAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 渠道配置与默认内容：__NotifyConfig（设置页全局 option 注入）
	raw, err := ctx.GetNodeJSON(defaultConfigNode)
	if err != nil {
		log.Error().Err(err).Str("component", "NotifySendAction").Str("node", defaultConfigNode).Msg("failed to get node json")
		return true
	}
	config, err := ParseConfig(raw)
	if err != nil {
		log.Error().Err(err).Str("component", "NotifySendAction").Str("node", defaultConfigNode).Msg("failed to parse notify config")
		return true
	}

	// 调用节点自定义内容（attach 本地优先，如月卡到期提醒等业务通知）
	if raw, err := ctx.GetNodeJSON(arg.CurrentTaskName); err == nil {
		if local, err := ParseConfig(raw); err == nil {
			config = mergeConfig(config, local)
		}
	}

	// 设置页总开关：关闭时跳过（未配置视为允许，不破坏旧行为）
	if config.AllowTaskNotify != nil && !*config.AllowTaskNotify {
		log.Debug().Str("component", "NotifySendAction").Msg("task notify disabled by setting, skip")
		return true
	}

	// 通知项级开关：节点声明了 task_notify_key 时，按设置页分项开关判断，未配置默认启用
	if taskNotifySkipped(config) {
		log.Debug().Str("component", "NotifySendAction").Str("item", config.TaskNotifyKey).Msg("notify item disabled by setting, skip")
		return true
	}

	// 任务名优先取入口名（TaskID 反查）并解析为显示名；反查取不到（任务执行初期
	// node_ids 为空时 maa-framework-go 不返回 entry，见 resolveActionTaskName 注释）
	// 则回退用当前节点名解析（NotifyTask 节点名即入口名，可正常解析）。
	taskName := resolveActionTaskName(
		arg.TaskID,
		arg.CurrentTaskName,
		func(id int64) string {
			td, err := ctx.GetTasker().GetTaskDetail(id)
			if err != nil || td.Entry == "" {
				return ""
			}
			return td.Entry
		},
	)

	vars := BuildVars(taskName, "", time.Now(), getControllerStartTime())
	vars["title"] = resolveNotifyText(config.TaskTitle, vars)
	vars["body"] = resolveNotifyText(config.TaskBody, vars)
	Send(config, vars)
	// 通知发送失败不影响游戏流程，始终返回成功
	return true
}

// resolveActionTaskName 解析通知动作的任务显示名：
//   - 优先用 GetTaskDetail 反查的任务入口名解析；
//   - 反查取不到（任务执行初期 node_ids 为空时，maa-framework-go 的
//     MaaTaskerGetTaskDetail 绑定在 size==0 时直接返回空 Entry，不读响应里的入口名）
//     则退回到用当前节点名解析（NotifyTask 的节点名即入口名，可解析为显示名）；
//   - 两者都查不到翻译时回退原样（resolveTaskName 的兜底行为）。
func resolveActionTaskName(taskID int64, currentTaskName string, getEntry func(int64) string) string {
	if taskID > 0 {
		if entry := getEntry(taskID); entry != "" {
			return resolveTaskName(entry)
		}
	}
	return resolveTaskName(currentTaskName)
}

// mergeConfig 合并全局渠道配置与调用节点内容配置：
// 渠道字段以全局 __NotifyConfig 为准；内容字段调用节点 attach 优先、回退全局
// （NotifyTask 的内容由设置页 option 注入全局节点，此处保证两种写法都生效）。
func mergeConfig(global, local Config) Config {
	if local.TaskTitle != "" {
		global.TaskTitle = local.TaskTitle
	}
	if local.TaskBody != "" {
		global.TaskBody = local.TaskBody
	}
	if local.TaskNotifyKey != "" {
		global.TaskNotifyKey = local.TaskNotifyKey
	}
	return global
}

// taskNotifySkipped 判断通知项级开关是否跳过发送：
// 未声明 task_notify_key 不判断；声明了但设置页未配置该通知项开关时默认启用。
func taskNotifySkipped(config Config) bool {
	key := config.TaskNotifyKey
	if key == "" {
		return false
	}
	enabled, ok := config.TaskNotifyToggles[key]
	return ok && !enabled
}

// resolveNotifyText 解析标题/正文：以 "$" 开头的文本视为 i18n key（与 MXU 前端约定一致），
// 查到翻译时用翻译（再做变量替换），查不到或非 "$" 开头时按原文处理。
// 回退语义与 i18n.T 一致：查不到翻译显示去掉 $ 的 key 本身。
func resolveNotifyText(text string, vars map[string]string) string {
	if strings.HasPrefix(text, "$") {
		key := strings.TrimPrefix(text, "$")
		if translated := i18n.T(key); translated != key {
			return ReplaceVars(translated, vars)
		}
		return ReplaceVars(key, vars)
	}
	return ReplaceVars(text, vars)
}
