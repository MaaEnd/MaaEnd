// Copyright (c) 2026 Harry Huang
package puzzle

import (
	"encoding/json"
	"image"
	"math"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

type ProjDesc struct {
	XProjList []int
	YProjList []int
}

type BannedBlockDesc struct {
	Loc    [2]int
	RawLoc [2]int
}

type LockedBlockDesc struct {
	Loc    [2]int
	RawLoc [2]int
	Hue    int
}

type PuzzleDesc struct {
	Blocks [][2]int
	Hue    int
}

type BoardDesc struct {
	W               int
	H               int
	ProjDescList    []ProjDesc
	BannedBlockList []*BannedBlockDesc
	LockedBlockList [][]*LockedBlockDesc
	PuzzleList      []*PuzzleDesc
	HueList         []int
}

type Recognition struct{}

// Known color hues: 77 (green), 206(blue), 169(cyan), 33(orange)

func getPossibleHues(puzzles []*PuzzleDesc) []int {
	hues := make([]int, 0, len(puzzles))
	for _, p := range puzzles {
		hues = append(hues, p.Hue)
	}
	clusters := clusterHues(hues, PUZZLE_CLUSTER_DIFF_GRT)

	results := make([]int, 0, len(clusters))
	for _, members := range clusters {
		if len(members) == 0 {
			continue
		}
		results = append(results, meanHue(members))
	}
	return results
}

func getPossibleBoardSize(ctx *maa.Context, img image.Image) [2]int {
	maxExtent := BOARD_MAX_EXTENT_ONE_SIDE
	biasFactor := 0.075
	cropFactor := 0.8 // important
	bestW, bestH := 0, 0

	// Convert to SVGB format
	imgSvgb := getSVGBImage(img)

	// 1. Determine H (using XProj figures at the top)
	xMatches := matchTemplateAll(ctx, imgSvgb, "PuzzleSolver/ProjX_SVGB.png", []int{0, 0, WORK_W, int(float64(WORK_H) * 0.5)}, 16)
	if len(xMatches) > 0 {
		hScores := make(map[int]float64)
		for h := 2; h <= 2*maxExtent+1; h++ {
			distY := float64(h-1) / 2.0
			expectedY := BOARD_CENTER_BLOCK_LT_Y - distY*BOARD_BLOCK_H - biasFactor*BOARD_BLOCK_H
			score := 0.0
			for _, m := range xMatches {
				delta := expectedY - float64(m.CenterY)
				if 0 < delta && delta < BOARD_BLOCK_H*cropFactor {
					score += m.Score * m.Score
				}
			}
			hScores[h] = score
		}
		maxS := -1.0
		for h, s := range hScores {
			if s > maxS {
				maxS = s
				bestH = h
			} else if s == maxS && h > bestH {
				bestH = h
			}
		}
		log.Debug().Int("bestH", bestH).Interface("scores", hScores).Msg("Estimated board height")
	} else {
		log.Warn().Msg("No X projection figures detected")
	}

	// 2. Determine W (using YProj figures at the left)
	yMatches := matchTemplateAll(ctx, imgSvgb, "PuzzleSolver/ProjY_SVGB.png", []int{0, 0, int(float64(WORK_W) * 0.5), WORK_H}, 16)
	if len(yMatches) > 0 {
		wScores := make(map[int]float64)
		for w := 2; w <= 2*maxExtent+1; w++ {
			distX := float64(w-1) / 2.0
			expectedX := BOARD_CENTER_BLOCK_LT_X - distX*BOARD_BLOCK_W - biasFactor*BOARD_BLOCK_W
			score := 0.0
			for _, m := range yMatches {
				delta := expectedX - float64(m.CenterX)
				if 0 < delta && delta < BOARD_BLOCK_W*cropFactor {
					score += m.Score * m.Score
				}
			}
			wScores[w] = score
		}
		maxS := -1.0
		for w, s := range wScores {
			if s > maxS {
				maxS = s
				bestW = w
			} else if s == maxS && w > bestW {
				bestW = w
			}
		}
		log.Debug().Int("bestW", bestW).Interface("scores", wScores).Msg("Estimated board width")
	} else {
		log.Warn().Msg("No Y projection figures detected")
	}

	return [2]int{bestW, bestH}
}

func convertBlockLtToBannedBlockDesc(boardW, boardH int, blocks [][2]int) []*BannedBlockDesc {
	gridBlocks := make([]*BannedBlockDesc, 0, len(blocks))

	for _, b := range blocks {
		// Calculate grid coordinate
		idx, idy := convertLTCoordToBoardCoord(b[0], b[1], boardW, boardH)

		// Validate coordinates bounds [0, W-1][0, H-1]
		if idx >= 0 && idx < boardW && idy >= 0 && idy < boardH {
			gridBlocks = append(gridBlocks, &BannedBlockDesc{
				Loc:    [2]int{idx, idy},
				RawLoc: b,
			})
		}
	}
	return gridBlocks
}

func getProjDesc(ctx *maa.Context, img image.Image, boardSize [2]int, targetHue int) *ProjDesc {
	// First, determine the board dimensions using template matching analysis
	W, H := boardSize[0], boardSize[1]

	// Now scan the projection numbers based on the determined dimensions

	// X Projection (Top Row)
	// Determine the Y-coordinate of the X Projection figures relative to the board
	// distY is the distance from the visual center to the top edge of the board in blocks
	distY := float64(H-1) / 2.0
	projFigY := BOARD_CENTER_BLOCK_LT_Y - distY*BOARD_BLOCK_H - BOARD_X_PROJ_FIGURE_H

	finalXProjList := make([]int, W)
	for gridX := range W {
		// Calculate precise X-coordinate for each column's projection figure
		// gridIdxRel is the column index relative to the visual center (0)
		gridIdxRel := float64(gridX) - float64(W-1)/2.0
		projFigX := BOARD_CENTER_BLOCK_LT_X + gridIdxRel*BOARD_BLOCK_W

		finalXProjList[gridX] = getProjFigureNumber(ctx, img, int(projFigX), int(projFigY), "X", targetHue)
	}

	// Y Projection (Left Column)
	// Determine the X-coordinate of the Y Projection figures relative to the board
	distX := float64(W-1) / 2.0
	projFigX := BOARD_CENTER_BLOCK_LT_X - distX*BOARD_BLOCK_W - BOARD_Y_PROJ_FIGURE_W

	finalYProjList := make([]int, H)
	for gridY := range H {
		// Calculate precise Y-coordinate for each row's projection figure
		gridIdxRel := float64(gridY) - float64(H-1)/2.0
		projFigY := BOARD_CENTER_BLOCK_LT_Y + gridIdxRel*BOARD_BLOCK_H

		finalYProjList[gridY] = getProjFigureNumber(ctx, img, int(projFigX), int(projFigY), "Y", targetHue)
	}

	return &ProjDesc{
		XProjList: finalXProjList,
		YProjList: finalYProjList,
	}
}

func getProjFigureNumber(ctx *maa.Context, img image.Image, ltX, ltY int, axis string, targetHue int) int {
	samplingPoints := []float64{0.333, 0.5, 0.667}
	maxOffset := 0

	var w, h int
	if axis == "X" {
		w = int(BOARD_BLOCK_W)
		h = int(BOARD_X_PROJ_FIGURE_H)
	} else {
		w = int(BOARD_Y_PROJ_FIGURE_W)
		h = int(BOARD_BLOCK_H)
	}

	if axis == "X" {
		// X-Axis Projection Figure (Top of Board). Inner Edge: Bottom. Scan Outwards: Up.
		bottomY := ltY + h
		for i := range h {
			y := bottomY - 1 - i // Move up
			valid := false

			for _, p := range samplingPoints {
				x := ltX + int(float64(w)*p)
				_, s, v := getPixelHSV(img, x, y, targetHue, PUZZLE_CLUSTER_DIFF_GRT)
				if s > BOARD_PROJ_COLOR_SAT_GRT && v > BOARD_PROJ_COLOR_VAL_GRT {
					valid = true
					break
				}
			}
			if valid {
				maxOffset = i + 1
			}
		}
	} else {
		// Y-Axis Projection Figure (Left of Board). Inner Edge: Right. Scan Outwards: Left.
		rightX := ltX + w
		for i := range w {
			x := rightX - 1 - i // Move left
			valid := false

			for _, p := range samplingPoints {
				y := ltY + int(float64(h)*p)
				_, s, v := getPixelHSV(img, x, y, targetHue, PUZZLE_CLUSTER_DIFF_GRT)
				if s > BOARD_PROJ_COLOR_SAT_GRT && v > BOARD_PROJ_COLOR_VAL_GRT {
					valid = true
					break
				}
			}
			if valid {
				maxOffset = i + 1
			}
		}
	}

	val := (float64(maxOffset) - float64(BOARD_PROJ_INIT_GAP)) / float64(BOARD_PROJ_EACH_GAP)
	result := int(math.Round(val))
	if result < 0 {
		return 0
	}
	return result
}

func getAllPuzzleDesc(ctx *maa.Context, img image.Image) []*PuzzleDesc {
	thumbs := getAllPuzzleThumbLoc(img)
	log.Info().Interface("thumbs", thumbs).Msg("Puzzle thumbnail positions")

	var puzzleList []*PuzzleDesc
	for _, thumb := range thumbs {
		desc := doPreviewPuzzle(ctx, thumb[0], thumb[1])
		if desc != nil {
			puzzleList = append(puzzleList, desc)
			log.Info().Interface("puzzle", desc).Msg("Puzzle structure")
		}
	}
	return puzzleList
}

func doEnsureTab(ctx *maa.Context, img image.Image) image.Image {
	rect1 := image.Rect(int(TAB_1_X), int(TAB_Y), int(TAB_1_X+TAB_W), int(TAB_Y+TAB_H))
	rect2 := image.Rect(int(TAB_2_X), int(TAB_Y), int(TAB_2_X+TAB_W), int(TAB_Y+TAB_H))

	_, _, val1 := getAreaMeanHSV(img, rect1)
	_, _, val2 := getAreaMeanHSV(img, rect2)
	log.Debug().Float64("val1", val1).Float64("val2", val2).Msg("Checking tab selection state")

	var ctrl = ctx.GetTasker().GetController()

	// If tab 1 brightness is not greater than tab 2, it's likely on tab 2
	if val1 <= val2 {
		log.Info().Msg("Tab 2 detected as active, switching back to Tab 1")
		ctrl.PostClickKey(9) // Tab
		time.Sleep(500 * time.Millisecond)
	}

	// Then refresh screenshot
	ctrl.PostScreencap().Wait()
	newImg := ctrl.CacheImage()
	if newImg == nil {
		log.Error().Msg("Failed to capture image")
		return nil
	}
	return newImg
}

func getPuzzleDesc(img image.Image) *PuzzleDesc {
	blocks := [][2]int{}
	var totalHue float64
	count := 0
	// Center block is at (0, 0) relative to core
	// Coordinates of the center block in the preview image
	// The drag target (PUZZLE_PREVIEW_MV_X, PUZZLE_PREVIEW_MV_Y) corresponds to the CENTER of the core block.
	coreX := PUZZLE_PREVIEW_MV_X
	coreY := PUZZLE_PREVIEW_MV_Y

	for offsetY := -PUZZLE_MAX_EXTENT_ONE_SIDE; offsetY <= PUZZLE_MAX_EXTENT_ONE_SIDE; offsetY++ {
		for offsetX := -PUZZLE_MAX_EXTENT_ONE_SIDE; offsetX <= PUZZLE_MAX_EXTENT_ONE_SIDE; offsetX++ {
			// Calculate block center
			blockCenterX := coreX + float64(offsetX)*PUZZLE_W
			blockCenterY := coreY + float64(offsetY)*PUZZLE_H

			// Calculate block rect (top-left to bottom-right)
			x1 := int(blockCenterX - PUZZLE_W/2)
			y1 := int(blockCenterY - PUZZLE_H/2)
			x2 := x1 + int(PUZZLE_W)
			y2 := y1 + int(PUZZLE_H)

			rect := image.Rect(x1, y1, x2, y2)

			variance := getAreaVariance(img, rect)
			_, sat, val := getAreaMeanHSV(img, rect)
			isBlock := variance > PUZZLE_COLOR_VAR_GRT && sat > PUZZLE_COLOR_SAT_GRT && val > PUZZLE_COLOR_VAL_GRT

			if isBlock {
				blocks = append(blocks, [2]int{offsetX, offsetY})
				hue, _, _ := getAreaMidHSV(img, rect) // Use mid to avoid noises
				totalHue += hue
				count++
			}
		}
	}
	if count == 0 {
		return nil
	}
	return &PuzzleDesc{
		Blocks: blocks,
		Hue:    int(totalHue / float64(count)),
	}
}

func getAllPuzzleThumbLoc(img image.Image) [][2]int {
	results := [][2]int{}
	hasGap := false

	for r := 0; r < PUZZLE_THUMBNAIL_MAX_ROWS; r++ {
		for c := 0; c < PUZZLE_THUMBNAIL_MAX_COLS; c++ {
			x := int(PUZZLE_THUMBNAIL_START_X + float64(c)*PUZZLE_THUMBNAIL_W)
			y := int(PUZZLE_THUMBNAIL_START_Y + float64(r)*PUZZLE_THUMBNAIL_H)
			rect := image.Rect(x, y, x+int(PUZZLE_THUMBNAIL_W), y+int(PUZZLE_THUMBNAIL_H))

			variance := getAreaVariance(img, rect)
			// log.Debug().Int("r", r).Int("c", c).Float64("var", variance).Msg("Puzzle thumbnail area color variance")

			if variance > PUZZLE_THUMBNAIL_COLOR_VAR_GRT {
				// Color variation is sufficient, likely a puzzle thumbnail
				if variance > PUZZLE_THUMBNAIL_COLOR_VAR_LES {
					// False-positive
					log.Warn().Msg("Detected uncertain puzzle thumbnail area, skipping")
					return [][2]int{}
				}
				if hasGap {
					// False-positive
					log.Warn().Msg("Detected non-contiguous puzzle thumbnails (gap found), skipping")
					return [][2]int{}
				}
				results = append(results, [2]int{x, y})
			} else {
				// Color variation too low, the area is likely a solid background.
				hasGap = true
			}
		}
	}

	if len(results) >= PUZZLE_THUMBNAIL_MAX_ROWS*PUZZLE_THUMBNAIL_MAX_COLS {
		// False-positive
		log.Warn().Int("count", len(results)).Msg("Detected too many puzzle thumbnails, skipping")
		return [][2]int{}
	}

	return results
}

func doPreviewPuzzle(ctx *maa.Context, thumbX, thumbY int) *PuzzleDesc {
	ctrl := ctx.GetTasker().GetController()
	log.Debug().Int("thumbX", thumbX).Int(" thumbY", thumbY).Msg("Previewing puzzle thumbnail")

	// 1. Drag thumbnail to preview area
	// Start point is center of the thumbnail
	startX := int(float64(thumbX + int(PUZZLE_THUMBNAIL_W)/2))
	startY := int(float64(thumbY + int(PUZZLE_THUMBNAIL_H)/2))

	// End point is preview area center
	endX := int(PUZZLE_PREVIEW_MV_X)
	endY := int(PUZZLE_PREVIEW_MV_Y)

	aw := NewActionWrapper(ctrl)
	aw.TouchUpSync(100)
	aw.TouchDownSync(0, startX, startY, 100)
	aw.TouchMoveSync(0, endX, endY, 500)

	// 2. Screenshot
	ctrl.PostScreencap().Wait()
	previewImg := ctrl.CacheImage()
	if previewImg == nil {
		log.Error().Msg("Failed to capture preview image")
		aw.TouchUpSync(0)
		return nil
	}

	// 3. Touch Up (Release)
	aw.TouchUpSync(0)

	// 4. Analyze
	return getPuzzleDesc(previewImg)
}

func getLockedBlocksDesc(img image.Image, boardW, boardH int) []*LockedBlockDesc {
	locked := []*LockedBlockDesc{}

	for gridY := range boardH {
		for gridX := range boardW {
			// Get LT coordinate from Grid Index (gridX, gridY)
			ltX, ltY := convertBoardCoordToLTCoord(gridX, gridY, boardW, boardH)
			rect := image.Rect(ltX, ltY, ltX+int(BOARD_BLOCK_W), ltY+int(BOARD_BLOCK_H))

			_, sat, val := getAreaMeanHSV(img, rect)
			isLocked := sat > BOARD_LOCKED_COLOR_SAT_GRT && val > BOARD_LOCKED_COLOR_VAL_GRT

			if isLocked {
				hue, _, _ := getAreaMidHSV(img, rect)
				locked = append(locked, &LockedBlockDesc{
					Loc:    [2]int{gridX, gridY},
					RawLoc: [2]int{ltX, ltY},
					Hue:    int(hue),
				})
			}
		}
	}
	return locked
}

func getBannedBlocksLTCoord(ctx *maa.Context, img image.Image) [][2]int {
	result := matchTemplateAll(ctx, img, "PuzzleSolver/BlockBanned.png", []int{
		int(0.2 * WORK_W),
		int(0.2 * WORK_H),
		int(0.6 * WORK_W),
		int(0.6 * WORK_H),
	}, 64)
	blocks := make([][2]int, 0, len(result))
	for _, m := range result {
		blocks = append(blocks, [2]int{m.X, m.Y})
	}
	return blocks
}

func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	log.Info().
		Str("recognition", arg.CustomRecognitionName).
		Msg("Starting PuzzleSolver recognition")

	img := arg.Img // 1280x720 for MaaEnd
	if img == nil {
		log.Error().Msg("Prepared image is nil")
		return nil, false
	}

	// 1. Find all puzzles to be placed
	puzzleList := getAllPuzzleDesc(ctx, img)

	if len(puzzleList) == 0 {
		log.Info().Msg("No puzzles detected or invalid puzzles")
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{}`,
		}, false
	}

	// 2. Ensure tab state and determine board size (moved from step 4)
	img = doEnsureTab(ctx, img)

	boardSize := getPossibleBoardSize(ctx, img)
	if boardSize[0] == 0 || boardSize[1] == 0 {
		log.Error().Msg("Failed to determine board size")
		return nil, false
	}
	log.Info().Int("boardW", boardSize[0]).Int("boardH", boardSize[1]).Msg("Determined possible board size")

	// 3. Find banned and locked blocks
	banned := getBannedBlocksLTCoord(ctx, img)
	log.Info().Interface("banned", banned).Msg("Puzzle board banned blocks")

	locked := getLockedBlocksDesc(img, boardSize[0], boardSize[1])
	log.Info().Interface("locked", locked).Msg("Puzzle board locked blocks")

	// 4. Find possible hues from puzzles
	hueList := getPossibleHues(puzzleList)
	var projDescList []ProjDesc
	var lockedBlockList [][]*LockedBlockDesc

	// 5. For each hue, determine board projection and locked blocks
	for _, hue := range hueList {
		projDesc := getProjDesc(ctx, img, boardSize, hue)
		log.Debug().Int("hue", hue).Interface("projDesc", projDesc).Msg("Puzzle board projection description for hue")

		// Validate projection list dimensions match board size
		if len(projDesc.XProjList) != boardSize[0] || len(projDesc.YProjList) != boardSize[1] {
			log.Error().
				Int("hue", hue).
				Int("XProjLen", len(projDesc.XProjList)).Int("YProjLen", len(projDesc.YProjList)).
				Int("boardW", boardSize[0]).Int("boardH", boardSize[1]).
				Msg("Projection list length mismatch with board dimensions")
			return nil, false
		}

		// Get locked blocks for this hue
		thisLocked := []*LockedBlockDesc{}
		for i, lb := range locked {
			if lb != nil && diffHue(lb.Hue, hue) <= PUZZLE_CLUSTER_DIFF_GRT {
				thisLocked = append(thisLocked, lb)
				locked[i] = nil
			}
		}
		log.Debug().Int("hue", hue).Interface("locked", thisLocked).Msg("Puzzle board locked blocks for hue")

		projDescList = append(projDescList, *projDesc)
		lockedBlockList = append(lockedBlockList, thisLocked)
	}

	// 6. Construct board description
	boardDesc := &BoardDesc{
		W:               boardSize[0],
		H:               boardSize[1],
		ProjDescList:    projDescList,
		BannedBlockList: convertBlockLtToBannedBlockDesc(boardSize[0], boardSize[1], banned),
		LockedBlockList: lockedBlockList,
		PuzzleList:      puzzleList,
		HueList:         hueList,
	}
	log.Info().Interface("boardDesc", boardDesc).Msg("Puzzle board description")

	// 7. Convert to JSON and return
	detailJSON, err := json.Marshal(boardDesc)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal boardDesc")
		detailJSON = []byte(`{}`)
	}

	log.Info().Msg("Finished PuzzleSolver recognition")
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}
