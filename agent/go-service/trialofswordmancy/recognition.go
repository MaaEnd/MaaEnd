package trialofswordmancy

import (
	"encoding/json"
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

// Recognition 是选剑演武总成识别器：取一张截图 arg.Img，把当前局面的「全部」状态字段
// 从截图读出（无状态、无记忆），组装 GameState 后序列化进 CustomRecognitionResult.Detail
// 交给 Decide 动作。
//
// 识别策略（「一图多位置」）：所有识别都基于同一 arg.Img。
//   - 屏幕态：RewardMode / DrawCard 在场 → 处于抽牌界面。
//   - Hand：5 个卡槽先判在场（TrialOfSwordmancyEnemyCardN），在场则按点数模板匹配
//     （Point1-5.png）取最高分得到点数值。
//   - RemainCalc / RemainAband / RemainDouble：OCR 读取屏幕上的剩余次数（RunRecognitionDirect）。
//   - IsDoubled：模板匹配「已翻倍」指示（RunRecognitionDirect）。
//   - Overflow：OverflowExclamation 在场（观测字段，不参与求解）。
//
// 无 statetrack：每步都从当前截图重读全部字段，天然支持中断/重启、无漂移。
type Recognition struct{}

// Run 执行总成识别。
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().
			Str("component", component).
			Msg("custom recognition arg or image is nil")
		return nil, false
	}

	// 配置基线：reward/maxDouble 为等级 4 常量；overflowMode 固定 OverflowTwice（硬编码，
	// 不再识别——奖励不按溢出归零）。deck 默认 0，识别成功才覆盖。
	cfg := solver.DefaultConfig
	cfg.Deck = [5]int{} // 默认 0：未识别到牌库时状态退化/不可达 → 安全结束，不用错误的默认牌库

	// 牌库构成每 72h 轮换，必须 OCR 识别（不能硬编码）；未校准则 deck 留 0（见上）。
	if deck, deckOK := recognizeDeck(ctx, arg.Img); deckOK {
		cfg.Deck = deck
	} else {
		log.Warn().
			Str("component", component).
			Msg("deck OCR failed (roiDeckCount not calibrated?); deck stays 0 → state unreachable")
	}

	onCardScreen := r.detectCardScreen(ctx, arg.Img)
	overflow := r.detectOverflow(ctx, arg.Img)

	handCounts, handRaw, handOK := r.recognizeHand(ctx, arg.Img)
	if !handOK {
		log.Warn().
			Str("component", component).
			Msg("hand recognition incomplete (some present cards' point value unreadable); state may be unreachable")
	}

	// 剩余次数 OCR（ROI 未校准时返回 false，对应字段为 0 → Decide 判定不可达并安全结束）。
	remainCalc, calcOK := recognizeCount(ctx, arg.Img, roiRemainCalc)
	remainAband, abandOK := recognizeCount(ctx, arg.Img, roiRemainAband)
	remainDouble, doubleOK := recognizeCount(ctx, arg.Img, roiRemainDouble)
	if !(calcOK && abandOK && doubleOK) {
		log.Warn().
			Str("component", component).
			Bool("calcOK", calcOK).
			Bool("abandOK", abandOK).
			Bool("doubleOK", doubleOK).
			Msg("some remain-count OCR failed (ROI not calibrated?); state may be unreachable")
	}
	isDoubled := recognizeIsDoubled(ctx, arg.Img)

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
		log.Error().
			Err(err).
			Str("component", component).
			Msg("failed to marshal game state")
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

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailBytes),
	}, true
}

// detectCardScreen 判定是否处于奖励演算（抽牌）界面：RewardMode 或 DrawCard 在场即可。
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

// recognizeHand 识别 5 个卡槽的点数。
//
// 返回：handCounts（各点数张数，下标 0=点数1）、handRaw（各槽位点数，下标=槽位，0=空槽）、
// ok（所有在场卡牌的点数是否都成功读出）。
func (r *Recognition) recognizeHand(ctx *maa.Context, img image.Image) (handCounts [5]int, handRaw [5]int, ok bool) {
	ok = true
	for slot := 0; slot < 5; slot++ {
		nodeName := nodeEnemyCardPrefix + strconv.Itoa(slot+1)
		if !runTemplateHit(ctx, img, nodeName) {
			continue // 该槽位无牌
		}
		point, hit := recognizePointValue(ctx, img, slot)
		if !hit {
			ok = false
			log.Warn().
				Str("component", component).
				Int("slot", slot+1).
				Msg("card present but point value unreadable (need Point1-5.png templates)")
			continue
		}
		if point >= 1 && point <= 5 {
			handCounts[point-1]++
		}
		handRaw[slot] = point
	}
	return handCounts, handRaw, ok
}

