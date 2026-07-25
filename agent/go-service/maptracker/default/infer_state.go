// Copyright (c) 2026 Harry Huang
package maptrackerdefault

import (
	"sync"
	"time"

	internal "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/internal"
)

// InferLocationHitMode represents the mode of location inference hit
type InferLocationHitMode string

const (
	FULL_SEARCH_HIT InferLocationHitMode = "FullSearchHit"
	FAST_SEARCH_HIT InferLocationHitMode = "FastSearchHit"
)

// Time-series empirical optimization configuration
const (
	PENDING_TAKEOVER_TIME_MS         = 1000
	PENDING_TAKEOVER_COUNT_THRESHOLD = 3
	CONVINCED_DISTANCE_THRESHOLD     = 20
	CONVINCED_VALID_TIME_MS          = 2000
)

var emptyLocationRawResult = InferLocationRawResult{}

// InferState manages the state for map tracking inference
type InferState struct {
	convinced            InferLocationRawResult
	convincedLastHitTime int64

	pending             InferLocationRawResult
	pendingFirstHitTime int64
	pendingHitCount     int

	mu       sync.Mutex
	lockTime int64
}

var globalInferState InferState

// Lock acquires the state mutex and records the current time
func (s *InferState) Lock() {
	s.mu.Lock()
	s.lockTime = time.Now().UnixMilli()
}

// Unlock releases the state mutex and clears the lock time
func (s *InferState) Unlock() {
	s.lockTime = 0
	s.mu.Unlock()
}

// getLockTime returns the lock time, panics if not locked
func (s *InferState) getLockTime() int64 {
	if s.lockTime == 0 {
		panic("InferState method called without holding Lock")
	}
	return s.lockTime
}

// SetConvinced sets the convinced state and updates last hit time
func (s *InferState) SetConvinced(loc InferLocationRawResult) {
	nowMs := s.getLockTime()
	s.convinced = loc
	s.convincedLastHitTime = nowMs
}

// SetPending sets the pending state and initializes hit count to 1
func (s *InferState) SetPending(loc InferLocationRawResult) {
	nowMs := s.getLockTime()
	s.pending = loc
	s.pendingFirstHitTime = nowMs
	s.pendingHitCount = 1
}

// UpdatePending updates the pending location and increments hit count
func (s *InferState) UpdatePending(loc internal.Point) {
	_ = s.getLockTime()
	s.pending.Loc = loc
	s.pendingHitCount++
}

// TakeoverPending promotes pending to convinced state
func (s *InferState) TakeoverPending() {
	nowMs := s.getLockTime()
	s.convinced = s.pending
	s.convincedLastHitTime = nowMs
	s.ResetPending()
}

// ResetPending clears the pending state
func (s *InferState) ResetPending() {
	_ = s.getLockTime()
	s.pending = emptyLocationRawResult
	s.pendingFirstHitTime = 0
	s.pendingHitCount = 0
}

// IsConvincedValid checks if convinced state is still valid based on time
func (s *InferState) IsConvincedValid() bool {
	nowMs := s.getLockTime()
	return s.convinced.MapName != "" &&
		(nowMs-s.convincedLastHitTime < CONVINCED_VALID_TIME_MS) &&
		s.pendingHitCount == 0
}

// IsCloseToConvinced checks if a location is close to the convinced location
func (s *InferState) IsCloseToConvinced(loc *InferLocationRawResult) bool {
	if !isMapNameCoreMatch(s.convinced.MapName, loc.MapName) {
		return false
	}
	return s.convinced.Loc.DistanceTo(loc.Loc) < CONVINCED_DISTANCE_THRESHOLD
}

// IsCloseToPending checks if a location is close to the pending location
func (s *InferState) IsCloseToPending(loc *InferLocationRawResult) bool {
	if !isMapNameCoreMatch(s.pending.MapName, loc.MapName) {
		return false
	}
	return s.pending.Loc.DistanceTo(loc.Loc) < CONVINCED_DISTANCE_THRESHOLD
}

// ShouldTakeoverPending checks if pending should be promoted to convinced
func (s *InferState) ShouldTakeoverPending() bool {
	nowMs := s.getLockTime()
	return s.convinced.MapName == "" ||
		nowMs-s.pendingFirstHitTime >= PENDING_TAKEOVER_TIME_MS ||
		s.pendingHitCount >= PENDING_TAKEOVER_COUNT_THRESHOLD
}

// IsImmediateTrackLoss checks if this is an immediate track loss
func (s *InferState) IsImmediateTrackLoss() bool {
	nowMs := s.getLockTime()
	return nowMs-s.convincedLastHitTime < CONVINCED_VALID_TIME_MS
}
