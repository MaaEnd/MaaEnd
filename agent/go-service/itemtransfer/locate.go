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
	var taskParam map[string]any

	err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &taskParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("raw_json", arg.CustomRecognitionParam).
			Msg("Seems that we have received bad params")
		return nil, false
	}

	itemName, ok := taskParam["ItemName"].(string)
	if !ok {
		log.Error().
			Str("raw_json", arg.CustomRecognitionParam).
			Msg("ItemName is not a string")
		return nil, false
	}
	firstRun, ok := taskParam["FirstRun"].(bool)
	if !ok {
		log.Error().
			Str("raw_json", arg.CustomRecognitionParam).
			Msg("FirstRun is not a bool")
		return nil, false
	}
	//containerContent := userSetting["ContainerContent"] //todo put this into use

	log.Debug().
		Str("ItemName", itemName).
		Bool("FirstRun", firstRun).
		Any("ContainerContent", taskParam["ContainerContent"]).
		Msg("Task parameters initialized")

	if firstRun {
		// Step 0 - Precondition
		if status := ctx.RunTask("ItemTransferAtOrigin").Status; !status.Success() {
			log.Error().
				Str("status", status.String()).
				Msg("Precondition failed running Item Locate task")
		}
	}

	for row := range 4 {
		for col := range 8 {

			// Step 1 & 2
			img := MoveAndShot(ctx, REPOSITORY, row, col)

			// Step 3 - Call original OCR
			log.Debug().Msg("Starting Recognition")
			detail := ctx.RunRecognitionDirect(
				maa.NodeRecognitionTypeOCR,
				maa.NodeOCRParam{
					ROI: maa.NewTargetRect(
						TooltipRoi(REPOSITORY, row, col),
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
				template := "{\"ItemTransferToBackpack\": {\"recognition\": {\"param\": {\"custom_recognition_param\": {\"ItemLastFoundRowAbs\": %d,\"ItemLastFoundColumnX\": %d,\"FirstRun\": false}}}}}"
				defer ctx.OverridePipeline(fmt.Sprintf(template, row, col))

				return &maa.CustomRecognitionResult{
					Box:    ItemBoxRoi(REPOSITORY, row, col),
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
