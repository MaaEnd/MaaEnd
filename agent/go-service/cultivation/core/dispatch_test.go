package core

import (
	"image"
	"image/color"
	"image/draw"
	"reflect"
	"testing"
)

func TestRedOwnedIgnoresOtherColorsAndAreas(t *testing.T) {
	box := [4]int{10, 10, 50, 15}
	for _, test := range []struct {
		color color.RGBA
		want  bool
	}{
		{color.RGBA{212, 73, 112, 255}, true}, {color.RGBA{255, 255, 255, 255}, false},
		{color.RGBA{180, 180, 180, 255}, false}, {color.RGBA{240, 210, 10, 255}, false},
	} {
		img := image.NewRGBA(image.Rect(0, 0, 100, 40))
		draw.Draw(img, image.Rect(10, 10, 60, 25), &image.Uniform{test.color}, image.Point{}, draw.Src)
		if got := RedOwned(img, box); got != test.want {
			t.Fatalf("color %v: got %v", test.color, got)
		}
		if RedOwned(img, [4]int{60, 10, 20, 15}) || RedOwned(img, [4]int{-1, 0, 5, 5}) {
			t.Fatal("red outside the owned label was included")
		}
	}
	if RedOwned(nil, box) {
		t.Fatal("nil image matched")
	}
}

func TestRoutesDeduplicateWithoutPlanningStock(t *testing.T) {
	got := Routes([]Material{{ID: "item_expcard_stage1_low"}, {ID: "item_expcard_stage1_high"}, {ID: "item_expcard_stage1_low"}, {ID: "unknown"}, {ID: "item_gold"}})
	want := []string{"COMBAT_RECORD_EXP", "T_CREDS"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(Routes([]Material{{ID: "item_weapon_expcard_low"}, {ID: "plant_manual_only"}})) != 0 {
		t.Fatal("unsupported item was dispatched")
	}
}