// recognizeCount 用 OCR 在指定 ROI 读取一个整数（剩余演算/放弃/翻倍次数）。
// 取 OCR 文本里第一段连续数字（兼容 "2"、"2/3"、"剩余2次" 等写法）。
func recognizeCount(ctx *maa.Context, img image.Image, roi [4]int) (int, bool) {
	param := &maa.OCRParam{
		ROI: maa.NewTargetRect(maa.Rect{roi[0], roi[1], roi[2], roi[3]}),
	}
	detail, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, param, img)
	if err != nil || detail == nil || !detail.Hit {
		return 0, false
	}
	return parseFirstInt(bestOCRText(detail))
}

// recognizeDeck OCR 读取牌库构成（各点数 1..5 的库存数）。牌库每 72h 轮换，必须从截图识别。
// 任一点数读不到则整体失败（返回 false），由调用方决定是否回退。
func recognizeDeck(ctx *maa.Context, img image.Image) ([5]int, bool) {
	var deck [5]int
	for i := 0; i < 5; i++ {
		n, ok := recognizeCount(ctx, img, roiDeckCount[i])
		if !ok {
			return [5]int{}, false
		}
		deck[i] = n
	}
	return deck, true
}

// recognizeIsDoubled 用模板匹配判定本局是否已选择翻倍（命中 tplIsDoubled 模板即视为已翻倍）。
func recognizeIsDoubled(ctx *maa.Context, img image.Image) bool {
	param := &maa.TemplateMatchParam{
		ROI:      maa.NewTargetRect(maa.Rect{roiIsDoubled[0], roiIsDoubled[1], roiIsDoubled[2], roiIsDoubled[3]}),
		Template: []string{tplIsDoubled},
	}
	detail, err := ctx.RunRecognitionDirect(maa.RecognitionTypeTemplateMatch, param, img)
	if err != nil || detail == nil {
		return false
	}
	return detail.Hit
}

// runTemplateHit 运行一个既有 TemplateMatch 识别节点，返回是否命中。
func runTemplateHit(ctx *maa.Context, img image.Image, nodeName string) bool {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil || detail == nil {
		return false
	}
	return detail.Hit
}

// recognizePointValue 在指定卡槽 ROI 上对 Point1-5.png 做模板匹配，返回最高分的点数（1-5）。
// 全部未命中返回 (0, false)。
func recognizePointValue(ctx *maa.Context, img image.Image, slot int) (int, bool) {
	roi := cardSlotROI[slot]
	bestPoint := 0
	bestScore := 0.0
	for point := 1; point <= 5; point++ {
		tpl := fmt.Sprintf("%s%d.png", pointTemplatePrefix, point)
		score, hit := runPointTemplateScore(ctx, img, tpl, roi)
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

// runPointTemplateScore override 通用点数节点的 template + roi 后运行识别，返回 (score, hit)。
func runPointTemplateScore(ctx *maa.Context, img image.Image, templatePath string, roi [4]int) (float64, bool) {
	if err := overridePointValueNode(ctx, templatePath, roi); err != nil {
		log.Warn().
			Err(err).
			Str("component", component).
			Str("template", templatePath).
			Msg("failed to override point value node")
		return 0, false
	}
	detail, err := ctx.RunRecognition(nodePointValue, img, nil)
	if err != nil || detail == nil || !detail.Hit {
		return 0, false
	}
	return bestTemplateScore(detail)
}

// overridePointValueNode 动态设置通用点数节点的 template 与 roi。
func overridePointValueNode(ctx *maa.Context, templatePath string, roi [4]int) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	return ctx.OverridePipeline(map[string]any{
		nodePointValue: map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"template": []string{templatePath},
					"roi":      []int{roi[0], roi[1], roi[2], roi[3]},
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

// parseFirstInt 取字符串里第一段连续数字并解析为 int（用于剩余次数 OCR）。
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
