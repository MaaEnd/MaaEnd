package autodelivery

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	resolveDestinationActionName = "AutoDeliveryResolveDestinationAction"
	navigateDestinationNode      = "AutoDeliveryNavigateDestination"
	areaOCRNode                  = "AutoDeliveryAreaOCR"
	destinationOCRNode           = "AutoDeliveryDestinationOCR"
)

// AutoDeliveryResolveDestinationAction 根据 Pipeline OCR 文本匹配送货终点并注入 MapNavigateAction 参数。
type AutoDeliveryResolveDestinationAction struct{}

var _ maa.CustomActionRunner = &AutoDeliveryResolveDestinationAction{}

// Run 读取 Pipeline 提供的 OCR 结果，匹配唯一终点并更新终点导航节点。
func (a *AutoDeliveryResolveDestinationAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || arg.RecognitionDetail == nil {
		log.Error().
			Str("component", resolveDestinationActionName).
			Msg("action context or recognition detail is missing")
		return false
	}

	options, err := parseNavigationOptions(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", resolveDestinationActionName).
			Msg("failed to parse action parameters")
		return false
	}

	areaText, destinationText, combined := destinationOCRFields(arg.RecognitionDetail)
	var (
		dest       destination
		match      destinationMatch
		resolveErr error
	)
	if combined {
		dest, match, resolveErr = resolveDestinationByArea(areaText, destinationText)
	} else {
		destinationText, resolveErr = recognitionText(arg.RecognitionDetail)
		if resolveErr == nil {
			dest, match, resolveErr = resolveDestination(destinationText)
		}
	}
	if resolveErr != nil {
		log.Error().
			Err(resolveErr).
			Str("component", resolveDestinationActionName).
			Str("areaText", areaText).
			Str("destinationText", destinationText).
			Float64("similarity", match.Similarity).
			Float64("runnerUpSimilarity", match.RunnerUpSimilarity).
			Float64("areaSimilarity", match.AreaSimilarity).
			Float64("areaRunnerUpSimilarity", match.AreaRunnerUp).
			Msg("failed to resolve delivery destination")
		return false
	}
	if err := ctx.OverridePipeline(buildDestinationNavigationOverride(dest, options.Zip)); err != nil {
		log.Error().
			Err(err).
			Str("component", resolveDestinationActionName).
			Str("destination", dest.ID).
			Msg("failed to inject delivery navigation parameters")
		return false
	}

	event := log.Info().
		Str("component", resolveDestinationActionName).
		Str("areaText", areaText).
		Str("destinationText", destinationText).
		Str("destination", dest.ID).
		Str("depot", dest.DepotID).
		Str("matchedArea", match.AreaText).
		Str("matchedDestination", match.DestinationText).
		Str("matchedObjective", match.ObjectiveText).
		Float64("similarity", match.Similarity).
		Float64("runnerUpSimilarity", match.RunnerUpSimilarity).
		Float64("areaSimilarity", match.AreaSimilarity).
		Float64("areaRunnerUpSimilarity", match.AreaRunnerUp).
		Str("area", dest.AreaID).
		Bool("zip", options.Zip).
		Float64("targetX", dest.Target[0]).
		Float64("targetY", dest.Target[1])
	if dest.TargetDeckY != nil {
		event.Float64("targetDeckY", *dest.TargetDeckY)
	}
	event.Msg("resolved delivery job destination")
	return true
}

// OverridePipeline 采用字段级浅合并，因此这里必须提供完整的 custom_action_param。
func buildDestinationNavigationOverride(dest destination, zip bool) map[string]any {
	path := make([]any, 0, len(dest.InitialPath)+1)
	path = append(path, dest.InitialPath...)
	path = append(path, destinationWaypoint(dest))

	param := map[string]any{
		"path": path,
		"zip":  zip,
	}
	return map[string]any{
		navigateDestinationNode: map[string]any{
			"custom_action_param": param,
		},
	}
}

func destinationWaypoint(dest destination) map[string]any {
	waypoint := map[string]any{
		"action": "NAVMESH",
		"target": dest.Target,
	}
	if dest.TargetDeckY != nil {
		waypoint["target_deck_y"] = *dest.TargetDeckY
	}
	return waypoint
}
