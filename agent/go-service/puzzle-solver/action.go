package puzzle

import (
	"encoding/json"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

type Action struct{}

// doPlace performs the interaction to place a single puzzle piece
func doPlace(ctx *maa.Context, bd *BoardDesc, p Placement) {
	log.Info().
		Int("puzzleIndex", p.PuzzleIndex).
		Int("mx", p.MachineX).
		Int("my", p.MachineY).
		Int("rot", p.Rotation).
		Msg("Placing puzzle piece")

	ctrl := ctx.GetTasker().GetController()

	// 1. Recalculate thumbnail location
	// We assume thumbnails are analyzed in standard grid order (row by row, col by col)
	row := p.PuzzleIndex / int(PUZZLE_THUMBNAIL_MAX_COLS)
	col := p.PuzzleIndex % int(PUZZLE_THUMBNAIL_MAX_COLS)
	thumbX := PUZZLE_THUMBNAIL_START_X + float64(col)*PUZZLE_THUMBNAIL_W
	thumbY := PUZZLE_THUMBNAIL_START_Y + float64(row)*PUZZLE_THUMBNAIL_H

	startX := int32(thumbX + PUZZLE_THUMBNAIL_W/2)
	startY := int32(thumbY + PUZZLE_THUMBNAIL_H/2)

	// 2. Calculate target location on board
	// Find refProj to determine center alignment
	var refProj ProjDesc
	for _, pd := range bd.ProjDescList {
		if pd.ExtX+pd.ExtY > refProj.ExtX+refProj.ExtY {
			refProj = pd
		}
	}

	// Target pixel coordinates are centered at the block
	// MachineX is in range [0, 2*maxExtX]. Grid center is maxExtX.
	// However, solver uses maxExt across all hues.
	maxExtX, maxExtY := 0, 0
	for _, pd := range bd.ProjDescList {
		if pd.ExtX > maxExtX {
			maxExtX = pd.ExtX
		}
		if pd.ExtY > maxExtY {
			maxExtY = pd.ExtY
		}
	}

	// targetX = CENTER_BLOCK_LT_X + (MachineX - maxExtX) * BLOCK_W + BLOCK_W/2
	targetX := BOARD_CENTER_BLOCK_LT_X + float64(p.MachineX-maxExtX)*BOARD_BLOCK_W + BOARD_BLOCK_W/2
	targetY := BOARD_CENTER_BLOCK_LT_Y + float64(p.MachineY-maxExtY)*BOARD_BLOCK_H + BOARD_BLOCK_H/2

	endX := int32(targetX)
	endY := int32(targetY)

	// 3. Execution sequence
	ctrl.PostTouchUp(0).Wait()
	time.Sleep(100 * time.Millisecond)

	ctrl.PostTouchMove(0, startX, startY, 1).Wait()
	time.Sleep(100 * time.Millisecond)

	ctrl.PostTouchDown(0, startX, startY, 1).Wait()
	time.Sleep(100 * time.Millisecond)

	ctrl.PostTouchMove(0, endX, endY, 1).Wait()
	time.Sleep(250 * time.Millisecond)

	// 4. Rotation
	// Mapping: 0->0, 1->3, 2->2, 3->1
	rotTimes := (4 - p.Rotation) % 4
	for range rotTimes {
		ctx.RunTask("PressR", `{"PressR": {"action": "ClickKey", "key": 82}}`)
	}

	// 5. Release
	ctrl.PostTouchUp(0).Wait()
	time.Sleep(100 * time.Millisecond)
}

// Run executes the puzzle solving action.
func (a *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Debug().
		Str("action", arg.CustomActionName).
		Msg("Running PuzzleSolver action")

	// Get the recognition result (boardDesc JSON)
	recData := arg.RecognitionDetail.DetailJson
	if recData == "" {
		log.Warn().Msg("No recognition detail received for puzzle solver")
		return false
	}

	var boardDesc BoardDesc
	if err := json.Unmarshal([]byte(recData), &boardDesc); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal board state")
		return false
	}

	// MaaFramework wrapping logic: if HueList is missing, check if it's wrapped in "best.detail"
	if len(boardDesc.HueList) == 0 {
		var wrapped struct {
			Best struct {
				Detail json.RawMessage `json:"detail"`
			} `json:"best"`
		}
		if err := json.Unmarshal([]byte(recData), &wrapped); err == nil && len(wrapped.Best.Detail) > 0 {
			if err := json.Unmarshal(wrapped.Best.Detail, &boardDesc); err != nil {
				log.Error().Err(err).Msg("Failed to unmarshal wrapped board state")
				return false
			}
		}
	}

	// Solve the puzzle
	placements, err := Solve(&boardDesc)
	if err != nil {
		log.Error().Err(err).Str("detail", recData).Msg("Failed to solve puzzle")
		return false
	}

	log.Info().Interface("placements", placements).Msg("Puzzle solved successfully")

	// Execute the solution steps (placements)
	for _, p := range placements {
		doPlace(ctx, &boardDesc, p)
	}

	return true
}
