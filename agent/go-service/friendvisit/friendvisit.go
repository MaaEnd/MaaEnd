package friendvisit

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

var (
	supportScrollCount int
	intelScrollCount   int
	firstEntry         bool
)

type FriendVisitResetAction struct{}

func (a *FriendVisitResetAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	supportScrollCount = 0
	intelScrollCount = 0
	firstEntry = true
	log.Info().Str("node", arg.CurrentTaskName).Msg("[FriendVisit]Reset scroll counters")
	return true
}

type friendVisitScanParam struct {
	Mode           string  `json:"Mode"`
	Template       string  `json:"Template"`
	Threshold      float64 `json:"Threshold"`
	ROI            []int   `json:"ROI"`
	MaxScroll      int     `json:"MaxScroll"`
	ScrollDy       int     `json:"ScrollDy"`
	NextOnHit      string  `json:"NextOnHit"`
	NextOnContinue string  `json:"NextOnContinue"`
	NextOnExhaust  string  `json:"NextOnExhaust"`
}

type FriendVisitScanAction struct{}

func (a *FriendVisitScanAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params friendVisitScanParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("[FriendVisit]Failed to parse CustomActionParam")
		return false
	}

	if params.Template == "" {
		log.Error().Msg("[FriendVisit]Template is required")
		return false
	}

	mode := strings.ToLower(params.Mode)
	counter := getScrollCounter(mode)

	if params.MaxScroll <= 0 {
		params.MaxScroll = 12
	}
	if params.ScrollDy == 0 {
		params.ScrollDy = -240
	}
	if params.Threshold <= 0 {
		params.Threshold = 0.8
	}

	log.Info().
		Str("node", arg.CurrentTaskName).
		Str("mode", mode).
		Str("template", params.Template).
		Int("scrollCount", *counter).
		Int("maxScroll", params.MaxScroll).
		Msg("[FriendVisit]Scan start")

	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[FriendVisit]Controller is nil")
		return false
	}

	controller.PostScreencap().Wait()
	img := controller.CacheImage()
	if img == nil {
		log.Error().Msg("[FriendVisit]Failed to get screenshot")
		return false
	}

	roi := params.ROI
	if len(roi) != 4 || roi[2] <= 0 || roi[3] <= 0 {
		bounds := img.Bounds()
		roi = []int{bounds.Min.X, bounds.Min.Y, bounds.Dx(), bounds.Dy()}
	}

	detail := ctx.RunRecognitionDirect("TemplateMatch", maa.NodeTemplateMatchParam{
		Template:  []string{params.Template},
		Threshold: []float64{params.Threshold},
		ROI:       maa.NewTargetRect(maa.Rect{roi[0], roi[1], roi[2], roi[3]}),
	}, img)

	if detail != nil && detail.Hit {
		clicked := tryClickMatch(controller, detail.DetailJson)
		if clicked {
			log.Info().
				Str("node", arg.CurrentTaskName).
				Str("mode", mode).
				Msg("[FriendVisit]Match found, click and enter visit flow")
			firstEntry = false
			*counter = 0
			if params.NextOnHit != "" {
				ctx.OverrideNext(arg.CurrentTaskName, []string{params.NextOnHit})
			}
			return true
		}
	}

	if *counter >= params.MaxScroll {
		log.Info().
			Str("node", arg.CurrentTaskName).
			Str("mode", mode).
			Int("scrollCount", *counter).
			Msg("[FriendVisit]Scroll exhausted, return to quota check")
		*counter = 0
		if params.NextOnExhaust != "" {
			ctx.OverrideNext(arg.CurrentTaskName, []string{params.NextOnExhaust})
		}
		return true
	}

	controller.PostScroll(0, int32(params.ScrollDy)).Wait()
	*counter++
	log.Info().
		Str("node", arg.CurrentTaskName).
		Str("mode", mode).
		Int("scrollCount", *counter).
		Int("scrollDy", params.ScrollDy).
		Msg("[FriendVisit]Scroll list and continue")
	time.Sleep(200 * time.Millisecond)
	if params.NextOnContinue != "" {
		ctx.OverrideNext(arg.CurrentTaskName, []string{params.NextOnContinue})
	}
	return true
}

func getScrollCounter(mode string) *int {
	if mode == "intel" {
		return &intelScrollCount
	}
	return &supportScrollCount
}

