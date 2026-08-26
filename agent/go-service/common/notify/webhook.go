package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// webhookConfig 是 Webhook 渠道的私有配置（attach 顶层 webhook_* 键）。
type webhookConfig struct {
	Enabled  bool   `json:"webhook_enabled"`
	UseProxy bool   `json:"webhook_use_proxy"` // 是否走全局代理（配合全局 use_proxy 主开关）
	URL      string `json:"webhook_url"`
	Method   string `json:"webhook_method"`
	Headers  string `json:"webhook_headers"`
	Body     string `json:"webhook_body"`
}

// webhookChannel 通用 Webhook 渠道：自定义方法/请求头/请求体，支持全部模板变量。
// 不读取标题/正文，可在请求体里用 {{title}}/{{body}} 引用本次通知的标题/正文。
type webhookChannel struct {
	cfg webhookConfig
}

func init() { RegisterChannel(webhookChannel{}) }

var _ ChannelFactory = webhookChannel{}
var _ Channel = webhookChannel{}

func (webhookChannel) Name() string {
	return "webhook"
}

func (webhookChannel) Create(attach map[string]any) (Channel, error) {
	var cfg webhookConfig
	if err := decodeAttach(attach, &cfg); err != nil {
		return nil, err
	}
	return webhookChannel{cfg: cfg}, nil
}

func (c webhookChannel) Enabled() bool {
	return c.cfg.Enabled
}

func (c webhookChannel) UseProxy() bool {
	return c.cfg.UseProxy
}

func (c webhookChannel) Send(ctx *SendContext) error {
	config := c.cfg
	vars := ctx.Vars
	urlStr := ReplaceVars(strings.TrimSpace(config.URL), vars)
	if urlStr == "" {
		return fmt.Errorf("webhook url is empty")
	}
	urlStr = ensureHTTPS(urlStr)
	method := strings.ToUpper(strings.TrimSpace(config.Method))
	if method == "" {
		method = http.MethodPost
	}
	headers := ParseHeaders(ReplaceVars(config.Headers, vars))
	body := ReplaceVars(config.Body, vars)

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
	resp, err := ctx.Client.Do(req)
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
			// 不记录原文：输入可能包含 Authorization 等凭据，漏写冒号时整行会被当成待解析内容
			log.Warn().Str("component", "Notify").Msg("invalid header line, expect \"Key: Value\"")
			continue
		}
		headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return headers
}
