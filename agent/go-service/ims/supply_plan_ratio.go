package ims

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/boolexpr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentSupplyPlanSelectLowestRatio = "SupplyPlanSelectLowestRatio"

var _ maa.CustomActionRunner = &SupplyPlanSelectLowestRatio{}

type supplyPlanCandidate struct {
	name       string
	expression string
}

type supplyPlanSelection struct {
	name                string
	current             int
	target              int
	continueAfterTarget bool
}

type supplyPlanSelectLowestRatioParam struct {
	ContinueAfterTarget bool `json:"continue_after_target"`
}

// SupplyPlanSelectLowestRatio selects the lowest-ratio material at task start.
// It reuses the configured IMS expressions, including weighted experience, and
// limits the dispatch list to that material for the rest of the current task.
// With continue_after_target enabled, farming continues once all targets are met.
type SupplyPlanSelectLowestRatio struct{}

// Run implements maa.CustomActionRunner.
func (a *SupplyPlanSelectLowestRatio) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", componentSupplyPlanSelectLowestRatio).Msg("nil context or arg")
		return false
	}
	params, err := parseSupplyPlanSelectLowestRatioParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Str("component", componentSupplyPlanSelectLowestRatio).
			Msg("failed to parse target selection params")
		return false
	}
	if err := ensureHydrated(); err != nil {
		log.Error().Err(err).Str("component", componentSupplyPlanSelectLowestRatio).
			Msg("failed to hydrate ims cache")
		return false
	}

	candidates, err := readSupplyPlanCandidates(ctx)
	if err != nil {
		log.Error().Err(err).Str("component", componentSupplyPlanSelectLowestRatio).
			Msg("failed to read inventory targets")
		return false
	}

	selection, err := selectSupplyPlanTarget(candidates, globalCache.itemsCopy(), params.ContinueAfterTarget)
	if err != nil {
		log.Error().Err(err).Str("component", componentSupplyPlanSelectLowestRatio).
			Msg("failed to select inventory target")
		return false
	}
	next := []maa.NextItem{{Name: "SupplyPlanAllTargetsMet"}}
	if selection.name != "" {
		next = []maa.NextItem{{Name: selection.name}, {Name: "SupplyPlanTargetComplete"}}
		if selection.continueAfterTarget {
			// Enter the existing material setup directly, even if already stocked.
			node, err := ctx.GetNode(selection.name)
			if err != nil {
				log.Error().Err(err).Str("component", componentSupplyPlanSelectLowestRatio).
					Msg("failed to read selected material setup")
				return false
			}
			next = node.Next
			// Keep the existing reward/repeat flow after the last target is met.
			rewardMet := strings.Replace(selection.name, "SupplyPlanInsufficient_", "SupplyPlanRewardItemMet_", 1)
			if err := ctx.OverridePipeline(map[string]any{
				rewardMet: map[string]any{"enabled": false},
			}); err != nil {
				log.Error().Err(err).Str("component", componentSupplyPlanSelectLowestRatio).
					Msg("failed to enable farming beyond inventory target")
				return false
			}
		}
	}
	if err := ctx.OverrideNext("SupplyPlanDispatch", next); err != nil {
		log.Error().Err(err).Str("component", componentSupplyPlanSelectLowestRatio).
			Msg("failed to set fixed inventory target")
		return false
	}
	log.Info().Str("component", componentSupplyPlanSelectLowestRatio).
		Str("node", selection.name).Int("current", selection.current).Int("target", selection.target).
		Bool("continue_after_target", selection.continueAfterTarget).
		Msg("selected fixed inventory target for this task")
	return true
}

func parseSupplyPlanSelectLowestRatioParam(raw string) (supplyPlanSelectLowestRatioParam, error) {
	var params supplyPlanSelectLowestRatioParam
	if strings.TrimSpace(raw) == "" {
		return params, nil
	}
	err := json.Unmarshal([]byte(raw), &params)
	return params, err
}

// readSupplyPlanCandidates reads expressions after the task's input overrides.
// Keeping the dispatch list and R1 nodes as the source avoids a second item map.
func readSupplyPlanCandidates(ctx *maa.Context) ([]supplyPlanCandidate, error) {
	dispatch, err := ctx.GetNode("SupplyPlanDispatch")
	if err != nil {
		return nil, err
	}
	candidates := make([]supplyPlanCandidate, 0, len(dispatch.Next))
	for _, next := range dispatch.Next {
		if next.Name == "SupplyPlanAllTargetsMet" {
			continue
		}
		node, err := ctx.GetNode(next.Name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", next.Name, err)
		}
		if node.Enabled != nil && !*node.Enabled {
			continue
		}
		if node.Recognition == nil {
			return nil, fmt.Errorf("%s: missing inventory recognition", next.Name)
		}
		raw, err := json.Marshal(node.Recognition.Param)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", next.Name, err)
		}
		var params struct {
			Quantity itemQuantitySatisfiedParam `json:"custom_recognition_param"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, fmt.Errorf("%s: %w", next.Name, err)
		}
		candidates = append(candidates, supplyPlanCandidate{next.Name, params.Quantity.Expression})
	}
	return candidates, nil
}

// selectSupplyPlanTarget preserves candidate order when ratios are equal.
func selectSupplyPlanTarget(candidates []supplyPlanCandidate, items map[string]int, continueAfterTarget bool) (supplyPlanSelection, error) {
	var selected supplyPlanSelection
	var lowest *big.Rat
	understocked := 0
	for _, candidate := range candidates {
		current, target, err := supplyPlanQuantities(candidate.expression, items)
		if err != nil {
			return supplyPlanSelection{}, fmt.Errorf("%s: %w", candidate.name, err)
		}
		if target <= 0 || (!continueAfterTarget && current >= target) {
			continue
		}
		if current < target {
			understocked++
		}
		// Exact fractions avoid truncating every unfinished ratio to zero or
		// overflowing a cross multiplication for large inventory targets.
		ratio := big.NewRat(int64(current), int64(target))
		if lowest == nil || ratio.Cmp(lowest) < 0 {
			lowest = ratio
			selected = supplyPlanSelection{name: candidate.name, current: current, target: target}
		}
	}
	// When at most one material is understocked, meeting the selected target
	// satisfies the whole plan. Continue that material without another scan.
	selected.continueAfterTarget = continueAfterTarget && selected.name != "" && understocked <= 1
	return selected, nil
}

// supplyPlanQuantities evaluates the same current >= target expression as R1.
func supplyPlanQuantities(expression string, items map[string]int) (int, int, error) {
	resolved, _, err := boolexpr.ResolvePlaceholders(expression, func(itemID string) (int, error) {
		return items[itemID], nil
	})
	if err != nil {
		return 0, 0, err
	}
	left, right, ok := strings.Cut(resolved, ">=")
	if !ok {
		return 0, 0, fmt.Errorf("expected current >= target expression")
	}
	currentValue, err := boolexpr.Evaluate(left)
	if err != nil {
		return 0, 0, err
	}
	targetValue, err := boolexpr.Evaluate(right)
	if err != nil {
		return 0, 0, err
	}
	current, currentOK := currentValue.(int)
	target, targetOK := targetValue.(int)
	if !currentOK || !targetOK {
		return 0, 0, fmt.Errorf("inventory quantities must be integers")
	}
	return current, target, nil
}
