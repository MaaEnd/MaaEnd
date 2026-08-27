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

const failDetectDebounce = 1500 * time.Millisecond

var (
	configMu       sync.RWMutex
	configByTaskID = map[uint64]RuntimeConfig{} // taskID → 该任务的运行时配置（任务级 override 后）
	taskEntryByID  = map[uint64]string{}        // taskID → entry（来自 Tasker.Task.Starting，node 事件路径补全 {{task_name}}）

	// controllerStartTime 记录每个 controller 上首次任务启动的时间，用作 {{duration}} 起点。
	// 同一 controller 同时只会跑一个实例，不会串。
	controllerStartTime = map[string]time.Time{}

	// notifiedTaskIDs 记录已发送失败通知的 task_id。框架可能对同一任务失败事件
	// 重复广播，按 task_id 去重；LoadOrStore 原子，并行任务下也只发送一次。
	notifiedTaskIDs sync.Map

	// failTimerMu/failTimers/failTimerEpoch：任务终态失败的去抖动计时器。
	// MXU 在任务结束瞬间同步断开 Agent 会话，agent 收不到 Tasker.Task.Failed，
	// 只能由"最后一个不可恢复节点失败事件之后静默"推断失败。
	failTimerMu    sync.Mutex
	failTimers     = map[uint64]*time.Timer{}
	failTimerEpoch = map[uint64]uint64{}
)

// ConfigSink 在任务执行期间读取 __NotifyConfig 节点（含任务级 override 注入的用户配置）
// 并缓存。并行任务各自缓存到自己的 task_id 下，互不覆盖；任务失败时任务链中断、
// 事件回调没有 Context，通知配置由本次缓存提供。
type ConfigSink struct{}

var _ maa.ContextEventSink = &ConfigSink{}

func (s *ConfigSink) OnNodePipelineNode(ctx *maa.Context, event maa.EventStatus, detail maa.NodePipelineNodeDetail) {
	switch event {
	case maa.EventStatusStarting:
		cancelFailTimer(detail.TaskID)
		// 同一 taskID 下所有节点配置一致，首节点缓存后跳过后续节点
		if _, ok := getConfigByTask(detail.TaskID); ok {
			return
		}
		// 首节点同时缓存 entry：任务的根 pipeline 节点名 = 任务入口名，
		// 来自 node 事件流（可靠送达），供终态失败路径补全 {{task_name}}。
		setTaskEntry(detail.TaskID, detail.Name)
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
	case maa.EventStatusSucceeded:
		// 该 pipeline 节点成功 = 任务仍在继续，撤销此前不可恢复失败的去抖判定
		cancelFailTimer(detail.TaskID)
	case maa.EventStatusFailed:
		// 不可恢复的 pipeline 节点失败：任务可能已终态失败，进入去抖判定。
		// 若去抖窗口内该任务没有后续任何节点事件（任务链已断、无 fallback 继续跑），
		// 即判定失败并发送通知；否则由后续事件撤销。
		armFailTimer(detail.TaskID)
	}
}

func (s *ConfigSink) OnNodeRecognitionNode(ctx *maa.Context, event maa.EventStatus, detail maa.NodeRecognitionNodeDetail) {
	cancelFailTimer(detail.TaskID)
}
func (s *ConfigSink) OnNodeActionNode(ctx *maa.Context, event maa.EventStatus, detail maa.NodeActionNodeDetail) {
	cancelFailTimer(detail.TaskID)
}
func (s *ConfigSink) OnNodeNextList(ctx *maa.Context, event maa.EventStatus, detail maa.NodeNextListDetail) {
	// NextList.Failed 与 PipelineNode.Failed 一样属"不可恢复节点失败"，
	// 同样作为终态失败候选进入去抖；其余状态说明任务仍在继续，撤销判定
	if event == maa.EventStatusFailed {
		armFailTimer(detail.TaskID)
	} else {
		cancelFailTimer(detail.TaskID)
	}
}
func (s *ConfigSink) OnNodeRecognition(ctx *maa.Context, event maa.EventStatus, detail maa.NodeRecognitionDetail) {
	cancelFailTimer(detail.TaskID)
}
func (s *ConfigSink) OnNodeAction(ctx *maa.Context, event maa.EventStatus, detail maa.NodeActionDetail) {
	cancelFailTimer(detail.TaskID)
}

// Sink 监听任务失败事件，按该任务的缓存配置发送失败通知（任何任务失败都会通知）。
type Sink struct{}

var _ maa.TaskerEventSink = &Sink{}

