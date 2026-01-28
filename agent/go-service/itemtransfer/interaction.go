package itemtransfer

import (
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v3"
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

type LeftClickWithCtrlDown struct{}

func (*LeftClickWithCtrlDown) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Debug().Msg("Pressing Ctrl")
	success := ctx.RunActionDirect(
		maa.NodeActionTypeKeyDown,
		maa.NodeKeyDownParam{
			Key: 17, // Ctrl
		},
		maa.Rect{},
		nil,
	).Success
	if !success {
		log.Error().
			Msg("Failed pressing ctrl")
		return false
	}
	log.Debug().
		Int("x", arg.RecognitionDetail.Box.X()).
		Int("y", arg.RecognitionDetail.Box.Y()).
		Int("W", arg.RecognitionDetail.Box.Width()).
		Int("H", arg.RecognitionDetail.Box.Height()).
		Msg("Pressed Ctrl. Clicking left mouse.")

	success = ctx.RunActionDirect(
		maa.NodeActionTypeClick,
		maa.NodeClickParam{
			Target: maa.NewTargetRect(arg.RecognitionDetail.Box),
		},
		arg.RecognitionDetail.Box,
		arg.RecognitionDetail,
	).Success
	if !success {
		log.Error().
			Msg("Failed clicking")
		return false
	}
	log.Debug().Msg("Clicked left mouse.")

	success = ctx.RunActionDirect(
		maa.NodeActionTypeKeyUp,
		maa.NodeKeyUpParam{
			Key: 17, // Ctrl
		},
		maa.Rect{},
		nil,
	).Success
	if !success {
		log.Error().
			Msg("Failed releasing ctrl")
		return false
	}
	log.Debug().Msg("Released Ctrl.")
	return true
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

func RepoSquarePos(gridRowY, gridColX int) maa.Rect {
	x := FirstX + gridColX*(SquareSize+GridInterval)
	y := FirstY + gridRowY*(SquareSize+GridInterval)
	w := SquareSize
	h := SquareSize
	log.Trace().
		Int("grid_row_y", gridRowY).
		Int("grid_col_x", gridColX).
		Int("x", x).Int("y", y).Int("w", w).Int("h", h).
		Msg("Agent Requested a REPO SQUARE POS")
	return maa.Rect{x, y, w, h}
}

func MoveAndShot(ctx *maa.Context, gridRowY, gridColX int) (img image.Image) {
	// Step 1 - Hover to item
	if !HoverOnto(ctx, gridRowY, gridColX) {
		log.Error().
			Int("grid_row_y", gridRowY).
			Int("grid_col_x", gridColX).
			Msg("Failed to hover onto item")
		return nil
	}

	// Step 2 - Make screenshot
	log.Debug().
		Int("grid_row_y", gridRowY).
		Int("grid_col_x", gridColX).
		Msg("Start Capture")
	controller := ctx.GetTasker().GetController()
	controller.PostScreencap().Wait()
	log.Debug().
		Int("grid_row_y", gridRowY).
		Int("grid_col_x", gridColX).
		Msg("Done Capture")
	return controller.CacheImage()
}
