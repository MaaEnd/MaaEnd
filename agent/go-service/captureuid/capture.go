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

var (
	capturedUid   string
	capturedUidMu sync.Mutex

	uidDigitRe = regexp.MustCompile(`\d+`)
)

// Capture captures and hashes the player UID.
//   - useCache: if true and cache has a UID, return cached UID immediately
//   - stayOnCurrentScreen: if false, navigate to SceneEnterMenuOperationalManual before OCR
//   - allowUnknown: if true and OCR cannot extract UID, return "unknown" instead of error
func Capture(ctx *maa.Context, ctrl *maa.Controller, useCache, stayOnCurrentScreen, allowUnknown bool) (string, error) {
	if useCache {
		if uid := GetCachedUID(); uid != "" {
			log.Debug().Str("component", component).Str("uid", uid).Msg("returning cached uid")
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

	salt, err := loadOrCreateSalt()
	if err != nil {
		return captureErr(allowUnknown, "salt load/create failed: %w", err)
	}

	hash := sha256.Sum256([]byte(digits + salt))
	uid := hex.EncodeToString(hash[:])[:16]

	capturedUidMu.Lock()
	capturedUid = uid
	capturedUidMu.Unlock()

	log.Info().Str("component", component).Str("uid", uid).Msg("captured uid")
	return uid, nil
}

// ClearCache clears the cached UID (thread-safe).
func ClearCache() {
	capturedUidMu.Lock()
	capturedUid = ""
	capturedUidMu.Unlock()
	log.Info().Str("component", component).Msg("uid cache cleared")
}

// GetCachedUID returns the most recently captured UID (thread-safe).
func GetCachedUID() string {
	capturedUidMu.Lock()
	defer capturedUidMu.Unlock()
	return capturedUid
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
