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

// telegramEndpoint 端点构造函数为包级变量，便于测试注入本地服务器。
var telegramEndpoint = telegramEndpointDefault

// telegramAPIURLDefault 官方 Telegram Bot API 服务地址。
const telegramAPIURLDefault = "https://api.telegram.org"

// telegramChannel Telegram Bot 渠道：通过 Bot API sendMessage 推送通知。
// 复用统一调度（Send 遍历注册表）与标题/正文/模板变量约定。
type telegramChannel struct{}

func init() { RegisterChannel(telegramChannel{}) }

var _ Channel = telegramChannel{}

func (telegramChannel) Name() string {
	return "telegram"
}

func (telegramChannel) Enabled(config Config) bool {
	return config.TelegramEnabled
}

func (telegramChannel) Send(config Config, vars map[string]string) error {
	endpoint, err := telegramEndpoint(strings.TrimSpace(config.TelegramToken), config.TelegramAPIURL)
	if err != nil {
		return err
	}
	client := httpClient
	if config.TelegramUseProxy {
		proxyURL, err := resolveTelegramProxy(config)
		if err != nil {
			return err
		}
		if client, err = proxyClient(proxyURL); err != nil {
			return err
		}
	}
	chatIDs := splitList(config.TelegramChatID, ',')
	if len(chatIDs) == 0 {
		return fmt.Errorf("telegram chat_id is empty")
	}

	// Telegram sendMessage 只有一个 text 字段：标题 + 正文拼合（各自支持模板变量与 {{title}}/{{body}} 预填）
	title, body := channelTitleBody(config.TelegramTitle, config.TelegramBody, vars)
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

	parseMode := ReplaceVars(strings.TrimSpace(config.TelegramParseMode), vars)

	for _, chatID := range chatIDs {
		payload := map[string]any{
			"chat_id": chatID,
			"text":    text,
		}
		if parseMode != "" {
			payload["parse_mode"] = parseMode
		}
		if config.TelegramDisableNotification {
			payload["disable_notification"] = true
		}
		log.Debug().Str("component", "Notify").Str("channel", "telegram").Str("chat_id", chatID).Msg("sending telegram notify")
		if err := postTelegram(client, endpoint, payload); err != nil {
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
	if resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	// 限制响应体大小（1MB），防止服务端异常返回超大 body 吃内存
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse response body: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("telegram api error: %s", result.Description)
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
	base := strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if base == "" {
		base = telegramAPIURLDefault
	}
	return fmt.Sprintf("%s/bot%s/sendMessage", base, token), nil
}
