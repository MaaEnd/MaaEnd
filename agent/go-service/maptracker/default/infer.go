// Copyright (c) 2026 Harry Huang
package maptrackerdefault

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"regexp"
	"sync"
	"time"

	internal "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/internal"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// MapTrackerInferResult represents the result of map tracking inference
type MapTrackerInferResult struct {
	MapName     string         `json:"mapName"`     // Map name
	X           float64        `json:"x"`           // X coordinate on the map (backward capability)
	Y           float64        `json:"y"`           // Y coordinate on the map (backward capability)
	Loc         internal.Point `json:"point"`       // Point struct for X/Y coordinates
	Rot         int            `json:"rot"`         // Rotation angle (0-359 degrees)
	LocConf     float64        `json:"locConf"`     // Location confidence
	RotConf     float64        `json:"rotConf"`     // Rotation confidence
	LocTimeMs   int64          `json:"locTimeMs"`   // Location inference time in ms
	RotTimeMs   int64          `json:"rotTimeMs"`   // Rotation inference time in ms
	InferMode   string         `json:"inferMode"`   // Inference mode ("FullSearchHit", "FastSearchHit")
	InferTimeMs int64          `json:"inferTimeMs"` // Total inference time in ms
}

// MapTrackerInferParam represents the custom_recognition_param for MapTrackerInfer
type MapTrackerInferParam struct {
	// MapNameRegex is a regex pattern to filter which maps to consider during inference.
	MapNameRegex string `json:"map_name_regex,omitempty"`
	// Precision controls the inference precision/speed tradeoff.
	Precision float64 `json:"precision,omitempty"`
	// Threshold controls the minimum confidence required to consider the inference successful.
	Threshold float64 `json:"threshold,omitempty"`
	// AllowedModes controls which location inference modes are allowed.
	AllowedModes int `json:"allowed_modes,omitempty"`
}

const (
	INFER_MODE_FULL_SEARCH = 1
	INFER_MODE_FAST_SEARCH = 2
)

var mapTrackerInferDefaultParam = MapTrackerInferParam{
	MapNameRegex: "^[a-z]+\\d*_[a-z]+\\d+$",
	Precision:    0.5,
	Threshold:    0.4,
	AllowedModes: INFER_MODE_FULL_SEARCH | INFER_MODE_FAST_SEARCH,
}

// MapTrackerInfer is the custom recognition component for map tracking
type MapTrackerInfer struct {
	// Cache for scaled maps (recomputed per request scale)
	scaledMapsMu sync.Mutex
	scaledMaps   []internal.MapCache
	scaledScale  float64
}

// InferLocationRawResult represents the raw result of location inference
type InferLocationRawResult struct {
	MapName       string
	Loc           internal.Point
	Conf          float64
	Source        InferLocationHitMode
	ElapsedTimeMs int64
}

type InferRotationRawResult struct {
	Rot           int
	Conf          float64
	ElapsedTimeMs int64
}

var mapCoreNameRegexp = regexp.MustCompile(`^(.+?)(?:_tier_\w+)?$`)

var MapTrackerInferRunner maa.CustomRecognitionRunner = &MapTrackerInfer{}

