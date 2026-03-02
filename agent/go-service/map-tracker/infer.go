// Copyright (c) 2026 Harry Huang
package maptracker

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// MapTrackerInferResult represents the result of map tracking inference
type MapTrackerInferResult struct {
	MapName     string  `json:"mapName"`     // Map name
	X           int     `json:"x"`           // X coordinate on the map
	Y           int     `json:"y"`           // Y coordinate on the map
	Rot         int     `json:"rot"`         // Rotation angle (0-359 degrees)
	LocConf     float64 `json:"locConf"`     // Location confidence
	RotConf     float64 `json:"rotConf"`     // Rotation confidence
	LocTimeMs   int64   `json:"locTimeMs"`   // Location inference time in ms
	RotTimeMs   int64   `json:"rotTimeMs"`   // Rotation inference time in ms
	InferMode   string  `json:"inferMode"`   // Inference mode ("FullSearchHit", "FastSearchHit", "VirtualHit")
	InferTimeMs int64   `json:"inferTimeMs"` // Total inference time in ms
}

// MapTrackerInferParam represents the custom_recognition_param for MapTrackerInfer
type MapTrackerInferParam struct {
	// MapNameRegex is a regex pattern to filter which maps to consider during inference.
	MapNameRegex string `json:"map_name_regex,omitempty"`
	// Print controls whether to print inference results to the GUI.
	Print bool `json:"print,omitempty"`
	// Precision controls the inference precision/speed tradeoff.
	Precision float64 `json:"precision,omitempty"`
	// Threshold controls the minimum confidence required to consider the inference successful.
	Threshold float64 `json:"threshold,omitempty"`
}

// MapCache represents a preloaded map image
type MapCache struct {
	Name     string
	Img      *image.RGBA
	Integral minicv.IntegralArray
	OffsetX  int
	OffsetY  int
}

// MapTrackerInfer is the custom recognition component for map tracking
type MapTrackerInfer struct {
	// Cache for preloaded resources
	mapsOnce    sync.Once
	pointerOnce sync.Once
	maps        []MapCache
	pointer     *image.RGBA
	mapsErr     error
	pointerErr  error

	// Cache for scaled maps
	scaledMu    sync.Mutex
	scaledScale float64
	scaledMaps  []MapCache
}

type InferState struct {
	convinced              InferLocationRawResult
	convincedLastHitTime   int64
	convincedMoveDirection float64
	convincedMoveSpeed     float64

	pending             InferLocationRawResult
	pendingFirstHitTime int64
	pendingHitCount     int

	mu sync.Mutex
}

var globalInferState InferState

type InferLocationHitMode string

const (
	FULL_SEARCH_HIT InferLocationHitMode = "FullSearchHit"
	FAST_SEARCH_HIT InferLocationHitMode = "FastSearchHit"
	VIRTUAL_HIT     InferLocationHitMode = "VirtualHit"
)

type InferLocationRawResult struct {
	mapName       string
	x             int
	y             int
	conf          float64
	source        InferLocationHitMode
	elapsedTimeMs int64
}

var emptyLocationRawResult = InferLocationRawResult{"", 0, 0, 0.0, "", 0}

type InferRotationRawResult struct {
	rot           int
	conf          float64
	elapsedTimeMs int64
}

//go:embed messages/inference_failed.html
var inferenceFailedHTML string

//go:embed messages/inference_finished.html
var inferenceFinishedHTML string

var _ maa.CustomRecognitionRunner = &MapTrackerInfer{}

