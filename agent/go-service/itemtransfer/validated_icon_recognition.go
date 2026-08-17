package itemtransfer

import (
	"image"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const validatedIconRecognitionName = "ItemTransferValidatedIconRecognition"

var _ maa.CustomRecognitionRunner = &ValidatedIconRecognition{}

// ValidatedIconRecognition 先识别当前页所有候选格，再用物品分类对每个格子做单格反查。
type ValidatedIconRecognition struct{}

// Run 只返回通过分类反查且 ID 与当前任务目标一致的第一个格子。
func (r *ValidatedIconRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", validatedIconRecognitionName).Msg("invalid recognition context or argument")
		return nil, false
	}

	params, err := iconrecognition.ParseParams(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", validatedIconRecognitionName).Msg("failed to parse recognition parameters")
		return nil, false
	}
	if len(params.ItemIDs) != 1 || params.ItemIDs[0] == "" {
		log.Error().Int("item_id_count", len(params.ItemIDs)).Str("component", validatedIconRecognitionName).Msg("exactly one target item id is required")
		return nil, false
	}
	targetID := params.ItemIDs[0]
	if len(params.ItemFilters) != 1 || params.ItemFilters[0] == "" {
		log.Error().Int("item_filter_count", len(params.ItemFilters)).Str("item_id", targetID).Str("component", validatedIconRecognitionName).Msg("exactly one target item filter is required")
		return nil, false
	}
	categoryFilter := params.ItemFilters[0]

	initial, rawDetail, err := runIconRecognition(ctx, arg.Img, arg.Roi, params)
	if err != nil {
		log.Error().Err(err).Str("item_id", targetID).Str("component", validatedIconRecognitionName).Msg("initial icon recognition failed")
		return nil, false
	}
	if initial.Error != nil {
		if initial.Error.Code != iconrecognition.ErrorCodeNoMatch && initial.Error.Code != iconrecognition.ErrorCodeGridDetectionFailed {
			log.Error().
				Str("error_code", string(initial.Error.Code)).
				Str("error_message", initial.Error.Message).
				Str("item_id", targetID).
				Str("component", validatedIconRecognitionName).
				Msg("initial icon recognition returned an error")
		}
		return nil, false
	}
	if len(initial.Matches) == 0 {
		return nil, false
	}

	validationParams := iconrecognition.NewParams(
		iconrecognition.WithGridType(iconrecognition.GridTypeSingleROI),
		iconrecognition.WithItemFilters(categoryFilter),
		iconrecognition.WithTuningFrom(params),
	)

	for _, candidate := range initial.Matches {
		if candidate.CellBox[2] <= 0 || candidate.CellBox[3] <= 0 {
			continue
		}

		validated, _, err := runIconRecognition(ctx, arg.Img, candidate.CellBox, validationParams)
		if err != nil {
			log.Debug().Err(err).Str("item_id", targetID).Interface("cell_box", candidate.CellBox).Str("component", validatedIconRecognitionName).Msg("single cell validation failed")
			continue
		}
		if validated.Error != nil {
			log.Debug().
				Str("error_code", string(validated.Error.Code)).
				Str("error_message", validated.Error.Message).
				Str("item_id", targetID).
				Interface("cell_box", candidate.CellBox).
				Str("component", validatedIconRecognitionName).
				Msg("single cell validation returned an error")
			continue
		}
		if !containsItemID(validated.Matches, targetID) {
			log.Debug().Str("item_id", targetID).Interface("cell_box", candidate.CellBox).Str("component", validatedIconRecognitionName).Msg("candidate rejected by reverse validation")
			continue
		}

		return &maa.CustomRecognitionResult{
			Box:    candidate.CellBox,
			Detail: rawDetail,
		}, true
	}

	return nil, false
}

func runIconRecognition(
	ctx *maa.Context,
	img image.Image,
	roi maa.Rect,
	params iconrecognition.Params,
) (iconrecognition.Detail, string, error) {
	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeCustom,
		&maa.CustomRecognitionParam{
			ROI:                    maa.NewTargetRect(roi),
			CustomRecognition:      iconrecognition.CustomRecognitionName,
			CustomRecognitionParam: params,
		},
		img,
	)
	if err != nil {
		return iconrecognition.Detail{}, "", err
	}
	return iconrecognition.ParseRecognitionDetail(detail)
}

func containsItemID(matches []iconrecognition.Match, targetID string) bool {
	for _, match := range matches {
		if match.ItemID == targetID {
			return true
		}
	}
	return false
}
