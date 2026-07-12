// Copyright (c) 2026 Harry Huang
package maptrackercompatible

import (
	"fmt"
	"image"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	maptrackerinternal "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/internal"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/rs/zerolog/log"
)

const (
	mapLocatorImageDir         = "resource/image/MapLocator"
	compatibleTemplateMinScore = 0.80
	compatibleTemplateMinScale = 0.90
	compatibleTemplateMaxScale = 1.12
	compatibleTransformMargin  = 8.0
)

type compatibleSourcePoint struct {
	SourceName string
	X          float64
	Y          float64
}

type compatibleConvertedPath struct {
	MapName string
	Path    [][2]float64
}

type compatibleSourceRect struct {
	SourceName string
	X          float64
	Y          float64
	W          float64
	H          float64
}

type compatibleConvertedRect struct {
	MapName string
	Target  [4]float64
}

type compatibleTrackerPoint struct {
	MapName string
	X       float64
	Y       float64
}

type compatibleLocatorSource struct {
	Region       string
	MapPrefix    string
	LocatorFile  string
	CandidateMap string
	IsBase       bool
}

type compatibleMatchImage struct {
	Img      *image.RGBA
	Integral minicv.IntegralArray
}

type compatibleTransform struct {
	SourceName string
	MapName    string
	OffsetX    float64
	OffsetY    float64
	Scale      float64
	Score      float64
	Width      int
	Height     int
}

type compatibleCoordinateMapper struct {
	preferredMapName string
}

var compatibleTransformCache = struct {
	sync.RWMutex
	Transforms map[string]compatibleTransform
}{Transforms: map[string]compatibleTransform{}}

var compatibleTrackerMapCache = struct {
	sync.Mutex
	ByPrefix map[string][]string
}{ByPrefix: map[string][]string{}}

// compatibleZoneRegexp matches MapLocator tier zone IDs like ValleyIV_L1_114.
var compatibleZoneRegexp = regexp.MustCompile(`^(ValleyIV|Wuling)_L(\d+)_(\d+)$`)

// compatibleTrackerMapRegexp matches MapTracker map names like map01_lv001 or map01_lv003_tier_123.
var compatibleTrackerMapRegexp = regexp.MustCompile(`^((?:map0[12])|(?:base01))_lv(\d{3})(?:_tier_(\d+))?$`)

func convertCompatiblePoints(preferredMapName string, points []compatibleSourcePoint) (compatibleConvertedPath, error) {
	if len(points) == 0 {
		return compatibleConvertedPath{}, fmt.Errorf("MapTrackerMoveCompatible requires at least one coordinate waypoint")
	}

	mapper := &compatibleCoordinateMapper{preferredMapName: preferredMapName}
	path := make([][2]float64, 0, len(points))
	moveMapName := ""
	for _, sourcePoint := range points {
		point, err := mapper.mapPoint(sourcePoint.SourceName, sourcePoint.X, sourcePoint.Y)
		if err != nil {
			return compatibleConvertedPath{}, err
		}
		rootMapName := compatibleRootMapName(point.MapName)
		if moveMapName == "" {
			moveMapName = rootMapName
		} else if moveMapName != rootMapName {
			return compatibleConvertedPath{}, fmt.Errorf("MapTrackerMove cannot represent route across multiple levels: %s and %s", moveMapName, rootMapName)
		}
		path = append(path, [2]float64{point.X, point.Y})
	}

	if moveMapName == "" {
		return compatibleConvertedPath{}, fmt.Errorf("failed to resolve converted map_name")
	}
	return compatibleConvertedPath{MapName: moveMapName, Path: path}, nil
}

