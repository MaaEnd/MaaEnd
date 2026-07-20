package sellproduct

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type operatorConflictParam struct {
	Result   string `json:"result"`
	Usage    string `json:"usage"`
	Location string `json:"location"`
}

type operatorConflictDetail struct {
	Result         string `json:"result"`
	Usage          string `json:"usage"`
	Location       string `json:"location"`
	SourceLocation string `json:"source_location,omitempty"`
	SourceManaged  bool   `json:"source_managed"`
	PromptText     string `json:"prompt_text,omitempty"`
}

// OperatorConflictRecognition 判断派驻冲突来源是否属于本次任务启用的据点。
// 已启用来源允许确认接管；未启用或无法可靠识别的来源视为受保护，交由 Pipeline 取消。
type OperatorConflictRecognition struct{}

var _ maa.CustomRecognitionRunner = (*OperatorConflictRecognition)(nil)

func (r *OperatorConflictRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		log.Error().Str("component", operatorConflictRecognitionName).Msg("nil context or arg")
		return nil, false
	}
	p, err := parseOperatorConflictParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", operatorConflictRecognitionName).Msg("invalid params")
		return nil, false
	}

	prompt, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeOCR,
		maa.OCRParam{ROI: maa.NewTargetRect(arg.Roi)},
		arg.Img,
	)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", operatorConflictRecognitionName).
			Msg("conflict source OCR failed")
		return nil, false
	}
	items := collectOCRResults(prompt)
	data, err := loadSellProductSelectionDataCached()
	if err != nil {
		log.Error().Err(err).Str("component", operatorConflictRecognitionName).Msg("selection data unavailable")
		return nil, false
	}
	source, promptText, recognized := findOperatorConflictSource(items, data)
	managed := operatorConflictSourceManaged(
		source,
		recognized,
		operatorSessionSnapshot().ActiveLocations,
	)

	if (p.Result == operatorConflictResultManaged) != managed {
		return nil, false
	}

	detail := operatorConflictDetail{
		Result:         p.Result,
		Usage:          p.Usage,
		Location:       p.Location,
		SourceLocation: source,
		SourceManaged:  managed,
		PromptText:     promptText,
	}
	detailJSON, _ := json.Marshal(detail)
	event := log.Info().
		Str("component", operatorConflictRecognitionName).
		Str("result", p.Result).
		Str("usage", p.Usage).
		Str("location", p.Location).
		Str("source_location", source).
		Bool("source_recognized", recognized).
		Bool("source_managed", managed)
	if !recognized {
		event = event.Str("protection_reason", "source_unrecognized")
	}
	event.Msg("operator assignment conflict resolved")

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

func operatorConflictSourceManaged(
	source string,
	recognized bool,
	activeLocations map[string]struct{},
) bool {
	if !recognized {
		return false
	}
	_, active := activeLocations[source]
	return active
}

func parseOperatorConflictParam(raw string) (*operatorConflictParam, error) {
	var p operatorConflictParam
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("unmarshal custom_recognition_param: %w", err)
	}
	p.Result = strings.TrimSpace(p.Result)
	p.Usage = strings.TrimSpace(p.Usage)
	p.Location = strings.TrimSpace(p.Location)
	if p.Result != operatorConflictResultManaged && p.Result != operatorConflictResultProtected {
		return nil, fmt.Errorf("invalid result %q", p.Result)
	}
	if p.Usage != operatorActionUsageTarget && p.Usage != operatorActionUsageRestore {
		return nil, fmt.Errorf("invalid usage %q", p.Usage)
	}
	if p.Location == "" {
		return nil, fmt.Errorf("location is empty")
	}
	return &p, nil
}

func findOperatorConflictSource(
	items []ocrItem,
	data *sellProductSelectionDataFile,
) (location string, promptText string, ok bool) {
	if data == nil {
		return "", "", false
	}
	for _, item := range sortOCRItemsByPosition(items) {
		text := stripSeparators(item.text)
		if text == "" {
			continue
		}
		for _, locationName := range data.LocationOrder {
			entry, exists := data.Locations[locationName]
			if !exists {
				continue
			}
			for _, expected := range selectionExpectedNames(entry.Names) {
				candidate := stripSeparators(expected)
				if candidate != "" && strings.Contains(text, candidate) {
					return locationName, item.text, true
				}
			}
		}
	}
	return "", firstOperatorConflictPromptText(items), false
}

func firstOperatorConflictPromptText(items []ocrItem) string {
	items = sortOCRItemsByPosition(items)
	if len(items) == 0 {
		return ""
	}
	return items[0].text
}
