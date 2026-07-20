package visitfriends

import (
	"encoding/json"
	"fmt"
	"math"
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

// normalizeFriendName 清洗 OCR 识别到的好友名，去除尾部噪声：
// 若包含右括号（) 或 ）），保留到右括号为止；
// 否则若包含 #，保留到 # 后的 4 个字符为止。
func normalizeFriendName(name string) string {
	runes := []rune(name)
	for i, r := range runes {
		if r == ')' || r == '）' {
			return string(runes[:i+1])
		}
	}
	for i, r := range runes {
		if r == '#' {
			end := i + 1 + 4
			if end > len(runes) {
				end = len(runes)
			}
			return string(runes[:end])
		}
	}
	return name
}

// VisitFriendsSelectFriendRecognition 参考 DailyEventGoToRecognition：
// 读取 attach.visited，排除已点好友后识别列表项，返回进船按钮框供 Pipeline Click。
// 优先备注不走 custom param：由任务选项只改 Name OCR 的 expected + order_by=Expected，
// 本识别器读取该节点 order_by，仅在注入 visited 排除时保持相同 expected 形态。
type VisitFriendsSelectFriendRecognition struct{}

var _ maa.CustomRecognitionRunner = &VisitFriendsSelectFriendRecognition{}

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

	visited, err := loadSelectFriendVisited(ctx, nodeName)
	if err != nil {
		log.Error().Err(err).Str("component", selectFriendRecognitionName).Str("node", nodeName).Msg("load attach.visited failed")
		return nil, false
	}

	// 覆盖 OCR expected 排除已访问；同步写回 order_by，避免扁平 override 冲掉备注优先规则。
	prioritizeRemark, orderBy, err := loadSelectFriendNameOCROrder(ctx)
	if err != nil {
		log.Error().Err(err).Str("component", selectFriendRecognitionName).Msg("load name OCR order_by failed")
		return nil, false
	}
	expected := buildSelectFriendExpected(visited, prioritizeRemark)
	nameOCROverride := map[string]any{
		"expected": expected,
	}
	if orderBy != "" {
		nameOCROverride["order_by"] = orderBy
	}
	if err := ctx.OverridePipeline(map[string]any{
		selectFriendNameOCRNode: nameOCROverride,
	}); err != nil {
		log.Error().Err(err).Str("component", selectFriendRecognitionName).Msg("override name OCR expected failed")
		return nil, false
	}

	detail, err := ctx.RunRecognition(selectFriendCandidateNode, arg.Img)
	if err != nil {
		log.Error().Err(err).Str("component", selectFriendRecognitionName).Str("node", selectFriendCandidateNode).Msg("RunRecognition failed")
		return nil, false
	}
	if detail == nil || !detail.Hit || detail.CombinedResult == nil || len(detail.CombinedResult) < 3 {
		log.Info().Str("component", selectFriendRecognitionName).Strs("visited", visited).Msg("no friend candidate")
		return nil, false
	}

	buttonHits, nameHits, ok := parseSelectFriendCombinedHits(detail)
	if !ok {
		return nil, false
	}
	if len(buttonHits) == 0 || len(nameHits) == 0 {
		log.Info().
			Str("component", selectFriendRecognitionName).
			Int("buttons", len(buttonHits)).
			Int("names", len(nameHits)).
			Msg("empty button or name hits")
		return nil, false
	}

	var selected *selectFriendDetail
	for i := range nameHits {
		rawName := strings.TrimSpace(nameHits[i].Text)
		if rawName == "" {
			continue
		}

		name := normalizeFriendName(rawName)
		if selectFriendVisitedContains(visited, name) {
			log.Debug().Str("component", selectFriendRecognitionName).Str("name", name).Msg("already visited, skip")
			continue
		}

		buttonBox, paired := nearestButtonBoxByVertical(buttonHits, nameHits[i].Box)
		if !paired {
			log.Warn().Str("component", selectFriendRecognitionName).Str("name", name).Msg("no enter button near name")
			continue
		}

		selected = &selectFriendDetail{
			NameText:  name,
			ButtonBox: buttonBox,
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
	// CombinedResult 与 WithName.all_of 对齐：
	// [0]=进船按钮，[1]=线索交换，[2]=名称 OCR；Results.Best 为空时只能走 DetailJson。
	buttonRaw, ok := selectFriendCombinedDetailJSON(detail, 0, "button")
	if !ok {
		return nil, nil, false
	}
	nameRaw, ok := selectFriendCombinedDetailJSON(detail, 2, "name")
	if !ok {
		return nil, nil, false
	}

	var buttonJSON, nameJSON struct {
		Filtered []selectFriendOCRHit `json:"filtered"`
	}
	if err := json.Unmarshal([]byte(buttonRaw), &buttonJSON); err != nil {
		log.Error().Err(err).Str("component", selectFriendRecognitionName).Msg("parse button detail json")
		return nil, nil, false
	}
	if err := json.Unmarshal([]byte(nameRaw), &nameJSON); err != nil {
		log.Error().Err(err).Str("component", selectFriendRecognitionName).Msg("parse name detail json")
		return nil, nil, false
	}

	// Filtered 缺失时按空切片处理，交给调用方统一判定「无候选」。
	if buttonJSON.Filtered == nil {
		buttonJSON.Filtered = []selectFriendOCRHit{}
	}
	if nameJSON.Filtered == nil {
		nameJSON.Filtered = []selectFriendOCRHit{}
	}
	return buttonJSON.Filtered, nameJSON.Filtered, true
}

func selectFriendCombinedDetailJSON(detail *maa.RecognitionDetail, index int, kind string) (string, bool) {
	if detail == nil {
		log.Warn().Str("component", selectFriendRecognitionName).Str("kind", kind).Msg("combined detail is nil")
		return "", false
	}
	if index < 0 || index >= len(detail.CombinedResult) {
		log.Warn().
			Str("component", selectFriendRecognitionName).
			Str("kind", kind).
			Int("index", index).
			Int("combined_len", len(detail.CombinedResult)).
			Msg("combined result index out of range")
		return "", false
	}
	child := detail.CombinedResult[index]
	if child == nil {
		log.Warn().
			Str("component", selectFriendRecognitionName).
			Str("kind", kind).
			Int("index", index).
			Msg("combined result entry is nil")
		return "", false
	}
	raw := strings.TrimSpace(child.DetailJson)
	if raw == "" {
		log.Warn().
			Str("component", selectFriendRecognitionName).
			Str("kind", kind).
			Int("index", index).
			Msg("combined result DetailJson is empty")
		return "", false
	}
	return raw, true
}

func hitBoxCenterY(box []int) float64 {
	if len(box) < 4 {
		return math.NaN()
	}
	return float64(box[1]) + float64(box[3])/2
}

// nearestButtonBoxByVertical 按纵向中心距离，为名字框找最近的进船按钮。
func nearestButtonBoxByVertical(buttons []selectFriendOCRHit, nameBox []int) ([]int, bool) {
	nameY := hitBoxCenterY(nameBox)
	if math.IsNaN(nameY) || len(buttons) == 0 {
		return nil, false
	}

	bestIdx := -1
	bestDist := math.MaxFloat64
	for i := range buttons {
		if len(buttons[i].Box) < 4 {
			continue
		}
		dist := math.Abs(hitBoxCenterY(buttons[i].Box) - nameY)
		if dist < bestDist {
			bestDist = dist
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return nil, false
	}
	// 同行一般在几十像素内；过大说明名字与按钮不在同一行，放弃配对。
	const maxRowDist = 80.0
	if bestDist > maxRowDist {
		return nil, false
	}
	return append([]int(nil), buttons[bestIdx].Box...), true
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

// loadSelectFriendNameOCROrder 读取 Name OCR 的 order_by。
// order_by=Expected 表示任务选项开启了优先备注（expected 为「带括号 / 不带括号」两项）。
func loadSelectFriendNameOCROrder(ctx *maa.Context) (prioritizeRemark bool, orderBy string, err error) {
	raw, err := ctx.GetNodeJSON(selectFriendNameOCRNode)
	if err != nil {
		return false, "", err
	}
	orderBy, err = parseOCROrderBy([]byte(raw))
	if err != nil {
		return false, "", err
	}
	return strings.EqualFold(orderBy, "Expected"), orderBy, nil
}

func parseOCROrderBy(raw []byte) (string, error) {
	var flat struct {
		OrderBy     string          `json:"order_by"`
		Recognition json.RawMessage `json:"recognition"`
	}
	if err := json.Unmarshal(raw, &flat); err != nil {
		return "", err
	}
	if orderBy := strings.TrimSpace(flat.OrderBy); orderBy != "" {
		return orderBy, nil
	}
	if len(flat.Recognition) == 0 {
		return "", nil
	}
	// 扁平："recognition":"OCR"；v2："recognition":{"type":"OCR","param":{"order_by":...}}
	if flat.Recognition[0] == '"' {
		return "", nil
	}
	var v2 struct {
		Param struct {
			OrderBy string `json:"order_by"`
		} `json:"param"`
	}
	if err := json.Unmarshal(flat.Recognition, &v2); err != nil {
		return "", err
	}
	return strings.TrimSpace(v2.Param.OrderBy), nil
}

func buildSelectFriendExpected(visited []string, prioritizeRemark bool) []string {
	escaped := make([]string, 0, len(visited))
	for _, name := range visited {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		escaped = append(escaped, regexp.QuoteMeta(trimmed))
	}

	// 带备注好友名形如「备注(原名)#xxxx」，用左括号区分。
	// order_by=Expected 时：先带括号、后不带括号，优先命中备注好友。
	const withRemark = `.*[（(].*#.*`
	const anyFriend = `.*#.*`

	prefix := ""
	if len(escaped) > 0 {
		// 与 DailyEventGoTo 相同：用负向预测排除已访问；Go 侧仍会再按 normalize 过滤一层。
		prefix = fmt.Sprintf("^(?!(?:%s)$)", strings.Join(escaped, "|"))
	}
	if prioritizeRemark {
		return []string{
			prefix + withRemark,
			prefix + anyFriend,
		}
	}
	return []string{prefix + anyFriend}
}
