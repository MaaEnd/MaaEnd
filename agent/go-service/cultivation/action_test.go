package cultivation

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/jsonclean"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
)

func loadPipeline(t *testing.T) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile("../../../assets/resource/pipeline/Cultivation/Cultivation.json")
	if err != nil {
		t.Fatal(err)
	}
	var nodes map[string]map[string]any
	if err = json.Unmarshal(jsonclean.Clean(data), &nodes); err != nil {
		t.Fatal(err)
	}
	return nodes
}

func TestPipelineOwnsDecisionEdges(t *testing.T) {
	nodes := loadPipeline(t)
	for name, node := range nodes {
		encoded, _ := json.Marshal(node["action"])
		if strings.Contains(string(encoded), `"again"`) || strings.Contains(string(encoded), `"done"`) {
			t.Fatalf("%s carries a Go continuation", name)
		}
	}
	if !reflect.DeepEqual(nodes["CultivationFinish"]["next"], []any{"CultivationDone"}) {
		t.Fatal("finish hides its continuation")
	}
	if !reflect.DeepEqual(nodes["CultivationT1AtBottom"]["next"], []any{"CultivationFinish"}) {
		t.Fatal("the material tab pass must end after the skill tab")
	}
	for _, suffix := range []string{"0", "1"} {
		prefix := "CultivationT" + suffix
		for step, branches := range map[string][]any{"Top": {prefix + "AtTop", prefix + "AboveTop"}, "Bottom": {prefix + "AtBottom", prefix + "BelowPage"}} {
			if !reflect.DeepEqual(nodes[prefix+step]["next"], branches) {
				t.Fatalf("%s is not explicit", prefix+step)
			}
		}
	}
	for name := range nodes {
		for _, banned := range []string{"CultivationT2", "Weapon", "Dispatch", "Setup", "RouteReady", "NextRoute", "SanityLow"} {
			if strings.Contains(name, banned) {
				t.Fatalf("%s: weapon and forwarding nodes were removed", name)
			}
		}
	}
	for _, name := range []string{"CultivationDone", "CultivationSkipped", "CultivationOperatorEnd", "CultivationOperatorMissing", "CultivationExpectationMissing"} {
		if !reflect.DeepEqual(nodes[name]["next"], []any{"SupplyPlanRedCheckNext"}) {
			t.Fatalf("%s must hand control back to SupplyPlan", name)
		}
	}
}

func TestOperatorSwipesRequireSceneAndBoundaryChecks(t *testing.T) {
	nodes := loadPipeline(t)
	for _, direction := range []struct{ swipe, check, boundary string }{
		{"CultivationOperatorUp", "CultivationOperatorCheckUp", "CultivationOperatorTop"},
		{"CultivationOperatorDown", "CultivationOperatorCheckDown", "CultivationOperatorEnd"},
	} {
		t.Run(direction.swipe, func(t *testing.T) {
			reco, ok := nodes[direction.swipe]["recognition"].(map[string]any)
			if !ok || reco["type"] != "And" || !reflect.DeepEqual(reco["param"].(map[string]any)["all_of"], []any{"CultivationOperatorListReady"}) {
				t.Fatal("swipe is not guarded by the current overview/list state")
			}
			if !reflect.DeepEqual(nodes[direction.swipe]["next"], []any{direction.check}) {
				t.Fatal("swipe can repeat without a fresh scene and boundary check")
			}
			options := nodes[direction.check]["next"].([]any)
			if len(options) < 2 || options[len(options)-2] != direction.boundary || options[len(options)-1] != direction.swipe {
				t.Fatal("list boundary/progress is not checked before allowing another swipe")
			}
		})
	}
}

func TestOperatorSelectionWaitsForTheNameBeforeReportingFailure(t *testing.T) {
	nodes := loadPipeline(t)
	find := nodes["CultivationOperatorFind"]
	if !reflect.DeepEqual(find["next"], []any{"CultivationOperatorSelected"}) {
		t.Fatal("a stale first header can select a DirectHit failure instead of waiting for confirmation")
	}
	if !reflect.DeepEqual(find["on_error"], []any{"CultivationOperatorMissing"}) {
		t.Fatal("unconfirmed selection must terminate after the existing recognition timeout")
	}
	if find["action"].(map[string]any)["type"] != "Click" {
		t.Fatal("portrait selection lost its single click")
	}
	if _, ok := find["timeout"]; ok {
		t.Fatal("selection added a custom timeout instead of using normal recognition")
	}
	if _, ok := find["max_hit"]; ok {
		t.Fatal("selection added a retry counter")
	}
	for _, name := range []string{"CultivationOperatorPortrait", "CultivationOperatorSelected"} {
		if nodes[name]["enabled"] != false {
			t.Fatalf("%s must stay disabled until the runtime override enables it", name)
		}
	}
}

func TestUnrecognizedAndMissingAccumulatePerSession(t *testing.T) {
	s := &session{Missing: map[string]core.Material{}}
	for _, row := range []core.Material{{ID: "item_gold"}, {ID: "item_gold"}, {ID: "item_char_break_stage_1_2"}} {
		s.Missing[row.ID] = row
	}
	s.Unrecognized += 2
	s.Unrecognized += 1
	var rows []core.Material
	for _, row := range s.Missing {
		rows = append(rows, row)
	}
	if got := core.Routes(rows); !reflect.DeepEqual(got, []string{"PROTODISK", "T_CREDS"}) || s.Unrecognized != 3 {
		t.Fatalf("routes %v unrecognized %d", got, s.Unrecognized)
	}
}

