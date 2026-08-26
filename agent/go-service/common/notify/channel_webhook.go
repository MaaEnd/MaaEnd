package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// webhookConfig 是 Webhook 渠道的私有配置（attach 顶层 channel_webhook_* 键）。
type webhookConfig struct {
	Enabled  bool   `json:"channel_webhook_enabled"`
	UseProxy bool   `json:"channel_webhook_use_proxy"` // 是否走全局代理（配合全局 use_proxy 主开关）
	URL      string `json:"channel_webhook_url"`
	Method   string `json:"channel_webhook_method"`
	Headers  string `json:"channel_webhook_headers"`
	Body     string `json:"channel_webhook_body"`
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
	urlStr, err := normalizeWebhookURL(ReplaceVars(strings.TrimSpace(config.URL), vars))
	if err != nil {
		return err
	}
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
// 失败时回退按换行分隔的 "名称: 值" 文本格式（保留 | 在值内，如 Authorization: Bearer a|b）。
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
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' })
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
		key = strings.TrimSpace(key)
		if key == "" {
			log.Warn().Str("component", "Notify").Msg("empty header name ignored")
			continue
		}
		headers[key] = strings.TrimSpace(value)
	}
	return headers
}

// normalizeWebhookURL 规范化 webhook 地址：
// 无 scheme 且形如 host:port（含 IP:port，如 192.168.1.5:8080）时视为自托管 http 服务，补 http://；
// 否则走 validateHTTPURL（补 https:// 并校验 http/https scheme 白名单）。
func normalizeWebhookURL(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("webhook url is empty")
	}
	if strings.Index(raw, "://") > 0 {
		return validateHTTPURL(raw, "webhook") // 已含 scheme：直接校验
	}
	if isHostPort(raw) {
		return "http://" + raw, nil // 自托管 host:port：默认 http
	}
	return validateHTTPURL(raw, "webhook")
}

// isHostPort 判断无 scheme 的地址是否形如 host:port（如 192.168.1.5:8080、localhost:3000）。
func isHostPort(raw string) bool {
	i := strings.LastIndex(raw, ":")
	if i <= 0 || i == len(raw)-1 {
		return false
	}
	host, port := raw[:i], raw[i+1:]
	if strings.ContainsAny(host, "/") {
		return false
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
