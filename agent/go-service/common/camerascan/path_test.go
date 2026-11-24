package camerascan

import "testing"

func TestSpiralCellsCoverGridOnce(t *testing.T) {
	cells := spiralCells()
	wantCells := gridSize * gridSize
	if len(cells) != wantCells {
		t.Fatalf("spiral cells = %d, want %d", len(cells), wantCells)
	}

	seen := make(map[[2]int]struct{}, wantCells)
	for index, cell := range cells {
		if cell[0] < 0 || cell[0] >= gridSize || cell[1] < 0 || cell[1] >= gridSize {
			t.Fatalf("cell %d is outside the grid: %v", index, cell)
		}
		if _, duplicated := seen[cell]; duplicated {
			t.Fatalf("cell %v is visited twice", cell)
		}
		seen[cell] = struct{}{}
		if index == 0 {
			continue
		}
		previous := cells[index-1]
		if absInt(cell[0]-previous[0])+absInt(cell[1]-previous[1]) != 1 {
			t.Fatalf("cells %v and %v are not adjacent", previous, cell)
		}
	}
}

func TestGridStepsStartAtCenterWithoutMoving(t *testing.T) {
	steps := gridSteps()
	if len(steps) != gridSize*gridSize {
		t.Fatalf("grid steps = %d, want %d", len(steps), gridSize*gridSize)
	}

	first := steps[0]
	if first.yawDelta != 0 || first.pitchDelta != 0 {
		t.Fatalf("first step moves the camera: yaw=%d pitch=%d", first.yawDelta, first.pitchDelta)
	}
	if !first.recognize {
		t.Fatal("first step must recognize")
	}

	for index, step := range steps[1:] {
		if moving := absInt(step.yawDelta) + absInt(step.pitchDelta); moving != 1 {
			t.Fatalf("step %d moves %d units, want 1", index+1, moving)
		}
		if !step.recognize {
			t.Fatalf("step %d must recognize", index+1)
		}
	}
}

func TestResetReturnsCameraToCenter(t *testing.T) {
	steps := buildCameraScanPath(defaultFallbackYawSteps)

	yaw, pitch := 0, 0
	resetIndex := -1
	for index, step := range steps {
		if step.phase == phaseReset {
			resetIndex = index
			break
		}
		yaw += step.yawDelta
		pitch += step.pitchDelta
	}
	if resetIndex < 0 {
		t.Fatal("path has no reset step")
	}

	reset := steps[resetIndex]
	if reset.recognize {
		t.Fatal("reset step must not recognize")
	}
	yaw += reset.yawDelta
	pitch += reset.pitchDelta
	if yaw != 0 || pitch != 0 {
		t.Fatalf("camera is not centered after reset: yaw=%d pitch=%d", yaw, pitch)
	}
}

func TestFallbackRingsTurnFullCircle(t *testing.T) {
	const yawSteps = 8
	turns := make(map[string]int, 3)
	for _, step := range buildCameraScanPath(yawSteps) {
		if step.phase == phaseGrid || step.phase == phaseReset {
			continue
		}
		if !step.recognize {
			t.Fatalf("fallback step in %s must recognize", step.phase)
		}
		turns[step.phase] += step.yawDelta
	}

	if len(turns) != 3 {
		t.Fatalf("fallback phases = %d, want 3", len(turns))
	}
	wantTurns := ringStepUnits * yawSteps
	for phase, turn := range turns {
		if turn != wantTurns {
			t.Fatalf("%s turns %d units, want %d", phase, turn, wantTurns)
		}
	}
}
