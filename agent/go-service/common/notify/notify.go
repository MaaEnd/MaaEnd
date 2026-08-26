package notify

import (
	"encoding/json"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
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
	runtime.Global.TaskNotifyToggles = parseTaskNotifyToggles(attach)
	return runtime, nil
}

// Send 遍历注册表中所有已启用的渠道发送通知，任一渠道发送成功即返回 true；
// 全部失败返回 false（不影响调用方流程，仅记录日志）。
// 标题/正文优先级：渠道配置（如 discord_title，支持 {{title}}/{{body}} 引用通知项预填内容）> 通知项模板（vars["title"]/["body"]）> 默认标题。
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

	// 通知项预填内容：此时 vars 尚无 title/body，通知项模板里写 {{title}}/{{body}} 不会被替换（天然无效）
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

// NotifySendAction 供 Pipeline 手动触发通知：渠道配置从 __NotifyConfig 读取，
// 标题/正文支持两种来源——NotifyTask 由设置页 option 注入全局节点（玩家 UI，普通文本），
// 其他任务可在调用节点 attach 直接编写（本地优先）；仅第三方节点 attach 提供的
// 标题/正文支持以 "$" 开头的 i18n key（查不到翻译则显示去掉 $ 的 key）。
// 通知失败不会导致 Pipeline 节点失败。
type NotifySendAction struct{}

var _ maa.CustomActionRunner = &NotifySendAction{}

func (a *NotifySendAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 渠道配置与默认内容：__NotifyConfig（设置页全局 option 注入）
	raw, err := ctx.GetNodeJSON(defaultConfigNode)
	if err != nil {
		log.Error().Err(err).Str("component", "NotifySendAction").Str("node", defaultConfigNode).Msg("failed to get node json")
		return true
	}
	runtime, err := ParseConfig(raw)
	if err != nil {
		log.Error().Err(err).Str("component", "NotifySendAction").Str("node", defaultConfigNode).Msg("failed to parse notify config")
		return true
	}

	// 调用节点自定义内容（attach 本地优先，如月卡到期提醒等业务通知）。
	// localAttach 保留调用节点的原始 attach：后续仅对「第三方节点 attach 提供的
	// task_title/task_body」做 $ i18n 解析，玩家 UI（__NotifyConfig）注入的普通文本不解析。
	var localAttach map[string]any
	if raw, err := ctx.GetNodeJSON(arg.CurrentTaskName); err == nil {
		if local, err := parseAttach(raw); err == nil {
			localAttach = local
			if ContainsContent(local) {
				runtime.Attach = MergeAttach(runtime.Attach, local)
				_ = decodeAttach(runtime.Attach, &runtime.Global)
				runtime.Global.TaskNotifyToggles = parseTaskNotifyToggles(runtime.Attach)
			}
		}
	}

	// 设置页总开关：关闭时跳过（未配置视为允许，不破坏旧行为）
	if runtime.Global.AllowTaskNotify != nil && !*runtime.Global.AllowTaskNotify {
		log.Debug().Str("component", "NotifySendAction").Msg("task notify disabled by setting, skip")
		return true
	}

	// 通知项级开关：节点声明了 task_notify_key 时，按设置页分项开关判断，未配置默认启用
	if taskNotifySkipped(runtime.Global) {
		log.Debug().Str("component", "NotifySendAction").Str("item", runtime.Global.TaskNotifyKey).Msg("notify item disabled by setting, skip")
		return true
	}

	// 任务名优先取入口名（TaskID 反查）并解析为显示名；反查取不到（任务执行初期
	// node_ids 为空时 maa-framework-go 不返回 entry，见 resolveActionTaskName 注释）
	// 则回退用当前节点名解析（NotifyTask 节点名即入口名，可正常解析）。
	taskName := resolveActionTaskName(
		arg.TaskID,
		arg.CurrentTaskName,
		func(id int64) string {
			td, err := ctx.GetTasker().GetTaskDetail(id)
			if err != nil || td.Entry == "" {
				return ""
			}
			return td.Entry
		},
	)

	vars := BuildVars(taskName, "", time.Now(), getControllerStartTime())
	vars["title"], vars["body"] = resolveTitleBody(
		runtime.Global.TaskTitle,
		runtime.Global.TaskBody,
		localAttach,
		vars,
	)
	Send(runtime, vars)
	// 通知发送失败不影响游戏流程，始终返回成功
	return true
}

