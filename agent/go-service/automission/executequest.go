package automission

import (
	"encoding/json"
	"math"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	executeComponent     = "AutoMissionExecuteQuest"
	npcProxyNode         = "AutoMissionNpcProxy"
	npcProxyInWorldNode  = "AutoMissionNpcProxyInWorld"
	npcProxySecondNode   = "AutoMissionNpcProxySecondInWorld"
	npcProxyThirdNode    = "AutoMissionNpcProxyThirdInWorld"
	npcProxyInteractNode = "AutoMissionNpcProxyActionInteractRecognition"
	npcProxyPhotoNode    = "AutoMissionNpcProxyActionTakePhoto"
	npcProxyScanNode     = "AutoMissionNpcProxyActionScan"
	npcProxyStoryNode    = "InStory"

	missionAreaNode        = "AutoMissionMissionArea"
	missionAreaInWorldNode = "AutoMissionMissionAreaInWorld"
	missionAreaSecondNode  = "AutoMissionMissionAreaSecondInWorld"
	missionAreaPhotoNode   = "AutoMissionMissionAreaActionTakePhoto"
	missionAreaScanNode    = "AutoMissionMissionAreaActionScan"

	posNode         = "AutoMissionPos"
	posInWorldNode  = "AutoMissionPosInWorld"
	posInteractNode = "AutoMissionPosActionInteractRecognition"
	posMoveNode     = "AutoMissionPosActionMove"
	posPhotoNode    = "AutoMissionPosActionTakePhoto"
	posScanNode     = "AutoMissionPosActionScan"

	entityNode         = "AutoMissionEntity"
	entityInWorldNode  = "AutoMissionEntityInWorld"
	entityInteractNode = "AutoMissionEntityActionInteractRecognition"
	entityTriggerNode  = "AutoMissionEntityTrigger"
	entityCameraNode   = "AutoMissionEntityActionOpenCamera"
	entityScanNode     = "AutoMissionEntityActionScan"
)

// fallbackInteractTexts 是坐标点没写 NPC 名字、实体没写交互文字时兜底识别的交互文字（简繁）。
// 坐标点的完成条件常在服务器判断，数据里看不出要交互什么，但交互物一般就在追踪点旁边；
// 实体的选项也可能挂在与它重合的另一个交互物上，如「抚摸"蟠兽"」。
// 只收点错也没副作用的文字；使用、提交物品、进入之类可能消耗道具或切换玩法的不收。
var fallbackInteractTexts = []string{
	"查看",
	"修复",
	"修復",
	"调查",
	"調查",
	"打开",
	"打開",
	"采集",
	"採集",
	"触碰",
	"觸碰",
	"推开",
	"推開",
	"拿取文件",
	"抚摸",
	"撫摸",
}

// npcProxyNavigateNodes 是 NPC 追踪依次寻路的节点，前一个点没找到人才进下一个。
var npcProxyNavigateNodes = []string{
	npcProxyInWorldNode,
	npcProxySecondNode,
	npcProxyThirdNode,
}

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

// ExecuteQuestAction 读取识别结果里选中的子步骤，按顺序对每个追踪调用对应类型的节点。
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

	// 某个点失败不中断，继续走剩下的点；是否推进交给外层回任务界面判断。
	// 已经做过的点（如聊过的 NPC、收走的实体）再走一遍会失败或什么都不发生，不影响后面的点。
	tracks := targetTracks(detail.Tracks, detail.Condition)
	tasker := ctx.GetTasker()
	succeeded := 0
	for i, track := range tracks {
		if tasker != nil && tasker.Stopping() {
			log.Info().Str("component", executeComponent).Msg("tasker stopping")
			return false
		}
		ok := runTrack(ctx, track, detail.Condition)
		log.Info().
			Str("component", executeComponent).
			Str("type", track.Type).
			Int("track_index", i).
			Int("track_count", len(tracks)).
			Bool("success", ok).
			Msg("quest track done")
		if ok {
			succeeded++
		}
	}
	return succeeded > 0
}

// targetTracks 返回这一步要依次走的目标点。
// 到达条件带了区域时只走这个区域，走进去就算完成，不再走追踪点；否则走全部追踪点。
func targetTracks(tracks []trackData, condition conditionData) []trackData {
	area := condition.Area
	if area == nil {
		return tracks
	}
	x, y := area.X, area.Y
	// 扫描是这一步的要求，换成区域后照样要扫。
	scan := false
	for _, track := range tracks {
		scan = scan || track.Scan
	}
	return []trackData{
		{
			Type:   "MissionAreaTrackingInfo",
			ZoneID: area.ZoneID,
			X:      &x,
			Y:      &y,
			Height: area.Height,
			Scan:   scan,
		},
	}
}

