package autodelivery

import (
	"errors"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	resolveDestinationActionName = "AutoDeliveryResolveDestinationAction"
	navigateDestinationNode      = "AutoDeliveryNavigateDestination"
	retryNavigateDestinationNode = "AutoDeliveryRetryNavigateDestination"
	afterResolveDestinationNode  = "AutoDeliveryAfterResolveDestination"
	areaTextNode                 = "AutoDeliveryCheckAreaText"
	destinationTextNode          = "AutoDeliveryCheckDestinationText"
)

// AutoDeliveryResolveDestinationAction 根据 Pipeline OCR 文本或已确认的终点 ID 选择对应的生成路线节点。
type AutoDeliveryResolveDestinationAction struct{}

var _ maa.CustomActionRunner = &AutoDeliveryResolveDestinationAction{}

// Run 读取 Pipeline 提供的 OCR 结果或精确终点 ID，并更新终点导航节点。
func (a *AutoDeliveryResolveDestinationAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || arg.RecognitionDetail == nil {
		log.Error().
			Str("component", resolveDestinationActionName).
			Msg("action context or recognition detail is missing")
		return false
	}

	selection, err := parseDestinationSelection(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", resolveDestinationActionName).
			Msg("failed to parse action parameters")
		return false
	}
	options, err := loadNavigationOptions(ctx, navigateDestinationNode)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", resolveDestinationActionName).
			Str("node", navigateDestinationNode).
			Msg("failed to load navigation options")
		return false
	}

	var (
		areaText        string
		destinationText string
		dest            destination
		match           destinationMatch
		resolveErr      error
	)
	if selection.DestinationID != "" {
		dest, resolveErr = getDestination(selection.DestinationID)
	} else {
		var combined bool
		areaText, destinationText, combined = destinationOCRFields(arg.RecognitionDetail)
		if combined {
			dest, match, resolveErr = resolveDestinationByArea(areaText, destinationText)
		} else {
			destinationText, resolveErr = recognitionText(arg.RecognitionDetail)
			if resolveErr == nil {
				dest, match, resolveErr = resolveDestination(destinationText)
			}
		}
	}
	if resolveErr != nil {
		var ambiguity *recycleBinAmbiguityError
		if errors.As(resolveErr, &ambiguity) {
			if err := ctx.OverridePipeline(buildRecycleBinResolutionOverride(ambiguity.AreaID)); err != nil {
				log.Error().
					Err(err).
					Str("component", resolveDestinationActionName).
					Str("area", ambiguity.AreaID).
					Msg("failed to configure recycle bin map resolution")
				return false
			}

			candidateIDs := make([]string, 0, len(ambiguity.Candidates))
			for _, candidate := range ambiguity.Candidates {
				candidateIDs = append(candidateIDs, candidate.ID)
			}
			log.Info().
				Str("component", resolveDestinationActionName).
				Str("areaText", areaText).
				Str("destinationText", destinationText).
				Str("area", ambiguity.AreaID).
				Strs("candidates", candidateIDs).
				Msg("delivery recycle bin needs map resolution")
			return true
		}

		log.Error().
			Err(resolveErr).
			Str("component", resolveDestinationActionName).
			Str("requestedDestination", selection.DestinationID).
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
	maafocus.Print(ctx, i18n.T("autodelivery.focus.destination_resolved", destinationDisplayName(dest)))

	log.Info().
		Str("component", resolveDestinationActionName).
		Str("requestedDestination", selection.DestinationID).
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
		Str("retryRouteNode", dest.RetryRouteNode).
		Msg("resolved delivery job destination")
	return true
}

func destinationDisplayName(dest destination) string {
	if dest.Kind == destinationKindRecycleBin {
		areaName := localizedName(dest.AreaNames, dest.AreaID)
		return i18n.T("autodelivery.destination.recycle_bin", areaName, dest.SerialID)
	}
	return localizedName(dest.Names, dest.ID)
}

func buildDestinationNavigationOverride(dest destination, zip bool) map[string]any {
	override := map[string]any{
		afterResolveDestinationNode: map[string]any{
			"next": defaultDestinationFlow(),
		},
		navigateDestinationNode: map[string]any{
			"custom_action": "SubTask",
			"custom_action_param": map[string]any{
				"sub": []string{selectRouteNode(dest.RouteNode, dest.ZipRouteNode, zip)},
			},
		},
		retryNavigateDestinationNode: map[string]any{
			"enabled": false,
		},
	}
	if dest.RetryRouteNode != "" {
		override[retryNavigateDestinationNode] = map[string]any{
			"enabled":       true,
			"custom_action": "SubTask",
			"custom_action_param": map[string]any{
				"sub": []string{dest.RetryRouteNode},
			},
		}
	}
	return override
}

func buildRecycleBinResolutionOverride(areaID string) map[string]any {
	return map[string]any{
		afterResolveDestinationNode: map[string]any{
			"next": []string{
				"AutoDeliveryViewRecycleBin" + areaID + "Map",
				"AutoDeliveryStartTrackingRecycleBin" + areaID,
			},
		},
	}
}

func defaultDestinationFlow() []string {
	return []string{
		"AutoDeliveryCancelCurrentJobTracking",
		"AutoDeliveryCheckCurrentJobTrackingAlreadyOff",
	}
}
