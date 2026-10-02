package automission

import (
	"encoding/json"
	"sort"
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
	MissionID   string           `json:"mission_id"`
	QuestID     string           `json:"quest_id"`
	DescIndex   int              `json:"desc_index"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Candidates  []objectiveEntry `json:"candidates,omitempty"`
	Tracks      []trackData      `json:"tracks"`
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
		log.Warn().
			Str("component", componentName).
			Str("title", title).
			Msg("unknown mission title")
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

	hit, rest, ok := lookupObjective(description, idx.quests[missionID])
	if !ok {
		log.Warn().
			Str("component", componentName).
			Str("mission_id", missionID).
			Str("title", title).
			Str("description", description).
			Msg("description does not match an objective")
		return nil, false
	}
	if len(rest) > 0 {
		log.Info().
			Str("component", componentName).
			Str("mission_id", missionID).
			Str("quest_id", hit.QuestID).
			Int("desc_index", hit.DescIndex).
			Interface("candidates", rest).
			Msg("objective text matches multiple quests, using the first")
	}

	tracks := hit.Tracks
	if tracks == nil {
		tracks = []trackData{}
	}
	payload, err := json.Marshal(matchDetail{
		MissionID:   missionID,
		QuestID:     hit.QuestID,
		DescIndex:   hit.DescIndex,
		Title:       title,
		Description: description,
		Candidates:  rest,
		Tracks:      tracks,
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
	runes := []rune(strings.TrimSpace(text))
	candidates := entries
	for n := 1; n <= len(runes); n++ {
		prefix := string(runes[:n])
		narrowed := make([]textEntry, 0, len(candidates))
		for _, e := range candidates {
			for _, v := range e.variants {
				if strings.HasPrefix(v, prefix) {
					narrowed = append(narrowed, e)
					break
				}
			}
		}
		candidates = narrowed

		switch len(candidates) {
		case 0:
			return "", false
		case 1:
			return candidates[0].missionID, true
		}
	}
	return "", false
}

// lookupObjective 在一个任务的步骤名称里按 OCR 前缀收窄。
// 收窄到 0 条表示没找到，收窄到 1 条即命中。
// 用完全文仍有多条时，按主线顺序取第一个子步骤，同一子步骤内取最小说明下标，其余作为候选返回。
func lookupObjective(text string, quests *missionQuests) (objectiveEntry, []objectiveEntry, bool) {
	if quests == nil {
		return objectiveEntry{}, nil, false
	}
	runes := []rune(strings.TrimSpace(text))
	candidates := quests.objectives
	for n := 1; n <= len(runes); n++ {
		prefix := string(runes[:n])
		narrowed := make([]objectiveEntry, 0, len(candidates))
		for _, entry := range candidates {
			for _, variant := range entry.variants {
				if strings.HasPrefix(variant, prefix) {
					narrowed = append(narrowed, entry)
					break
				}
			}
		}
		candidates = narrowed

		switch len(candidates) {
		case 0:
			return objectiveEntry{}, nil, false
		case 1:
			return candidates[0], nil, true
		}
	}
	if len(candidates) == 0 {
		return objectiveEntry{}, nil, false
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return objectiveLess(candidates[i], candidates[j], quests.pathOrder)
	})
	return candidates[0], candidates[1:], true
}

// objectiveLess 不在主线上的子步骤排在主线之后。
func objectiveLess(a, b objectiveEntry, pathOrder map[string]int) bool {
	aOrder, aOnPath := pathOrder[a.QuestID]
	if !aOnPath {
		aOrder = len(pathOrder)
	}
	bOrder, bOnPath := pathOrder[b.QuestID]
	if !bOnPath {
		bOrder = len(pathOrder)
	}
	if aOrder != bOrder {
		return aOrder < bOrder
	}
	if a.QuestID != b.QuestID {
		return a.QuestID < b.QuestID
	}
	return a.DescIndex < b.DescIndex
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
