package notify

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
)

// serverChanEndpoint 端点构造函数为包级变量，便于测试注入本地服务器。
var serverChanEndpoint = serverChanEndpointDefault

// serverChanChannel ServerChan 渠道：SC3 / Turbo 双端点自动分流。
type serverChanChannel struct{}

func init() { RegisterChannel(serverChanChannel{}) }

var _ Channel = serverChanChannel{}

func (serverChanChannel) Name() string {
	return "serverchan"
}

func (serverChanChannel) Enabled(config Config) bool {
	return config.ServerChanEnabled
}

func (serverChanChannel) Send(config Config, vars map[string]string) error {
	endpoint, err := serverChanEndpoint(config.ServerChanKey)
	if err != nil {
		return err
	}
	title, body := channelTitleBody(config.ServerChanTitle, config.ServerChanBody, vars)
	payload := map[string]any{
		"title": title,
		"desp":  body,
	}
	if tags := pipeSeparated(ReplaceVars(config.ServerChanTags, vars)); tags != "" {
		payload["tags"] = tags
	}
	addIfPresent(payload, vars, "short", config.ServerChanShort)
	if config.ServerChanNoIP {
		payload["noip"] = 1
	}
	if channel := pipeSeparated(ReplaceVars(config.ServerChanChannel, vars)); channel != "" {
		payload["channel"] = channel
	}
	addIfPresent(payload, vars, "openid", config.ServerChanOpenID)
	log.Debug().Str("component", "Notify").Str("channel", "serverchan").Msg("sending serverchan notify")
	return postJSON(endpoint, payload, 0)
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
