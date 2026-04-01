package datacollect

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &DataCollectOcrTempFullScreenOCR{}

// internalOCRNodeName is a Pipeline node used only via Context.RunRecognition from this recognizer.
const internalOCRNodeName = "__DataCollectOcrTempInternalOCR"

// DataCollectOcrTempFullScreenOCR runs full-frame OCR on the current screenshot, saves the raw image under debug/,
// and logs every OCR line returned by the framework.
type DataCollectOcrTempFullScreenOCR struct{}

// Run implements maa.CustomRecognitionRunner.
func (r *DataCollectOcrTempFullScreenOCR) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", "DataCollectOcrTemp").Msg("missing context or image")
		return nil, false
	}

	savedPath := saveDebugOriginal(arg.Img)
	if savedPath != "" {
		log.Info().Str("component", "DataCollectOcrTemp").Str("debug_image", savedPath).Msg("saved screenshot")
	}

	detail, err := ctx.RunRecognition(internalOCRNodeName, arg.Img, nil)
	if err != nil {
		log.Error().Err(err).Str("component", "DataCollectOcrTemp").Msg("RunRecognition failed")
		return nil, false
	}
	if detail == nil {
		log.Error().Str("component", "DataCollectOcrTemp").Msg("recognition detail is nil")
		return nil, false
	}

	logAllOCRResults(detail)

	summary := map[string]any{
		"ok":             detail.Hit,
		"debug_image":    savedPath,
		"ocr_line_count": countOCRLines(detail),
	}
	detailJSON, err := json.Marshal(summary)
	if err != nil {
		detailJSON = []byte(`{"ok":true}`)
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

func saveDebugOriginal(img image.Image) string {
	dir := filepath.Join("debug", "datacollect_ocr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Error().Err(err).Str("component", "DataCollectOcrTemp").Str("dir", dir).Msg("mkdir debug")
		return ""
	}
	name := fmt.Sprintf("frame_%d.png", time.Now().UnixNano())
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		log.Error().Err(err).Str("component", "DataCollectOcrTemp").Str("path", path).Msg("create file")
		return ""
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Error().Err(err).Str("component", "DataCollectOcrTemp").Str("path", path).Msg("encode png")
		return ""
	}
	return path
}

func logAllOCRResults(detail *maa.RecognitionDetail) {
	if detail == nil || detail.Results == nil {
		log.Info().Str("component", "DataCollectOcrTemp").Msg("no OCR results structure")
		return
	}

	log.Info().
		Str("component", "DataCollectOcrTemp").
		Int("all_len", len(detail.Results.All)).
		Int("filtered_len", len(detail.Results.Filtered)).
		Bool("hit", detail.Hit).
		Msg("ocr result summary")

	for i, res := range detail.Results.All {
		if res == nil {
			continue
		}
		ocr, ok := res.AsOCR()
		if !ok {
			log.Info().
				Str("component", "DataCollectOcrTemp").
				Int("index", i).
				Msg("non-OCR result entry skipped")
			continue
		}
		text := strings.TrimSpace(ocr.Text)
		log.Info().
			Str("component", "DataCollectOcrTemp").
			Int("index", i).
			Str("text", text).
			Float64("score", ocr.Score).
			Int("box_x", ocr.Box.X()).
			Int("box_y", ocr.Box.Y()).
			Int("box_w", ocr.Box.Width()).
			Int("box_h", ocr.Box.Height()).
			Msg("ocr line")
	}
}

func countOCRLines(detail *maa.RecognitionDetail) int {
	if detail == nil || detail.Results == nil {
		return 0
	}
	n := 0
	for _, res := range detail.Results.All {
		if res == nil {
			continue
		}
		if ocr, ok := res.AsOCR(); ok && strings.TrimSpace(ocr.Text) != "" {
			n++
		}
	}
	return n
}
