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

// Time-series empirical optimization configuration
const (
	PENDING_TAKEOVER_TIME_MS         = 1000
	PENDING_TAKEOVER_COUNT_THRESHOLD = 3
	CONVINCED_DISTANCE_THRESHOLD     = 30
	CONVINCED_VALID_TIME_MS          = 2000
)

// Resource paths
const (
	MAP_DIR      = "image/MapTracker/map"
	POINTER_PATH = "image/MapTracker/pointer.png"
)

// Move action configuration
const (
	INFER_INTERVAL_MS               = 100
	ROTATION_MAX_SPEED              = 5.0
	ROTATION_DEFAULT_SPEED          = 3.0
	ROTATION_MIN_SPEED              = 1.0
	ROTATION_ADJUSTMENT_LOWER_BOUND = 6.0
)

// MapTrackerInfer parameters default values
var DEFAULT_INFERENCE_PARAM = MapTrackerInferParam{
	MapNameRegex: "^map\\d+_lv\\d+$",
	Precision:    0.5,
	Threshold:    0.4,
}

// MapTrackerInfer parameters for MapTrackerMove action default values
// (MapNameRegex is omitted here since MapTrackerMove always sets it)
var DEFAULT_INFERENCE_PARAM_FOR_MOVE = MapTrackerInferParam{
	Precision: 0.7,
	Threshold: 0.3,
}

// MapTrackerMove parameters default values
var DEFAULT_MOVING_PARAM = MapTrackerMoveParam{
	ArrivalThreshold:       3.5,
	ArrivalTimeout:         60000,
	RotationLowerThreshold: 7.5,
	RotationUpperThreshold: 60.0,
	SprintThreshold:        15.0,
	StuckThreshold:         2000,
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
