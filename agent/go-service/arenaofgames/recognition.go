package arenaofgames

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	component     = "ArenaOfGames.CheckHand"
	targetNode    = "ArenaOfGamesDecide"
	dailyHandNode = "ArenaOfGamesDailyHandText"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

// Recognition 识别五个手牌槽并对照勾选的目标牌型给出结论，不执行点击或修改流程。
type Recognition struct{}

type recognitionParam struct {
	Count *int `json:"count,omitempty"`
}

type handSlot struct {
	Type  string  `json:"type"`
	Point int     `json:"point"`
	Score float64 `json:"score"`
}

type handDetail struct {
	Slots    [5]handSlot `json:"slots"`
	Count    int         `json:"count"`
	Category string      `json:"category"`
}

// category 按游戏加成倍率从低到高排序，值越大等级越高，一手牌同时只会有一种生效
// （高倍率覆盖低倍率，如一色的 3+2 记同花而非葫芦）：
// 对子 1.2 < 两对 1.4 < 三条 1.6 < 顺子 1.8 < 葫芦 2 < 同花 4 < 四条 6 < 同花顺 8。
type category int

const (
	catNone category = iota
	catPair
	catTwoPair
	catThreeOfAKind
	catStraight
	catFullHouse
	catFlush
	catFourOfAKind
	catStraightFlush
)

// categoryKeys 与 ArenaOfGamesDailyHandText 节点 attach 的键保持一致，
// dailyHandNames 读取时做键集全等校验，漂移即报错。
var categoryKeys = map[string]category{
	"pair":            catPair,
	"two_pair":        catTwoPair,
	"three_of_a_kind": catThreeOfAKind,
	"straight":        catStraight,
	"full_house":      catFullHouse,
	"flush":           catFlush,
	"four_of_a_kind":  catFourOfAKind,
	"straight_flush":  catStraightFlush,
}

func (c category) name() string {
	for key, value := range categoryKeys {
		if value == c {
			return key
		}
	}
	return ""
}

type recognitionSource interface {
	GetNodeJSON(string) (string, error)
	RunRecognition(string, image.Image, ...any) (*maa.RecognitionDetail, error)
}

// —— 今日好运缓存（借鉴 trialofswordmancy/roundstate.go 的轮次状态）——

// dailyHandState 是「今日好运」牌型的轮次缓存：MaaFramework 回调模型下 Custom 组件无状态，
// 跨识别调用共享只能靠包级实例。当日牌型整日不变，但为免读到旧值，缓存有两道失效：
// 轮次边界（回到空手）重置，跨日（日期戳不符）过期——后者覆盖「进程跨天存活且起手非空」
// 的场景：昨日以保留手牌收尾后无空手心跳，只有日期戳能拦下昨日牌型。
// 无需加锁：MaaFramework 保证任务回调单线程。
type dailyHandState struct {
	category category
	day      int  // 解析时的游戏日（YYYYMMDD，04:00 切日）
	set      bool // 显式标志而非零值哨兵：缓存缺失与缓存空值必须可区分
}

var dailyHand = &dailyHandState{}

// 游戏内每日内容（含今日好运牌型）在本地时间 04:00 轮换，与 creditshopping、
// common/schedule 的游戏日边界保持一致。
const gameDayBoundaryHour = 4

// gameDay 给出「游戏日」（当日 04:00 至次日 04:00 为同一天）的 YYYYMMDD 整数，
// 仅用于缓存过期判断。今日好运按游戏日轮换：00:00-03:59 读到的牌型到 04:00 即过期，
// 若按自然日戳缓存，凌晨跑的任务会把昨日的牌型供给 04:00 之后的整个白天。
func gameDay(now time.Time) int {
	t := now.Local()
	if t.Hour() < gameDayBoundaryHour {
		t = t.AddDate(0, 0, -1)
	}
	return t.Year()*10000 + int(t.Month())*100 + t.Day()
}

func (s *dailyHandState) get(day int) (category, bool) {
	if !s.set || s.day != day {
		return catNone, false
	}
	return s.category, true
}

func (s *dailyHandState) store(c category) {
	s.category = c
	s.day = gameDay(time.Now())
	s.set = true
}

