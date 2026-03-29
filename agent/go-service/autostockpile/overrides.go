package autostockpile

import (
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func buildSelectionPipelineOverride(ctx *maa.Context, selection SelectionResult, decision quantityDecision) (map[string]any, error) {
	override := map[string]any{
		relayNodeDecisionReadyNodeName: map[string]any{
			"enabled": false,
		},
		selectedGoodsClickNodeName: map[string]any{
			"enabled":  true,
			"template": []string{BuildTemplatePath(selection.ProductID)},
		},
		swipeMaxNodeName: map[string]any{
			"enabled": decision.Mode == quantityModeSwipeMax,
		},
	}

	if decision.Mode != quantityModeSwipeSpecificQuantity {
		override[swipeSpecificQuantityNodeName] = map[string]any{
			"enabled": false,
		}
		return override, nil
	}

	customActionParam, err := loadSwipeSpecificQuantityCustomActionParam(ctx)
	if err != nil {
		return nil, err
	}

	override[swipeSpecificQuantityNodeName] = buildSwipeSpecificQuantityOverride(customActionParam, decision.Target)
	return override, nil
}

func buildSwipeSpecificQuantityOverride(customActionParam map[string]any, target int) map[string]any {
	clonedParam := make(map[string]any, len(customActionParam))
	for key, item := range customActionParam {
		clonedParam[key] = item
	}
	clonedParam["Target"] = target

	return map[string]any{
		"enabled": true,
		"action": map[string]any{
			"param": map[string]any{
				"custom_action_param": clonedParam,
			},
		},
	}
}

func enableDecisionReadyOverride(ctx *maa.Context) error {
	return ctx.OverridePipeline(map[string]any{
		relayNodeDecisionReadyNodeName: map[string]any{
			"enabled": true,
		},
	})
}

func overrideSkipBranch(ctx *maa.Context, currentTaskName string) error {
	if err := ctx.OverridePipeline(buildSkipResetOverride()); err != nil {
		return fmt.Errorf("reset skip pipeline state: %w", err)
	}

	if err := ctx.OverrideNext(currentTaskName, []maa.NextItem{{Name: skipNodeName}}); err != nil {
		return fmt.Errorf("override next for skip branch: %w", err)
	}

	return nil
}

func buildSkipResetOverride() map[string]any {
	return map[string]any{
		relayNodeDecisionReadyNodeName: map[string]any{
			"enabled": false,
		},
		selectedGoodsClickNodeName: map[string]any{
			"enabled": false,
		},
		swipeMaxNodeName: map[string]any{
			"enabled": false,
		},
		swipeSpecificQuantityNodeName: map[string]any{
			"enabled": false,
		},
	}
}

func overrideLocateGoodsRecognition(ctx *maa.Context, templatePath string, goodsROI []int) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	return ctx.OverridePipeline(map[string]any{
		locateGoodsNodeName: map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"template": []string{templatePath},
					"roi":      append([]int(nil), goodsROI...),
				},
			},
		},
	})
}

func resetSelectedGoodsClickROIY(ctx *maa.Context) {
	if overrideErr := overrideSelectedGoodsClickROIY(ctx, selectedGoodsClickResetY); overrideErr != nil {
		log.Warn().
			Err(overrideErr).
			Str("component", autoStockpileComponent).
			Str("node", selectedGoodsClickNodeName).
			Int("roi_y", selectedGoodsClickResetY).
			Msg("failed to reset selected goods click roi y")
	}
}

func overrideSelectedGoodsClickROIY(ctx *maa.Context, y int) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	node, err := ctx.GetNode(selectedGoodsClickNodeName)
	if err != nil {
		return err
	}

	roi, err := recognitionParamROI(node)
	if err != nil {
		return err
	}
	if len(roi) != 4 {
		return fmt.Errorf("invalid roi length %d", len(roi))
	}

	roi = append([]int(nil), roi...)
	roi[1] = y

	return ctx.OverridePipeline(map[string]any{
		selectedGoodsClickNodeName: map[string]any{
			"roi": roi,
		},
	})
}

func recognitionParamROI(node *maa.Node) ([]int, error) {
	if node == nil || node.Recognition == nil || node.Recognition.Param == nil {
		return nil, fmt.Errorf("node %s missing recognition param", selectedGoodsClickNodeName)
	}

	var target maa.Target
	switch param := node.Recognition.Param.(type) {
	case *maa.TemplateMatchParam:
		target = param.ROI
	case *maa.FeatureMatchParam:
		target = param.ROI
	case *maa.ColorMatchParam:
		target = param.ROI
	case *maa.OCRParam:
		target = param.ROI
	case *maa.NeuralNetworkClassifyParam:
		target = param.ROI
	case *maa.NeuralNetworkDetectParam:
		target = param.ROI
	case *maa.CustomRecognitionParam:
		target = param.ROI
	default:
		return nil, fmt.Errorf("node %s has unsupported recognition param type %T", selectedGoodsClickNodeName, node.Recognition.Param)
	}

	rect, err := target.AsRect()
	if err != nil {
		return nil, fmt.Errorf("node %s roi: %w", selectedGoodsClickNodeName, err)
	}

	return []int{rect[0], rect[1], rect[2], rect[3]}, nil
}

func overrideGoodsPriceROI(ctx *maa.Context, goodsROI []int) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	return ctx.OverridePipeline(map[string]any{
		goodsPriceNodeName: map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"roi": append([]int(nil), goodsROI...),
				},
			},
		},
	})
}
