package puzzle

import (
	"encoding/json"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

type Action struct{}

// doPlace performs the interaction to place a single puzzle piece
func doPlace(ctx *maa.Context, bd *BoardDesc, p Placement, isDryRun bool) {
	log.Debug().
		Int("PuzzleIndex", p.PuzzleIndex).
		Int("MachineX", p.MachineX).
		Int("MachineY", p.MachineY).
		Int("Rotation", p.Rotation).
		Msg("Placing puzzle piece")

	// 1. Recalculate thumbnail location
	// We assume thumbnails are analyzed in standard grid order (row by row, col by col)
	row := p.PuzzleIndex / int(PUZZLE_THUMBNAIL_MAX_COLS)
	col := p.PuzzleIndex % int(PUZZLE_THUMBNAIL_MAX_COLS)
	thumbX := PUZZLE_THUMBNAIL_START_X + float64(col)*PUZZLE_THUMBNAIL_W
	thumbY := PUZZLE_THUMBNAIL_START_Y + float64(row)*PUZZLE_THUMBNAIL_H

	startX := int(thumbX + PUZZLE_THUMBNAIL_W/2)
	startY := int(thumbY + PUZZLE_THUMBNAIL_H/2)

	// 2. Calculate target location on board
	if bd.W <= 0 || bd.H <= 0 {
		log.Error().Msg("Invalid BoardDesc: missing W/H dimensions")
		return
	}
	maxW, maxH := bd.W, bd.H

	// targetX = CENTER_BLOCK_LT_X + (MachineX - (maxW-1)/2) * BLOCK_W + BLOCK_W/2
	ltX, ltY := convertBoardCoordToLTCoord(p.MachineX, p.MachineY, maxW, maxH)
	targetX := float64(ltX) + BOARD_BLOCK_W/2
	targetY := float64(ltY) + BOARD_BLOCK_H/2

	endX := int(targetX)
	endY := int(targetY)

	// 3. Execution sequence
	aw := NewActionWrapper(ctx.GetTasker().GetController())
	aw.TouchUpSync(100)
	aw.TouchDownSync(0, startX, startY, 100)
	aw.TouchMoveSync(0, endX, endY, 250)

	// 4. Rotation
	// Mapping: 0->0, 1->3, 2->2, 3->1
	rotTimes := (4 - p.Rotation) % 4
	for range rotTimes {
		aw.TypeKeySync(82, 250) // R key
	}

	// 5. Complete
	if isDryRun {
		// In dry run mode, just return the piece to the thumbnail area
		time.Sleep(1000 * time.Millisecond)
		aw.TouchMoveSync(0, startX, startY, 250)
	}

	aw.TouchUpSync(0)
}

func doResetCursor(ctx *maa.Context) {
	aw := NewActionWrapper(ctx.GetTasker().GetController())
	aw.TouchUpSync(100)
	aw.TouchDownSync(0, 640, 620, 100)
	aw.TouchUpSync(0)
}

// Run executes the puzzle solving action.
func (a *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().
		Str("action", arg.CustomActionName).
		Msg("Starting PuzzleSolver action")

	// Parse custom action parameters
	isDryRun := false
	if arg.CustomActionParam != "" {
		var params struct {
			DryRun bool `json:"dryRun"`
		}
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err == nil {
			isDryRun = params.DryRun
		}
	}

	if isDryRun {
		log.Info().Msg("Dry run mode enabled: actions will be logged but not executed")
	}

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
		doPlace(ctx, &boardDesc, p, isDryRun)
		time.Sleep(250 * time.Millisecond)
	}
	doResetCursor(ctx)
	log.Info().Msg("Finished PuzzleSolver action")

	return true
}