func (s *dailyHandState) reset() {
	if !s.set {
		return
	}
	s.category = catNone
	s.set = false
	log.Debug().Str("component", component).Msg("daily hand cache reset")
}

// Run 从本次截图识别手牌，完整识别且满足 count 时才命中。
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Debug().Str("component", component).Msg("recognition context or image is unavailable")
		return nil, false
	}
	params, err := parseParams(arg.CustomRecognitionParam)
	if err != nil {
		log.Debug().Err(err).Str("component", component).Msg("invalid hand params")
		return nil, false
	}
	// 等待空手（count==0）时先走牌背快速通道：有槽未回背必非空手，省掉 15 次牌面扫描。
	if params.Count != nil && *params.Count == 0 && !handAllBack(ctx, arg.Img) {
		log.Debug().Str("component", component).Msg("hand is not empty yet")
		return nil, false
	}
	detail, err := recognizeHand(ctx, arg.Img)
	if err != nil {
		log.Debug().Err(err).Str("component", component).Msg("hand could not be confirmed")
		return nil, false
	}
	if detail.Count == 0 {
		dailyHand.reset()
	}
	if !params.matches(detail) {
		log.Debug().Str("component", component).
			Int("count", detail.Count).Str("category", detail.Category).
			Msg("hand does not match the requested count")
		return nil, false
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		log.Debug().Err(err).Str("component", component).Msg("failed to encode recognized hand")
		return nil, false
	}
	log.Info().Str("component", component).
		Int("count", detail.Count).Str("category", detail.Category).
		Interface("slots", detail.Slots).Msg("hand matched")
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(raw)}, true
}

// parseParams 解析识别参数：可整体省略（如 Decide 只判定牌面完整性），仅 count 可选。
func parseParams(raw string) (recognitionParam, error) {
	var params recognitionParam
	if strings.TrimSpace(raw) == "" {
		return params, nil
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return params, err
	}
	if params.Count != nil && (*params.Count < 0 || *params.Count > 5) {
		return params, fmt.Errorf("count must be between 0 and 5")
	}
	return params, nil
}

func (p recognitionParam) matches(detail handDetail) bool {
	return p.Count == nil || *p.Count == detail.Count
}

// targetAttach 是 ArenaOfGamesDecide 节点 attach 的结构，由界面勾选的选项写入。
type targetAttach struct {
	HandOfToday   bool `json:"hand_of_today"`
	StraightFlush bool `json:"straight_flush"`
	FourOfAKind   bool `json:"four_of_a_kind"`
	Flush         bool `json:"flush"`
	FullHouse     bool `json:"full_house"`
}

