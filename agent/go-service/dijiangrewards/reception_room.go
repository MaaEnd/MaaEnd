package dijiangrewards

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	countdownComponent = "ReceptionRoomExchangeCountdownWithinThresholdRecognition"
	keepAliveComponent = "ReceptionRoomWaitExchangeKeepAliveDueRecognition"
	countdownTextNode  = "ReceptionRoomExchangeCountdownText"

	defaultThresholdMinutes = 5

	waitingReportInterval = 10 * time.Second

	keepAliveInterval   = 20 * time.Minute
	keepAliveSessionGap = 2 * time.Minute
)

var countdownPattern = regexp.MustCompile(`\b(\d{1,2})\s*[:：]\s*(\d{2})(?:\s*[:：]\s*(\d{2}))?\b`)

type ExchangeCountdownWithinThresholdRecognition struct {
	mu                  sync.Mutex
	lastWaitingReportAt time.Time
}

type ExchangeKeepAliveDueRecognition struct {
	mu              sync.Mutex
	lastCheckAt     time.Time
	lastKeepAliveAt time.Time
}

type countdownParams struct {
	ThresholdMinutes int  `json:"threshold_minutes"`
	ReportWaiting    bool `json:"report_waiting,omitempty"`
}

func (r *ExchangeCountdownWithinThresholdRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Warn().Str("component", countdownComponent).Msg("context, arg, or image is nil")
		return nil, false
	}

	params, err := parseCountdownParams(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", countdownComponent).
			Str("raw_param", arg.CustomRecognitionParam).
			Msg("failed to parse params")
		return nil, false
	}

	text, seconds, box, ok := recognizeCountdownSeconds(ctx, arg, countdownComponent)
	if !ok {
		return nil, false
	}

	thresholdSeconds := params.ThresholdMinutes * 60
	matched := seconds <= thresholdSeconds
	detailJSON, _ := json.Marshal(map[string]any{
		"ocr_text":          text,
		"seconds":           seconds,
		"threshold_minutes": params.ThresholdMinutes,
		"matched":           matched,
	})

	log.Debug().
		Str("component", countdownComponent).
		Str("ocr_text", text).
		Int("seconds", seconds).
		Int("threshold_minutes", params.ThresholdMinutes).
		Bool("matched", matched).
		Msg("countdown evaluated")

	if !matched {
		return nil, false
	}

	if params.ReportWaiting && r.shouldReportWaiting(time.Now()) {
		maafocus.PrintLargeContentTrimNewline(
			i18n.RenderHTML("dijiangrewards.wait_exchange_countdown", map[string]any{
				"Formatted": formatCountdown(seconds),
			}),
		)
	}

	return &maa.CustomRecognitionResult{
		Box:    box,
		Detail: string(detailJSON),
	}, true
}

func (r *ExchangeCountdownWithinThresholdRecognition) shouldReportWaiting(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.lastWaitingReportAt.IsZero() || now.Sub(r.lastWaitingReportAt) >= waitingReportInterval {
		r.lastWaitingReportAt = now
		return true
	}
	return false
}

func (r *ExchangeKeepAliveDueRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Warn().Str("component", keepAliveComponent).Msg("context, arg, or image is nil")
		return nil, false
	}

	text, seconds, box, ok := recognizeCountdownSeconds(ctx, arg, keepAliveComponent)
	if !ok {
		return nil, false
	}

	now := time.Now()
	matched := r.shouldKeepAlive(now)
	detailJSON, _ := json.Marshal(map[string]any{
		"ocr_text":         text,
		"seconds":          seconds,
		"interval_seconds": int(keepAliveInterval / time.Second),
		"matched":          matched,
	})

	log.Debug().
		Str("component", keepAliveComponent).
		Str("ocr_text", text).
		Int("seconds", seconds).
		Dur("interval", keepAliveInterval).
		Bool("matched", matched).
		Msg("keep alive evaluated")

	if !matched {
		return nil, false
	}

	return &maa.CustomRecognitionResult{
		Box:    box,
		Detail: string(detailJSON),
	}, true
}

func (r *ExchangeKeepAliveDueRecognition) shouldKeepAlive(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.lastCheckAt.IsZero() || now.Sub(r.lastCheckAt) > keepAliveSessionGap {
		r.lastKeepAliveAt = now
	}
	r.lastCheckAt = now

	if now.Sub(r.lastKeepAliveAt) < keepAliveInterval {
		return false
	}

	r.lastKeepAliveAt = now
	return true
}

