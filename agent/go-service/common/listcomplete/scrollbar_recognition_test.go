package listcomplete

import (
	"encoding/json"
	"image"
	"image/color"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestScrollbarRecognitionReturnsAbsoluteBoxAndRelativeDetail(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 30, 50))
	paintWhiteRowsAtX(img, 12, 15, 22)
	arg := &maa.CustomRecognitionArg{
		Img: img,
		Roi: maa.Rect{10, 10, 5, 30},
	}

	result, matched := (&ScrollbarRecognition{}).Run(nil, arg)
	if !matched || result == nil {
		t.Fatal("expected scrollbar recognition to match")
	}
	if result.Box != (maa.Rect{10, 15, 5, 8}) {
		t.Fatalf("box = %v, want [10,15,5,8]", result.Box)
	}

	var detail struct {
		Top    int `json:"top"`
		Bottom int `json:"bottom"`
		Length int `json:"length"`
	}
	if err := json.Unmarshal([]byte(result.Detail), &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detail.Top != 5 || detail.Bottom != 12 || detail.Length != 8 {
		t.Fatalf("detail = %+v, want top=5 bottom=12 length=8", detail)
	}
}

func TestScrollbarRecognitionMissesWithoutValidSegment(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 10, 20))
	result, matched := (&ScrollbarRecognition{}).Run(nil, &maa.CustomRecognitionArg{
		Img: img,
		Roi: maa.Rect{0, 0, 5, 20},
	})
	if matched || result != nil {
		t.Fatalf("result = %+v, matched = %v, want miss", result, matched)
	}
}

func TestParseScrollbarParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{name: "empty", raw: "", want: defaultPositionTolerance},
		{name: "empty object", raw: `{}`, want: defaultPositionTolerance},
		{name: "custom", raw: `{"position_tolerance":4}`, want: 4},
		{name: "exact match", raw: `{"position_tolerance":0}`, want: 0},
		{name: "negative", raw: `{"position_tolerance":-1}`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params, err := parseScrollbarParams(test.raw)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected parameter validation error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseScrollbarParams() error = %v", err)
			}
			if params.PositionTolerance != test.want {
				t.Fatalf("position_tolerance = %d, want %d", params.PositionTolerance, test.want)
			}
		})
	}
}

func TestDetectScrollbarSegmentFillsShortGapAndSelectsLongest(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 5, 40))
	paintWhiteRows(img, 2, 7)
	paintWhiteRows(img, 15, 20)
	paintWhiteRows(img, 23, 29)

	segment, ok := detectScrollbarSegment(img, image.Rect(0, 0, 5, 40))
	if !ok {
		t.Fatal("expected scrollbar segment")
	}
	if segment != (scrollbarSegment{Top: 15, Bottom: 29}) {
		t.Fatalf("segment = %+v, want {Top:15 Bottom:29}", segment)
	}
}

func TestDetectScrollbarSegmentRejectsShortHighlight(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 5, 20))
	paintWhiteRows(img, 4, 7)

	if _, ok := detectScrollbarSegment(img, image.Rect(0, 0, 5, 20)); ok {
		t.Fatal("short highlight must not be treated as scrollbar")
	}
}

func TestScrollbarSegmentsMatchWithinTolerance(t *testing.T) {
	t.Parallel()

	previous := scrollbarSegment{Top: 10, Bottom: 32}
	if !scrollbarSegmentsMatch(previous, scrollbarSegment{Top: 12, Bottom: 30}, 2) {
		t.Fatal("both boundaries within tolerance should match")
	}
	if scrollbarSegmentsMatch(previous, scrollbarSegment{Top: 13, Bottom: 31}, 2) {
		t.Fatal("top boundary outside tolerance must not match")
	}
	if scrollbarSegmentsMatch(previous, scrollbarSegment{Top: 11, Bottom: 35}, 2) {
		t.Fatal("bottom boundary outside tolerance must not match")
	}
}

