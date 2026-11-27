package quantitybudget

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 参数与目标键名。参数全部从调用节点的 attach 读取：其他页面可以用
// pipeline_override 改 attach 里的取值来源，复用同一个动作节点。
const (
	attachKeyMode           = "mode"
	attachKeyBalance        = "balance"
	attachKeyUnitPrice      = "unit_price"
	attachKeyLimit          = "limit"
	attachKeyTargetNode     = "target_node"
	attachKeyEmptyNode      = "empty_node"
	attachKeyTargetQuantity = "TargetQuantity"
)

// budgetLog 是带上 component 字段的包级 logger。
var budgetLog = log.With().Str("component", actionName).Logger()

// budgetParam 是调用节点 attach 里的参数。
// 数值字段保留 JSON 原始形态：number 直接取用，string 按 OCR 节点名解析。
type budgetParam struct {
	Mode       string `json:"mode"`
	Balance    any    `json:"balance"`
	UnitPrice  any    `json:"unit_price"`
	Limit      any    `json:"limit"`
	TargetNode string `json:"target_node"`
	EmptyNode  string `json:"empty_node"`
}

// QuantityBudgetAction 按预算反推滑条目标数量，并覆写目标 BetterSliding 节点。
//
// 流程：读 attach 参数 → 按需识别取值 → 计算目标数量 → 覆写目标节点。
// 全程不写中间值，也不依赖任何跨节点状态。
type QuantityBudgetAction struct{}

var _ maa.CustomActionRunner = &QuantityBudgetAction{}

func (a *QuantityBudgetAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		budgetLog.Error().Msg("context or custom action arg is nil")
		return false
	}

	param, err := readParam(ctx, arg.CurrentTaskName)
	if err != nil {
		budgetLog.Error().Err(err).Str("node", arg.CurrentTaskName).Msg("failed to read attach params")
		return false
	}

	mode, err := normalizeMode(param.Mode)
	if err != nil {
		budgetLog.Error().Err(err).Str("mode", param.Mode).Msg("invalid mode")
		return false
	}

	// 三个值都是数字时不需要截图。
	var img image.Image
	if needsFrame(param.Balance) || needsFrame(param.UnitPrice) || needsFrame(param.Limit) {
		if img, err = captureImage(ctx); err != nil {
			budgetLog.Error().Err(err).Msg("failed to capture current image")
			return false
		}
	}

	balance, err := resolveIntSource(ctx, img, attachKeyBalance, param.Balance)
	if err != nil {
		budgetLog.Error().Err(err).Msg("failed to resolve balance")
		return false
	}
	unitPrice, err := resolveIntSource(ctx, img, attachKeyUnitPrice, param.UnitPrice)
	if err != nil {
		budgetLog.Error().Err(err).Msg("failed to resolve unit price")
		return false
	}
	limit, err := resolveIntSource(ctx, img, attachKeyLimit, param.Limit)
	if err != nil {
		budgetLog.Error().Err(err).Msg("failed to resolve target balance")
		return false
	}

	target, err := resolveBudgetTarget(mode, balance, limit, unitPrice)
	if err != nil {
		budgetLog.Error().Err(err).Str("mode", mode).Int("balance", balance).Int("unit_price", unitPrice).Int("limit", limit).Msg("failed to resolve target quantity")
		return false
	}

	if target > 0 {
		if err := ctx.OverridePipeline(map[string]any{
			param.TargetNode: map[string]any{
				"enabled": true,
				"attach":  map[string]any{attachKeyTargetQuantity: target},
			},
		}); err != nil {
			budgetLog.Error().Err(err).Str("target_node", param.TargetNode).Int("target_quantity", target).Msg("failed to override target node")
			return false
		}

		budgetLog.Info().Str("target_node", param.TargetNode).Str("mode", mode).Int("balance", balance).Int("unit_price", unitPrice).Int("limit", limit).Int("target_quantity", target).Msg("budget resolved and target quantity overridden")
		return true
	}

	// 目标数量不为正：本轮不需要调整数量。
	if param.EmptyNode == "" {
		budgetLog.Error().Str("mode", mode).Int("balance", balance).Int("unit_price", unitPrice).Int("limit", limit).Msg("target quantity is not positive but empty_node is not configured")
		return false
	}
	if param.EmptyNode == param.TargetNode {
		budgetLog.Error().Str("target_node", param.TargetNode).Msg("empty_node must differ from target_node")
		return false
	}

	if err := ctx.OverridePipeline(map[string]any{
		param.EmptyNode:  map[string]any{"enabled": true},
		param.TargetNode: map[string]any{"enabled": false},
	}); err != nil {
		budgetLog.Error().Err(err).Str("empty_node", param.EmptyNode).Str("target_node", param.TargetNode).Msg("failed to route empty budget branch")
		return false
	}

	budgetLog.Info().Str("empty_node", param.EmptyNode).Str("target_node", param.TargetNode).Str("mode", mode).Int("balance", balance).Int("unit_price", unitPrice).Int("limit", limit).Msg("budget requires no adjustment, empty branch enabled")
	return true
}

