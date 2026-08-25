package notify

import (
	"net/http"

	"github.com/rs/zerolog/log"
)

// Channel 表示一个已配置的通知渠道实例。由各渠道工厂（ChannelFactory）按
// attach 创建，配置是实例的类型化字段（如 discordChannel.cfg），方法内
// 直接读写，无需类型断言。
type Channel interface {
	// Enabled 返回该渠道在当前配置下是否启用；Send 调度据此跳过未启用渠道。
	Enabled() bool
	// UseProxy 返回该渠道是否走全局代理（仅当全局主开关 use_proxy 开启时生效）。
	UseProxy() bool
	// Send 发送一条通知，失败返回 error（调度层统一记录日志）。
	// client 由调度统一构造（含全局代理），渠道直接使用，不得自行实现代理。
	Send(ctx *SendContext) error
}

// ChannelFactory 渠道工厂：各渠道文件用零值实例在 init() 中注册，
// 调度层每次发送前调用 Create 从 attach 解析配置并创建渠道实例。
type ChannelFactory interface {
	// Name 返回渠道名（注册表键，日志中标识渠道）。
	Name() string
	// Create 从节点 attach（整棵顶层键 map）解析本渠道配置并创建实例。
	// 只解析本渠道关心的键（json tag 对应），未知键自动忽略；解析失败返回 error。
	Create(attach map[string]any) (Channel, error)
}

// SendContext 渠道发送上下文：调度层在调用 Send 前填充。
// 不含渠道配置——配置是渠道实例自身的类型化字段。
type SendContext struct {
	// Client 统一 HTTP 客户端（全局代理已配置时含代理 transport），渠道直接使用。
	Client *http.Client
	// Vars 模板变量：已含解析后的 title/body（通知项预填内容），渠道可直接引用。
	Vars map[string]string
}

// channelFactories 渠道工厂注册表；channelOrder 保持注册顺序，保证遍历稳定。
// 仅在 init 阶段写入，运行期只读，无并发问题。
var (
	channelFactories = map[string]ChannelFactory{}
	channelOrder     []string
)

// RegisterChannel 注册渠道工厂；重复注册同名渠道时忽略并告警。
// 仅在包初始化（各渠道文件 init）阶段调用，运行期只读注册表。
func RegisterChannel(factory ChannelFactory) {
	if _, dup := channelFactories[factory.Name()]; dup {
		log.Warn().Str("component", "Notify").Str("channel", factory.Name()).Msg("duplicate channel registration ignored")
		return
	}
	channelFactories[factory.Name()] = factory
	channelOrder = append(channelOrder, factory.Name())
}
