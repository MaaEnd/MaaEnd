package bettersliding

import "testing"

// 数据取自云终末地实测：终点被认成禁用的「+」按钮，目标 320 实际点到 519
func TestInterpolateClickTargetFromMeasuredQuantity(t *testing.T) {
	start := [2]int{506, 539}
	measured := [2]int{581, 539}

	if got := interpolateClickTarget(start, measured, 519, 320); got != [2]int{552, 539} {
		t.Fatalf("target = %v, want [552 539]", got)
	}
	// 外推出的终点落在滑到最大时手柄的真实位置，而不是右侧 763 处的「+」按钮
	if got := interpolateClickTarget(start, measured, 519, 960); got != [2]int{645, 539} {
		t.Fatalf("end = %v, want [645 539]", got)
	}
}

func TestBoxCenteredAtKeepsCenterPoint(t *testing.T) {
	like := []int{487, 521, 39, 37}
	offset := [2]int{2, -1}
	box := boxCenteredAt([2]int{645, 539}, like, offset)
	if x, y := centerPoint(box, offset); x != 645 || y != 539 {
		t.Fatalf("center = (%d, %d), want (645, 539)", x, y)
	}
}

func TestShouldRecalibratePreciseClickOnlyOnceAndOnlyWhenFarOff(t *testing.T) {
	a := &BetterSlidingAction{}
	a.TargetQuantity = 320
	a.startBox = []int{487, 521, 39, 37}
	a.preciseClickBase = [2]int{581, 539}

	if !a.shouldRecalibratePreciseClick(519) {
		t.Fatal("a miss of 199 should recalibrate")
	}
	if a.shouldRecalibratePreciseClick(320 + maxClickRepeat) {
		t.Fatal("a miss one fine-tune round can cover should not recalibrate")
	}
	if a.shouldRecalibratePreciseClick(1) {
		t.Fatal("quantity 1 cannot define a slope")
	}
	a.preciseClickRecalibrated = true
	if a.shouldRecalibratePreciseClick(519) {
		t.Fatal("recalibration must happen at most once per precise click")
	}
}
