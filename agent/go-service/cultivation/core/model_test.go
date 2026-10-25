package core

import (
	"encoding/json"
	"image"
	"image/draw"
	"image/png"
	"os"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/jsonclean"
)

func TestMergedOwnedLabelsSnapToPreservedIconCards(t *testing.T) {
	f, err := os.Open("testdata/merged-owned.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	crop, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	frame := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	draw.Draw(frame, image.Rect(800, 500, 1010, 636), crop, image.Point{}, draw.Src)
	parts := SplitOwned([]Text{{Text: "拥有2.74万 拥有4467", Box: [4]int{823, 604, 165, 20}, Score: .99}})
	for i, part := range parts {
		x := part.Box[0] + part.Box[2]/2 - 40
		cell := AlignCellX(frame, image.Rect(x, part.Box[1]-83, x+80, part.Box[1]-3), 24)
		want := []int{824, 911}[i]
		if cell.Min.X < want-2 || cell.Min.X > want+2 {
			t.Fatalf("label %d snapped to %v, expected card x=%d", i, cell, want)
		}
	}
}

func TestHeaderRejectsChangedOrAmbiguousTarget(t *testing.T) {
	texts := []Text{{"//培养总览", [4]int{78, 27, 104, 28}, 1}, {"管理员", [4]int{489, 88, 63, 24}, 1}, {"培养期望：卓越", [4]int{1002, 116, 127, 22}, 1}}
	h, err := ReadHeader(texts)
	if err != nil || h.Operator != "管理员" || h.Expectation != "卓越" {
		t.Fatalf("%+v %v", h, err)
	}
	for _, changed := range []struct {
		index int
		text  string
	}{
		{1, "别礼"},
		{2, "培养期望：优秀"},
	} {
		other := append([]Text{}, texts...)
		other[changed.index].Text = changed.text
		got, err := ReadHeader(other)
		if err != nil || got == h {
			t.Fatalf("real identity or goal change was hidden: %+v %v", got, err)
		}
	}
	texts = append(texts, Text{"其他干员", [4]int{600, 90, 70, 20}, 1})
	if _, err = ReadHeader(texts); err == nil {
		t.Fatal("accepted ambiguous operator")
	}
}

func TestSplitOwnedMergedLabels(t *testing.T) {
	got := SplitOwned([]Text{{Text: "拥有2.73万 拥有4173", Box: [4]int{823, 359, 164, 17}, Score: .97}, {Text: "拥有251", Box: [4]int{835, 489, 60, 17}, Score: .9}})
	if len(got) != 3 || got[0].Text != "拥有2.73万" || got[1].Text != "拥有4173" || got[2].Text != "拥有251" {
		t.Fatalf("split = %+v", got)
	}
	if c := got[1].Box[0] + got[1].Box[2]/2; c < 940 || c > 965 {
		t.Fatalf("second label centre %d", c)
	}
}

func TestNothingNeededOnMaxedTab(t *testing.T) {
	if !NothingNeeded([]Text{{Text: "干员等级已达上限", Box: [4]int{752, 505, 166, 21}, Score: 1}}) {
		t.Fatal("maxed operator level not recognised")
	}
	if !NothingNeeded([]Text{{Text: "干员技能与天赋培养已满级", Box: [4]int{745, 498, 190, 20}, Score: 1}}) {
		t.Fatal("maxed skills and talents not recognised")
	}
	if NothingNeeded([]Text{{Text: "干员等级提升", Box: [4]int{443, 270, 103, 20}, Score: 1}}) {
		t.Fatal("section title taken as no demand")
	}
}

func TestOwnedRowsKeepsWholeEdgeRow(t *testing.T) {
	// 祀 on CloudADB: the bottom row's labels straddle the limit by a few pixels.
	in := []Text{{"拥有9", [4]int{747, 635, 49, 19}, 1}, {"拥有0", [4]int{661, 637, 48, 19}, 1}, {"拥有93", [4]int{944, 636, 59, 21}, 1}}
	if got := OwnedRows(in, false); len(got) != 3 || got[0].Text != "拥有0" {
		t.Fatalf("row starting inside the limit must be kept whole: %+v", got)
	}
	in = []Text{{"拥有9", [4]int{747, 640, 49, 19}, 1}, {"拥有0", [4]int{661, 636, 48, 19}, 1}}
	if got := OwnedRows(in, false); len(got) != 0 {
		t.Fatalf("row starting below the limit must be dropped whole: %+v", got)
	}
}

func TestOwnedRowsSkipsFadedTopRowWhenScrolled(t *testing.T) {
	// 祀 on CloudADB: the 稀有采集物 icon under the top fade read as unknown.
	in := []Text{{"拥有94", [4]int{922, 346, 50, 19}, 1}, {"拥有0", [4]int{661, 476, 48, 19}, 1}}
	if got := OwnedRows(in, false); len(got) != 2 {
		t.Fatalf("list top keeps the first row: %+v", got)
	}
	if got := OwnedRows(in, true); len(got) != 1 || got[0].Text != "拥有0" {
		t.Fatalf("scrolled page skips the faded top row: %+v", got)
	}
}

func TestPreservedHeaderOCRVariantDoesNotChangeOperator(t *testing.T) {
	data, err := os.ReadFile("testdata/last-rite-header.json")
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		Texts []Text `json:"texts"`
	}
	if err := json.Unmarshal(jsonclean.Clean(data), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 || len(records)%2 != 0 {
		t.Fatal("incomplete header evidence")
	}
	for i := 0; i < len(records); i += 2 {
		texts := append(append([]Text{}, records[i].Texts...), records[i+1].Texts...)
		got, err := ReadHeader(texts)
		if err != nil || got != (Header{Operator: "别礼", Expectation: "卓越"}) {
			t.Fatalf("header pair %d: %+v %v", i/2, got, err)
		}
	}
}

func TestClippedCardDoesNotSnapToOneBrightEdge(t *testing.T) {
	for _, sample := range []struct {
		name string
		x, y int
	}{{"SiClippedMaterial0029.png", 622, 479}, {"SiClippedMaterial0033.png", 623, 396}} {
		t.Run(sample.name, func(t *testing.T) {
			f, err := os.Open("testdata/" + sample.name)
			if err != nil {
				t.Fatal(err)
			}
			crop, err := png.Decode(f)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			frame := image.NewRGBA(image.Rect(0, 0, 1280, 720))
			draw.Draw(frame, image.Rect(600, sample.y-5, 810, sample.y+100), crop, image.Point{}, draw.Src)
			cell := image.Rect(sample.x, sample.y, sample.x+80, sample.y+80)
			got := AlignCellX(frame, cell, 24)
			if got != cell {
				t.Fatalf("one-sided clipped card moved from %v to %v", cell, got)
			}
		})
	}
}
