package notify

import (
	"encoding/json"
	"maps"
	"net/http"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/rs/zerolog/log"
)

const defaultConfigNode = "__NotifyConfig"

// ParseConfig 从节点 JSON（含 attach）解析运行期配置：全局配置 + 原始 attach（供渠道 Decode）。
func ParseConfig(nodeJSON string) (RuntimeConfig, error) {
	var node struct {
		Attach json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(nodeJSON), &node); err != nil {
		return RuntimeConfig{}, err
	}
	var runtime RuntimeConfig
	if len(node.Attach) == 0 {
		return runtime, nil
	}
	var attach map[string]any
	if err := json.Unmarshal(node.Attach, &attach); err != nil {
		return RuntimeConfig{}, err
	}
	if err := decodeAttach(attach, &runtime.Global); err != nil {
		return RuntimeConfig{}, err
	}
	runtime.Attach = attach
	return runtime, nil
}

// Send 遍历注册表中所有已启用的渠道发送通知，任一渠道发送成功即返回 true；
// 全部失败返回 false（不影响调用方流程，仅记录日志）。
// 标题/正文优先级：渠道配置（如 channel_discord_title，支持 {{title}}/{{body}} 引用预填内容）> 预填内容（vars["title"]/["body"]）> 默认标题。
// Webhook 不读取标题/正文，可在请求体里用 {{title}}/{{body}} 引用本次通知的标题/正文。
//
// 代理统一在此解析：配置了全局代理（use_proxy）时构造代理 client，
// 同一个 client 传给所有渠道（渠道自身零代理代码）。
func Send(runtime RuntimeConfig, vars map[string]string) bool {
	if !runtime.Enabled() {
		log.Debug().Str("component", "Notify").Msg("no enabled channel, skip notify")
		return true
	}

	// 全局代理：主开关 use_proxy 开启时，解析一次代理地址并构造代理 client，
	// 具体渠道是否走代理由其 UseProxy() 决定（每渠道可独立开关）。
	var proxyClientInstance *http.Client
	if runtime.Global.UseProxy {
		proxyURL, err := resolveProxy(runtime.Global.UseUpdateProxy, runtime.Global.ProxyURL)
		if err != nil {
			log.Error().Err(err).Str("component", "Notify").Msg("notify proxy config error, skip notify")
			return false
		}
		if proxyClientInstance, err = proxyClient(proxyURL); err != nil {
			log.Error().Err(err).Str("component", "Notify").Msg("notify proxy config error, skip notify")
			return false
		}
	}

	// 复制 vars：下方会把解析后的 title/body 写回供渠道引用，
	// clone 避免隐式修改调用方 map（sink 中 go Send 与调用方并发使用同一 map）。
	vars = maps.Clone(vars)
	if vars == nil {
		vars = map[string]string{}
	}

	// 预填内容：此时 vars 尚无 title/body，模板里写 {{title}}/{{body}} 不会被替换（天然无效）
	prefillTitle := ReplaceVars(firstNonEmpty(vars["title"]), vars)
	prefillBody := ReplaceVars(firstNonEmpty(vars["body"]), vars)
	// 写回变量，供渠道标题/正文与 Webhook 请求体 {{title}}/{{body}} 引用
	vars["title"] = firstNonEmpty(prefillTitle, i18n.T("notify.default_title"))
	vars["body"] = firstNonEmpty(prefillBody, i18n.T("notify.default_body"))

	sent := false
	for _, name := range channelOrder {
		factory := channelFactories[name]
		ch, err := factory.Create(runtime.Attach)
		if err != nil {
			log.Warn().Err(err).Str("component", "Notify").Str("channel", name).Msg("channel config parse failed, skipped")
			continue
		}
		if !ch.Enabled() {
			continue
		}
		// 主开关开启且该渠道声明走代理 → 用代理 client，否则直连
		client := httpClient
		if proxyClientInstance != nil && ch.UseProxy() {
			client = proxyClientInstance
		}
		ctx := &SendContext{Client: client, Vars: vars}
		if err := ch.Send(ctx); err != nil {
			log.Error().Err(err).Str("component", "Notify").Str("channel", name).Msg("notify failed")
		} else {
			sent = true
		}
	}
	return sent
}
