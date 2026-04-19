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
// from its validator expression, overrides BetterSliding attach.Target, and runs it once.
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

	override := map[string]any{
		slidingNode: map[string]any{
			"attach": map[string]any{
				"Target": target,
			},
		},
	}

	detail, err := ctx.RunTask(slidingNode, override)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockStapleQuantityActionName).
			Str("item_name", param.ItemName).
			Str("sliding_node", slidingNode).
			Int("target", target).
			Msg("failed to run BetterSliding task")
		return false
	}
	if detail == nil {
		log.Error().
			Str("component", autoStockStapleQuantityActionName).
			Str("item_name", param.ItemName).
			Str("sliding_node", slidingNode).
			Int("target", target).
			Msg("BetterSliding task returned nil detail")
		return false
	}
	if !detail.Status.Success() {
		log.Error().
			Str("component", autoStockStapleQuantityActionName).
			Str("item_name", param.ItemName).
			Str("sliding_node", slidingNode).
			Int("target", target).
			Int64("subtask_id", detail.ID).
			Str("subtask_status", detail.Status.String()).
			Msg("BetterSliding task did not succeed")
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
		Msg("BetterSliding target resolved and executed")

	return true
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
	if detail == nil || detail.Results == nil || detail.Results.Best == nil {
		return 0, fmt.Errorf("recognition detail is empty")
	}

	ocrResult, ok := detail.Results.Best.AsOCR()
	if !ok {
		return 0, fmt.Errorf("best recognition result is not OCR")
	}

	match := firstIntegerPattern.FindString(ocrResult.Text)
	if match == "" {
		return 0, fmt.Errorf("ocr text %q does not contain integer", ocrResult.Text)
	}

	value, err := strconv.Atoi(match)
	if err != nil {
		return 0, err
	}
	return value, nil
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
