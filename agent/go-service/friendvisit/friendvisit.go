package friendvisit

import (
	"encoding/json"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

var (
	supportScrollCount int
	intelScrollCount   int
)

type FriendVisitResetAction struct{}

func (a *FriendVisitResetAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	supportScrollCount = 0
	intelScrollCount = 0
	log.Info().Msg("[FriendVisit]Reset scroll counters")
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
				Str("mode", mode).
				Msg("[FriendVisit]Match found, click and enter visit flow")
			*counter = 0
			if params.NextOnHit != "" {
				ctx.OverrideNext(arg.CurrentTaskName, []string{params.NextOnHit})
			}
			return true
		}
	}

	if *counter >= params.MaxScroll {
		log.Info().
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
