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

	fullPath, fileName := plannedDebugPath()
	log.Info().
		Str("component", "DataCollectOcrTemp").
		Str("filename", fileName).
		Str("path", fullPath).
		Msg("datacollect_ocr will save (copy filename from here)")

	if err := savePNGTo(arg.Img, fullPath); err != nil {
		log.Error().Err(err).Str("component", "DataCollectOcrTemp").Str("path", fullPath).Msg("save screenshot failed")
		return nil, false
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

	logOCRBatch(fileName, fullPath, detail)

	summary := map[string]any{
		"ok":             detail.Hit,
		"debug_image":    fullPath,
		"filename":       fileName,
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

func plannedDebugPath() (fullPath, fileName string) {
	dir := filepath.Join("debug", "datacollect_ocr")
	fileName = fmt.Sprintf("frame_%d.png", time.Now().UnixNano())
	fullPath = filepath.Join(dir, fileName)
	return fullPath, fileName
}

func savePNGTo(img image.Image, fullPath string) error {
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// ocrLineEntry is one OCR box for JSON logging.
type ocrLineEntry struct {
	Index int     `json:"index"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
	X     int     `json:"x"`
	Y     int     `json:"y"`
	W     int     `json:"w"`
	H     int     `json:"h"`
}

func logOCRBatch(fileName, savedPath string, detail *maa.RecognitionDetail) {
	if detail == nil || detail.Results == nil {
		log.Info().
			Str("component", "DataCollectOcrTemp").
			Str("filename", fileName).
			Str("path", savedPath).
			Msg("datacollect_ocr no results structure")
		return
	}

	all := collectOCRLines(detail.Results.All)
	filtered := collectOCRLines(detail.Results.Filtered)

	payload := map[string]any{
		"filename":       fileName,
		"path":           savedPath,
		"hit":            detail.Hit,
		"all_count":      len(detail.Results.All),
		"filtered_count": len(detail.Results.Filtered),
		"all":            all,
		"filtered":       filtered,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Str("component", "DataCollectOcrTemp").Msg("marshal ocr batch failed")
		return
	}

	log.Info().
		Str("component", "DataCollectOcrTemp").
		RawJSON("ocr_batch", raw).
		Msg("datacollect_ocr full dump (all + filtered)")
}

func collectOCRLines(list []*maa.RecognitionResult) []ocrLineEntry {
	if len(list) == 0 {
		return []ocrLineEntry{}
	}
	out := make([]ocrLineEntry, 0, len(list))
	for i, res := range list {
		if res == nil {
			continue
		}
		ocr, ok := res.AsOCR()
		if !ok {
			continue
		}
		out = append(out, ocrLineEntry{
			Index: i,
			Text:  strings.TrimSpace(ocr.Text),
			Score: ocr.Score,
			X:     ocr.Box.X(),
			Y:     ocr.Box.Y(),
			W:     ocr.Box.Width(),
			H:     ocr.Box.Height(),
		})
	}
	return out
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