func TestNoisyListStopsWithoutAnotherBlindSwipe(t *testing.T) {
	a, b := image.NewRGBA(image.Rect(0, 0, 1280, 720)), image.NewRGBA(image.Rect(0, 0, 1280, 720))
	r := rand.New(rand.NewSource(11))
	for y := 250; y < 665; y++ {
		for x := 430; x < 1225; x++ {
			a.SetRGBA(x, y, color.RGBA{uint8(r.Intn(200)), uint8(r.Intn(200)), uint8(r.Intn(200)), 255})
			b.SetRGBA(x, y, color.RGBA{uint8(r.Intn(200)), uint8(r.Intn(200)), uint8(r.Intn(200)), 255})
		}
	}
	if _, err := listBottom(a, b, true); err == nil {
		t.Fatal("noise permitted another swipe")
	}
	if _, err := listTop(a, b); err == nil {
		t.Fatal("noise permitted another upward swipe")
	}
	if _, err := listBottom(a, a, false); err == nil {
		t.Fatal("empty page accepted as bottom")
	}
	if decision, err := listBottom(a, a, true); err != nil || decision != "done" {
		t.Fatal("stationary short list rejected")
	}
}

// A stopped list away from its endpoint must never cause another blind swipe.
func TestStationaryListAwayFromEndpointsStops(t *testing.T) {
	img := scrollbarFrame(300, 459)
	if _, err := listTop(img, img); err == nil {
		t.Fatal("stationary top page was retried")
	}
	if _, err := listBottom(img, img, true); err == nil {
		t.Fatal("stationary bottom page was retried")
	}
}

func scrollbarFrame(start, end int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := start; y <= end; y++ {
		for x := 1232; x < 1238; x++ {
			img.SetRGBA(x, y, color.RGBA{220, 220, 220, 255})
		}
	}
	return img
}

func TestLostScrollbarCannotCompleteEitherBoundary(t *testing.T) {
	previous, current := scrollbarFrame(300, 459), scrollbarFrame(0, -1)
	if _, err := listTop(previous, current); err == nil {
		t.Fatal("lost middle scrollbar accepted as top")
	}
	if _, err := listBottom(previous, current, true); err == nil {
		t.Fatal("lost middle scrollbar accepted as bottom")
	}
}

func TestScrollbarEvidenceSurvivesPageReferenceReset(t *testing.T) {
	middle, empty := scrollbarFrame(300, 459), scrollbarFrame(0, -1)
	s := &session{}
	s.resetList(empty)
	if err := s.observeScrollbar(middle); err != nil {
		t.Fatal(err)
	}
	// Top completion clears Previous; later reads replace it. Neither operation
	// may discard scrollbar evidence for this tab.
	s.Previous = nil
	if err := s.observeScrollbar(empty); err == nil {
		t.Fatal("top-to-read transition forgot scrollbar")
	}
	s.Previous = empty
	if err := s.observeScrollbar(empty); err == nil {
		t.Fatal("replacement read frame forgot scrollbar")
	}
	s.resetList(empty)
	if err := s.observeScrollbar(empty); err != nil || s.ScrollbarSeen {
		t.Fatal("new short tab inherited the preceding tab's scrollbar")
	}
	if got, err := listTop(empty, empty); err != nil || got != "done" {
		t.Fatalf("short-list top: %q %v", got, err)
	}
	if got, err := listBottom(empty, empty, true); err != nil || got != "done" {
		t.Fatalf("short-list bottom: %q %v", got, err)
	}
}

func TestVisibleScrollbarEndpointsStillComplete(t *testing.T) {
	top, bottom := scrollbarFrame(260, 420), scrollbarFrame(490, 660)
	if got, err := listTop(nil, top); err != nil || got != "done" {
		t.Fatalf("confirmed top: %q %v", got, err)
	}
	if got, err := listBottom(bottom, bottom, true); err != nil || got != "done" {
		t.Fatalf("confirmed bottom: %q %v", got, err)
	}
}

func TestLaevatainSkillScrollUsesDistinctMaterialContent(t *testing.T) {
	var frames []image.Image
	for _, name := range []string{"LaevatainSkillScrollPrev.png", "LaevatainSkillScrollCur.png"} {
		f, err := os.Open(filepath.Join("core", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		crop, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		frame := image.NewRGBA(image.Rect(0, 0, 1280, 720))
		draw.Draw(frame, image.Rect(420, 250, 1250, 665), crop, crop.Bounds().Min, draw.Src)
		frames = append(frames, frame)
	}
	if got, err := listBottom(frames[0], frames[1], true); err != nil || got != "again" {
		t.Fatalf("visible downward movement rejected: %q %v", got, err)
	}
	if _, err := listBottom(frames[1], frames[0], true); err == nil {
		t.Fatal("reverse movement accepted as downward")
	}
	if got, err := listTop(frames[1], frames[0]); err != nil || got != "done" {
		t.Fatalf("visible upward movement rejected: %q %v", got, err)
	}
}

// A short list has no scrollbar; a cloud toast appearing on one frame must not be
// mistaken for movement (seen on a maxed operator's level tab).
func TestShortListIgnoresTransientOverlay(t *testing.T) {
	prev := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	cur := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := 631; y < 680; y++ {
		for x := 420; x < 760; x++ {
			prev.Set(x, y, color.RGBA{30, 30, 30, 255})
		}
	}
	if got, err := listBottom(prev, cur, true); err != nil || got != "done" {
		t.Fatalf("bottom: %q %v", got, err)
	}
	if got, err := listTop(prev, cur); err != nil || got != "done" {
		t.Fatalf("top: %q %v", got, err)
	}
}
