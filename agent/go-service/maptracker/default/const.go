// Copyright (c) 2026 Harry Huang
package maptrackerdefault

const (
	WORK_W = 1280
	WORK_H = 720
)

// Move action configuration
const (
	INFER_INTERVAL_MS              = 100
	ROTATION_MAX_SPEED             = 5.0
	ROTATION_DEFAULT_SPEED         = 2.5
	ROTATION_DEFAULT_SPEED_WLROOTS = 4.0
	ROTATION_MIN_SPEED             = 1.0
)

// Fine approach configuration
const (
	// FINE_APPROACH_COMPLETE_THRESHOLD is the distance in pixels
	// within which the target is considered precisely reached.
	FINE_APPROACH_COMPLETE_THRESHOLD = 0.325
	// FINE_APPROACH_IMPULSE_OFFSET is the fixed displacement in pixels that any movement impulse
	// produces regardless of its on-time, caused by the movement start-up and stop animations.
	FINE_APPROACH_IMPULSE_OFFSET = 0.325
	// FINE_APPROACH_MAX_CALIBRATIONS is the maximum number of movement impulses attempted.
	FINE_APPROACH_MAX_CALIBRATIONS = 3
	// FINE_APPROACH_SETTLE_MS is the time in ms to wait for the player to come to
	// a complete stop before measuring the location again.
	FINE_APPROACH_SETTLE_MS = 325
	// FINE_APPROACH_MIN_YAW_UPDATE_MS is the minimum impulse on-time in ms required
	// before the measured rotation is trusted to recalibrate the camera yaw. Shorter impulses
	// may not give the player enough time to finish turning toward the movement direction.
	FINE_APPROACH_MIN_YAW_UPDATE_MS = 325 / 2
	// FINE_APPROACH_MAX_YAW_UPDATE_DEG is the maximum camera yaw correction in degrees accepted
	// from one measurement. The camera is never rotated during the fine approach, so a larger
	// deviation indicates an unfinished player turn rather than a real yaw error.
	FINE_APPROACH_MAX_YAW_UPDATE_DEG = 32.5
)
