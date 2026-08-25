package notify

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// barkConfig 是 Bark 渠道的私有配置（attach 顶层 bark_* 键，含官方全部参数）。
type barkConfig struct {
	Enabled  bool   `json:"bark_enabled"`
	UseProxy bool   `json:"bark_use_proxy"` // 是否走全局代理（配合全局 use_proxy 主开关）
	Key      string `json:"bark_key"`
	Title    string `json:"bark_title"` // 渠道级标题，优先级高于通知项；支持 {{title}} 引用通知项预填，留空回退通知项
	Body     string `json:"bark_body"`  // 渠道级正文，同标题语义；支持 {{body}} 引用通知项预填

	// Bark 官方参数（https://bark.day.app/#/tutorial）
	Subtitle   string `json:"bark_subtitle"`
	Group      string `json:"bark_group"`
	Level      string `json:"bark_level"` // critical | active | timeSensitive | passive
	Sound      string `json:"bark_sound"`
	Icon       string `json:"bark_icon"`
	Image      string `json:"bark_image"`
	URL        string `json:"bark_url"`
	Badge      string `json:"bark_badge"`
	Markdown   string `json:"bark_markdown"`
	Copy       string `json:"bark_copy"`
	IsArchive  string `json:"bark_isarchive"`
	TTL        string `json:"bark_ttl"`
	DeviceKeys string `json:"bark_devicekeys"`
	Volume     string `json:"bark_volume"`
	Call       string `json:"bark_call"`
	AutoCopy   string `json:"bark_autocopy"`
	Ciphertext string `json:"bark_ciphertext"`
	Action     string `json:"bark_action"`
	ID         string `json:"bark_id"`
	Delete     string `json:"bark_delete"`
}

// barkEndpoint 单设备端点构造函数为包级变量，便于测试注入本地服务器。
var barkEndpoint = barkEndpointDefault

// barkBatchEndpoint 批量推送端点构造函数为包级变量，便于测试注入本地服务器。
var barkBatchEndpoint = barkBatchEndpointDefault

// barkChannel Bark 渠道：支持官方文档全部参数（非空才携带，均做变量替换）。
// 配置了 device_keys（逗号分隔的 key 数组）时走批量推送接口 /push。
type barkChannel struct {
	cfg barkConfig
}

func init() { RegisterChannel(barkChannel{}) }

var _ ChannelFactory = barkChannel{}
var _ Channel = barkChannel{}

func (barkChannel) Name() string {
	return "bark"
}

func (barkChannel) Create(attach map[string]any) (Channel, error) {
	var cfg barkConfig
	if err := decodeAttach(attach, &cfg); err != nil {
		return nil, err
	}
	return barkChannel{cfg: cfg}, nil
}

func (c barkChannel) Enabled() bool {
	return c.cfg.Enabled
}

func (c barkChannel) UseProxy() bool {
	return c.cfg.UseProxy
}

func (c barkChannel) Send(ctx *SendContext) error {
	config := c.cfg
	vars := ctx.Vars
	deviceKeys := parseDeviceKeys(config.DeviceKeys, vars)
	var endpoint string
	var err error
	if len(deviceKeys) > 0 {
		// 批量推送：POST JSON 到 /push，device_keys 为 key 数组，无需单设备 key
		endpoint = barkBatchEndpoint()
	} else {
		endpoint, err = barkEndpoint(config.Key)
		if err != nil {
			return err
		}
	}
	title, body := channelTitleBody(config.Title, config.Body, vars)
	payload := map[string]any{
		"title": title,
		"body":  body,
	}
	if len(deviceKeys) > 0 {
		payload["device_keys"] = deviceKeys
	}
	for _, p := range []struct{ key, val string }{
		{"subtitle", config.Subtitle},
		{"group", config.Group},
		{"level", config.Level},
		{"sound", config.Sound},
		{"icon", config.Icon},
		{"image", config.Image},
		{"url", config.URL},
		{"markdown", config.Markdown},
		{"copy", config.Copy},
		{"isArchive", config.IsArchive},
		{"ttl", config.TTL},
		{"volume", config.Volume},
		{"call", config.Call},
		{"autoCopy", config.AutoCopy},
		{"ciphertext", config.Ciphertext},
		{"action", config.Action},
		{"id", config.ID},
		{"delete", config.Delete},
	} {
		addIfPresent(payload, vars, p.key, p.val)
	}
	// badge 为数字参数，纯数字时按整数发送
	if badge := ReplaceVars(strings.TrimSpace(config.Badge), vars); badge != "" {
		if n, err := strconv.Atoi(badge); err == nil {
			payload["badge"] = n
		} else {
			payload["badge"] = badge
		}
	}
	log.Debug().Str("component", "Notify").Str("channel", "bark").Msg("sending bark notify")
	return postJSON(ctx.Client, endpoint, payload, 200)
}

// parseDeviceKeys 解析批量推送的 device_keys：按逗号/换行分隔，逐段做变量替换并去空白，跳过空段。
func parseDeviceKeys(raw string, vars map[string]string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var keys []string
	for _, part := range strings.FieldsFunc(ReplaceVars(raw, vars), func(r rune) bool { return r == ',' || r == '\n' }) {
		if k := strings.TrimSpace(part); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// barkEndpointDefault 构造 Bark 单设备推送端点（POST JSON 到 https://api.day.app/{key}）。
func barkEndpointDefault(key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("bark key is empty")
	}
	return fmt.Sprintf("https://api.day.app/%s", url.PathEscape(strings.TrimSpace(key))), nil
}

// barkBatchEndpointDefault 返回 Bark 批量推送端点（POST JSON 到 https://api.day.app/push）。
func barkBatchEndpointDefault() string {
	return "https://api.day.app/push"
}
