package itemtransfer

import (
	maa "github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

func HoverOnto(ctx *maa.Context, gridRowY, gridColX int) (success bool) {
	log.Debug().
		Int("grid_row_y", gridRowY).
		Int("grid_col_x", gridColX).
		Msg("Agent Start Hovering onto Item")
	success = ctx.RunActionDirect(
		maa.NodeActionTypeSwipe,
		maa.NodeSwipeParam{
			OnlyHover: true,
		},
		maa.Rect{RepoRoi(gridRowY, gridColX).X(), RepoRoi(gridRowY, gridColX).Y(), 1, 1},
		nil,
	).Success
	if success {
		log.Debug().
			Int("grid_row_y", gridRowY).
			Int("grid_col_x", gridColX).
			Msg("Agent Successfully Hovered to Item")
	} else {
		log.Error().
			Int("grid_row_y", gridRowY).
			Int("grid_col_x", gridColX).
			Msg("Agent Failed Hovering to Item")
	}
	return success
}

func RepoRoi(gridRowY, gridColX int) maa.Rect {
	x := FirstX + gridColX*(SquareSize+GridInterval) + ToolTipCursorOffset
	y := FirstY + gridRowY*(SquareSize+GridInterval) + ToolTipCursorOffset
	w := TooltipRoiScaleX
	h := TooltipRoiScaleY
	log.Trace().
		Int("grid_row_y", gridRowY).
		Int("grid_col_x", gridColX).
		Int("x", x).Int("y", y).Int("w", w).Int("h", h).
		Msg("Agent Requested a REPO ROI")
	return maa.Rect{x, y, w, h}
}
