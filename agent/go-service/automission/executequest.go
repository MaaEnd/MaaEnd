package automission

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const executeComponent = "AutoMissionExecuteQuest"

var _ maa.CustomActionRunner = &ExecuteQuestAction{}

// trackingNodes 把解包追踪类型对应到占位 Pipeline 节点，这些节点尚未编写。
var trackingNodes = map[string]string{
	"MissionAreaTrackingInfo": "AutoMissionMissionArea",
	"NpcProxyTrackingInfo":    "AutoMissionNpcProxy",
	"PosTrackingInfo":         "AutoMissionPos",
	"EntityTrackingInfo":      "AutoMissionEntity",
	"JumpToUITrackingInfo":    "AutoMissionJumpToUI",
	"SnsTrackingInfo":         "AutoMissionSns",
}

// ExecuteQuestAction 读取 AutoMissionMatchQuest 识别结果里的追踪，按第一条追踪的类型调用对应占位节点。
type ExecuteQuestAction struct{}

func (a *ExecuteQuestAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", executeComponent).Msg("nil context or arg")
		return false
	}

	if arg.RecognitionDetail == nil || arg.RecognitionDetail.DetailJson == "" {
		log.Warn().Str("component", executeComponent).Msg("no recognition detail")
		return false
	}
	detail, err := unmarshalExecuteDetail(arg.RecognitionDetail.DetailJson)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", executeComponent).
			Msg("unmarshal recognition detail failed")
		return false
	}
	if len(detail.Tracks) == 0 {
		log.Info().
			Str("component", executeComponent).
			Str("mission_id", detail.MissionID).
			Str("quest_id", detail.QuestID).
			Int("desc_index", detail.DescIndex).
			Msg("quest has no tracking")
		return true
	}

	track := detail.Tracks[0]
	node, known := trackingNodes[track.Type]
	event := log.Info().
		Str("component", executeComponent).
		Str("mission_id", detail.MissionID).
		Str("quest_id", detail.QuestID).
		Int("desc_index", detail.DescIndex).
		Str("type", track.Type).
		Interface("track", track).
		Int("track_count", len(detail.Tracks))
	if !known {
		event.Msg("unknown tracking type")
		return true
	}
	event.Str("node", node).Msg("dispatch quest track")

	if _, err := ctx.RunTask(node); err != nil {
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

// unmarshalExecuteDetail 兼容 MaaFramework 把自定义识别结果包在 best.detail 里的情况。
func unmarshalExecuteDetail(raw string) (matchDetail, error) {
	var detail matchDetail
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		return matchDetail{}, err
	}
	if detail.QuestID != "" {
		return detail, nil
	}

	var wrapped struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapped); err != nil || len(wrapped.Best.Detail) == 0 {
		return detail, nil
	}
	if err := json.Unmarshal(wrapped.Best.Detail, &detail); err != nil {
		return matchDetail{}, err
	}
	return detail, nil
}
