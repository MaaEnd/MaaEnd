package camerascan

import "testing"

func TestNineGridStepsFollowCenterOutSpiral(t *testing.T) {
	want := [][2]int{
		{0, -1},
		{1, -1},
		{1, 0},
		{1, 1},
		{0, 1},
		{-1, 1},
		{-1, 0},
		{-1, -1},
	}

	yaw, pitch := 0, 0
	for index, step := range nineGridSteps {
		yaw += step.yawDelta
		pitch += step.pitchDelta
		if got := [2]int{yaw, pitch}; got != want[index] {
			t.Fatalf("step %d: got position %v, want %v", index, got, want[index])
		}
	}
}

func TestResetStepsSkipRecognition(t *testing.T) {
	path := buildCameraScanPath(8)
	var resetCount int
	for index, step := range path {
		if step.phase == phaseReset {
			resetCount++
			if step.needsRecognition() {
				t.Fatalf("reset step %d should skip recognition", index)
			}
			continue
		}
		if !step.needsRecognition() {
			t.Fatalf("step %d phase %q should recognize", index, step.phase)
		}
	}
	if resetCount != 1 {
		t.Fatalf("got %d reset steps, want 1", resetCount)
	}
}

func TestCameraScanPathResetsBeforeFallback(t *testing.T) {
	const yawSteps = 12
	path := buildCameraScanPath(yawSteps)

	if got, want := len(path), len(nineGridSteps)+1+3*(yawSteps+1); got != want {
		t.Fatalf("got %d steps, want %d", got, want)
	}

	yaw, pitch := 0, 0
	for _, step := range path[:len(nineGridSteps)+1] {
		yaw += step.yawDelta
		pitch += step.pitchDelta
	}
	if yaw != 0 || pitch != 0 {
		t.Fatalf("nine-grid reset ended at (%d, %d), want origin", yaw, pitch)
	}
}

func TestFallbackRingsReturnToSameYaw(t *testing.T) {
	const yawSteps = 12
	path := buildCameraScanPath(yawSteps)
	fallback := path[len(nineGridSteps)+1:]

	wantPhases := []string{
		phaseFallbackMid,
		phaseFallbackUp,
		phaseFallbackDown,
	}
	wantPitches := []int{0, -1, 1}
	yaw, pitch := 0, 0
	for ringIndex, wantPhase := range wantPhases {
		ring := fallback[ringIndex*(yawSteps+1) : (ringIndex+1)*(yawSteps+1)]
		if ring[0].phase != wantPhase {
			t.Fatalf("ring %d phase is %q, want %q", ringIndex, ring[0].phase, wantPhase)
		}
		pitch += ring[0].pitchDelta
		for _, step := range ring[1:] {
			yaw = (yaw + step.yawDelta) % yawSteps
		}
		if yaw != 0 {
			t.Fatalf("ring %d ended at yaw %d, want 0", ringIndex, yaw)
		}
		if pitch != wantPitches[ringIndex] {
			t.Fatalf("ring %d ended at pitch %d, want %d", ringIndex, pitch, wantPitches[ringIndex])
		}
	}
}
