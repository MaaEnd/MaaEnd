package mapservice

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

// SaveDebugImage 保存带时间戳的调试图像。
func SaveDebugImage(outputDir, name string, img image.Image) {
	if outputDir == "" {
		outputDir = "."
	}
	_ = os.MkdirAll(outputDir, 0755)
	path := filepath.Join(outputDir, fmt.Sprintf("%s_%d.png", name, time.Now().UnixMilli()))
	f, _ := os.Create(path)
	if f != nil {
		png.Encode(f, img)
		f.Close()
	}
}

func (m *MapLocator) saveDebugVisualization(pos *MapPosition, w, h int) {
	// Identify Zone
	zoneImg, ok := m.zones[pos.ZoneID]
	if !ok {
		log.Error().Str("zone", pos.ZoneID).Msg("Debug: Zone not found")
		return
	}

	// Define Context Rect (e.g. 512x512 around match)
	const ContextSize = 512
	cx, cy := int(pos.X), int(pos.Y)

	ctxRect := image.Rect(
		cx-ContextSize/2, cy-ContextSize/2,
		cx+ContextSize/2, cy+ContextSize/2,
	)

	// Copy from full map
	// Use Intersect to strictly handle edges
	safeRect := ctxRect.Intersect(zoneImg.Bounds())
	// Base image
	debugImg := copySubImage(zoneImg, safeRect)
	// Note: copySubImage returns image with (0,0) origin.
	// Translate match coordinates to this new local space.

	// Local Match Center
	lcx := cx - safeRect.Min.X
	lcy := cy - safeRect.Min.Y

	// Draw Red Box
	// Box Rect relative to debugImg
	boxMinX := lcx - w/2
	boxMinY := lcy - h/2
	boxMaxX := lcx + w/2
	boxMaxY := lcy + h/2

	// Draw lines
	col := color.RGBA{255, 0, 0, 255}
	thickness := 2

	// Top
	drawRect(debugImg, boxMinX, boxMinY, boxMaxX, boxMinY+thickness, col)
	// Bottom
	drawRect(debugImg, boxMinX, boxMaxY-thickness, boxMaxX, boxMaxY, col)
	// Left
	drawRect(debugImg, boxMinX, boxMinY, boxMinX+thickness, boxMaxY, col)
	// Right
	drawRect(debugImg, boxMaxX-thickness, boxMinY, boxMaxX, boxMaxY, col)

	// Save
	m.saveDebugImage(fmt.Sprintf("result_%s", pos.ZoneID), debugImg)
}