func parseTargetAttach(raw string) (targetAttach, error) {
	var node struct {
		Attach targetAttach `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return targetAttach{}, fmt.Errorf("parse %s attach: %w", targetNode, err)
	}
	return node.Attach, nil
}

func (a targetAttach) categories() map[category]bool {
	targets := make(map[category]bool)
	if a.StraightFlush {
		targets[catStraightFlush] = true
	}
	if a.FourOfAKind {
		targets[catFourOfAKind] = true
	}
	if a.Flush {
		targets[catFlush] = true
	}
	if a.FullHouse {
		targets[catFullHouse] = true
	}
	return targets
}

// resolveTargets 读取勾选的目标牌型，勾选「今日好运」时再并入当日牌型。
// 当日牌型读取失败时降级：仍有勾选的静态目标则告警沿用；一个目标都不剩才报错。
func resolveTargets(src recognitionSource, img image.Image) (map[category]bool, error) {
	raw, err := src.GetNodeJSON(targetNode)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", targetNode, err)
	}
	attach, err := parseTargetAttach(raw)
	if err != nil {
		return nil, err
	}
	targets := attach.categories()
	if attach.HandOfToday {
		daily, err := resolveDailyHand(src, img)
		if err != nil {
			if len(targets) > 0 {
				log.Warn().Err(err).Str("component", component).
					Msg("daily hand unreadable, falling back to the other selected hands")
			} else {
				return nil, err
			}
		} else {
			targets[daily] = true
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no target hand is selected on %s", targetNode)
	}
	return targets, nil
}

// resolveDailyHand 给出当日牌型：当日已缓存则直接返回，否则从截图 OCR 一次并缓存。
func resolveDailyHand(src recognitionSource, img image.Image) (category, error) {
	if cached, ok := dailyHand.get(gameDay(time.Now())); ok {
		return cached, nil
	}
	daily, err := recognizeDailyHand(src, img)
	if err != nil {
		return catNone, err
	}
	dailyHand.store(daily)
	log.Info().Str("component", component).Str("daily_hand", daily.name()).
		Msg("daily hand resolved from screen")
	return daily, nil
}

// recognizeDailyHand OCR「今日好运」的牌型名并映射为牌型。
func recognizeDailyHand(src recognitionSource, img image.Image) (category, error) {
	names, err := dailyHandNames(src)
	if err != nil {
		return catNone, err
	}
	detail, err := src.RunRecognition(dailyHandNode, img)
	if err != nil {
		return catNone, fmt.Errorf("recognize %s: %w", dailyHandNode, err)
	}
	if detail == nil {
		return catNone, fmt.Errorf("recognize %s returned no detail", dailyHandNode)
	}
	if !detail.Hit {
		return catNone, fmt.Errorf("recognize %s returned no hit", dailyHandNode)
	}
	text := allOCRText(detail)
	if text == "" {
		return catNone, fmt.Errorf("recognize %s returned no text", dailyHandNode)
	}
	return matchDailyCategory(text, names)
}

func dailyHandNames(src recognitionSource) (map[string][]string, error) {
	raw, err := src.GetNodeJSON(dailyHandNode)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dailyHandNode, err)
	}
	var node struct {
		Attach map[string][]string `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return nil, fmt.Errorf("parse %s attach: %w", dailyHandNode, err)
	}
	// 键集全等校验：attach 名单是牌型名的唯一源（OCR 节点不写 expected），
	// 键与 categoryKeys 多一个或少一个都会让对应牌型静默失配，启动即拦下。
	for key := range node.Attach {
		if _, ok := categoryKeys[key]; !ok {
			return nil, fmt.Errorf("node %s attach has unknown key %s", dailyHandNode, key)
		}
	}
	names := make(map[string][]string)
	for key := range categoryKeys {
		list := node.Attach[key]
		if len(list) == 0 {
			return nil, fmt.Errorf("node %s attach has no names for %s", dailyHandNode, key)
		}
		names[key] = list
	}
	return names, nil
}

// normalizeHandName 统一小写并去除全部空白（含词间）：OCR 文本与 attach 名单同用本函数
// 归一，「TwoPair」「Two  Pair」「투 페어」等任意空白形态都落到同一名字，不降级误配短名。
func normalizeHandName(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), "")
}

// matchDailyCategory 把 OCR 文本映射为牌型，只认文本中出现的完整名字并取最长，
// 避免「同花顺」被「同花」、「Straight」被「Straight Flush」抢先匹配；
// 等长平局按牌型等级决胜，保证同一文本的映射确定。
func matchDailyCategory(text string, names map[string][]string) (category, error) {
	normalized := normalizeHandName(text)
	var best category
	bestLen := 0
	for key, cat := range categoryKeys {
		for _, name := range names[key] {
			candidate := normalizeHandName(name)
			if candidate == "" {
				continue
			}
			length := len([]rune(candidate))
			if !strings.Contains(normalized, candidate) {
				continue
			}
			if length > bestLen || (length == bestLen && cat > best) {
				best, bestLen = cat, length
			}
		}
	}
	if best == catNone {
		return catNone, fmt.Errorf("daily hand text %q matches no known hand name", text)
	}
	return best, nil
}

// allOCRText 拼接 OCR 节点全部识别框的文本（优先 All，为空再退回 Filtered）。
// expected 是按框过滤的：ppocrv5 把多词牌型名拆成多框时，只有恰好单独可匹配的片段
// 留在 Filtered/Best 里，只取单框会把 "Two Pair" 读成 "Pair" 而误配短名，
// 必须用未过滤的 All 拼回全文。框间以空格连接，normalizeHandName 会去掉全部空白。
func allOCRText(detail *maa.RecognitionDetail) string {
	if detail == nil || detail.Results == nil {
		return ""
	}
	results := detail.Results.All
	if len(results) == 0 {
		results = detail.Results.Filtered
	}
	texts := make([]string, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		if o, ok := result.AsOCR(); ok {
			if t := strings.TrimSpace(o.Text); t != "" {
				texts = append(texts, t)
			}
		}
	}
	return strings.Join(texts, " ")
}

