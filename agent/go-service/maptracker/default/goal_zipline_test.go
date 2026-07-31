// Copyright (c) 2026 MaaEnd Contributors
package maptrackerdefault

import (
	"image"
	"image/color"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
)

func TestZiplineTemplatesDistinguishOwnership(t *testing.T) {
	owned := loadZiplineTemplate(t, "Zipline.png")
	shared := loadZiplineTemplate(t, "SharedZipline.png")

	canvas := image.NewRGBA(image.Rect(0, 0, 220, 80))
	fillZiplineTestBackground(canvas)
	ownedAt := image.Pt(24, 28)
	sharedAt := image.Pt(164, 28)
	drawGreenMaskedTemplate(canvas, owned.Image, ownedAt)
	drawGreenMaskedTemplate(canvas, shared.Image, sharedAt)
	integral := minicv.GetIntegralArray(canvas)

	tests := []struct {
		name       string
		template   *minicv.Template
		threshold  float64
		expectedAt image.Point
		rejectedAt image.Point
	}{
		{
			name:       "owned template",
			template:   owned,
			threshold:  0.5,
			expectedAt: ownedAt,
			rejectedAt: sharedAt,
		},
		{
			name:       "shared template",
			template:   shared,
			threshold:  sharedZiplineThreshold,
			expectedAt: sharedAt,
			rejectedAt: ownedAt,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hits := minicv.MatchTemplateMultiHitWithMask(
				canvas,
				integral,
				test.template.Image,
				test.template.Stats,
				0x00FF00,
				test.threshold,
				16,
			)
			if !hasZiplineHitNear(hits, test.expectedAt) {
				t.Fatalf("expected template match near %v, got %v", test.expectedAt, hits)
			}
			if hasZiplineHitNear(hits, test.rejectedAt) {
				t.Fatalf("template incorrectly matched the other ownership color near %v", test.rejectedAt)
			}
		})
	}
}

func loadZiplineTemplate(t *testing.T, name string) *minicv.Template {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve test source path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", ".."))
	path := filepath.Join(repoRoot, "assets", "resource", "image", "MapTracker", "BigMapIcons", name)
	template, err := minicv.NewTemplateLoaderOfPath(path).Get()
	if err != nil {
		t.Fatalf("failed to load %s: %v", path, err)
	}
	return template
}

func fillZiplineTestBackground(img *image.RGBA) {
	for y := range img.Rect.Dy() {
		for x := range img.Rect.Dx() {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(40 + (x*7+y*3)%80),
				G: uint8(55 + (x*5+y*11)%90),
				B: uint8(45 + (x*13+y*2)%70),
				A: 255,
			})
		}
	}
}

func drawGreenMaskedTemplate(dst *image.RGBA, template *image.RGBA, at image.Point) {
	for y := range template.Rect.Dy() {
		for x := range template.Rect.Dx() {
			pixel := template.RGBAAt(x, y)
			if pixel.R == 0 && pixel.G == 255 && pixel.B == 0 {
				continue
			}
			dst.SetRGBA(at.X+x, at.Y+y, pixel)
		}
	}
}

func hasZiplineHitNear(hits []minicv.MatchTemplateHit, target image.Point) bool {
	for _, hit := range hits {
		if hit.X >= float64(target.X-1) && hit.X <= float64(target.X+1) &&
			hit.Y >= float64(target.Y-1) && hit.Y <= float64(target.Y+1) {
			return true
		}
	}
	return false
}