// Run implements maa.CustomRecognitionRunner
func (i *MapTrackerInfer) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// Parse custom recognition parameters
	param, err := i.parseParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerInfer")
		return nil, false
	}

	// Compile regex
	mapNameRegex, err := regexp.Compile(param.MapNameRegex)
	if err != nil {
		log.Error().Err(err).Str("regex", param.MapNameRegex).Msg("Invalid map_name_regex")
		return nil, false
	}

	var rotStep int
	if param.Precision < 0.3 {
		rotStep = 12
	} else if param.Precision < 0.6 {
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

	// Perform inference
	screenImg := minicv.ImageConvertRGBA(arg.Img)
	t0 := time.Now()

	var wg sync.WaitGroup
	wg.Add(2)

	var loc *InferLocationRawResult
	var rot *InferRotationRawResult

	go func() {
		defer wg.Done()
		loc = i.inferLocation(screenImg, mapNameRegex, param)
	}()

	go func() {
		defer wg.Done()
		rot = i.inferRotation(screenImg, rotStep)
	}()

	wg.Wait()

	// Determine if recognition hit natively
	internalLocHit := loc != nil && loc.conf > param.Threshold
	internalRotHit := rot != nil && rot.conf > param.Threshold

	// Final results (nil for now)
	var finalLoc *InferLocationRawResult
	var finalRot *InferRotationRawResult

	globalInferState.mu.Lock()
	nowMs := time.Now().UnixMilli()

	// Process internal location hit
	if internalLocHit {
		isCloseToConvinced := func() bool {
			if globalInferState.convinced.mapName == "" || globalInferState.convinced.mapName != loc.mapName {
				return false
			}
			dx := float64(globalInferState.convinced.x - loc.x)
			dy := float64(globalInferState.convinced.y - loc.y)
			return math.Hypot(dx, dy) < CONVINCED_DISTANCE_THRESHOLD
		}

		isCloseToPending := func() bool {
			if globalInferState.pending.mapName == "" || globalInferState.pending.mapName != loc.mapName {
				return false
			}
			dx := float64(globalInferState.pending.x - loc.x)
			dy := float64(globalInferState.pending.y - loc.y)
			return math.Hypot(dx, dy) < CONVINCED_DISTANCE_THRESHOLD
		}

		if isCloseToConvinced() {
			// This hit is close to the currently convinced location
			dt := nowMs - globalInferState.convincedLastHitTime
			if dt > 0 {
				dx := float64(loc.x - globalInferState.convinced.x)
				dy := float64(loc.y - globalInferState.convinced.y)
				dist := math.Hypot(dx, dy)
				globalInferState.convincedMoveSpeed = dist / float64(dt)
				globalInferState.convincedMoveDirection = math.Atan2(dy, dx)
			}
			globalInferState.convinced = *loc
			globalInferState.convincedLastHitTime = nowMs
			finalLoc = loc

		} else if isCloseToPending() {
			// This hit is close to the pending location
			globalInferState.pending.x = loc.x
			globalInferState.pending.y = loc.y
			globalInferState.pendingHitCount++

			if globalInferState.convinced.mapName == "" ||
				nowMs-globalInferState.pendingFirstHitTime >= PENDING_TAKEOVER_TIME_MS ||
				globalInferState.pendingHitCount >= PENDING_TAKEOVER_COUNT_THRESHOLD {
				// Do takeover (replace convinced with pending)
				globalInferState.convinced = globalInferState.pending
				globalInferState.convincedLastHitTime = nowMs
				globalInferState.convincedMoveSpeed = 0
				globalInferState.convincedMoveDirection = 0
				globalInferState.pending = emptyLocationRawResult
				globalInferState.pendingHitCount = 0
				finalLoc = &globalInferState.convinced
			}
		} else {
			// This hit is far from both convinced and pending locations, start a new pending
			globalInferState.pending = *loc
			globalInferState.pendingFirstHitTime = nowMs
			globalInferState.pendingHitCount = 1
		}
	}

	if finalLoc == nil {
		if globalInferState.convinced.mapName != "" && nowMs-globalInferState.convincedLastHitTime < CONVINCED_VALID_TIME_MS {
			// This is a temporary miss, but we can generate a virtual result
			dt := nowMs - globalInferState.convincedLastHitTime
			sx := globalInferState.convincedMoveSpeed * math.Cos(globalInferState.convincedMoveDirection)
			sy := globalInferState.convincedMoveSpeed * math.Sin(globalInferState.convincedMoveDirection)
			vx := globalInferState.convinced.x + int(sx*float64(dt))
			vy := globalInferState.convinced.y + int(sy*float64(dt))

			finalLoc = &InferLocationRawResult{
				mapName:       globalInferState.convinced.mapName,
				x:             vx,
				y:             vy,
				conf:          0,
				source:        VIRTUAL_HIT,
				elapsedTimeMs: 0,
			}
		}
	}

	// Process internal rotation hit
	if internalRotHit {
		finalRot = rot
	}

	globalInferState.mu.Unlock()

	finalHit := finalLoc != nil && finalRot != nil
	finalElapsedTimeMs := time.Since(t0).Milliseconds()

	if !finalHit {
		log.Info().Bool("finalLocHit", finalLoc != nil).Bool("finalRotHit", finalRot != nil).Msg("Map tracking inference did not hit")
		if param.Print {
			maafocus.NodeActionStarting(ctx, inferenceFailedHTML)
		}

		// Return as not hit
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: "",
		}, false
	}

	// Build hit result
	result := MapTrackerInferResult{
		MapName:     finalLoc.mapName,
		X:           finalLoc.x,
		Y:           finalLoc.y,
		Rot:         finalRot.rot,
		LocConf:     finalLoc.conf,
		RotConf:     finalRot.conf,
		LocTimeMs:   finalLoc.elapsedTimeMs,
		RotTimeMs:   finalRot.elapsedTimeMs,
		InferMode:   string(finalLoc.source),
		InferTimeMs: finalElapsedTimeMs,
	}

	// Serialize result to JSON
	detailJSON, err := json.Marshal(result)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal result")
		return nil, false
	}

	log.Info().Str("InferMode", result.InferMode).
		Int64("InferTimeMs", result.InferTimeMs).
		Str("MapName", result.MapName).
		Int("X", result.X).Int("Y", result.Y).
		Int("Rot", result.Rot).
		Float64("LocConf", result.LocConf).
		Float64("RotConf", result.RotConf).
		Msg("Map tracking inference completed")
	if param.Print {
		maafocus.NodeActionStarting(ctx, fmt.Sprintf(inferenceFinishedHTML, finalLoc.x, finalLoc.y, result.Rot, finalLoc.mapName))
	}

	// Return as hit
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

