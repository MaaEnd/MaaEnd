// Copyright (c) 2026 Harry Huang
package maptracker

import (
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	resourcePath     atomic.Value // string
	registerSinkOnce sync.Once

	Resource = &MapTrackerResource{
		PointerTemplateLoader: minicv.NewTemplateLoaderOfDynamicPath(
			func() string { return FindResource("resource/image/MapTracker/pointer.png") },
		),
		ZoomInTemplate: minicv.NewTemplateLoaderOfDynamicPath(
			func() string { return FindResource("resource/image/MapTracker/BigMapZoomIn.png") },
		),
		ZoomOutTemplate: minicv.NewTemplateLoaderOfDynamicPath(
			func() string { return FindResource("resource/image/MapTracker/BigMapZoomOut.png") },
		),
	}
)

// MapTrackerResource stores globally shared map resources for map-tracker.
type MapTrackerResource struct {
	RawMapsOnce sync.Once
	RawMaps     []MapCache
	RawMapsErr  error

	IntegralCacheMu sync.Mutex

	PointerTemplateLoader *minicv.TemplateLoader
	ZoomInTemplate        *minicv.TemplateLoader
	ZoomOutTemplate       *minicv.TemplateLoader
}

// MapCache represents a preloaded map image.
type MapCache struct {
	Name    string
	Img     *image.RGBA
	OffsetX int
	OffsetY int

	cachedIntegralArray *minicv.IntegralArray
}

// GetIntegralArray lazily initializes integral array when first needed.
func (m *MapCache) GetIntegralArray() minicv.IntegralArray {
	Resource.IntegralCacheMu.Lock()
	defer Resource.IntegralCacheMu.Unlock()

	if m.cachedIntegralArray == nil {
		integral := minicv.GetIntegralArray(m.Img)
		m.cachedIntegralArray = &integral
	}
	return *m.cachedIntegralArray
}

// EnsureResourcePathSink ensures the resource path sink is registered.
func EnsureResourcePathSink() {
	registerSinkOnce.Do(func() {
		maa.AgentServerAddResourceSink(&resourcePathSink{})
		log.Debug().Msg("Resource path sink registered for map-tracker")
	})
}

type resourcePathSink struct{}

// OnResourceLoading captures the resource path when a resource is loaded.
func (c *resourcePathSink) OnResourceLoading(resource *maa.Resource, status maa.EventStatus, detail maa.ResourceLoadingDetail) {
	if status != maa.EventStatusSucceeded || detail.Path == "" {
		return
	}
	abs := detail.Path
	if p, err := filepath.Abs(detail.Path); err == nil {
		abs = p
	}
	resourcePath.Store(abs)
	log.Debug().Str("resource_path", abs).Msg("Resource loaded; cached path for map-tracker")
}

