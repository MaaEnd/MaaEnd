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

// GameState 是识别器写进 Detail、Decide 读回的 JSON 载体。
type GameState struct {
	State solver.State `json:"state"`

	// HandRaw 仅用于展示：推导路径没有槽位级识别结果，这里给点数升序的合成值。
	HandRaw [5]int `json:"handRaw"`
}

var _ maa.CustomRecognitionRunner = &BoardRecognition{}

// BoardRecognition 每步识别当前局面并组装 GameState：牌库由 RecognizeFullDeck 缓存，手牌由 Deck - remainDeck 推导。
type BoardRecognition struct{}

// Run 识别当前局面；关键字段任一读不到即中止，不在缺失信息上做决策。
func (r *BoardRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", component).Msg("custom recognition arg or image is nil")
		return nil, false
	}

	deck, ok := roundState.Deck()
	if !ok {
		log.Error().Str("component", component).Msg("deck cache missing")
		return nil, false
	}

	remainDeck, ok := recognizeDeck(ctx, arg.Img)
	if !ok {
		return nil, recognitionFailed(ctx, "牌库 OCR 失败")
	}

	remainCalc, ok := recognizeCount(ctx, arg.Img, nodeRemainCalcText)
	if !ok {
		return nil, recognitionFailed(ctx, "剩余演算次数 OCR 失败")
	}

	remainDouble, ok := recognizeCount(ctx, arg.Img, nodeRemainDoubleText)
	if !ok {
		return nil, recognitionFailed(ctx, "剩余翻倍次数 OCR 失败")
	}

	gs, ok := deriveGameState(stepReadings{
		deck:         deck,
		remainDeck:   remainDeck,
		remainCalc:   remainCalc,
		remainAband:  roundState.Aband(),
		remainDouble: remainDouble,
		isDoubled:    recognizeIsDoubled(ctx, arg.Img),
	})
	if !ok {
		log.Error().
			Str("component", component).
			Ints("deck", deck[:]).
			Ints("remainDeck", remainDeck[:]).
			Msg("derived hand invalid")
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
		Msg("game state recognized")

	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detailBytes)}, true
}

// stepReadings 是单步识别到的原始读数，喂给 deriveGameState 做纯推导。
type stepReadings struct {
	deck         [5]int
	remainDeck   [5]int
	remainCalc   int
	remainAband  int
	remainDouble int
	isDoubled    bool
}

// deriveGameState 把读数推导为 GameState。演算次数 +1：屏幕显示的是进行中这局之外的剩余，求解器把这局算作可用。
// 唯一失败面是 Deck - remainDeck 矛盾（负值或总张数 > 5），两个读数必有一个读错，不在矛盾信息上做决策；
// 其余自洽但越界的读数原样送入，由求解器判为不可达。
func deriveGameState(r stepReadings) (GameState, bool) {
	hand, ok := deriveHand(r.deck, r.remainDeck)
	if !ok {
		return GameState{}, false
	}

	return GameState{
		State: solver.State{
			RemainCalc:   r.remainCalc + 1,
			RemainAband:  r.remainAband,
			RemainDouble: r.remainDouble,
			IsDoubled:    r.isDoubled,
			Hand:         hand,
		},
		HandRaw: synthesizeHandRaw(hand),
	}, true
}

func validHand(hand [5]int) bool {
	total := 0
	for _, c := range hand {
		if c < 0 {
			return false
		}
		total += c
	}
	return total <= 5
}

// deriveHand 由总牌库与当前剩余库存推导手牌：牌库面板显示的是抽走后的库存，差值即手牌。
// ok=false 表示两个读数矛盾（负值或总张数超 5）。
func deriveHand(deck, remainDeck [5]int) (hand [5]int, ok bool) {
	for i := range hand {
		hand[i] = deck[i] - remainDeck[i]
	}
	return hand, validHand(hand)
}

func synthesizeHandRaw(hand [5]int) (handRaw [5]int) {
	slot := 0
	for point := 1; point <= 5 && slot < 5; point++ {
		for c := 0; c < hand[point-1] && slot < 5; c++ {
			handRaw[slot] = point
			slot++
		}
	}
	return handRaw
}

// recognitionFailed 是关键字段识别失败的统一出口：记日志 + focus 提示 + 返回 false。
func recognitionFailed(ctx *maa.Context, reason string) bool {
	log.Warn().Str("component", component).Str("reason", reason).Msg("recognition failed, aborting task")
	maafocus.Print(ctx, i18n.T("trialofswordmancy.recognition_failed"))
	return false
}

// recognizeDeck 读牌库整列 OCR，再按 DeckCount1-5 的 ROI 分出各点数的剩余库存，任一读不到数字即失败。
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

