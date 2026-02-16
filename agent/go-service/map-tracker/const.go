// Copyright (c) 2026 Harry Huang
package maptracker

import "image"

// Location inference configuration
var (
	// Mini-map crop area
	LOC_CENTER_X = 108
	LOC_CENTER_Y = 111
	LOC_RADIUS   = 40

	// Valid area mapping for each map (Optional)
	// Key is MapData.Name, Value is the valid rectangle in the map image
	VALID_RECT_MAP = map[string]image.Rectangle{
		"map01_lv001": image.Rect(135, 195, 840, 685),
		"map01_lv002": image.Rect(85, 80, 360, 400),
		"map01_lv003": image.Rect(60, 90, 365, 405),
		"map01_lv005": image.Rect(60, 95, 585, 470),
		"map01_lv006": image.Rect(115, 100, 590, 690),
		"map01_lv007": image.Rect(170, 105, 605, 620),
		"map02_lv001": image.Rect(90, 30, 605, 620),
		"map02_lv002": image.Rect(90, 110, 790, 1010),
	}
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
