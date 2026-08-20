package camerascan

const (
	phaseNineGrid     = "nine_grid"
	phaseReset        = "reset"
	phaseFallbackUp   = "fallback_up"
	phaseFallbackMid  = "fallback_middle"
	phaseFallbackDown = "fallback_down"
)

type cameraScanStep struct {
	yawDelta   int
	pitchDelta int
	phase      string
}

func (s cameraScanStep) needsRecognition() bool {
	return s.phase != phaseReset
}

var nineGridSteps = []cameraScanStep{
	{pitchDelta: -1, phase: phaseNineGrid},
	{yawDelta: 1, phase: phaseNineGrid},
	{pitchDelta: 1, phase: phaseNineGrid},
	{pitchDelta: 1, phase: phaseNineGrid},
	{yawDelta: -1, phase: phaseNineGrid},
	{yawDelta: -1, phase: phaseNineGrid},
	{pitchDelta: -1, phase: phaseNineGrid},
	{pitchDelta: -1, phase: phaseNineGrid},
}

func buildCameraScanPath(fallbackYawSteps int) []cameraScanStep {
	steps := append([]cameraScanStep(nil), nineGridSteps...)

	yaw, pitch := 0, 0
	for _, step := range nineGridSteps {
		yaw += step.yawDelta
		pitch += step.pitchDelta
	}
	steps = append(steps, cameraScanStep{
		yawDelta:   -yaw,
		pitchDelta: -pitch,
		phase:      phaseReset,
	})

	// After reset the camera is already at middle pitch. Scan middle, then look
	// up, then jump two steps down to the lower pitch.
	steps = appendFallbackRing(steps, 0, phaseFallbackMid, fallbackYawSteps)
	steps = appendFallbackRing(steps, -1, phaseFallbackUp, fallbackYawSteps)
	steps = appendFallbackRing(steps, 2, phaseFallbackDown, fallbackYawSteps)
	return steps
}

func appendFallbackRing(
	steps []cameraScanStep,
	pitchDelta int,
	phase string,
	fallbackYawSteps int,
) []cameraScanStep {
	steps = append(steps, cameraScanStep{
		pitchDelta: pitchDelta,
		phase:      phase,
	})
	for range fallbackYawSteps {
		steps = append(steps, cameraScanStep{
			yawDelta: 1,
			phase:    phase,
		})
	}
	return steps
}