// getResourceBase returns the cached resource path or common defaults as fallback.
func getResourceBase() string {
	if v := resourcePath.Load(); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// FindResource tries to find a file in the cached resource path or standard fallbacks.
func FindResource(relativePath string) string {
	rel := filepath.FromSlash(strings.TrimSpace(relativePath))
	rel = strings.TrimPrefix(rel, string(filepath.Separator))
	relNoResourcePrefix := strings.TrimPrefix(rel, "resource"+string(filepath.Separator))

	tryPath := func(path string) string {
		if path == "" {
			return ""
		}
		if _, err := os.Stat(path); err == nil {
			return path
		}
		return ""
	}

	candidates := make([]string, 0, 18)

	if base := getResourceBase(); base != "" {
		base = filepath.Clean(base)
		baseParent := filepath.Dir(base)

		candidates = append(candidates,
			filepath.Join(base, rel),
			filepath.Join(base, relNoResourcePrefix),
			filepath.Join(baseParent, rel),
		)
	}

	cwd, _ := os.Getwd()
	wd := filepath.Clean(cwd)
	wdParent := filepath.Dir(wd)
	wdGrandParent := filepath.Dir(wdParent)

	fallbacks := []string{
		wd,
		wdParent,
		wdGrandParent,
		filepath.Join(wd, "resource"),
		filepath.Join(wdParent, "resource"),
		filepath.Join(wdGrandParent, "resource"),
		filepath.Join(wd, "assets"),
		filepath.Join(wdParent, "assets"),
		filepath.Join(wdGrandParent, "assets"),
	}

	for _, base := range fallbacks {
		candidates = append(candidates,
			filepath.Join(base, rel),
			filepath.Join(base, relNoResourcePrefix),
		)
	}

	for _, p := range candidates {
		if found := tryPath(filepath.Clean(p)); found != "" {
			return found
		}
	}

	return ""
}

// InitRawMaps initializes global raw maps cache exactly once.
func (r *MapTrackerResource) InitRawMaps(ctx *maa.Context) {
	r.RawMapsOnce.Do(func() {
		r.RawMaps, r.RawMapsErr = r.LoadMaps()
		if r.RawMapsErr != nil {
			log.Error().Err(r.RawMapsErr).Msg("Failed to load maps")
		} else {
			log.Info().Int("mapsCount", len(r.RawMaps)).Msg("Map images loaded")
		}
	})
}

// LoadMaps loads all map images from the resource directory and crops them when map bbox data exists.
func (r *MapTrackerResource) LoadMaps() ([]MapCache, error) {
	mapDir := FindResource(MAP_DIR)
	if mapDir == "" {
		return nil, fmt.Errorf("map directory not found (searched in cache and standard locations)")
	}

	rectList := make(map[string][]int)
	rectPath := FindResource(MAP_BBOX_DATA_PATH)
	if rectPath != "" {
		if data, err := os.ReadFile(rectPath); err == nil {
			if err := json.Unmarshal(data, &rectList); err != nil {
				log.Warn().Err(err).Str("path", rectPath).Msg("Failed to unmarshal map bbox data")
			} else {
				log.Info().Str("path", rectPath).Msg("Map bbox data loaded")
			}
		} else {
			log.Warn().Err(err).Str("path", rectPath).Msg("Failed to read map bbox data")
		}
	}

	entries, err := os.ReadDir(mapDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read map directory: %w", err)
	}

	type indexedFile struct {
		idx      int
		filename string
	}
	files := make([]indexedFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if !strings.HasSuffix(filename, ".png") {
			continue
		}
		files = append(files, indexedFile{idx: len(files), filename: filename})
	}

	type result struct {
		idx int
		m   MapCache
		ok  bool
	}

	results := make([]MapCache, len(files))
	okFlags := make([]bool, len(files))
	resChan := make(chan result, len(files))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup

	for _, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(item indexedFile) {
			defer wg.Done()
			defer func() { <-sem }()

			filename := item.filename
			imgPath := filepath.Join(mapDir, filename)
			file, err := os.Open(imgPath)
			if err != nil {
				log.Warn().Err(err).Str("path", imgPath).Msg("Failed to open map image")
				return
			}

			img, _, err := image.Decode(file)
			file.Close()
			if err != nil {
				log.Warn().Err(err).Str("path", imgPath).Msg("Failed to decode map image")
				return
			}

			name := strings.TrimSuffix(filename, ".png")
			fullRGBA := minicv.ImageConvertRGBA(img)

			imgRGBA := fullRGBA
			offsetX, offsetY := 0, 0

			if bboxRect, ok := rectList[name]; ok && len(bboxRect) == 4 {
				rect := image.Rect(bboxRect[0], bboxRect[1], bboxRect[2], bboxRect[3])
				expand := RAW_MAP_BBOX_EXPAND_PX
				rect = image.Rect(rect.Min.X-expand, rect.Min.Y-expand, rect.Max.X+expand, rect.Max.Y+expand)

				clipped := rect.Intersect(fullRGBA.Bounds())
				imgRGBA = minicv.ImageCropRect(fullRGBA, rect)
				if !clipped.Empty() {
					offsetX, offsetY = clipped.Min.X, clipped.Min.Y
				}
			}

			resChan <- result{
				idx: item.idx,
				m: MapCache{
					Name:    name,
					Img:     imgRGBA,
					OffsetX: offsetX,
					OffsetY: offsetY,
				},
				ok: true,
			}
		}(f)
	}

	go func() {
		wg.Wait()
		close(resChan)
	}()

	for res := range resChan {
		if !res.ok {
			continue
		}
		results[res.idx] = res.m
		okFlags[res.idx] = true
	}

	maps := make([]MapCache, 0, len(files))
	for idx := range results {
		if okFlags[idx] {
			maps = append(maps, results[idx])
		}
	}

	if len(maps) == 0 {
		return nil, fmt.Errorf("no valid map images found in %s", mapDir)
	}

	return maps, nil
}
