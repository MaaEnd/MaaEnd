package itemtransfer

import (
	"encoding/json"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

const (
	FirstX       = 161
	FirstY       = 217
	LastX        = 643
	LastY        = 423
	SquareSize   = 64
	GridInterval = 5
)

const (
	ToolTipCursorOffset = 32
	TooltipRoiScaleX    = 275
	TooltipRoiScaleY    = 130
)

// const (
// 	OCRFilter = "^(?![^a-zA-Z0-9]*(?:升序|降序|默认|品质|一键存放|材料|战术物品|消耗品|功能设备|普通设备|培养晶核)[^a-zA-Z0-9]*$)[^a-zA-Z0-9]+$"
// )

type RepoLocate struct{}

func (*RepoLocate) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	tasker := ctx.GetTasker()
	ctrl := tasker.GetController()
	var userSetting map[string]any

	err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &userSetting)
	if err != nil {
		log.Error().
			Err(err).
			Str("raw_json", arg.CustomRecognitionParam).
			Msg("Seems that we have received bad params")
		return nil, false
	}
	log.Debug().
		Str("ItemName", userSetting["ItemName"].(string)).
		Any("ContainerContent", userSetting["ContainerContent"]).
		Msg("User setting initialized")

	itemName := userSetting["ItemName"].(string)
	//containerContent := userSetting["ContainerContent"] //todo put this into use

	for row := range 1 { //todo change to 3
		for col := range 8 {

			// Step 1 - Hover to item
			if !HoverOnto(ctx, row, col) {
				log.Error().
					Int("grid_row_y", row).
					Int("grid_col_x", col).
					Msg("Failed to hover onto item")
				return nil, false
			}

			// Step 2 - Make screenshot
			log.Debug().
				Int("grid_row_y", row).
				Int("grid_col_x", col).
				Msg("Start Capture")
			ctrl.PostScreencap().Wait()
			log.Debug().
				Int("grid_row_y", row).
				Int("grid_col_x", col).
				Msg("Done Capture")

			// Step 3 - Call original OCR
			log.Debug().Msg("Starting Recognition")
			detail := ctx.RunRecognitionDirect(
				maa.NodeRecognitionTypeOCR,
				maa.NodeOCRParam{
					ROI: maa.NewTargetRect(
						RepoRoi(row, col),
					),
					OrderBy:  "Expected",
					Expected: []string{itemName},
				},
				ctrl.CacheImage(),
			)
			log.Debug().Msg("Done Recognition")
			if detail.Hit {
				return &maa.CustomRecognitionResult{
					Box:    detail.Box,
					Detail: "",
				}, true
			} else {
				return nil, false
			}

		}

	}
	log.Warn().
		Msg("No item with given name found. Please check input")
	return nil, false
	//todo: switch to next page

}
