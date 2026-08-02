package ims

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/boolexpr"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentItemQuantitySatisfied = "ItemQuantitySatisfied"
	// Avoid flooding the UI when Pipeline scans many ItemQuantitySatisfied
	// nodes in one dispatch next-list; identical lines share a throttle window.
	itemQuantityFocusThrottle = 10 * time.Second
)

var _ maa.CustomRecognitionRunner = &ItemQuantitySatisfied{}

// itemQuantitySatisfiedParam is custom_recognition_param for ItemQuantitySatisfied.
type itemQuantitySatisfiedParam struct {
	// Expression is a boolean expression over cached item quantities.
	// Placeholders use {ITEM_ID}, same arithmetic/compare/logic as ExpressionRecognition.
	Expression string `json:"expression"`
	// NotifyUI when true prints the resolved expression to UI Focus.
	// Default false (omit or false) to avoid flooding dispatch-style next scans.
	NotifyUI bool `json:"notify_ui"`
}

// ItemQuantitySatisfied reports whether cached item quantities meet an expression (R1).
// Read-only; does not check readiness — combine with ItemDataReady via And when needed.
type ItemQuantitySatisfied struct{}

// Run implements maa.CustomRecognitionRunner.
func (r *ItemQuantitySatisfied) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().
			Str("component", componentItemQuantitySatisfied).
			Msg("got nil custom recognition arg")
		return nil, false
	}

	params, err := parseItemQuantitySatisfiedParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemQuantitySatisfied).
			Str("custom_recognition_param", arg.CustomRecognitionParam).
			Msg("failed to parse params")
		return nil, false
	}

	if err := ensureHydrated(); err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemQuantitySatisfied).
			Msg("failed to hydrate ims cache")
		return nil, false
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
			Str("component", componentItemQuantitySatisfied).
			Str("expression", params.Expression).
			Msg("failed to resolve expression values")
		return nil, false
	}

	result, err := boolexpr.Evaluate(resolvedExpression)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemQuantitySatisfied).
			Str("expression", params.Expression).
			Str("resolved_expression", resolvedExpression).
			Msg("failed to evaluate expression")
		return nil, false
	}

	matched, ok := result.(bool)
	if !ok {
		log.Error().
			Str("component", componentItemQuantitySatisfied).
			Str("expression", params.Expression).
			Str("resolved_expression", resolvedExpression).
			Interface("result", result).
			Msg("expression result must be boolean")
		return nil, false
	}

	if params.NotifyUI {
		focusKey := "ims.expression_ok"
		if !matched {
			focusKey = "ims.expression_short"
		}
		maafocus.PrintThrottle(
			ctx,
			itemQuantityFocusThrottle,
			i18n.T(focusKey, resolvedExpression),
		)
	}

	log.Info().
		Str("component", componentItemQuantitySatisfied).
		Str("expression", params.Expression).
		Str("resolved_expression", resolvedExpression).
		Interface("values", values).
		Bool("matched", matched).
		Msg("item expression evaluated")

	if !matched {
		return nil, false
	}

	detailJSON, err := json.Marshal(map[string]any{
		"satisfied":           true,
		"expression":          params.Expression,
		"resolved_expression": resolvedExpression,
		"values":              values,
	})
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemQuantitySatisfied).
			Msg("failed to marshal detail")
		return nil, false
	}
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

func parseItemQuantitySatisfiedParam(raw string) (itemQuantitySatisfiedParam, error) {
	var params itemQuantitySatisfiedParam
	if strings.TrimSpace(raw) == "" {
		return itemQuantitySatisfiedParam{}, fmt.Errorf("custom_recognition_param is required")
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return itemQuantitySatisfiedParam{}, err
	}

	params.Expression = strings.TrimSpace(params.Expression)
	if params.Expression == "" {
		return itemQuantitySatisfiedParam{}, fmt.Errorf("expression is required")
	}
	return params, nil
}
