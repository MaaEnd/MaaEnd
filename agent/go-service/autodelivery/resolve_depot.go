package autodelivery

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	resolveDepotActionName = "AutoDeliveryResolveDepotAction"
	navigateDepotNode      = "AutoDeliveryNavigateDepot"
	retryNavigateDepotNode = "AutoDeliveryRetryNavigateDepot"
)

// AutoDeliveryResolveDepotAction 根据区域 OCR 匹配仓储节点并注入对应的 MapNavigator 路线。
type AutoDeliveryResolveDepotAction struct{}

var _ maa.CustomActionRunner = &AutoDeliveryResolveDepotAction{}

// Run 从当前任务详情读取区域文本，并更新仓储导航节点。
func (a *AutoDeliveryResolveDepotAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || arg.RecognitionDetail == nil {
		log.Error().
			Str("component", resolveDepotActionName).
			Msg("action context or recognition detail is missing")
		return false
	}

	options, err := parseNavigationOptions(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", resolveDepotActionName).
			Msg("failed to parse action parameters")
		return false
	}

	areaDetail := findRecognitionDetail(arg.RecognitionDetail, areaOCRNode)
	if areaDetail == nil {
		log.Error().
			Str("component", resolveDepotActionName).
			Msg("delivery area OCR detail is missing")
		return false
	}
	areaText, err := recognitionText(areaDetail)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", resolveDepotActionName).
			Msg("failed to read delivery area OCR")
		return false
	}
	areas, err := getAreas()
	if err != nil {
		log.Error().
			Err(err).
			Str("component", resolveDepotActionName).
			Msg("failed to load delivery areas")
		return false
	}
	area, match, err := resolveArea(areaText, areas)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", resolveDepotActionName).
			Str("areaText", areaText).
			Float64("similarity", match.Similarity).
			Float64("runnerUpSimilarity", match.RunnerUpSimilarity).
			Msg("failed to resolve delivery depot from area OCR")
		return false
	}
	if area.DepotID == "" {
		log.Error().
			Str("component", resolveDepotActionName).
			Str("area", area.ID).
			Str("areaText", areaText).
			Msg("delivery area has no associated depot")
		return false
	}

	route, err := getDepot(area.DepotID)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", resolveDepotActionName).
			Str("depot", area.DepotID).
			Msg("failed to resolve delivery depot")
		return false
	}
	if err := ctx.OverridePipeline(buildDepotNavigationOverride(route, options.Zip)); err != nil {
		log.Error().
			Err(err).
			Str("component", resolveDepotActionName).
			Str("depot", route.ID).
			Msg("failed to apply depot navigation")
		return false
	}

	log.Info().
		Str("component", resolveDepotActionName).
		Str("depot", route.ID).
		Str("areaText", areaText).
		Str("map", route.Map).
		Int("pathPoints", len(route.Path)).
		Int("retryPathPoints", len(route.RetryPath)).
		Bool("zip", options.Zip).
		Msg("configured delivery depot navigation")
	return true
}

// OverridePipeline 采用字段级浅合并，因此这里必须提供完整的 custom_action_param。
func buildDepotNavigationOverride(route depot, zip bool) map[string]any {
	override := map[string]any{
		navigateDepotNode: map[string]any{
			"custom_action": "MapNavigateAction",
			"custom_action_param": map[string]any{
				"path": route.Path,
				"zip":  zip,
			},
		},
		retryNavigateDepotNode: map[string]any{
			"enabled": false,
		},
	}
	if len(route.RetryPath) != 0 {
		override[retryNavigateDepotNode] = map[string]any{
			"enabled":       true,
			"custom_action": "MapNavigateAction",
			"custom_action_param": map[string]any{
				"path": route.RetryPath,
			},
		}
	}
	return override
}
