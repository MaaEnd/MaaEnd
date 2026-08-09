package trialofswordmancy

import (
	"encoding/json"
	"image"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

// Recognition 是选剑演武总成识别器：取一张截图 arg.Img，识别当前局面的各状态字段，
// 组装 GameState 后序列化进 CustomRecognitionResult.Detail 交给 Decide 动作。
//
// 每步只做稳定识别（remainDeck OCR），手牌由缓存的 Deck 反推；完整识别（remainDeck + Hand 模板匹配）
// 由 RecognizeDeck 在轮次开始时执行并缓存（见 roundstate.go）。缓存缺失 / 推导越界 → 严格中止，不猜测。
//
// 各字段来源（ROI/模板都在 TrialOfSwordmancyCommon.json 的 [go] 节点里，Go 按名调用 maafw）：
//   - 屏幕态：RewardMode / DrawCard 在场 → 处于抽牌界面。
//   - Deck：RecognizeDeck 缓存的完整识别结果（remainDeck + Hand），决策到开始演算/放弃后重置。
//   - Hand：Deck - remainDeck 推导（校验非负且总张数 ≤ 5）；HandRaw 为点数升序合成值，仅展示。
//   - RemainCalc / RemainDouble：OCR（RemainCalc / RemainDouble 节点）。
//   - RemainAband：RecognizeAband 从放弃弹窗识别后写入的持久化缓存。
//   - IsDoubled：模板匹配（IsDoubled 节点）。
type Recognition struct{}

// Run 执行总成识别。
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", component).Msg("custom recognition arg or image is nil")
		return nil, false
	}

	// —— 关键字段识别：任一读不到即 return false（任务中止），不在错误/缺失信息上做决策 ——

	// 牌库：完整识别（remainDeck + Hand → 总牌量 Deck）由 RecognizeDeck 在轮次开始时执行并缓存。
	// 一轮演算内 Deck 恒定，此处只读 remainDeck（OCR，稳定），反推 Hand = Deck - remainDeck，
	// 不再每步跑不稳定的 Hand 模板匹配。
	// 缓存缺失 = pipeline 漏跑 RecognizeDeck 或轮次切换后未重置 → 严格中止，不猜测。
	deck, cacheOK := roundState.Deck()
	if !cacheOK {
		log.Error().Str("component", component).Msg("deck cache missing: 轮次开始前未执行 RecognizeDeck（或缓存已被开始演算/放弃重置）")
		return nil, false
	}

	remainDeck, remainOK := recognizeDeck(ctx, arg.Img)
	if !remainOK {
		return nil, recognitionFailed(ctx, "牌库 OCR 失败")
	}

	remainCalc, calcOK := recognizeCount(ctx, arg.Img, nodeRemainCalc)
	if !calcOK {
		return nil, recognitionFailed(ctx, "剩余演算次数 OCR 失败")
	}
	remainDouble, doubleOK := recognizeCount(ctx, arg.Img, nodeRemainDouble)
	if !doubleOK {
		return nil, recognitionFailed(ctx, "剩余翻倍次数 OCR 失败")
	}
	isDoubled := recognizeIsDoubled(ctx, arg.Img)

	// 剩余放弃次数由独立的 RecognizeAband 从放弃弹窗识别并缓存。
	// 缓存仍未知时保留 -1，交给求解器判不可达并中止，不在总成识别中执行任何界面操作。
	remainAband := roundState.Aband()

	// 纯推导：手牌 = 缓存 Deck − remainDeck（校验自洽）→ 合成 HandRaw → 组装 GameState
	// （含 +1 偏移规则）。规则与失败语义的完整说明见 roundstate.go deriveGameState。
	gs, ok := deriveGameState(stepReadings{
		deck:         deck,
		remainDeck:   remainDeck,
		remainCalc:   remainCalc,
		remainAband:  remainAband,
		remainDouble: remainDouble,
		isDoubled:    isDoubled,
	})
	if !ok {
		log.Error().
			Str("component", component).
			Ints("deck", deck[:]).
			Ints("remainDeck", remainDeck[:]).
			Msg("derived hand invalid: remainDeck 读数与缓存 Deck 不一致")
		return nil, false
	}

	detailBytes, err := json.Marshal(gs)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to marshal game state")
		return nil, false
	}

	log.Info().
		Str("component", component).
		Int("remainCalc", gs.State.RemainCalc).
		Int("remainAband", gs.State.RemainAband).
		Int("remainDouble", gs.State.RemainDouble).
		Bool("isDoubled", gs.State.IsDoubled).
		Ints("hand", gs.State.Hand[:]).
		Ints("handRaw", gs.HandRaw[:]).
		Str("overflowMode", gs.Config.OverflowMode.String()).
		Msg("game state recognized")

	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detailBytes)}, true
}

