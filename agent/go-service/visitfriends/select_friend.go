package visitfriends

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	selectFriendRecognitionName = "VisitFriendsSelectFriendRecognition"
	selectFriendCandidateNode   = "VisitFriendsRecognitionItemWithName"
	selectFriendNameOCRNode     = "VisitFriendsRecognitionItemNameByEnterButton"
	selectFriendAttachVisited   = "visited"
)

// VisitFriendsSelectFriendRecognition 参考 DailyEventGoToRecognition：
// 读取 attach.visited，排除已点好友后识别列表项，返回进船按钮框供 Pipeline Click。
type VisitFriendsSelectFriendRecognition struct{}

var _ maa.CustomRecognitionRunner = &VisitFriendsSelectFriendRecognition{}

type selectFriendParam struct {
	OnlyRemarkFriends bool `json:"only_remark_friends"`
}

type selectFriendDetail struct {
	NameText  string `json:"name_text"`
	ButtonBox []int  `json:"button_box"`
}

func (r *VisitFriendsSelectFriendRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		log.Error().Str("component", selectFriendRecognitionName).Msg("nil context or arg")
		return nil, false
	}

	nodeName := strings.TrimSpace(arg.CurrentTaskName)
	if nodeName == "" {
		log.Error().Str("component", selectFriendRecognitionName).Msg("current task name is empty")
		return nil, false
	}

	var params selectFriendParam
	if raw := strings.TrimSpace(arg.CustomRecognitionParam); raw != "" {
		if err := json.Unmarshal([]byte(raw), &params); err != nil {
			log.Error().Err(err).Str("component", selectFriendRecognitionName).Msg("failed to parse custom_recognition_param")
			return nil, false
		}
	}

	visited, err := loadSelectFriendVisited(ctx, nodeName)
	if err != nil {
		log.Error().Err(err).Str("component", selectFriendRecognitionName).Str("node", nodeName).Msg("load attach.visited failed")
		return nil, false
	}

	// 与 DailyEventGoTo 一样覆盖 OCR expected，减少已访问命中；
	// And 子结果按按钮/名 zip，负向预测若导致两侧数量不一致会 miss，届时 Go 侧仍按 normalize(visited) 再过滤。
	expected := buildSelectFriendExpected(visited)
	if err := ctx.OverridePipeline(map[string]any{
		selectFriendNameOCRNode: map[string]any{
			"expected": []string{expected},
		},
	}); err != nil {
		log.Error().Err(err).Str("component", selectFriendRecognitionName).Msg("override name OCR expected failed")
		return nil, false
	}

	detail, err := ctx.RunRecognition(selectFriendCandidateNode, arg.Img)
	if err != nil {
		log.Error().Err(err).Str("component", selectFriendRecognitionName).Str("node", selectFriendCandidateNode).Msg("RunRecognition failed")
		return nil, false
	}
	if detail == nil || !detail.Hit || detail.CombinedResult == nil || len(detail.CombinedResult) < 2 {
		log.Info().Str("component", selectFriendRecognitionName).Strs("visited", visited).Msg("no friend candidate")
		return nil, false
	}

	buttonHits, nameHits, ok := parseSelectFriendCombinedHits(detail)
	if !ok {
		return nil, false
	}
	if len(buttonHits) != len(nameHits) {
		log.Warn().
			Str("component", selectFriendRecognitionName).
			Int("buttons", len(buttonHits)).
			Int("names", len(nameHits)).
			Msg("button/name count mismatch after expected override, retry with base expected")
		if err := ctx.OverridePipeline(map[string]any{
			selectFriendNameOCRNode: map[string]any{
				"expected": []string{".*#.*"},
			},
		}); err != nil {
			log.Error().Err(err).Str("component", selectFriendRecognitionName).Msg("reset name OCR expected failed")
			return nil, false
		}
		detail, err = ctx.RunRecognition(selectFriendCandidateNode, arg.Img)
		if err != nil {
			log.Error().Err(err).Str("component", selectFriendRecognitionName).Msg("retry RunRecognition failed")
			return nil, false
		}
		if detail == nil || !detail.Hit || detail.CombinedResult == nil || len(detail.CombinedResult) < 2 {
			log.Info().Str("component", selectFriendRecognitionName).Strs("visited", visited).Msg("no friend candidate on retry")
			return nil, false
		}
		buttonHits, nameHits, ok = parseSelectFriendCombinedHits(detail)
		if !ok || len(buttonHits) != len(nameHits) {
			log.Warn().
				Str("component", selectFriendRecognitionName).
				Int("buttons", len(buttonHits)).
				Int("names", len(nameHits)).
				Msg("button/name count still mismatch")
			return nil, false
		}
	}

	var selected *selectFriendDetail
	for i := range nameHits {
		rawName := strings.TrimSpace(nameHits[i].Text)
		if rawName == "" {
			continue
		}
		if params.OnlyRemarkFriends && !friendNameHasRemark(rawName) {
			log.Debug().Str("component", selectFriendRecognitionName).Str("name", rawName).Msg("no remark, skip")
			continue
		}

		name := normalizeFriendName(rawName)
		if selectFriendVisitedContains(visited, name) {
			log.Debug().Str("component", selectFriendRecognitionName).Str("name", name).Msg("already visited, skip")
			continue
		}
		if len(buttonHits[i].Box) < 4 {
			log.Warn().Str("component", selectFriendRecognitionName).Int("index", i).Msg("invalid button box")
			continue
		}

		selected = &selectFriendDetail{
			NameText:  name,
			ButtonBox: append([]int(nil), buttonHits[i].Box...),
		}
		break
	}
	if selected == nil {
		log.Info().Str("component", selectFriendRecognitionName).Strs("visited", visited).Msg("no unvisited friend on screen")
		return nil, false
	}

	newVisited := append(append([]string{}, visited...), selected.NameText)
	if err := saveSelectFriendVisited(ctx, nodeName, newVisited); err != nil {
		log.Error().Err(err).Str("component", selectFriendRecognitionName).Str("name", selected.NameText).Msg("save attach.visited failed")
		return nil, false
	}

	detailJSON, _ := json.Marshal(selected)
	log.Info().
		Str("component", selectFriendRecognitionName).
		Str("name", selected.NameText).
		Ints("button_box", selected.ButtonBox).
		Strs("visited", newVisited).
		Msg("selected friend to click")

	return &maa.CustomRecognitionResult{
		Box:    maa.Rect{selected.ButtonBox[0], selected.ButtonBox[1], selected.ButtonBox[2], selected.ButtonBox[3]},
		Detail: string(detailJSON),
	}, true
}

