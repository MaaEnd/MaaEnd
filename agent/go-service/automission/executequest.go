package automission

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const executeComponent = "AutoMissionExecuteQuest"

var _ maa.CustomActionRunner = &ExecuteQuestAction{}

// trackingNodes 把解包追踪类型对应到占位 Pipeline 节点。
var trackingNodes = map[string]string{
	"MissionAreaTrackingInfo": "AutoMissionMissionArea",
	"NpcProxyTrackingInfo":    "AutoMissionNpcProxy",
	"PosTrackingInfo":         "AutoMissionPos",
	"EntityTrackingInfo":      "AutoMissionEntity",
	"JumpToUITrackingInfo":    "AutoMissionJumpToUI",
	"SnsTrackingInfo":         "AutoMissionSns",
}

// ExecuteQuestAction 读取识别结果里的唯一追踪，按类型调用对应占位节点。
type ExecuteQuestAction struct{}

func (a *ExecuteQuestAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", executeComponent).Msg("nil context or arg")
		return false
	}

	raw, ok := customDetail(arg.RecognitionDetail)
	if !ok {
		log.Warn().Str("component", executeComponent).Msg("no recognition detail")
		return false
	}
	var detail matchDetail
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		log.Error().
			Err(err).
			Str("component", executeComponent).
			Msg("unmarshal recognition detail failed")
		return false
	}
	track := detail.Track
	node, known := trackingNodes[track.Type]
	event := log.Info().
		Str("component", executeComponent).
		Str("type", track.Type).
		Interface("track", track)
	if !known {
		event.Msg("unknown tracking type")
		return true
	}
	event.Str("node", node).Msg("dispatch quest track")

	taskDetail, err := ctx.RunTask(node)
	if err != nil || taskDetail == nil || !taskDetail.Status.Success() {
		log.Warn().
			Err(err).
			Str("component", executeComponent).
			Str("node", node).
			Str("type", track.Type).
			Msg("placeholder pipeline node failed")
		return false
	}
	return true
}

// customDetail 取绑定已经从 best.detail 拆出的自定义识别结果。
func customDetail(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil || detail.Results == nil || detail.Results.Best == nil {
		return "", false
	}
	custom, ok := detail.Results.Best.AsCustom()
	if !ok || custom == nil || custom.Detail == "" {
		return "", false
	}
	return custom.Detail, true
}
