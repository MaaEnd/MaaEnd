package itemtransfer

import (
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

const (
	OCRFilter = "^(?![^a-zA-Z0-9]*(?:升序|降序|默认|品质|一键存放|材料|战术物品|消耗品|功能设备|普通设备|培养晶核)[^a-zA-Z0-9]*$)[^a-zA-Z0-9]+$"
)

type RepoLocate struct{}

func (*RepoLocate) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	tasker := ctx.GetTasker()
	ctrl := tasker.GetController()

	for grid_row := 0; grid_row < 1; grid_row++ {
		for grid_col := 0; grid_col < 8; grid_col++ {

			// Step 1 - hover to item
			hover_detail := ctx.RunActionDirect(
				maa.NodeActionTypeSwipe,
				maa.NodeSwipeParam{
					OnlyHover: true,
				},
				maa.Rect{RepoRoi(grid_row, 1).X(), RepoRoi(1, 1).Y(), 1, 1},
				nil,
			)
			if hover_detail.Success {
				log.Debug().
					Int("row", grid_row).
					Int("col", grid_col).
					Msg("Agent Hovered to Item")
			} else {
				log.Error().
					Int("row", grid_row).
					Int("col", grid_col).
					Msg("Agent Failed to Hover to Item")
				return nil, false
			}

			// Step 2 - Make screenshot
			ctrl.PostScreencap().Wait()

			// Step 3 - Recognize item name
			tasker.PostRecognition(
				maa.NodeRecognitionTypeOCR,
				maa.NodeOCRParam{
					ROI: maa.NewTargetRect(
						RepoRoi(1, 1),
					),
					OrderBy:  "Expected",
					Expected: []string{OCRFilter},
					OnlyRec:  true,
				},
				ctrl.CacheImage(),
			)
		}

	}

}

func RepoRoi(gridx, gridy int) maa.Rect {
	x := FirstX + gridx*(SquareSize+GridInterval) + ToolTipCursorOffset
	y := FirstY + gridy*(SquareSize+GridInterval) + ToolTipCursorOffset
	w := TooltipRoiScaleX
	h := TooltipRoiScaleY
	log.Trace().
		Int("gridx", gridx).Int("gridy", gridy).
		Int("x", x).Int("y", y).Int("w", w).Int("h", h).
		Msg("Agent Requested a REPO ROI")
	return maa.Rect{x, y, w, h}
}