func TestScrollbarPositionRoundTrip(t *testing.T) {
	t.Parallel()

	const node = "ScrollbarBoundary"
	store := newFakeNodeStore()

	if _, ready, err := loadScrollbarPosition(store, node); err != nil || ready {
		t.Fatalf("empty state: ready = %v, error = %v", ready, err)
	}

	want := scrollbarSegment{Top: 9, Bottom: 31}
	if err := saveScrollbarPosition(store, node, want); err != nil {
		t.Fatalf("saveScrollbarPosition() error = %v", err)
	}
	got, ready, err := loadScrollbarPosition(store, node)
	if err != nil {
		t.Fatalf("loadScrollbarPosition() error = %v", err)
	}
	if !ready || got != want {
		t.Fatalf("position = %+v, ready = %v, want %+v and ready", got, ready, want)
	}
}

func TestMissingScrollbarCompletesAfterConsecutiveConfirmation(t *testing.T) {
	t.Parallel()

	const node = "ScrollbarBoundary"
	store := newFakeNodeStore()

	complete, err := observeMissingScrollbar(store, node)
	if err != nil {
		t.Fatalf("first missing observation: %v", err)
	}
	if complete {
		t.Fatal("first missing observation must request one confirmation scroll")
	}

	complete, err = observeMissingScrollbar(store, node)
	if err != nil {
		t.Fatalf("second missing observation: %v", err)
	}
	if !complete {
		t.Fatal("second consecutive missing observation must treat the list as not scrollable")
	}

	if err := saveReady(store, node, false); err != nil {
		t.Fatalf("reset scan state: %v", err)
	}
	complete, err = observeMissingScrollbar(store, node)
	if err != nil {
		t.Fatalf("first missing observation after scan reset: %v", err)
	}
	if complete {
		t.Fatal("attach.ready=false must start a fresh missing confirmation cycle")
	}

	if err := saveScrollbarPosition(store, node, scrollbarSegment{Top: 8, Bottom: 30}); err != nil {
		t.Fatalf("save valid scrollbar position: %v", err)
	}
	complete, err = observeMissingScrollbar(store, node)
	if err != nil {
		t.Fatalf("missing observation after valid scrollbar: %v", err)
	}
	if complete {
		t.Fatal("a valid scrollbar observation must reset missing confirmation state")
	}
}

func TestUnchangedScrollbarObservationResetsMissingConfirmation(t *testing.T) {
	t.Parallel()

	const node = "ScrollbarBoundary"
	store := newFakeNodeStore()
	segment := scrollbarSegment{Top: 8, Bottom: 30}

	_, ready, complete, err := observeScrollbar(store, node, segment, defaultPositionTolerance)
	if err != nil {
		t.Fatalf("first valid scrollbar observation: %v", err)
	}
	if ready || complete {
		t.Fatal("first valid scrollbar observation must record the position without completing")
	}

	complete, err = observeMissingScrollbar(store, node)
	if err != nil {
		t.Fatalf("missing observation after valid scrollbar: %v", err)
	}
	if complete {
		t.Fatal("first missing observation must request confirmation")
	}

	_, ready, complete, err = observeScrollbar(store, node, segment, defaultPositionTolerance)
	if err != nil {
		t.Fatalf("unchanged valid scrollbar observation: %v", err)
	}
	if !ready || !complete {
		t.Fatal("unchanged valid scrollbar observation must report the list complete")
	}

	complete, err = observeMissingScrollbar(store, node)
	if err != nil {
		t.Fatalf("missing observation after unchanged valid scrollbar: %v", err)
	}
	if complete {
		t.Fatal("unchanged valid scrollbar observation must reset missing confirmation state")
	}
}

func paintWhiteRows(img *image.RGBA, top, bottom int) {
	paintWhiteRowsAtX(img, 2, top, bottom)
}

func paintWhiteRowsAtX(img *image.RGBA, x, top, bottom int) {
	for y := top; y <= bottom; y++ {
		img.Set(x, y, color.RGBA{R: 230, G: 230, B: 230, A: 255})
	}
}
