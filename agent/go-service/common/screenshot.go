package common

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// ScreenShot 将当前截图保存为 PNG 到指定目录，可用于调试。
// custom_action_param 为 JSON，可选字段：
// - "type": 文件名前缀；
// - "dir": 保存目录（默认 "debug"）；
// - "clean_days": 清理 N 天前旧文件（默认 3 天）。
type ScreenShot struct{}

var _ maa.CustomActionRunner = (*ScreenShot)(nil)

// Run 实现 maa.CustomActionRunner：截屏并保存为 PNG。
func (a *ScreenShot) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 解析参数，如有格式错误会记录日志，方便排查配置问题
	var params struct {
		Type      string `json:"type"`
		Dir       string `json:"dir"`
		CleanDays int    `json:"clean_days"`
	}
	rawParam := strings.TrimSpace(arg.CustomActionParam)
	if rawParam != "" {
		if err := json.Unmarshal([]byte(rawParam), &params); err != nil {
			log.Error().
				Err(err).
				Str("raw_param", rawParam).
				Msg("[ScreenShot] Failed to parse custom_action_param, fallback to defaults")
		}
	}

	typePrefix := strings.TrimSpace(params.Type)
	if typePrefix != "" {
		typePrefix = typePrefix + "_"
	}

	dir := strings.TrimSpace(params.Dir)
	if dir == "" {
		dir = "debug"
	}

	cleanDays := params.CleanDays
	if cleanDays <= 0 {
		cleanDays = 3
	}

	ctrl := ctx.GetTasker().GetController()
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		log.Error().Err(err).Msg("[ScreenShot] 截图失败")
		return false
	}
	if img == nil {
		log.Error().Msg("[ScreenShot] 截图为空")
		return false
	}

	outputDir, err := filepath.Abs(dir)
	if err != nil {
		log.Error().Err(err).Str("dir", dir).Msg("[ScreenShot] 解析截图目录失败")
		return false
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		log.Error().Err(err).Str("dir", outputDir).Msg("[ScreenShot] 创建截图目录失败")
		return false
	}

	// 清理 N 天前的 PNG 文件；若清理目录读取失败，也会记录日志
	cleanBefore := time.Now().Add(time.Duration(-cleanDays) * 24 * time.Hour)
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		log.Error().Err(err).Str("dir", outputDir).Msg("[ScreenShot] 读取截图目录失败，跳过清理旧文件")
	} else {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			nameLower := strings.ToLower(e.Name())
			if !strings.HasSuffix(nameLower, ".png") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				log.Debug().Err(err).Str("name", e.Name()).Msg("[ScreenShot] 获取文件信息失败，跳过该文件的清理判断")
				continue
			}
			if info.ModTime().Before(cleanBefore) {
				p := filepath.Join(outputDir, e.Name())
				if err := os.Remove(p); err != nil {
					log.Debug().Err(err).Str("path", p).Msg("[ScreenShot] 清理旧文件失败")
				}
			}
		}
	}

	// 使用可读时间 + 纳秒后缀，既方便调试又避免同一秒内的文件名冲突
	now := time.Now()
	fileName := fmt.Sprintf("%s%s_%09d.png",
		typePrefix,
		now.Format("2006-01-02_15-04-05"),
		now.Nanosecond(),
	)
	debugPath := filepath.Join(outputDir, fileName)

	// 若 CacheImage 返回的是非 *image.RGBA，转为 RGBA 以便编码
	toEncode := image.Image(img)
	if _, ok := img.(*image.RGBA); !ok {
		b := img.Bounds()
		rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
		toEncode = rgba
	}

	f, err := os.Create(debugPath)
	if err != nil {
		log.Error().Err(err).Str("path", debugPath).Msg("[ScreenShot] 创建文件失败")
		return false
	}
	defer f.Close()

	if err := png.Encode(f, toEncode); err != nil {
		log.Error().Err(err).Str("path", debugPath).Msg("[ScreenShot] 写入 PNG 失败")
		return false
	}

	log.Info().Str("path", debugPath).Msg("[ScreenShot] 已保存截图")
	return true
}
