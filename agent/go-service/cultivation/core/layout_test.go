package core

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"
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
	p := plan(Material{ID: "item_gold", Name: "折金票", Need: 40, Owned: 30})
	p.Weapons = []Weapon{{Name: "宏愿", CandidateIDs: []string{"wpn_sword_0021", "wpn_sword_0021_ink"}, EssenceStatus: "unverified_manual"}}
	p.WeaponChoice = "宏愿"
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

func TestIconHashIgnoresQuantityBand(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := range 100 {
		for x := range 100 {
			a.Set(x, y, color.RGBA{uint8(x * 2), uint8(y), 80, 255})
		}
	}
	b := image.NewRGBA(a.Bounds())
	copy(b.Pix, a.Pix)
	// Different quantity text drawn in the bottom band
	for y := 80; y < 100; y++ {
		for x := range 100 {
			b.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	cell := image.Rect(10, 10, 90, 90)
	if IconHash(a, cell) != IconHash(b, cell) {
		t.Fatal("quantity band changed the icon hash")
	}
	if !strings.HasPrefix(UnknownID(IconHash(a, cell)), UnknownIDPrefix) {
		t.Fatal("unknown ID prefix")
	}
}

func TestFindCardsOnCloudScreenshot(t *testing.T) {
	path := os.Getenv("CULTIVATION_OVERVIEW_SAMPLE")
	if path == "" {
		t.Skip("set CULTIVATION_OVERVIEW_SAMPLE to a 1280x720 cultivation overview screenshot")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	cards := FindCards(img, image.Rect(30, 100, 380, 620))
	// 3 recent cards plus two full rows of 3 below them.
	if len(cards) < 9 {
		t.Fatalf("found %d cards: %v", len(cards), cards)
	}
	if first := cards[0]; abs(first.Min.X-44) > 3 || abs(first.Max.Y-256) > 3 {
		t.Fatalf("first card %v", first)
	}
}

func TestFindCardsSynthetic(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for _, c := range []image.Point{{44, 256}, {154, 256}, {44, 450}} {
		draw.Draw(img, image.Rect(c.X, c.Y, c.X+98, c.Y+4), &image.Uniform{color.RGBA{247, 105, 0, 255}}, image.Point{}, draw.Src)
	}
	cards := FindCards(img, image.Rect(30, 100, 380, 620))
	if len(cards) != 3 || cards[0].Min.X != 44 || cards[1].Min.X != 154 || cards[2].Max.Y != 450 {
		t.Fatalf("cards %v", cards)
	}
}