// recognitionFailed 关键字段识别失败的统一出口：记日志 + focus「识别失败」+ 返回 false。
// 任一关键字段（牌库/演算次数/翻倍次数）读不到都走这里——读不到就不在错误信息上做决策，让任务中止。
// （放弃次数缓存未知不在此中止，见 Run 内注释；牌库缓存缺失 / 推导越界走 log.Error 严格中止，不打 focus。）
func recognitionFailed(ctx *maa.Context, reason string) bool {
	log.Warn().Str("component", component).Str("reason", reason).Msg("recognition failed, aborting task")
	maafocus.Print(ctx, i18n.T("trialofswordmancy.recognition_failed"))
	return false
}

// recognizeHand 跑 HandPoint1-5 五个整行模板节点，再按 HandPosition1-5 ROI 筛选各槽点数。
// 同一槽命中多个点数模板时取最高分；都没中则为空槽。
// 仅供 RecognizeDeck 的完整识别使用（总成识别每步走推导路径，不再调用）。
func recognizeHand(ctx *maa.Context, img image.Image) (handCounts [5]int, handRaw [5]int) {
	var rois [5]maa.Rect
	for i := range rois {
		roi, err := nodeROI(ctx, nodeHandPositionPrefix+strconv.Itoa(i+1))
		if err != nil {
			return handCounts, handRaw
		}
		rois[i] = roi
	}

	var bestScores [5]float64
	for point := 1; point <= 5; point++ {
		detail, err := ctx.RunRecognition(nodeHandPointPrefix+strconv.Itoa(point), img, nil)
		if err != nil || detail == nil || detail.Results == nil {
			continue
		}
		for _, result := range detail.Results.Filtered {
			if result == nil {
				continue
			}
			tm, ok := result.AsTemplateMatch()
			if !ok || tm == nil {
				continue
			}
			for slot, roi := range rois {
				if rectContains(roi, tm.Box) && tm.Score > bestScores[slot] {
					bestScores[slot] = tm.Score
					handRaw[slot] = point
					break
				}
			}
		}
	}

	for _, point := range handRaw {
		if point != 0 {
			handCounts[point-1]++
		}
	}
	return handCounts, handRaw
}

// recognizeCount 跑一个 OCR 节点，取识别文本里第一段连续数字（兼容 "2"、"2/3"、"剩余2次"）。
func recognizeCount(ctx *maa.Context, img image.Image, nodeName string) (int, bool) {
	text, ok := ocrNodeText(ctx, img, nodeName)
	if !ok {
		return 0, false
	}
	return parseFirstInt(text)
}

// recognizeDeck 跑一次 Deck OCR 读牌库整列，再按 DeckCount1-5 的 ROI 筛选各点数「剩余库存」。
// 任一 ROI 读不到数字则整体失败。
func recognizeDeck(ctx *maa.Context, img image.Image) ([5]int, bool) {
	detail, err := ctx.RunRecognition(nodeDeck, img, nil)
	if err != nil || detail == nil || detail.Results == nil {
		return [5]int{}, false
	}

	var rois [5]maa.Rect
	for i := range rois {
		roi, err := nodeROI(ctx, nodeDeckCountPrefix+strconv.Itoa(i+1))
		if err != nil {
			return [5]int{}, false
		}
		rois[i] = roi
	}

	var texts [5]strings.Builder
	for _, result := range detail.Results.Filtered {
		if result == nil {
			continue
		}
		ocr, ok := result.AsOCR()
		if !ok || ocr == nil {
			continue
		}
		for i, roi := range rois {
			if rectContains(roi, ocr.Box) {
				texts[i].WriteString(strings.TrimSpace(ocr.Text))
				break
			}
		}
	}

	var deck [5]int
	for i := range deck {
		n, ok := parseFirstInt(texts[i].String())
		if !ok {
			return [5]int{}, false
		}
		deck[i] = n
	}
	return deck, true
}