// runTrack 按追踪类型调用对应节点，返回节点是否执行成功；未知类型直接算成功。
func runTrack(ctx *maa.Context, track trackData, condition conditionData) bool {
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
		targets := npcProxyTargets(track)
		log.Info().
			Str("component", executeComponent).
			Str("zone_id", targets[0].ZoneID).
			Interface("targets", targets).
			Int("point_count", len(track.Points)).
			Strs("npc_names", npcNames).
			Msg("npc proxy navigate target")
		override := map[string]any{
			npcProxyInteractNode: map[string]any{
				"expected": npcNames,
			},
			npcProxyPhotoNode: map[string]any{
				"enabled": track.Snapshot,
			},
			npcProxyScanNode: map[string]any{
				"enabled": track.Scan,
			},
		}
		// 第一个寻路节点总是开着；后面的节点有点才打开，每次都显式写 enabled。
		for i, navNode := range npcProxyNavigateNodes {
			nodeOverride := map[string]any{}
			if i > 0 {
				nodeOverride["enabled"] = i < len(targets)
			}
			if i < len(targets) {
				nodeOverride["custom_action_param"] = npcProxyNavigateParam(targets[i])
			}
			override[navNode] = nodeOverride
		}
		taskDetail, err = ctx.RunTask(node, override)
	case missionAreaNode:
		// 任务区域只要走进去就会推进，寻路到区域中心即可，不做交互。
		target, ok := navigateTarget(track, condition)
		if !ok {
			log.Warn().
				Str("component", executeComponent).
				Str("zone_id", track.ZoneID).
				Bool("has_x", track.X != nil).
				Bool("has_y", track.Y != nil).
				Msg("mission area track missing zone or position")
			return false
		}
		event := log.Info().
			Str("component", executeComponent).
			Str("node", missionAreaInWorldNode).
			Str("source", target.source).
			Str("zone_id", target.zoneID).
			Float64("x", target.x).
			Float64("y", target.y)
		if track.TriggerPoint != nil {
			event = event.Interface("trigger_point", track.TriggerPoint)
		}
		event.Msg("mission area navigate target")
		// 区域里有推进任务的触发区时，走完区域中心再去触发区；每次都显式写 enabled。
		secondOverride := map[string]any{
			"enabled": track.TriggerPoint != nil,
		}
		if track.TriggerPoint != nil {
			secondOverride["custom_action_param"] = missionAreaNavigateParam(track.TriggerPoint.X, track.TriggerPoint.Y, track.TriggerPoint.Height)
		}
		taskDetail, err = ctx.RunTask(node, map[string]any{
			missionAreaPhotoNode: map[string]any{
				"enabled": track.Snapshot,
			},
			missionAreaScanNode: map[string]any{
				"enabled": track.Scan,
			},
			missionAreaInWorldNode: map[string]any{
				"custom_action_param": missionAreaNavigateParam(target.x, target.y, target.height),
			},
			missionAreaSecondNode: secondOverride,
		})
	case posNode:
		// 坐标点一般走到就会推进；数据带了 NPC 名字时，走到后再找这个 NPC 交互。
		// 暂不支持：完成条件要求分散在追踪点周围的多个物品都交互一遍，
		// 如 sm2l4m5_q#7「拿取奚墨需要的文件」要在 4 个关卡脚本里各拿一份文件，只走到追踪点不会推进。
		npcNames := npcNameVariants(track)
		target, ok := navigateTarget(track, condition)
		if !ok {
			log.Warn().
				Str("component", executeComponent).
				Str("zone_id", track.ZoneID).
				Bool("has_x", track.X != nil).
				Bool("has_y", track.Y != nil).
				Msg("pos track missing zone or position")
			return false
		}
		// 没有人名：识别兜底交互文字，并关掉按追踪图标找人，免得附近没有交互物时一直走到超时。
		moveEnabled := len(npcNames) > 0
		if !moveEnabled {
			npcNames = fallbackInteractTexts
		}
		log.Info().
			Str("component", executeComponent).
			Str("node", posInWorldNode).
			Str("source", target.source).
			Str("zone_id", target.zoneID).
			Float64("x", target.x).
			Float64("y", target.y).
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
			posScanNode: map[string]any{
				"enabled": track.Scan,
			},
			posInWorldNode: map[string]any{
				"custom_action_param": map[string]any{
					"heading_source": "camera",
					"path": []any{
						navmeshStep(target.x, target.y, target.height),
						map[string]any{
							"action":       "TRIGGER",
							"target":       []float64{target.x, target.y},
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
			// 没有交互文字：识别兜底交互文字。
			interactTexts = fallbackInteractTexts
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
			entityCameraNode: map[string]any{
				"enabled": track.Snapshot,
			},
			entityScanNode: map[string]any{
				"enabled": track.Scan,
			},
			entityInWorldNode: map[string]any{
				"custom_action_param": map[string]any{
					"heading_source": "camera",
					"path": []any{
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

// navTarget 是寻路目标和它的来源，来源为 enemy 或 track。
type navTarget struct {
	source string
	zoneID string
	x, y   float64
	height *float64
}

// npcPointMinDistance 是后面的点离前面已选点的最小底图距离；再近的话寻路到前面的点时就已经到了。
const npcPointMinDistance = 3.0

// npcProxyTargets 按顺序选 NPC 追踪的寻路点，最多 len(npcProxyNavigateNodes) 个，前一个点没找到人时再去下一个。
// 没有 points 时只有初始位置；有 points 时依次取，跳过离已选点太近的点。
func npcProxyTargets(track trackData) []groundPoint {
	if len(track.Points) == 0 {
		return []groundPoint{{ZoneID: track.ZoneID, X: *track.X, Y: *track.Y, Height: track.Height}}
	}
	targets := make([]groundPoint, 0, len(npcProxyNavigateNodes))
	for _, point := range track.Points {
		if len(targets) == len(npcProxyNavigateNodes) {
			break
		}
		near := false
		for _, target := range targets {
			if point.ZoneID == target.ZoneID && math.Hypot(point.X-target.X, point.Y-target.Y) <= npcPointMinDistance {
				near = true
				break
			}
		}
		if !near {
			targets = append(targets, point)
		}
	}
	return targets
}

// npcProxyNavigateParam 生成 NPC 追踪寻路到一个点的参数，到达或进入剧情即停。
// 不冲刺：冲刺停不住，容易冲过 NPC 或错过交互提示。
func npcProxyNavigateParam(point groundPoint) map[string]any {
	return map[string]any{
		"heading_source": "camera",
		"enable_sprint":  false,
		"path": []any{
			navmeshStep(point.X, point.Y, point.Height),
			map[string]any{
				"action":       "TRIGGER",
				"target":       []float64{point.X, point.Y},
				"trigger_node": npcProxyStoryNode,
			},
		},
	}
}

// missionAreaNavigateParam 生成任务区域寻路到一个点的参数，到达或进入剧情即停。
// 有高度时钉住那一层，区域可能在楼上，只按平面坐标会停在楼下。
func missionAreaNavigateParam(x, y float64, height *float64) map[string]any {
	return map[string]any{
		"heading_source": "camera",
		"path": []any{
			navmeshStep(x, y, height),
			map[string]any{
				"action":       "TRIGGER",
				"target":       []float64{x, y},
				"trigger_node": npcProxyStoryNode,
			},
		},
	}
}

// navmeshStep 生成寻路到目标的 NAVMESH 路点；有高度时钉住目标所在的那层可走面，
// 到不了这层会直接失败，不会对着断崖反复盲走。
func navmeshStep(x, y float64, height *float64) map[string]any {
	step := map[string]any{
		"action": "NAVMESH",
		"target": []float64{x, y},
	}
	if height != nil {
		step["target_deck_y"] = *height
	}
	return step
}

// navigateTarget 选寻路目标：击杀条件有怪物坐标时去第一只怪，否则去追踪点。
// 追踪点可能在挡路的怪后面，直接寻路过去会被空气墙挡住。
func navigateTarget(track trackData, condition conditionData) (navTarget, bool) {
	if len(condition.Enemies) > 0 {
		enemy := condition.Enemies[0]
		return navTarget{source: "enemy", zoneID: enemy.ZoneID, x: enemy.X, y: enemy.Y}, true
	}
	if track.ZoneID == "" || track.X == nil || track.Y == nil {
		return navTarget{}, false
	}
	return navTarget{source: "track", zoneID: track.ZoneID, x: *track.X, y: *track.Y, height: track.Height}, true
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
