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
	dailyEventGoToAttachVisited   = "visited"
)

var dailyEventGoToEntryNameNodes = []string{
	"DailyEventRecognitionItemTextByRedDot",
	"DailyEventRecognitionItemTextByNew",
}

// DailyEventGoToRecognition 读取 attach.visited，排除已点入口后调用外部节点
// DailyEventGoToCandidate（红点入口 ∨ NEW入口）完成识别，并从 Or 命中结果提取点击框与文案。
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
	override := make(map[string]any, len(dailyEventGoToEntryNameNodes))
	for _, entryNode := range dailyEventGoToEntryNameNodes {
		override[entryNode] = map[string]any{
			"expected": []string{expected},
		}
	}
	if err := ctx.OverridePipeline(override); err != nil {
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

	text, ok := extractDailyEventGoToEntryText(detail)
	if !ok {
		log.Warn().Str("component", dailyEventGoToRecognitionName).Msg("or hit but entry text missing in results")
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
		Interface("box", detail.Box).
		Strs("visited", newVisited).
		Msg("selected unread event entry")

	return &maa.CustomRecognitionResult{
		Box:    detail.Box,
		Detail: string(detailJSON),
	}, true
}

// extractDailyEventGoToEntryText 从 Or(And) 命中结果中提取入口名。
// And/Or 的 Results 为空，文案在 CombinedResult 里对应 ItemText 子节点。
func extractDailyEventGoToEntryText(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil {
		return "", false
	}
	for _, name := range dailyEventGoToEntryNameNodes {
		child := findDailyEventRecognitionDetailByName(detail, name)
		if child == nil {
			continue
		}
		if text, ok := ocrTextFromRecognitionResults(child.Results); ok {
			return text, true
		}
	}
	return "", false
}

func findDailyEventRecognitionDetailByName(detail *maa.RecognitionDetail, targetName string) *maa.RecognitionDetail {
	if detail == nil {
		return nil
	}
	if detail.Name == targetName {
		return detail
	}
	for _, child := range detail.CombinedResult {
		if found := findDailyEventRecognitionDetailByName(child, targetName); found != nil {
			return found
		}
	}
	return nil
}

func ocrTextFromRecognitionResults(results *maa.RecognitionResults) (string, bool) {
	if results == nil {
		return "", false
	}
	tryOCR := func(result *maa.RecognitionResult) (string, bool) {
		if result == nil {
			return "", false
		}
		ocrResult, ok := result.AsOCR()
		if !ok {
			return "", false
		}
		text := strings.TrimSpace(ocrResult.Text)
		return text, text != ""
	}
	if text, ok := tryOCR(results.Best); ok {
		return text, true
	}
	for _, result := range results.Filtered {
		if text, ok := tryOCR(result); ok {
			return text, true
		}
	}
	for _, result := range results.All {
		if text, ok := tryOCR(result); ok {
			return text, true
		}
	}
	return "", false
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
