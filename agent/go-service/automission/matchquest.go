package automission

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName = "AutoMissionMatchQuest"

	nodeTitle       = "AutoMissionTitle"
	nodeDescription = "AutoMissionDescription"
)

// MatchQuestRecognition 分别运行标题与说明两个 And，
// 标题按前缀匹配任务，说明只在该任务的步骤名称里按前缀匹配当前子步骤。
type MatchQuestRecognition struct{}

var _ maa.CustomRecognitionRunner = &MatchQuestRecognition{}

type matchDetail struct {
	Tracks    []trackData   `json:"tracks"`
	Condition conditionData `json:"condition"`
}

func (r *MatchQuestRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", componentName).Msg("nil context, arg or image")
		return nil, false
	}

	// 构建数据缓存
	idx, err := getMissionIndex()
	if err != nil {
		return nil, false
	}

	// 识别任务标题
	titleDetail, err := ctx.RunRecognition(nodeTitle, arg.Img)
	if err != nil || titleDetail == nil || !titleDetail.Hit {
		log.Debug().
			Err(err).
			Str("component", componentName).
			Str("node", nodeTitle).
			Msg("title not recognized")
		return nil, false
	}

	// 提取任务标题文本
	title := ocrText(titleDetail)
	if title == "" {
		log.Debug().
			Str("component", componentName).
			Str("node", nodeTitle).
			Msg("title recognized but no OCR text")
		return nil, false
	}

	// 在标题索引中查找匹配的任务，返回唯一符合条件的任务ID
	missionID, ok := lookupByPrefix(title, idx.titles)

	// 本次运行做的是开头那个大任务，标题换了说明它已经做完，结束任务。
	// 换上来的任务可能查不到或同名多个，所以查不到时也比一下标题文字。
	if currentMission.taskID == arg.TaskID {
		changed := missionID != currentMission.missionID
		if !ok {
			changed = comparableText(title) != comparableText(currentMission.title)
		}
		if changed {
			log.Info().
				Str("component", componentName).
				Str("finished_mission_id", currentMission.missionID).
				Str("mission_id", missionID).
				Str("title", title).
				Msg("mission changed, previous mission finished")
			maafocus.Print(ctx, i18n.T("automission.mission_finished", currentMission.title))
			return nil, false
		}
	}

	if !ok {
		maafocus.Print(ctx, i18n.T("automission.unknown_mission", title))
		return nil, false
	}

	if currentMission.taskID != arg.TaskID {
		currentMission.taskID = arg.TaskID
		currentMission.missionID = missionID
		currentMission.title = title
	}

	// 主线任务异常情况较多，暂不支持
	if importance, known := idx.importance[missionID]; known && importance == 0 {
		log.Info().
			Str("component", componentName).
			Str("mission_id", missionID).
			Str("title", title).
			Msg("critical mission is not supported")
		maafocus.Print(ctx, i18n.T("automission.unsupported_critical"))
		return nil, false
	}

	// 识别任务步骤
	descDetail, err := ctx.RunRecognition(nodeDescription, arg.Img)
	if err != nil || descDetail == nil || !descDetail.Hit {
		log.Debug().
			Err(err).
			Str("component", componentName).
			Str("node", nodeDescription).
			Str("mission_id", missionID).
			Msg("description not recognized")
		return nil, false
	}

	// 提取任务步骤文本
	description := ocrText(descDetail)
	if description == "" {
		log.Debug().
			Str("component", componentName).
			Str("node", nodeDescription).
			Str("mission_id", missionID).
			Msg("description recognized but no OCR text")
		return nil, false
	}

	maafocus.Print(ctx, i18n.T("automission.current", title, description))

	// 在当前任务中所有子步骤中尝试找text
	hits, ok := lookupObjective(description, idx.quests[missionID])
	if !ok {
		maafocus.Print(ctx, i18n.T("automission.unsupported_description", description))
		return nil, false
	}

	// 候选是有追踪或有到达区域的子步骤，一个子步骤的全部目标点由执行动作依次走完。
	var candidates []objectiveEntry
	for _, hit := range hits {
		if len(hit.Tracks) > 0 || hit.Condition.Area != nil {
			candidates = append(candidates, hit)
		}
	}
	objectiveIDs := objectiveIDsOf(hits)
	if len(candidates) == 0 {
		log.Info().
			Str("component", componentName).
			Str("mission_id", missionID).
			Strs("objective_ids", objectiveIDs).
			Str("description", description).
			Msg("quest has no tracking")
		maafocus.Print(ctx, i18n.T("automission.unsupported_description", description))
		return nil, false
	}

	// 每次执行到这里时依次选择下一个候选子步骤
	pick, ok := nextTrackIndex(arg.TaskID, missionID+"|"+strings.Join(objectiveIDs, ","), len(candidates))
	if !ok {
		log.Info().
			Str("component", componentName).
			Str("mission_id", missionID).
			Strs("objective_ids", objectiveIDs).
			Str("description", description).
			Int("candidate_count", len(candidates)).
			Msg("all candidate objectives tried without progress")
		maafocus.Print(ctx, i18n.T("automission.unsupported_description", description))
		return nil, false
	}
	hit := candidates[pick]

	payload, err := json.Marshal(matchDetail{
		Tracks:    uniqueSnsTracks(hit.Tracks),
		Condition: hit.Condition,
	})
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("marshal detail failed")
		return nil, false
	}

	log.Info().
		Str("component", componentName).
		Str("mission_id", missionID).
		Str("quest_id", hit.QuestID).
		Int("desc_index", hit.DescIndex).
		Int("candidate_index", pick).
		Int("candidate_count", len(candidates)).
		Int("track_count", len(hit.Tracks)).
		Str("title", title).
		Str("description", description).
		Msg("mission matched")

	return &maa.CustomRecognitionResult{
		Box:    titleDetail.Box,
		Detail: string(payload),
	}, true
}