type selectFriendOCRHit struct {
	Box  []int  `json:"box"`
	Text string `json:"text"`
}

func parseSelectFriendCombinedHits(detail *maa.RecognitionDetail) (buttons, names []selectFriendOCRHit, ok bool) {
	var buttonJSON, nameJSON struct {
		Filtered []selectFriendOCRHit `json:"filtered"`
	}
	// CombinedResult[0]=进船按钮 TemplateMatch，[1]=名称 OCR；Results.Best 为空时只能走 DetailJson。
	if err := json.Unmarshal([]byte(detail.CombinedResult[0].DetailJson), &buttonJSON); err != nil {
		log.Error().Err(err).Str("component", selectFriendRecognitionName).Msg("parse button detail json")
		return nil, nil, false
	}
	if err := json.Unmarshal([]byte(detail.CombinedResult[1].DetailJson), &nameJSON); err != nil {
		log.Error().Err(err).Str("component", selectFriendRecognitionName).Msg("parse name detail json")
		return nil, nil, false
	}
	return buttonJSON.Filtered, nameJSON.Filtered, true
}

func friendNameHasRemark(name string) bool {
	return strings.Contains(name, "(") || strings.Contains(name, "（")
}

func loadSelectFriendVisited(ctx *maa.Context, nodeName string) ([]string, error) {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Attach struct {
			Visited []string `json:"visited"`
		} `json:"attach"`
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
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out, nil
}

func saveSelectFriendVisited(ctx *maa.Context, nodeName string, visited []string) error {
	return ctx.OverridePipeline(map[string]any{
		nodeName: map[string]any{
			"attach": map[string]any{
				selectFriendAttachVisited: visited,
			},
		},
	})
}

func selectFriendVisitedContains(visited []string, name string) bool {
	for _, v := range visited {
		if v == name {
			return true
		}
	}
	return false
}

func buildSelectFriendExpected(visited []string) string {
	escaped := make([]string, 0, len(visited))
	for _, name := range visited {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		escaped = append(escaped, regexp.QuoteMeta(trimmed))
	}
	if len(escaped) == 0 {
		return ".*#.*"
	}
	// 与 DailyEventGoTo 相同：用负向预测排除已访问；Go 侧仍会再按 normalize 过滤一层。
	return fmt.Sprintf("^(?!(?:%s)$).*#.*", strings.Join(escaped, "|"))
}
