// Copyright (c) 2026 Harry Huang
package maptracker

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// MapData represents a preloaded map image with its name and precomputed integral images
type MapData struct {
	Name     string
	Img      *image.RGBA
	Integral *IntegralImage
}

// InferResult represents the result of map tracking inference
type InferResult struct {
	MapName   string  `json:"mapName"`   // Map name
	X         int     `json:"x"`         // X coordinate on the map
	Y         int     `json:"y"`         // Y coordinate on the map
	Rot       int     `json:"rot"`       // Rotation angle (0-359 degrees)
	LocConf   float64 `json:"locConf"`   // Location confidence
	RotConf   float64 `json:"rotConf"`   // Rotation confidence
	LocTimeMs int64   `json:"locTimeMs"` // Location inference time in ms
	RotTimeMs int64   `json:"rotTimeMs"` // Rotation inference time in ms
}

// Infer is the custom recognition component for map tracking
type Infer struct {
	// Cache for preloaded resources
	mapsOnce    sync.Once
	pointerOnce sync.Once
	maps        []MapData
	pointer     image.Image
	mapsErr     error
	pointerErr  error

	// Cache for scaled maps
	scaledMu    sync.Mutex
	scaledScale float64
	scaledMaps  []MapData
}

var (
	_ maa.CustomRecognitionRunner = &Infer{}
)

// Run implements maa.CustomRecognitionRunner
func (i *Infer) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// Parse custom recognition parameters
	precision := 0.4
	threshold := 0.5
	if arg.CustomRecognitionParam != "" {
		var params struct {
			Precision float64 `json:"precision"`
			Threshold float64 `json:"threshold"`
		}
		if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err == nil {
			if params.Precision > 0.0 && params.Precision <= 1.0 {
				precision = params.Precision
			}
			if params.Threshold >= 0.0 && params.Threshold < 1.0 {
				threshold = params.Threshold
			}
		}
	}

	locScale := precision
	var rotStep int
	if precision < 0.3 {
		rotStep = 12
	} else if precision < 0.6 {
		rotStep = 6
	} else {
		rotStep = 3
	}

	// Initialize resources on first run
	i.initMaps(ctx)
	i.initPointer(ctx)

	// Check for initialization errors
	if i.mapsErr != nil {
		log.Error().Err(i.mapsErr).Msg("Failed to initialize maps")
		return nil, false
	}
	if i.pointerErr != nil {
		log.Warn().Err(i.pointerErr).Msg("Failed to initialize pointer, rotation will be skipped")
	}

	// Perform location inference
	t0 := time.Now()
	locX, locY, locConf, mapName := i.inferLocation(arg.Img, locScale)
	locTime := time.Since(t0)

	// Perform rotation inference (if pointer is loaded)
	rot, rotConf := 0, 0.0
	var rotTime time.Duration
	if i.pointer != nil {
		t1 := time.Now()
		rot, rotConf = i.inferRotation(arg.Img, rotStep)
		rotTime = time.Since(t1)
	}

	// Build result
	result := InferResult{
		MapName:   mapName,
		X:         locX,
		Y:         locY,
		Rot:       rot,
		LocConf:   locConf,
		RotConf:   rotConf,
		LocTimeMs: locTime.Milliseconds(),
		RotTimeMs: rotTime.Milliseconds(),
	}

	// Determine if recognition hit
	hit := locConf > threshold && rotConf > threshold

	// Serialize result to JSON
	detailJSON, err := json.Marshal(result)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal result")
		return nil, false
	}

	log.Info().
		Str("mapName", mapName).
		Dur("locTime", locTime).
		Dur("rotTime", rotTime).
		Int("x", locX).
		Int("y", locY).
		Int("rot", rot).
		Float64("locConf", locConf).
		Float64("rotConf", rotConf).
		Bool("hit", hit).
		Msg("Map tracking inference completed")

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, hit
}

// initMaps initializes the map cache (thread-safe, runs once)
func (i *Infer) initMaps(ctx *maa.Context) {
	i.mapsOnce.Do(func() {
		i.maps, i.mapsErr = i.loadMaps(ctx)
		if i.mapsErr != nil {
			log.Error().Err(i.mapsErr).Msg("Failed to load maps")
		} else {
			log.Info().Int("count", len(i.maps)).Msg("Maps loaded successfully")
		}
	})
}

// initPointer initializes the pointer template cache (thread-safe, runs once)
func (i *Infer) initPointer(ctx *maa.Context) {
	i.pointerOnce.Do(func() {
		i.pointer, i.pointerErr = i.loadPointer(ctx)
		if i.pointerErr != nil {
			log.Error().Err(i.pointerErr).Msg("Failed to load pointer template")
		} else {
			log.Info().Msg("Pointer template loaded successfully")
		}
	})
}

// loadMaps loads all map images from the resource directory
func (i *Infer) loadMaps(ctx *maa.Context) ([]MapData, error) {
	// Find map directory using search strategy
	mapDir := findResource(MAP_DIR)
	if mapDir == "" {
		return nil, fmt.Errorf("map directory not found (searched in cache and standard locations)")
	}

	// Read directory entries
	entries, err := os.ReadDir(mapDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read map directory: %w", err)
	}

	// Load all PNG files
	maps := make([]MapData, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if !strings.HasSuffix(filename, ".png") {
			continue
		}

		// Load image
		imgPath := filepath.Join(mapDir, filename)
		file, err := os.Open(imgPath)
		if err != nil {
			log.Warn().Err(err).Str("path", imgPath).Msg("Failed to open map image")
			continue
		}

		img, _, err := image.Decode(file)
		file.Close()
		if err != nil {
			log.Warn().Err(err).Str("path", imgPath).Msg("Failed to decode map image")
			continue
		}

		imgRGBA := ToRGBA(img)

		// Precompute integral image
		integral := NewIntegralImage(imgRGBA)

		// Extract map name (remove "_merged.png" suffix)
		name := strings.TrimSuffix(filename, "_merged.png")

		maps = append(maps, MapData{
			Name:     name,
			Img:      imgRGBA,
			Integral: integral,
		})

		log.Debug().Str("name", name).Str("path", imgPath).Msg("Loaded map image")
	}

	if len(maps) == 0 {
		return nil, fmt.Errorf("no valid map images found in %s", mapDir)
	}

	return maps, nil
}

