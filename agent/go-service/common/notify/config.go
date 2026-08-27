package notify

import (
	"encoding/json"
)

// GlobalConfig 是 __NotifyConfig 节点 attach 中「系统级」配置的解析结果：
// 失败通知开关与模板、全局代理。渠道级配置由各渠道
// 各自 Decode（见 Channel 接口），不在此结构体内。
//
// 注意：各字段均为 attach 的顶层 key——MaaFramework 的 override 对 attach
// 按顶层 key 合并，多个设置项各自写入一个顶层 key 才不会互相覆盖。
type GlobalConfig struct {
	// 失败通知
	OnFail    bool   `json:"on_fail"`
	FailTitle string `json:"fail_title"`
	FailBody  string `json:"fail_body"`

	// 全局代理（作用于所有渠道，由调度层统一解析并构造 client）
	UseProxy       bool   `json:"use_proxy"`        // 是否使用代理发送通知
	UseUpdateProxy bool   `json:"use_update_proxy"` // 复用「更新设置」里配置的代理（读取 install/config/mxu-*.json）
	ProxyURL       string `json:"proxy_url"`        // 手动代理地址（http://、https:// 或 socks5://）
}

// RuntimeConfig 是 __NotifyConfig 节点解析出的运行期配置：全局配置 + 原始 attach。
// attach 保留给各渠道 Decode 使用；按 taskID 缓存于 sink（失败事件无 Context 时使用）。
type RuntimeConfig struct {
	Global GlobalConfig
	Attach map[string]any
}

// Enabled 返回配置中是否有至少一个已注册且启用的渠道。
// 遍历注册表而非手写 || 链，新增渠道自动纳入判定。
func (r *RuntimeConfig) Enabled() bool {
	for _, name := range channelOrder {
		ch, err := channelFactories[name].Create(r.Attach)
		if err != nil {
			continue
		}
		if ch.Enabled() {
			return true
		}
	}
	return false
}

// decodeAttach 把整棵 attach 解码到目标结构体；未知键自动忽略（json 语义）。
// 各渠道 Decode 与 GlobalConfig 解析共用。
func decodeAttach(attach map[string]any, target any) error {
	data, err := json.Marshal(attach)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