// recognizeHand 跑 HandPoint1-5 整行模板，再按 HandPosition1-5 的 ROI 归到各槽位；
// 同槽命中多个点数时取最高分，全没中即空槽。
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

func recognizeIsDoubled(ctx *maa.Context, img image.Image) bool {
	return runTemplateHit(ctx, img, nodeIsDoubled)
}

// recognizeFullDeck 完整识别一次牌库：总牌量 = 剩余库存 + 已抽手牌，因为牌库面板显示的是剩余库存。
func recognizeFullDeck(ctx *maa.Context, img image.Image) (deck, remainDeck, handCounts, handRaw [5]int, ok bool) {
	remainDeck, ok = recognizeDeck(ctx, img)
	if !ok {
		return
	}
	handCounts, handRaw = recognizeHand(ctx, img)
	for i := 0; i < 5; i++ {
		deck[i] = remainDeck[i] + handCounts[i]
	}
	return
}

var _ maa.CustomRecognitionRunner = &FullDeckRecognition{}

// FullDeckRecognition 完整识别牌库并缓存到 RoundState，一轮演算只需在轮次开始时执行一次。
type FullDeckRecognition struct{}

func (r *FullDeckRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
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

	log.Info().
		Str("component", component).
		Ints("deck", deck[:]).
		Ints("remainDeck", remainDeck[:]).
		Ints("hand", handCounts[:]).
		Ints("handRaw", handRaw[:]).
		Msg("deck recognized and cached")

	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detailBytes)}, true
}

// deckDetail 是 FullDeckRecognition 写进 Detail 的观测载体，仅日志/调试用。
type deckDetail struct {
	Deck       [5]int `json:"deck"`
	RemainDeck [5]int `json:"remainDeck"`
	Hand       [5]int `json:"hand"`
	HandRaw    [5]int `json:"handRaw"`
}

var _ maa.CustomRecognitionRunner = &DeckRecognition{}

// DeckRecognition 读当前剩余牌库库存，连同本轮缓存的总牌库一起经 Detail 传给 FreeCalcDecide。
type DeckRecognition struct{}

func (r *DeckRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", component).Msg("deck recognition arg or image is nil")
		return nil, false
	}

	deck, ok := roundState.Deck()
	if !ok {
		log.Error().Str("component", component).Msg("deck cache missing")
		return nil, false
	}

	remainDeck, ok := recognizeDeck(ctx, arg.Img)
	if !ok {
		return nil, recognitionFailed(ctx, "牌库 OCR 失败")
	}

	detailBytes, err := json.Marshal(deckReading{Deck: deck, RemainDeck: remainDeck})
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to marshal deck reading")
		return nil, false
	}

	log.Info().
		Str("component", component).
		Ints("deck", deck[:]).
		Ints("remainDeck", remainDeck[:]).
		Msg("remaining deck recognized")

	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detailBytes)}, true
}

// deckReading 是 DeckRecognition 写进 Detail 的观测载体：总牌库 + 当前剩余库存，手牌由两者差值推导。
type deckReading struct {
	Deck       [5]int `json:"deck"`
	RemainDeck [5]int `json:"remainDeck"`
}

var _ maa.CustomRecognitionRunner = &RemainAbandonRecognition{}

// RemainAbandonRecognition 从已显示的放弃确认弹窗识别剩余放弃次数，只读文本并更新缓存。
type RemainAbandonRecognition struct{}

func (r *RemainAbandonRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
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
		text, ok = ocrNodeText(ctx, arg.Img, nodeAbandonPopupText)
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
	detail, err := ctx.RunRecognition(nodeAbandonPopupExhausted, arg.Img, nil)
	if err != nil || detail == nil {
		log.Warn().Err(err).Str("component", component).Msg("aband exhausted ColorMatch failed")
		return false, false
	}
	return detail.Hit, true
}

// ocrNodeText 拼接节点 ROI 内全部 OCR 框的文本：ppocrv5 常把一行切成多个框，只取 Best 会丢数字与关键词。
func ocrNodeText(ctx *maa.Context, img image.Image, nodeName string) (string, bool) {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil || detail == nil {
		return "", false
	}
	return allOCRText(detail)
}

func runTemplateHit(ctx *maa.Context, img image.Image, nodeName string) bool {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil || detail == nil {
		return false
	}
	return detail.Hit
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

func rectContains(outer, inner maa.Rect) bool {
	return inner.X() >= outer.X() &&
		inner.Y() >= outer.Y() &&
		inner.X()+inner.Width() <= outer.X()+outer.Width() &&
		inner.Y()+inner.Height() <= outer.Y()+outer.Height()
}

// parseFirstInt 取字符串里第一段连续数字。
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
