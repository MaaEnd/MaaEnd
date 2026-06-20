package trialofswordmancy

import (
	"encoding/json"
	"fmt"
	"image"
	"strconv"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

// Recognition 是选剑演武总成识别器：取一张截图 arg.Img，识别当前局面的各状态字段，
// 组装 GameState 后序列化进 CustomRecognitionResult.Detail 交给 Decide 动作。
//
// 几乎无状态——除剩余放弃次数外（界面不显示，持久化+探测），其余字段每步都从当前截图重读。
//
// 各字段来源（ROI/模板都在 TrialOfSwordmancyCommon.json 的 [go] 节点里，Go 按名调用 maafw）：
//   - 屏幕态：RewardMode / DrawCard 在场 → 处于抽牌界面。
//   - Hand：5 个手牌位（HandPoint1-5）匹配 Point1-5.png，命中即该槽点数+在场。
//   - Deck：牌库构成 OCR（DeckCount1-5）。
//   - RemainCalc / RemainDouble：OCR（RemainCalc / RemainDouble 节点）。
//   - RemainAband：持久化缓存；未知(-1)时探测（点放弃→OCR 弹窗→取消）。
//   - IsDoubled：模板匹配（IsDoubled 节点）。
//   - Overflow：OverflowExclamation 在场（观测字段，不参与求解）。
type Recognition struct{}

// Run 执行总成识别。
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", component).Msg("custom recognition arg or image is nil")
		return nil, false
	}

	// 配置基线：reward/maxDouble 为等级 4 常量；overflowMode 固定 OverflowTwice（奖励不按溢出归零）。
	// deck 默认 0，识别成功才覆盖（未识别到牌库 → 状态退化/不可达 → 安全结束，不用错误默认）。
	cfg := solver.DefaultConfig
	cfg.Deck = [5]int{}
	if deck, deckOK := recognizeDeck(ctx, arg.Img); deckOK {
		cfg.Deck = deck
	} else {
		log.Warn().Str("component", component).Msg("deck OCR 失败，deck 留 0 → 状态可能不可达")
	}

	onCardScreen := r.detectCardScreen(ctx, arg.Img)
	overflow := r.detectOverflow(ctx, arg.Img)

	handCounts, handRaw, handOK := r.recognizeHand(ctx, arg.Img)
	if !handOK {
		log.Warn().Str("component", component).Msg("手牌点数识别不完整，状态可能不可达")
	}

	remainCalc, calcOK := recognizeCount(ctx, arg.Img, nodeRemainCalc)
	remainDouble, doubleOK := recognizeCount(ctx, arg.Img, nodeRemainDouble)
	if !(calcOK && doubleOK) {
		log.Warn().Str("component", component).Bool("calcOK", calcOK).Bool("doubleOK", doubleOK).Msg("剩余次数 OCR 失败，状态可能不可达")
	}
	isDoubled := recognizeIsDoubled(ctx, arg.Img)

	// 剩余放弃次数：持久化缓存；未知(-1)则在所有识别之后探测（有副作用，故放最后）。
	remainAband := getAband()
	if remainAband < 0 {
		if n := r.probeAband(ctx, arg.Img); n >= 0 {
			remainAband = n
			setAband(n)
		}
	}

	state := solver.State{
		RemainCalc:   remainCalc,
		RemainAband:  remainAband,
		RemainDouble: remainDouble,
		IsDoubled:    isDoubled,
		Hand:         handCounts,
	}
	gs := GameState{
		State:        state,
		Config:       cfg,
		HandRaw:      handRaw,
		OnCardScreen: onCardScreen,
		Overflow:     overflow,
	}

	detailBytes, err := json.Marshal(gs)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to marshal game state")
		return nil, false
	}

	log.Info().
		Str("component", component).
		Int("remainCalc", state.RemainCalc).
		Int("remainAband", state.RemainAband).
		Int("remainDouble", state.RemainDouble).
		Bool("isDoubled", state.IsDoubled).
		Ints("hand", state.Hand[:]).
		Ints("handRaw", handRaw[:]).
		Bool("onCardScreen", onCardScreen).
		Bool("overflow", overflow).
		Str("overflowMode", cfg.OverflowMode.String()).
		Msg("game state recognized")

	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detailBytes)}, true
}