// parseAttach 解析节点 JSON 的 attach 为 map（无 attach 返回空 map）。
func parseAttach(nodeJSON string) (map[string]any, error) {
	var node struct {
		Attach json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(nodeJSON), &node); err != nil {
		return nil, err
	}
	if len(node.Attach) == 0 {
		return map[string]any{}, nil
	}
	var attach map[string]any
	if err := json.Unmarshal(node.Attach, &attach); err != nil {
		return nil, err
	}
	return attach, nil
}

// resolveActionTaskName 解析通知动作的任务显示名：
//   - 优先用 GetTaskDetail 反查的任务入口名解析；
//   - 反查取不到（任务执行初期 node_ids 为空时，maa-framework-go 的
//     MaaTaskerGetTaskDetail 绑定在 size==0 时直接返回空 Entry，不读响应里的入口名）
//     则退回到用当前节点名解析（NotifyTask 的节点名即入口名，可解析为显示名）；
//   - 两者都查不到翻译时回退原样（resolveTaskName 的兜底行为）。
func resolveActionTaskName(taskID int64, currentTaskName string, getEntry func(int64) string) string {
	if taskID > 0 {
		if entry := getEntry(taskID); entry != "" {
			return resolveTaskName(entry)
		}
	}
	return resolveTaskName(currentTaskName)
}

// taskNotifySkipped 判断通知项级开关是否跳过发送：
// 未声明 task_notify_key 不判断；声明了但设置页未配置该通知项开关时默认启用。
func taskNotifySkipped(global GlobalConfig) bool {
	key := global.TaskNotifyKey
	if key == "" {
		return false
	}
	enabled, ok := global.TaskNotifyToggles[key]
	return ok && !enabled
}

// resolveTitleBody 按来源解析标题/正文：
//   - 第三方节点 attach 提供的 task_title/task_body 支持 "$" i18n（走 resolveNotifyText）；
//   - 设置页（玩家 UI）注入 __NotifyConfig 的全局 task_title/task_body 为普通文本，原样返回。
//
// mergedTitle/mergedBody 是 MergeAttach 后的值（本地优先）；local 是调用节点原始 attach。
// 仅当 local 显式提供非空字段时，才对该字段做 $ 解析；否则沿用全局值（普通文本）。
func resolveTitleBody(mergedTitle, mergedBody string, local map[string]any, vars map[string]string) (string, string) {
	title, body := mergedTitle, mergedBody
	if v, ok := local["task_title"].(string); ok && v != "" {
		title = resolveNotifyText(v, vars)
	}
	if v, ok := local["task_body"].(string); ok && v != "" {
		body = resolveNotifyText(v, vars)
	}
	return title, body
}

// resolveNotifyText 解析标题/正文：以 "$" 开头的文本视为 i18n key（与 MXU 前端约定一致），
// 查到翻译时用翻译（再做变量替换），查不到或非 "$" 开头时按原文处理。
// 回退语义与 i18n.T 一致：查不到翻译显示去掉 $ 的 key 本身。
func resolveNotifyText(text string, vars map[string]string) string {
	if strings.HasPrefix(text, "$") {
		key := strings.TrimPrefix(text, "$")
		if translated := i18n.T(key); translated != key {
			return ReplaceVars(translated, vars)
		}
		return ReplaceVars(key, vars)
	}
	return ReplaceVars(text, vars)
}
