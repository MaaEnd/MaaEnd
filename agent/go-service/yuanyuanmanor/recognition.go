package yuanyuanmanor

import (
	"encoding/json"
	"image"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// menuReading 是 InnkeepingMenuRecognition 写入 Detail、InnkeepingMenuDecide 读回的识别结果载体。
type menuReading struct {
	Tab   MenuTab `json:"tab"`
	Count int     `json:"count"`
}

type menuTabCandidate struct {
	tab  MenuTab
	node string
}

var menuTabCandidates = [5]menuTabCandidate{
	{tab: TabEntree, node: nodeInnkeepingMenuSwitchTabEntreeSuccess},
	{tab: TabSide, node: nodeInnkeepingMenuSwitchTabSideSuccess},
	{tab: TabDessert, node: nodeInnkeepingMenuSwitchTabDessertSuccess},
	{tab: TabSoup, node: nodeInnkeepingMenuSwitchTabSoupSuccess},
	{tab: TabDrinks, node: nodeInnkeepingMenuSwitchTabDrinksSuccess},
}

var _ maa.CustomRecognitionRunner = &MenuRecognition{}

// MenuRecognition 识别菜单设定界面当前所处页签与左下角已选菜品/饮品数量。
type MenuRecognition struct{}

// Run 识别当前页签与左下角数量，序列化为 JSON 经 Detail 传给 InnkeepingMenuDecide。
func (r *MenuRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", component).Msg("menu recognition context, arg or image is nil")
		return nil, false
	}

	tab, ok := recognizeMenuTab(ctx, arg.Img)
	if !ok {
		log.Warn().Str("component", component).Msg("failed to recognize current menu tab")
		return nil, false
	}

	count, ok := recognizeMenuCount(ctx, arg.Img, tab)
	if !ok {
		log.Warn().Str("component", component).Str("tab", string(tab)).Msg("failed to recognize current menu item count")
		return nil, false
	}

	detailBytes, err := json.Marshal(menuReading{
		Tab:   tab,
		Count: count,
	})
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to marshal menu reading")
		return nil, false
	}

	log.Info().
		Str("component", component).
		Str("tab", string(tab)).
		Int("count", count).
		Msg("menu state recognized")

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailBytes),
	}, true
}

func recognizeMenuTab(ctx *maa.Context, img image.Image) (MenuTab, bool) {
	for _, candidate := range menuTabCandidates {
		detail, err := ctx.RunRecognition(candidate.node, img, nil)
		if err == nil && detail != nil && detail.Hit {
			return candidate.tab, true
		}
	}
	return "", false
}

func menuCountNode(tab MenuTab) string {
	if tab == TabDrinks {
		return nodeInnkeepingMenuDrinksCount
	}
	return nodeInnkeepingMenuDishCount
}

func recognizeMenuCount(ctx *maa.Context, img image.Image, tab MenuTab) (int, bool) {
	countNode := menuCountNode(tab)
	// 计数节点的 expected 在点击前会被覆写为目标值，识别当前值时需临时恢复通配正则。
	detail, err := ctx.RunRecognition(countNode, img, map[string]any{
		countNode: map[string]any{
			"expected": []string{"[0-8]/8"},
		},
	})
	if err != nil || detail == nil {
		return 0, false
	}
	text, ok := allOCRText(detail)
	if !ok {
		return 0, false
	}
	count, ok := parseFirstInt(text)
	if !ok || count < 0 || count > 8 {
		return 0, false
	}
	return count, true
}

func allOCRText(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil || detail.Results == nil {
		return "", false
	}
	results := detail.Results.Filtered
	if len(results) == 0 {
		results = detail.Results.All
	}
	var b strings.Builder
	hit := false
	for _, r := range results {
		if r == nil {
			continue
		}
		ocr, ok := r.AsOCR()
		if !ok || ocr == nil {
			continue
		}
		t := strings.TrimSpace(ocr.Text)
		if t == "" {
			continue
		}
		b.WriteString(t)
		hit = true
	}
	return b.String(), hit
}

func parseFirstInt(s string) (int, bool) {
	var buf strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			buf.WriteRune(r)
		} else if buf.Len() > 0 {
			break
		}
	}
	if buf.Len() == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(buf.String())
	if err != nil {
		return 0, false
	}
	return n, true
}