// 在所有entries任务标题中尝试找text
func lookupByPrefix(text string, entries []textEntry) (string, bool) {

	// 匹配出所有任务名前缀相同的任务
	text = strings.TrimSpace(text)
	runes := []rune(text)
	candidates := entries
	for n := 1; n <= len(runes); n++ {
		prefix := string(runes[:n])
		narrowed := make([]textEntry, 0, len(candidates))
		for _, e := range candidates {
			for _, v := range e.variants {
				if strings.HasPrefix(comparableText(v), comparableText(prefix)) {
					narrowed = append(narrowed, e)
					break
				}
			}
		}
		candidates = narrowed
		// 剩 0 个或 1 个时已有结论，不用再看后面的字
		if len(candidates) <= 1 {
			break
		}
	}

	// 整句用完仍剩多个时，与 OCR 全文完全一致的优先。
	// 例如 OCR 读到「童心速绘」，会剩下「童心速绘」a1m3 和「童心速绘寻旅」a1m7，这里取 a1m3。
	if len(candidates) > 1 {
		var exact []textEntry
		for _, e := range candidates {
			for _, v := range e.variants {
				if comparableText(v) == comparableText(text) {
					exact = append(exact, e)
					break
				}
			}
		}
		if len(exact) == 1 {
			candidates = exact
		}
	}

	switch len(candidates) {
	case 0:
		// 没找到
		log.Warn().
			Str("component", componentName).
			Str("text", text).
			Msg("title lookup missed")
		return "", false
	case 1:
		// 找到一个，符合预期
		log.Info().
			Str("component", componentName).
			Str("text", text).
			Str("mission_id", candidates[0].missionID).
			Msg("title lookup matched")
		return candidates[0].missionID, true
	default:
		// 找到多个，不支持
		missionIDs := make([]string, len(candidates))
		for i, entry := range candidates {
			missionIDs[i] = entry.missionID
		}
		log.Warn().
			Str("component", componentName).
			Str("text", text).
			Strs("mission_ids", missionIDs).
			Msg("title lookup ambiguous")
		return "", false
	}
}

