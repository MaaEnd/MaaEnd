package trialofswordmancy

import (
	"encoding/json"
	"fmt"
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
// 几乎无状态——除剩余放弃次数外（界面不显示，持久化+探测），其余字段每步都从当前截图重读。
//
// 各字段来源（ROI/模板都在 TrialOfSwordmancyCommon.json 的 [go] 节点里，Go 按名调用 maafw）：
//   - 屏幕态：RewardMode / DrawCard 在场 → 处于抽牌界面。
//   - Hand：5 个手牌位（HandPoint1-5）匹配 Point1-5.png，命中即该槽点数+在场。
//   - Deck：牌库「剩余库存」OCR（DeckCount1-5，抽牌递减）；总牌量 = 剩余 + 手牌。
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

	// —— 关键字段识别：任一读不到即 return false（任务中止），不在错误/缺失信息上做决策 ——
	cfg := solver.DefaultConfig
	deck, deckOK := recognizeDeck(ctx, arg.Img)
	if !deckOK {
		return nil, r.recognitionFailed(ctx, "牌库 OCR 失败")
	}

	onCardScreen := r.detectCardScreen(ctx, arg.Img)
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

	// 剩余放弃次数：持久化缓存；未知(-1)则探测（有副作用，故放最后）。
	// 探测失败不在此 return false——否则 Decide 节点的 20s 识别超时内会反复点击放弃/取消；
	// 改为留 -1 → 求解器判不可达 → Decide 动作 return false（一次性中止，不重试）。
	remainAband := getAband()
	if remainAband < 0 {
		if n := r.probeAband(ctx); n >= 0 {
			remainAband = n
			setAband(n)
		}
	}

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

// recognitionFailed 关键字段识别失败的统一出口：记日志 + focus「识别失败」+ 返回 false。
// 任一关键字段（牌库/演算次数/翻倍次数）读不到都走这里——读不到就不在错误信息上做决策，让任务中止。
// （放奔次数探测失败不在此中止，见 Run 内注释。）
func (r *Recognition) recognitionFailed(ctx *maa.Context, reason string) bool {
	log.Warn().Str("component", component).Str("reason", reason).Msg("recognition failed, aborting task")
	maafocus.Print(ctx, "选剑演武：识别失败")
	return false
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
func (r *Recognition) recognizeHand(ctx *maa.Context, img image.Image) (handCounts [5]int, handRaw [5]int) {
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

// recognizeDeck 跑 DeckCount1-5 五个 OCR 节点读牌库「剩余库存」（抽一张递减一次）；任一读不到则整体失败。
// 返回的是剩余量，调用方需 + hand 还原总牌量（求解器吃总牌量）。
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

// probeAband 探测剩余放弃次数：跑两段 pipeline 子链，go 在中间读一次放弃弹窗 OCR。
//
// 子链定义在 TrialOfSwordmancyCommon.json（TrialOfSwordmancyAbandProbe*）：
//   - ① 点放弃 → 轮询等放弃确认弹窗出现（弹窗留在屏上，子链结束）；
//   - ② 点取消 → 轮询等回抽牌页（EnemyCard1）→ freeze。
//
// 交互/等待/点击全在 pipeline 里——pipeline 的 next 本身就是带重试的轮询（MaaFramework
// PipelineTask::run_next：截图→识别 next 候选→没中就 sleep rate_limit 重试，直到 timeout），
// 等价于以前 go 手写的两个轮询循环。go 只负责触发子链 + 在两段之间取一次 OCR 文本。
// 返回读到的次数（0-3），读不到返回 -1。仅在 getAband()<0 时调用一次。
func (r *Recognition) probeAband(ctx *maa.Context) int {
	// ① 点放弃 → 等弹窗。RunTask 返回即弹窗已在屏上（WaitPopup 命中 CancelButton 后子链结束、空 next）。
	if _, err := ctx.RunTask(nodeAbandProbeClickGiveUp); err != nil {
		log.Warn().Err(err).Str("component", component).Msg("probeAband: 点放弃/等弹窗失败")
		return -1
	}

	// go 取 OCR：弹窗在屏上，截一张图跑放弃弹窗 OCR。
	ctrl := ctx.GetTasker().GetController()
	if ctrl == nil {
		log.Warn().Str("component", component).Msg("probeAband: controller 为空")
		return -1
	}
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		log.Warn().Err(err).Str("component", component).Msg("probeAband: 截屏失败")
		return -1
	}
	text, _ := ocrNodeText(ctx, img, nodeAbandPopup)
	count := parseAbandCount(text)

	// ② 点取消 → 等回抽牌页 → freeze。失败只记日志（次数已读到，界面若残留由上游兜底）。
	if _, err := ctx.RunTask(nodeAbandProbeClickCancel); err != nil {
		log.Warn().Err(err).Str("component", component).Msg("probeAband: 点取消/等回抽牌页失败，界面可能残留")
	}

	if count < 0 {
		log.Warn().Str("component", component).Str("ocr", text).Msg("probeAband: 未能解析放弃次数")
	} else {
		log.Info().Str("component", component).Int("aband", count).Str("ocr", text).Msg("probeAband: 探测到剩余放弃次数")
	}
	return count
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

// 放弃确认弹窗两种形态（原文）：
//   - 有次数：「本日剩余放弃次数x次，放弃将扣除，但不会扣除奖励演算次数，是否确认放弃？」（x ∈ 1..3）
//   - 已耗尽：「本日放弃次数已用完，继续放弃将会扣除1次奖励演算次数，是否确认放弃？」
//
// 两种都含数字、都含「奖励演算次数」「扣除」「是否确认放弃」——这些词无法区分两态。
// 只有「用完 / 继续放弃 / 将会扣除」是耗尽态独有，拿来做耗尽判定。
// 关键陷阱：耗尽态的「1」是「扣除几次奖励演算次数」，不是放弃次数 → 必须先用耗尽标记拦截，
// 否则 parseFirstInt 会把那个「1」误当放弃次数。
var abandExhaustedMarkers = []string{"用完", "继续放弃", "将会扣除"}

// parseAbandCount 从放弃弹窗的拼接文本解析剩余次数。
//   - 耗尽标记命中 → 0
//   - 否则取首段数字（有次数态的唯一数字即放弃次数 x）
//   - 无标记也无数字 → 未知 -1（例如有次数态的数字被 OCR 切错）。不臆断为 0/耗尽：
//     返回 0 会被 setAband 缓存，污染整局决策；返回 -1 由上游留作未知、求解器判不可达中止。
func parseAbandCount(text string) int {
	for _, m := range abandExhaustedMarkers {
		if strings.Contains(text, m) {
			return 0
		}
	}
	if n, ok := parseFirstInt(text); ok {
		return n
	}
	return -1
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
