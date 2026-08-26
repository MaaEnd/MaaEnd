package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// gotifyConfig 是 Gotify 渠道的私有配置（attach 顶层 channel_gotify_* 键）。
type gotifyConfig struct {
	Enabled  bool   `json:"channel_gotify_enabled"`
	UseProxy bool   `json:"channel_gotify_use_proxy"` // 是否走全局代理（配合全局 use_proxy 主开关）
	URL      string `json:"channel_gotify_url"`       // 服务器根地址（如 https://push.example.de，不含 /message；不写入日志）
	Token    string `json:"channel_gotify_token"`     // app token（X-Gotify-Key header，不写入日志）
	Title    string `json:"channel_gotify_title"`     // 渠道级标题，支持 {{title}} 引用通知项预填，留空回退通知项
	Body     string `json:"channel_gotify_body"`      // 渠道级正文，同标题语义；支持 {{body}} 引用通知项预填
	Priority string `json:"channel_gotify_priority"`  // "0"~"10"，空=用 application 默认优先级
	Markdown bool   `json:"channel_gotify_markdown"`  // 正文按 markdown 渲染（WebUI≥2.0.5 GFM / Android≥2.0.7 commonmark）
}

// gotifyEndpoint 端点构造函数为包级变量，便于测试注入本地服务器。
var gotifyEndpoint = gotifyEndpointDefault

// gotifyChannel Gotify 渠道：向服务器发布通知。
// 鉴权用 X-Gotify-Key header，标题/正文/优先级都在 JSON body；
// 响应按 HTTP 状态判断（错误体含 errorCode 但为状态码镜像，无需解析业务码）。
type gotifyChannel struct {
	cfg gotifyConfig
}

func init() { RegisterChannel(gotifyChannel{}) }

var _ ChannelFactory = gotifyChannel{}
var _ Channel = gotifyChannel{}

func (gotifyChannel) Name() string {
	return "gotify"
}

func (gotifyChannel) Create(attach map[string]any) (Channel, error) {
	var cfg gotifyConfig
	if err := decodeAttach(attach, &cfg); err != nil {
		return nil, err
	}
	return gotifyChannel{cfg: cfg}, nil
}

func (c gotifyChannel) Enabled() bool {
	return c.cfg.Enabled
}

func (c gotifyChannel) UseProxy() bool {
	return c.cfg.UseProxy
}

func (c gotifyChannel) Send(ctx *SendContext) error {
	config := c.cfg
	vars := ctx.Vars
	endpoint, err := gotifyEndpoint(config.URL)
	if err != nil {
		return err
	}

	title, body := channelTitleBody(config.Title, config.Body, vars)
	if body == "" {
		return fmt.Errorf("gotify message is empty")
	}

	payload := map[string]any{"message": body}
	if title != "" {
		payload["title"] = title
	}
	if priority := strings.TrimSpace(config.Priority); priority != "" {
		n, err := strconv.Atoi(priority)
		if err != nil {
			return fmt.Errorf("invalid gotify priority: %s", priority)
		}
		if n < 0 || n > 10 {
			return fmt.Errorf("gotify priority out of range 0~10: %d", n)
		}
		payload["priority"] = n
	}
	if config.Markdown {
		// Gotify 官方：消息自带 extras["client::display"]["contentType"]="text/markdown"
		// 决定客户端逐条渲染（JSON body 才接受 extras；默认 text/plain）
		payload["extras"] = map[string]any{
			"client::display": map[string]any{"contentType": "text/markdown"},
		}
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return sanitizeError(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", strings.TrimSpace(config.Token))

	log.Debug().Str("component", "Notify").Str("channel", "gotify").Msg("sending gotify notify")
	resp, err := ctx.Client.Do(req)
	if err != nil {
		return sanitizeError(err)
	}
	defer resp.Body.Close()
	return checkStatus(resp)
}

// gotifyEndpointDefault 校验并返回 Gotify 发布端点（服务器根地址拼 /message）。
// 留空/非法协议直接报错，token 相当于密码不进入日志（sanitizeError 对完整 URL 打码）。
func gotifyEndpointDefault(rawURL string) (string, error) {
	rawURL = ensureHTTPS(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("gotify url is empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid gotify url")
	}
	return strings.TrimRight(rawURL, "/") + "/message", nil
}