// slotROIsCache 缓存 10 个静态槽位 ROI 节点（HandPosition/BackPosition1-5）的解析结果：
// 值在运行期不会变化，而 CheckHand 每个心跳都会调用，重复 10 次 FFI 往返 + 反序列化
// 纯属浪费。与 dailyHand 相同，依赖 MaaFramework 回调单线程，无需加锁。
var slotROIsCache struct {
	hand [5]maa.Rect
	back [5]maa.Rect
	set  bool
}

// slotROIs 给出五组牌面/牌背槽位 ROI，首次调用时读取并缓存节点定义。
func slotROIs(src recognitionSource) (hand, back [5]maa.Rect, err error) {
	if slotROIsCache.set {
		return slotROIsCache.hand, slotROIsCache.back, nil
	}
	for i := range hand {
		hand[i], err = readROI(src, "ArenaOfGamesHandPosition"+strconv.Itoa(i+1))
		if err != nil {
			return hand, back, err
		}
		back[i], err = readROI(src, "ArenaOfGamesBackPosition"+strconv.Itoa(i+1))
		if err != nil {
			return hand, back, err
		}
	}
	slotROIsCache.hand, slotROIsCache.back, slotROIsCache.set = hand, back, true
	return hand, back, nil
}

// handAllBack 只扫牌背并回答「五个槽是否都已回背」。
// 等待空手（count==0）时的快速通道：有槽未回背（清牌动画未结束）必非空手，
// 直接不命中，省掉 15 次牌面模板扫描；背面齐了仍由调用方走完整识别保留歧义校验。
// ROI 读取或扫描失败时返回 true，交给完整路径兜底报错。
func handAllBack(src recognitionSource, img image.Image) bool {
	_, back, err := slotROIs(src)
	if err != nil {
		return true
	}
	var candidates [5][]handSlot
	if err := collectMatches(src, img, "ArenaOfGamesCardBack", back, handSlot{Type: "back"}, &candidates); err != nil {
		return true
	}
	for _, slot := range candidates {
		if len(slot) == 0 {
			return false
		}
	}
	return true
}

// recognizeHand 只在本次截图内汇总牌面和牌背，所有槽均明确后才进行判定。
func recognizeHand(src recognitionSource, img image.Image) (handDetail, error) {
	handROIs, backROIs, err := slotROIs(src)
	if err != nil {
		return handDetail{}, err
	}
	var candidates [5][]handSlot
	for _, suit := range []string{"Dot", "Bamboo", "Character"} {
		for point := 1; point <= 5; point++ {
			node := "ArenaOfGamesHand" + suit + strconv.Itoa(point)
			if err := collectMatches(src, img, node, handROIs, handSlot{Type: suit, Point: point}, &candidates); err != nil {
				return handDetail{}, err
			}
		}
	}
	if err := collectMatches(src, img, "ArenaOfGamesCardBack", backROIs, handSlot{Type: "back"}, &candidates); err != nil {
		return handDetail{}, err
	}
	return classifyHand(candidates)
}

func collectMatches(src recognitionSource, img image.Image, node string, rois [5]maa.Rect, card handSlot, candidates *[5][]handSlot) error {
	detail, err := src.RunRecognition(node, img)
	if err != nil {
		return fmt.Errorf("recognize %s: %w", node, err)
	}
	if detail == nil {
		return fmt.Errorf("recognize %s returned no detail", node)
	}
	if !detail.Hit {
		return nil
	}
	if detail.Results == nil || len(detail.Results.Filtered) == 0 {
		return fmt.Errorf("recognize %s returned no filtered template matches", node)
	}
	for _, result := range detail.Results.Filtered {
		if result == nil {
			return fmt.Errorf("recognize %s returned an empty template match", node)
		}
		match, ok := result.AsTemplateMatch()
		if !ok || match == nil {
			return fmt.Errorf("recognize %s returned a non-template result", node)
		}
		card.Score = match.Score
		if err := addMatch(candidates, rois, match.Box, card); err != nil {
			return fmt.Errorf("assign %s: %w", node, err)
		}
	}
	return nil
}

