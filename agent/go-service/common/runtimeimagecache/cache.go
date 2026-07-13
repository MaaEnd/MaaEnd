package runtimeimagecache

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"path"
	"strings"
)

const imageNamePrefix = "__MaaEndRuntimeImageCacheV1__"

// BuildImageName 生成供 MaaFramework OverrideImage 与 Pipeline template 共用的内存图片名。
func BuildImageName(module, key string) (string, error) {
	module = strings.TrimSpace(module)
	key = strings.TrimSpace(key)
	if module == "" || key == "" {
		return "", errors.New("module and key are required")
	}
	if strings.ContainsAny(module, `/\`) || module == "." || module == ".." {
		return "", fmt.Errorf("invalid module %q", module)
	}
	if strings.ContainsRune(key, '\\') || strings.HasPrefix(key, "/") || path.Clean(key) != key || key == "." {
		return "", fmt.Errorf("invalid key %q", key)
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid key %q", key)
		}
	}
	return imageNamePrefix + "/" + module + "/" + key, nil
}

// EscapeKeyComponent 转义单个 key 片段中的路径保留字符，同时保留中文等可读字符。
func EscapeKeyComponent(value string) string {
	replacer := strings.NewReplacer(
		"%", "%25",
		"/", "%2F",
		"\\", "%5C",
	)
	return replacer.Replace(value)
}

// ApplyROIOffset 按 [x, y, width, height] 的增量调整识别区域。
func ApplyROIOffset(rect image.Rectangle, offset [4]int) (image.Rectangle, error) {
	adjusted := image.Rect(
		rect.Min.X+offset[0],
		rect.Min.Y+offset[1],
		rect.Max.X+offset[0]+offset[2],
		rect.Max.Y+offset[1]+offset[3],
	)
	if adjusted.Empty() {
		return image.Rectangle{}, fmt.Errorf("roi offset produces empty rectangle: %v", adjusted)
	}
	return adjusted, nil
}

// Crop 返回从源图深拷贝出的零原点 RGBA 图片。
func Crop(src image.Image, rect image.Rectangle) (*image.RGBA, error) {
	if src == nil || src.Bounds().Empty() {
		return nil, errors.New("source image is empty")
	}
	if rect.Empty() || !rect.In(src.Bounds()) {
		return nil, fmt.Errorf("crop rectangle %v is outside image bounds %v", rect, src.Bounds())
	}

	dst := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(dst, dst.Bounds(), src, rect.Min, draw.Src)
	return dst, nil
}
