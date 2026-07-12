package autostockstaple

import (
	"encoding/json"
	"fmt"
	"image"
	"regexp"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	autoStockStapleQuantityActionName = "AutoStockStapleQuantityControlAction"
	defaultSlidingNodeName            = "AutoStockStapleBetterSliding"
)

var (
	validatorExpressionPattern = regexp.MustCompile(`\{([^{}]+)\}`)
	firstIntegerPattern        = regexp.MustCompile(`-?\d+`)
)

var _ maa.CustomActionRunner = &QuantityControlAction{}

type quantityControlActionParam struct {
	ItemName      string `json:"item_name"`
	ValidatorNode string `json:"validator_node,omitempty"`
	SlidingNode   string `json:"sliding_node,omitempty"`
}

type quantityValidatorNode struct {
	Recognition struct {
		Param struct {
			CustomRecognitionParam struct {
				Expression string `json:"expression"`
			} `json:"custom_recognition_param"`
		} `json:"param"`
	} `json:"recognition"`
}

// QuantityControlAction calculates the purchase quantity for an AutoStockStaple item
// from its validator expression and overrides BetterSliding attach.Target via pipeline.
// Sliding itself is executed by the sibling CheckSliding / BetterSliding branch after Buy.
type QuantityControlAction struct{}

func (a *QuantityControlAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		log.Error().Str("component", autoStockStapleQuantityActionName).Msg("context is nil")
		return false
	}
	if arg == nil {
		log.Error().Str("component", autoStockStapleQuantityActionName).Msg("custom action arg is nil")
		return false
	}

	param, err := parseQuantityControlActionParam(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockStapleQuantityActionName).
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse custom action param")
		return false
	}

	validatorNode := param.ValidatorNode
	if validatorNode == "" {
		validatorNode = buildValidatorNodeName(param.ItemName)
	}

	threshold, countNode, expression, err := resolveValidatorSpec(ctx, validatorNode)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockStapleQuantityActionName).
			Str("item_name", param.ItemName).
			Str("validator_node", validatorNode).
			Msg("failed to resolve validator spec")
		return false
	}

	img, err := captureCurrentImage(ctx)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockStapleQuantityActionName).
			Msg("failed to capture current image")
		return false
	}

	currentCount, err := runCountRecognition(ctx, img, countNode)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockStapleQuantityActionName).
			Str("item_name", param.ItemName).
			Str("count_node", countNode).
			Msg("failed to read current item count")
		return false
	}

	target := threshold - currentCount
	if target <= 0 {
		log.Info().
			Str("component", autoStockStapleQuantityActionName).
			Str("item_name", param.ItemName).
			Str("validator_node", validatorNode).
			Str("expression", expression).
			Int("threshold", threshold).
			Int("current_count", currentCount).
			Int("target", target).
			Msg("computed target is non-positive, skip BetterSliding")
		return true
	}

	slidingNode := param.SlidingNode
	if slidingNode == "" {
		slidingNode = defaultSlidingNodeName
	}

	if err := ctx.OverridePipeline(buildQuantityControlOverride(slidingNode, target)); err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockStapleQuantityActionName).
			Str("item_name", param.ItemName).
			Str("sliding_node", slidingNode).
			Int("target", target).
			Msg("failed to override BetterSliding pipeline")
		return false
	}

	log.Info().
		Str("component", autoStockStapleQuantityActionName).
		Str("item_name", param.ItemName).
		Str("validator_node", validatorNode).
		Str("count_node", countNode).
		Str("sliding_node", slidingNode).
		Str("expression", expression).
		Int("threshold", threshold).
		Int("current_count", currentCount).
		Int("target", target).
		Msg("BetterSliding target resolved")

	return true
}

func buildQuantityControlOverride(slidingNode string, target int) map[string]any {
	return map[string]any{
		slidingNode: map[string]any{
			"enabled": target > 0,
			"attach": map[string]any{
				"Target": target,
			},
		},
	}
}

func parseQuantityControlActionParam(raw string) (*quantityControlActionParam, error) {
	var param quantityControlActionParam
	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		return nil, err
	}

	param.ItemName = strings.TrimSpace(param.ItemName)
	param.ValidatorNode = strings.TrimSpace(param.ValidatorNode)
	param.SlidingNode = strings.TrimSpace(param.SlidingNode)

	if param.ItemName == "" && param.ValidatorNode == "" {
		return nil, fmt.Errorf("item_name or validator_node is required")
	}

	return &param, nil
}