// readParam 读取并校验调用节点 attach 里的参数，顺带去掉字符串字段的首尾空白。
func readParam(ctx *maa.Context, nodeName string) (budgetParam, error) {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return budgetParam{}, fmt.Errorf("get node json of %s: %w", nodeName, err)
	}
	if strings.TrimSpace(raw) == "" {
		return budgetParam{}, fmt.Errorf("node json of %s is empty", nodeName)
	}

	var node struct {
		Attach budgetParam `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return budgetParam{}, fmt.Errorf("unmarshal node json of %s: %w", nodeName, err)
	}

	attach := node.Attach
	attach.Mode = strings.TrimSpace(attach.Mode)
	attach.TargetNode = strings.TrimSpace(attach.TargetNode)
	attach.EmptyNode = strings.TrimSpace(attach.EmptyNode)

	missing := make([]string, 0, 5)
	for _, check := range []struct {
		key     string
		present bool
	}{
		{attachKeyMode, attach.Mode != ""},
		{attachKeyBalance, attach.Balance != nil},
		{attachKeyUnitPrice, attach.UnitPrice != nil},
		{attachKeyLimit, attach.Limit != nil},
		{attachKeyTargetNode, attach.TargetNode != ""},
	} {
		if !check.present {
			missing = append(missing, check.key)
		}
	}
	if len(missing) > 0 {
		return budgetParam{}, fmt.Errorf("attach is missing %s", strings.Join(missing, ", "))
	}

	return attach, nil
}

// needsFrame 判断该取值是否依赖识别结果。数字形态不需要截图。
func needsFrame(raw any) bool {
	_, ok := raw.(string)
	return ok
}

// captureImage 抓取当前画面，供三个取值复用同一帧。
func captureImage(ctx *maa.Context) (image.Image, error) {
	tasker := ctx.GetTasker()
	if tasker == nil {
		return nil, fmt.Errorf("tasker is nil")
	}
	controller := tasker.GetController()
	if controller == nil {
		return nil, fmt.Errorf("controller is nil")
	}

	controller.PostScreencap().Wait()
	return controller.CacheImage()
}

// resolveIntSource 把单个取值来源解析成整数：JSON number 直接取用，
// 字符串按 OCR 节点名识别后解析。三种取值共用同一帧。
func resolveIntSource(ctx *maa.Context, img image.Image, field string, raw any) (int, error) {
	if number, ok := raw.(float64); ok {
		if math.Trunc(number) != number || number <= float64(math.MinInt) || number >= float64(math.MaxInt) {
			return 0, fmt.Errorf("%s: %v is not an integer in range", field, number)
		}
		return int(number), nil
	}

	nodeName, ok := raw.(string)
	if !ok {
		return 0, fmt.Errorf("%s: expected a number or an OCR node name, got %T", field, raw)
	}
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return 0, fmt.Errorf("%s: node name is empty", field)
	}
	if img == nil {
		return 0, fmt.Errorf("%s: node %s requires an image", field, nodeName)
	}

	detail, err := ctx.RunRecognition(nodeName, img)
	if err != nil {
		return 0, fmt.Errorf("%s: run recognition %s: %w", field, nodeName, err)
	}
	if detail == nil || !detail.Hit {
		return 0, fmt.Errorf("%s: node %s did not hit", field, nodeName)
	}

	selected, err := recogtarget.SelectDetail(ctx, nodeName, detail)
	if err != nil {
		return 0, fmt.Errorf("%s: select detail of %s: %w", field, nodeName, err)
	}

	number, err := ocrnum.Extract(selected)
	if err != nil {
		return 0, fmt.Errorf("%s: node %s: %w", field, nodeName, err)
	}
	return number, nil
}
