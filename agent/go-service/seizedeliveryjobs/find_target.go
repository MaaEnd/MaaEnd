package seizedeliveryjobs

import (
	"encoding/json"
	"fmt"
	"image"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 抢单识别节点名（定义在 SeizeDeliveryJobsCommon.json）
const (
	recoWulingTokenNode  = "__SeizeDeliveryJobsRecoWulingToken"
	recoRewardNode       = "__SeizeDeliveryJobsRecoReward"
	recoOriginNode       = "__SeizeDeliveryJobsRecoOrigin"
	recoAcceptNode       = "__SeizeDeliveryJobsRecoAccept"
	recoViewLocationNode = "__SeizeDeliveryJobsRecoViewLocation"
	minRewardNode        = "__SeizeDeliveryJobsMinReward"
)

// 链式 roi 偏移（照搬原档位 And 的 sub 间相对 offset）。
// box + offset = 下一个识别的 roi。
var (
	offsetWulingToReward = [4]int{0, 30, 0, -20}
	offsetRewardToOrigin = [4]int{-229, -38, 50, 14}
	offsetRewardToAccept = [4]int{226, -4, 70, 12}
	offsetRewardToView   = [4]int{-215, -8, 34, 10}
)

// readMinReward 从单价仓库节点（__SeizeDeliveryJobsMinReward）读取价格下限（单位：万）。
// 该节点的 expected 由 tasks 覆写为用户输入值（{Reward}）。
func readMinReward(ctx *maa.Context) (float64, error) {
	raw, err := ctx.GetNodeJSON(minRewardNode)
	if err != nil {
		return 0, fmt.Errorf("get node %s: %w", minRewardNode, err)
	}
	log.Debug().
		Str("component", "SeizeDeliveryJobs").
		Str("step", "read_min_reward").
		Str("raw", raw).
		Msg("MinReward node json")
	// expected 可能出现在顶层（V1 pipeline）或 recognition.param.expected（V2）
	var node struct {
		Expected    []string `json:"expected"`
		Recognition struct {
			Param struct {
				Expected []string `json:"expected"`
			} `json:"param"`
		} `json:"recognition"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return 0, fmt.Errorf("parse %s: %w", minRewardNode, err)
	}
	exps := node.Expected
	if len(exps) == 0 {
		exps = node.Recognition.Param.Expected
	}
	if len(exps) == 0 {
		return 0, fmt.Errorf("%s.expected empty (raw: %s)", minRewardNode, raw)
	}
	return parseRewardFloat(exps[0])
}

// parseRewardFloat 解析价格文本为 float（单位统一为「万」）。
// 支持「万」/「萬」单位（如 "16.3万"），也支持英文缩写 K/M（如 "119K"、"1.2M"，
// 繁体等部分地区客户端用此格式显示报酬）；无单位时假定已是万单位。
func parseRewardFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if num, ok := strings.CutSuffix(s, "万"); ok {
		return strconv.ParseFloat(num, 64)
	}
	if num, ok := strings.CutSuffix(s, "萬"); ok {
		return strconv.ParseFloat(num, 64)
	}
	if num, ok := cutSuffixFold(s, "K"); ok {
		v, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return 0, err
		}
		return v / 10, nil // 1K = 1000 = 0.1万
	}
	if num, ok := cutSuffixFold(s, "M"); ok {
		v, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return 0, err
		}
		return v * 100, nil // 1M = 1000000 = 100万
	}
	return strconv.ParseFloat(s, 64)
}

// cutSuffixFold 忽略大小写地裁剪后缀（如 "119k"/"119K" 均可匹配 "K"）。
func cutSuffixFold(s, suffix string) (string, bool) {
	if len(s) < len(suffix) {
		return "", false
	}
	if strings.EqualFold(s[len(s)-len(suffix):], suffix) {
		return s[:len(s)-len(suffix)], true
	}
	return "", false
}

// offsetBox 把 box 加上偏移得到新 box。
func offsetBox(box []int, off [4]int) maa.Rect {
	if len(box) < 4 {
		return maa.Rect{}
	}
	return maa.Rect{box[0] + off[0], box[1] + off[1], box[2] + off[2], box[3] + off[3]}
}

// roiOverride 构造 RunRecognition 的 pipeline_override（V1：节点顶层 roi）。
func roiOverride(node string, rect maa.Rect) map[string]any {
	return map[string]any{
		node: map[string]any{
			"roi": rect,
		},
	}
}

// parseFiltered 解析识别命中后返回的 JSON 详情。
func parseFiltered(detail *maa.RecognitionDetail) (filteredDetail, error) {
	if detail == nil {
		return filteredDetail{}, fmt.Errorf("recognition detail is nil")
	}
	var fd filteredDetail
	if err := json.Unmarshal([]byte(detail.DetailJson), &fd); err != nil {
		return filteredDetail{}, fmt.Errorf("parse recognition detail: %w", err)
	}
	return fd, nil
}

// ocrFirst 在指定 ROI 上运行 OCR，并返回第一个筛选结果。
// 正常未命中时返回 ok=false、err=nil；执行或详情解析失败时返回 error，
// 避免调用方将内部错误误判为正常未命中。
func ocrFirst(ctx *maa.Context, img image.Image, node string, rect maa.Rect) (string, []int, bool, error) {
	d, err := ctx.RunRecognition(node, img, roiOverride(node, rect))
	if err != nil {
		return "", nil, false, fmt.Errorf("run recognition %s: %w", node, err)
	}
	if d == nil {
		return "", nil, false, fmt.Errorf("run recognition %s returned nil detail", node)
	}
	if !d.Hit {
		return "", nil, false, nil
	}
	fd, err := parseFiltered(d)
	if err != nil {
		return "", nil, false, fmt.Errorf("parse recognition %s: %w", node, err)
	}
	if len(fd.Filtered) == 0 {
		return "", nil, false, nil
	}
	return fd.Filtered[0].Text, fd.Filtered[0].Box, true, nil
}

// scanJobs 扫描报酬不低于 minReward 的全部委托。
// 当前列表没有符合条件的委托时返回空列表和 nil error；识别执行或详情解析失败时返回 error。
func scanJobs(ctx *maa.Context, img image.Image, minReward float64) ([]deliveryJobItem, error) {
	wulingDetail, err := ctx.RunRecognition(recoWulingTokenNode, img)
	if err != nil {
		return nil, fmt.Errorf("run recognition %s: %w", recoWulingTokenNode, err)
	}
	if wulingDetail == nil {
		return nil, fmt.Errorf("run recognition %s returned nil detail", recoWulingTokenNode)
	}
	if !wulingDetail.Hit {
		return nil, nil
	}
	wulingFD, err := parseFiltered(wulingDetail)
	if err != nil {
		return nil, fmt.Errorf("parse recognition %s: %w", recoWulingTokenNode, err)
	}

	var items []deliveryJobItem
	for _, wf := range wulingFD.Filtered {
		if len(wf.Box) < 4 {
			continue
		}
		// 价格（基于 WulingToken box 偏移）
		rewardText, rewardBox, ok, err := ocrFirst(ctx, img, recoRewardNode, offsetBox(wf.Box, offsetWulingToReward))
		if err != nil {
			return nil, fmt.Errorf("scan reward: %w", err)
		}
		if !ok || len(rewardBox) < 4 {
			continue
		}
		price, err := parseRewardFloat(rewardText)
		if err != nil {
			log.Debug().Err(err).Str("component", "SeizeDeliveryJobs").Str("step", "scan_jobs").Str("reward_text", rewardText).Msg("parse reward")
			continue
		}
		if price < minReward {
			continue
		}
		// 出发地 / 接取 / 查看位置（基于 RewardOcr box 偏移）；任一 OCR 未命中即跳过，避免下游拿到空 box
		originText, _, originOk, err := ocrFirst(ctx, img, recoOriginNode, offsetBox(rewardBox, offsetRewardToOrigin))
		if err != nil {
			return nil, fmt.Errorf("scan origin: %w", err)
		}
		_, acceptBox, acceptOk, err := ocrFirst(ctx, img, recoAcceptNode, offsetBox(rewardBox, offsetRewardToAccept))
		if err != nil {
			return nil, fmt.Errorf("scan accept button: %w", err)
		}
		_, viewBox, viewOk, err := ocrFirst(ctx, img, recoViewLocationNode, offsetBox(rewardBox, offsetRewardToView))
		if err != nil {
			return nil, fmt.Errorf("scan view-location button: %w", err)
		}
		if !originOk || !acceptOk || !viewOk {
			log.Debug().
				Str("component", "SeizeDeliveryJobs").
				Str("step", "scan_jobs").
				Str("reward_text", rewardText).
				Bool("origin_ok", originOk).
				Bool("accept_ok", acceptOk).
				Bool("view_ok", viewOk).
				Msg("skip job: incomplete downstream ocr")
			continue
		}

		items = append(items, deliveryJobItem{
			RewardBox:       rewardBox,
			OriginText:      originText,
			AcceptBox:       acceptBox,
			ViewLocationBox: viewBox,
		})
	}
	return items, nil
}

// SeizeDeliveryJobsFindTargetRecognition 是直接抢单路径的识别器。
// 它扫描所有符合条件的委托，并返回列表最上方委托的接取按钮框。
type SeizeDeliveryJobsFindTargetRecognition struct{}

// Run 扫描当前委托列表，并返回第一个符合条件的目标。
func (r *SeizeDeliveryJobsFindTargetRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	minReward, err := readMinReward(ctx)
	if err != nil {
		log.Error().Err(err).Str("component", "SeizeDeliveryJobs").Str("step", "find_target").Msg("read min reward")
		return nil, false
	}
	items, err := scanJobs(ctx, arg.Img, minReward)
	if err != nil {
		log.Error().Err(err).Str("component", "SeizeDeliveryJobs").Str("step", "find_target").Msg("scan jobs")
		return nil, false
	}
	if len(items) == 0 {
		return nil, false
	}
	item := items[0]
	if len(item.AcceptBox) < 4 {
		log.Warn().Str("component", "SeizeDeliveryJobs").Str("step", "find_target").Int("box_len", len(item.AcceptBox)).Msg("accept box invalid")
		return nil, false
	}
	log.Info().
		Str("component", "SeizeDeliveryJobs").
		Str("step", "find_target").
		Float64("min_reward", minReward).
		Int("matched", len(items)).
		Str("origin", item.OriginText).
		Msg("found target")
	return &maa.CustomRecognitionResult{
		Box:    boxToRect(item.AcceptBox),
		Detail: `{"custom": "SeizeDeliveryJobsFindTarget"}`,
	}, true
}

var _ maa.CustomRecognitionRunner = &SeizeDeliveryJobsFindTargetRecognition{}