// 在当前任务中所有子步骤中尝试找text
func lookupObjective(text string, quests *missionQuests) ([]objectiveEntry, bool) {
	text = strings.TrimSpace(text)
	if quests == nil {
		log.Warn().
			Str("component", componentName).
			Str("text", text).
			Msg("objective lookup missed")
		return nil, false
	}
	runes := []rune(text)
	candidates := quests.objectives
	for n := 1; n <= len(runes); n++ {
		prefix := string(runes[:n])
		narrowed := make([]objectiveEntry, 0, len(candidates))
		for _, entry := range candidates {
			for _, variant := range entry.variants {
				if strings.HasPrefix(comparableText(variant), comparableText(prefix)) {
					narrowed = append(narrowed, entry)
					break
				}
			}
		}
		candidates = narrowed
		// 剩 0 条或 1 条时已有结论，不用再看后面的字
		if len(candidates) <= 1 {
			break
		}
	}

	// 整句用完仍剩多条时，只保留与 OCR 全文完全一致的。
	// 例如 OCR 读到「跟随黎风」，会剩下 q#8、q#15「跟随黎风」和 q#21「跟随黎风完成祭奠」等，
	// 这里只留 q#8、q#15，它们才是真正的同名步骤。一条都不一致（OCR 被截断）时保持原样。
	if len(candidates) > 1 {
		var exact []objectiveEntry
		for _, entry := range candidates {
			for _, variant := range entry.variants {
				if comparableText(variant) == comparableText(text) {
					exact = append(exact, entry)
					break
				}
			}
		}
		if len(exact) > 0 {
			candidates = exact
		}
	}

	switch len(candidates) {
	case 0:
		log.Warn().
			Str("component", componentName).
			Str("text", text).
			Msg("objective lookup missed")
		return nil, false
	case 1:
		log.Info().
			Str("component", componentName).
			Str("text", text).
			Str("quest_id", candidates[0].QuestID).
			Int("desc_index", candidates[0].DescIndex).
			Msg("objective lookup matched")
		return candidates[:1], true
	}

	// 用完全文仍有多条，是同名步骤，按任务先后顺序排序，供上层循环执行一遍
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		if a.QuestID != b.QuestID {
			return a.QuestID < b.QuestID
		}
		return a.DescIndex < b.DescIndex
	})
	log.Info().
		Str("component", componentName).
		Str("text", text).
		Int("count", len(candidates)).
		Msg("objective lookup ambiguous, rotate candidates")
	return candidates, true
}

// uniqueSnsTracks 去掉重复的 SnsTrackingInfo，只留第一个。
// 它没有坐标，执行的都是同一个流程（重新追踪后进 Baker 推进会话），跑一次就够。
func uniqueSnsTracks(tracks []trackData) []trackData {
	result := make([]trackData, 0, len(tracks))
	hasSns := false
	for _, track := range tracks {
		if track.Type == "SnsTrackingInfo" {
			if hasSns {
				continue
			}
			hasSns = true
		}
		result = append(result, track)
	}
	return result
}

// objectiveIDsOf 返回每条步骤名称的标识，用于拼轮换 key 和打日志。
// 普通目标就是 questId，如 sm2l2m1_q#2；多说明目标再带上说明下标，如 a1m14_q#2#1。
// 多说明目标的几句说明同属一个 quest，会随进度依次切换，只用 questId 会把推进误判为没推进。
func objectiveIDsOf(entries []objectiveEntry) []string {
	ids := make([]string, len(entries))
	for i, entry := range entries {
		ids[i] = entry.QuestID
		if entry.DescIndex >= 0 {
			ids[i] += "#" + strconv.Itoa(entry.DescIndex)
		}
	}
	return ids
}

// currentMission 记录本次任务运行开头识别到的大任务。
var currentMission struct {
	taskID    int64
	missionID string
	title     string
}

// trackRotation 记录当前步骤已经试到第几个候选子步骤。
// 描述没变说明上一个子步骤没推进，下一轮换下一个；换了任务运行或步骤就从头开始。
var trackRotation struct {
	taskID int64
	key    string
	next   int
}

// nextTrackIndex 返回本轮要执行的候选下标；每个候选只试一次，用完返回 false。
func nextTrackIndex(taskID int64, key string, count int) (int, bool) {
	if trackRotation.taskID != taskID || trackRotation.key != key {
		trackRotation.taskID = taskID
		trackRotation.key = key
		trackRotation.next = 0
	}
	if trackRotation.next >= count {
		return 0, false
	}
	index := trackRotation.next
	trackRotation.next++
	return index, true
}

// comparableText 去掉整段文本中的每一处中点类分隔符，供前缀比较使用。
// OCR 常把「·」认成其它点号，或直接漏掉。
func comparableText(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '·', '・', '･', '•', '.', '．':
			return -1
		default:
			return r
		}
	}, s)
}

// ocrText 在识别结果树中取第一段非空 OCR，只读 Filtered。
// And 节点自身 Results 为空，文字在 CombinedResult 的子结果上。
func ocrText(detail *maa.RecognitionDetail) string {
	if detail == nil {
		return ""
	}
	if detail.Results != nil {
		for _, res := range detail.Results.Filtered {
			if res == nil {
				continue
			}
			ocr, ok := res.AsOCR()
			if !ok {
				continue
			}
			if text := strings.TrimSpace(ocr.Text); text != "" {
				return text
			}
		}
	}
	for _, sub := range detail.CombinedResult {
		if text := ocrText(sub); text != "" {
			return text
		}
	}
	return ""
}