func convertCompatibleRect(preferredMapName string, rect compatibleSourceRect) (compatibleConvertedRect, error) {
	if rect.W <= 0 || rect.H <= 0 {
		return compatibleConvertedRect{}, fmt.Errorf("target width and height must be positive")
	}

	mapper := &compatibleCoordinateMapper{preferredMapName: preferredMapName}
	centerX := rect.X + rect.W/2.0
	centerY := rect.Y + rect.H/2.0
	transform, err := mapper.resolveTransform(rect.SourceName, centerX, centerY)
	if err != nil {
		return compatibleConvertedRect{}, err
	}

	left := transform.OffsetX + rect.X*transform.Scale
	top := transform.OffsetY + rect.Y*transform.Scale
	right := transform.OffsetX + (rect.X+rect.W)*transform.Scale
	bottom := transform.OffsetY + (rect.Y+rect.H)*transform.Scale
	if right < left {
		left, right = right, left
	}
	if bottom < top {
		top, bottom = bottom, top
	}
	return compatibleConvertedRect{
		MapName: compatibleRootMapName(transform.MapName),
		Target:  [4]float64{left, top, right - left, bottom - top},
	}, nil
}

func (m *compatibleCoordinateMapper) mapPoint(sourceName string, x, y float64) (compatibleTrackerPoint, error) {
	transform, err := m.resolveTransform(sourceName, x, y)
	if err != nil {
		return compatibleTrackerPoint{}, err
	}
	return compatibleTrackerPoint{
		MapName: transform.MapName,
		X:       transform.OffsetX + x*transform.Scale,
		Y:       transform.OffsetY + y*transform.Scale,
	}, nil
}

func (m *compatibleCoordinateMapper) resolveTransform(sourceName string, x, y float64) (compatibleTransform, error) {
	// 1. Resolve Navigator zone or Tracker map name to concrete image candidates.
	source, err := resolveCompatibleSourceMap(sourceName, m.preferredMapName)
	if err != nil {
		return compatibleTransform{}, err
	}

	// 2. Reuse cached transforms before loading any image material.
	if transform, ok := m.tryKnownTransforms(source, x, y); ok {
		return transform, nil
	}

	// 3. Match images once and cache only the lightweight transform.
	transform, err := matchCompatibleTransform(source, x, y)
	if err != nil {
		return compatibleTransform{}, err
	}
	storeCompatibleTransform(source.transformKey(transform.MapName), transform)
	return transform, nil
}

func (m *compatibleCoordinateMapper) tryKnownTransforms(source compatibleLocatorSource, x, y float64) (compatibleTransform, bool) {
	compatibleTransformCache.RLock()
	defer compatibleTransformCache.RUnlock()
	for _, transform := range compatibleTransformCache.Transforms {
		if transform.SourceName != source.SourceName() || !source.acceptsCandidate(transform.MapName) {
			continue
		}
		tx := transform.OffsetX + x*transform.Scale
		ty := transform.OffsetY + y*transform.Scale
		if tx >= -compatibleTransformMargin && tx < float64(transform.Width)+compatibleTransformMargin &&
			ty >= -compatibleTransformMargin && ty < float64(transform.Height)+compatibleTransformMargin {
			return transform, true
		}
	}
	return compatibleTransform{}, false
}

func storeCompatibleTransform(key string, transform compatibleTransform) {
	compatibleTransformCache.Lock()
	compatibleTransformCache.Transforms[key] = transform
	compatibleTransformCache.Unlock()
}

func resolveCompatibleSourceMap(sourceName string, preferredMapName string) (compatibleLocatorSource, error) {
	// 1. Accept Tracker map names directly.
	if sourceName == "" {
		return compatibleLocatorSource{}, fmt.Errorf("empty source map name")
	}

	if info, ok := parseCompatibleTrackerMapName(sourceName); ok {
		return info, nil
	}
	// 2. Resolve special one-file base maps.
	if sourceName == "OMVBase01" {
		return compatibleLocatorSource{Region: "OMVBase", MapPrefix: "base01", LocatorFile: "OMVBase01.png", CandidateMap: "base01_lv003", IsBase: true}, nil
	}
	// 3. Resolve region Base zones such as ValleyIV_Base.
	if strings.HasSuffix(sourceName, "_Base") {
		region := strings.TrimSuffix(sourceName, "_Base")
		prefix, ok := compatibleRegionMapPrefix(region)
		if !ok {
			return compatibleLocatorSource{}, fmt.Errorf("unsupported MapNavigator region: %s", region)
		}
		candidate := ""
		if strings.HasPrefix(preferredMapName, prefix+"_lv") && !strings.Contains(preferredMapName, "_tier_") {
			candidate = preferredMapName
		}
		return compatibleLocatorSource{Region: region, MapPrefix: prefix, LocatorFile: "Base.png", CandidateMap: candidate, IsBase: true}, nil
	}
	// 4. Resolve tier zones such as ValleyIV_L1_114.
	if matches := compatibleZoneRegexp.FindStringSubmatch(sourceName); len(matches) == 4 {
		region := matches[1]
		prefix, ok := compatibleRegionMapPrefix(region)
		if !ok {
			return compatibleLocatorSource{}, fmt.Errorf("unsupported MapNavigator region: %s", region)
		}
		level, err := strconv.Atoi(matches[2])
		if err != nil {
			return compatibleLocatorSource{}, fmt.Errorf("invalid MapNavigator level: %s", matches[2])
		}
		tier := matches[3]
		return compatibleLocatorSource{
			Region:       region,
			MapPrefix:    prefix,
			LocatorFile:  fmt.Sprintf("Lv%03dTier%s.png", level, tier),
			CandidateMap: fmt.Sprintf("%s_lv%03d_tier_%s", prefix, level, tier),
		}, nil
	}
	return compatibleLocatorSource{}, fmt.Errorf("unsupported MapNavigator map name: %s", sourceName)
}

