// Copyright (c) 2026 Harry Huang
package maptracker

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"math"
	"regexp"
	"sync"
	"time"

	mt "github.com/MaaXYZ/MaaEnd/agent/go-service/map-tracker/internal"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type BigMapViewport = mt.BigMapViewport

// MapTrackerBigMapInferResult represents the output of big map inference.
type MapTrackerBigMapInferResult struct {
	MapName     string            `json:"mapName"`
	ViewPort    mt.BigMapViewport `json:"viewPort"`
	InferTimeMs int64             `json:"inferTimeMs"`
}

// MapTrackerBigMapInferParam represents the custom_recognition_param for MapTrackerBigMapInfer.
type MapTrackerBigMapInferParam struct {
	MapNameRegex string  `json:"map_name_regex,omitempty"`
	Threshold    float64 `json:"threshold,omitempty"`
}

var mapTrackerBigMapInferDefaultParam = MapTrackerBigMapInferParam{
	MapNameRegex: "^map\\d+_lv\\d+$",
	Threshold:    0.5,
}

// MapTrackerBigMapInfer is the custom recognition component for big-map location inference.
type MapTrackerBigMapInfer struct {
	mapsOnce sync.Once
	mapsErr  error

	scaledMapsMu sync.Mutex
	scaledMaps   []mt.MapCache
	scaledScale  float64
}

var _ maa.CustomRecognitionRunner = &MapTrackerBigMapInfer{}
var mapTrackerBigMapInferRunner maa.CustomRecognitionRunner = &MapTrackerBigMapInfer{}

