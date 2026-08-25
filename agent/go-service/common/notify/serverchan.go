package notify

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
)

// serverChanConfig 是 ServerChan 渠道的私有配置（attach 顶层 serverchan_* 键）。
type serverChanConfig struct {
	Enabled  bool   `json:"serverchan_enabled"`
	UseProxy bool   `json:"serverchan_use_proxy"` // 是否走全局代理（配合全局 use_proxy 主开关）
	Key      string `json:"serverchan_key"`
	Tags     string `json:"serverchan_tags"`    // SC3：标签列表，界面按逗号输入，发送时转 |
	Short    string `json:"serverchan_short"`   // 消息卡片简短描述
	NoIP     bool   `json:"serverchan_noip"`    // SCT：隐藏调用 IP
	Channel  string `json:"serverchan_channel"` // SCT：消息通道，界面按逗号输入，发送时转 |
	OpenID   string `json:"serverchan_openid"`  // SCT：抄送 openid，官方接口即逗号分隔
	Title    string `json:"serverchan_title"`   // 渠道级标题，优先级高于通知项；支持 {{title}} 引用通知项预填，留空回退通知项
	Body     string `json:"serverchan_body"`    // 渠道级正文，同标题语义；支持 {{body}} 引用通知项预填
}

// serverChanEndpoint 端点构造函数为包级变量，便于测试注入本地服务器。
var serverChanEndpoint = serverChanEndpointDefault

// serverChanChannel ServerChan 渠道：SC3 / Turbo 双端点自动分流。
type serverChanChannel struct {
	cfg serverChanConfig
}

func init() { RegisterChannel(serverChanChannel{}) }

var _ ChannelFactory = serverChanChannel{}
var _ Channel = serverChanChannel{}

func (serverChanChannel) Name() string {
	return "serverchan"
}

func (serverChanChannel) Create(attach map[string]any) (Channel, error) {
	var cfg serverChanConfig
	if err := decodeAttach(attach, &cfg); err != nil {
		return nil, err
	}
	return serverChanChannel{cfg: cfg}, nil
}

func (c serverChanChannel) Enabled() bool {
	return c.cfg.Enabled
}

func (c serverChanChannel) UseProxy() bool {
	return c.cfg.UseProxy
}

func (c serverChanChannel) Send(ctx *SendContext) error {
	config := c.cfg
	vars := ctx.Vars
	endpoint, err := serverChanEndpoint(config.Key)
	if err != nil {
		return err
	}
	title, body := channelTitleBody(config.Title, config.Body, vars)
	payload := map[string]any{
		"title": title,
		"desp":  body,
	}
	if tags := pipeSeparated(ReplaceVars(config.Tags, vars)); tags != "" {
		payload["tags"] = tags
	}
	addIfPresent(payload, vars, "short", config.Short)
	if config.NoIP {
		payload["noip"] = 1
	}
	if channel := pipeSeparated(ReplaceVars(config.Channel, vars)); channel != "" {
		payload["channel"] = channel
	}
	addIfPresent(payload, vars, "openid", config.OpenID)
	log.Debug().Str("component", "Notify").Str("channel", "serverchan").Msg("sending serverchan notify")
	return postJSON(ctx.Client, endpoint, payload, 0)
}

// serverChanEndpointDefault 根据 SendKey 前缀选择端点：
// sctp 开头为 ServerChan3（SC3），端点为 https://{uid}.push.ft07.com/send/{sendkey}.send（uid 从 sendkey 提取）；
// 其余为 ServerChan Turbo，端点为 https://sctapi.ftqq.com/{sendkey}.send。
func serverChanEndpointDefault(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("serverchan sendkey is empty")
	}
	if strings.HasPrefix(key, "sctp") {
		uid, ok := sc3UID(key)
		if !ok {
			// 错误信息不带 sendkey 原文（含 token 凭据，直接进日志会泄漏）
			return "", fmt.Errorf("invalid serverchan3 sendkey")
		}
		return fmt.Sprintf("https://%s.push.ft07.com/send/%s.send", uid, url.PathEscape(key)), nil
	}
	return fmt.Sprintf("https://sctapi.ftqq.com/%s.send", url.PathEscape(key)), nil
}

// sc3UIDRegexp 官方正则：sctp{uid}t{token}，uid 为纯数字（https://sct.ftqq.com 文档）。
var sc3UIDRegexp = regexp.MustCompile(`^sctp(\d+)t`)

// sc3UID 从 sctp{uid}t{token} 中提取 uid；格式不符合官方正则（无 t 分隔或 uid 非纯数字）时返回 false。
func sc3UID(key string) (string, bool) {
	m := sc3UIDRegexp.FindStringSubmatch(key)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// pipeSeparated 把用户按逗号输入的多值列表统一为官方接口要求的 | 分隔。
func pipeSeparated(raw string) string {
	return strings.Join(splitList(raw, ',', '，', '、', '|'), "|")
}
