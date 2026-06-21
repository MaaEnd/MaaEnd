// Copyright (c) 2026 Harry Huang
package maptrackerbigmap

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"math"
	"math/rand"
	"sort"
	"sync"

	internal "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/internal"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// MapTrackerBigMapFindImageParam represents the custom_recognition_param for MapTrackerBigMapFindImage.
type MapTrackerBigMapFindImageParam struct {
	// Template is the path to the image file to match on the big map.
	Template string `json:"template"`
	// Expected controls whether the final match list should hit recognition.
	Expected mapTrackerBigMapFindImageExpected `json:"expected"`
	// Threshold is the minimum confidence for a valid match.
	Threshold float64 `json:"threshold,omitempty"`
	// GreenMask indicates whether to apply a #00FF00 color mask to the template during matching.
	GreenMask bool `json:"green_mask,omitempty"`
	// WithRotation indicates whether to perform rotation-invariant matching.
	WithRotation bool `json:"with_rotation,omitempty"`
	// MapTrackerBigMapZoomParam is the transient field set for pre-zoom operation.
	MapTrackerBigMapZoomParam
	// MapTrackerBigMapInferParam controls big-map inference behavior via embedded MapTrackerBigMapInfer parameters.
	MapTrackerBigMapInferParam
	// MaxMatches controls the maximum number of matches to return.
	MaxMatches int `json:"max_matches,omitempty"`
	// MustSeePoints, if specified, will perform a multi-viewport search to cover all given map coordinates.
	MustSeePoints [][2]int `json:"must_see_points,omitempty"`
}

// MapTrackerBigMapFindImageMatch represents a single template match result.
type MapTrackerBigMapFindImageMatch struct {
	ScreenX  float64 `json:"ScreenX"`
	ScreenY  float64 `json:"ScreenY"`
	MapName  string  `json:"MapName"`
	MapX     float64 `json:"MapX"`
	MapY     float64 `json:"MapY"`
	Conf     float64 `json:"Conf"`
	Rotation float64 `json:"Rotation"`
}

var mapTrackerBigMapFindImageDefaultParam = MapTrackerBigMapFindImageParam{
	Threshold:  0.5,
	MaxMatches: 32,
}

type mapTrackerBigMapFindImageExpected struct {
	mode      string
	boolValue bool
	count     int
	mapName   string
	target    [4]float64
}

func (e *mapTrackerBigMapFindImageExpected) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return fmt.Errorf("expected must not be null")
	}

	var boolValue bool
	if err := json.Unmarshal(data, &boolValue); err == nil {
		*e = mapTrackerBigMapFindImageExpected{mode: findImageExpectedModeBool, boolValue: boolValue}
		return nil
	}

	var countValue float64
	if err := json.Unmarshal(data, &countValue); err == nil {
		maxInt := int(^uint(0) >> 1)
		if countValue < 0 || math.Trunc(countValue) != countValue || countValue > float64(maxInt) {
			return fmt.Errorf("expected count must be a non-negative integer")
		}
		*e = mapTrackerBigMapFindImageExpected{mode: findImageExpectedModeCount, count: int(countValue)}
		return nil
	}

	var conditionValue struct {
		MapName string    `json:"map_name"`
		Target  []float64 `json:"target"`
	}
	if err := json.Unmarshal(data, &conditionValue); err == nil {
		if conditionValue.MapName == "" {
			return fmt.Errorf("expected map_name must not be empty")
		}
		if len(conditionValue.Target) != 4 {
			return fmt.Errorf("expected target must have 4 numbers [x, y, w, h]")
		}
		if conditionValue.Target[2] <= 0 || conditionValue.Target[3] <= 0 {
			return fmt.Errorf("expected target width and height must be positive")
		}
		*e = mapTrackerBigMapFindImageExpected{
			mode:    findImageExpectedModeLocation,
			mapName: conditionValue.MapName,
			target:  [4]float64{conditionValue.Target[0], conditionValue.Target[1], conditionValue.Target[2], conditionValue.Target[3]},
		}
		return nil
	}

	return fmt.Errorf("expected must be a boolean, non-negative integer, or {\"map_name\": string, \"target\": [x, y, w, h]}")
}