// Run implements maa.CustomRecognitionRunner.
func (r *MapTrackerBigMapInfer) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	t0 := time.Now()

	param, err := r.parseParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerBigMapInfer")
		return nil, false
	}

	mapNameRegex, err := regexp.Compile(param.MapNameRegex)
	if err != nil {
		log.Error().Err(err).Str("regex", param.MapNameRegex).Msg("Invalid map_name_regex")
		return nil, false
	}

	r.initMaps(ctx)
	if r.mapsErr != nil {
		log.Error().Err(r.mapsErr).Msg("Failed to initialize maps for MapTrackerBigMapInfer")
		return nil, false
	}

	screenImg := minicv.ImageConvertRGBA(arg.Img)
	template, fullLeft, fullTop, ok := cropBigMapTemplate(screenImg)
	if !ok {
		log.Warn().Msg("Big-map crop area is invalid")
		return nil, false
	}

	sampleTemplate, sampleOffsetX, sampleOffsetY, ok := cropBigMapSample(template, fullLeft, fullTop, screenImg.Rect.Dx(), screenImg.Rect.Dy())
	if !ok {
		log.Warn().Msg("Big-map sample crop area is invalid")
		return nil, false
	}

	fastTpl := minicv.ImageScale(sampleTemplate, mt.WIRE_MATCH_PRECISION)
	fastTplStats := minicv.GetImageStats(fastTpl)
	if fastTplStats.Std < 1e-6 {
		log.Warn().Msg("Big-map template standard deviation is too small")
		return nil, false
	}

	coarseBestScore := -1.0
	coarseBestTplScale := 0.0
	var coarseBestMap *mt.MapCache
	hasCoarseBestMap := false
	triedMaps := 0
	coarseMatchingSteps := []int{12}
	coarseTplScaleMin := 1.0 / mt.GAME_MAP_SCALE_MAX
	coarseTplScaleMax := 1.0 / mt.GAME_MAP_SCALE_MIN

	scaledMaps := r.getScaledMaps(mt.WIRE_MATCH_PRECISION)
	candidateMaps := make([]*mt.MapCache, 0, len(scaledMaps))
	for idx := range scaledMaps {
		m := &scaledMaps[idx]
		if mapNameRegex.MatchString(m.Name) {
			candidateMaps = append(candidateMaps, m)
		}
	}
	triedMaps = len(candidateMaps)

	type coarseResult struct {
		score    float64
		tplScale float64
		m        *mt.MapCache
	}

	if triedMaps == 1 {
		single := candidateMaps[0]
		_, _, score, tplScale := minicv.MatchTemplateAnyScale(
			single.Img,
			single.GetIntegralArray(),
			fastTpl,
			coarseTplScaleMin,
			coarseTplScaleMax,
			coarseMatchingSteps,
		)
		coarseBestScore = score
		coarseBestTplScale = tplScale
		coarseBestMap = single
		hasCoarseBestMap = true
	} else if triedMaps > 1 {
		resChan := make(chan coarseResult, triedMaps)
		var wg sync.WaitGroup

		for _, mapData := range candidateMaps {
			wg.Add(1)
			go func(m *mt.MapCache) {
				defer wg.Done()
				_, _, score, tplScale := minicv.MatchTemplateAnyScale(
					m.Img,
					m.GetIntegralArray(),
					fastTpl,
					coarseTplScaleMin,
					coarseTplScaleMax,
					coarseMatchingSteps,
				)
				resChan <- coarseResult{score: score, tplScale: tplScale, m: m}
			}(mapData)
		}

		go func() {
			wg.Wait()
			close(resChan)
		}()

		for res := range resChan {
			if res.score > coarseBestScore {
				coarseBestScore = res.score
				coarseBestTplScale = res.tplScale
				coarseBestMap = res.m
				hasCoarseBestMap = true
			}
		}
	}

	if triedMaps == 0 {
		log.Warn().Str("regex", mapNameRegex.String()).Msg("No maps matched regex for big-map inference")
		return nil, false
	}

	if !hasCoarseBestMap {
		log.Warn().Msg("Big-map coarse matching did not produce a candidate")
		return nil, false
	}

	fineMatchingSteps := []int{4, 2}
	fineMatchingScaleOffset := (coarseTplScaleMax - coarseTplScaleMin) / 24.0
	fineMinScale := max(coarseTplScaleMin, coarseBestTplScale-fineMatchingScaleOffset)
	fineMaxScale := min(coarseTplScaleMax, coarseBestTplScale+fineMatchingScaleOffset)

	matchX, matchY, fineScore, fineTplScale := minicv.MatchTemplateAnyScale(
		coarseBestMap.Img,
		coarseBestMap.GetIntegralArray(),
		fastTpl,
		fineMinScale,
		fineMaxScale,
		fineMatchingSteps,
	)

	if fineScore < param.Threshold {
		log.Info().
			Int("triedMaps", triedMaps).
			Str("map", coarseBestMap.Name).
			Float64("coarseScore", coarseBestScore).
			Float64("fineScore", fineScore).
			Float64("threshold", param.Threshold).
			Msg("Big-map inference confidence below threshold")
		return nil, false
	}

	viewScale := 1.0 / fineTplScale
	viewScale = min(mt.GAME_MAP_SCALE_MAX, max(mt.GAME_MAP_SCALE_MIN, viewScale))
	sampleOriginMapX := matchX/float64(mt.WIRE_MATCH_PRECISION) + float64(coarseBestMap.OffsetX)
	sampleOriginMapY := matchY/float64(mt.WIRE_MATCH_PRECISION) + float64(coarseBestMap.OffsetY)
	viewOriginMapX := roundTo1Decimal(sampleOriginMapX - float64(sampleOffsetX)/viewScale)
	viewOriginMapY := roundTo1Decimal(sampleOriginMapY - float64(sampleOffsetY)/viewScale)

	result := MapTrackerBigMapInferResult{
		MapName: coarseBestMap.Name,
		ViewPort: *mt.NewBigMapViewport(
			viewOriginMapX,
			viewOriginMapY,
			viewScale,
		),
		InferTimeMs: time.Since(t0).Milliseconds(),
	}

	detailJSON, err := json.Marshal(result)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal MapTrackerBigMapInfer result")
		return nil, false
	}

	log.Info().
		Int("triedMaps", triedMaps).
		Str("map", result.MapName).
		Float64("coarseScore", coarseBestScore).
		Float64("fineScore", fineScore).
		Float64("x", result.ViewPort.OriginMapX).
		Float64("y", result.ViewPort.OriginMapY).
		Float64("scale", result.ViewPort.Scale).
		Int64("inferTimeMs", result.InferTimeMs).
		Msg("Big-map inference completed")

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

