// Copyright (c) 2026 Harry Huang
package maptracker

const (
	WORK_W = 1280
	WORK_H = 720
)

// Location inference configuration
var (
	// Mini-map crop area
	LOC_CENTER_X = 108
	LOC_CENTER_Y = 111
	LOC_RADIUS   = 40
)

// Rotation inference configuration
var (
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
	INFER_INTERVAL_MS                = 200
	ARRIVAL_TOLERANCE                = 4.5 // Unit: mini-map pixel distance
	ROTATION_LOW_TOLERANCE           = 8   // Unit: degree
	ROTATION_HIGH_TOLERANCE          = 60  // Unit: degree
	ROTATION_SENSITIVITY             = 2.0
	STUCK_MIN_DURATION_MS            = 1500
	SPRINT_MIN_DISTANCE              = 15.0 // Unit: mini-map pixel distance
	FAILURE_ARRIVAL_MAX_DURATION_MS  = 60000
	FAILURE_ROTATION_MAX_DURATION_MS = 30000
	FAILURE_STUCK_MAX_DURATION_MS    = 10000
)

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
