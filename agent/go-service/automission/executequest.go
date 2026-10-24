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
	npcProxyPhotoNode    = "AutoMissionNpcProxyActionTakePhoto"
	npcProxyStoryNode    = "InStory"

	missionAreaNode        = "AutoMissionMissionArea"
	missionAreaInWorldNode = "AutoMissionMissionAreaInWorld"
	missionAreaPhotoNode   = "AutoMissionMissionAreaActionTakePhoto"

	posNode         = "AutoMissionPos"
	posInWorldNode  = "AutoMissionPosInWorld"
	posInteractNode = "AutoMissionPosActionInteractRecognition"
	posMoveNode     = "AutoMissionPosActionMove"
	posPhotoNode    = "AutoMissionPosActionTakePhoto"

	entityNode         = "AutoMissionEntity"
	entityInWorldNode  = "AutoMissionEntityInWorld"
	entityInteractNode = "AutoMissionEntityActionInteractRecognition"
	entityTriggerNode  = "AutoMissionEntityTrigger"
)

var _ maa.CustomActionRunner = &ExecuteQuestAction{}

// trackingNodes 把追踪类型对应到 Pipeline 节点。
var trackingNodes = map[string]string{
	"MissionAreaTrackingInfo": missionAreaNode,
	"NpcProxyTrackingInfo":    npcProxyNode,
	"PosTrackingInfo":         posNode,
	"EntityTrackingInfo":      entityNode,
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
	switch node {
	case npcProxyNode:
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
			npcProxyPhotoNode: map[string]any{
				"enabled": track.Snapshot,
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
	case missionAreaNode:
		// 任务区域只要走进去就会推进，寻路到区域中心即可，不做交互。
		if track.ZoneID == "" || track.X == nil || track.Y == nil {
			log.Warn().
				Str("component", executeComponent).
				Str("zone_id", track.ZoneID).
				Bool("has_x", track.X != nil).
				Bool("has_y", track.Y != nil).
				Msg("mission area track missing zone or position")
			return false
		}
		log.Info().
			Str("component", executeComponent).
			Str("node", missionAreaInWorldNode).
			Str("zone_id", track.ZoneID).
			Float64("x", *track.X).
			Float64("y", *track.Y).
			Msg("mission area navigate target")
		taskDetail, err = ctx.RunTask(node, map[string]any{
			missionAreaPhotoNode: map[string]any{
				"enabled": track.Snapshot,
			},
			missionAreaInWorldNode: map[string]any{
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
	case posNode:
		// 坐标点一般走到就会推进；数据带了 NPC 名字时，走到后再找这个 NPC 交互。
		npcNames := npcNameVariants(track)
		if track.ZoneID == "" || track.X == nil || track.Y == nil {
			log.Warn().
				Str("component", executeComponent).
				Str("zone_id", track.ZoneID).
				Bool("has_x", track.X != nil).
				Bool("has_y", track.Y != nil).
				Msg("pos track missing zone or position")
			return false
		}
		// 没有人名：注入永不匹配的正则并关掉按追踪图标找人，只走到坐标，不做交互。
		moveEnabled := len(npcNames) > 0
		if !moveEnabled {
			npcNames = []string{"(?!)"}
		}
		log.Info().
			Str("component", executeComponent).
			Str("node", posInWorldNode).
			Str("zone_id", track.ZoneID).
			Float64("x", *track.X).
			Float64("y", *track.Y).
			Strs("npc_names", npcNames).
			Msg("pos navigate target")
		taskDetail, err = ctx.RunTask(node, map[string]any{
			posInteractNode: map[string]any{
				"expected": npcNames,
			},
			posMoveNode: map[string]any{
				"enabled": moveEnabled,
			},
			posPhotoNode: map[string]any{
				"enabled": track.Snapshot,
			},
			posInWorldNode: map[string]any{
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
	case entityNode:
		interactTexts := interactTextVariants(track)
		if track.ZoneID == "" || track.X == nil || track.Y == nil {
			log.Warn().
				Str("component", executeComponent).
				Str("zone_id", track.ZoneID).
				Str("entity", track.EntityDataIDKey).
				Bool("has_x", track.X != nil).
				Bool("has_y", track.Y != nil).
				Msg("entity track missing zone or position")
			return false
		}
		if len(interactTexts) == 0 {
			// 没有交互文字：注入永不匹配的正则，只走到坐标，不做交互。
			interactTexts = []string{"(?!)"}
		}
		log.Info().
			Str("component", executeComponent).
			Str("node", entityInWorldNode).
			Str("zone_id", track.ZoneID).
			Str("entity", track.EntityDataIDKey).
			Float64("x", *track.X).
			Float64("y", *track.Y).
			Strs("interact_texts", interactTexts).
			Msg("entity navigate target")
		taskDetail, err = ctx.RunTask(node, map[string]any{
			entityInteractNode: map[string]any{
				"expected": interactTexts,
			},
			entityInWorldNode: map[string]any{
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
							"trigger_node": entityTriggerNode,
						},
					},
				},
			},
		})
	default:
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
