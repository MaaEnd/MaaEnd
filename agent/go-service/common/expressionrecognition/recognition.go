package expressionrecognition

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/boolexpr"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

type Recognition struct{}

type Params struct {
	Expression                       string `json:"expression"`
	BoxNode                          string `json:"box_node"`
	FocusMatchedResolvedExpression   bool   `json:"focus_matched_resolved_expression"`
	FocusUnmatchedResolvedExpression bool   `json:"focus_unmatched_resolved_expression"`
}

// Run evaluates a boolean expression composed of numeric recognition nodes.
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	params, err := parseParams(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "ExpressionRecognition").
			Str("custom_recognition_param", arg.CustomRecognitionParam).
			Msg("failed to parse expression recognition params")
		return nil, false
	}

	resolvedExpression, values, err := boolexpr.ResolvePlaceholders(
		params.Expression,
		func(nodeName string) (int, error) {
			return runNumericRecognition(ctx, arg, nodeName)
		},
	)
	if err != nil {
		// 这里常见于画面/节点结果尚未稳定（例如 And 节点 combined_result 不完整）。
		// 维持原行为：本次不匹配，等待下一次心跳重试；但将日志降级，避免刷屏 error。
		log.Debug().
			Err(err).
			Str("component", "ExpressionRecognition").
			Str("expression", params.Expression).
			Msg("failed to resolve expression values")
		return nil, false
	}

	result, err := boolexpr.Evaluate(resolvedExpression)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "ExpressionRecognition").
			Str("expression", params.Expression).
			Str("resolved_expression", resolvedExpression).
			Msg("failed to evaluate expression")
		return nil, false
	}

	matched, ok := result.(bool)
	if !ok {
		log.Error().
			Str("component", "ExpressionRecognition").
			Str("expression", params.Expression).
			Str("resolved_expression", resolvedExpression).
			Interface("result", result).
			Msg("expression result must be boolean")
		return nil, false
	}

	logEvaluationResult(params.Expression, resolvedExpression, values, matched)
	if matched && params.FocusMatchedResolvedExpression {
		maafocus.Print(ctx, i18n.T("expressionrecognition.focus_matched", resolvedExpression))
	} else if !matched && params.FocusUnmatchedResolvedExpression {
		maafocus.Print(ctx, i18n.T("expressionrecognition.focus_unmatched", resolvedExpression))
	}

	if !matched {
		return nil, false
	}

	resultBox, err := resolveResultBox(ctx, arg, params)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "ExpressionRecognition").
			Str("expression", params.Expression).
			Str("box_node", params.BoxNode).
			Msg("failed to resolve result box")
		return nil, false
	}

	detailJSON, _ := json.Marshal(map[string]any{
		"expression":          params.Expression,
		"resolved_expression": resolvedExpression,
		"values":              values,
		"matched":             matched,
	})

	return &maa.CustomRecognitionResult{
		Box:    resultBox,
		Detail: string(detailJSON),
	}, true
}

func logEvaluationResult(expression string, resolvedExpression string, values map[string]int, matched bool) {
	log.Info().
		Str("component", "ExpressionRecognition").
		Str("expression", expression).
		Str("resolved_expression", resolvedExpression).
		Interface("values", values).
		Bool("matched", matched).
		Msg("expression evaluated")
}

func parseParams(raw string) (*Params, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("expression is required")
	}

	var params Params
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return nil, err
	}

	params.Expression = strings.TrimSpace(params.Expression)
	if params.Expression == "" {
		return nil, fmt.Errorf("expression is required")
	}
	params.BoxNode = strings.TrimSpace(params.BoxNode)

	return &params, nil
}

func runNumericRecognition(ctx *maa.Context, arg *maa.CustomRecognitionArg, nodeName string) (int, error) {
	detail, err := ctx.RunRecognition(nodeName, arg.Img)
	if err != nil {
		return 0, err
	}

	value, err := extractRecognitionNumberFromNode(ctx, nodeName, detail)
	if err != nil {
		return 0, fmt.Errorf("failed to parse node result from %s: %w", nodeName, err)
	}

	return value, nil
}

func resolveResultBox(ctx *maa.Context, arg *maa.CustomRecognitionArg, params *Params) (maa.Rect, error) {
	if params == nil || params.BoxNode == "" {
		return arg.Roi, nil
	}

	detail, err := ctx.RunRecognition(params.BoxNode, arg.Img)
	if err != nil {
		return maa.Rect{}, err
	}

	return extractRecognitionBoxFromNode(ctx, params.BoxNode, detail)
}

func extractRecognitionNumberFromNode(ctx *maa.Context, nodeName string, detail *maa.RecognitionDetail) (int, error) {
	selectedDetail, err := recogtarget.SelectDetail(ctx, nodeName, detail)
	if err != nil {
		return 0, fmt.Errorf("resolve %s numeric source: %w", nodeName, err)
	}
	return ocrnum.Extract(selectedDetail)
}

func extractRecognitionBoxFromNode(ctx *maa.Context, nodeName string, detail *maa.RecognitionDetail) (maa.Rect, error) {
	selectedDetail, err := recogtarget.SelectDetail(ctx, nodeName, detail)
	if err != nil {
		return maa.Rect{}, fmt.Errorf("resolve %s box source: %w", nodeName, err)
	}
	return selectedDetail.Box, nil
}
