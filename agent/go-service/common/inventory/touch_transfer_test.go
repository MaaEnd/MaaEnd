package inventory

import (
	"errors"
	"image"
	"reflect"
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type fakeTouchRunner struct {
	events        []string
	waits         []time.Duration
	boxes         []maa.Rect
	images        []image.Image
	frame         image.Image
	hitNode       string
	failNode      string
	nilNode       string
	missFrames    int
	frames        int
	stopAfter     string
	stopped       bool
	failRelease   int32
	screenshotErr bool
}

func (r *fakeTouchRunner) record(event string) {
	r.events = append(r.events, event)
	if event == r.stopAfter {
		r.stopped = true
	}
}

func (r *fakeTouchRunner) RunAction(node string, box maa.Rect, _ string, _ ...any) (*maa.ActionDetail, error) {
	r.record(node)
	r.boxes = append(r.boxes, box)
	return &maa.ActionDetail{Success: node != r.failNode}, nil
}

func (r *fakeTouchRunner) RunRecognition(node string, img image.Image, _ ...any) (*maa.RecognitionDetail, error) {
	r.record(node)
	r.images = append(r.images, img)
	if node == r.failNode {
		return nil, errors.New("recognition failed")
	}
	if node == r.nilNode {
		return nil, nil
	}
	return &maa.RecognitionDetail{Hit: node == r.hitNode && r.frames > r.missFrames, Box: maa.Rect{180, 200, 30, 30}}, nil
}

func (r *fakeTouchRunner) screenshot() (image.Image, error) {
	r.record("screenshot")
	r.frames++
	if r.screenshotErr {
		return nil, errors.New("screencap failed")
	}
	return r.frame, nil
}

func (r *fakeTouchRunner) stopping() bool { return r.stopped }

func (r *fakeTouchRunner) release(contact int32) bool {
	if contact == sourceContact {
		r.record("release_source")
	} else {
		r.record("release_button")
	}
	return contact != r.failRelease
}

func (r *fakeTouchRunner) wait(duration time.Duration) bool {
	r.record("wait_before_release_source")
	r.waits = append(r.waits, duration)
	return !r.stopped
}

func TestTouchTransferLifecycle(t *testing.T) {
	prefix := "__InventoryTransferStackButton"
	left, right := prefix+"Left", prefix+"Right"
	source := maa.Rect{400, 300, 30, 30}
	for _, test := range []struct {
		name    string
		setup   func(*fakeTouchRunner)
		timeout time.Duration
		success bool
		want    []string
	}{
		{"left_hit", func(r *fakeTouchRunner) { r.hitNode = left }, time.Second, true,
			[]string{sourceTouchDownNode, "screenshot", left, buttonTouchDownNode, "release_button", "wait_before_release_source", "release_source"}},
		{"right_hit", func(r *fakeTouchRunner) { r.hitNode = right }, time.Second, true,
			[]string{sourceTouchDownNode, "screenshot", left, right, buttonTouchDownNode, "release_button", "wait_before_release_source", "release_source"}},
		{"menu_appears_next_frame", func(r *fakeTouchRunner) { r.hitNode = left; r.missFrames = 1 }, time.Second, true,
			[]string{sourceTouchDownNode, "screenshot", left, right, "screenshot", left, buttonTouchDownNode, "release_button", "wait_before_release_source", "release_source"}},
		{"timeout", func(r *fakeTouchRunner) {}, 0, false,
			[]string{sourceTouchDownNode, "release_source"}},
		{"stop_before_input", func(r *fakeTouchRunner) { r.stopped = true }, time.Second, false, nil},
		{"source_down_failed", func(r *fakeTouchRunner) { r.failNode = sourceTouchDownNode }, time.Second, false,
			[]string{sourceTouchDownNode, "release_source"}},
		{"screenshot_failed", func(r *fakeTouchRunner) { r.screenshotErr = true }, time.Second, false,
			[]string{sourceTouchDownNode, "screenshot", "release_source"}},
		{"nil_image", func(r *fakeTouchRunner) { r.frame = nil }, time.Second, false,
			[]string{sourceTouchDownNode, "screenshot", "release_source"}},
		{"recognition_error_is_not_miss", func(r *fakeTouchRunner) { r.failNode = left }, time.Second, false,
			[]string{sourceTouchDownNode, "screenshot", left, "release_source"}},
		{"nil_recognition", func(r *fakeTouchRunner) { r.nilNode = left }, time.Second, false,
			[]string{sourceTouchDownNode, "screenshot", left, "release_source"}},
		{"stop_after_source_down", func(r *fakeTouchRunner) { r.stopAfter = sourceTouchDownNode }, time.Second, false,
			[]string{sourceTouchDownNode, "release_source"}},
		{"stop_after_screenshot", func(r *fakeTouchRunner) { r.stopAfter = "screenshot" }, time.Second, false,
			[]string{sourceTouchDownNode, "screenshot", "release_source"}},
		{"stop_after_left_miss", func(r *fakeTouchRunner) { r.stopAfter = left }, time.Second, false,
			[]string{sourceTouchDownNode, "screenshot", left, "release_source"}},
		{"stop_after_hit", func(r *fakeTouchRunner) { r.hitNode = left; r.stopAfter = left }, time.Second, false,
			[]string{sourceTouchDownNode, "screenshot", left, "release_source"}},
		{"button_down_failed", func(r *fakeTouchRunner) { r.hitNode = left; r.failNode = buttonTouchDownNode }, time.Second, false,
			[]string{sourceTouchDownNode, "screenshot", left, buttonTouchDownNode, "release_button", "wait_before_release_source", "release_source"}},
		{"stop_after_button_down", func(r *fakeTouchRunner) { r.hitNode = left; r.stopAfter = buttonTouchDownNode }, time.Second, false,
			[]string{sourceTouchDownNode, "screenshot", left, buttonTouchDownNode, "release_button", "wait_before_release_source", "release_source"}},
		{"stop_during_release", func(r *fakeTouchRunner) { r.hitNode = left; r.stopAfter = "release_button" }, time.Second, false,
			[]string{sourceTouchDownNode, "screenshot", left, buttonTouchDownNode, "release_button", "wait_before_release_source", "release_source"}},
		{"button_release_failed", func(r *fakeTouchRunner) { r.hitNode = left; r.failRelease = buttonContact }, time.Second, false,
			[]string{sourceTouchDownNode, "screenshot", left, buttonTouchDownNode, "release_button", "wait_before_release_source", "release_source"}},
		{"source_release_failed", func(r *fakeTouchRunner) { r.hitNode = left; r.failRelease = sourceContact }, time.Second, false,
			[]string{sourceTouchDownNode, "screenshot", left, buttonTouchDownNode, "release_button", "wait_before_release_source", "release_source"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeTouchRunner{frame: image.NewRGBA(image.Rect(0, 0, 1280, 720)), failRelease: -1}
			test.setup(runner)
			if got := runTouchTransfer(runner, prefix, source, test.timeout); got != test.success {
				t.Fatalf("success = %v, want %v", got, test.success)
			}
			if !reflect.DeepEqual(runner.events, test.want) {
				t.Fatalf("events = %v, want %v", runner.events, test.want)
			}
			if len(runner.waits) > 0 && !reflect.DeepEqual(runner.waits, []time.Duration{sourceReleaseDelay}) {
				t.Fatalf("waits = %v, want [%v]", runner.waits, sourceReleaseDelay)
			}
			if len(runner.boxes) > 0 && runner.boxes[0] != source {
				t.Fatal("source target was changed")
			}
			if len(runner.boxes) > 1 && runner.boxes[1] != (maa.Rect{180, 200, 30, 30}) {
				t.Fatal("button target did not come from recognition")
			}
			for _, img := range runner.images {
				if img != runner.frame {
					t.Fatal("left and right recognition must use the captured frame")
				}
			}
		})
	}
}

func TestParseTouchTransferMode(t *testing.T) {
	for mode, want := range map[string]string{"all": "All", "stack": "Stack", "half": "Half"} {
		got, err := parseTouchTransferMode(`{"mode":"` + mode + `"}`)
		if err != nil || got != "__InventoryTransfer"+want+"Button" {
			t.Fatalf("mode %s: prefix = %q, err = %v", mode, got, err)
		}
	}
	for _, raw := range []string{"", "{", "{}", `{"mode":"ALL"}`, `{"mode":"unknown"}`} {
		if _, err := parseTouchTransferMode(raw); err == nil {
			t.Fatalf("invalid mode accepted: %s", raw)
		}
	}
}
