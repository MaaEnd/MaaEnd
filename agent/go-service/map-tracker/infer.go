// Copyright (c) 2026 Harry Huang
package maptracker

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"regexp"
	"sync"
	"time"

	mt "github.com/MaaXYZ/MaaEnd/agent/go-service/map-tracker/internal"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// MapTrackerInferResult represents the result of map tracking inference
type MapTrackerInferResult struct {
	MapName     string  `json:"mapName"`     // Map name
	X           float64 `json:"x"`           // X coordinate on the map
	Y           float64 `json:"y"`           // Y coordinate on the map
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

var mapTrackerInferDefaultParam = MapTrackerInferParam{
	MapNameRegex: "^map\\d+_lv\\d+$",
	Precision:    0.5,
	Threshold:    0.4,
}

// MapTrackerInfer is the custom recognition component for map tracking
type MapTrackerInfer struct {
	// Cache for scaled maps (recomputed per request scale)
	scaledMapsMu sync.Mutex
	scaledMaps   []mt.MapCache
	scaledScale  float64
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

// Location inference configuration
const (
	// Mini-map crop area
	LOC_CENTER_X = 108
	LOC_CENTER_Y = 111
	LOC_RADIUS   = 40
)

// Rotation inference configuration
const (
	// Pointer crop area
	ROT_CENTER_X = 108
	ROT_CENTER_Y = 111
	ROT_RADIUS   = 12
)

// Time-series empirical optimization configuration
const (
	PENDING_TAKEOVER_TIME_MS         = 1000
	PENDING_TAKEOVER_COUNT_THRESHOLD = 3
	CONVINCED_DISTANCE_THRESHOLD     = 30
	CONVINCED_VALID_TIME_MS          = 2000
)

type InferLocationRawResult struct {
	mapName       string
	x             float64
	y             float64
	conf          float64
	source        InferLocationHitMode
	elapsedTimeMs int64
}

var emptyLocationRawResult = InferLocationRawResult{"", 0, 0, 0.0, "", 0}

var mapCoreNameRegexp = regexp.MustCompile(`^(.+?)(?:_tier_\w+)?$`)

type InferRotationRawResult struct {
	rot           int
	conf          float64
	elapsedTimeMs int64
}

//go:embed messages/inference_failed.html
var inferenceFailedHTML string

//go:embed messages/inference_finished.html
var inferenceFinishedHTML string

var mapTrackerInferRunner maa.CustomRecognitionRunner = &MapTrackerInfer{}

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

	rotStep := max(2, min(8, int(math.Round(8-param.Precision*6))))

	// Initialize map resources
	mt.Resource.InitRawMaps(ctx)
	if mt.Resource.RawMapsErr != nil {
		log.Error().Err(mt.Resource.RawMapsErr).Msg("Failed to initialize maps")
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
			if !isMapNameCoreMatch(globalInferState.convinced.mapName, loc.mapName) {
				return false
			}
			dx := globalInferState.convinced.x - loc.x
			dy := globalInferState.convinced.y - loc.y
			return math.Hypot(dx, dy) < CONVINCED_DISTANCE_THRESHOLD
		}

		isCloseToPending := func() bool {
			if !isMapNameCoreMatch(globalInferState.pending.mapName, loc.mapName) {
				return false
			}
			dx := globalInferState.pending.x - loc.x
			dy := globalInferState.pending.y - loc.y
			return math.Hypot(dx, dy) < CONVINCED_DISTANCE_THRESHOLD
		}

		if isCloseToConvinced() {
			// This hit is close to the currently convinced location
			dt := nowMs - globalInferState.convincedLastHitTime
			if dt > 0 {
				dx := loc.x - globalInferState.convinced.x
				dy := loc.y - globalInferState.convinced.y
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
			// This hit is far from both convinced and pending locations
			if nowMs-globalInferState.convincedLastHitTime < CONVINCED_VALID_TIME_MS {
				// It's an immediate track loss, start a new pending
				globalInferState.pending = *loc
				globalInferState.pendingFirstHitTime = nowMs
				globalInferState.pendingHitCount = 1
			} else {
				// It's a stale track loss, directly replace convinced with this new hit
				globalInferState.convinced = *loc
				globalInferState.convincedLastHitTime = nowMs
				globalInferState.convincedMoveSpeed = 0
				globalInferState.convincedMoveDirection = 0
				globalInferState.pending = emptyLocationRawResult
				globalInferState.pendingHitCount = 0
				finalLoc = &globalInferState.convinced
			}
		}
	}

	if finalLoc == nil {
		if globalInferState.convinced.mapName != "" && nowMs-globalInferState.convincedLastHitTime < CONVINCED_VALID_TIME_MS {
			// This is a temporary miss, but we can generate a virtual result
			dt := nowMs - globalInferState.convincedLastHitTime
			sx := globalInferState.convincedMoveSpeed * math.Cos(globalInferState.convincedMoveDirection)
			sy := globalInferState.convincedMoveSpeed * math.Sin(globalInferState.convincedMoveDirection)
			vx := roundTo1Decimal(globalInferState.convinced.x + sx*float64(dt))
			vy := roundTo1Decimal(globalInferState.convinced.y + sy*float64(dt))

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
		Float64("X", result.X).Float64("Y", result.Y).
		Int("Rot", result.Rot).
		Float64("LocConf", result.LocConf).
		Float64("RotConf", result.RotConf).
		Msg("Map tracking inference completed")
	if param.Print {
		maafocus.NodeActionStarting(
			ctx,
			fmt.Sprintf(inferenceFinishedHTML, finalLoc.x, finalLoc.y, result.Rot, finalLoc.mapName),
		)
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
				param.MapNameRegex = mapTrackerInferDefaultParam.MapNameRegex
			}

			if param.Precision == 0.0 {
				param.Precision = mapTrackerInferDefaultParam.Precision
			} else if param.Precision < 0.0 || param.Precision > 1.0 {
				return nil, fmt.Errorf("invalid precision value: %f", param.Precision)
			}

			if param.Threshold == 0.0 {
				param.Threshold = mapTrackerInferDefaultParam.Threshold
			} else if param.Threshold < 0.0 || param.Threshold > 1.0 {
				return nil, fmt.Errorf("invalid threshold value: %f", param.Threshold)
			}
		} else {
			return nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
		}
		return &param, nil
	} else {
		return &mapTrackerInferDefaultParam, nil
	}
}

func getMapCoreName(mapName string) string {
	matches := mapCoreNameRegexp.FindStringSubmatch(mapName)
	if len(matches) < 2 {
		return mapName
	}
	return matches[1]
}

func isMapNameCoreMatch(mapName1, mapName2 string) bool {
	if mapName1 == "" || mapName2 == "" {
		return false
	}
	return getMapCoreName(mapName1) == getMapCoreName(mapName2)
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
	miniMapHalfW, miniMapHalfH := float64(miniMapW)/2.0, float64(miniMapH)/2.0

	// Precompute needle (minimap) statistics for all matches
	miniStats := minicv.GetImageStats(miniMap)
	if miniStats.Std < 1e-6 {
		return nil
	}

	// Time-series empirical optimization
	// If the user is in a stable state (convinced location updated recently, no pending drifts),
	// try to match the convinced map around the convinced location first.
	globalInferState.mu.Lock()

	stableConvincedMapName := globalInferState.convinced.mapName
	stableLocX := globalInferState.convinced.x
	stableLocY := globalInferState.convinced.y
	isInTime := globalInferState.convinced.mapName != "" &&
		(time.Now().UnixMilli()-globalInferState.convincedLastHitTime < CONVINCED_VALID_TIME_MS) &&
		globalInferState.pendingHitCount == 0

	globalInferState.mu.Unlock()

	isStable := func() bool {
		if !isInTime {
			return false
		}
		for _, mapData := range scaledMaps {
			if isMapNameCoreMatch(stableConvincedMapName, mapData.Name) && mapNameRegex.MatchString(mapData.Name) {
				return true
			}
		}
		return false
	}

	// Try fast search if stable
	if isStable() {
		fastBestVal := -1.0
		fastBestX, fastBestY := 0.0, 0.0
		fastBestMapName := ""

		for idx := range scaledMaps {
			mapData := &scaledMaps[idx]
			if !isMapNameCoreMatch(stableConvincedMapName, mapData.Name) || !mapNameRegex.MatchString(mapData.Name) {
				continue
			}

			expectedCenterX := int(math.Round((stableLocX - float64(mapData.OffsetX)) * scale))
			expectedCenterY := int(math.Round((stableLocY - float64(mapData.OffsetY)) * scale))
			searchRadius := max(int(float64(CONVINCED_DISTANCE_THRESHOLD)*scale), 1)
			searchArea := [4]int{
				expectedCenterX - searchRadius,
				expectedCenterY - searchRadius,
				searchRadius * 2,
				searchRadius * 2,
			}

			matchX, matchY, matchVal := minicv.MatchTemplateInArea(mapData.Img, mapData.GetIntegralArray(), miniMap, miniStats, searchArea)

			if matchVal > fastBestVal {
				fastBestVal = matchVal
				fastBestX = roundTo1Decimal((matchX+miniMapHalfW)/scale + float64(mapData.OffsetX))
				fastBestY = roundTo1Decimal((matchY+miniMapHalfH)/scale + float64(mapData.OffsetY))
				fastBestMapName = mapData.Name
			}
		}

		if fastBestVal > param.Threshold {
			elapsedTimeMs := time.Since(t0).Milliseconds()
			log.Debug().Float64("conf", fastBestVal).
				Str("map", fastBestMapName).
				Float64("X", fastBestX).
				Float64("Y", fastBestY).
				Int64("elapsedTimeMs", elapsedTimeMs).
				Msg("Internal fast search location inference completed")

			return &InferLocationRawResult{
				mapName:       fastBestMapName,
				x:             fastBestX,
				y:             fastBestY,
				conf:          fastBestVal,
				source:        FAST_SEARCH_HIT,
				elapsedTimeMs: elapsedTimeMs,
			}
		}
	} else {
		log.Debug().Msg("Empirical fast search skipped, not in stable state or regex mismatch")
	}

	// Match against all maps in parallel
	type mapResult struct {
		val     float64
		x, y    float64
		mapName string
	}

	bestVal := -1.0
	bestX, bestY := 0.0, 0.0
	bestMapName := ""
	triedCount := 0

	// Special case: if there's only one map to check, run it directly to avoid goroutine overhead
	var singleMapToTry *mt.MapCache
	for i := range scaledMaps {
		if mapNameRegex.MatchString(scaledMaps[i].Name) {
			triedCount++
			if triedCount == 1 {
				singleMapToTry = &scaledMaps[i]
			}
		}
	}
	if triedCount != 1 {
		singleMapToTry = nil
	}

	if singleMapToTry != nil {
		matchX, matchY, matchVal := minicv.MatchTemplate(singleMapToTry.Img, singleMapToTry.GetIntegralArray(), miniMap, miniStats)
		bestVal = matchVal
		bestX = roundTo1Decimal((matchX+miniMapHalfW)/scale + float64(singleMapToTry.OffsetX))
		bestY = roundTo1Decimal((matchY+miniMapHalfH)/scale + float64(singleMapToTry.OffsetY))
		bestMapName = singleMapToTry.Name
	} else if triedCount > 1 {
		resChan := make(chan mapResult, triedCount)
		var wg sync.WaitGroup

		for idx := range scaledMaps {
			mapData := &scaledMaps[idx]
			if !mapNameRegex.MatchString(mapData.Name) {
				continue
			}

			wg.Add(1)
			go func(m *mt.MapCache) {
				defer wg.Done()
				matchX, matchY, matchVal := minicv.MatchTemplate(m.Img, m.GetIntegralArray(), miniMap, miniStats)
				mx := roundTo1Decimal((matchX+miniMapHalfW)/scale + float64(m.OffsetX))
				my := roundTo1Decimal((matchY+miniMapHalfH)/scale + float64(m.OffsetY))
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
		Float64("X", bestX).
		Float64("Y", bestY).
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

// inferRotation infers the player's rotation angle
// Returns (angle, confidence)
func (i *MapTrackerInfer) inferRotation(screenImg *image.RGBA, rotStep int) *InferRotationRawResult {
	t0 := time.Now()

	pointerTemplate, err := mt.Resource.PointerTemplateLoader.Get()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load pointer template image")
		return nil
	}

	// Crop pointer area from screen
	patch := minicv.ImageCropSquareByRadius(screenImg, ROT_CENTER_X, ROT_CENTER_Y, ROT_RADIUS)

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
			_, _, matchVal := minicv.MatchTemplate(rotatedRGBA, integral, pointerTemplate.Image, pointerTemplate.Stats)

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

func roundTo1Decimal(value float64) float64 {
	return math.Round(value*10.0) / 10.0
}

// getScaledMaps recomputes scaled map cache for the requested scale.
func (i *MapTrackerInfer) getScaledMaps(scale float64) []mt.MapCache {
	i.scaledMapsMu.Lock()
	defer i.scaledMapsMu.Unlock()

	if i.scaledMaps != nil && math.Abs(i.scaledScale-scale) < 1e-6 {
		return i.scaledMaps
	}

	newScaled := make([]mt.MapCache, 0, len(mt.Resource.RawMaps))
	for _, m := range mt.Resource.RawMaps {
		sImg := minicv.ImageScale(m.Img, scale)
		newScaled = append(newScaled, mt.MapCache{
			Name:    m.Name,
			Img:     sImg,
			OffsetX: m.OffsetX,
			OffsetY: m.OffsetY,
		})
	}

	i.scaledMaps = newScaled
	i.scaledScale = scale
	return i.scaledMaps
}
