//go:build linux

package ziplineimport

import (
	"encoding/json"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// actionParam 全部从节点 attach 读取；缺省用下方常量。
//
// Linux 默认写死国服森空岛地图；国际服 MITM 不支持。timeout / template_ids /
// firefox / proxy_port 仍可从 attach 透传（便于本地调试），但任务界面不暴露。
//
// clear_login 在 Linux 无语义：每次导入都用一次性临时 profile。
type actionParam struct {
	URL         string   `json:"url"`
	Timeout     int64    `json:"timeout"`
	TemplateIDs []string `json:"template_ids"`
	Firefox     string   `json:"firefox"`
	ProxyPort   int      `json:"proxy_port"`
}

const (
	defaultFirefox = "firefox"
	defaultTimeout = 3 * 60 * 1000 // 与 cpp kDefaultTimeoutMs 一致
	defaultMapURL  = "https://game.skland.com/map/endfield"
)

func loadParam(ctx *maa.Context, nodeName string) actionParam {
	p := actionParam{
		URL:     defaultMapURL,
		Firefox: defaultFirefox,
		Timeout: defaultTimeout,
	}
	if ctx == nil || nodeName == "" {
		return p
	}

	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil || raw == "" {
		return p
	}

	var node struct {
		Attach json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		log.Warn().Err(err).Str("component", componentName).Str("node", nodeName).
			Msg("zipline import: failed to unmarshal node json")
		return p
	}
	if len(node.Attach) == 0 || string(node.Attach) == "null" {
		return p
	}

	// 缺省字段保持不变；仅覆盖 attach 里出现的键。
	if err := json.Unmarshal(node.Attach, &p); err != nil {
		log.Warn().Err(err).Str("component", componentName).Str("node", nodeName).
			Msg("zipline import: failed to unmarshal attach")
		return p
	}
	if p.URL == "" {
		p.URL = defaultMapURL
	}
	if p.Firefox == "" {
		p.Firefox = defaultFirefox
	}
	if p.Timeout <= 0 {
		p.Timeout = defaultTimeout
	}
	return p
}

// isGlobalRegionHost 判断页面主机是否属于国际服 SKPORT（skport.com 及其子域）。
// Linux 的 MITM 解密白名单只含国服 API 域名，国际服流量经 PAC 直连、代理全程不可见，
// 与其让用户干等满超时，不如在入口处快速失败。
func isGlobalRegionHost(host string) bool {
	host = strings.ToLower(host)
	return host == "skport.com" || strings.HasSuffix(host, ".skport.com")
}
