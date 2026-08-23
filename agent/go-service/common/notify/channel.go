package notify

import (
	"strings"

	"github.com/rs/zerolog/log"
)

// Channel 表示一种通知渠道。新增渠道只需：新文件内实现本接口，
// 并在该文件 init() 中调用一次 RegisterChannel 完成注册。
type Channel interface {
	// Name 返回渠道名（注册表键，日志中标识渠道）。
	Name() string
	// Enabled 返回该渠道在配置中是否启用；Send 调度据此跳过未启用渠道。
	Enabled(config Config) bool
	// Send 发送一条通知，失败返回 error（调度层统一记录日志）。
	Send(config Config, vars map[string]string) error
}

// channels 渠道注册表；channelOrder 保持注册顺序，保证遍历稳定。
// 仅在 init 阶段写入，运行期只读，无并发问题。
var (
	channels     = map[string]Channel{}
	channelOrder []string
)

// RegisterChannel 注册渠道；重复注册同名渠道时忽略并告警。
// 仅在包初始化（各渠道文件 init）阶段调用，运行期只读注册表。
func RegisterChannel(ch Channel) {
	if _, dup := channels[ch.Name()]; dup {
		log.Warn().Str("component", "Notify").Str("channel", ch.Name()).Msg("duplicate channel registration ignored")
		return
	}
	channels[ch.Name()] = ch
	channelOrder = append(channelOrder, ch.Name())
}

// addIfPresent 非空字段做变量替换后加入 payload（Bark/ServerChan 参数收集共用）。
func addIfPresent(payload map[string]any, vars map[string]string, key, value string) {
	if v := ReplaceVars(strings.TrimSpace(value), vars); v != "" {
		payload[key] = v
	}
}

// firstNonEmpty 返回第一个非空白字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// channelTitleBody 解析渠道级标题/正文：渠道配置优先（支持 {{title}}/{{body}}
// 引用通知项预填），留空回退 vars 预填内容。
func channelTitleBody(chTitle, chBody string, vars map[string]string) (string, string) {
	return ReplaceVars(firstNonEmpty(chTitle, vars["title"]), vars),
		ReplaceVars(firstNonEmpty(chBody, vars["body"]), vars)
}