func (s *Sink) OnTaskerTask(_ *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	switch event {
	case maa.EventStatusStarting:
		// 缓存 taskID → entry：终态失败走节点事件推断路径时，node 事件里没有
		// entry，由此补全 {{task_name}}。Tasker.Task.Starting 是 agent 唯一能
		// 可靠收到的 Tasker 级事件（任务一结束会话即被 MXU 断开）。
		setTaskEntry(detail.TaskID, detail.Entry)
	case maa.EventStatusFailed:
		runtime, ok := getConfigByTask(detail.TaskID)
		if !ok {
			// 已发送过失败通知的任务被重复广播（配置已清理）：静默，避免刷 warn
			if _, notified := notifiedTaskIDs.Load(detail.TaskID); notified {
				return
			}
			// 任务在首个节点事件（ConfigSink 缓存时机）之前失败，配置未缓存。
			// 框架限制：失败事件（TaskerEventSink）无 Context，无法读 override 后的用户配置
			// （on_fail / 渠道开关均在 Context 层），Tasker.GetResource 只能读静态默认定义，
			// 故无法在此发出失败通知；仅记录日志说明边界。
			log.Warn().Str("component", "Notify").Uint64("task_id", detail.TaskID).Str("entry", detail.Entry).Msg("task failed before any node event; no cached config (framework limitation), failure notification skipped")
			return
		}
		enabled := runtime.Enabled()
		if enabled && runtime.Global.OnFail && shouldNotifyFail(detail.TaskID) {
			now := time.Now()
			// 失败通知的 {{duration}} 用实例总耗时（controllerStartTime）：
			// 从实例首个任务启动到失败时刻，而非失败任务自身耗时
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
		deleteTaskEntry(detail.TaskID)
	case maa.EventStatusSucceeded:
		deleteConfigByTask(detail.TaskID)
		deleteTaskEntry(detail.TaskID)
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

func setTaskEntry(taskID uint64, entry string) {
	configMu.Lock()
	taskEntryByID[taskID] = entry
	configMu.Unlock()
}

func getTaskEntry(taskID uint64) string {
	configMu.RLock()
	entry := taskEntryByID[taskID]
	configMu.RUnlock()
	return entry
}

func deleteTaskEntry(taskID uint64) {
	configMu.Lock()
	delete(taskEntryByID, taskID)
	configMu.Unlock()
}

// armFailTimer 为任务启动终态失败去抖计时：去抖窗口内该任务没有后续任何节点
// 事件（任务链已断）即判定失败并发送通知。epoch 递增保证旧计时器回调不会误删
// 新计时器。
func armFailTimer(taskID uint64) {
	failTimerMu.Lock()
	defer failTimerMu.Unlock()
	failTimerEpoch[taskID]++
	epoch := failTimerEpoch[taskID]
	if old, ok := failTimers[taskID]; ok {
		old.Stop()
	}
	failTimers[taskID] = time.AfterFunc(failDetectDebounce, func() {
		failTimerMu.Lock()
		if failTimerEpoch[taskID] != epoch {
			// 已被后续 arm/cancel 取代：任务仍在跑或配置路径已处理，静默退出
			failTimerMu.Unlock()
			return
		}
		delete(failTimers, taskID)
		delete(failTimerEpoch, taskID)
		failTimerMu.Unlock()
		dispatchFailure(taskID)
	})
}

// cancelFailTimer 撤销任务的终态失败去抖判定（后续节点事件表明任务仍在继续）。
func cancelFailTimer(taskID uint64) {
	failTimerMu.Lock()
	if t, ok := failTimers[taskID]; ok {
		t.Stop()
		delete(failTimers, taskID)
		delete(failTimerEpoch, taskID)
	}
	failTimerMu.Unlock()
}

// dispatchFailure 按任务缓存配置发送失败通知（节点事件推断路径）。
// 与 Sink 的 Tasker.Task.Failed 路径共用 shouldNotifyFail 原子去重，双路径不会双发；
// 配置已被终态事件路径清理时静默返回（说明那条路径已处理）。
func dispatchFailure(taskID uint64) {
	runtime, ok := getConfigByTask(taskID)
	if !ok {
		return
	}
	entry := getTaskEntry(taskID)
	enabled := runtime.Enabled()
	if enabled && runtime.Global.OnFail && shouldNotifyFail(taskID) {
		now := time.Now()
		// 失败通知的 {{duration}} 用实例总耗时（controllerStartTime），
		// 与 Tasker 终态事件路径语义一致
		vars := BuildVars(resolveTaskName(entry), i18n.T("notify.status.failed"), now, getControllerStartTime())
		vars["title"] = runtime.Global.FailTitle
		vars["body"] = runtime.Global.FailBody
		log.Info().Str("component", "Notify").Uint64("task_id", taskID).Str("entry", entry).Msg("task failure inferred from node events, sending notify")
		go Send(runtime, vars)
	} else if !enabled && runtime.Global.OnFail && shouldNotifyFail(taskID) {
		log.Warn().Str("component", "Notify").Uint64("task_id", taskID).Str("entry", entry).Msg("on_fail enabled but no channel enabled; failure notification will not be delivered")
	}
	deleteConfigByTask(taskID)
	deleteTaskEntry(taskID)
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
