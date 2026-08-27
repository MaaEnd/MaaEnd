package notify

import (
	"encoding/json"
	"strings"
)

// GlobalConfig 是 __NotifyConfig 节点 attach 中「系统级」配置的解析结果：
// 失败通知开关、通知项总开关、标题/正文模板、全局代理。渠道级配置由各渠道
// 各自 Decode（见 Channel 接口），不在此结构体内。
//
// 注意：各字段均为 attach 的顶层 key——MaaFramework 的 override 对 attach
// 按顶层 key 合并，多个设置项各自写入一个顶层 key 才不会互相覆盖。
type GlobalConfig struct {
	// 失败通知
	OnFail    bool   `json:"on_fail"`
	FailTitle string `json:"fail_title"`
	FailBody  string `json:"fail_body"`

	// 自定义通知内容（经 attach 顶层键合并写入）：由调用节点 attach 直接编写。
	// 第三方节点 attach 编写的支持以 "$" 开头的 i18n key（查不到翻译则回退去掉 $ 的 key），
	// 见 resolveTitleBody。
	TaskTitle string `json:"task_title"` // 标题模板：第三方 attach 的普通文本或 $ i18n key
	TaskBody  string `json:"task_body"`  // 正文模板：第三方 attach 的普通文本或 $ i18n key

	// AllowTaskNotify 设置页总开关（收纳开关）：是否允许任务/节点通过 NotifySendAction 发送自定义通知。
	// nil=未配置（默认允许，不破坏旧行为）；设置页关闭时写入 false，同时收起所有通知项分项开关。
	AllowTaskNotify *bool `json:"allow_task_notify"`

	// TaskNotifyKey 通知项标识（调用节点 attach 写入，如 "monthly_card"）：配合设置页的
	// 通知项分项开关（attach 顶层键 "task_notify.<id>"）决定该通知项是否被用户关闭。
	TaskNotifyKey string `json:"task_notify_key"`

	// TaskNotifyToggles 通知项开关表：键为通知项 ID，值为设置页配置的开关。
	// 由 ParseConfig 从 attach 顶层 "task_notify.<id>" 键收集；未配置的通知项默认启用。
	TaskNotifyToggles map[string]bool `json:"-"`

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

// ContainsContent 判断调用节点是否携带了内容类配置（标题/正文/通知项 ID），
// 供 NotifySendAction 判断是否需要对全局内容做本地覆盖。
func ContainsContent(attach map[string]any) bool {
	for _, key := range []string{"task_title", "task_body", "task_notify_key"} {
		if v, ok := attach[key]; ok && v != "" {
			return true
		}
	}
	return false
}

// MergeAttach 合并全局 attach 与调用节点 attach：渠道字段以全局为准，
// 内容字段（task_title/task_body/task_notify_key）调用节点优先、回退全局。
func MergeAttach(global, local map[string]any) map[string]any {
	merged := make(map[string]any, len(global)+3)
	for k, v := range global {
		merged[k] = v
	}
	for _, key := range []string{"task_title", "task_body", "task_notify_key"} {
		if v, ok := local[key]; ok && v != "" {
			merged[key] = v
		}
	}
	return merged
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

// parseTaskNotifyToggles 从 attach 顶层键中提取 "task_notify.<id>" 形式的通知项开关。
// attach 合并按顶层 key 进行，每个通知项独立一个键，互不覆盖。
func parseTaskNotifyToggles(attach map[string]any) map[string]bool {
	toggles := make(map[string]bool)
	for key, value := range attach {
		id, ok := strings.CutPrefix(key, "task_notify.")
		if !ok || id == "" {
			continue
		}
		if v, ok := value.(bool); ok {
			toggles[id] = v
		}
	}
	return toggles
}
