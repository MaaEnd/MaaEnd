package ims

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/boolexpr"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentEvaluateItemQuantity = "EvaluateItemQuantity"

var _ maa.CustomActionRunner = &EvaluateItemQuantity{}

// evaluateItemQuantityParam is custom_action_param for EvaluateItemQuantity (A4).
type evaluateItemQuantityParam struct {
	// Expression is an integer expression over cached item quantities.
	// Placeholders use {ITEM_ID}, same arithmetic as ItemQuantitySatisfied / ExpressionRecognition,
	// plus max(a,b) / min(a,b). Result must be int (not bool).
	Expression string `json:"expression"`
}

// EvaluateItemQuantity evaluates an inventory formula against the IMS cache and prints the result (A4).
// Read-only; does not change cache / readiness.
type EvaluateItemQuantity struct{}

// Run implements maa.CustomActionRunner.
func (a *EvaluateItemQuantity) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().
			Str("component", componentEvaluateItemQuantity).
			Msg("nil context or arg")
		return false
	}

	params, err := parseEvaluateItemQuantityParam(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentEvaluateItemQuantity).
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse params")
		return false
	}

	if err := ensureHydrated(); err != nil {
		log.Error().
			Err(err).
			Str("component", componentEvaluateItemQuantity).
			Msg("failed to hydrate ims cache")
		return false
	}

	resolvedExpression, values, err := boolexpr.ResolvePlaceholders(
		params.Expression,
		func(itemID string) (int, error) {
			return globalCache.quantity(itemID), nil
		},
	)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentEvaluateItemQuantity).
			Str("expression", params.Expression).
			Msg("failed to resolve expression values")
		return false
	}

	result, err := boolexpr.Evaluate(resolvedExpression)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentEvaluateItemQuantity).
			Str("expression", params.Expression).
			Str("resolved_expression", resolvedExpression).
			Msg("failed to evaluate expression")
		return false
	}

	qty, ok := result.(int)
	if !ok {
		log.Error().
			Str("component", componentEvaluateItemQuantity).
			Str("expression", params.Expression).
			Str("resolved_expression", resolvedExpression).
			Interface("result", result).
			Msg("expression result must be int")
		return false
	}

	maafocus.Print(ctx, i18n.T("ims.expression_result", resolvedExpression, qty))
	log.Info().
		Str("component", componentEvaluateItemQuantity).
		Str("expression", params.Expression).
		Str("resolved_expression", resolvedExpression).
		Interface("values", values).
		Int("result", qty).
		Msg("item quantity expression evaluated")
	return true
}

func parseEvaluateItemQuantityParam(raw string) (evaluateItemQuantityParam, error) {
	var params evaluateItemQuantityParam
	if strings.TrimSpace(raw) == "" {
		return evaluateItemQuantityParam{}, fmt.Errorf("custom_action_param is empty")
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return evaluateItemQuantityParam{}, err
	}
	params.Expression = strings.TrimSpace(params.Expression)
	if params.Expression == "" {
		return evaluateItemQuantityParam{}, fmt.Errorf("expression is required")
	}
	return params, nil
}
