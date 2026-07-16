package dailyrewards

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	dailyEventGoToRecognitionName = "DailyEventGoToRecognition"
	dailyEventGoToCandidateNode   = "DailyEventGoToCandidate"
	dailyEventGoToEntryNameNode   = "DailyEventRecognitionItemText"
	dailyEventGoToAttachVisited   = "visited"
)

// DailyEventGoToRecognition 读取 attach.visited，排除已点入口后调用外部节点
// DailyEventGoToCandidate（未读标记 ∧ 入口名）完成识别，并回写 visited。
type DailyEventGoToRecognition struct{}

var _ maa.CustomRecognitionRunner = &DailyEventGoToRecognition{}

type dailyEventGoToAttach struct {
	Visited []string `json:"visited"`
}

type dailyEventGoToDetail struct {
	Text string `json:"text"`
}

func (r *DailyEventGoToRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		log.Error().Str("component", dailyEventGoToRecognitionName).Msg("nil context or arg")
		return nil, false
	}

	nodeName := strings.TrimSpace(arg.CurrentTaskName)
	if nodeName == "" {
		log.Error().Str("component", dailyEventGoToRecognitionName).Msg("current task name is empty")
		return nil, false
	}

	visited, err := loadDailyEventGoToVisited(ctx, nodeName)
	if err != nil {
		log.Error().Err(err).Str("component", dailyEventGoToRecognitionName).Str("node", nodeName).Msg("load attach.visited failed")
		return nil, false
	}

	expected := buildDailyEventGoToEntryExpected(visited)
	if err := ctx.OverridePipeline(map[string]any{
		dailyEventGoToEntryNameNode: map[string]any{
			"expected": []string{expected},
		},
	}); err != nil {
		log.Error().Err(err).Str("component", dailyEventGoToRecognitionName).Msg("override entry expected failed")
		return nil, false
	}

	detail, err := ctx.RunRecognition(dailyEventGoToCandidateNode, arg.Img)
	if err != nil {
		log.Error().Err(err).Str("component", dailyEventGoToRecognitionName).Str("node", dailyEventGoToCandidateNode).Msg("RunRecognition failed")
		return nil, false
	}
	if detail == nil || !detail.Hit {
		log.Info().Str("component", dailyEventGoToRecognitionName).Strs("visited", visited).Msg("no unread entry candidate")
		return nil, false
	}

	text, textBox, ok := extractDailyEventGoToEntryText(ctx, arg)
	if !ok {
		log.Warn().Str("component", dailyEventGoToRecognitionName).Msg("candidate hit but entry text missing")
		return nil, false
	}

	newVisited := append(append([]string{}, visited...), text)
	if err := saveDailyEventGoToVisited(ctx, nodeName, newVisited); err != nil {
		log.Error().Err(err).Str("component", dailyEventGoToRecognitionName).Str("text", text).Msg("save attach.visited failed")
		return nil, false
	}

	detailJSON, _ := json.Marshal(dailyEventGoToDetail{Text: text})
	log.Info().
		Str("component", dailyEventGoToRecognitionName).
		Str("text", text).
		Interface("box", textBox).
		Strs("visited", newVisited).
		Msg("selected unread event entry")

	return &maa.CustomRecognitionResult{
		Box:    textBox,
		Detail: string(detailJSON),
	}, true
}

func extractDailyEventGoToEntryText(ctx *maa.Context, arg *maa.CustomRecognitionArg) (string, maa.Rect, bool) {
	// ItemText.roi 已相对 EntryUnread；Candidate 命中后直接再跑一次取文案与点击框
	ocrDetail, err := ctx.RunRecognition(dailyEventGoToEntryNameNode, arg.Img, map[string]any{
		dailyEventGoToEntryNameNode: map[string]any{
			"expected": []string{".{3,}"},
		},
	})
	if err != nil || ocrDetail == nil || !ocrDetail.Hit || ocrDetail.Results == nil || len(ocrDetail.Results.Filtered) == 0 {
		return "", maa.Rect{}, false
	}
	ocrResult, ok := ocrDetail.Results.Filtered[0].AsOCR()
	if !ok {
		return "", maa.Rect{}, false
	}
	text := strings.TrimSpace(ocrResult.Text)
	if text == "" {
		return "", maa.Rect{}, false
	}
	return text, ocrDetail.Box, true
}

func loadDailyEventGoToVisited(ctx *maa.Context, nodeName string) ([]string, error) {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Attach dailyEventGoToAttach `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(wrapper.Attach.Visited))
	seen := make(map[string]struct{}, len(wrapper.Attach.Visited))
	for _, name := range wrapper.Attach.Visited {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out, nil
}

func saveDailyEventGoToVisited(ctx *maa.Context, nodeName string, visited []string) error {
	return ctx.OverridePipeline(map[string]any{
		nodeName: map[string]any{
			"attach": map[string]any{
				dailyEventGoToAttachVisited: visited,
			},
		},
	})
}

func buildDailyEventGoToEntryExpected(visited []string) string {
	escaped := make([]string, 0, len(visited))
	for _, name := range visited {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		escaped = append(escaped, regexp.QuoteMeta(trimmed))
	}
	if len(escaped) == 0 {
		return ".{3,}"
	}
	return fmt.Sprintf("^(?!(?:%s)$).{3,}$", strings.Join(escaped, "|"))
}
