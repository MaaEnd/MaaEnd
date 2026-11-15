package ims

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestIcon writes a transparent gradient PNG of the given size.
func writeTestIcon(t *testing.T, size int) string {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 0x40, A: 0xFF})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test icon: %v", err)
	}
	path := filepath.Join(t.TempDir(), "icon.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write test icon: %v", err)
	}
	return path
}

func TestFocusIconDataURLDownscales(t *testing.T) {
	dataURL := focusIconDataURL(writeTestIcon(t, 128), focusItemIconSizePx)

	if !strings.HasPrefix(dataURL, focusIconDataURLPrefix) {
		t.Fatalf("data URL = %q, want prefix %q", dataURL, focusIconDataURLPrefix)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, focusIconDataURLPrefix))
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	if got, want := img.Bounds().Size(), image.Pt(16, 16); got != want {
		t.Fatalf("icon size = %v, want %v", got, want)
	}
	// The source data URL for a 128×128 PNG is tens of KB; keep the encoded
	// 16×16 icon well below that so the client log cache stays small.
	if len(dataURL) > 4096 {
		t.Fatalf("data URL is %d bytes, icon does not look downscaled", len(dataURL))
	}
}

func TestFocusIconDataURLMissingFile(t *testing.T) {
	if got := focusIconDataURL(filepath.Join(t.TempDir(), "missing.png"), focusItemIconSizePx); got != "" {
		t.Fatalf("missing icon = %q, want empty", got)
	}
}

// TestItemIconSrcUsesCatalogIcon covers the catalog → rarity/iconId → PNG path
// end to end, including a 128×128 and a 256×256 source icon. It resolves the
// asset under the repository checkout because the test working directory is not
// an install root; itemIconSrc itself only adds resource.FindResource on top.
func TestItemIconSrcUsesCatalogIcon(t *testing.T) {
	withRecognitionItemsPath(t, filepath.Join(
		"..", "..", "..", "assets", "data", "IconRecognition", "recognition_items.json",
	))

	for _, itemID := range []string{"item_byproduct_potato_1", "item_weekraid_ore_5_3"} {
		relPath := itemIconResourcePath(itemID)
		if relPath == "" {
			t.Fatalf("%s: catalog icon path is empty", itemID)
		}
		assetPath := filepath.Join("..", "..", "..", "assets", filepath.FromSlash(relPath))
		if _, err := os.Stat(assetPath); err != nil {
			t.Fatalf("%s: icon asset %s: %v", itemID, relPath, err)
		}

		dataURL := focusIconDataURL(assetPath, focusItemIconSizePx)
		if !strings.HasPrefix(dataURL, focusIconDataURLPrefix) {
			t.Fatalf("%s: icon = %q, want data URL", itemID, dataURL)
		}
		if len(dataURL) > 4096 {
			t.Fatalf("%s: data URL is %d bytes, icon does not look downscaled", itemID, len(dataURL))
		}
	}
}

func TestItemIconSrcWithoutItemID(t *testing.T) {
	if got := itemIconSrc("   "); got != "" {
		t.Fatalf("blank item ID icon = %q, want empty", got)
	}
}
