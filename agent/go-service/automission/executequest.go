package automission

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	executeComponent     = "AutoMissionExecuteQuest"
	npcProxyNode         = "AutoMissionNpcProxy"
	npcProxyInWorldNode  = "AutoMissionNpcProxyInWorld"
	npcProxyInteractNode = "AutoMissionNpcProxyActionInteractRecognition"
	npcProxyStoryNode    = "InStory"
)

var _ maa.CustomActionRunner = &ExecuteQuestAction{}

// trackingNodes 把解包追踪类型对应到占位 Pipeline 节点。
var trackingNodes = map[string]string{
	"MissionAreaTrackingInfo": "AutoMissionMissionArea",
	"NpcProxyTrackingInfo":    npcProxyNode,
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

	var (
		taskDetail *maa.TaskDetail
		err        error
	)
	if node == npcProxyNode {
		npcNames := npcNameVariants(track)
		if track.ZoneID == "" || track.X == nil || track.Y == nil {
			log.Warn().
				Str("component", executeComponent).
				Str("zone_id", track.ZoneID).
				Bool("has_x", track.X != nil).
				Bool("has_y", track.Y != nil).
				Msg("npc proxy track missing zone or position")
			return false
		}
		if len(npcNames) == 0 {
			// 没有人名：注入永不匹配的正则，只走到坐标，不做交互。
			npcNames = []string{"(?!)"}
		}
		log.Info().
			Str("component", executeComponent).
			Str("node", npcProxyInWorldNode).
			Str("zone_id", track.ZoneID).
			Float64("x", *track.X).
			Float64("y", *track.Y).
			Strs("npc_names", npcNames).
			Msg("npc proxy navigate target")
		taskDetail, err = ctx.RunTask(node, map[string]any{
			npcProxyInteractNode: map[string]any{
				"expected": npcNames,
			},
			npcProxyInWorldNode: map[string]any{
				"custom_action_param": map[string]any{
					"heading_source": "camera",
					"path": []any{
						map[string]any{
							"action":  "ZONE",
							"zone_id": track.ZoneID,
						},
						map[string]any{
							"action": "NAVMESH",
							"target": []float64{*track.X, *track.Y},
						},
						map[string]any{
							"action":       "TRIGGER",
							"target":       []float64{*track.X, *track.Y},
							"trigger_node": npcProxyStoryNode,
						},
					},
				},
			},
		})
	} else {
		taskDetail, err = ctx.RunTask(node)
	}
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
