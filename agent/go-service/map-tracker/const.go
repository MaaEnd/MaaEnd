// Copyright (c) 2026 Harry Huang
package maptracker

const (
	WORK_W = 1280
	WORK_H = 720
)

// Location inference configuration
const (
	// Mini-map crop area
	LOC_CENTER_X = 108
	LOC_CENTER_Y = 111
	LOC_RADIUS   = 40
)

// Rotation inference configuration
const (
	// Pointer crop area
	ROT_CENTER_X = 108
	ROT_CENTER_Y = 111
	ROT_RADIUS   = 12
)

// Resource paths
const (
	MAP_DIR      = "image/MapTracker/map"
	POINTER_PATH = "image/MapTracker/pointer.png"
)

// Move action configuration
const (
	INFER_INTERVAL_MS = 200
)

// MapTrackerInfer parameters default values
var DEFAULT_INFERENCE_PARAM = MapTrackerInferParam{
	MapNameRegex: "^map\\d+_lv\\d+$",
	Precision:    0.4,
	Threshold:    0.5,
}

// MapTrackerMove parameters default values
var DEFAULT_MOVING_PARAM = MapTrackerMoveParam{
	ArrivalThreshold:       4.5,
	ArrivalTimeout:         60000,
	RotationLowerThreshold: 8.0,
	RotationUpperThreshold: 60.0,
	RotationSpeed:          2.0,
	RotationTimeout:        30000,
	SprintThreshold:        25.0,
	StuckThreshold:         1500,
	StuckTimeout:           10000,
}

// Win32 action related codes
const (
	KEY_W     = 0x57
	KEY_A     = 0x41
	KEY_S     = 0x53
	KEY_D     = 0x44
	KEY_SHIFT = 0x10
	KEY_CTRL  = 0x11
	KEY_ALT   = 0x12
	KEY_SPACE = 0x20
)