// loadPointer loads the pointer template image
func (i *Infer) loadPointer(ctx *maa.Context) (image.Image, error) {
	// Find pointer template using search strategy
	pointerPath := findResource(POINTER_PATH)
	if pointerPath == "" {
		return nil, fmt.Errorf("pointer template not found (searched in cache and standard locations)")
	}

	// Load image
	file, err := os.Open(pointerPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open pointer template: %w", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("failed to decode pointer template: %w", err)
	}

	log.Debug().Str("path", pointerPath).Msg("Loaded pointer template")

	return img, nil
}

// inferLocation infers the player's location on the map
// Returns (x, y, confidence, mapName)
func (i *Infer) inferLocation(screenImg image.Image, locScale float64) (int, int, float64, string) {
	// Crop mini-map area from screen
	miniMap := cropArea(screenImg, LOC_CENTER_X, LOC_CENTER_Y, LOC_RADIUS)

	// Scale mini-map
	if locScale != 1.0 {
		miniMap = scaleImage(miniMap, locScale)
	}

	miniMapRGBA := ToRGBA(miniMap)

	miniMapBounds := miniMap.Bounds()
	miniMapW, miniMapH := miniMapBounds.Dx(), miniMapBounds.Dy()

	// Precompute needle (minimap) statistics for all matches
	miniStats := GetNeedleStats(miniMapRGBA)
	if miniStats.Dn < 1e-6 {
		return 0, 0, 0.0, "None"
	}

	// Match against all maps
	bestVal := -1.0
	bestX, bestY := 0, 0
	bestMapName := "None"

	// Use cached scaled maps
	scaledMaps := i.getScaledMaps(locScale)

	for _, mapData := range scaledMaps {
		// Get valid area for this map and scale it
		validRect := image.Rectangle{}
		if rect, ok := VALID_RECT_MAP[mapData.Name]; ok {
			validRect = image.Rect(
				int(float64(rect.Min.X)*locScale),
				int(float64(rect.Min.Y)*locScale),
				int(float64(rect.Max.X)*locScale),
				int(float64(rect.Max.Y)*locScale),
			)
		}

		// Perform template matching (using optimized version with precomputed stats and valid area)
		matchX, matchY, matchVal := MatchTemplateOptimized(mapData.Img, mapData.Integral, miniMapRGBA, miniStats, validRect)

		if matchVal > bestVal {
			bestVal = matchVal
			// Convert top-left corner to center position
			// Then convert back to original scale
			bestX = int(float64(matchX+miniMapW/2) / locScale)
			bestY = int(float64(matchY+miniMapH/2) / locScale)
			bestMapName = mapData.Name
		}
	}

	return bestX, bestY, bestVal, bestMapName
}

// getScaledMaps returns cached scaled maps or recomputes them
func (i *Infer) getScaledMaps(scale float64) []MapData {
	i.scaledMu.Lock()
	defer i.scaledMu.Unlock()

	if i.scaledScale == scale && len(i.scaledMaps) > 0 {
		return i.scaledMaps
	}

	log.Info().Float64("scale", scale).Msg("Recomputing scaled maps cache")
	newScaled := make([]MapData, 0, len(i.maps))
	for _, m := range i.maps {
		sImg := scaleImage(m.Img, scale)
		sRGBA := ToRGBA(sImg)
		newScaled = append(newScaled, MapData{
			Name:     m.Name,
			Img:      sRGBA,
			Integral: NewIntegralImage(sRGBA),
		})
	}
	i.scaledScale = scale
	i.scaledMaps = newScaled
	return i.scaledMaps
}

// inferRotation infers the player's rotation angle
// Returns (angle, confidence)
func (i *Infer) inferRotation(screenImg image.Image, rotStep int) (int, float64) {
	if i.pointer == nil {
		return 0, 0.0
	}

	// Crop pointer area from screen
	patch := cropArea(screenImg, ROT_CENTER_X, ROT_CENTER_Y, ROT_RADIUS)
	patchRGBA := ToRGBA(patch)

	// Precompute needle (pointer) statistics
	pointerRGBA := ToRGBA(i.pointer)
	pointerStats := GetNeedleStats(pointerRGBA)
	if pointerStats.Dn < 1e-6 {
		return 0, 0.0
	}

	// Try all rotation angles
	bestAngle := 0
	maxVal := -1.0

	for angle := 0; angle < 360; angle += rotStep {
		// Rotate the patch
		rotatedRGBA := rotateImageRGBA(patchRGBA, float64(angle))

		// Match against pointer template
		integral := NewIntegralImage(rotatedRGBA)
		_, _, matchVal := MatchTemplateOptimized(rotatedRGBA, integral, pointerRGBA, pointerStats, image.Rectangle{})

		if matchVal > maxVal {
			maxVal = matchVal
			bestAngle = angle
		}
	}

	return bestAngle, maxVal
}