// detectCardScreen 判定是否处于抽牌界面：RewardMode 或 DrawCard 在场。
func (r *Recognition) detectCardScreen(ctx *maa.Context, img image.Image) bool {
	if runTemplateHit(ctx, img, nodeRewardMode) {
		return true
	}
	return runTemplateHit(ctx, img, nodeDrawCard)
}

// detectOverflow 判定是否识别到溢出叹号（爆表）。
func (r *Recognition) detectOverflow(ctx *maa.Context, img image.Image) bool {
	return runTemplateHit(ctx, img, nodeOverflowExclamation)
}

// recognizeHand 识别 5 个手牌位置的点数。每个位置（HandPoint1-5 节点）上匹配 Point1-5.png，
// 最高分模板即该牌点数，同时表示该槽有牌；都没中 → 空槽。
func (r *Recognition) recognizeHand(ctx *maa.Context, img image.Image) (handCounts [5]int, handRaw [5]int, ok bool) {
	ok = true
	for slot := 0; slot < 5; slot++ {
		point, hit := recognizePointValue(ctx, img, slot)
		if !hit {
			continue
		}
		if point >= 1 && point <= 5 {
			handCounts[point-1]++
		}
		handRaw[slot] = point
	}
	return handCounts, handRaw, ok
}

// recognizeCount 跑一个 OCR 节点，取识别文本里第一段连续数字（兼容 "2"、"2/3"、"剩余2次"）。
func recognizeCount(ctx *maa.Context, img image.Image, nodeName string) (int, bool) {
	text, ok := ocrNodeText(ctx, img, nodeName)
	if !ok {
		return 0, false
	}
	return parseFirstInt(text)
}

// recognizeDeck 跑 DeckCount1-5 五个 OCR 节点读牌库构成；任一读不到则整体失败。
func recognizeDeck(ctx *maa.Context, img image.Image) ([5]int, bool) {
	var deck [5]int
	for i := 0; i < 5; i++ {
		n, ok := recognizeCount(ctx, img, nodeDeckCountPrefix+strconv.Itoa(i+1))
		if !ok {
			return [5]int{}, false
		}
		deck[i] = n
	}
	return deck, true
}

// recognizeIsDoubled 跑 IsDoubled 模板节点，命中即本局已翻倍。
func recognizeIsDoubled(ctx *maa.Context, img image.Image) bool {
	return runTemplateHit(ctx, img, nodeIsDoubled)
}

// probeAband 探测剩余放弃次数：点放弃 → 等弹窗 → 截新图 OCR → 点取消回正常界面。
// 返回读到的次数（0-3），读不到返回 -1。有副作用（点击、截屏），仅在 getAband()<0 时调用一次。
func (r *Recognition) probeAband(ctx *maa.Context, img image.Image) int {
	giveUpBox, ok := findNodeBox(ctx, img, nodeGiveUpButton)
	if !ok {
		log.Warn().Str("component", component).Msg("probeAband: 放弃按钮未找到")
		return -1
	}
	if err := clickBox(ctx, giveUpBox); err != nil {
		log.Warn().Err(err).Str("component", component).Msg("probeAband: 点击放弃失败")
		return -1
	}

	time.Sleep(300 * time.Millisecond)
	ctrl := ctx.GetTasker().GetController()
	if ctrl == nil {
		return -1
	}
	ctrl.PostScreencap().Wait()
	fresh, err := ctrl.CacheImage()
	if err != nil || fresh == nil {
		log.Warn().Err(err).Str("component", component).Msg("probeAband: 截屏失败")
		return -1
	}

	text, _ := ocrNodeText(ctx, fresh, nodeAbandPopup)
	count := parseAbandCount(text)

	if cancelBox, ok := findNodeBox(ctx, fresh, nodeCancelButton); ok {
		if err := clickBox(ctx, cancelBox); err != nil {
			log.Warn().Err(err).Str("component", component).Msg("probeAband: 点击取消失败，弹窗可能残留")
		}
	} else {
		log.Warn().Str("component", component).Msg("probeAband: 取消按钮未找到，弹窗可能残留")
	}

	if count < 0 {
		log.Warn().Str("component", component).Str("ocr", text).Msg("probeAband: 未能解析放弃次数")
	} else {
		log.Info().Str("component", component).Int("aband", count).Str("ocr", text).Msg("probeAband: 探测到剩余放弃次数")
	}
	return count
}

