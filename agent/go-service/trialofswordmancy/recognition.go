package trialofswordmancy

import (
	"encoding/json"
	"image"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

// Recognition 是选剑演武总成识别器：取一张截图 arg.Img，识别当前局面的各状态字段，
// 组装 GameState 后序列化进 CustomRecognitionResult.Detail 交给 Decide 动作。
//
// 几乎无状态——除剩余放弃次数外（界面不显示，由 RecognizeAband 识别后缓存），其余字段每步都从当前截图重读。
//
// 各字段来源（ROI/模板都在 TrialOfSwordmancyCommon.json 的 [go] 节点里，Go 按名调用 maafw）：
//   - 屏幕态：RewardMode / DrawCard 在场 → 处于抽牌界面。
//   - Hand：HandPoint1-5 分别整行匹配 Point1-5.png，按 HandPosition1-5 ROI 归槽。
//   - Deck：牌库「剩余库存」整列 OCR（Deck，按 DeckCount1-5 ROI 分组，抽牌递减）；总牌量 = 剩余 + 手牌。
//   - RemainCalc / RemainDouble：OCR（RemainCalc / RemainDouble 节点）。
//   - RemainAband：RecognizeAband 从放弃弹窗识别后写入的持久化缓存。
//   - IsDoubled：模板匹配（IsDoubled 节点）。
//   - Overflow：OverflowExclamation 在场（观测字段，不参与求解）。
type Recognition struct{}

// Run 执行总成识别。
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", component).Msg("custom recognition arg or image is nil")
		return nil, false
	}

	// —— 关键字段识别：任一读不到即 return false（任务中止），不在错误/缺失信息上做决策 ——
	cfg := solver.DefaultConfig
	deck, deckOK := recognizeDeck(ctx, arg.Img)
	if !deckOK {
		return nil, r.recognitionFailed(ctx, "牌库 OCR 失败")
	}

	overflow := r.detectOverflow(ctx, arg.Img)
	handCounts, handRaw := r.recognizeHand(ctx, arg.Img)

	// 牌库面板显示的是「剩余库存」（抽一张即递减）；求解器的 Deck 是「总牌量」——它自己按 Deck-Hand 推剩余
	// （见 solver/state.go 的 remain = Deck - Hand）。故总牌量 = 剩余读数 + 已抽手牌。
	// 否则抽牌后 remaining < hand，求解器会判手牌超牌库 → 不可达（实测 322 手牌 + 牌库读到 1 个点数2 即此因）。
	for i := 0; i < 5; i++ {
		cfg.Deck[i] = deck[i] + handCounts[i]
	}

	remainCalc, calcOK := recognizeCount(ctx, arg.Img, nodeRemainCalc)
	if !calcOK {
		return nil, r.recognitionFailed(ctx, "剩余演算次数 OCR 失败")
	}
	remainDouble, doubleOK := recognizeCount(ctx, arg.Img, nodeRemainDouble)
	if !doubleOK {
		return nil, r.recognitionFailed(ctx, "剩余翻倍次数 OCR 失败")
	}
	isDoubled := recognizeIsDoubled(ctx, arg.Img)

	// 剩余放弃次数由独立的 RecognizeAband 从放弃弹窗识别并缓存。
	// 缓存仍未知时保留 -1，交给求解器判不可达并中止，不在总成识别中执行任何界面操作。
	remainAband := getAband()

	// 屏幕的「本日剩余奖励演算次数」显示的是「当前进行中这局之外的剩余」——进入抽牌界面即扣 1，
	// 而求解器把进行中这局也算作可用 → solver = OCR + 1（solver 态空间 RemainCalc 1..3，对应 OCR 0..2）。
	// 仅演算次数有此偏移：放弃/翻倍次数界面显示的就是真实值，直接用。走到这里 calcOK 必为真。
	// 跨天残局那局白送：OCR 读到 3 → RemainCalc=4，超出态空间上界——本处不钳制，原样交给 Decide 直接放弃。
	state := solver.State{
		RemainCalc:   remainCalc + 1,
		RemainAband:  remainAband,
		RemainDouble: remainDouble,
		IsDoubled:    isDoubled,
		Hand:         handCounts,
	}
	gs := GameState{
		State:    state,
		Config:   cfg,
		HandRaw:  handRaw,
		Overflow: overflow,
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
		Bool("overflow", overflow).
		Str("overflowMode", cfg.OverflowMode.String()).
		Msg("game state recognized")

	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detailBytes)}, true
}

// recognitionFailed 关键字段识别失败的统一出口：记日志 + focus「识别失败」+ 返回 false。
// 任一关键字段（牌库/演算次数/翻倍次数）读不到都走这里——读不到就不在错误信息上做决策，让任务中止。
// （放弃次数缓存未知不在此中止，见 Run 内注释。）
func (r *Recognition) recognitionFailed(ctx *maa.Context, reason string) bool {
	log.Warn().Str("component", component).Str("reason", reason).Msg("recognition failed, aborting task")
	maafocus.Print(ctx, "选剑演武：识别失败")
	return false
}

// detectOverflow 判定是否识别到溢出叹号（爆表）。
func (r *Recognition) detectOverflow(ctx *maa.Context, img image.Image) bool {
	return runTemplateHit(ctx, img, nodeOverflowExclamation)
}

// recognizeHand 跑 HandPoint1-5 五个整行模板节点，再按 HandPosition1-5 ROI 筛选各槽点数。
// 同一槽命中多个点数模板时取最高分；都没中则为空槽。
func (r *Recognition) recognizeHand(ctx *maa.Context, img image.Image) (handCounts [5]int, handRaw [5]int) {
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
