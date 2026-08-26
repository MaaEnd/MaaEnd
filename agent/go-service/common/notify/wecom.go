package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"
)

// wecomConfig 是企业微信（WeCom）群机器人渠道的私有配置（attach 顶层 wecom_* 键）。
type wecomConfig struct {
	Enabled    bool   `json:"wecom_enabled"`
	UseProxy   bool   `json:"wecom_use_proxy"`   // 是否走全局代理（配合全局 use_proxy 主开关）
	WebhookURL string `json:"wecom_webhook_url"` // 完整 webhook 地址（含 key query 参数，不写入日志）
	MsgType    string `json:"wecom_msgtype"`     // text（默认）| markdown | markdown_v2
	Title      string `json:"wecom_title"`       // 渠道级标题，支持 {{title}} 引用通知项预填，留空回退通知项
	Body       string `json:"wecom_body"`        // 渠道级正文，同标题语义；支持 {{body}} 引用通知项预填
}

// wecomEndpoint 端点构造函数为包级变量，便于测试注入本地服务器。
var wecomEndpoint = wecomEndpointDefault

// wecomChannel 企业微信群机器人渠道：向 webhook 推送消息。
// 消息类型为文本类（text/markdown/markdown_v2），content 为标题+正文拼合（与 Telegram/Discord 约定一致）。
// 响应校验 errcode（0=成功），避免企微业务失败被 HTTP 200 掩盖。
type wecomChannel struct {
	cfg wecomConfig
}

func init() { RegisterChannel(wecomChannel{}) }

var _ ChannelFactory = wecomChannel{}
var _ Channel = wecomChannel{}

func (wecomChannel) Name() string {
	return "wecom"
}

func (wecomChannel) Create(attach map[string]any) (Channel, error) {
	var cfg wecomConfig
	if err := decodeAttach(attach, &cfg); err != nil {
		return nil, err
	}
	return wecomChannel{cfg: cfg}, nil
}

func (c wecomChannel) Enabled() bool {
	return c.cfg.Enabled
}

func (c wecomChannel) UseProxy() bool {
	return c.cfg.UseProxy
}

func (c wecomChannel) Send(ctx *SendContext) error {
	config := c.cfg
	vars := ctx.Vars
	endpoint, err := wecomEndpoint(config.WebhookURL)
	if err != nil {
		return err
	}

	// 企微文本类消息只有单一 content 字段：标题 + 正文拼合（各自支持模板变量与 {{title}}/{{body}} 预填）
	title, body := channelTitleBody(config.Title, config.Body, vars)
	content := title
	if body != "" {
		if content != "" {
			content += "\n\n"
		}
		content += body
	}
	if content == "" {
		return fmt.Errorf("wecom content is empty")
	}

	msgType := strings.TrimSpace(config.MsgType)
	if msgType == "" {
		msgType = "text"
	}
	payload := map[string]any{
		"msgtype": msgType,
		msgType:   map[string]any{"content": content},
	}

	log.Debug().Str("component", "Notify").Str("channel", "wecom").Str("msgtype", msgType).Msg("sending wecom notify")
	return postWeCom(ctx.Client, endpoint, payload)
}

// postWeCom 发送企微消息并校验响应（HTTP < 400 且 errcode == 0）。
// 企微成功响应为 {"errcode":0,"errmsg":"ok"}，失败时 errcode 非 0 但 HTTP 仍 200，
// 故单独实现（与 postJSON 的 code 字段约定不同，也区别于 Telegram 的 ok 布尔）。
func postWeCom(client *http.Client, endpoint string, payload map[string]any) error {
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
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse response body: %w", err)
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("wecom api error: %s (errcode %d)", result.ErrMsg, result.ErrCode)
	}
	return nil
}

// wecomEndpointDefault 校验并返回企微消息推送端点（完整 URL，含 key query 参数）。
// 留空/非法协议直接报错，key 不进入日志（sanitizeError 会对完整 URL 打码）。
func wecomEndpointDefault(webhookURL string) (string, error) {
	webhookURL = ensureHTTPS(webhookURL)
	if webhookURL == "" {
		return "", fmt.Errorf("wecom webhook url is empty")
	}
	u, err := url.Parse(webhookURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid wecom webhook url")
	}
	return webhookURL, nil
}