// findNodeBox 跑一个识别节点，返回命中框。
func findNodeBox(ctx *maa.Context, img image.Image, nodeName string) (maa.Rect, bool) {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil || detail == nil || !detail.Hit {
		return maa.Rect{}, false
	}
	return detail.Box, true
}

// clickBox 点击给定框中心。
func clickBox(ctx *maa.Context, box maa.Rect) error {
	_, err := ctx.RunActionDirect(maa.ActionTypeClick, &maa.ClickParam{
		Target: maa.NewTargetRect(box),
	}, box, nil)
	return err
}

// ocrNodeText 跑一个 OCR 节点，返回识别文本。
func ocrNodeText(ctx *maa.Context, img image.Image, nodeName string) (string, bool) {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil || detail == nil {
		return "", false
	}
	return bestOCRText(detail), true
}

// parseAbandCount 从放弃弹窗文本解析剩余次数：「已用完/用完」→0，否则取首段数字；读不出→-1。
func parseAbandCount(text string) int {
	if strings.Contains(text, "已用完") || strings.Contains(text, "用完") {
		return 0
	}
	n, ok := parseFirstInt(text)
	if !ok {
		return -1
	}
	return n
}

// runTemplateHit 跑一个 TemplateMatch 节点，返回是否命中。
func runTemplateHit(ctx *maa.Context, img image.Image, nodeName string) bool {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil || detail == nil {
		return false
	}
	return detail.Hit
}

// recognizePointValue 在第 slot 个手牌位上匹配 Point1-5.png，返回最高分的点数（1-5）。
// 槽位 roi 由 HandPoint{slot+1} 节点定，Go 只 override template 逐点取分。
func recognizePointValue(ctx *maa.Context, img image.Image, slot int) (int, bool) {
	nodeName := nodeHandPointPrefix + strconv.Itoa(slot+1)
	bestPoint := 0
	bestScore := 0.0
	for point := 1; point <= 5; point++ {
		tpl := fmt.Sprintf("%s%d.png", pointTemplatePrefix, point)
		score, hit := runHandPointScore(ctx, img, nodeName, tpl)
		if !hit {
			continue
		}
		if score > bestScore {
			bestScore = score
			bestPoint = point
		}
	}
	return bestPoint, bestPoint != 0
}

// runHandPointScore 把 HandPoint 节点的 template override 成指定模板后跑识别，返回 (score, hit)。
func runHandPointScore(ctx *maa.Context, img image.Image, nodeName, templatePath string) (float64, bool) {
	if err := overrideTemplate(ctx, nodeName, templatePath); err != nil {
		log.Warn().Err(err).Str("component", component).Str("template", templatePath).Msg("override hand-point template 失败")
		return 0, false
	}
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil || detail == nil || !detail.Hit {
		return 0, false
	}
	return bestTemplateScore(detail)
}

// overrideTemplate 把某节点的 template（运行时）覆盖成指定模板路径，roi 等保持节点原定义。
func overrideTemplate(ctx *maa.Context, nodeName, templatePath string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	return ctx.OverridePipeline(map[string]any{
		nodeName: map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"template": []string{templatePath},
				},
			},
		},
	})
}

// bestTemplateScore 取模板匹配最佳结果的分数。
func bestTemplateScore(detail *maa.RecognitionDetail) (float64, bool) {
	if detail == nil || detail.Results == nil || detail.Results.Best == nil {
		return 0, false
	}
	tm, ok := detail.Results.Best.AsTemplateMatch()
	if !ok {
		return 0, false
	}
	return tm.Score, true
}

// bestOCRText 取 OCR 最佳结果的文本。
func bestOCRText(detail *maa.RecognitionDetail) string {
	if detail == nil || detail.Results == nil || detail.Results.Best == nil {
		return ""
	}
	if ocr, ok := detail.Results.Best.AsOCR(); ok {
		return strings.TrimSpace(ocr.Text)
	}
	return ""
}

// parseFirstInt 取字符串里第一段连续数字并解析为 int。
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