func (r *MapTrackerBigMapInfer) parseParam(paramStr string) (*MapTrackerBigMapInferParam, error) {
	if paramStr == "" {
		return &mapTrackerBigMapInferDefaultParam, nil
	}

	var param MapTrackerBigMapInferParam
	if err := json.Unmarshal([]byte(paramStr), &param); err != nil {
		return nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
	}

	if param.MapNameRegex == "" {
		param.MapNameRegex = mapTrackerBigMapInferDefaultParam.MapNameRegex
	}
	if param.Threshold == 0.0 {
		param.Threshold = mapTrackerBigMapInferDefaultParam.Threshold
	} else if param.Threshold < 0.0 || param.Threshold > 1.0 {
		return nil, fmt.Errorf("invalid threshold value: %f", param.Threshold)
	}

	return &param, nil
}

// initMaps initializes map cache for big-map inference only.
func (r *MapTrackerBigMapInfer) initMaps(ctx *maa.Context) {
	r.mapsOnce.Do(func() {
		mt.Resource.InitRawMaps(ctx)
		if mt.Resource.RawMapsErr != nil {
			r.mapsErr = mt.Resource.RawMapsErr
			return
		}
		log.Info().Int("mapsCount", len(mt.Resource.RawMaps)).Msg("Big-map maps cache initialized")
	})
}

// getScaledMaps recomputes scaled map cache for the requested scale.
func (r *MapTrackerBigMapInfer) getScaledMaps(scale float64) []mt.MapCache {
	r.scaledMapsMu.Lock()
	defer r.scaledMapsMu.Unlock()

	if r.scaledMaps != nil && math.Abs(r.scaledScale-scale) < 1e-6 {
		return r.scaledMaps
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

	r.scaledMaps = newScaled
	r.scaledScale = scale
	return r.scaledMaps
}

func cropBigMapTemplate(screen *image.RGBA) (*image.RGBA, int, int, bool) {
	w, h := screen.Rect.Dx(), screen.Rect.Dy()
	padLR := int(math.Round(mt.PADDING_LR))
	padTB := int(math.Round(mt.PADDING_TB))

	left := max(0, min(w, padLR))
	right := max(0, min(w, w-padLR))
	top := max(0, min(h, padTB))
	bottom := max(0, min(h, h-padTB))

	if right <= left || bottom <= top {
		return nil, 0, 0, false
	}

	region := image.Rect(left, top, right, bottom)
	dst := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	draw.Draw(dst, dst.Bounds(), screen, region.Min, draw.Src)

	return dst, left, top, true
}

func cropBigMapSample(fullTemplate *image.RGBA, fullLeft, fullTop, screenW, screenH int) (*image.RGBA, int, int, bool) {
	sampleLeftAbs := int(math.Round(mt.SAMPLE_PADDING_LR))
	sampleTopAbs := int(math.Round(mt.SAMPLE_PADDING_TB))
	sampleRightAbs := screenW - sampleLeftAbs
	sampleBottomAbs := screenH - sampleTopAbs

	fullRight := fullLeft + fullTemplate.Rect.Dx()
	fullBottom := fullTop + fullTemplate.Rect.Dy()

	leftAbs := max(fullLeft, min(fullRight, sampleLeftAbs))
	rightAbs := max(fullLeft, min(fullRight, sampleRightAbs))
	topAbs := max(fullTop, min(fullBottom, sampleTopAbs))
	bottomAbs := max(fullTop, min(fullBottom, sampleBottomAbs))

	if rightAbs <= leftAbs || bottomAbs <= topAbs {
		return nil, 0, 0, false
	}

	leftRel := leftAbs - fullLeft
	topRel := topAbs - fullTop
	rightRel := rightAbs - fullLeft
	bottomRel := bottomAbs - fullTop

	region := image.Rect(leftRel, topRel, rightRel, bottomRel)
	dst := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	draw.Draw(dst, dst.Bounds(), fullTemplate, region.Min, draw.Src)

	return dst, leftRel, topRel, true
}
