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
	ExtX      int
	ExtY      int
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

func convertBlockLtToBoardCoord(proj *ProjDesc, blocks [][2]int) []*BannedBlockDesc {
	gridBlocks := make([]*BannedBlockDesc, 0, len(blocks))

	// Center block (0,0) LT coordinate
	midX := BOARD_CENTER_BLOCK_LT_X
	midY := BOARD_CENTER_BLOCK_LT_Y

	for _, b := range blocks {
		// LT coordinate of each block
		bx, by := float64(b[0]), float64(b[1])

		// Calculate grid differences
		dx := math.Round((bx - midX) / BOARD_BLOCK_W) // dx > 0 means right
		dy := math.Round((by - midY) / BOARD_BLOCK_H) // dy > 0 means down
		idx := int(dx)
		idy := int(dy)

		// Validate coordinates bounds
		if idx >= -proj.ExtX && idx <= proj.ExtX && idy >= -proj.ExtY && idy <= proj.ExtY {
			// Normalize: [-extX, extX] -> [0, 2*extX]
			gridBlocks = append(gridBlocks, &BannedBlockDesc{
				Loc:    [2]int{idx + proj.ExtX, idy + proj.ExtY},
				RawLoc: b,
			})
		}
	}
	return gridBlocks
}

func getProjDesc(ctx *maa.Context, img image.Image, targetHue int) *ProjDesc {
	maxExtent := BOARD_MAX_EXTENT_ONE_SIDE

	// Scan X Axis
	bestExtY := 0
	var fullXProjList []int

	// Try extents from high to low
	for extY := maxExtent; extY >= 0; extY-- {
		// Calculate Y coordinate of the X-Projection row
		projY := BOARD_CENTER_BLOCK_LT_Y + float64(-extY)*BOARD_BLOCK_H

		// Scan columns
		currentSum := 0
		currentList := make([]int, 2*maxExtent+1)
		for dx := -maxExtent; dx <= maxExtent; dx++ {
			projX := BOARD_CENTER_BLOCK_LT_X + float64(dx)*BOARD_BLOCK_W
			count := getProjFigureNumber(ctx, img, int(projX), int(projY-BOARD_X_PROJ_FIGURE_H), "X", targetHue)
			currentSum += count
			currentList[dx+maxExtent] = count
		}

		if currentSum > 0 {
			bestExtY = extY
			fullXProjList = currentList
			break
		}
	}

	// Scan Y Axis
	bestExtX := 0
	var fullYProjList []int

	for extX := maxExtent; extX >= 0; extX-- {
		// Calculate X coordinate of the Y-Projection column
		projX := BOARD_CENTER_BLOCK_LT_X + float64(-extX)*BOARD_BLOCK_W

		// Scan rows
		currentSum := 0
		currentList := make([]int, 2*maxExtent+1)
		for dy := -maxExtent; dy <= maxExtent; dy++ {
			projY := BOARD_CENTER_BLOCK_LT_Y + float64(dy)*BOARD_BLOCK_H
			count := getProjFigureNumber(ctx, img, int(projX-BOARD_Y_PROJ_FIGURE_W), int(projY), "Y", targetHue)
			currentSum += count
			currentList[dy+maxExtent] = count
		}

		if currentSum > 0 {
			bestExtX = extX
			fullYProjList = currentList
			break
		}
	}

	log.Debug().Int("bestExtX", bestExtX).Int("bestExtY", bestExtY).Msg("Board shape extents determination")
	log.Debug().Interface("fullXProjList", fullXProjList).Interface("fullYProjList", fullYProjList).Msg("Board full projection number lists")

	// Construct Result
	finalXProjList := make([]int, 0)
	startIdxX := maxExtent - bestExtX
	endIdxX := maxExtent + bestExtX
	if startIdxX >= 0 && endIdxX < len(fullXProjList) {
		finalXProjList = fullXProjList[startIdxX : endIdxX+1]
	}

	finalYProjList := make([]int, 0)
	startIdxY := maxExtent - bestExtY
	endIdxY := maxExtent + bestExtY
	if startIdxY >= 0 && endIdxY < len(fullYProjList) {
		finalYProjList = fullYProjList[startIdxY : endIdxY+1]
	}

	return &ProjDesc{
		ExtX:      bestExtX,
		ExtY:      bestExtY,
		XProjList: finalXProjList,
		YProjList: finalYProjList,
	}
}

