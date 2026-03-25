// Copyright (c) 2026 Harry Huang
package maptracker

const (
	WORK_W = 1280
	WORK_H = 720
)

// Big map viewport configuration
const (
	VIEWPORT_PADDING_LR = 0.192 * WORK_W
	VIEWPORT_PADDING_TB = 0.208 * WORK_H
)

// Big map zoom button configuration
const (
	ZOOM_BUTTON_AREA_X    = 0.95 * WORK_W
	ZOOM_BUTTON_AREA_Y    = 0.25 * WORK_H
	ZOOM_BUTTON_AREA_W    = 0.05 * WORK_W
	ZOOM_BUTTON_AREA_H    = 0.50 * WORK_H
	ZOOM_BUTTON_THRESHOLD = 0.75
)

// Big map infer configuration
const (
	PADDING_LR           = 0.192 * WORK_W
	PADDING_TB           = 0.208 * WORK_H
	SAMPLE_PADDING_LR    = 0.4 * WORK_W
	SAMPLE_PADDING_TB    = 0.4 * WORK_H
	WIRE_MATCH_PRECISION = 0.5
	GAME_MAP_SCALE_MIN   = 1.0
	GAME_MAP_SCALE_MAX   = 7.0
)

// Big map pick configuration
const (
	BIG_MAP_PAN_FACTOR = 1.5
	BIG_MAP_PICK_RETRY = 10
)

// Resource paths
const (
	MAP_BBOX_DATA_PATH     = "data/MapTracker/map_bbox_data.json"
	MAP_EXTERNAL_DATA_PATH = "data/MapTracker/map_external_data.json"
	MAP_DIR                = "resource/image/MapTracker/map"
)

// Move action configuration
const (
	INFER_INTERVAL_MS      = 100
	ROTATION_MAX_SPEED     = 4.0
	ROTATION_DEFAULT_SPEED = 2.0
	ROTATION_MIN_SPEED     = 1.0
)

// Misc
const (
	RAW_MAP_BBOX_EXPAND_PX           = 20
	FINE_APPROACH_COMPLETE_THRESHOLD = 0.5
)