// Run implements maa.CustomRecognitionRunner
func (i *MapTrackerInfer) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// Parse custom recognition parameters
	param, err := i.parseParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerInfer")
		return nil, false
	}

	ctrlType, _ := control.GetControlType(ctx.GetTasker().GetController())

	// Compile regex
	mapNameRegex, err := regexp.Compile(param.MapNameRegex)
	if err != nil {
		log.Error().Err(err).Str("regex", param.MapNameRegex).Msg("Invalid map_name_regex")
		return nil, false
	}

	rotStep := max(2, min(6, int(math.Round(6-param.Precision*4))))

	// Initialize map resources
	internal.Resource.InitRawMaps(ctx)
	if internal.Resource.RawMapsErr != nil {
		log.Error().Err(internal.Resource.RawMapsErr).Msg("Failed to initialize maps")
		return nil, false
	}

	// Perform inference
	screenImg := minicv.ImageConvertRGBA(arg.Img)
	t0 := time.Now()

	ch := make(chan *InferLocationRawResult, 1)

	go func() {
		ch <- i.inferLocation(ctrlType, screenImg, mapNameRegex, param)
	}()

	rot := i.inferRotation(ctrlType, screenImg, rotStep)
	loc := <-ch

	// Determine if recognition hit natively
	internalLocHit := loc != nil && loc.Conf > param.Threshold
	internalRotHit := rot != nil && rot.Conf > param.Threshold

	// Final results (nil for now)
	var finalLoc *InferLocationRawResult
	var finalRot *InferRotationRawResult

	globalInferState.Lock()

	// Process internal location hit
	if internalLocHit {
		if globalInferState.IsCloseToConvinced(loc) {
			// This hit is close to the currently convinced location
			globalInferState.SetConvinced(*loc)
			finalLoc = loc

		} else if globalInferState.IsCloseToPending(loc) {
			// This hit is close to the pending location
			globalInferState.UpdatePending(loc.Loc)

			if globalInferState.ShouldTakeoverPending() {
				// Do takeover (replace convinced with pending)
				globalInferState.TakeoverPending()
				finalLoc = loc
			}
		} else {
			// This hit is far from both convinced and pending locations
			if globalInferState.IsImmediateTrackLoss() {
				// It's an immediate track loss, start a new pending
				globalInferState.SetPending(*loc)

				if param.AllowedModes&INFER_MODE_FAST_SEARCH == 0 {
					// If fast search is not allowed, directly take this hit as final to avoid long wait for next hit
					// Otherwise, don't set finalLoc here to wait for next fast search
					finalLoc = loc
				}
			} else {
				// It's a stale track loss, directly replace convinced with this new hit
				globalInferState.SetConvinced(*loc)
				globalInferState.ResetPending()
				finalLoc = loc
			}
		}
	}

	// Process internal rotation hit
	if internalRotHit {
		finalRot = rot
	}

	globalInferState.Unlock()

	finalHit := finalLoc != nil && finalRot != nil
	finalElapsedTimeMs := time.Since(t0).Milliseconds()

	if !finalHit {
		log.Info().Bool("finalLocHit", finalLoc != nil).Bool("finalRotHit", finalRot != nil).Msg("Map tracking inference did not hit")

		// Return as not hit
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: "",
		}, false
	}

	// Build hit result
	result := MapTrackerInferResult{
		MapName:     finalLoc.MapName,
		X:           finalLoc.Loc.X,
		Y:           finalLoc.Loc.Y,
		Loc:         finalLoc.Loc,
		Rot:         finalRot.Rot,
		LocConf:     finalLoc.Conf,
		RotConf:     finalRot.Conf,
		LocTimeMs:   finalLoc.ElapsedTimeMs,
		RotTimeMs:   finalRot.ElapsedTimeMs,
		InferMode:   string(finalLoc.Source),
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
		Float64("X", result.Loc.X).Float64("Y", result.Loc.Y). // Reserved for backward compatibility
		Object("LOC", result.Loc).
		Int("Rot", result.Rot).
		Float64("LocConf", result.LocConf).
		Float64("RotConf", result.RotConf).
		Msg("Map tracking inference completed")

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

			if param.AllowedModes == 0 {
				param.AllowedModes = mapTrackerInferDefaultParam.AllowedModes
			} else if param.AllowedModes&INFER_MODE_FULL_SEARCH == 0 {
				return nil, fmt.Errorf("allowed_modes must include INFER_MODE_FULL_SEARCH")
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
func (i *MapTrackerInfer) inferLocation(ctrlType string, screenImg *image.RGBA, mapNameRegex *regexp.Regexp, param *MapTrackerInferParam) *InferLocationRawResult {
	t0 := time.Now()

	// Use cached scaled maps
	scale := param.Precision
	scaledMaps := i.getScaledMaps(scale)
	if len(scaledMaps) == 0 {
		log.Warn().Msg("No maps available for matching")
		return nil
	}

	// Crop and scale mini-map area from screen
	var miniMap *image.RGBA
	switch ctrlType {
	case control.CONTROL_TYPE_ADB:
		miniMap = minicv.ImageCropSquareByRadius(screenImg, 136, 131, 50)
		miniMap = minicv.ImageScale(miniMap, 0.8)
	default: // Win32 and others
		miniMap = minicv.ImageCropSquareByRadius(screenImg, 108, 111, 40)
	}

	miniMap = minicv.ImageScale(miniMap, scale)
	miniMapHalfW, miniMapHalfH := float64(miniMap.Bounds().Dx())/2.0, float64(miniMap.Bounds().Dy())/2.0

	// Precompute needle (minimap) statistics for all matches
	miniStats := minicv.GetImageStats(miniMap)
	if miniStats.Std < 1e-6 {
		return nil
	}

	// Time-series empirical optimization
	// If the user is in a stable state (convinced location updated recently, no pending drifts),
	// try to match the convinced map around the convinced location first.
	globalInferState.Lock()

	stableConvincedMapName := globalInferState.convinced.MapName
	stableLoc := globalInferState.convinced.Loc
	isInTime := globalInferState.IsConvincedValid()

	globalInferState.Unlock()

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
	if param.AllowedModes&INFER_MODE_FAST_SEARCH != 0 && isStable() {
		fastBestVal := -1.0
		fastBestLoc := internal.Point{}
		fastBestMapName := ""

		for idx := range scaledMaps {
			mapData := &scaledMaps[idx]
			if !isMapNameCoreMatch(stableConvincedMapName, mapData.Name) || !mapNameRegex.MatchString(mapData.Name) {
				continue
			}

			expectedCenter := mapData.PixelTransform(scale).Apply(stableLoc)
			expectedCenterX := expectedCenter.IntX()
			expectedCenterY := expectedCenter.IntY()
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
				pixel := internal.Point{X: matchX + miniMapHalfW, Y: matchY + miniMapHalfH}
				loc := mapData.PixelTransform(scale).Inverse(pixel)
				fastBestLoc = internal.Point{
					X: roundTo1Decimal(loc.X),
					Y: roundTo1Decimal(loc.Y),
				}
				fastBestMapName = mapData.Name
			}
		}

		if fastBestVal > param.Threshold {
			elapsedTimeMs := time.Since(t0).Milliseconds()
			log.Debug().Float64("conf", fastBestVal).
				Str("map", fastBestMapName).
				Float64("X", fastBestLoc.X).
				Float64("Y", fastBestLoc.Y).
				Int64("elapsedTimeMs", elapsedTimeMs).
				Msg("Internal fast search location inference completed")

			return &InferLocationRawResult{
				MapName:       fastBestMapName,
				Loc:           fastBestLoc,
				Conf:          fastBestVal,
				Source:        FAST_SEARCH_HIT,
				ElapsedTimeMs: elapsedTimeMs,
			}
		}
	} else {
		log.Debug().Msg("Empirical fast search skipped, not in stable state or regex mismatch")
	}

	// Match against all maps in parallel
	type mapResult struct {
		val     float64
		loc     internal.Point
		mapName string
	}

	bestVal := -1.0
	bestLoc := internal.Point{}
	bestMapName := ""
	triedCount := 0

	// Special case: if there's only one map to check, run it directly to avoid goroutine overhead
	var singleMapToTry *internal.MapCache
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
		pixel := internal.Point{X: matchX + miniMapHalfW, Y: matchY + miniMapHalfH}
		loc := singleMapToTry.PixelTransform(scale).Inverse(pixel)
		bestLoc = internal.Point{
			X: roundTo1Decimal(loc.X),
			Y: roundTo1Decimal(loc.Y),
		}
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
			go func(m *internal.MapCache) {
				defer wg.Done()
				matchX, matchY, matchVal := minicv.MatchTemplate(m.Img, m.GetIntegralArray(), miniMap, miniStats)
				pixel := internal.Point{X: matchX + miniMapHalfW, Y: matchY + miniMapHalfH}
				loc := m.PixelTransform(scale).Inverse(pixel)
				resChan <- mapResult{
					val: matchVal,
					loc: internal.Point{
						X: roundTo1Decimal(loc.X),
						Y: roundTo1Decimal(loc.Y),
					},
					mapName: m.Name,
				}
			}(mapData)
		}

		go func() {
			wg.Wait()
			close(resChan)
		}()

		for res := range resChan {
			if res.val > bestVal {
				bestVal = res.val
				bestLoc = res.loc
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
		Float64("X", bestLoc.X).
		Float64("Y", bestLoc.Y).
		Int64("elapsedTimeMs", elapsedTimeMs).
		Msg("Internal location inference completed")

	return &InferLocationRawResult{
		MapName:       bestMapName,
		Loc:           bestLoc,
		Conf:          bestVal,
		Source:        FULL_SEARCH_HIT,
		ElapsedTimeMs: time.Since(t0).Milliseconds(),
	}
}

// inferRotation infers the player's rotation angle
// Returns (angle, confidence)
func (i *MapTrackerInfer) inferRotation(ctrlType string, screenImg *image.RGBA, rotStep int) *InferRotationRawResult {
	t0 := time.Now()

	pointerTemplate, err := internal.Resource.PointerTemplateLoader.Get()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load pointer template image")
		return nil
	}

	// Crop pointer area from screen
	var patch *image.RGBA
	switch ctrlType {
	case control.CONTROL_TYPE_ADB:
		patch = minicv.ImageCropSquareByRadius(screenImg, 136, 131, 15)
		patch = minicv.ImageScale(patch, 0.8)
	default: // Win32 and others
		patch = minicv.ImageCropSquareByRadius(screenImg, 108, 111, 12)
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

			// Match against pointer template and ignore green-screen pixels in the template.
			integral := minicv.GetIntegralArray(rotatedRGBA)
			_, _, matchVal := minicv.MatchTemplateWithMask(rotatedRGBA, integral, pointerTemplate.Image, pointerTemplate.Stats, 0x00FF00)

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
		Rot:           bestAngle,
		Conf:          maxVal,
		ElapsedTimeMs: time.Since(t0).Milliseconds(),
	}
}

func roundTo1Decimal(value float64) float64 {
	return math.Round(value*10.0) / 10.0
}

// getScaledMaps recomputes scaled map cache for the requested scale.
func (i *MapTrackerInfer) getScaledMaps(scale float64) []internal.MapCache {
	i.scaledMapsMu.Lock()
	defer i.scaledMapsMu.Unlock()

	if i.scaledMaps != nil && math.Abs(i.scaledScale-scale) < 1e-6 {
		return i.scaledMaps
	}

	newScaled := make([]internal.MapCache, 0, len(internal.Resource.RawMaps))
	for _, m := range internal.Resource.RawMaps {
		sImg := minicv.ImageScale(m.Img, scale)
		newScaled = append(newScaled, internal.MapCache{
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
