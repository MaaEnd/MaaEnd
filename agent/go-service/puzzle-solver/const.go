// Copyright (c) 2026 Harry Huang
package puzzle

const (
	WORK_W = 1280
	WORK_H = 720
)

// Puzzle thumbnail area parameters
var (
	PUZZLE_THUMBNAIL_W             = 0.078 * float64(WORK_W)
	PUZZLE_THUMBNAIL_H             = 0.140 * float64(WORK_H)
	PUZZLE_THUMBNAIL_START_X       = 0.808 * float64(WORK_W)
	PUZZLE_THUMBNAIL_START_Y       = 0.166 * float64(WORK_H)
	PUZZLE_THUMBNAIL_MAX_COLS      = 2
	PUZZLE_THUMBNAIL_MAX_ROWS      = 4
	PUZZLE_THUMBNAIL_COLOR_VAR_GRT = 15.0
	PUZZLE_THUMBNAIL_COLOR_VAR_LES = 45.0
	PUZZLE_CLUSTER_DIFF_GRT        = 24
)

// Puzzle preview parameters
var (
	PUZZLE_X                   = 0.048 * float64(WORK_W)
	PUZZLE_Y                   = 0.085 * float64(WORK_H)
	PUZZLE_W                   = 0.048 * float64(WORK_W)
	PUZZLE_H                   = 0.082 * float64(WORK_H)
	PUZZLE_MAX_EXTENT_ONE_SIDE = 3
	PUZZLE_PREVIEW_MV_X        = 0.800 * float64(WORK_W)
	PUZZLE_PREVIEW_MV_Y        = 0.755 * float64(WORK_H)
	PUZZLE_COLOR_VAR_GRT       = 25.0
	PUZZLE_COLOR_SAT_GRT       = 0.50
	PUZZLE_COLOR_VAL_GRT       = 0.60
)

// Board parameters
var (
	BOARD_CENTER_BLOCK_LT_X    = 0.477 * float64(WORK_W)
	BOARD_CENTER_BLOCK_LT_Y    = 0.460 * float64(WORK_H)
	BOARD_BLOCK_W              = 0.048 * float64(WORK_W)
	BOARD_BLOCK_H              = 0.085 * float64(WORK_H)
	BOARD_LOCKED_COLOR_SAT_GRT = 0.45
	BOARD_LOCKED_COLOR_VAL_GRT = 0.35
	BOARD_MAX_EXTENT_ONE_SIDE  = 3
	BOARD_X_PROJ_FIGURE_H      = 1.25 * BOARD_BLOCK_W
	BOARD_Y_PROJ_FIGURE_W      = 1.25 * BOARD_BLOCK_H
	BOARD_PROJ_COLOR_SAT_GRT   = 0.40
	BOARD_PROJ_COLOR_VAL_GRT   = 0.15
	BOARD_PROJ_INIT_GAP        = 0.007 * float64(WORK_H)
	BOARD_PROJ_EACH_GAP        = 0.013 * float64(WORK_H)
)

// Other UI parameters
var (
	TAB_1_X = 0.463 * float64(WORK_W)
	TAB_2_X = 0.505 * float64(WORK_W)
	TAB_Y   = 0.910 * float64(WORK_H)
	TAB_W   = 0.029 * float64(WORK_W)
	TAB_H   = 0.029 * float64(WORK_H)
)
