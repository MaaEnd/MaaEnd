package essencefilter

import (
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// firstOCRText returns the first non-empty OCR string from Best, then Filtered, then All.
func firstOCRText(d *maa.RecognitionDetail) (string, bool) {
	if d == nil || d.Results == nil {
		return "", false
	}
	for _, results := range [][]*maa.RecognitionResult{{d.Results.Best}, d.Results.Filtered, d.Results.All} {
		if len(results) > 0 {
			if ocrResult, ok := results[0].AsOCR(); ok {
				if t := strings.TrimSpace(ocrResult.Text); t != "" {
					return t, true
				}
			}
		}
	}
	return "", false
}
