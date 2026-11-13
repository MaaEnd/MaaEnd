package ims

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/boolexpr"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentSetItemQuantity = "SetItemQuantity"

var _ maa.CustomActionRunner = &SetItemQuantity{}

// setItemQuantityParam is custom_action_param for SetItemQuantity (A4).
// Value is optional: an integer, an expression string (same syntax as R1), or
// omitted to read the number from this node's own recognition result.
type setItemQuantityParam struct {
	Item  string          `json:"item"`
	Value json.RawMessage `json:"value"`
}

// valueSource records where the written number came from (logging only).
type valueSource string

const (
	valueSourceParam       valueSource = "param"
	valueSourceExpression  valueSource = "expression"
	valueSourceRecognition valueSource = "recognition"
)

// SetItemQuantity writes one item quantity as an absolute value into the
// in-process IMS cache (A4).
//
// It is a volatile register: it never persists IMS.json, never changes readiness
// (hasData / updated_at) and never touches the A2 scan gates, so a later A2 sync
// stays authoritative and other tasks still scan the screen.
type SetItemQuantity struct{}

// Run implements maa.CustomActionRunner.
func (a *SetItemQuantity) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", componentSetItemQuantity).
			Msg("got nil custom action arg")
		return false
	}

	params, err := parseSetItemQuantityParam(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentSetItemQuantity).
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse params")
		return false
	}

	if err := ensureHydrated(); err != nil {
		log.Error().
			Err(err).
			Str("component", componentSetItemQuantity).
			Msg("failed to hydrate ims cache")
		return false
	}

	value, source, resolved, err := resolveSetItemQuantityValue(ctx, arg, params)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentSetItemQuantity).
			Str("item", params.Item).
			Str("source", string(source)).
			Msg("failed to resolve value")
		return false
	}

	before, after, clamped, hasData, lastSync := globalCache.setAbsolute(params.Item, value)

	log.Info().
		Str("component", componentSetItemQuantity).
		Str("item", params.Item).
		Str("source", string(source)).
		Str("resolved_value", resolved).
		Int("before", before).
		Int("after", after).
		Bool("clamped", clamped).
		Bool("has_data", hasData).
		Time("last_sync", lastSync).
		Msg("item quantity set in memory")
	return true
}

// resolveSetItemQuantityValue returns the absolute value plus its source and a
// resolved debug string. An empty value reads the number from the recognition
// result of the node this action runs on.
func resolveSetItemQuantityValue(
	ctx *maa.Context,
	arg *maa.CustomActionArg,
	params setItemQuantityParam,
) (int, valueSource, string, error) {
	raw := strings.TrimSpace(string(params.Value))
	if raw != "" && raw != "null" {
		var literal int
		if err := json.Unmarshal(params.Value, &literal); err == nil {
			return literal, valueSourceParam, strconv.Itoa(literal), nil
		}

		var expression string
		if err := json.Unmarshal(params.Value, &expression); err != nil {
			return 0, valueSourceParam, "", fmt.Errorf("value must be an integer or an expression string")
		}
		expression = strings.TrimSpace(expression)
		if expression == "" {
			return 0, valueSourceExpression, "", fmt.Errorf("value expression is empty")
		}

		resolved, _, err := boolexpr.ResolvePlaceholders(expression, func(itemID string) (int, error) {
			return globalCache.quantity(itemID), nil
		})
		if err != nil {
			return 0, valueSourceExpression, expression, fmt.Errorf("resolve value expression: %w", err)
		}

		result, err := boolexpr.Evaluate(resolved)
		if err != nil {
			return 0, valueSourceExpression, resolved, fmt.Errorf("evaluate value expression: %w", err)
		}

		number, ok := result.(int)
		if !ok {
			return 0, valueSourceExpression, resolved, fmt.Errorf("value expression must evaluate to an integer, got %T", result)
		}
		return number, valueSourceExpression, resolved, nil
	}

	number, err := valueFromRecognition(ctx, arg)
	if err != nil {
		return 0, valueSourceRecognition, "", err
	}
	return number, valueSourceRecognition, strconv.Itoa(number), nil
}

// valueFromRecognition reads an integer from this node's recognition result.
// And nodes are resolved along their native box_index (same selection as
// ExpressionRecognition); magnitude suffixes such as 1.38万 are handled by ocrnum.
func valueFromRecognition(ctx *maa.Context, arg *maa.CustomActionArg) (int, error) {
	if arg.RecognitionDetail == nil {
		return 0, fmt.Errorf("no recognition detail for this node; pass value explicitly")
	}

	detail := arg.RecognitionDetail
	if ctx != nil && strings.TrimSpace(arg.CurrentTaskName) != "" {
		selected, err := recogtarget.SelectDetail(ctx, arg.CurrentTaskName, detail)
		if err != nil {
			log.Warn().
				Err(err).
				Str("component", componentSetItemQuantity).
				Str("node", arg.CurrentTaskName).
				Msg("failed to select recognition target, falling back to the raw detail")
		} else {
			detail = selected
		}
	}

	number, err := ocrnum.Extract(detail)
	if err != nil {
		return 0, fmt.Errorf("extract number from recognition detail: %w", err)
	}
	return number, nil
}

func parseSetItemQuantityParam(raw string) (setItemQuantityParam, error) {
	var params setItemQuantityParam
	if strings.TrimSpace(raw) == "" {
		return setItemQuantityParam{}, fmt.Errorf("custom_action_param is required")
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return setItemQuantityParam{}, err
	}
	params.Item = strings.TrimSpace(params.Item)
	if params.Item == "" {
		return setItemQuantityParam{}, fmt.Errorf("item is required")
	}
	return params, nil
}