// addMatch 将同一模板的重复匹配合并，保留不同牌型候选供分类时判定歧义。
func addMatch(candidates *[5][]handSlot, rois [5]maa.Rect, box maa.Rect, card handSlot) error {
	if box.Width() <= 0 || box.Height() <= 0 || math.IsNaN(card.Score) || math.IsInf(card.Score, 0) {
		return fmt.Errorf("invalid template match")
	}
	slot := -1
	for i, roi := range rois {
		if box.X() >= roi.X() && box.Y() >= roi.Y() &&
			box.X()+box.Width() <= roi.X()+roi.Width() &&
			box.Y()+box.Height() <= roi.Y()+roi.Height() {
			if slot >= 0 {
				return fmt.Errorf("template match belongs to multiple slots")
			}
			slot = i
		}
	}
	if slot < 0 {
		return nil
	}
	for i, existing := range candidates[slot] {
		if existing.Type == card.Type && existing.Point == card.Point {
			if card.Score > existing.Score {
				candidates[slot][i] = card
			}
			return nil
		}
	}
	candidates[slot] = append(candidates[slot], card)
	return nil
}

// classifyHand 只在本次截图内确认每个槽的牌面或牌背，再判定当前唯一生效的牌型。
func classifyHand(candidates [5][]handSlot) (handDetail, error) {
	detail := handDetail{}
	emptySeen := false
	for i, matches := range candidates {
		if len(matches) != 1 {
			return handDetail{}, fmt.Errorf("slot %d has %d candidates", i+1, len(matches))
		}
		card := matches[0]
		detail.Slots[i] = card
		if card.Type == "back" && card.Point == 0 {
			emptySeen = true
			continue
		}
		if card.Point < 1 || card.Point > 5 ||
			(card.Type != "Dot" && card.Type != "Bamboo" && card.Type != "Character") {
			return handDetail{}, fmt.Errorf("slot %d has an invalid card", i+1)
		}
		if emptySeen {
			return handDetail{}, fmt.Errorf("slot %d follows an empty slot", i+1)
		}
		detail.Count++
	}
	detail.Category = categoryOf(handCounts(detail)).name()
	return detail, nil
}

// handCounts 将手牌折叠为 15 种牌面（3 花色 × 5 点数）的计数。
func handCounts(detail handDetail) [15]int {
	suitIndex := map[string]int{"Dot": 0, "Bamboo": 1, "Character": 2}
	var counts [15]int
	for _, card := range detail.Slots {
		if suit, ok := suitIndex[card.Type]; ok && card.Point >= 1 && card.Point <= 5 {
			counts[suit*5+card.Point-1]++
		}
	}
	return counts
}

