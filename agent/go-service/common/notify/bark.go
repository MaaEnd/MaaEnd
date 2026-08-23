package notify

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// barkEndpoint 端点构造函数为包级变量，便于测试注入本地服务器。
var barkEndpoint = barkEndpointDefault

// barkChannel Bark 渠道：支持官方文档全部参数（非空才携带，均做变量替换）。
type barkChannel struct{}

func init() { RegisterChannel(barkChannel{}) }

var _ Channel = barkChannel{}

func (barkChannel) Name() string {
	return "bark"
}

func (barkChannel) Enabled(config Config) bool {
	return config.BarkEnabled
}

func (barkChannel) Send(config Config, vars map[string]string) error {
	endpoint, err := barkEndpoint(config.BarkKey)
	if err != nil {
		return err
	}
	title, body := channelTitleBody(config.BarkTitle, config.BarkBody, vars)
	payload := map[string]any{
		"title": title,
		"body":  body,
	}
	for _, p := range []struct{ key, val string }{
		{"subtitle", config.BarkSubtitle},
		{"group", config.BarkGroup},
		{"level", config.BarkLevel},
		{"sound", config.BarkSound},
		{"icon", config.BarkIcon},
		{"image", config.BarkImage},
		{"url", config.BarkURL},
		{"markdown", config.BarkMarkdown},
		{"copy", config.BarkCopy},
		{"isArchive", config.BarkIsArchive},
		{"ttl", config.BarkTTL},
		{"device_keys", config.BarkDeviceKeys},
		{"volume", config.BarkVolume},
		{"call", config.BarkCall},
		{"autoCopy", config.BarkAutoCopy},
		{"ciphertext", config.BarkCiphertext},
		{"action", config.BarkAction},
		{"id", config.BarkID},
		{"delete", config.BarkDelete},
	} {
		addIfPresent(payload, vars, p.key, p.val)
	}
	// badge 为数字参数，纯数字时按整数发送
	if badge := ReplaceVars(strings.TrimSpace(config.BarkBadge), vars); badge != "" {
		if n, err := strconv.Atoi(badge); err == nil {
			payload["badge"] = n
		} else {
			payload["badge"] = badge
		}
	}
	log.Debug().Str("component", "Notify").Str("channel", "bark").Msg("sending bark notify")
	return postJSON(endpoint, payload, 200)
}

// barkEndpointDefault 构造 Bark 推送端点（POST JSON 到 https://api.day.app/{key}）。
func barkEndpointDefault(key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("bark key is empty")
	}
	return fmt.Sprintf("https://api.day.app/%s", url.PathEscape(strings.TrimSpace(key))), nil
}
