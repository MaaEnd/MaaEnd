// Copyright (c) 2026 Harry Huang
package maptracker

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

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

// InferParam represents the parameters for map tracking inference
type InferParam struct {
	MapNameRegex string  `json:"map_name_regex"` // Regex to filter map names
	Precision    float64 `json:"precision"`      // Matching precision (0.0, 1.0]
	Threshold    float64 `json:"threshold"`      // Confidence threshold [0.0, 1.0)
}

// MapData represents a preloaded map image
type MapData struct {
	Name     string
	Img      *image.RGBA
	Integral *IntegralImage
	OffsetX  int
	OffsetY  int
}

// Infer is the custom recognition component for map tracking
type Infer struct {
	// Cache for preloaded resources
	mapsOnce    sync.Once
	pointerOnce sync.Once
	maps        []MapData
	pointer     *image.RGBA
	mapsErr     error
	pointerErr  error

	// Cache for scaled maps
	scaledMu    sync.Mutex
	scaledScale float64
	scaledMaps  []MapData
}

var _ maa.CustomRecognitionRunner = &Infer{}

// Run implements maa.CustomRecognitionRunner
func (i *Infer) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// Parse custom recognition parameters
	precision := 0.4
	threshold := 0.5
	mapNameRegexStr := "^map\\d+_lv\\d+$"
	if arg.CustomRecognitionParam != "" {
		var params InferParam
		if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err == nil {
			if params.MapNameRegex != "" {
				mapNameRegexStr = params.MapNameRegex
			}
			if params.Precision > 0.0 && params.Precision <= 1.0 {
				precision = params.Precision
			}
			if params.Threshold >= 0.0 && params.Threshold < 1.0 {
				threshold = params.Threshold
			}
		}
	}

	// Compile regex
	mapNameRegex, err := regexp.Compile(mapNameRegexStr)
	if err != nil {
		log.Error().Err(err).Str("regex", mapNameRegexStr).Msg("Invalid map_name_regex")
		return nil, false
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
		log.Error().Err(i.pointerErr).Msg("Failed to initialize pointer")
		return nil, false
	}

	// Perform location inference
	t0 := time.Now()
	locX, locY, locConf, mapName := i.inferLocation(arg.Img, locScale, mapNameRegex)
	locTime := time.Since(t0)

	// Perform rotation inference (if pointer is loaded)
	rot, rotConf := 0, 0.0
	var rotTime time.Duration
	t1 := time.Now()
	rot, rotConf = i.inferRotation(arg.Img, rotStep)
	rotTime = time.Since(t1)

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
		Int("x", locX).
		Int("y", locY).
		Int("rot", rot).
		Dur("locTime", locTime).
		Dur("rotTime", rotTime).
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
			log.Info().Int("mapsCount", len(i.maps)).Msg("Map images loaded")
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
			log.Info().Msg("Pointer template image loaded")
		}
	})
}

// loadMaps loads all map images from the resource directory
// and try crops them if map_rect.json exists
func (i *Infer) loadMaps(ctx *maa.Context) ([]MapData, error) {
	// Find map directory using search strategy
	mapDir := findResource(MAP_DIR)
	if mapDir == "" {
		return nil, fmt.Errorf("map directory not found (searched in cache and standard locations)")
	}

	// Read map_rect.json if it exists
	rectList := make(map[string][]int)
	rectPath := filepath.Join(mapDir, "map_rect.json")
	if data, err := os.ReadFile(rectPath); err == nil {
		if err := json.Unmarshal(data, &rectList); err != nil {
			log.Warn().Err(err).Str("path", rectPath).Msg("Failed to unmarshal map_rect.json")
		} else {
			log.Info().Msg("Map rect JSON loaded")
		}
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

		// Extract map name (remove "_merged.png" suffix)
		name := strings.TrimSuffix(filename, "_merged.png")

		var imgRGBA *image.RGBA
		offsetX, offsetY := 0, 0

		// Crop if valid rect exists
		if r, ok := rectList[name]; ok && len(r) == 4 {
			rect := image.Rect(r[0], r[1], r[2], r[3])
			// Crop precisely using drawing
			b := img.Bounds()
			r0 := rect.Intersect(b)
			dst := image.NewRGBA(image.Rect(0, 0, r0.Dx(), r0.Dy()))
			draw.Draw(dst, dst.Bounds(), img, r0.Min, draw.Src)
			imgRGBA = dst
			offsetX, offsetY = r0.Min.X, r0.Min.Y
		} else {
			imgRGBA = ToRGBA(img)
		}

		// Precompute integral image
		integral := NewIntegralImage(imgRGBA)

		maps = append(maps, MapData{
			Name:     name,
			Img:      imgRGBA,
			Integral: integral,
			OffsetX:  offsetX,
			OffsetY:  offsetY,
		})
	}

	if len(maps) == 0 {
		return nil, fmt.Errorf("no valid map images found in %s", mapDir)
	}

	return maps, nil
}

// loadPointer loads the pointer template image
func (i *Infer) loadPointer(ctx *maa.Context) (*image.RGBA, error) {
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

	rgba := ToRGBA(img)
	return rgba, nil
}

// inferLocation infers the player's location on the map
// Returns (x, y, confidence, mapName)
func (i *Infer) inferLocation(screenImg image.Image, locScale float64, mapNameRegex *regexp.Regexp) (int, int, float64, string) {
	// Use cached scaled maps
	scaledMaps := i.getScaledMaps(locScale)
	if len(scaledMaps) == 0 {
		log.Warn().Msg("No maps available for matching")
		return 0, 0, 0.0, "None"
	}

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

	triedCount := 0

	for _, mapData := range scaledMaps {
		// Filter maps based on regex
		if !mapNameRegex.MatchString(mapData.Name) {
			continue
		}
		triedCount++

		// Perform template matching (using optimized version with precomputed stats)
		// Note: mapData.Img is already cropped if a rect was provided in map_rect.json
		matchX, matchY, matchVal := MatchTemplateOptimized(mapData.Img, mapData.Integral, miniMapRGBA, miniStats)

		if matchVal > bestVal {
			bestVal = matchVal
			// Convert top-left corner to center position
			// Then convert back to original scale and add map offset
			bestX = int(float64(matchX+miniMapW/2)/locScale) + mapData.OffsetX
			bestY = int(float64(matchY+miniMapH/2)/locScale) + mapData.OffsetY
			bestMapName = mapData.Name
		}
	}

	if triedCount == 0 {
		log.Warn().Str("regex", mapNameRegex.String()).Msg("No maps matched the regex")
	}

	log.Debug().Int("triedMaps", triedCount).
		Float64("bestVal", bestVal).
		Str("bestMap", bestMapName).
		Msg("Location inference completed")

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
			OffsetX:  m.OffsetX,
			OffsetY:  m.OffsetY,
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
	pointerStats := GetNeedleStats(i.pointer)
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
		_, _, matchVal := MatchTemplateOptimized(rotatedRGBA, integral, i.pointer, pointerStats)

		if matchVal > maxVal {
			maxVal = matchVal
			bestAngle = angle
		}
	}

	// Convert to clockwise angle
	bestAngle = (360 - bestAngle) % 360

	return bestAngle, maxVal
}