func tryClickMatch(controller *maa.Controller, detailJSON string) bool {
	var matchDetail struct {
		Filtered []struct {
			Box [4]int `json:"box"`
		} `json:"filtered"`
		All []struct {
			Box [4]int `json:"box"`
		} `json:"all"`
	}

	if err := json.Unmarshal([]byte(detailJSON), &matchDetail); err != nil {
		log.Error().Err(err).Msg("[FriendVisit]Failed to parse TemplateMatch detail")
		return false
	}

	boxes := matchDetail.Filtered
	if len(boxes) == 0 {
		boxes = matchDetail.All
	}
	if len(boxes) == 0 {
		return false
	}

	box := boxes[0].Box
	centerX := box[0] + box[2]/2
	centerY := box[1] + box[3]/2
	controller.PostClick(int32(centerX), int32(centerY)).Wait()
	return true
}

type friendVisitCheckQuotaParam struct {
	ROI        []int   `json:"ROI"`
	Expected   string  `json:"Expected"`
	Threshold  float64 `json:"Threshold"`
	NextOnHit  string  `json:"NextOnHit"`
	NextOnMiss string  `json:"NextOnMiss"`
}

type FriendVisitCheckQuotaAction struct{}

func (a *FriendVisitCheckQuotaAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params friendVisitCheckQuotaParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("[FriendVisit]Failed to parse quota param")
		return false
	}

	if len(params.ROI) != 4 {
		log.Error().Str("node", arg.CurrentTaskName).Msg("[FriendVisit]Invalid OCR ROI")
		return false
	}
	if params.Threshold <= 0 {
		params.Threshold = 0.6
	}

	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[FriendVisit]Controller is nil")
		return false
	}

	controller.PostScreencap().Wait()
	img := controller.CacheImage()
	if img == nil {
		log.Error().Msg("[FriendVisit]Failed to get screenshot")
		return false
	}

	ocrParam := &maa.NodeOCRParam{
		ROI:       maa.NewTargetRect(maa.Rect{params.ROI[0], params.ROI[1], params.ROI[2], params.ROI[3]}),
		OrderBy:   "Expected",
		Expected:  []string{".*"},
		Threshold: params.Threshold,
	}

	detail := ctx.RunRecognitionDirect(maa.NodeRecognitionTypeOCR, ocrParam, img)
	text := ""
	if detail != nil && detail.DetailJson != "" {
		text = extractTextFromOCR(detail.DetailJson)
	}

	log.Info().
		Str("node", arg.CurrentTaskName).
		Str("text", text).
		Str("expected", params.Expected).
		Msg("[FriendVisit]OCR result")

	matched := false
	if params.Expected != "" && text != "" {
		re, err := regexp.Compile(params.Expected)
		if err != nil {
			log.Error().Err(err).Str("expected", params.Expected).Msg("[FriendVisit]Invalid OCR regex")
		} else if re.MatchString(text) {
			matched = true
		}
	}

	if matched {
		if params.NextOnHit != "" {
			ctx.OverrideNext(arg.CurrentTaskName, []string{params.NextOnHit})
		}
		return true
	}

	if params.NextOnMiss != "" {
		ctx.OverrideNext(arg.CurrentTaskName, []string{params.NextOnMiss})
	}
	return true
}

func extractTextFromOCR(detailJSON string) string {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(detailJSON), &raw); err != nil {
		return ""
	}

	for _, key := range []string{"filtered", "best", "all"} {
		if data, ok := raw[key]; ok {
			switch v := data.(type) {
			case []interface{}:
				if len(v) > 0 {
					if result, ok := v[0].(map[string]interface{}); ok {
						if text, ok := result["text"].(string); ok {
							return text
						}
					}
				}
			case map[string]interface{}:
				if text, ok := v["text"].(string); ok {
					return text
				}
			}
		}
	}

	return ""
}

type friendVisitFirstEntryExitParam struct {
	NextOnElse string `json:"NextOnElse"`
}

type FriendVisitFirstEntryExitAction struct{}

func (a *FriendVisitFirstEntryExitAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params friendVisitFirstEntryExitParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("[FriendVisit]Failed to parse FirstEntryExit param")
		return false
	}

	if firstEntry {
		log.Info().Str("node", arg.CurrentTaskName).Msg("[FriendVisit]First entry exhausted, exit task")
		controller := ctx.GetTasker().GetController()
		if controller != nil {
			controller.PostClickKey(27).Wait()
		}
		ctx.GetTasker().PostStop()
		return true
	}

	if params.NextOnElse != "" {
		ctx.OverrideNext(arg.CurrentTaskName, []string{params.NextOnElse})
	}
	return true
}
