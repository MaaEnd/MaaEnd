package autostockpile

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &CaptureUidAction{}

var (
	capturedUid   string
	capturedUidMu sync.Mutex
)

// CaptureUidAction 从 AutoStockpileGetUid 节点的 OCR 识别结果中提取玩家 UID，
// 数字部分加盐哈希后存储，供后续数据保存使用。
type CaptureUidAction struct{}

func (a *CaptureUidAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", autoStockpileComponent).
			Msg("custom action arg is nil")
		return false
	}

	if arg.RecognitionDetail == nil {
		log.Error().
			Str("component", autoStockpileComponent).
			Msg("recognition detail is nil")
		return false
	}

	uid := extractAndHashUid(arg.RecognitionDetail)

	capturedUidMu.Lock()
	capturedUid = uid
	capturedUidMu.Unlock()

	log.Info().
		Str("component", autoStockpileComponent).
		Str("uid", uid).
		Msg("captured uid")

	return true
}

var uidDigitRe = regexp.MustCompile(`\d+`)

// extractAndHashUid 从 OCR 识别结果的 best 文本中提取连续数字，
// 使用 SHA256 加盐 "AutoStockpile" 后取前 16 位十六进制。
// 无法提取有效数字时返回 "unknown"。
func extractAndHashUid(detail *maa.RecognitionDetail) string {
	ocrTexts := ocrTextCandidates(detail, ocrTextPolicyBestOnly)
	if len(ocrTexts) == 0 {
		log.Warn().
			Str("component", autoStockpileComponent).
			Msg("no OCR text found in recognition detail")
		return "unknown"
	}

	bestText := ocrTexts[0]
	digits := uidDigitRe.FindAllString(bestText, -1)
	if len(digits) == 0 {
		log.Warn().
			Str("component", autoStockpileComponent).
			Str("ocr_text", bestText).
			Msg("no digits found in OCR result")
		return "unknown"
	}

	numericPart := ""
	for _, d := range digits {
		numericPart += d
	}

	hash := sha256.Sum256([]byte(numericPart + "AutoStockpile"))
	uid := fmt.Sprintf("%x", hash)[:16]

	log.Debug().
		Str("component", autoStockpileComponent).
		Str("uid", uid).
		Msg("uid extracted and hashed")

	return uid
}

// getCapturedUid 返回最近一次采集的 UID（线程安全）。
func getCapturedUid() string {
	capturedUidMu.Lock()
	defer capturedUidMu.Unlock()
	return capturedUid
}