func (r *MapTrackerInfer) parseParam(paramStr string) (*MapTrackerInferParam, error) {
	if paramStr != "" {
		var param MapTrackerInferParam
		if err := json.Unmarshal([]byte(paramStr), &param); err == nil {
			if param.MapNameRegex == "" {
				param.MapNameRegex = DEFAULT_INFERENCE_PARAM.MapNameRegex
			}

			if param.Precision == 0.0 {
				param.Precision = DEFAULT_INFERENCE_PARAM.Precision
			} else if param.Precision < 0.0 || param.Precision > 1.0 {
				return nil, fmt.Errorf("invalid precision value: %f", param.Precision)
			}

			if param.Threshold == 0.0 {
				param.Threshold = DEFAULT_INFERENCE_PARAM.Threshold
			} else if param.Threshold < 0.0 || param.Threshold > 1.0 {
				return nil, fmt.Errorf("invalid threshold value: %f", param.Threshold)
			}
		} else {
			return nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
		}
		return &param, nil
	} else {
		return &DEFAULT_INFERENCE_PARAM, nil
	}
}

// initMaps initializes the map cache (thread-safe, runs once)
func (i *MapTrackerInfer) initMaps(ctx *maa.Context) {
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
func (i *MapTrackerInfer) initPointer(ctx *maa.Context) {
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
// and try crops them if map bbox data exists
func (i *MapTrackerInfer) loadMaps(ctx *maa.Context) ([]MapCache, error) {
	// Find map directory using search strategy
	mapDir := findResource(MAP_DIR)
	if mapDir == "" {
		return nil, fmt.Errorf("map directory not found (searched in cache and standard locations)")
	}

	// Read map_bbox.json if it exists
	rectList := make(map[string][]int)
	rectPath := filepath.Join(mapDir, "map_bbox.json")
	if data, err := os.ReadFile(rectPath); err == nil {
		if err := json.Unmarshal(data, &rectList); err != nil {
			log.Warn().Err(err).Str("path", rectPath).Msg("Failed to unmarshal map_bbox.json")
		} else {
			log.Info().Msg("Map bbox JSON loaded")
		}
	}

	// Read directory entries
	entries, err := os.ReadDir(mapDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read map directory: %w", err)
	}

	// Load all PNG files
	maps := make([]MapCache, 0)
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

		// Extract map name (remove ".png" suffix)
		name := strings.TrimSuffix(filename, ".png")

		var imgRGBA *image.RGBA
		offsetX, offsetY := 0, 0

		// Crop if valid rect exists
		if r, ok := rectList[name]; ok && len(r) == 4 {
			rect := image.Rect(r[0], r[1], r[2], r[3])
			expand := LOC_RADIUS / 2
			rect = image.Rect(rect.Min.X-expand, rect.Min.Y-expand, rect.Max.X+expand, rect.Max.Y+expand)

			// Crop precisely using drawing
			b := img.Bounds()
			r0 := rect.Intersect(b)
			dst := image.NewRGBA(image.Rect(0, 0, r0.Dx(), r0.Dy()))
			draw.Draw(dst, dst.Bounds(), img, r0.Min, draw.Src)
			imgRGBA = dst
			offsetX, offsetY = r0.Min.X, r0.Min.Y
		} else {
			imgRGBA = minicv.ImageConvertRGBA(img)
		}

		// Precompute integral image
		integral := minicv.GetIntegralArray(imgRGBA)

		maps = append(maps, MapCache{
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
func (i *MapTrackerInfer) loadPointer(ctx *maa.Context) (*image.RGBA, error) {
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

	rgba := minicv.ImageConvertRGBA(img)
	return rgba, nil
}

// inferLocation infers the player's location on the map.
// Returns a raw result with mapName, x/y (map coordinates), conf, source, and elapsedTimeMs.
func (i *MapTrackerInfer) inferLocation(screenImg *image.RGBA, mapNameRegex *regexp.Regexp, param *MapTrackerInferParam) *InferLocationRawResult {
	t0 := time.Now()

	// Use cached scaled maps
	scale := param.Precision
	scaledMaps := i.getScaledMaps(scale)
	if len(scaledMaps) == 0 {
		log.Warn().Msg("No maps available for matching")
		return nil
	}

	// Crop and scale mini-map area from screen
	miniMap := minicv.ImageCropSquareByRadius(screenImg, LOC_CENTER_X, LOC_CENTER_Y, LOC_RADIUS)
	miniMap = minicv.ImageScale(miniMap, scale)
	miniMapBounds := miniMap.Bounds()
	miniMapW, miniMapH := miniMapBounds.Dx(), miniMapBounds.Dy()

	// Precompute needle (minimap) statistics for all matches
	miniStats := minicv.GetImageStats(miniMap)
	if miniStats.Std < 1e-6 {
		return nil
	}

	// Time-series empirical optimization
	// If the user is in a stable state (convinced location updated recently, no pending drifts),
	// try to match the convinced map around the convinced location first.
	globalInferState.mu.Lock()

	isStable := globalInferState.convinced.mapName != "" &&
		(time.Now().UnixMilli()-globalInferState.convincedLastHitTime < CONVINCED_VALID_TIME_MS) &&
		globalInferState.pendingHitCount == 0
	stableMapName := globalInferState.convinced.mapName
	stableLocX := globalInferState.convinced.x
	stableLocY := globalInferState.convinced.y

	globalInferState.mu.Unlock()

	// Try fast search if stable
	if isStable && mapNameRegex.MatchString(stableMapName) {
		for _, mapData := range scaledMaps {
			if mapData.Name == stableMapName {
				expectedCenterX := int(float64(stableLocX-mapData.OffsetX) * scale)
				expectedCenterY := int(float64(stableLocY-mapData.OffsetY) * scale)
				searchRadius := max(int(float64(CONVINCED_DISTANCE_THRESHOLD)*scale), 1)

				matchX, matchY, matchVal := MatchTemplateAround(mapData.Img, mapData.Integral, miniMap, miniStats, expectedCenterX, expectedCenterY, searchRadius)

				if matchVal > param.Threshold {
					// Fast search hit
					bestX := int(float64(matchX+miniMapW/2)/scale) + mapData.OffsetX
					bestY := int(float64(matchY+miniMapH/2)/scale) + mapData.OffsetY
					elapsedTimeMs := time.Since(t0).Milliseconds()
					log.Debug().Float64("conf", matchVal).
						Str("map", stableMapName).
						Int("X", bestX).
						Int("Y", bestY).
						Int64("elapsedTimeMs", elapsedTimeMs).
						Msg("Internal fast search location inference completed")

					return &InferLocationRawResult{
						mapName:       mapData.Name,
						x:             bestX,
						y:             bestY,
						conf:          matchVal,
						source:        FAST_SEARCH_HIT,
						elapsedTimeMs: elapsedTimeMs,
					}
				}

				// If fast search fails (low confidence), fallback to full search
				log.Debug().Float64("conf", matchVal).Msg("Empirical fast search miss")
				break
			}
		}
	} else {
		log.Debug().Msg("Empirical fast search skipped, not in stable state or regex mismatch")
	}

	// Match against all maps in parallel
	type mapResult struct {
		val     float64
		x, y    int
		mapName string
	}

	bestVal := -1.0
	bestX, bestY := 0, 0
	bestMapName := ""
	triedCount := 0

	// Special case: if there's only one map to check, run it directly to avoid goroutine overhead
	var singleMapToTry *MapCache
	for i := range scaledMaps {
		if mapNameRegex.MatchString(scaledMaps[i].Name) {
			triedCount++
			if singleMapToTry == nil {
				singleMapToTry = &scaledMaps[i]
			} else {
				singleMapToTry = nil // Found more than one
				break
			}
		}
	}

	if singleMapToTry != nil {
		matchX, matchY, matchVal := MatchTemplateOptimized(singleMapToTry.Img, singleMapToTry.Integral, miniMap, miniStats)
		bestVal = matchVal
		bestX = int(float64(matchX+miniMapW/2)/scale) + singleMapToTry.OffsetX
		bestY = int(float64(matchY+miniMapH/2)/scale) + singleMapToTry.OffsetY
		bestMapName = singleMapToTry.Name
	} else if triedCount > 1 {
		resChan := make(chan mapResult, triedCount)
		var wg sync.WaitGroup

		for _, mapData := range scaledMaps {
			if !mapNameRegex.MatchString(mapData.Name) {
				continue
			}

			wg.Add(1)
			go func(m MapCache) {
				defer wg.Done()
				matchX, matchY, matchVal := MatchTemplateOptimized(m.Img, m.Integral, miniMap, miniStats)
				mx := int(float64(matchX+miniMapW/2)/scale) + m.OffsetX
				my := int(float64(matchY+miniMapH/2)/scale) + m.OffsetY
				resChan <- mapResult{matchVal, mx, my, m.Name}
			}(mapData)
		}

		go func() {
			wg.Wait()
			close(resChan)
		}()

		for res := range resChan {
			if res.val > bestVal {
				bestVal = res.val
				bestX = res.x
				bestY = res.y
				bestMapName = res.mapName
			}
		}
	}

	if triedCount == 0 {
		log.Warn().Str("regex", mapNameRegex.String()).Msg("No maps matched the regex")
	}
	elapsedTimeMs := time.Since(t0).Milliseconds()

	log.Debug().Int("triedMaps", triedCount).
		Float64("bestConf", bestVal).
		Str("bestMap", bestMapName).
		Int("X", bestX).
		Int("Y", bestY).
		Int64("elapsedTimeMs", elapsedTimeMs).
		Msg("Internal location inference completed")

	return &InferLocationRawResult{
		mapName:       bestMapName,
		x:             bestX,
		y:             bestY,
		conf:          bestVal,
		source:        FULL_SEARCH_HIT,
		elapsedTimeMs: time.Since(t0).Milliseconds(),
	}
}

// getScaledMaps returns cached scaled maps or recomputes them
func (i *MapTrackerInfer) getScaledMaps(scale float64) []MapCache {
	i.scaledMu.Lock()
	defer i.scaledMu.Unlock()

	if i.scaledScale == scale && len(i.scaledMaps) > 0 {
		return i.scaledMaps
	}

	log.Info().Float64("scale", scale).Msg("Recomputing scaled maps cache")
	newScaled := make([]MapCache, 0, len(i.maps))
	for _, m := range i.maps {
		sImg := minicv.ImageScale(m.Img, scale)
		newScaled = append(newScaled, MapCache{
			Name:     m.Name,
			Img:      sImg,
			Integral: minicv.GetIntegralArray(sImg),
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
func (i *MapTrackerInfer) inferRotation(screenImg *image.RGBA, rotStep int) *InferRotationRawResult {
	t0 := time.Now()

	if i.pointer == nil {
		return nil
	}

	// Crop pointer area from screen
	patch := minicv.ImageCropSquareByRadius(screenImg, ROT_CENTER_X, ROT_CENTER_Y, ROT_RADIUS)

	// Precompute needle (pointer) statistics
	pointerStats := minicv.GetImageStats(i.pointer)
	if pointerStats.Std < 1e-6 {
		return nil
	}

	// Try all rotation angles in parallel
	type result struct {
		angle int
		conf  float64
	}

	resChan := make(chan result, 360/rotStep+1)
	var wg sync.WaitGroup

	for angle := 0; angle < 360; angle += rotStep {
		wg.Add(1)
		go func(a int) {
			defer wg.Done()
			// Rotate the patch
			rotatedRGBA := minicv.ImageRotate(patch, float64(a))

			// Match against pointer template
			integral := minicv.GetIntegralArray(rotatedRGBA)
			_, _, matchVal := MatchTemplateOptimized(rotatedRGBA, integral, i.pointer, pointerStats)

			resChan <- result{a, matchVal}
		}(angle)
	}

	go func() {
		wg.Wait()
		close(resChan)
	}()

	bestAngle := 0
	maxVal := -1.0
	for res := range resChan {
		if res.conf > maxVal {
			maxVal = res.conf
			bestAngle = res.angle
		}
	}

	// Convert to clockwise angle
	bestAngle = (360 - bestAngle) % 360
	elapsedTimeMs := time.Since(t0).Milliseconds()

	log.Debug().
		Float64("bestConf", maxVal).
		Int("bestAngle", bestAngle).
		Int64("elapsedTimeMs", elapsedTimeMs).
		Msg("Internal rotation inference completed")

	return &InferRotationRawResult{
		rot:           bestAngle,
		conf:          maxVal,
		elapsedTimeMs: time.Since(t0).Milliseconds(),
	}
}