func parseCompatibleTrackerMapName(mapName string) (compatibleLocatorSource, bool) {
	matches := compatibleTrackerMapRegexp.FindStringSubmatch(mapName)
	if len(matches) != 4 {
		return compatibleLocatorSource{}, false
	}
	region, ok := compatibleMapPrefixRegion(matches[1])
	if !ok {
		return compatibleLocatorSource{}, false
	}
	if matches[3] != "" {
		return compatibleLocatorSource{
			Region:       region,
			MapPrefix:    matches[1],
			LocatorFile:  fmt.Sprintf("Lv%sTier%s.png", matches[2], matches[3]),
			CandidateMap: mapName,
		}, true
	}
	return compatibleLocatorSource{
		Region:       region,
		MapPrefix:    matches[1],
		LocatorFile:  "Base.png",
		CandidateMap: mapName,
		IsBase:       true,
	}, true
}

func matchCompatibleTransform(source compatibleLocatorSource, x, y float64) (compatibleTransform, error) {
	// 1. Crop a template around the source waypoint from the Locator image.
	locator, err := loadCompatibleImage(filepath.Join(mapLocatorImageDir, source.Region, source.LocatorFile))
	if err != nil {
		return compatibleTransform{}, err
	}

	// 2. Pick candidate Tracker level/tier images from naming rules.
	candidates, err := source.candidateMaps()
	if err != nil {
		return compatibleTransform{}, err
	}
	if len(candidates) == 0 {
		return compatibleTransform{}, fmt.Errorf("no MapTracker candidates for %s", source.SourceName())
	}

	// 3. Search scale and offset by template matching.
	best := compatibleTransform{Score: -1}
	for _, cropSize := range []int{64, 96, 128} {
		crop, cropLeft, cropTop, ok := cropCompatibleTemplate(locator.Img, x, y, cropSize)
		if !ok {
			continue
		}
		cropStats := minicv.GetImageStats(crop)
		if cropStats.Std < 1e-6 {
			continue
		}
		for _, mapName := range candidates {
			tracker, err := loadCompatibleImage(filepath.Join(maptrackerinternal.MAP_DIR, mapName+".png"))
			if err != nil {
				continue
			}
			matchX, matchY, score, scale := minicv.MatchTemplateAnyScale(
				tracker.Img,
				tracker.Integral,
				crop,
				compatibleTemplateMinScale,
				compatibleTemplateMaxScale,
				[]int{4, 4},
			)
			if score <= best.Score {
				continue
			}
			best = compatibleTransform{
				SourceName: source.SourceName(),
				MapName:    mapName,
				OffsetX:    matchX - float64(cropLeft)*scale,
				OffsetY:    matchY - float64(cropTop)*scale,
				Scale:      scale,
				Score:      score,
				Width:      tracker.Img.Bounds().Dx(),
				Height:     tracker.Img.Bounds().Dy(),
			}
		}
		if best.Score >= 0.90 {
			break
		}
	}

	// 4. Keep only high-confidence transforms.
	if best.Score < compatibleTemplateMinScore {
		return compatibleTransform{}, fmt.Errorf("failed to match MapNavigator point to MapTracker map: source=%s x=%.1f y=%.1f score=%.3f", source.SourceName(), x, y, best.Score)
	}
	log.Info().
		Str("source", source.SourceName()).
		Str("map", best.MapName).
		Float64("score", best.Score).
		Float64("scale", best.Scale).
		Msg("MapTrackerMoveCompatible matched runtime coordinate transform")
	return best, nil
}

