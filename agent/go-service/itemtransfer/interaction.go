package itemtransfer

import (
	"errors"
	"image"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

const (
	RepoFirstX  = 161
	RepoFirstY  = 217
	RepoColumns = 8

	BackpackFirstX  = 771
	BackpackFirstY  = 217
	BackpackColumns = 5

	BackpackRows = 7 // subject to change in later versions of endfield? whatever~

	RowsPerPage  = 4
	BoxSize      = 64
	GridInterval = 5
)

const (
	ToolTipCursorOffset = 32
	TooltipRoiScaleX    = 275
	TooltipRoiScaleY    = 130
)

const (
	RepoTitleX = 185
	RepoTitleY = 130
	RepoTitleW = 145
	RepoTitleH = 40
)

const (
	ResetInvViewSwipeTimes     = 5
	ResetInvViewScrollDistance = 120 * 10
)

type Inventory int

const (
	REPOSITORY Inventory = iota
	BACKPACK
)

func (inv Inventory) String() string {
	switch inv {
	case REPOSITORY:
		return "Repository"
	case BACKPACK:
		return "Backpack"
	default:
		return "Unknown"
	}
}

func (inv Inventory) FirstX() int {
	switch inv {
	case REPOSITORY:
		return RepoFirstX
	case BACKPACK:
		return BackpackFirstX
	default:
		return 0
	}
}
func (inv Inventory) FirstY() int {
	switch inv {
	case REPOSITORY:
		return RepoFirstY
	case BACKPACK:
		return BackpackFirstY
	default:
		return 0
	}
}

func (inv Inventory) Columns() int {
	switch inv {
	case REPOSITORY:
		return RepoColumns
	case BACKPACK:
		return BackpackColumns
	default:
		return 0
	}
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

func TooltipRoi(inv Inventory, gridRowY, gridColX int) maa.Rect {
	x := inv.FirstX() + gridColX*(BoxSize+GridInterval) + ToolTipCursorOffset
	y := inv.FirstY() + gridRowY*(BoxSize+GridInterval) + ToolTipCursorOffset
	w := TooltipRoiScaleX
	h := TooltipRoiScaleY
	log.Trace().
		Str("inventory", inv.String()).
		Int("grid_row_y", gridRowY).
		Int("grid_col_x", gridColX).
		Int("x", x).Int("y", y).Int("w", w).Int("h", h).
		Msg("Agent Requested a TOOLTIP ROI")
	return maa.Rect{x, y, w, h}
}

func ItemBoxRoi(inv Inventory, gridRowY, gridColX int) maa.Rect {
	x := inv.FirstX() + gridColX*(BoxSize+GridInterval)
	y := inv.FirstY() + gridRowY*(BoxSize+GridInterval)
	w := BoxSize
	h := BoxSize

	log.Trace().
		Str("inventory", inv.String()).
		Int("grid_row_y", gridRowY).
		Int("grid_col_x", gridColX).
		Int("x", x).Int("y", y).Int("w", w).Int("h", h).
		Msg("Agent Requested a BOX ROI")
	return maa.Rect{x, y, w, h}
}

func HoverOnto(ctx *maa.Context, inv Inventory, gridRowY, gridColX int) (err error) {
	log.Debug().
		Str("inventory", inv.String()).
		Int("grid_row_y", gridRowY).
		Int("grid_col_x", gridColX).
		Msg("Agent Start Hovering onto Item")
	if !ctx.RunActionDirect(
		maa.NodeActionTypeSwipe,
		maa.NodeSwipeParam{
			OnlyHover: true,
		},
		Pointize(TooltipRoi(inv, gridRowY, gridColX)),
		nil,
	).Success {
		log.Error().
			Str("inventory", inv.String()).
			Int("grid_row_y", gridRowY).
			Int("grid_col_x", gridColX).
			Msg("Agent Failed Hovering to Item")
		return errors.New("Agent Failed Hovering to Item")
	}
	log.Debug().
		Str("inventory", inv.String()).
		Int("grid_row_y", gridRowY).
		Int("grid_col_x", gridColX).
		Msg("Agent Successfully Hovered to Item")
	return nil
}

func MoveAndShot(ctx *maa.Context, inv Inventory, gridRowY, gridColX int) (img image.Image) {
	// Step 1 - Hover to item
	if HoverOnto(ctx, inv, gridRowY, gridColX) != nil {
		log.Error().
			Str("inventory", inv.String()).
			Int("grid_row_y", gridRowY).
			Int("grid_col_x", gridColX).
			Msg("Failed to hover onto item")
		return nil
	}

	// Step 2 - Make screenshot
	log.Debug().
		Str("inventory", inv.String()).
		Int("grid_row_y", gridRowY).
		Int("grid_col_x", gridColX).
		Msg("Start Capture")
	controller := ctx.GetTasker().GetController()
	controller.PostScreencap().Wait()
	log.Debug().
		Str("inventory", inv.String()).
		Int("grid_row_y", gridRowY).
		Int("grid_col_x", gridColX).
		Msg("Done Capture")
	return controller.CacheImage()
}

func ResetInventoryView(ctx *maa.Context, inv Inventory, inverse bool) (err error) {
	log.Debug().
		Str("inventory", inv.String()).
		Bool("inverse", inverse).
		Msg("Agent Requested a Reset to Inventory View")
	if !ctx.RunActionDirect(
		maa.NodeActionTypeScroll,
		maa.NodeScrollParam{
			Dy: ResetInvViewScrollDistance,
		},
		Pointize(TooltipRoi(inv, 0, 0)),
		nil,
	).Success {
		log.Error().
			Str("inventory", inv.String()).
			Bool("inverse", inverse).
			Msg("Agent Failed Resetting Inventory View")
		return errors.New("Agent Failed Resetting Inventory View")
	}
	log.Debug().
		Str("inventory", inv.String()).
		Bool("inverse", inverse).
		Msg("Agent Successfully Reset Inventory View")

	// what the hell
	// this pyramid waits until screen became stable
	ctx.RunTask(
		"ItemTransferWaitFreeze101",
		maa.NewPipeline().
			AddNode(
				maa.NewNode(
					"ItemTransferWaitFreeze101",
					maa.WithPostWaitFreezes(
						maa.WaitFreezes(
							maa.WithWaitFreezesTarget(
								maa.NewTargetRect(
									maa.Rect{
										143,
										122,
										993,
										418,
									},
								),
							),
							maa.WithWaitFreezesTime(
								700*time.Millisecond,
							),
						),
					),
				),
			),
	)
	return nil
}

func ResetInventoryView2(ctx *maa.Context, inv Inventory, inverse bool) (err error) {
	params := maa.NodeSwipeParam{}
	UpCorner := Pointize(ItemBoxRoi(inv, 0, 0))
	UpCornerOffset := maa.Rect{-(GridInterval / 2), (GridInterval / 2)}
	DownCorner := Pointize(ItemBoxRoi(inv, RowsPerPage-1, 0))
	DownCornerOffset := maa.Rect{-(GridInterval / 2), -(GridInterval / 2)}
	log.Debug().
		Str("inventory", inv.String()).
		Bool("inverse", inverse).
		Int("up_x", UpCorner.X()+UpCornerOffset.X()).
		Int("up_y", UpCorner.Y()+UpCornerOffset.Y()).
		Int("down_x", DownCorner.X()+DownCornerOffset.X()).
		Int("down_y", DownCorner.Y()+DownCornerOffset.Y()).
		Msg("Agent Requested a Reset to Inventory View")
	if !inverse {
		params.Begin = maa.NewTargetRect(UpCorner)
		params.BeginOffset = UpCornerOffset
		params.End = []maa.Target{maa.NewTargetRect(DownCorner)}
		params.EndOffset = []maa.Rect{DownCornerOffset}
	} else {
		params.Begin = maa.NewTargetRect(DownCorner)
		params.BeginOffset = DownCornerOffset
		params.End = []maa.Target{maa.NewTargetRect(UpCorner)}
		params.EndOffset = []maa.Rect{UpCornerOffset}
	}
	for range ResetInvViewSwipeTimes {
		if !ctx.RunActionDirect(
			maa.NodeActionTypeSwipe,
			params,
			maa.Rect{},
			nil,
		).Success {
			log.Error().
				Str("inventory", inv.String()).
				Msg("Error occurred while swiping, in ResetInventory")
			return errors.New("Error occurred while swiping, in ResetInventory")
		}
		log.Trace().
			Str("inventory", inv.String()).
			Msg("ResetInventory Swiped Once")
	}
	log.Debug().
		Str("inventory", inv.String()).
		Msg("Done Reset Inventory View")
	return nil
}