func (e mapTrackerBigMapFindImageExpected) isSatisfied(mapName string, matches []MapTrackerBigMapFindImageMatch) bool {
	switch e.mode {
	case findImageExpectedModeBool:
		if e.boolValue {
			return len(matches) > 0
		}
		return len(matches) == 0
	case findImageExpectedModeCount:
		return len(matches) == e.count
	case findImageExpectedModeLocation:
		if mapName != e.mapName {
			return false
		}
		x, y, w, h := e.target[0], e.target[1], e.target[2], e.target[3]
		for _, match := range matches {
			if match.MapX >= x && match.MapX < x+w && match.MapY >= y && match.MapY < y+h {
				return true
			}
		}
	}
	return false
}

const (
	findImageExpectedModeBool     = "bool"
	findImageExpectedModeCount    = "count"
	findImageExpectedModeLocation = "location"
)

const (
	SCAN_VIEWPORT_PADDING          = 40
	SCAN_VIEWPORT_MAX_RETRY        = 20
	ROTATION_STEP_DEG              = 3.0
	NMS_MIN_DISTANCE               = 10.0
	GREEN_SCREEN_MASK_COLOR_RGB888 = 0x00FF00
)

// MapTrackerBigMapFindImage is the custom recognition component for finding icon images on the big map.
type MapTrackerBigMapFindImage struct{}

var _ maa.CustomRecognitionRunner = &MapTrackerBigMapFindImage{}

// Run implements maa.CustomRecognitionRunner.
func (r *MapTrackerBigMapFindImage) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// 1. Parse parameters and load the template.
	param, err := r.parseParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerBigMapFindImage")
		return nil, false
	}

	if param.ZoomValue != 0 {
		zoomParamBytes, err := json.Marshal(param.MapTrackerBigMapZoomParam)
		if err != nil {
			log.Error().Err(err).Msg("Failed to marshal MapTrackerBigMapZoom parameters for FindImage")
			return nil, false
		}
		if !(&MapTrackerBigMapZoom{}).Run(ctx, &maa.CustomActionArg{
			TaskID:            arg.TaskID,
			CurrentTaskName:   arg.CurrentTaskName,
			CustomActionName:  "MapTrackerBigMapZoom",
			CustomActionParam: string(zoomParamBytes),
			Box:               arg.Roi,
		}) {
			log.Error().Float64("zoomValue", param.ZoomValue).Msg("Failed to pre-zoom for FindImage")
			return nil, false
		}
	}

	tpl, err := r.resolveTemplate(param.Template)
	if err != nil {
		log.Error().Err(err).Msg("Failed to resolve template for MapTrackerBigMapFindImage")
		return nil, false
	}

	// 2. Search the current viewport or multiple viewports.
	if len(param.MustSeePoints) > 0 {
		return r.runMultiViewportSearch(ctx, arg, tpl, param)
	}
	return r.runSingleViewportSearch(ctx, arg, tpl, param)
}

func (r *MapTrackerBigMapFindImage) parseParam(paramStr string) (*MapTrackerBigMapFindImageParam, error) {
	if paramStr == "" {
		return nil, fmt.Errorf("custom_recognition_param is required for MapTrackerBigMapFindImage")
	}

	var rawKeys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(paramStr), &rawKeys); err != nil {
		return nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
	}

	var param MapTrackerBigMapFindImageParam
	if err := json.Unmarshal([]byte(paramStr), &param); err != nil {
		return nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
	}

	if param.Template == "" {
		return nil, fmt.Errorf("template must not be empty")
	}
	if _, ok := rawKeys["expected"]; !ok {
		return nil, fmt.Errorf("expected is required")
	}
	if param.Threshold == 0.0 {
		param.Threshold = mapTrackerBigMapFindImageDefaultParam.Threshold
	}
	if param.MaxMatches == 0 {
		param.MaxMatches = mapTrackerBigMapFindImageDefaultParam.MaxMatches
	}

	return &param, nil
}

func (r *MapTrackerBigMapFindImage) resolveTemplate(tplPath string) (*minicv.Template, error) {
	resolvedPath := resource.FindResource(tplPath)
	if resolvedPath == "" {
		return nil, fmt.Errorf("template resource not found: %s", tplPath)
	}

	loader := minicv.NewTemplateLoaderOfPath(resolvedPath)
	tpl, err := loader.Get()
	if err != nil {
		return nil, fmt.Errorf("failed to load template %s: %w", tplPath, err)
	}
	return tpl, nil
}

