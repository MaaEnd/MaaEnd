package captureuid

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const component = "captureuid"

const saltPath = "debug/record/random_salt.txt"

// OutputType 表示 CaptureUid 捕获结果的输出格式。
type OutputType string

const (
	// OutputTypeHashed 输出加盐 SHA-256 哈希的前 16 位十六进制（默认）。
	OutputTypeHashed OutputType = "hashed"
	// OutputTypeMasked 输出仅保留开头与末尾各 3 位、中间以 * 打码的形式。
	OutputTypeMasked OutputType = "masked"
	// OutputTypeRaw 输出原始 UID 数字。
	OutputTypeRaw OutputType = "raw"
)

var (
	// capturedUid 缓存 OCR 得到的原始 UID 数字；未捕获或失败时为空字符串。
	capturedUid   string
	capturedUidMu sync.Mutex

	uidDigitRe = regexp.MustCompile(`\d+`)

	// loadSaltFunc 是加载（或首次生成）盐的注入点，单元测试可替换为固定盐。
	loadSaltFunc = loadOrCreateSalt
)

// Capture 捕获玩家 UID，并按 outputType 返回格式化结果。
// 缓存恒存原始 UID 数字，输出时才进行转换，因此 use_cache 命中时也能按任意 outputType 返回。
//   - useCache: if true and cache has a UID, return cached UID immediately
//   - stayOnCurrentScreen: if false, navigate to SceneEnterMenuOperationalManual before OCR
//   - allowUnknown: if true and OCR cannot extract UID, return "unknown" instead of error
//   - outputType: 输出格式，hashed / masked / raw
func Capture(ctx *maa.Context, ctrl *maa.Controller, useCache, stayOnCurrentScreen, allowUnknown bool, outputType OutputType) (string, error) {
	if useCache {
		if uid := GetCachedUID(outputType); uid != "" {
			log.Debug().Str("component", component).Str("uid", uid).Str("output_type", string(outputType)).Msg("returning cached uid")
			return uid, nil
		}
	}

	if !stayOnCurrentScreen {
		if _, err := ctx.RunTask("SceneEnterMenuOperationalManual"); err != nil {
			return captureErr(allowUnknown, "failed to navigate to SceneEnterMenuOperationalManual: %w", err)
		}
	}

	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		return captureErr(allowUnknown, "screenshot failed: %w", err)
	}

	param := maa.OCRParam{
		ROI:      targetRect(maa.Rect{60, 690, 155, 25}),
		Expected: []string{".*"},
		OnlyRec:  true,
		OrderBy:  maa.OCROrderByLength,
	}
	detail, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, &param, img)
	if err != nil || detail == nil || !detail.Hit {
		return captureErr(allowUnknown, "uid OCR miss")
	}

	text := bestOCRText(detail)
	digits := extractAllDigits(text)
	if len(digits) < 8 || len(digits) > 12 {
		return captureErr(allowUnknown, "uid digit count %d not in [8,12], text=%q", len(digits), text)
	}

	capturedUidMu.Lock()
	capturedUid = digits
	capturedUidMu.Unlock()

	uid, err := formatUID(digits, outputType)
	if err != nil {
		return captureErr(allowUnknown, "format uid: %w", err)
	}

	log.Info().Str("component", component).Str("uid", uid).Str("output_type", string(outputType)).Msg("captured uid")
	return uid, nil
}

// ClearCache clears the cached UID (thread-safe).
func ClearCache() {
	capturedUidMu.Lock()
	capturedUid = ""
	capturedUidMu.Unlock()
	log.Info().Str("component", component).Msg("uid cache cleared")
}

// GetCachedUID 返回最近捕获的原始 UID 按 outputType 转换后的结果（线程安全）。
// 无缓存时返回空字符串；非法 outputType 记录错误并返回空字符串。
func GetCachedUID(outputType OutputType) string {
	capturedUidMu.Lock()
	defer capturedUidMu.Unlock()
	uid, err := formatUID(capturedUid, outputType)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("output_type", string(outputType)).Msg("GetCachedUID: format failed")
		return ""
	}
	return uid
}

// formatUID 将原始 UID 数字按 outputType 转换为输出格式。
// hashed 保持原算法 SHA-256(uid+盐) 前 16 位十六进制；masked 保留首尾各 3 位；
// raw 原样返回；空字符串与 "unknown" 在所有模式下原样透传。
func formatUID(raw string, outputType OutputType) (string, error) {
	switch outputType {
	case OutputTypeHashed, OutputTypeMasked, OutputTypeRaw:
	default:
		return "", fmt.Errorf("unsupported output_type %q, want hashed|masked|raw", outputType)
	}
	if raw == "" || raw == "unknown" {
		return raw, nil
	}
	switch outputType {
	case OutputTypeHashed:
		salt, err := loadSaltFunc()
		if err != nil {
			return "", fmt.Errorf("salt load/create failed: %w", err)
		}
		hash := sha256.Sum256([]byte(raw + salt))
		return hex.EncodeToString(hash[:])[:16], nil
	case OutputTypeMasked:
		return maskUID(raw), nil
	default:
		return raw, nil
	}
}

// maskUID 保留开头 3 位与末尾 3 位，中间以 * 打码；长度不足 7 位时原样返回。
func maskUID(uid string) string {
	if len(uid) <= 6 {
		return uid
	}
	return uid[:3] + strings.Repeat("*", len(uid)-6) + uid[len(uid)-3:]
}

// normalizeOutputType 校验并规范化 output_type 参数；空字符串按 hashed 处理。
func normalizeOutputType(s string) (OutputType, error) {
	if s == "" {
		return OutputTypeHashed, nil
	}
	ot := OutputType(s)
	switch ot {
	case OutputTypeHashed, OutputTypeMasked, OutputTypeRaw:
		return ot, nil
	default:
		return "", fmt.Errorf("unsupported output_type %q, want hashed|masked|raw", s)
	}
}

func bestOCRText(detail *maa.RecognitionDetail) string {
	if detail == nil || detail.Results == nil {
		return ""
	}
	if detail.Results.Best != nil {
		if o, ok := detail.Results.Best.AsOCR(); ok {
			return strings.TrimSpace(o.Text)
		}
	}
	for _, r := range detail.Results.Filtered {
		if r == nil {
			continue
		}
		if o, ok := r.AsOCR(); ok {
			return strings.TrimSpace(o.Text)
		}
	}
	return ""
}

func extractAllDigits(s string) string {
	parts := uidDigitRe.FindAllString(s, -1)
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p)
	}
	return b.String()
}

func targetRect(r maa.Rect) maa.Target {
	return maa.NewTargetRect(r)
}

func loadOrCreateSalt() (string, error) {
	path := saltPath
	data, err := os.ReadFile(path)
	if err == nil && len(strings.TrimSpace(string(data))) > 0 {
		return strings.TrimSpace(string(data)), nil
	}

	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	salt := hex.EncodeToString(saltBytes)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("create salt dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(salt), 0644); err != nil {
		return "", fmt.Errorf("write salt file: %w", err)
	}
	return salt, nil
}

func captureErr(allowUnknown bool, format string, args ...any) (string, error) {
	if allowUnknown {
		msg := fmt.Sprintf(format, args...)
		log.Warn().Str("component", component).Str("reason", msg).Msg("uid capture failed, returning unknown")
		return "unknown", nil
	}
	return "", fmt.Errorf(format, args...)
}
