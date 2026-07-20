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
		Expected []string `json:"expected"`
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
// 仅接受「万」单位（如 "16.3万"）；无单位时假定已是万单位。
func parseRewardFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if num, ok := strings.CutSuffix(s, "万"); ok {
		return strconv.ParseFloat(num, 64)
	}
	return strconv.ParseFloat(s, 64)
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

// parseFiltered 把 detail.DetailJson 解析成 filteredDetail。
func parseFiltered(detail *maa.RecognitionDetail) (filteredDetail, bool) {
	if detail == nil {
		return filteredDetail{}, false
	}
	var fd filteredDetail
	if err := json.Unmarshal([]byte(detail.DetailJson), &fd); err != nil {
		return filteredDetail{}, false
	}
	return fd, true
}

// ocrFirst 在指定 roi 上运行 OCR 节点，返回第一个 filtered 项。
func ocrFirst(ctx *maa.Context, img image.Image, node string, rect maa.Rect) (string, []int, bool) {
	d, err := ctx.RunRecognition(node, img, roiOverride(node, rect))
	if err != nil || d == nil || !d.Hit {
		return "", nil, false
	}
	fd, ok := parseFiltered(d)
	if !ok || len(fd.Filtered) == 0 {
		return "", nil, false
	}
	return fd.Filtered[0].Text, fd.Filtered[0].Box, true
}

// scanJobs 链式扫描所有「价格 >= minReward」的委托。
// 流程：WulingToken 多 box → 每个 box 链式 OCR 价格/出发地/接取/查看位置，价格达标则组装。
// 返回的 items 顺序与 WulingToken 的 filtered 顺序一致（自上而下）。
func scanJobs(ctx *maa.Context, img image.Image, minReward float64) ([]deliveryJobItem, bool) {
	wulingDetail, err := ctx.RunRecognition(recoWulingTokenNode, img)
	if err != nil || wulingDetail == nil || !wulingDetail.Hit {
		log.Debug().Err(err).Str("component", "SeizeDeliveryJobs").Str("step", "scan_jobs").Msg("WulingToken miss")
		return nil, false
	}
	wulingFD, ok := parseFiltered(wulingDetail)
	if !ok {
		return nil, false
	}

	var items []deliveryJobItem
	for _, wf := range wulingFD.Filtered {
		if len(wf.Box) < 4 {
			continue
		}
		// 价格（基于 WulingToken box 偏移）
		rewardText, rewardBox, ok := ocrFirst(ctx, img, recoRewardNode, offsetBox(wf.Box, offsetWulingToReward))
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
		originText, _, originOk := ocrFirst(ctx, img, recoOriginNode, offsetBox(rewardBox, offsetRewardToOrigin))
		_, acceptBox, acceptOk := ocrFirst(ctx, img, recoAcceptNode, offsetBox(rewardBox, offsetRewardToAccept))
		_, viewBox, viewOk := ocrFirst(ctx, img, recoViewLocationNode, offsetBox(rewardBox, offsetRewardToView))
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
	return items, true
}

// SeizeDeliveryJobsFindTargetRecognition 是 grab 路径的识别：
// 扫描所有价格达标的委托，返回首个（列表最上）的接取按钮 box 供 action Click 接单。
type SeizeDeliveryJobsFindTargetRecognition struct{}

func (r *SeizeDeliveryJobsFindTargetRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	minReward, err := readMinReward(ctx)
	if err != nil {
		log.Error().Err(err).Str("component", "SeizeDeliveryJobs").Str("step", "find_target").Msg("read min reward")
		return nil, false
	}
	items, ok := scanJobs(ctx, arg.Img, minReward)
	if !ok || len(items) == 0 {
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