func getProjFigureNumber(ctx *maa.Context, img image.Image, ltX, ltY int, axis string, targetHue int) int {
	samplingPoints := []float64{0.25, 0.50, 0.75}
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

func getPuzzleDesc(img image.Image) *PuzzleDesc {
	blocks := [][2]int{}
	var totalHue float64
	count := 0
	// Center block is at (0, 0) relative to core
	// Coordinates of the center block in the preview image
	// The drag target (PUZZLE_PREVIEW_MV_X, PUZZLE_PREVIEW_MV_Y) corresponds to the CENTER of the core block.
	coreX := PUZZLE_PREVIEW_MV_X
	coreY := PUZZLE_PREVIEW_MV_Y

	for dy := -PUZZLE_MAX_EXTENT_ONE_SIDE; dy <= PUZZLE_MAX_EXTENT_ONE_SIDE; dy++ {
		for dx := -PUZZLE_MAX_EXTENT_ONE_SIDE; dx <= PUZZLE_MAX_EXTENT_ONE_SIDE; dx++ {
			// Calculate block center
			blockCenterX := coreX + float64(dx)*PUZZLE_W
			blockCenterY := coreY + float64(dy)*PUZZLE_H

			// Calculate block rect (top-left to bottom-right)
			x1 := int(blockCenterX - PUZZLE_W/2)
			y1 := int(blockCenterY - PUZZLE_H/2)
			x2 := x1 + int(PUZZLE_W)
			y2 := y1 + int(PUZZLE_H)

			rect := image.Rect(x1, y1, x2, y2)

			variance := calcColorVar(img, rect)
			saturation := calcColorSat(img, rect)
			value := calcColorVal(img, rect)
			hue := calcColorHue(img, rect)

			isBlock := variance > PUZZLE_COLOR_VAR_GRT && saturation > PUZZLE_COLOR_SAT_GRT && value > PUZZLE_COLOR_VAL_GRT

			if isBlock {
				blocks = append(blocks, [2]int{dx, dy})
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
	for r := 0; r < PUZZLE_THUMBNAIL_MAX_ROWS; r++ {
		for c := 0; c < PUZZLE_THUMBNAIL_MAX_COLS; c++ {
			x := int(PUZZLE_THUMBNAIL_START_X + float64(c)*PUZZLE_THUMBNAIL_W)
			y := int(PUZZLE_THUMBNAIL_START_Y + float64(r)*PUZZLE_THUMBNAIL_H)
			rect := image.Rect(x, y, x+int(PUZZLE_THUMBNAIL_W), y+int(PUZZLE_THUMBNAIL_H))

			variance := calcColorVar(img, rect)
			// log.Debug().Int("r", r).Int("c", c).Float64("var", variance).Msg("Puzzle thumbnail area color variance")

			// Threshold for standard deviation: if it's too low, the area is likely a solid background.
			if variance > PUZZLE_THUMBNAIL_COLOR_VAR_GRT {
				results = append(results, [2]int{x, y})
			}
		}
	}
	return results
}

func doPreviewPuzzle(ctx *maa.Context, thumbX, thumbY int) *PuzzleDesc {
	ctrl := ctx.GetTasker().GetController()
	log.Debug().Int("thumbX", thumbX).Int(" thumbY", thumbY).Msg("Previewing puzzle thumbnail")

	// 1. Drag thumbnail to preview area
	// Start point is center of the thumbnail
	startX := int32(float64(thumbX + int(PUZZLE_THUMBNAIL_W)/2))
	startY := int32(float64(thumbY + int(PUZZLE_THUMBNAIL_H)/2))

	// End point is preview area center
	endX := int32(PUZZLE_PREVIEW_MV_X)
	endY := int32(PUZZLE_PREVIEW_MV_Y)

	ctrl.PostTouchUp(0).Wait()
	time.Sleep(100 * time.Millisecond)

	ctrl.PostTouchMove(0, startX, startY, 1).Wait()
	time.Sleep(100 * time.Millisecond)

	ctrl.PostTouchDown(0, startX, startY, 1).Wait()
	time.Sleep(250 * time.Millisecond)

	ctrl.PostTouchMove(0, endX, endY, 1).Wait()
	time.Sleep(500 * time.Millisecond)

	// 2. Screenshot
	ctrl.PostScreencap().Wait()
	previewImg := ctrl.CacheImage()
	if previewImg == nil {
		log.Error().Msg("Failed to capture preview image")
		ctrl.PostTouchUp(0).Wait()
		return nil
	}

	// 3. Touch Up (Release)
	ctrl.PostTouchUp(0).Wait()

	// 4. Analyze
	return getPuzzleDesc(previewImg)
}

func getLockedBlocks(img image.Image, proj *ProjDesc, targetHue int) []*LockedBlockDesc {
	locked := []*LockedBlockDesc{}
	extX := proj.ExtX
	extY := proj.ExtY

	for dy := -extY; dy <= extY; dy++ {
		for dx := -extX; dx <= extX; dx++ {
			// Top-left of the block (dx, dy)
			ltX := BOARD_CENTER_BLOCK_LT_X + float64(dx)*BOARD_BLOCK_W
			ltY := BOARD_CENTER_BLOCK_LT_Y + float64(dy)*BOARD_BLOCK_H

			// Sampling center point of the block
			centerX := int(ltX + BOARD_BLOCK_W/2)
			centerY := int(ltY + BOARD_BLOCK_H/2)

			h, s, v := getPixelHSV(img, centerX, centerY, targetHue, PUZZLE_CLUSTER_DIFF_GRT)
			// log.Debug().Int("dx", dx).Int("dy", dy).
			// 	Int("hue", int(h)).Float64("sat", s).Float64("val", v).
			// 	Msg("Block color HSV for locked block detection")

			if s > BOARD_LOCKED_COLOR_SAT_GRT && v > BOARD_LOCKED_COLOR_VAL_GRT {
				locked = append(locked, &LockedBlockDesc{
					Loc:    [2]int{dx + extX, dy + extY}, // Normalized origin
					RawLoc: [2]int{int(ltX), int(ltY)},
					Hue:    int(h),
				})
			}
		}
	}
	return locked
}

func getBannedBlocks(ctx *maa.Context, img image.Image) [][2]int {
	return getBlocksByTemplate(ctx, img, "PuzzleSolver/BlockBanned.png")
}

func getBlocksByTemplate(ctx *maa.Context, img image.Image, templatePath string) [][2]int {
	nodeName := "PuzzleBlockCheck_" + templatePath
	config := map[string]any{
		nodeName: map[string]any{
			"recognition": "TemplateMatch",
			"template":    templatePath,
			"threshold":   0.65,
			"roi": []int{
				int(0.3 * WORK_W),
				int(0.2 * WORK_H),
				int(0.4 * WORK_W),
				int(0.6 * WORK_H),
			},
			"method": 5, // TM_CCOEFF_NORMED
		},
	}

	res := ctx.RunRecognition(nodeName, img, config)
	if res == nil || !res.Hit {
		return nil
	}

	var detail struct {
		All []struct {
			Box   []int   `json:"box"`
			Score float64 `json:"score"`
		} `json:"all"`
	}
	if err := json.Unmarshal([]byte(res.DetailJson), &detail); err != nil {
		log.Error().Err(err).Str("json", res.DetailJson).Msg("Failed to unmarshal recognition detail")
		return nil
	}

	blocks := make([][2]int, 0, len(detail.All))
	for _, match := range detail.All {
		if len(match.Box) >= 2 {
			// Box is [x, y, w, h]
			blocks = append(blocks, [2]int{match.Box[0], match.Box[1]})
		}
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

	// 1. Find banned blocks

	banned := getBannedBlocks(ctx, img)
	log.Info().Interface("banned", banned).Msg("Puzzle banned blocks")

	// 2. Find all puzzles to be placed
	puzzleList := getAllPuzzleDesc(ctx, img)

	if len(puzzleList) == 0 {
		log.Info().Msg("No puzzles detected")
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{}`,
		}, false
	}

	// 3. Find possible hues from puzzles
	hueList := getPossibleHues(puzzleList)
	var projDescList []ProjDesc
	var lockedBlockList [][]*LockedBlockDesc

	// 4. For each hue, determine board projection and locked blocks
	var refProj *ProjDesc

	for i, hue := range hueList {
		projDesc := getProjDesc(ctx, img, hue)
		log.Debug().Int("hue", hue).Interface("projDesc", projDesc).Msg("Puzzle board projection description for hue")

		if i == 0 {
			refProj = projDesc
		} else {
			if projDesc.ExtX != refProj.ExtX || projDesc.ExtY != refProj.ExtY {
				log.Error().
					Int("hue", hue).
					Int("extX", projDesc.ExtX).Int("extY", projDesc.ExtY).
					Int("refExtX", refProj.ExtX).Int("refExtY", refProj.ExtY).
					Msg("Inconsistent board extents detected between hues")
				return nil, false
			}
		}

		locked := getLockedBlocks(img, projDesc, hue)
		log.Debug().Int("hue", hue).Interface("locked", locked).Msg("Puzzle locked blocks for hue")

		projDescList = append(projDescList, *projDesc)
		lockedBlockList = append(lockedBlockList, locked)
	}

	if refProj == nil {
		refProj = &ProjDesc{}
	}

	// 5. Construct board description
	boardDesc := &BoardDesc{
		ProjDescList:    projDescList,
		BannedBlockList: convertBlockLtToBoardCoord(refProj, banned),
		LockedBlockList: lockedBlockList,
		PuzzleList:      puzzleList,
		HueList:         hueList,
	}
	log.Info().Interface("boardDesc", boardDesc).Msg("Puzzle board description")

	// 6. Convert to JSON and return
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