func buildValidatorNodeName(itemName string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(itemName), func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	if len(parts) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("AutoStockStapleGoods")
	for _, part := range parts {
		if part == "" {
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			builder.WriteString(part[1:])
		}
	}
	builder.WriteString("Validate")
	return builder.String()
}

func resolveValidatorSpec(ctx *maa.Context, validatorNode string) (threshold int, countNode string, expression string, err error) {
	if validatorNode == "" {
		return 0, "", "", fmt.Errorf("validator node is empty")
	}

	raw, err := ctx.GetNodeJSON(validatorNode)
	if err != nil {
		return 0, "", "", fmt.Errorf("get node json: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return 0, "", "", fmt.Errorf("node json is empty")
	}

	var node quantityValidatorNode
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return 0, "", "", fmt.Errorf("unmarshal node json: %w", err)
	}

	expression = strings.TrimSpace(node.Recognition.Param.CustomRecognitionParam.Expression)
	if expression == "" {
		return 0, "", "", fmt.Errorf("validator expression is empty")
	}

	threshold, countNode, err = parseValidatorExpression(expression)
	if err != nil {
		return 0, "", "", err
	}

	return threshold, countNode, expression, nil
}

func parseValidatorExpression(expression string) (int, string, error) {
	intToken := firstIntegerPattern.FindString(expression)
	if intToken == "" {
		return 0, "", fmt.Errorf("expression %q does not contain integer threshold", expression)
	}

	threshold, err := strconv.Atoi(intToken)
	if err != nil {
		return 0, "", fmt.Errorf("parse threshold: %w", err)
	}

	matches := validatorExpressionPattern.FindStringSubmatch(expression)
	if len(matches) != 2 {
		return 0, "", fmt.Errorf("expression %q does not contain exactly one node placeholder", expression)
	}

	countNode := strings.TrimSpace(matches[1])
	if countNode == "" {
		return 0, "", fmt.Errorf("expression %q contains empty node placeholder", expression)
	}

	return threshold, countNode, nil
}

func runCountRecognition(ctx *maa.Context, img image.Image, nodeName string) (int, error) {
	detail, err := ctx.RunRecognition(nodeName, img)
	if err != nil {
		return 0, err
	}
	if detail == nil {
		return 0, fmt.Errorf("recognition detail is empty")
	}

	text, ok := findFirstOCRText(detail)
	if !ok {
		return 0, fmt.Errorf("recognition does not contain OCR result")
	}

	match := firstIntegerPattern.FindString(text)
	if match == "" {
		return 0, fmt.Errorf("ocr text %q does not contain integer", text)
	}

	value, err := strconv.Atoi(match)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func findFirstOCRText(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil {
		return "", false
	}

	// Prefer standard results buckets (Best -> Filtered -> All),
	// consistent with common custom recognition consumers.
	ocrTextFromResults := func(results *maa.RecognitionResults) (string, bool) {
		if results == nil {
			return "", false
		}
		for _, bucket := range [][]*maa.RecognitionResult{
			{results.Best},
			results.Filtered,
			results.All,
		} {
			for _, r := range bucket {
				if r == nil {
					continue
				}
				ocr, ok := r.AsOCR()
				if !ok || ocr == nil {
					continue
				}
				if ocr.Text != "" {
					return ocr.Text, true
				}
			}
		}
		return "", false
	}

	// Non-combined recognition (direct OCR etc.).
	if text, ok := ocrTextFromResults(detail.Results); ok {
		return text, true
	}

	// Combined recognition (And/Or): Results is nil by design, use CombinedResult.
	// Use the final box (already determined by box_index in pipeline) to select the child.
	if len(detail.CombinedResult) == 0 {
		return "", false
	}

	for _, child := range detail.CombinedResult {
		if child == nil {
			continue
		}
		if child.Box == detail.Box {
			if text, ok := ocrTextFromResults(child.Results); ok {
				return text, true
			}
		}
	}

	return "", false
}

func captureCurrentImage(ctx *maa.Context) (image.Image, error) {
	tasker := ctx.GetTasker()
	if tasker == nil {
		return nil, fmt.Errorf("tasker is nil")
	}

	controller := tasker.GetController()
	if controller == nil {
		return nil, fmt.Errorf("controller is nil")
	}

	controller.PostScreencap().Wait()
	img, err := controller.CacheImage()
	if err != nil {
		return nil, err
	}
	if img == nil {
		return nil, fmt.Errorf("cached image is nil")
	}

	return img, nil
}
