package listcomplete

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestParseParamsDefaults(t *testing.T) {
	t.Parallel()

	p, err := parseParams("")
	if err != nil {
		t.Fatalf("empty param: %v", err)
	}
	if p.Threshold != defaultThreshold {
		t.Fatalf("threshold = %v, want %v", p.Threshold, defaultThreshold)
	}

	p, err = parseParams(`{}`)
	if err != nil {
		t.Fatalf("empty object: %v", err)
	}
	if p.Threshold != defaultThreshold {
		t.Fatalf("threshold = %v, want %v", p.Threshold, defaultThreshold)
	}

	p, err = parseParams(`{"threshold":0.8}`)
	if err != nil {
		t.Fatalf("custom param: %v", err)
	}
	if p.Threshold != 0.8 {
		t.Fatalf("threshold = %v, want 0.8", p.Threshold)
	}

	p, err = parseParams(`{"threshold":0}`)
	if err != nil {
		t.Fatalf("zero threshold should fall back to default: %v", err)
	}
	if p.Threshold != defaultThreshold {
		t.Fatalf("threshold = %v, want default %v", p.Threshold, defaultThreshold)
	}

	if _, err := parseParams(`{"threshold":1.5}`); err == nil {
		t.Fatal("expected error for threshold > 1")
	}
}

func TestResolveROIFullscreenAndClip(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	full, err := resolveROI(img, maa.Rect{})
	if err != nil {
		t.Fatalf("empty native roi: %v", err)
	}
	if full != (maa.Rect{0, 0, 1280, 720}) {
		t.Fatalf("fullscreen = %v, want [0,0,1280,720]", full)
	}

	clipped, err := resolveROI(img, maa.Rect{1200, 700, 200, 100})
	if err != nil {
		t.Fatalf("clip: %v", err)
	}
	if clipped != (maa.Rect{1200, 700, 80, 20}) {
		t.Fatalf("clipped = %v, want [1200,700,80,20]", clipped)
	}

	if _, err := resolveROI(img, maa.Rect{2000, 2000, 10, 10}); err == nil {
		t.Fatal("expected error for roi outside image")
	}
}

func TestResolveROIEmptyImage(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 0, 0))
	if _, err := resolveROI(img, maa.Rect{}); err == nil {
		t.Fatal("expected error for empty image bounds")
	}
}

func TestCropROI(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 100, 80))
	img.Set(10, 20, color.RGBA{R: 255, A: 255})
	cropped := cropROI(img, maa.Rect{5, 15, 20, 30})
	if cropped.Bounds().Dx() != 20 || cropped.Bounds().Dy() != 30 {
		t.Fatalf("crop size = %v, want 20x30", cropped.Bounds())
	}
	r, _, _, _ := cropped.At(5, 5).RGBA()
	if r>>8 != 255 {
		t.Fatalf("expected red pixel preserved at local (5,5)")
	}
}

func TestReadyAttachRoundTrip(t *testing.T) {
	t.Parallel()

	const node = "ScrollList"
	store := newFakeNodeStore()

	ready, err := loadReady(store, node)
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if ready {
		t.Fatal("empty attach should be not ready")
	}

	if err := saveReady(store, node, true); err != nil {
		t.Fatalf("save ready: %v", err)
	}
	ready, err = loadReady(store, node)
	if err != nil {
		t.Fatalf("load ready: %v", err)
	}
	if !ready {
		t.Fatal("expected ready=true after save")
	}

	if err := saveReady(store, node, false); err != nil {
		t.Fatalf("save reset: %v", err)
	}
	ready, err = loadReady(store, node)
	if err != nil {
		t.Fatalf("load reset: %v", err)
	}
	if ready {
		t.Fatal("expected ready=false after reset")
	}
}

func TestMarshalDetail(t *testing.T) {
	t.Parallel()

	raw := marshalDetail(map[string]any{"first_run": true, "score": 0.9})
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if payload["first_run"] != true {
		t.Fatalf("first_run = %v, want true", payload["first_run"])
	}
}

type fakeNodeStore struct {
	nodes map[string]map[string]any
}

func newFakeNodeStore() *fakeNodeStore {
	return &fakeNodeStore{nodes: make(map[string]map[string]any)}
}

func (f *fakeNodeStore) GetNodeJSON(nodeName string) (string, error) {
	node, ok := f.nodes[nodeName]
	if !ok {
		return `{}`, nil
	}
	raw, err := json.Marshal(node)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (f *fakeNodeStore) OverridePipeline(pipelineOverride any) error {
	overrideMap, ok := pipelineOverride.(map[string]any)
	if !ok {
		return fmt.Errorf("pipeline override must be map[string]any, got %T", pipelineOverride)
	}
	for name, rawPatch := range overrideMap {
		patch, ok := rawPatch.(map[string]any)
		if !ok {
			return fmt.Errorf("override for %s must be object", name)
		}
		node := f.nodes[name]
		if node == nil {
			node = make(map[string]any)
			f.nodes[name] = node
		}
		for key, value := range patch {
			node[key] = value
		}
	}
	return nil
}