// recognizeFullDeck 完整识别一次牌库：remainDeck OCR + Hand 模板匹配，推导总牌量 Deck = remainDeck + Hand。
// 由 RecognizeDeck 节点调用并缓存；任一环节失败返回 ok=false——调用方中止，不写半截缓存。
func recognizeFullDeck(ctx *maa.Context, img image.Image) (deck, remainDeck, handCounts, handRaw [5]int, ok bool) {
	remainDeck, ok = recognizeDeck(ctx, img)
	if !ok {
		return
	}
	handCounts, handRaw = recognizeHand(ctx, img)
	// 牌库面板显示的是「剩余库存」（抽一张即递减）；求解器的 Deck 是「总牌量」——它自己按 Deck-Hand 推剩余
	// （见 solver/state.go 的 remain = Deck - Hand）。故总牌量 = 剩余读数 + 已抽手牌。
	// 否则抽牌后 remaining < hand，求解器会判手牌超牌库 → 不可达（实测 322 手牌 + 牌库读到 1 个点数2 即此因）。
	for i := 0; i < 5; i++ {
		deck[i] = remainDeck[i] + handCounts[i]
	}
	return
}

var _ maa.CustomRecognitionRunner = &DeckRecognition{}

// DeckRecognition 是完整牌库识别器：一次读 remainDeck（OCR）+ Hand（模板），推导总牌量 Deck 并缓存。
// 一轮演算内 Deck 恒定，只需在轮次开始时执行一次；之后总成识别只读 remainDeck 反推 Hand，
// 跳过不稳定的 Hand 模板识别。决策到开始演算/放弃演算后由 DecideAction 重置缓存（见 roundstate.go）。
// 识别出牌库后随即异步预热求解器（presolve.go preSolveIfNeeded）——牌库是求解配置里最后一个
// 才识别出来的字段，这里是轮次内最早具备全量求解条件的时机，Decide 取用时无需再等。
// 由 pipeline 节点以 custom_recognition 方式调用（TrialOfSwordmancy.RecognizeDeck）。
type DeckRecognition struct{}

// Run 执行完整牌库识别并缓存。
func (r *DeckRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", component).Msg("deck recognition arg or image is nil")
		return nil, false
	}

	deck, remainDeck, handCounts, handRaw, ok := recognizeFullDeck(ctx, arg.Img)
	if !ok {
		return nil, recognitionFailed(ctx, "完整牌库识别失败（remainDeck 或 Hand 读不到）")
	}

	detailBytes, err := json.Marshal(deckDetail{
		Deck:       deck,
		RemainDeck: remainDeck,
		Hand:       handCounts,
		HandRaw:    handRaw,
	})
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to marshal deck detail")
		return nil, false
	}

	roundState.setDeck(deck)

	// 异步预热：全量求解 ~100ms，放后台让 Decide 阻塞取用（presolve.go awaitPreSolve），
	// 这 100ms 不落在任何节点的关键路径上。溢出模式必须与 Decide 最终配置同源
	// （decideOverflowMode），读不到 = pipeline 配置错误，硬中止而非猜默认值预热。
	overflowMode, ok := decideOverflowMode(ctx)
	if !ok {
		return nil, recognitionFailed(ctx, "读取 Decide 节点 overflowMode 失败")
	}
	cfg := solver.DefaultConfig
	cfg.Deck = deck
	cfg.OverflowMode = overflowMode
	preSolveIfNeeded(cfg) // 预热（nil = 缓存已热，Decide 无需等待）；在途条目复用由备忘内部处理

	log.Info().
		Str("component", component).
		Ints("deck", deck[:]).
		Ints("remainDeck", remainDeck[:]).
		Ints("hand", handCounts[:]).
		Ints("handRaw", handRaw[:]).
		Msg("deck recognized and cached")

	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detailBytes)}, true
}

