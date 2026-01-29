package itemtransfer

import (
	"encoding/json"
	"fmt"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

// const (
// 	OCRFilter = "^(?![^a-zA-Z0-9]*(?:升序|降序|默认|品质|一键存放|材料|战术物品|消耗品|功能设备|普通设备|培养晶核)[^a-zA-Z0-9]*$)[^a-zA-Z0-9]+$"
// )

type RepoLocate struct{}

func (*RepoLocate) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
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

	for row := range 4 {
		for col := range 8 {

			// Step 1 & 2
			img := MoveAndShot(ctx, row, col)

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
				img,
			)
			log.Debug().Msg("Done Recognition")
			if detail.Hit {
				log.Info().
					Int("grid_row_y", row).
					Int("grid_col_x", col).
					Msg("Yes That's it! We have found proper item.")

				// saving cache todo move standalone
				template := "{\"ItemTransferToBackpack\": {\"recognition\": {\"param\": {\"custom_recognition_param\": {\"ItemLastFoundRowAbs\": %d,\"ItemLastFoundColumnX\": %d}}}}}"
				defer ctx.OverridePipeline(fmt.Sprintf(template, row, col))

				return &maa.CustomRecognitionResult{
					Box:    RepoSquarePos(row, col),
					Detail: detail.DetailJson,
				}, true
			} else {
				log.Info().
					Int("grid_row_y", row).
					Int("grid_col_x", col).
					Msg("Not this one. Bypass.")
			}

		}

	}
	log.Warn().
		Msg("No item with given name found. Please check input")
	return nil, false
	//todo: switch to next page

}
