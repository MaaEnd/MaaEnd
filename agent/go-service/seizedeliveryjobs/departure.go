package seizedeliveryjobs

import (
	"encoding/json"
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	deliveryZiplinePolicyNever  = "Never"
	deliveryZiplinePolicyLazy   = "Lazy"
	deliveryZiplinePolicyActive = "Active"
)

type resolveDeliveryDestinationParam struct {
	ZiplinePolicy string `json:"zipline_policy"`
}

const (
	seizeDeliveryJobsResolveDestinationComponent = "SeizeDeliveryJobsResolveDestinationAction"
	seizeDeliveryJobsNavigateNode                = "SeizeDeliveryJobsNavigate"
	seizeDeliveryJobsRetryNavigateNode           = "SeizeDeliveryJobsRetryNavigate"
	seizeDeliveryJobsAreaOCRNode                 = "SeizeDeliveryJobsAreaOCR"
	seizeDeliveryJobsDestinationOCRNode          = "SeizeDeliveryJobsDestinationOCR"
)

// SeizeDeliveryJobsResolveDestinationAction resolves Pipeline OCR text and injects the matching MapNavigateAction parameters.
type SeizeDeliveryJobsResolveDestinationAction struct{}

var _ maa.CustomActionRunner = &SeizeDeliveryJobsResolveDestinationAction{}

// Run reads OCR results supplied by Pipeline, resolves a unique destination, and updates the Pipeline navigation nodes.
func (a *SeizeDeliveryJobsResolveDestinationAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || arg.RecognitionDetail == nil {
		log.Error().
			Str("component", seizeDeliveryJobsResolveDestinationComponent).
			Msg("action context or recognition detail is missing")
		return false
	}

	param, err := parseResolveDeliveryDestinationParam(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", seizeDeliveryJobsResolveDestinationComponent).
			Msg("failed to parse action parameters")
		return false
	}

	areaText, destinationText, combined := deliveryDestinationOCRFields(arg.RecognitionDetail)
	var (
		destination deliveryDestination
		match       deliveryDestinationMatch
		resolveErr  error
	)
	if combined {
		destination, match, resolveErr = resolveDeliveryDestinationByArea(areaText, destinationText)
	} else {
		destinationText, resolveErr = deliveryObjectiveText(arg.RecognitionDetail)
		if resolveErr == nil {
			destination, match, resolveErr = resolveDeliveryDestination(destinationText)
		}
	}
	if resolveErr != nil {
		log.Error().
			Err(resolveErr).
			Str("component", seizeDeliveryJobsResolveDestinationComponent).
			Str("areaText", areaText).
			Str("destinationText", destinationText).
			Float64("similarity", match.Similarity).
			Float64("runnerUpSimilarity", match.RunnerUpSimilarity).
			Float64("areaSimilarity", match.AreaSimilarity).
			Float64("areaRunnerUpSimilarity", match.AreaRunnerUp).
			Msg("failed to resolve delivery destination")
		return false
	}

	if err := ctx.OverridePipeline(buildDeliveryNavigationOverride(destination, param.ZiplinePolicy)); err != nil {
		log.Error().
			Err(err).
			Str("component", seizeDeliveryJobsResolveDestinationComponent).
			Str("destination", destination.ID).
			Msg("failed to inject delivery navigation parameters")
		return false
	}

	event := log.Info().
		Str("component", seizeDeliveryJobsResolveDestinationComponent).
		Str("areaText", areaText).
		Str("destinationText", destinationText).
		Str("destination", destination.ID).
		Str("mapNavigatorSource", destination.MapNavigatorSourceID).
		Str("matchedArea", match.AreaText).
		Str("matchedDestination", match.DestinationText).
		Str("matchedObjective", match.ObjectiveText).
		Float64("similarity", match.Similarity).
		Float64("runnerUpSimilarity", match.RunnerUpSimilarity).
		Float64("areaSimilarity", match.AreaSimilarity).
		Float64("areaRunnerUpSimilarity", match.AreaRunnerUp).
		Str("area", destination.AreaID).
		Str("navigatorZone", destination.MapNavigatorZone).
		Str("ziplinePolicy", param.ZiplinePolicy).
		Float64("targetX", destination.MapNavigatorTarget[0]).
		Float64("targetY", destination.MapNavigatorTarget[1])
	if destination.MapNavigatorTargetDeckY != nil {
		event.Float64("targetDeckY", *destination.MapNavigatorTargetDeckY)
	}
	event.Msg("resolved delivery job destination")
	return true
}

