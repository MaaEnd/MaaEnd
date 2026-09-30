package automission

import (
	"encoding/json"
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
// 从各自结果中取出 OCR 后按前缀匹配 missions.json；两者指向同一任务才算命中。
type MatchQuestRecognition struct{}

var _ maa.CustomRecognitionRunner = &MatchQuestRecognition{}

type matchDetail struct {
	MissionID   string `json:"mission_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type unknownKey struct {
	taskID int64
	title  string
}

// 识别失败会被流水线反复重试，同一次任务里同一个标题只提示一次。
var reportedUnknown sync.Map

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
		reportUnknown(ctx, arg.TaskID, title)
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

	descMissionID, ok := lookupByPrefix(description, idx.descriptions)
	if !ok || descMissionID != missionID {
		log.Warn().
			Str("component", componentName).
			Str("mission_id", missionID).
			Str("description_mission_id", descMissionID).
			Str("title", title).
			Str("description", description).
			Msg("description does not match title")
		return nil, false
	}

	payload, err := json.Marshal(matchDetail{
		MissionID:   missionID,
		Title:       title,
		Description: description,
	})
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("marshal detail failed")
		return nil, false
	}

	log.Info().
		Str("component", componentName).
		Str("mission_id", missionID).
		Str("title", title).
		Str("description", description).
		Msg("mission matched")

	return &maa.CustomRecognitionResult{
		Box:    titleDetail.Box,
		Detail: string(payload),
	}, true
}

func reportUnknown(ctx *maa.Context, taskID int64, title string) {
	if _, seen := reportedUnknown.LoadOrStore(unknownKey{taskID: taskID, title: title}, struct{}{}); seen {
		return
	}
	maafocus.Print(ctx, i18n.T("automission.unknown_mission", title))
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