func cropCompatibleTemplate(img *image.RGBA, x, y float64, size int) (*image.RGBA, int, int, bool) {
	if img == nil || size <= 0 {
		return nil, 0, 0, false
	}
	cx := int(math.Round(x))
	cy := int(math.Round(y))
	rect := image.Rect(cx-size/2, cy-size/2, cx+size/2, cy+size/2).Intersect(img.Bounds())
	if rect.Dx() < 24 || rect.Dy() < 24 {
		return nil, 0, 0, false
	}
	return minicv.ImageCropRect(img, rect), rect.Min.X, rect.Min.Y, true
}

func loadCompatibleImage(relativePath string) (*compatibleMatchImage, error) {
	resolvedPath := resource.FindResource(relativePath)
	if resolvedPath == "" {
		return nil, fmt.Errorf("resource not found: %s", relativePath)
	}

	file, err := os.Open(resolvedPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}
	rgba := minicv.ImageConvertRGBA(img)
	return &compatibleMatchImage{Img: rgba, Integral: minicv.GetIntegralArray(rgba)}, nil
}

func (s compatibleLocatorSource) candidateMaps() ([]string, error) {
	if s.CandidateMap != "" {
		return []string{s.CandidateMap}, nil
	}
	if !s.IsBase {
		return nil, fmt.Errorf("missing tier candidate map for %s", s.SourceName())
	}
	return compatibleTrackerBaseMaps(s.MapPrefix)
}

func compatibleTrackerBaseMaps(prefix string) ([]string, error) {
	compatibleTrackerMapCache.Lock()
	if cached, ok := compatibleTrackerMapCache.ByPrefix[prefix]; ok {
		compatibleTrackerMapCache.Unlock()
		return cached, nil
	}
	compatibleTrackerMapCache.Unlock()

	mapDir := resource.FindResource(maptrackerinternal.MAP_DIR)
	if mapDir == "" {
		return nil, fmt.Errorf("MapTracker map directory not found")
	}
	entries, err := os.ReadDir(mapDir)
	if err != nil {
		return nil, err
	}

	maps := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".png") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".png")
		if strings.HasPrefix(name, prefix+"_lv") && !strings.Contains(name, "_tier_") {
			maps = append(maps, name)
		}
	}
	sort.Strings(maps)

	compatibleTrackerMapCache.Lock()
	compatibleTrackerMapCache.ByPrefix[prefix] = maps
	compatibleTrackerMapCache.Unlock()
	return maps, nil
}

func (s compatibleLocatorSource) SourceName() string {
	return filepath.ToSlash(filepath.Join(s.Region, s.LocatorFile))
}

func (s compatibleLocatorSource) transformKey(mapName string) string {
	return s.SourceName() + "|" + mapName
}

func (s compatibleLocatorSource) acceptsCandidate(mapName string) bool {
	if s.CandidateMap != "" {
		return s.CandidateMap == mapName
	}
	return strings.HasPrefix(mapName, s.MapPrefix+"_lv")
}

func compatibleRegionMapPrefix(region string) (string, bool) {
	switch region {
	case "ValleyIV":
		return "map01", true
	case "Wuling":
		return "map02", true
	case "OMVBase":
		return "base01", true
	default:
		return "", false
	}
}

func compatibleMapPrefixRegion(prefix string) (string, bool) {
	switch prefix {
	case "map01":
		return "ValleyIV", true
	case "map02":
		return "Wuling", true
	case "base01":
		return "OMVBase", true
	default:
		return "", false
	}
}

func compatibleRootMapName(mapName string) string {
	if index := strings.Index(mapName, "_tier_"); index >= 0 {
		return mapName[:index]
	}
	return mapName
}
