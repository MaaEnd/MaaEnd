package notify

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"
)

// discordConfig 是 Discord Webhook 渠道的私有配置（attach 顶层 channel_discord_* 键）。
type discordConfig struct {
	Enabled    bool   `json:"channel_discord_enabled"`
	UseProxy   bool   `json:"channel_discord_use_proxy"`   // 是否走全局代理（配合全局 use_proxy 主开关）
	WebhookURL string `json:"channel_discord_webhook_url"` // 完整 webhook 地址（含 id 与 token，不写入日志）
	Username   string `json:"channel_discord_username"`    // 覆盖 webhook 默认用户名（可选，支持模板变量）
	AvatarURL  string `json:"channel_discord_avatar_url"`  // 覆盖 webhook 默认头像（可选，支持模板变量）
	Title      string `json:"channel_discord_title"`       // 渠道级标题，支持 {{title}} 引用通知项预填，留空回退通知项
	Body       string `json:"channel_discord_body"`        // 渠道级正文，同标题语义；支持 {{body}} 引用通知项预填
}

// discordEndpoint 端点构造函数为包级变量，便于测试注入本地服务器。
var discordEndpoint = discordEndpointDefault

// discordChannel Discord Webhook 渠道：Execute Webhook 推送。
// content 为标题+正文拼合（与 Telegram 的 text 约定一致）；可选 username/avatar_url 覆盖。
// 复用统一调度（Send 遍历注册表）与标题/正文/模板变量约定，代理由调度统一提供。
type discordChannel struct {
	cfg discordConfig
}

func init() { RegisterChannel(discordChannel{}) }

var _ ChannelFactory = discordChannel{}
var _ Channel = discordChannel{}

func (discordChannel) Name() string {
	return "discord"
}

func (discordChannel) Create(attach map[string]any) (Channel, error) {
	var cfg discordConfig
	if err := decodeAttach(attach, &cfg); err != nil {
		return nil, err
	}
	return discordChannel{cfg: cfg}, nil
}

func (c discordChannel) Enabled() bool {
	return c.cfg.Enabled
}

func (c discordChannel) UseProxy() bool {
	return c.cfg.UseProxy
}

func (c discordChannel) Send(ctx *SendContext) error {
	config := c.cfg
	vars := ctx.Vars
	endpoint, err := discordEndpoint(config.WebhookURL)
	if err != nil {
		return err
	}

	// Discord Execute Webhook 的 content 字段：标题 + 正文拼合（各自支持模板变量与 {{title}}/{{body}} 预填）
	title, body := channelTitleBody(config.Title, config.Body, vars)
	content := title
	if body != "" {
		if content != "" {
			content += "\n\n"
		}
		content += body
	}
	if content == "" {
		return fmt.Errorf("discord content is empty")
	}

	payload := map[string]any{
		"content": content,
	}
	if username := strings.TrimSpace(ReplaceVars(config.Username, vars)); username != "" {
		payload["username"] = username
	}
	if avatarURL := strings.TrimSpace(ReplaceVars(config.AvatarURL, vars)); avatarURL != "" {
		payload["avatar_url"] = avatarURL
	}

	log.Debug().Str("component", "Notify").Str("channel", "discord").Msg("sending discord notify")
	// Execute Webhook 默认返回 204 No Content，无业务 code 字段 → 直接按 HTTP 状态判断（<400 成功）
	return postJSON(ctx.Client, endpoint, payload, -1)
}

// discordEndpointDefault 校验并返回 Discord Webhook 执行端点（完整 URL，含 webhook id 与 token）。
// Webhook URL 即完整端点：留空/非法协议直接报错，token 不进入日志（sanitizeError 会对完整 URL 打码）。
func discordEndpointDefault(webhookURL string) (string, error) {
	webhookURL = ensureHTTPS(webhookURL)
	if webhookURL == "" {
		return "", fmt.Errorf("discord webhook url is empty")
	}
	u, err := url.Parse(webhookURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid discord webhook url")
	}
	return webhookURL, nil
}