func (r *MapTrackerBigMapFindImage) runSingleViewportSearch(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
	tpl *minicv.Template,
	param *MapTrackerBigMapFindImageParam,
) (*maa.CustomRecognitionResult, bool) {
	matchImg := arg.Img
	inferRes, err := r.inferResultWithImg(ctx, arg, matchImg, &param.MapTrackerBigMapInferParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to prepare big-map viewport for FindImage")
		return nil, false
	}
	viewport := &inferRes.ViewPort

	screenImg := minicv.ImageConvertRGBA(matchImg)
	cropped, _, _, ok := cropBigMapTemplate(screenImg)
	if !ok {
		log.Warn().Msg("Big-map crop area is invalid for FindImage")
		return nil, false
	}

	result := r.matchTemplate(cropped, tpl, param, viewport)
	saveFindImageDebug(inferRes.MapName, tpl, result)
	return r.buildResult(arg, param, inferRes.MapName, result, "Big-map FindImage completed")
}

// runMultiViewportSearch iteratively pans the big map to cover all must-see points,
// running template matching at each stop and aggregating all found icons.
//
// Flow: zoom → screenshot + infer → match → remove visible points → pick next → loop
func (r *MapTrackerBigMapFindImage) runMultiViewportSearch(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
	tpl *minicv.Template,
	param *MapTrackerBigMapFindImageParam,
) (*maa.CustomRecognitionResult, bool) {
	ctrl := ctx.GetTasker().GetController()
	ca, err := control.NewControlAdaptor(ctx, ctrl, WORK_W, WORK_H)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create control adaptor for multi-viewport search")
		return nil, false
	}

	panFactor := BIG_MAP_PAN_FACTOR_NUMERATOR / control.GetScreenDiagonalSize(ctrl)

	remaining := make([][2]int, len(param.MustSeePoints))
	copy(remaining, param.MustSeePoints)

	var allMatches []MapTrackerBigMapFindImageMatch
	var inferRes *MapTrackerBigMapInferResult
	pickAttempt := 0

	// Initial screenshot, infer, and match.
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get cached image in multi-viewport search")
		return nil, false
	}
	inferRes, err = r.inferResultWithImg(ctx, arg, img, &param.MapTrackerBigMapInferParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to infer viewport in multi-viewport search")
		return nil, false
	}
	remaining = filterPointsInView(remaining, &inferRes.ViewPort)
	r.matchCurrentViewport(tpl, param, &inferRes.ViewPort, img, &allMatches)

	for len(remaining) > 0 {
		if ctx.GetTasker().Stopping() {
			log.Warn().Msg("Task is stopping, exiting multi-viewport search")
			return nil, false
		}

		// Pick the next uncovered point: drag viewport toward it.
		target := remaining[0]
		pickAttempt++
		if pickAttempt > SCAN_VIEWPORT_MAX_RETRY {
			log.Error().Int("maxAttempts", SCAN_VIEWPORT_MAX_RETRY).Int("remaining", len(remaining)).Msg("Exceeded max pick attempts in multi-viewport search")
			break
		}

		targetInViewX, targetInViewY := inferRes.ViewPort.GetScreenCoordOf(float64(target[0]), float64(target[1]))
		if inferRes.ViewPort.IsScreenCoordInView(targetInViewX, targetInViewY) {
			remaining = remaining[1:]
			pickAttempt = 0
			continue
		}

		centerX := (inferRes.ViewPort.Left + inferRes.ViewPort.Right) * 0.5
		centerY := (inferRes.ViewPort.Top + inferRes.ViewPort.Bottom) * 0.5
		if !doDragViewport(ca, &inferRes.ViewPort, targetInViewX-centerX, targetInViewY-centerY, panFactor, rand.Intn(3)+1) {
			remaining = remaining[1:]
			pickAttempt = 0
			continue
		}

		// Fresh screenshot + infer after drag.
		ca.TouchMove(0, 1, 1, 0)
		ctrl.PostScreencap().Wait()
		img, err = ctrl.CacheImage()
		if err != nil {
			log.Error().Err(err).Msg("Failed to get cached image in multi-viewport search")
			return nil, false
		}
		inferRes, err = r.inferResultWithImg(ctx, arg, img, &param.MapTrackerBigMapInferParam)
		if err != nil {
			log.Error().Err(err).Msg("Failed to infer viewport in multi-viewport search")
			return nil, false
		}

		remaining = filterPointsInView(remaining, &inferRes.ViewPort)
		r.matchCurrentViewport(tpl, param, &inferRes.ViewPort, img, &allMatches)
		log.Info().Int("remaining", len(remaining)).Int("totalMatches", len(allMatches)).Msg("Multi-viewport search progress")
	}

	sort.Slice(allMatches, func(i, j int) bool {
		return allMatches[i].Conf > allMatches[j].Conf
	})
	allMatches = deduplicateMatches(allMatches)
	if len(allMatches) > param.MaxMatches {
		allMatches = allMatches[:param.MaxMatches]
	}

	saveFindImageDebug(inferRes.MapName, tpl, allMatches)
	return r.buildResult(arg, param, inferRes.MapName, allMatches, "Big-map multi-viewport FindImage completed")
}

