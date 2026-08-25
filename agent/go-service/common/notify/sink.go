package notify

import (
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	configMu       sync.RWMutex
	configByTaskID = map[uint64]RuntimeConfig{} // taskID → 该任务的运行时配置（任务级 override 后）

	// controllerStartTime 记录每个 controller 上首次任务启动的时间，用作 {{duration}} 起点。
	// 同一 controller 同时只会跑一个实例，不会串。
	controllerStartTime = map[string]time.Time{}

	// notifiedTaskIDs 记录已发送失败通知的 task_id。框架可能对同一任务失败事件
	// 重复广播，按 task_id 去重；LoadOrStore 原子，并行任务下也只发送一次。
	notifiedTaskIDs sync.Map
)

// ConfigSink 在任务执行期间读取 __NotifyConfig 节点（含任务级 override 注入的用户配置）
// 并缓存。并行任务各自缓存到自己的 task_id 下，互不覆盖；任务失败时任务链中断、
// 事件回调没有 Context，通知配置由本次缓存提供。
type ConfigSink struct{}

var _ maa.ContextEventSink = &ConfigSink{}

func (s *ConfigSink) OnNodePipelineNode(ctx *maa.Context, event maa.EventStatus, detail maa.NodePipelineNodeDetail) {
	if event != maa.EventStatusStarting {
		return
	}
	// 同一 taskID 下所有节点配置一致，首节点缓存后跳过后续节点
	if _, ok := getConfigByTask(detail.TaskID); ok {
		return
	}
	raw, err := ctx.GetNodeJSON(defaultConfigNode)
	if err != nil {
		log.Warn().Err(err).Str("component", "Notify").Str("node", defaultConfigNode).Msg("failed to read notify config node, notify disabled")
		return
	}
	runtime, err := ParseConfig(raw)
	if err != nil {
		log.Warn().Err(err).Str("component", "Notify").Str("node", defaultConfigNode).Msg("failed to parse notify config, notify disabled")
		return
	}
	setConfigByTask(detail.TaskID, runtime)
	log.Debug().Str("component", "Notify").Uint64("task_id", detail.TaskID).Msg("notify config cached for task")
}

func (s *ConfigSink) OnNodeRecognitionNode(ctx *maa.Context, event maa.EventStatus, detail maa.NodeRecognitionNodeDetail) {
}
func (s *ConfigSink) OnNodeActionNode(ctx *maa.Context, event maa.EventStatus, detail maa.NodeActionNodeDetail) {
}
func (s *ConfigSink) OnNodeNextList(ctx *maa.Context, event maa.EventStatus, detail maa.NodeNextListDetail) {
}
func (s *ConfigSink) OnNodeRecognition(ctx *maa.Context, event maa.EventStatus, detail maa.NodeRecognitionDetail) {
}
func (s *ConfigSink) OnNodeAction(ctx *maa.Context, event maa.EventStatus, detail maa.NodeActionDetail) {
}

// Sink 监听任务失败事件，按该任务的缓存配置发送失败通知（任何任务失败都会通知）。
type Sink struct{}

var _ maa.TaskerEventSink = &Sink{}

func (s *Sink) OnTaskerTask(_ *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	switch event {
	case maa.EventStatusFailed:
		runtime, ok := getConfigByTask(detail.TaskID)
		if !ok {
			// 已发送过失败通知的任务被重复广播（配置已清理）：静默，避免刷 warn
			if _, notified := notifiedTaskIDs.Load(detail.TaskID); notified {
				return
			}
			log.Warn().Str("component", "Notify").Uint64("task_id", detail.TaskID).Str("entry", detail.Entry).Msg("no cached config for task; failure notification skipped")
			return
		}
		enabled := runtime.Enabled()
		if enabled && runtime.Global.OnFail && shouldNotifyFail(detail.TaskID) {
			now := time.Now()
			// 失败通知的 {{duration}} 同样用实例总耗时（controllerStartTime），
			// 与 NotifyTask 一致：从实例首个任务启动到失败时刻，而非失败任务自身耗时
			vars := BuildVars(resolveTaskName(detail.Entry), i18n.T("notify.status.failed"), now, getControllerStartTime())
			vars["title"] = runtime.Global.FailTitle
			vars["body"] = runtime.Global.FailBody
			log.Info().Str("component", "Notify").Uint64("task_id", detail.TaskID).Str("entry", detail.Entry).Msg("task failed, sending notify")
			go Send(runtime, vars)
		} else if !enabled && runtime.Global.OnFail && shouldNotifyFail(detail.TaskID) {
			// on_fail 打开但没有启用任何渠道：属于无效配置，提示一次（按 task 去重）
			log.Warn().Str("component", "Notify").Uint64("task_id", detail.TaskID).Str("entry", detail.Entry).Msg("on_fail enabled but no channel enabled; failure notification will not be delivered")
		}
		// 无论是否发送，均清理本次任务缓存
		deleteConfigByTask(detail.TaskID)
	case maa.EventStatusSucceeded:
		deleteConfigByTask(detail.TaskID)
	}
}

func setConfigByTask(taskID uint64, runtime RuntimeConfig) {
	configMu.Lock()
	defer configMu.Unlock()
	configByTaskID[taskID] = runtime
	now := time.Now()
	if name := pienv.ControllerName(); name != "" {
		if _, ok := controllerStartTime[name]; !ok {
			controllerStartTime[name] = now
		}
	}
}

// getConfigByTask 取指定任务缓存的配置；未缓存过（任务在节点事件前失败）返回 false。
func getConfigByTask(taskID uint64) (RuntimeConfig, bool) {
	configMu.RLock()
	defer configMu.RUnlock()
	runtime, ok := configByTaskID[taskID]
	return runtime, ok
}

func deleteConfigByTask(taskID uint64) {
	configMu.Lock()
	delete(configByTaskID, taskID)
	configMu.Unlock()
}

func getControllerStartTime() time.Time {
	configMu.RLock()
	defer configMu.RUnlock()
	return controllerStartTime[pienv.ControllerName()]
}

// shouldNotifyFail 判断该 task_id 是否应发送失败通知：同一 task_id 只发送一次，
// 用于抵消框架对同一任务失败事件的重复广播。
func shouldNotifyFail(taskID uint64) bool {
	_, loaded := notifiedTaskIDs.LoadOrStore(taskID, struct{}{})
	return !loaded
}

// splitList 按分隔符集合分割列表，逐段去首尾空白、跳过空段；空串返回 nil。
func splitList(raw string, seps ...rune) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		for _, s := range seps {
			if r == s {
				return true
			}
		}
		return false
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
