package exprcoord

import (
	"encoding/json"
	"image"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &MpExprTemplateMatchRecognition{}
var _ maa.CustomRecognitionRunner = &MpExprOCRRecognition{}
var _ maa.CustomRecognitionRunner = &MpExprOCRBottomStableRecognition{}

type MpExprTemplateMatchRecognition struct{}

type MpExprOCRRecognition struct{}

type MpExprOCRBottomStableRecognition struct{}

type templateMatchParams struct {
	ROI       []any     `json:"roi"`
	Template  []string  `json:"template"`
	Threshold []float64 `json:"threshold,omitempty"`
	GreenMask bool      `json:"green_mask,omitempty"`
	OrderBy   string    `json:"order_by,omitempty"`
	Index     int       `json:"index,omitempty"`
	Method    int       `json:"method,omitempty"`
}

type ocrParams struct {
	ROI       []any       `json:"roi"`
	Expected  []string    `json:"expected"`
	Threshold float64     `json:"threshold,omitempty"`
	Replace   [][2]string `json:"replace,omitempty"`
	OrderBy   string      `json:"order_by,omitempty"`
	Index     int         `json:"index,omitempty"`
	OnlyRec   bool        `json:"only_rec,omitempty"`
	Model     string      `json:"model,omitempty"`
}

type ocrBottomStableParams struct {
	ocrParams
	Key string `json:"key,omitempty"`
}

var mpExprOCRBottomState = struct {
	sync.Mutex
	last map[string]string
}{last: map[string]string{}}

// Run executes TemplateMatch with expression-based ROI.
func (r *MpExprTemplateMatchRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	var params templateMatchParams
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "MpExprTemplateMatch").Msg("failed to parse params")
		return nil, false
	}
	bounds := arg.Img.Bounds()
	roi, err := ResolveRect(params.ROI, bounds.Dx(), bounds.Dy())
	if err != nil {
		log.Error().Err(err).Str("component", "MpExprTemplateMatch").Str("expr", marshalRaw(params.ROI)).Msg("failed to resolve ROI")
		return nil, false
	}
	pipeline := maa.NewPipeline()
	node := maa.NewNode("MpExprTemplateMatchInner").SetRecognition(maa.RecTemplateMatch(maa.TemplateMatchParam{
		ROI:       maa.NewTargetRect(roi),
		Template:  params.Template,
		Threshold: params.Threshold,
		GreenMask: params.GreenMask,
		OrderBy:   maa.TemplateMatchOrderBy(params.OrderBy),
		Index:     params.Index,
		Method:    maa.TemplateMatchMethod(params.Method),
	}))
	pipeline.AddNode(node)
	detail, err := ctx.RunRecognition(node.Name, arg.Img, pipeline)
	if err != nil {
		log.Error().Err(err).Str("component", "MpExprTemplateMatch").Msg("inner recognition failed")
		return nil, false
	}
	if detail == nil || !detail.Hit {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: detail.Box, Detail: detail.DetailJson}, true
}

// Run executes OCR with expression-based ROI.
func (r *MpExprOCRRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	detail, _, err := runMpExprOCR(ctx, arg.Img, arg.CustomRecognitionParam, "MpExprOCR")
	if err != nil || detail == nil || !detail.Hit {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: detail.Box, Detail: detail.DetailJson}, true
}

func (r *MpExprOCRBottomStableRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	var params ocrBottomStableParams
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "MpExprOCRBottomStable").Msg("failed to parse params")
		return nil, false
	}
	detail, roi, err := runMpExprOCRWithParams(ctx, arg.Img, params.ocrParams, "MpExprOCRBottomStable")
	if err != nil || detail == nil || !detail.Hit {
		return nil, false
	}
	text, ok := bottomOCRText(detail)
	if !ok {
		log.Debug().Str("component", "MpExprOCRBottomStable").Msg("no bottom OCR text")
		return nil, false
	}
	key := params.Key
	if key == "" {
		key = marshalRaw(params.ROI)
	}
	text = normalizeOCRBottomText(text)

	mpExprOCRBottomState.Lock()
	last := mpExprOCRBottomState.last[key]
	mpExprOCRBottomState.last[key] = text
	mpExprOCRBottomState.Unlock()

	if last == "" || last != text {
		log.Debug().Str("component", "MpExprOCRBottomStable").Str("key", key).Str("last", last).Str("current", text).Msg("bottom OCR changed")
		return nil, false
	}
	log.Info().Str("component", "MpExprOCRBottomStable").Str("key", key).Str("bottom", text).Msg("bottom OCR stable")
	return &maa.CustomRecognitionResult{Box: roi, Detail: detail.DetailJson}, true
}

func runMpExprOCR(ctx *maa.Context, img image.Image, raw string, component string) (*maa.RecognitionDetail, maa.Rect, error) {
	var params ocrParams
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to parse params")
		return nil, maa.Rect{}, err
	}
	return runMpExprOCRWithParams(ctx, img, params, component)
}

func runMpExprOCRWithParams(ctx *maa.Context, img image.Image, params ocrParams, component string) (*maa.RecognitionDetail, maa.Rect, error) {
	bounds := img.Bounds()
	roi, err := ResolveRect(params.ROI, bounds.Dx(), bounds.Dy())
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("expr", marshalRaw(params.ROI)).Msg("failed to resolve ROI")
		return nil, maa.Rect{}, err
	}
	pipeline := maa.NewPipeline()
	node := maa.NewNode(component + "Inner").SetRecognition(maa.RecOCR(maa.OCRParam{
		ROI:       maa.NewTargetRect(roi),
		Expected:  params.Expected,
		Threshold: params.Threshold,
		Replace:   params.Replace,
		OrderBy:   maa.OCROrderBy(params.OrderBy),
		Index:     params.Index,
		OnlyRec:   params.OnlyRec,
		Model:     params.Model,
	}))
	pipeline.AddNode(node)
	detail, err := ctx.RunRecognition(node.Name, img, pipeline)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("inner recognition failed")
		return nil, maa.Rect{}, err
	}
	return detail, roi, nil
}

func bottomOCRText(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil || detail.Results == nil {
		return "", false
	}
	var bestText string
	bestY := -1
	for _, bucket := range [][]*maa.RecognitionResult{detail.Results.Filtered, detail.Results.All, {detail.Results.Best}} {
		for _, result := range bucket {
			if result == nil {
				continue
			}
			ocr, ok := result.AsOCR()
			if !ok || ocr == nil || strings.TrimSpace(ocr.Text) == "" {
				continue
			}
			y := ocr.Box.Y()
			if y > bestY {
				bestY = y
				bestText = ocr.Text
			}
		}
		if bestText != "" {
			return bestText, true
		}
	}
	return "", false
}

func normalizeOCRBottomText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "×", "x")
	text = strings.ReplaceAll(text, "*", "x")
	text = strings.ReplaceAll(text, " ", "")
	return strings.ToLower(text)
}

func resetMpExprOCRBottomState(key string) {
	mpExprOCRBottomState.Lock()
	defer mpExprOCRBottomState.Unlock()
	if key == "" {
		mpExprOCRBottomState.last = map[string]string{}
		return
	}
	delete(mpExprOCRBottomState.last, key)
}

func marshalRaw(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
