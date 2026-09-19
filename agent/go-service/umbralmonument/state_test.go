package umbralmonument

import (
	"image"
	"image/color"
	"testing"
)

func TestProgress(t *testing.T) {
	for _, tc := range []struct {
		completed                int
		attempts                 uint8
		includeHard, hard, found bool
	}{
		{0, 0, false, false, true}, {0, 0, true, false, true},
		{1, 0, false, false, false}, {1, 0, true, true, true},
		{2, 0, true, false, false}, {0, 3, true, false, false}, {1, 2, true, false, false},
	} {
		hard, found := nextDifficulty(tc.completed, tc.attempts, tc.includeHard)
		if hard != tc.hard || found != tc.found {
			t.Fatalf("%+v: got hard=%v found=%v", tc, hard, found)
		}
	}
	s := state{Stage: "关卡", Attempts: map[string]uint8{"关卡": 1}, Passed: map[string]int{}}
	s.finish(false)
	if s.Attempts[s.Stage] != 3 {
		t.Fatal("normal failure must also skip hard")
	}
	s.Attempts[s.Stage] = 1
	s.finish(true)
	if hard, ok := nextDifficulty(s.Passed[s.Stage], s.Attempts[s.Stage], true); !hard || !ok {
		t.Fatal("normal success must unlock hard")
	}
	s.Hard = true
	s.Attempts[s.Stage] |= 2
	s.finish(false)
	if _, ok := nextDifficulty(s.Passed[s.Stage], s.Attempts[s.Stage], true); ok {
		t.Fatal("hard failure must not retry")
	}
	s.finish(true)
	if s.Passed[s.Stage] != 2 {
		t.Fatal("hard success must complete both modes")
	}
}

func TestStageIconAndTitle(t *testing.T) {
	for _, tc := range []struct {
		c    color.RGBA
		want int
	}{
		{color.RGBA{220, 220, 220, 255}, 0}, {color.RGBA{220, 50, 50, 255}, 1},
		{color.RGBA{220, 200, 30, 255}, 2}, {color.RGBA{20, 20, 20, 255}, -1},
	} {
		img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
		for y := 95; y < 106; y++ {
			for x := 50; x < 60; x++ {
				img.SetRGBA(x, y, tc.c)
			}
		}
		if got := stageState(img, [4]int{75, 90, 70, 20}); got != tc.want {
			t.Fatalf("icon=%v got=%d want=%d", tc.c, got, tc.want)
		}
	}
	if !sameTitle("忿鼓咆声苦难", "忿鼓咆声") || !sameTitle("忿鼓咆声", "忿鼓咆生") || sameTitle("刺痛盾阵", "死寂表象") {
		t.Fatal("selected-title verification changed")
	}
}
