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

type JobWithGridInfo struct {
	*maa.TaskJob
	gridRowY int
	gridColX int
}

func (*RepoLocate) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	tasker := ctx.GetTasker()
	ctrl := tasker.GetController()
	recognitionTasks := make([]*JobWithGridInfo, 0, 32)
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

			log.Debug().Msg("Starting Screencap")
			// Step 2 - Make screenshot
			ctrl.PostScreencap().Wait()
			log.Debug().Msg("Done Screencap")

			// Step 3 - Before continuing: Does any early recognition found what we need?
			result, done := checkFoundItem(recognitionTasks)
			if done {
				// todo rewrite pipeline, as input to result
				return &maa.CustomRecognitionResult{
					Box:    result,
					Detail: "",
				}, true
			}

			// Step 4 - Recognize current item's name, add to tasker
			recognitionTasks = append(recognitionTasks, NewJobWithGridInfo(tasker, row, col, itemName))
			log.Trace().
				Int("row", row).
				Int("col", col).
				Msg("Task added to tasklist")
		}

	}
	log.Warn().
		Msg("No item with given name found. Please check input")
	return nil, false
	//todo: switch to next page
}

func checkFoundItem(recognitionTasks []*JobWithGridInfo) (maa.Rect, bool) {
	for _, task := range recognitionTasks {
		if task != nil {
			if task.Done() {
				log.Trace().
					Int64("task_id", task.GetDetail().ID).
					Msg("A recognition job is done, does it fail? let me see")
				if task.Success() {
					log.Debug().
						Int64("task_id", task.GetDetail().ID).
						Msg("Recognition job succeeded. Checking item")
					if task.GetDetail().NodeDetails[0].Recognition.Hit {
						log.Info().
							Int("grid_row_y", task.gridRowY).
							Int("grid_col_x", task.gridColX).
							Msg("Hooray! We have found the right Item")
						return RepoRoi(task.gridRowY, task.gridColX), true
					}
					// Not this one, continue
					log.Info().
						Int("grid_row_y", task.gridRowY).
						Int("grid_col_x", task.gridColX).
						Msg("Hmm... Seems we have not reached the item")
				} else {
					log.Error().
						Int("grid_row_y", task.gridRowY).
						Int("grid_col_x", task.gridColX).
						Msg("Task Job reported an error.")
					//roadmap: retry?
				}
			} else {
				log.Trace().
					Int("grid_row_y", task.gridRowY).
					Int("grid_col_x", task.gridColX).
					Msg("Task is not done yet")

			}
		} else {
			log.Error().
				Msg("Task is nil, but how??")
		}

	}
	return maa.Rect{}, false
}

func NewJobWithGridInfo(tasker *maa.Tasker, gridRowY, gridRowX int, keyword string) *JobWithGridInfo {
	log.Debug().
		Int("grid_row_y", gridRowY).
		Int("grid_col_x", gridRowX).
		Msg("Start recognizing item")
	task := tasker.PostRecognition(
		maa.NodeRecognitionTypeOCR,
		maa.NodeOCRParam{
			ROI: maa.NewTargetRect(
				RepoRoi(gridRowY, gridRowX),
			),
			OrderBy:  "Expected",
			Expected: []string{keyword},
			OnlyRec:  true,
		},
		tasker.GetController().CacheImage(),
	)
	log.Trace().
		Int("grid_row_y", gridRowY).
		Int("grid_col_x", gridRowX).
		Msg("Task created")
	return &JobWithGridInfo{task, gridRowY, gridRowX}
}