// categoryOf 按「高倍率覆盖低倍率」（同花顺 8 → 四条 6 → 同花 4 → 葫芦 2 →
// 顺子 1.8 → 三条 1.6 → 两对 1.4 → 对子 1.2）判定牌面计数下唯一生效的牌型。
func categoryOf(counts [15]int) category {
	var points [6]int
	var suits [3]int
	total := 0
	for tile, c := range counts {
		if c == 0 {
			continue
		}
		points[tile%5+1] += c
		suits[tile/5] += c
		total += c
	}
	if total == 0 {
		return catNone
	}
	// 同花顺：五张点数连续且花色相同，点数只可能是 1–5 各一张。
	distinct := true
	for p := 1; p <= 5; p++ {
		if points[p] > 1 {
			distinct = false
			break
		}
	}
	if total == 5 && distinct &&
		(suits[0] == 5 || suits[1] == 5 || suits[2] == 5) {
		return catStraightFlush
	}
	// 四条：不限花色，四张同点数。
	for p := 1; p <= 5; p++ {
		if points[p] >= 4 {
			return catFourOfAKind
		}
	}
	// 同花：手牌至少 4 张且花色全部相同（4 张时 4 张全同即可停，5 张时须 5 张全同）。
	if total >= 4 && (suits[0] == total || suits[1] == total || suits[2] == total) {
		return catFlush
	}
	// 葫芦：不限花色，3 张同点数 + 2 张另一同点数，两组点数不能相同。
	if total == 5 {
		var triplets, pairs int
		for p := 1; p <= 5; p++ {
			switch {
			case points[p] >= 3:
				triplets++
			case points[p] == 2:
				pairs++
			}
		}
		if triplets == 1 && pairs == 1 {
			return catFullHouse
		}
	}
	// 顺子：不限花色，四张点数连续。
	if (points[1] > 0 && points[2] > 0 && points[3] > 0 && points[4] > 0) ||
		(points[2] > 0 && points[3] > 0 && points[4] > 0 && points[5] > 0) {
		return catStraight
	}
	// 三条：不限花色，三张点数相同。
	for p := 1; p <= 5; p++ {
		if points[p] >= 3 {
			return catThreeOfAKind
		}
	}
	// 两对：不限花色，两组同点数，组间点数不能相同。
	pairs := 0
	for p := 1; p <= 5; p++ {
		if points[p] >= 2 {
			pairs++
		}
	}
	if pairs >= 2 {
		return catTwoPair
	}
	// 对子：不限花色，两张点数相同。
	for p := 1; p <= 5; p++ {
		if points[p] >= 2 {
			return catPair
		}
	}
	return catNone
}

// evaluateHand 判断手牌相对目标牌型的状态：
// 已凑成任一目标（complete）、继续抽牌仍可凑出（continuing）或已全部无缘（rejected）。
// 凑成即停（早停），因此可达牌型允许在任意张数停牌时成立。
func evaluateHand(counts [15]int, drawn int, targets map[category]bool) string {
	if targets[categoryOf(counts)] {
		return "complete"
	}
	for c := range reachableCategories(counts, drawn) {
		if targets[c] {
			return "continuing"
		}
	}
	return "rejected"
}

// reachableCategories 枚举继续抽牌可达的所有牌型，牌面组合有限故直接去重遍历。
func reachableCategories(counts [15]int, drawn int) map[category]bool {
	results := make(map[category]bool)
	visited := make(map[[15]int]bool)
	visited[counts] = true
	var walk func(current [15]int, size int)
	walk = func(current [15]int, size int) {
		if size >= 5 {
			return
		}
		for tile := range 15 {
			current[tile]++
			if !visited[current] {
				visited[current] = true
				results[categoryOf(current)] = true
				walk(current, size+1)
			}
			current[tile]--
		}
	}
	walk(counts, drawn)
	return results
}

// readROI 读取节点 recognition.param.roi 并校验 720p 边界。GetNodeJSON 返回框架
// 归一化后的嵌套格式（源文件里的扁平写法也会被 dumper 归一化，与 trialofswordmancy、
// bettersliding 的同类实现一致），无需兼容扁平格式。
func readROI(src interface{ GetNodeJSON(string) (string, error) }, node string) (maa.Rect, error) {
	raw, err := src.GetNodeJSON(node)
	if err != nil {
		return maa.Rect{}, fmt.Errorf("read %s: %w", node, err)
	}
	var definition struct {
		Recognition struct {
			Param struct {
				ROI *maa.Rect `json:"roi"`
			} `json:"param"`
		} `json:"recognition"`
	}
	if err := json.Unmarshal([]byte(raw), &definition); err != nil {
		return maa.Rect{}, fmt.Errorf("parse %s: %w", node, err)
	}
	if definition.Recognition.Param.ROI == nil {
		return maa.Rect{}, fmt.Errorf("node %s has no slot ROI", node)
	}
	roi := *definition.Recognition.Param.ROI
	if roi.X() < 0 || roi.Y() < 0 || roi.Width() <= 0 || roi.Height() <= 0 ||
		roi.X()+roi.Width() > 1280 || roi.Y()+roi.Height() > 720 {
		return maa.Rect{}, fmt.Errorf("node %s has invalid 720p ROI %v", node, roi)
	}
	return roi, nil
}
