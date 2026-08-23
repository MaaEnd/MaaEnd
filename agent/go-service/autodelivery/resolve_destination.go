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

// AutoDeliveryResolveDestinationAction 根据 Pipeline OCR 文本匹配送货终点并选择对应的生成路线节点。
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

	log.Info().
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
		Str("routeNode", selectRouteNode(dest.RouteNode, dest.ZipRouteNode, options.Zip)).
		Msg("resolved delivery job destination")
	return true
}

func buildDestinationNavigationOverride(dest destination, zip bool) map[string]any {
	return map[string]any{
		navigateDestinationNode: map[string]any{
			"custom_action": "SubTask",
			"custom_action_param": map[string]any{
				"sub": []string{selectRouteNode(dest.RouteNode, dest.ZipRouteNode, zip)},
			},
		},
	}
}