// deckDetail 是 DeckRecognition 写入 Detail 的观测载体（仅日志/调试用，pipeline 与 Decide 不读）。
type deckDetail struct {
	Deck       [5]int `json:"deck"`
	RemainDeck [5]int `json:"remainDeck"`
	Hand       [5]int `json:"hand"`
	HandRaw    [5]int `json:"handRaw"`
}

var _ maa.CustomRecognitionRunner = &AbandRecognition{}

// AbandRecognition 从当前截图中已显示的放弃确认文本识别剩余放弃次数。
// 它只读取文本并更新缓存，不负责打开弹窗、等待界面或关闭弹窗。
type AbandRecognition struct{}

// Run 识别放弃确认弹窗文本，并将剩余放弃次数写入总成识别使用的缓存。
func (r *AbandRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", component).Msg("aband recognition arg or image is nil")
		return nil, false
	}

	count := 0
	text := ""
	exhausted, ok := recognizeAbandExhausted(ctx, arg)
	if !ok {
		return nil, false
	}
	if !exhausted {
		var ok bool
		text, ok = ocrNodeText(ctx, arg.Img, nodeAbandPopup)
		if !ok {
			log.Warn().Str("component", component).Msg("aband popup OCR failed")
			return nil, false
		}

		var parsed bool
		count, parsed = parseFirstInt(text)
		if !parsed {
			log.Warn().Str("component", component).Str("ocr", text).Msg("failed to parse remaining aband count")
			return nil, false
		}
	}

	roundState.setAband(count)
	log.Info().Str("component", component).Int("aband", count).Str("ocr", text).Msg("remaining aband count recognized")

	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: strconv.Itoa(count)}, true
}

func recognizeAbandExhausted(ctx *maa.Context, arg *maa.CustomRecognitionArg) (bool, bool) {
	detail, err := ctx.RunRecognition(nodeAbandExhausted, arg.Img, nil)
	if err != nil || detail == nil {
		log.Warn().Err(err).Str("component", component).Msg("aband exhausted ColorMatch failed")
		return false, false
	}
	return detail.Hit, true
}

func nodeROI(ctx *maa.Context, nodeName string) (maa.Rect, error) {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return maa.Rect{}, err
	}
	var node struct {
		Recognition struct {
			Param struct {
				ROI maa.Rect `json:"roi"`
			} `json:"param"`
		} `json:"recognition"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return maa.Rect{}, err
	}
	return node.Recognition.Param.ROI, nil
}

func rectContains(outer, inner maa.Rect) bool {
	return inner.X() >= outer.X() &&
		inner.Y() >= outer.Y() &&
		inner.X()+inner.Width() <= outer.X()+outer.Width() &&
		inner.Y()+inner.Height() <= outer.Y()+outer.Height()
}

// recognizeIsDoubled 跑 IsDoubled 模板节点，命中即本局已翻倍。
func recognizeIsDoubled(ctx *maa.Context, img image.Image) bool {
	return runTemplateHit(ctx, img, nodeIsDoubled)
}

// ocrNodeText 跑一个 OCR 节点，返回该 ROI 内所有识别框文本的拼接。
// ppocrv5 常把一行文本切成多个识别框（标点、数字往往单独成框），只取 Best 会丢掉关键数字/关键词，
// 故此处拼接全部框，调用方再自行 parseFirstInt / 子串判断。
func ocrNodeText(ctx *maa.Context, img image.Image, nodeName string) (string, bool) {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil || detail == nil {
		return "", false
	}
	return allOCRText(detail)
}

// runTemplateHit 跑一个 TemplateMatch 节点，返回是否命中。
func runTemplateHit(ctx *maa.Context, img image.Image, nodeName string) bool {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil || detail == nil {
		return false
	}
	return detail.Hit
}

// allOCRText 拼接一个识别节点全部 OCR 框的文本（优先 Filtered，空则退回 All），用空串连接。
// 配合 ppocrv5 的切框行为：把被切成多段的文本重新拼回，避免数字/关键词落在非 Best 框里被丢。
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
		if !ok {
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
