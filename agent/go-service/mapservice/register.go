package mapservice

import (
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	_ maa.CustomActionRunner = &MapLocateAction{}

	globalLocator *MapLocator
	locatorMutex  sync.Mutex

	// Regex to parse filenames like Lv001Tier172.png
	// Matches: Lv[Digits]Tier[Digits].[Ext]
	layerFileRegex = regexp.MustCompile(`(?i)Lv(\d+)Tier(\d+)\.(png|jpg|webp)$`)
)

const (
	MinimapROIX      = 49
	MinimapROIY      = 51
	MinimapROIWidth  = 117
	MinimapROIHeight = 120
)

func Register() {
	maa.AgentServerRegisterCustomAction("MapLocateAction", &MapLocateAction{})
}

type MapLocateAction struct {
	debug     bool
	outputDir string
}

type MapLocateActionParam struct {
	Debug     bool   `json:"debug"`
	OutputDir string `json:"output_dir"`
}

func (a *MapLocateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var p MapLocateActionParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil {
		var s string
		if err2 := json.Unmarshal([]byte(arg.CustomActionParam), &s); err2 == nil {
			if err3 := json.Unmarshal([]byte(s), &p); err3 != nil {
				log.Warn().Err(err3).Msg("Failed to parse param (inner), using default")
			}
		} else {
			log.Warn().Err(err).Msg("Failed to parse param, using default")
		}
	}

	locator, err := getLocator(p.Debug, p.OutputDir)
	if err != nil {
		log.Error().Err(err).Msg("failed to get locator")
		return false
	}

	img, err := ctx.GetTasker().GetController().CacheImage()
	if err != nil {
		log.Error().Err(err).Msg("failed to get image")
		return false
	}
	if img == nil {
		log.Error().Msg("failed to get image (nil)")
		return false
	}

	// ROI Crop: [49, 51, 117, 120] (x, y, w, h)
	roi := image.Rect(MinimapROIX, MinimapROIY, MinimapROIX+MinimapROIWidth, MinimapROIY+MinimapROIHeight)

	bounds := img.Bounds()
	roi = roi.Intersect(bounds)

	if roi.Empty() {
		log.Error().Msg("ROI is empty or out of bounds")
		return false
	}

	// Crop
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}

	var subImg image.Image
	if si, ok := img.(subImager); ok {
		subImg = si.SubImage(roi)
	} else {
		log.Error().Msg("image does not support SubImage")
		return false
	}

	// Perform Locate
	start := time.Now()
	pos, err := locator.Locate(ctx, subImg)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		log.Error().Err(err).Msg("Localization failed")
		return true
	}

	if pos != nil {
		log.Info().
			Str("zone", pos.ZoneID).
			Float64("x", pos.X).
			Float64("y", pos.Y).
			Float64("score", pos.Score).
			Int("slice", pos.SliceIndex).
			Int64("latency", latency).
			Msg("[MapLocate] Position Found")

		// [Debug] Result in MXU for testing
		msg := fmt.Sprintf("Located: zone=%s x=%.2f y=%.2f score=%.2f latency=%dms", pos.ZoneID, pos.X, pos.Y, pos.Score, latency)
		MapShowMessage(ctx, msg)
	} else {
		log.Warn().Msg("[MapLocate] Position Not Found")
	}

	return true
}

func MapShowMessage(ctx *maa.Context, text string) {
	ctx.RunTask("MapShowMessage", map[string]interface{}{
		"MapShowMessage": map[string]interface{}{
			"recognition": "DirectHit",
			"action":      "DoNothing",
			"focus": map[string]interface{}{
				"Node.Action.Starting": text,
			},
		},
	})
}

// AutoScanZones 扫描 mapRoot 目录加载地图资源。
// 命名规范：
// - Base Map: [Region]_Base
// - Tier Map: [Region]_L[Lv]_[Tier]
func AutoScanZones(rootDir string) (map[string]string, error) {
	zones := make(map[string]string)

	// 读取 mapRoot 查找区域（子目录）
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, err
	}

	for _, areaDir := range entries {
		if !areaDir.IsDir() {
			continue
		}

		areaName := areaDir.Name() // e.g., "ValleyIV"
		areaPath := filepath.Join(rootDir, areaName)

		// 遍历区域目录中的文件
		files, err := os.ReadDir(areaPath)
		if err != nil {
			log.Warn().Err(err).Str("region", areaName).Msg("Failed to read region directory")
			continue
		}

		for _, file := range files {
			if file.IsDir() {
				continue
			}

			fileName := file.Name()
			absPath := filepath.Join(areaPath, fileName)

			// 处理 Base.png（不区分大小写）
			if strings.EqualFold(fileName, "Base.png") {
				// Key: ValleyIV_Base
				key := fmt.Sprintf("%s_Base", areaName)
				zones[key] = absPath
				log.Info().Str("id", key).Str("path", absPath).Msg("Loaded base map")
				continue
			}

			// 处理 Tier Map：LvXXXTierYYY.png
			matches := layerFileRegex.FindStringSubmatch(fileName)
			if len(matches) == 4 { // [FullMatch, Lv, Tier, Ext]
				lv := strings.TrimLeft(matches[1], "0")
				if lv == "" {
					lv = "0"
				} // Handle "000" -> "0"

				tier := strings.TrimLeft(matches[2], "0")
				if tier == "" {
					tier = "0"
				}

				// Key: ValleyIV_L1_172
				key := fmt.Sprintf("%s_L%s_%s", areaName, lv, tier)
				zones[key] = absPath
				log.Info().Str("id", key).Str("path", absPath).Msg("Loaded tier map")
				continue
			}
		}
	}

	return zones, nil
}

func getLocator(debug bool, outputDir string) (*MapLocator, error) {
	locatorMutex.Lock()
	defer locatorMutex.Unlock()

	if globalLocator != nil {
		globalLocator.debug = debug
		return globalLocator, nil
	}

	cwd, _ := os.Getwd()
	// 地图根目录：assets/resource/image/Map
	mapRoot := filepath.Join(cwd, "resource", "image", "Map")

	// 按照命名规范自动扫描区域
	zones, err := AutoScanZones(mapRoot)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to auto-scan zones")
	}

	if len(zones) == 0 {
		return nil, fmt.Errorf("no valid map resources found in %s", mapRoot)
	}

	loc, err := NewMapLocator(zones, debug, outputDir)
	if err != nil {
		return nil, err
	}

	globalLocator = loc
	return globalLocator, nil
}

// SetGlobalLocator 允许外部调用者获取单例实例
func SetGlobalLocator(loc *MapLocator) {
	locatorMutex.Lock()
	defer locatorMutex.Unlock()
	globalLocator = loc
}
