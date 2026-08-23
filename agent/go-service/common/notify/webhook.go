package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// webhookChannel 通用 Webhook 渠道：自定义方法/请求头/请求体，支持全部模板变量。
// 不读取标题/正文，可在请求体里用 {{title}}/{{body}} 引用本次通知的标题/正文。
type webhookChannel struct{}

func init() { RegisterChannel(webhookChannel{}) }

var _ Channel = webhookChannel{}

func (webhookChannel) Name() string {
	return "webhook"
}

func (webhookChannel) Enabled(config Config) bool {
	return config.WebhookEnabled
}

func (webhookChannel) Send(config Config, vars map[string]string) error {
	urlStr := ReplaceVars(strings.TrimSpace(config.WebhookURL), vars)
	if urlStr == "" {
		return fmt.Errorf("webhook url is empty")
	}
	method := strings.ToUpper(strings.TrimSpace(config.WebhookMethod))
	if method == "" {
		method = http.MethodPost
	}
	headers := ParseHeaders(ReplaceVars(config.WebhookHeaders, vars))
	body := ReplaceVars(config.WebhookBody, vars)

	req, err := http.NewRequest(method, urlStr, strings.NewReader(body))
	if err != nil {
		return sanitizeError(err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	log.Debug().Str("component", "Notify").Str("channel", "webhook").Str("method", method).Msg("sending webhook notify")
	resp, err := httpClient.Do(req)
	if err != nil {
		return sanitizeError(err)
	}
	defer resp.Body.Close()
	return checkStatus(resp)
}

// ParseHeaders 解析请求头：优先按 JSON 对象（map[string]string）解析；
// 失败时回退按换行或 | 分隔的 "名称: 值" 文本格式。
func ParseHeaders(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}
	}
	var jsonHeaders map[string]string
	if err := json.Unmarshal([]byte(raw), &jsonHeaders); err == nil {
		return jsonHeaders
	}
	// JSON 解析失败：若输入意图明显是 JSON（以 { 开头），提示后回退文本格式
	if strings.HasPrefix(raw, "{") {
		log.Warn().Str("component", "Notify").Msg("headers JSON invalid, fall back to text format")
	}
	headers := make(map[string]string)
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '|' })
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, ":")
		if !ok {
			log.Warn().Str("component", "Notify").Str("line", part).Msg("invalid header line, expect \"Key: Value\"")
			continue
		}
		headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return headers
}
