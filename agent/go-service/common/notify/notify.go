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

	// 失败通知
	OnFail bool `json:"on_fail"`

	FailTitle string `json:"fail_title"`
	FailBody  string `json:"fail_body"`

	// 自定义通知内容（经 attach 顶层键合并写入；NotifyTask 由设置页 option 注入，
	// 其他任务可在调用节点 attach 直接编写，i18n key 优先于原文模板）
	TaskTitleKey string `json:"task_title_key"` // 标题 i18n key，查到翻译优先于 TaskTitle
	TaskBodyKey  string `json:"task_body_key"`  // 正文 i18n key，查到翻译优先于 TaskBody
	TaskTitle    string `json:"task_title"`     // 标题模板（原文）
	TaskBody     string `json:"task_body"`      // 正文模板（原文）

	// AllowTaskNotify 设置页总开关：是否允许任务/节点通过 NotifySendAction 发送自定义通知。
	// nil=未配置（默认允许，不破坏旧行为）；设置页关闭时写入 false。
	AllowTaskNotify *bool `json:"allow_task_notify"`
}

// ParseConfig 从节点 JSON（含 attach）解析通知配置。
func ParseConfig(nodeJSON string) (Config, error) {
	var node struct {
		Attach Config `json:"attach"`
	}
	if err := json.Unmarshal([]byte(nodeJSON), &node); err != nil {
		return Config{}, err
	}
	return node.Attach, nil
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
// 其他任务可在调用节点 attach 直接编写（本地优先），并支持 i18n key。
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

	vars := BuildVars(arg.CurrentTaskName, "", time.Now(), getControllerStartTime())
	vars["title"] = resolveNotifyText(config.TaskTitleKey, config.TaskTitle, vars)
	vars["body"] = resolveNotifyText(config.TaskBodyKey, config.TaskBody, vars)
	Send(config, vars)
	// 通知发送失败不影响游戏流程，始终返回成功
	return true
}

// mergeConfig 合并全局渠道配置与调用节点内容配置：
// 渠道字段以全局 __NotifyConfig 为准；内容字段调用节点 attach 优先、回退全局
// （NotifyTask 的内容由设置页 option 注入全局节点，此处保证两种写法都生效）。
func mergeConfig(global, local Config) Config {
	if local.TaskTitleKey != "" {
		global.TaskTitleKey = local.TaskTitleKey
	}
	if local.TaskBodyKey != "" {
		global.TaskBodyKey = local.TaskBodyKey
	}
	if local.TaskTitle != "" {
		global.TaskTitle = local.TaskTitle
	}
	if local.TaskBody != "" {
		global.TaskBody = local.TaskBody
	}
	return global
}

// resolveNotifyText 解析标题/正文：i18n key 查到翻译时用翻译（再做变量替换），
// key 未配置或查不到翻译时回退原文模板。
func resolveNotifyText(key, fallback string, vars map[string]string) string {
	if key != "" {
		if translated := i18n.T(key); translated != key {
			return ReplaceVars(translated, vars)
		}
	}
	return ReplaceVars(fallback, vars)
}