func (r *MapTrackerBigMapFindImage) buildResult(
	arg *maa.CustomRecognitionArg,
	param *MapTrackerBigMapFindImageParam,
	mapName string,
	matches []MapTrackerBigMapFindImageMatch,
	message string,
) (*maa.CustomRecognitionResult, bool) {
	for _, m := range matches {
		log.Debug().
			Str("template", param.Template).
			Float64("vx", m.ScreenX).
			Float64("vy", m.ScreenY).
			Float64("conf", m.Conf).
			Float64("rot", m.Rotation).
			Msg("FindImage match")
	}

	// Backfill the inferred map name into each match for downstream consumers.
	for i := range matches {
		matches[i].MapName = mapName
	}

	detailJSON, err := json.Marshal(matches)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal MapTrackerBigMapFindImage result")
		return nil, false
	}

	hit := param.Expected.isSatisfied(mapName, matches)
	log.Info().
		Str("template", param.Template).
		Int("matches", len(matches)).
		Bool("hit", hit).
		Bool("greenMask", param.GreenMask).
		Bool("withRotation", param.WithRotation).
		Msg(message)

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, hit
}

func (r *MapTrackerBigMapFindImage) inferResultWithImg(ctx *maa.Context, arg *maa.CustomRecognitionArg, img any, inferParam *MapTrackerBigMapInferParam) (*MapTrackerBigMapInferResult, error) {
	paramJSON := ""
	if inferParam != nil {
		paramBytes, err := json.Marshal(inferParam)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal MapTrackerBigMapInfer parameters: %w", err)
		}
		paramJSON = string(paramBytes)
	}

	resultWrapper, hit := MapTrackerBigMapInferRunner.Run(ctx, &maa.CustomRecognitionArg{
		TaskID:                 arg.TaskID,
		CurrentTaskName:        arg.CurrentTaskName,
		CustomRecognitionName:  "MapTrackerBigMapInfer",
		CustomRecognitionParam: paramJSON,
		Img:                    img.(image.Image),
		Roi:                    arg.Roi,
	})
	if !hit {
		return nil, fmt.Errorf("big-map inference not hit")
	}
	if resultWrapper == nil || resultWrapper.Detail == "" {
		return nil, fmt.Errorf("big-map inference result is empty")
	}

	var result MapTrackerBigMapInferResult
	if err := json.Unmarshal([]byte(resultWrapper.Detail), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal big-map inference result: %w", err)
	}
	if result.ViewPort.Scale <= 0 {
		return nil, fmt.Errorf("invalid inferred scale: %f", result.ViewPort.Scale)
	}

	return &result, nil
}

// filterPointsInView removes points that are within the viewport and returns the remaining.
func filterPointsInView(points [][2]int, viewport *BigMapViewport) [][2]int {
	remaining := points[:0]
	for _, p := range points {
		if !viewport.IsMapCoordInViewWithPadding(float64(p[0]), float64(p[1]), SCAN_VIEWPORT_PADDING) {
			remaining = append(remaining, p)
		}
	}
	return remaining
}

