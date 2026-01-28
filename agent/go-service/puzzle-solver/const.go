// Copyright (c) 2026 Harry Huang
package puzzle

const (
	WORK_W = 1280
	WORK_H = 720
)

// Puzzle thumbnail area parameters
var (
	PUZZLE_THUMB_W             = 0.078 * float64(WORK_W)
	PUZZLE_THUMB_H             = 0.140 * float64(WORK_H)
	PUZZLE_THUMB_START_X       = 0.808 * float64(WORK_W)
	PUZZLE_THUMB_START_Y       = 0.166 * float64(WORK_H)
	PUZZLE_THUMB_MAX_COLS      = 2
	PUZZLE_THUMB_MAX_ROWS      = 4
	PUZZLE_THUMB_COLOR_VAR_GRT = 15.0
	PUZZLE_THUMB_COLOR_VAR_LES = 45.0
	PUZZLE_HUE_DIFF_GRT        = 24
)

// Puzzle preview parameters
var (
	PUZZLE_W                   = 0.048 * float64(WORK_W)
	PUZZLE_H                   = 0.084 * float64(WORK_H)
	PUZZLE_PREVIEW_MV_CENTER_X = 0.800 * float64(WORK_W)
	PUZZLE_PREVIEW_MV_CENTER_Y = 0.755 * float64(WORK_H)
	PUZZLE_COLOR_VAR_GRT       = 25.0
	PUZZLE_COLOR_SAT_GRT       = 0.50
	PUZZLE_COLOR_VAL_GRT       = 0.60
	PUZZLE_MAX_EXTENT_ONE_SIDE = 3
)

// Board parameters
var (
	BOARD_X_LOWER_BOUND        = 0.226 * float64(WORK_W)
	BOARD_X_UPPER_BOUND        = 0.774 * float64(WORK_W)
	BOARD_Y_LOWER_BOUND        = 0.088 * float64(WORK_H)
	BOARD_Y_UPPER_BOUND        = 0.912 * float64(WORK_H)
	BOARD_CENTER_BLOCK_LT_X    = 0.477 * float64(WORK_W)
	BOARD_CENTER_BLOCK_LT_Y    = 0.460 * float64(WORK_H)
	BOARD_BLOCK_W              = 0.048 * float64(WORK_W)
	BOARD_BLOCK_H              = 0.085 * float64(WORK_H)
	BOARD_LOCKED_COLOR_SAT_GRT = 0.45
	BOARD_LOCKED_COLOR_VAL_GRT = 0.35
	BOARD_MAX_EXTENT_ONE_SIDE  = 3
)

// Projection figure parameters
var (
	PROJ_X_FIGURE_H    = 1.25 * BOARD_BLOCK_W
	PROJ_Y_FIGURE_W    = 1.25 * BOARD_BLOCK_H
	PROJ_COLOR_SAT_GRT = 0.50
	PROJ_COLOR_VAL_GRT = 0.30
	PROJ_INIT_GAP      = 0.007 * float64(WORK_H)
	PROJ_EACH_GAP      = 0.013 * float64(WORK_H)
)

// Other UI parameters
var (
	TAB_1_X = 0.463 * float64(WORK_W)
	TAB_2_X = 0.505 * float64(WORK_W)
	TAB_Y   = 0.910 * float64(WORK_H)
	TAB_W   = 0.029 * float64(WORK_W)
	TAB_H   = 0.029 * float64(WORK_H)
)
