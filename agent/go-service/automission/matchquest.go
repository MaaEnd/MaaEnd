package automission

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"

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
	Track trackData `json:"track"`
}

func (r *MatchQuestRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", componentName).Msg("nil context, arg or image")
		return nil, false
	}

	idx, err := getMissionIndex()
	if err != nil {
		return nil, false
	}

	titleDetail, err := ctx.RunRecognition(nodeTitle, arg.Img)
	if err != nil || titleDetail == nil || !titleDetail.Hit {
		log.Debug().
			Err(err).
			Str("component", componentName).
			Str("node", nodeTitle).
			Msg("title not recognized")
		return nil, false
	}

	title := ocrText(titleDetail)
	if title == "" {
		log.Debug().
			Str("component", componentName).
			Str("node", nodeTitle).
			Msg("title recognized but no OCR text")
		return nil, false
	}

	missionID, ok := lookupByPrefix(title, idx.titles)
	if !ok {
		maafocus.Print(ctx, i18n.T("automission.unknown_mission", title))
		return nil, false
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

	hits, ok := lookupObjective(description, idx.quests[missionID])
	if !ok {
		maafocus.Print(ctx, i18n.T("automission.unsupported_description", description))
		return nil, false
	}

	// 命中的子步骤按先后展开成候选追踪点，每轮取一个。
	var candidates []trackCandidate
	for _, hit := range hits {
		for _, track := range hit.Tracks {
			candidates = append(candidates, trackCandidate{hit: hit, track: track})
		}
	}
	questIDs := questIDsOf(hits)
	if len(candidates) == 0 {
		log.Info().
			Str("component", componentName).
			Str("mission_id", missionID).
			Strs("quest_ids", questIDs).
			Str("description", description).
			Msg("quest has no tracking")
		maafocus.Print(ctx, i18n.T("automission.unsupported_description", description))
		return nil, false
	}

	pick, ok := nextTrackIndex(arg.TaskID, missionID+"|"+strings.Join(questIDs, ","), len(candidates))
	if !ok {
		log.Info().
			Str("component", componentName).
			Str("mission_id", missionID).
			Strs("quest_ids", questIDs).
			Str("description", description).
			Int("track_count", len(candidates)).
			Msg("all tracking points tried without progress")
		maafocus.Print(ctx, i18n.T("automission.unsupported_description", description))
		return nil, false
	}
	hit := candidates[pick].hit

	payload, err := json.Marshal(matchDetail{
		Track: candidates[pick].track,
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
		Int("track_index", pick).
		Int("track_count", len(candidates)).
		Str("title", title).
		Str("description", description).
		Msg("mission matched")

	return &maa.CustomRecognitionResult{
		Box:    titleDetail.Box,
		Detail: string(payload),
	}, true
}

// lookupByPrefix 用 OCR 文本逐字加长的前缀在 entries 中收窄候选：
// 候选为 0 表示未找到；只剩 1 个即命中；用完 OCR 全文仍有多个则视为无法确定。
func lookupByPrefix(text string, entries []textEntry) (string, bool) {
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

		switch len(candidates) {
		case 0:
			log.Warn().
				Str("component", componentName).
				Str("text", text).
				Msg("title lookup missed")
			return "", false
		case 1:
			log.Info().
				Str("component", componentName).
				Str("text", text).
				Str("mission_id", candidates[0].missionID).
				Msg("title lookup matched")
			return candidates[0].missionID, true
		}
	}

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

// lookupObjective 在一个任务的步骤名称里按 OCR 前缀收窄。
// 收窄到 0 条表示没找到，收窄到 1 条即命中。
// 用完全文仍有多条时，若全部都是单条 SnsTrackingInfo 则取第一条；否则按子步骤先后全部返回。
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
	}

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
	questIDs := questIDsOf(candidates)
	if singleSnsTracking(candidates) {
		log.Info().
			Str("component", componentName).
			Str("text", text).
			Str("quest_id", candidates[0].QuestID).
			Int("desc_index", candidates[0].DescIndex).
			Strs("quest_ids", questIDs).
			Msg("ambiguous sns objectives share one tracking step")
		return candidates[:1], true
	}
	log.Info().
		Str("component", componentName).
		Str("text", text).
		Strs("quest_ids", questIDs).
		Msg("objective lookup ambiguous, rotate candidates")
	return candidates, true
}

// trackCandidate 是一个候选追踪点及其所属的子步骤。
type trackCandidate struct {
	hit   objectiveEntry
	track trackData
}

func questIDsOf(entries []objectiveEntry) []string {
	ids := make([]string, len(entries))
	for i, entry := range entries {
		ids[i] = entry.QuestID
	}
	return ids
}

// trackRotation 记录当前步骤已经试到第几个候选追踪点。
// 描述没变说明上一个点没推进，下一轮换下一个；换了任务运行或步骤就从头开始。
var trackRotation struct {
	mu     sync.Mutex
	taskID int64
	key    string
	next   int
}

// nextTrackIndex 返回本轮要执行的候选下标；每个候选只试一次，用完返回 false。
func nextTrackIndex(taskID int64, key string, count int) (int, bool) {
	trackRotation.mu.Lock()
	defer trackRotation.mu.Unlock()
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

// singleSnsTracking 判断这些同名步骤是否都能走同一条 SNS 流程。
// SnsTrackingInfo 没有坐标，多条之间没有可区分的下一步。
func singleSnsTracking(entries []objectiveEntry) bool {
	if len(entries) == 0 {
		return false
	}
	for _, entry := range entries {
		if len(entry.Tracks) != 1 || entry.Tracks[0].Type != "SnsTrackingInfo" {
			return false
		}
	}
	return true
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