func (r *MapTrackerBigMapFindImage) matchCurrentViewport(tpl *minicv.Template, param *MapTrackerBigMapFindImageParam, viewport *BigMapViewport, img any, result *[]MapTrackerBigMapFindImageMatch) {
	screenImg := minicv.ImageConvertRGBA(img.(image.Image))
	cropped, _, _, ok := cropBigMapTemplate(screenImg)
	screenImg = nil // Release full-screen RGBA immediately.
	if !ok {
		log.Warn().Msg("Big-map crop area is invalid during full scan")
		return
	}

	matches := r.matchTemplate(cropped, tpl, param, viewport)

	// Deduplicate by map coordinates against previously accumulated results.
	var newMatches []MapTrackerBigMapFindImageMatch
	for _, m := range matches {
		isDup := false
		for _, existing := range *result {
			dx := m.MapX - existing.MapX
			dy := m.MapY - existing.MapY
			if dx*dx+dy*dy < NMS_MIN_DISTANCE*NMS_MIN_DISTANCE {
				isDup = true
				break
			}
		}
		if !isDup {
			newMatches = append(newMatches, m)
		}
	}

	*result = append(*result, newMatches...)
}

func (r *MapTrackerBigMapFindImage) matchTemplate(
	cropped *image.RGBA,
	tpl *minicv.Template,
	param *MapTrackerBigMapFindImageParam,
	viewport *BigMapViewport,
) []MapTrackerBigMapFindImageMatch {
	tplImg := tpl.Image
	tplStats := tpl.Stats
	halfW := float64(tplImg.Rect.Dx()) / 2.0
	halfH := float64(tplImg.Rect.Dy()) / 2.0

	var allMatches []MapTrackerBigMapFindImageMatch
	if param.WithRotation {
		allMatches = matchFindImageWithRotation(cropped, tplImg, tplStats, param, viewport, halfW, halfH)
	} else {
		allMatches = collectFindImageMatches(cropped, tplImg, tplStats, param, func(mx, my, score float64) MapTrackerBigMapFindImageMatch {
			vx := mx + halfW + viewport.Left
			vy := my + halfH + viewport.Top
			return newFindImageMatch(viewport, vx, vy, score, 0)
		})
	}

	sort.Slice(allMatches, func(i, j int) bool {
		return allMatches[i].Conf > allMatches[j].Conf
	})

	if param.WithRotation && len(allMatches) > 1 {
		allMatches = deduplicateMatches(allMatches)
	}
	if len(allMatches) > param.MaxMatches {
		allMatches = allMatches[:param.MaxMatches]
	}

	return allMatches
}

func matchFindImageWithRotation(
	cropped *image.RGBA,
	tplImg *image.RGBA,
	tplStats minicv.StatsResult,
	param *MapTrackerBigMapFindImageParam,
	viewport *BigMapViewport,
	halfW float64,
	halfH float64,
) []MapTrackerBigMapFindImageMatch {
	type angleResult struct {
		matches []MapTrackerBigMapFindImageMatch
	}

	numAngles := int(360.0 / ROTATION_STEP_DEG)
	resChan := make(chan angleResult, numAngles)
	var wg sync.WaitGroup

	cx := float64(cropped.Rect.Dx()) / 2.0
	cy := float64(cropped.Rect.Dy()) / 2.0

	for angle := 0.0; angle < 360.0; angle += ROTATION_STEP_DEG {
		wg.Add(1)
		go func(a float64) {
			defer wg.Done()

			rotatedSrc := minicv.ImageRotate(cropped, a)
			rad := a * math.Pi / 180.0
			cosA, sinA := math.Cos(rad), math.Sin(rad)

			matches := collectFindImageMatches(rotatedSrc, tplImg, tplStats, param, func(mx, my, score float64) MapTrackerBigMapFindImageMatch {
				dx := mx + halfW - cx
				dy := my + halfH - cy
				origX := dx*cosA + dy*sinA + cx
				origY := -dx*sinA + dy*cosA + cy
				return newFindImageMatch(viewport, origX+viewport.Left, origY+viewport.Top, score, a)
			})

			resChan <- angleResult{matches: matches}
		}(angle)
	}

	go func() {
		wg.Wait()
		close(resChan)
	}()

	var allMatches []MapTrackerBigMapFindImageMatch
	for res := range resChan {
		allMatches = append(allMatches, res.matches...)
	}
	return allMatches
}

