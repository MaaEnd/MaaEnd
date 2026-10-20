package core

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"testing"
)

func TestScrollbarAndChangedViewport(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	draw.Draw(a, image.Rect(1232, 270, 1238, 461), &image.Uniform{color.White}, image.Point{}, draw.Src)
	start, end, ok := Scrollbar(a)
	if !ok || start != 270 || end != 460 {
		t.Fatalf("thumb %d %d %v", start, end, ok)
	}
	b := image.NewRGBA(a.Bounds())
	draw.Draw(b, b.Bounds(), a, image.Point{}, draw.Src)
	if !Stationary(a, b) {
		t.Fatal("identical viewport not stationary")
	}
	draw.Draw(b, image.Rect(450, 300, 700, 500), &image.Uniform{color.White}, image.Point{}, draw.Src)
	if Stationary(a, b) {
		t.Fatal("changed material viewport was treated as stationary")
	}
}

func TestPreservedDeviceTabLayout(t *testing.T) {
	path := os.Getenv("CULTIVATION_LAYOUT_FIXTURE")
	if path == "" {
		t.Skip("set CULTIVATION_LAYOUT_FIXTURE to a preserved, redacted 1280x720 screenshot")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if !TabSelected(img, 1143, 207) || TabSelected(img, 568, 207) {
		t.Fatal("old weapon-essence selected tab geometry differs")
	}
	if !Stationary(img, img) {
		t.Fatal("identical preserved screenshot differs")
	}
}

func TestWeaponMetadataDoesNotBecomeMaterialGoal(t *testing.T) {
	p := plan(Material{"item_gold", "折金票", 40, 30})
	p.Weapon = &Weapon{Name: "宏愿", CandidateIDs: []string{"wpn_sword_0021", "wpn_sword_0021_ink"}, EssenceStatus: "unverified_manual"}
	goals, manual, err := BuildTargets(p)
	if err != nil || len(goals) != 16 || len(manual) != 0 || goals["T_CREDS"] != 40 {
		t.Fatalf("%v %+v %+v", err, goals, manual)
	}
	for _, key := range []string{"wpn_sword_0021", "wpn_sword_0021_ink"} {
		if _, exists := goals[key]; exists {
			t.Fatal("weapon became farming material")
		}
	}
}