func recognizeCountdownSeconds(ctx *maa.Context, arg *maa.CustomRecognitionArg, component string) (string, int, maa.Rect, bool) {
	detail, err := ctx.RunRecognition(countdownTextNode, arg.Img)
	if err != nil || detail == nil {
		log.Debug().
			Err(err).
			Str("component", component).
			Str("node", countdownTextNode).
			Msg("countdown OCR miss")
		return "", 0, maa.Rect{}, false
	}

	text := bestOCRText(detail)
	seconds, err := parseCountdownSeconds(text)
	if err != nil {
		log.Debug().
			Err(err).
			Str("component", component).
			Str("ocr_text", text).
			Msg("failed to parse countdown")
		return "", 0, maa.Rect{}, false
	}

	return text, seconds, detail.Box, true
}

func parseCountdownParams(raw string) (*countdownParams, error) {
	params := countdownParams{ThresholdMinutes: defaultThresholdMinutes}
	if strings.TrimSpace(raw) == "" {
		return &params, nil
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return nil, fmt.Errorf("unmarshal custom_recognition_param: %w", err)
	}
	if params.ThresholdMinutes <= 0 {
		return nil, fmt.Errorf("threshold_minutes must be positive")
	}
	return &params, nil
}

func parseCountdownSeconds(text string) (int, error) {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return 0, fmt.Errorf("countdown text is empty")
	}

	match := findCountdownMatch(cleaned)
	if match == nil {
		return 0, fmt.Errorf("countdown text %q contains no time value", cleaned)
	}

	first, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, err
	}
	second, err := strconv.Atoi(match[2])
	if err != nil {
		return 0, err
	}
	if second >= 60 {
		return 0, fmt.Errorf("invalid minute/second value %d", second)
	}

	if match[3] == "" {
		return first*60 + second, nil
	}

	third, err := strconv.Atoi(match[3])
	if err != nil {
		return 0, err
	}
	if third >= 60 {
		return 0, fmt.Errorf("invalid second value %d", third)
	}
	return first*3600 + second*60 + third, nil
}

func findCountdownMatch(text string) []string {
	for _, loc := range countdownPattern.FindAllStringSubmatchIndex(text, -1) {
		prev := previousNonSpaceRune(text[:loc[0]])
		if prev == ':' || prev == '：' {
			continue
		}

		// loc 每两个元素是一组 start/end：loc[0:2] 是完整匹配，
		// loc[2:4]、loc[4:6]、loc[6:8] 分别是三个捕获组。
		match := make([]string, len(loc)/2)
		for i := range match {
			start := loc[i*2]
			end := loc[i*2+1]
			if start >= 0 && end >= 0 {
				match[i] = text[start:end]
			}
		}
		return match
	}
	return nil
}

func previousNonSpaceRune(text string) rune {
	for len(text) > 0 {
		prev, size := utf8.DecodeLastRuneInString(text)
		if prev == utf8.RuneError && size == 0 {
			return 0
		}
		if !unicode.IsSpace(prev) {
			return prev
		}
		text = text[:len(text)-size]
	}
	return 0
}

func formatCountdown(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}

	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	remainingSeconds := seconds % 60
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, remainingSeconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, remainingSeconds)
}

func bestOCRText(detail *maa.RecognitionDetail) string {
	if detail == nil || detail.Results == nil {
		return ""
	}
	if detail.Results.Best != nil {
		if ocr, ok := detail.Results.Best.AsOCR(); ok {
			return strings.TrimSpace(ocr.Text)
		}
	}
	for _, result := range detail.Results.Filtered {
		if result == nil {
			continue
		}
		if ocr, ok := result.AsOCR(); ok {
			return strings.TrimSpace(ocr.Text)
		}
	}
	for _, result := range detail.Results.All {
		if result == nil {
			continue
		}
		if ocr, ok := result.AsOCR(); ok {
			return strings.TrimSpace(ocr.Text)
		}
	}
	return ""
}

var _ maa.CustomRecognitionRunner = &ExchangeCountdownWithinThresholdRecognition{}
var _ maa.CustomRecognitionRunner = &ExchangeKeepAliveDueRecognition{}
