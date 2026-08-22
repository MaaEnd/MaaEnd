package notify

import (
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	configMu       sync.RWMutex
	configByTaskID = map[uint64]Config{} // taskID → 该任务的运行时配置（任务级 override 后）
	lastConfig     Config                // 兜底：任务创建即失败（无节点事件）时回退

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
	raw, err := ctx.GetNodeJSON(defaultConfigNode)
	if err != nil {
		log.Warn().Err(err).Str("component", "Notify").Str("node", defaultConfigNode).Msg("failed to read notify config node, notify disabled")
		setConfigByTask(detail.TaskID, Config{})
		return
	}
	config, err := ParseConfig(raw)
	if err != nil {
		log.Warn().Err(err).Str("component", "Notify").Str("node", defaultConfigNode).Msg("failed to parse notify config, notify disabled")
		setConfigByTask(detail.TaskID, Config{})
		return
	}
	setConfigByTask(detail.TaskID, config)
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
		config := getConfigByTask(detail.TaskID)
		if !config.Enabled() || !config.OnFail {
			return
		}
		if !shouldNotifyFail(detail.TaskID) {
			return
		}
		now := time.Now()
		vars := BuildVars(detail.Entry, i18n.T("notify.status.failed"), now)
		vars["title"] = config.FailTitle
		vars["body"] = config.FailBody
		log.Info().Str("component", "Notify").Uint64("task_id", detail.TaskID).Str("entry", detail.Entry).Msg("task failed, sending notify")
		go Send(config, vars)
		notifiedTaskIDs.Delete(detail.TaskID)
	case maa.EventStatusSucceeded:
		notifiedTaskIDs.Delete(detail.TaskID)
		configMu.Lock()
		delete(configByTaskID, detail.TaskID)
		configMu.Unlock()
	}
}

func setConfigByTask(taskID uint64, config Config) {
	configMu.Lock()
	defer configMu.Unlock()
	if config.Enabled() {
		lastConfig = config
	}
	configByTaskID[taskID] = config
}

// getConfigByTask 取指定任务缓存的配置；任务创建即失败（无节点事件）时回退全局最近配置。
func getConfigByTask(taskID uint64) Config {
	configMu.RLock()
	defer configMu.RUnlock()
	if config, ok := configByTaskID[taskID]; ok {
		return config
	}
	return lastConfig
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
