package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// telegramConfig 是 Telegram Bot 渠道的私有配置（attach 顶层 channel_telegram_* 键）。
type telegramConfig struct {
	Enabled             bool   `json:"channel_telegram_enabled"`
	UseProxy            bool   `json:"channel_telegram_use_proxy"`            // 是否走全局代理（配合全局 use_proxy 主开关）
	Token               string `json:"channel_telegram_token"`                // Bot token（@BotFather 创建，仅拼接进 URL，不写入日志）
	APIURL              string `json:"channel_telegram_api_url"`              // 第三方 API 服务地址；留空用官方 https://api.telegram.org
	ChatID              string `json:"channel_telegram_chat_id"`              // 接收 chat_id，逗号分隔支持多个
	Title               string `json:"channel_telegram_title"`                // 渠道级标题，支持 {{title}} 引用通知项预填，留空回退通知项
	Body                string `json:"channel_telegram_body"`                 // 渠道级正文，同标题语义；支持 {{body}} 引用通知项预填
	ParseMode           string `json:"channel_telegram_parse_mode"`           // 空=纯文本；HTML / Markdown / MarkdownV2
	DisableNotification bool   `json:"channel_telegram_disable_notification"` // 静默推送（disable_notification=true，不响铃）
}

// telegramEndpoint 端点构造函数为包级变量，便于测试注入本地服务器。
var telegramEndpoint = telegramEndpointDefault

// telegramAPIURLDefault 官方 Telegram Bot API 服务地址。
const telegramAPIURLDefault = "https://api.telegram.org"

// telegramChannel Telegram Bot 渠道：通过 Bot API sendMessage 推送通知。
// 复用统一调度（Send 遍历注册表）与标题/正文/模板变量约定。
type telegramChannel struct {
	cfg telegramConfig
}

func init() { RegisterChannel(telegramChannel{}) }

var _ ChannelFactory = telegramChannel{}
var _ Channel = telegramChannel{}

func (telegramChannel) Name() string {
	return "telegram"
}

func (telegramChannel) Create(attach map[string]any) (Channel, error) {
	var cfg telegramConfig
	if err := decodeAttach(attach, &cfg); err != nil {
		return nil, err
	}
	return telegramChannel{cfg: cfg}, nil
}

func (c telegramChannel) Enabled() bool {
	return c.cfg.Enabled
}

func (c telegramChannel) UseProxy() bool {
	return c.cfg.UseProxy
}

func (c telegramChannel) Send(ctx *SendContext) error {
	config := c.cfg
	vars := ctx.Vars
	endpoint, err := telegramEndpoint(strings.TrimSpace(config.Token), config.APIURL)
	if err != nil {
		return err
	}
	chatIDs := splitList(config.ChatID, ',')
	if len(chatIDs) == 0 {
		return fmt.Errorf("telegram chat_id is empty")
	}

	// Telegram sendMessage 只有一个 text 字段：标题 + 正文拼合（各自支持模板变量与 {{title}}/{{body}} 预填）
	title, body := channelTitleBody(config.Title, config.Body, vars)
	text := title
	if body != "" {
		if text != "" {
			text += "\n\n"
		}
		text += body
	}
	if text == "" {
		return fmt.Errorf("telegram text is empty")
	}

	parseMode := ReplaceVars(strings.TrimSpace(config.ParseMode), vars)
	switch parseMode {
	case "", "HTML", "Markdown", "MarkdownV2":
		// 合法取值
	default:
		return fmt.Errorf("invalid telegram parse_mode: %s", parseMode)
	}
	// Markdown/MarkdownV2/HTML 模式需转义正文特殊字符，否则 Telegram 返回 400 can't parse entities；
	// 统一走 markdown.go 的解析器渲染（实体外转义、实体内部按官方规则豁免、按方言输出）
	switch parseMode {
	case "MarkdownV2":
		text = renderTelegramV2(text)
	case "Markdown":
		text = renderTelegramLegacy(text)
	case "HTML":
		text = renderTelegramHTML(text)
	}

	for _, chatID := range chatIDs {
		payload := map[string]any{
			"chat_id": chatID,
			"text":    text,
		}
		if parseMode != "" {
			payload["parse_mode"] = parseMode
		}
		if config.DisableNotification {
			payload["disable_notification"] = true
		}
		log.Debug().Str("component", "Notify").Str("channel", "telegram").Str("chat_id", chatID).Msg("sending telegram notify")
		if err := postTelegram(ctx.Client, endpoint, payload); err != nil {
			return err
		}
	}
	return nil
}

// postTelegram 发送 sendMessage 请求并校验 Telegram 响应（HTTP < 400 且 ok == true）。
// Telegram 的成功响应是 {"ok":true,...}，与 postJSON 的 code 字段约定不同，故单独实现。
func postTelegram(client *http.Client, endpoint string, payload map[string]any) error {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := client.Post(endpoint, "application/json;charset=utf-8", bytes.NewReader(jsonBody))
	if err != nil {
		return sanitizeError(err)
	}
	defer resp.Body.Close()
	// 先读响应体（限流），Telegram 真实失败几乎都返回 4xx + {"ok":false,"description":...}，
	// 需读 body 拿 description，不能先按状态码早退
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		if resp.StatusCode >= 400 {
			return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}
		return fmt.Errorf("failed to parse response body: %w", err)
	}
	if !result.OK {
		if result.Description != "" {
			return fmt.Errorf("telegram api error: %s (status %d)", result.Description, resp.StatusCode)
		}
		return fmt.Errorf("telegram api error: ok=false (status %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}

// telegramEndpointDefault 构造 Telegram Bot API sendMessage 端点。
// apiURL 留空使用官方服务地址，填写第三方 API 服务地址（如 https://tg-proxy.example.com），
// 自动拼接 /bot{token}/sendMessage；注意：token 是 URL 路径的一部分，不进入日志（sanitizeError 会对完整 URL 打码）。
func telegramEndpointDefault(token, apiURL string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("telegram token is empty")
	}
	base := strings.TrimRight(ensureHTTPS(apiURL), "/")
	if base == "" {
		base = telegramAPIURLDefault
	}
	return fmt.Sprintf("%s/bot%s/sendMessage", base, token), nil
}

// escapeTelegramMarkdownV2 转义 Telegram MarkdownV2 模式的特殊字符（官方要求转义 18 个字符 + 转义前缀反斜杠）。
func escapeTelegramMarkdownV2(s string) string {
	return strings.NewReplacer(
		"_", `\_`, "*", `\*`, "[", `\[`, "]", `\]`, "(", `\(`, ")", `\)`,
		"~", `\~`, "`", "\\`", ">", `\>`, "#", `\#`, "+", `\+`, "-", `\-`,
		"=", `\=`, "|", `\|`, "{", `\{`, "}", `\}`, ".", `\.`, "!", `\!`,
		"\\", `\\`,
	).Replace(s)
}

// escapeTelegramMarkdown 转义 Telegram legacy Markdown 模式实体外的特殊字符。
// 官方只允许转义 _ * ` [ 四字符（"To escape characters '_', '*', '`', '[' outside
// of an entity, prepend the character '\' before them"），故不转义 ] 与 \。
func escapeTelegramMarkdown(s string) string {
	return strings.NewReplacer(
		"_", `\_`, "*", `\*`, "`", "\\`", "[", `\[`,
	).Replace(s)
}

// escapeTelegramHTML 转义 Telegram HTML 模式的特殊字符（< > &）。
func escapeTelegramHTML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
