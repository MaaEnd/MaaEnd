package notify

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// ntfyConfig 是 ntfy 渠道的私有配置（attach 顶层 channel_ntfy_* 键）。
type ntfyConfig struct {
	Enabled  bool   `json:"channel_ntfy_enabled"`
	UseProxy bool   `json:"channel_ntfy_use_proxy"` // 是否走全局代理（配合全局 use_proxy 主开关）
	URL      string `json:"channel_ntfy_url"`       // 完整地址（含 topic，如 https://ntfy.sh/mytopic，支持自托管；不写入日志）
	Title    string `json:"channel_ntfy_title"`     // 渠道级标题，支持 {{title}} 引用通知项预填，留空回退通知项
	Body     string `json:"channel_ntfy_body"`      // 渠道级正文，同标题语义；支持 {{body}} 引用通知项预填
	Priority string `json:"channel_ntfy_priority"`  // min | low | default | high | max，空=default
	Tags     string `json:"channel_ntfy_tags"`      // 标签，逗号分隔（emoji short code 自动转 emoji），可选
	Token    string `json:"channel_ntfy_token"`     // 访问令牌（私有 topic 认证），可选，仅放 header 不进 URL
}

// ntfyEndpoint 端点构造函数为包级变量，便于测试注入本地服务器。
var ntfyEndpoint = ntfyEndpointDefault

// ntfyChannel ntfy 渠道：向 topic 发布通知。
// 标题用 Title header、正文用请求 body（ntfy 原生语义，标题正文分离），
// 可选优先级/标签/认证；响应按 HTTP 状态判断（ntfy 无业务 code 字段）。
type ntfyChannel struct {
	cfg ntfyConfig
}

func init() { RegisterChannel(ntfyChannel{}) }

var _ ChannelFactory = ntfyChannel{}
var _ Channel = ntfyChannel{}

func (ntfyChannel) Name() string {
	return "ntfy"
}

func (ntfyChannel) Create(attach map[string]any) (Channel, error) {
	var cfg ntfyConfig
	if err := decodeAttach(attach, &cfg); err != nil {
		return nil, err
	}
	return ntfyChannel{cfg: cfg}, nil
}

func (c ntfyChannel) Enabled() bool {
	return c.cfg.Enabled
}

func (c ntfyChannel) UseProxy() bool {
	return c.cfg.UseProxy
}

func (c ntfyChannel) Send(ctx *SendContext) error {
	config := c.cfg
	vars := ctx.Vars
	endpoint, err := ntfyEndpoint(config.URL)
	if err != nil {
		return err
	}

	title, body := channelTitleBody(config.Title, config.Body, vars)
	if body == "" {
		return fmt.Errorf("ntfy message is empty")
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return sanitizeError(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	if title != "" {
		req.Header.Set("Title", title)
	}
	if priority := strings.TrimSpace(config.Priority); priority != "" && priority != "default" {
		if !isNtfyPriority(priority) {
			return fmt.Errorf("invalid ntfy priority: %s", priority)
		}
		req.Header.Set("Priority", priority)
	}
	if tags := strings.TrimSpace(ReplaceVars(config.Tags, vars)); tags != "" {
		req.Header.Set("Tags", tags)
	}
	if token := strings.TrimSpace(config.Token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	log.Debug().Str("component", "Notify").Str("channel", "ntfy").Msg("sending ntfy notify")
	resp, err := ctx.Client.Do(req)
	if err != nil {
		return sanitizeError(err)
	}
	defer resp.Body.Close()
	return checkStatus(resp)
}

// ntfyEndpointDefault 校验并返回 ntfy 发布端点（完整 URL，含 topic）。
// 留空/非法协议直接报错，topic 相当于密码不进入日志（sanitizeError 会对完整 URL 打码）。
func ntfyEndpointDefault(rawURL string) (string, error) {
	rawURL = ensureHTTPS(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("ntfy url is empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid ntfy url")
	}
	return rawURL, nil
}

// isNtfyPriority 校验 ntfy 优先级为官方白名单值（min/low/default/high/max，或数字 1~5）。
func isNtfyPriority(p string) bool {
	switch p {
	case "min", "low", "default", "high", "max":
		return true
	}
	// 兼容数字形式（官方允许 1~5）
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 5
}