func parseResolveDeliveryDestinationParam(paramJSON string) (resolveDeliveryDestinationParam, error) {
	param := resolveDeliveryDestinationParam{ZiplinePolicy: deliveryZiplinePolicyLazy}
	if paramJSON != "" {
		if err := json.Unmarshal([]byte(paramJSON), &param); err != nil {
			return resolveDeliveryDestinationParam{}, fmt.Errorf("unmarshal parameters: %w", err)
		}
	}
	if param.ZiplinePolicy == "" {
		param.ZiplinePolicy = deliveryZiplinePolicyLazy
	}
	switch param.ZiplinePolicy {
	case deliveryZiplinePolicyNever, deliveryZiplinePolicyLazy, deliveryZiplinePolicyActive:
		return param, nil
	default:
		return resolveDeliveryDestinationParam{}, fmt.Errorf(
			"zipline_policy must be one of %q, %q, %q",
			deliveryZiplinePolicyNever,
			deliveryZiplinePolicyLazy,
			deliveryZiplinePolicyActive,
		)
	}
}

func deliveryDestinationOCRFields(detail *maa.RecognitionDetail) (areaText, destinationText string, ok bool) {
	areaDetail := findDeliveryRecognitionDetail(detail, seizeDeliveryJobsAreaOCRNode)
	destinationDetail := findDeliveryRecognitionDetail(detail, seizeDeliveryJobsDestinationOCRNode)
	if areaDetail == nil || destinationDetail == nil {
		return "", "", false
	}

	areaText, areaErr := deliveryObjectiveText(areaDetail)
	destinationText, destinationErr := deliveryObjectiveText(destinationDetail)
	if areaErr != nil || destinationErr != nil {
		return areaText, destinationText, true
	}
	return areaText, destinationText, true
}

func findDeliveryRecognitionDetail(detail *maa.RecognitionDetail, name string) *maa.RecognitionDetail {
	if detail == nil {
		return nil
	}
	if detail.Name == name {
		return detail
	}
	for _, child := range detail.CombinedResult {
		if found := findDeliveryRecognitionDetail(child, name); found != nil {
			return found
		}
	}
	return nil
}

// OverridePipeline performs a field-level shallow merge, so each custom_action_param is intentionally complete.
func buildDeliveryNavigationOverride(destination deliveryDestination, ziplinePolicy string) map[string]any {
	initialPath := make([]any, 0, len(destination.InitialPathPrefix)+len(destination.InitialPathSuffix)+1)
	initialPath = append(initialPath, destination.InitialPathPrefix...)
	initialPath = append(initialPath, destination.InitialPathSuffix...)
	initialPath = append(initialPath, deliveryDestinationWaypoint(destination))
	retryPath := []any{deliveryDestinationWaypoint(destination)}

	return map[string]any{
		seizeDeliveryJobsNavigateNode: map[string]any{
			"custom_action_param": map[string]any{
				"map_name":       destination.MapNavigatorZone,
				"zipline_policy": ziplinePolicy,
				"path":           initialPath,
			},
		},
		seizeDeliveryJobsRetryNavigateNode: map[string]any{
			"custom_action_param": map[string]any{
				"map_name":       destination.MapNavigatorZone,
				"zipline_policy": deliveryZiplinePolicyNever,
				"path":           retryPath,
			},
		},
	}
}

func deliveryDestinationWaypoint(destination deliveryDestination) map[string]any {
	waypoint := map[string]any{
		"action": "NAVMESH",
		"target": destination.MapNavigatorTarget,
	}
	if destination.MapNavigatorTargetDeckY != nil {
		waypoint["target_deck_y"] = *destination.MapNavigatorTargetDeckY
	}
	return waypoint
}
