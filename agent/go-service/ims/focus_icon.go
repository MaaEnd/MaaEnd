package ims

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/rs/zerolog/log"
	xdraw "golang.org/x/image/draw"
)

const (
	iconRecognitionImageDir = "resource/image/IconRecognition"
	focusIconDataURLPrefix  = "data:image/png;base64,"
)

// focusIconCache caches encoded icons by item ID. Icons are static resource
// files, so entries never need invalidation; the cache is bounded by the number
// of catalog items and keeps repeated broadcasts from re-decoding the same PNG.
// Empty values are cached too (missing or undecodable icon) to avoid relogging.
var (
	focusIconCacheMu sync.RWMutex
	focusIconCache   = make(map[string]string)
)

// itemIconSrc returns the item icon as a 16×16 PNG data URL for Focus HTML
// <img src>, or "" when the item has no catalog icon.
//
// The client persists rendered Focus HTML in localStorage. Handing it the
// source PNG path makes the client inline the original 128×128 (13–16 KB)
// file, and one inventory summary can carry dozens of icons; downscaling to
// 16×16 first keeps an icon around 1 KB.
func itemIconSrc(itemID string) string {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return ""
	}

	focusIconCacheMu.RLock()
	cached, ok := focusIconCache[itemID]
	focusIconCacheMu.RUnlock()
	if ok {
		return cached
	}

	encoded := ""
	if relPath := itemIconResourcePath(itemID); relPath != "" {
		encoded = focusIconDataURL(resource.FindResource(relPath), focusItemIconSizePx)
	}

	focusIconCacheMu.Lock()
	focusIconCache[itemID] = encoded
	focusIconCacheMu.Unlock()
	return encoded
}

// itemIconResourcePath resolves the install-root-relative icon path from the
// IconRecognition catalog. Returns "" when the item has no catalog icon.
func itemIconResourcePath(itemID string) string {
	catalog, err := loadRecognitionItems()
	if err != nil {
		return ""
	}
	meta, ok := catalog[itemID]
	if !ok {
		return ""
	}
	iconID := strings.TrimSpace(meta.IconID)
	if iconID == "" || meta.Rarity <= 0 {
		return ""
	}
	return filepath.ToSlash(filepath.Join(
		iconRecognitionImageDir,
		fmt.Sprintf("%d", meta.Rarity),
		iconID+".png",
	))
}

// focusIconDataURL reads the PNG at absPath, scales it to size×size and encodes
// the result as a data URL. Returns "" when absPath is empty, the image cannot
// be read or decoded, or encoding fails; the caller then omits the icon.
func focusIconDataURL(absPath string, size int) string {
	if absPath == "" || size <= 0 {
		return ""
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", "IMSFocusIcon").
			Str("path", absPath).
			Msg("failed to read item icon")
		return ""
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", "IMSFocusIcon").
			Str("path", absPath).
			Msg("failed to decode item icon")
		return ""
	}

	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)

	var buf bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&buf, dst); err != nil {
		log.Warn().
			Err(err).
			Str("component", "IMSFocusIcon").
			Str("path", absPath).
			Msg("failed to encode item icon")
		return ""
	}
	return focusIconDataURLPrefix + base64.StdEncoding.EncodeToString(buf.Bytes())
}