func collectFindImageMatches(
	img *image.RGBA,
	tplImg *image.RGBA,
	tplStats minicv.StatsResult,
	param *MapTrackerBigMapFindImageParam,
	buildMatch func(mx, my, score float64) MapTrackerBigMapFindImageMatch,
) []MapTrackerBigMapFindImageMatch {
	imgIntArr := minicv.GetIntegralArray(img)
	var hits []minicv.MatchTemplateHit
	if param.GreenMask {
		hits = minicv.MatchTemplateMultiHitWithMask(
			img,
			imgIntArr,
			tplImg,
			tplStats,
			GREEN_SCREEN_MASK_COLOR_RGB888,
			param.Threshold,
			param.MaxMatches,
		)
	} else {
		hits = minicv.MatchTemplateMultiHit(
			img,
			imgIntArr,
			tplImg,
			tplStats,
			param.Threshold,
			param.MaxMatches,
		)
	}

	matches := make([]MapTrackerBigMapFindImageMatch, 0, len(hits))
	for _, hit := range hits {
		matches = append(matches, buildMatch(hit.X, hit.Y, hit.Val))
	}
	return matches
}

func newFindImageMatch(
	viewport *BigMapViewport,
	vx float64,
	vy float64,
	score float64,
	rotation float64,
) MapTrackerBigMapFindImageMatch {
	mapX, mapY := viewport.GetMapCoordOf(vx, vy)
	return MapTrackerBigMapFindImageMatch{
		ScreenX:  roundTo1Decimal(vx),
		ScreenY:  roundTo1Decimal(vy),
		MapX:     roundTo1Decimal(mapX),
		MapY:     roundTo1Decimal(mapY),
		Conf:     math.Round(score*10000) / 10000,
		Rotation: rotation,
	}
}

func deduplicateMatches(matches []MapTrackerBigMapFindImageMatch) []MapTrackerBigMapFindImageMatch {
	if len(matches) <= 1 {
		return matches
	}

	keep := make([]bool, len(matches))
	for i := range keep {
		keep[i] = true
	}

	for i := range len(matches) {
		if !keep[i] {
			continue
		}
		for j := i + 1; j < len(matches); j++ {
			if !keep[j] {
				continue
			}
			dx := matches[i].ScreenX - matches[j].ScreenX
			dy := matches[i].ScreenY - matches[j].ScreenY
			if math.Sqrt(dx*dx+dy*dy) < NMS_MIN_DISTANCE {
				// Keep the one with higher conf (list is sorted, so i has higher or equal conf)
				keep[j] = false
			}
		}
	}

	result := make([]MapTrackerBigMapFindImageMatch, 0, len(matches))
	for i, k := range keep {
		if k {
			result = append(result, matches[i])
		}
	}
	return result
}

func saveFindImageDebug(mapName string, tpl *minicv.Template, matches []MapTrackerBigMapFindImageMatch) {
	if len(matches) == 0 {
		return
	}

	// Find the map image from the preloaded cache.
	var mapImg *image.RGBA
	var offsetX, offsetY int
	found := false
	for i := range internal.Resource.RawMaps {
		m := &internal.Resource.RawMaps[i]
		if m.Name == mapName {
			mapImg = m.Img
			offsetX = m.OffsetX
			offsetY = m.OffsetY
			found = true
			break
		}
	}
	if !found {
		log.Warn().Str("map", mapName).Msg("Map not found in cache, skipping debug visualization")
		return
	}

	canvas := minicv.ImageCopy(mapImg)
	tplImg := tpl.Image
	tplW := tplImg.Rect.Dx()
	tplH := tplImg.Rect.Dy()

	// Paste the template image at each match location.
	// MapX/MapY are center coordinates; convert to left-top for drawing.
	for _, m := range matches {
		ltX := int(math.Round(m.MapX)) - offsetX - tplW/2
		ltY := int(math.Round(m.MapY)) - offsetY - tplH/2
		draw.Draw(canvas, image.Rect(ltX, ltY, ltX+tplW, ltY+tplH), tplImg, image.Point{}, draw.Over)
	}

	if err := minicv.ImageSaveDebug(canvas, "debug/vision", "MapTrackerBigMapFindImage", 4); err != nil {
		log.Warn().Err(err).Msg("Failed to save FindImage debug image")
		return
	}

	log.Info().Str("path", "debug/vision").Str("map", mapName).Int("matches", len(matches)).Msg("FindImage debug visualization saved")
}
