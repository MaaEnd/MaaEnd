package seizedeliveryjobs

import (
	"encoding/json"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type deliveryJobItem struct {
	RewardBox       []int  `json:"reward_box"`
	OriginText      string `json:"origin_text"`
	AcceptBox       []int  `json:"accept_box"`
	ViewLocationBox []int  `json:"view_location_box"` // computed: RewardBox + offset [-214,-12,38,17]
}

var (
	scannedJobItems []deliveryJobItem
	currentIndex    int
)

func computeViewLocationBox(rewardBox []int) []int {
	return []int{
		rewardBox[0] - 214,
		rewardBox[1] - 12,
		38,
		17,
	}
}

type SeizeDeliveryJobsMainAction struct{}

func (a *SeizeDeliveryJobsMainAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().
		Str("component", "SeizeDeliveryJobs").
		Str("step", "main_run").
		Msg("start")
	scannedJobItems = []deliveryJobItem{}
	currentIndex = 0
	return true
}

type SeizeDeliveryJobsScanTargetRecognition struct{}

func (r *SeizeDeliveryJobsScanTargetRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	detail, recoErr := ctx.RunRecognition("SeizeDeliveryJobsFindTarget", arg.Img)
	if recoErr != nil || detail == nil {
		log.Error().Err(recoErr).Str("component", "SeizeDeliveryJobs").Str("step", "scan_target").Msg("run recognition")
		return nil, false
	}

	if !detail.Hit || detail.CombinedResult == nil || len(detail.CombinedResult) < 3 {
		log.Warn().Str("component", "SeizeDeliveryJobs").Str("step", "scan_target").Msg("recognition miss")
		return nil, false
	}

	var rewardDetail struct {
		Filtered []struct {
			Box   []int   `json:"box"`
			Score float64 `json:"score"`
		} `json:"filtered"`
	}
	var originDetail struct {
		Filtered []struct {
			Box   []int   `json:"box"`
			Score float64 `json:"score"`
			Text  string  `json:"text"`
		} `json:"filtered"`
	}
	var acceptDetail struct {
		Filtered []struct {
			Box   []int   `json:"box"`
			Score float64 `json:"score"`
		} `json:"filtered"`
	}

	if err := json.Unmarshal([]byte(detail.CombinedResult[0].DetailJson), &rewardDetail); err != nil {
		log.Error().Err(err).Str("component", "SeizeDeliveryJobs").Str("step", "scan_target").Msg("parse reward detail json")
		return nil, false
	}
	if err := json.Unmarshal([]byte(detail.CombinedResult[1].DetailJson), &originDetail); err != nil {
		log.Error().Err(err).Str("component", "SeizeDeliveryJobs").Str("step", "scan_target").Msg("parse origin detail json")
		return nil, false
	}
	if err := json.Unmarshal([]byte(detail.CombinedResult[2].DetailJson), &acceptDetail); err != nil {
		log.Error().Err(err).Str("component", "SeizeDeliveryJobs").Str("step", "scan_target").Msg("parse accept detail json")
		return nil, false
	}

	if len(rewardDetail.Filtered) != len(originDetail.Filtered) || len(rewardDetail.Filtered) != len(acceptDetail.Filtered) {
		log.Warn().
			Int("reward_count", len(rewardDetail.Filtered)).
			Int("origin_count", len(originDetail.Filtered)).
			Int("accept_count", len(acceptDetail.Filtered)).
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_target").
			Msg("recognition count mismatch")
		return nil, false
	}

	// Build all items from the filtered results
	items := make([]deliveryJobItem, 0, len(rewardDetail.Filtered))
	for i := range rewardDetail.Filtered {
		item := deliveryJobItem{
			RewardBox:       rewardDetail.Filtered[i].Box,
			OriginText:      originDetail.Filtered[i].Text,
			AcceptBox:       acceptDetail.Filtered[i].Box,
			ViewLocationBox: computeViewLocationBox(rewardDetail.Filtered[i].Box),
		}
		items = append(items, item)
	}
	scannedJobItems = items

	maafocus.Print(ctx, i18n.T("seizedeliveryjobs.scanning"))

	if currentIndex >= len(scannedJobItems) {
		maafocus.Print(ctx, i18n.T("seizedeliveryjobs.no_more_targets"))
		return nil, false
	}

	item := scannedJobItems[currentIndex]
	resultJson, err := json.Marshal(item)
	if err != nil {
		log.Error().Err(err).Str("component", "SeizeDeliveryJobs").Str("step", "scan_target").Msg("marshal result json")
		return nil, false
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(resultJson),
	}, true
}

type SeizeDeliveryJobsScanTargetAction struct{}

func (a *SeizeDeliveryJobsScanTargetAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	customResult, ok := arg.RecognitionDetail.Results.Best.AsCustom()
	if !ok {
		log.Error().Str("component", "SeizeDeliveryJobs").Str("step", "scan_action").Msg("get custom result")
		return false
	}

	var item deliveryJobItem
	if err := json.Unmarshal([]byte(customResult.Detail), &item); err != nil {
		log.Error().Err(err).Str("component", "SeizeDeliveryJobs").Str("step", "scan_action").Msg("parse custom result")
		return false
	}

	maafocus.Print(ctx, i18n.T("seizedeliveryjobs.checking_job", currentIndex+1, len(scannedJobItems), item.OriginText))

	// Click "查看位置" to view the delivery destination
	if len(item.ViewLocationBox) < 4 {
		log.Error().Str("component", "SeizeDeliveryJobs").Str("step", "scan_action").Msg("view location box invalid")
		return false
	}
	{
		override := map[string]any{
			"SeizeDeliveryJobsFoundTargetViewLocationClick": map[string]any{
				"target": maa.Rect{
					item.ViewLocationBox[0],
					item.ViewLocationBox[1],
					item.ViewLocationBox[2],
					item.ViewLocationBox[3],
				},
			},
		}
		_, err := ctx.RunTask("SeizeDeliveryJobsFoundTargetViewLocationClick", override)
		if err != nil {
			log.Error().Err(err).Str("component", "SeizeDeliveryJobs").Str("step", "scan_action").Msg("click view location failed")
			return false
		}
	}

	// Check endpoint filter
	{
		detail, err := ctx.RunTask("SeizeDeliveryJobsEndpointFilterCheck", nil)
		if err != nil {
			log.Error().Err(err).Str("component", "SeizeDeliveryJobs").Str("step", "scan_action").Msg("endpoint filter check failed")
			return false
		}

		if detail != nil {
			// Endpoint matched - ESC back and accept the job
			maafocus.Print(ctx, i18n.T("seizedeliveryjobs.endpoint_matched"))

			// ESC back to delivery list
			_, _ = ctx.RunTask("SeizeDeliveryJobsPressEsc", nil)

			// Click "接取运送委托" at the saved AcceptBox position
			maafocus.Print(ctx, i18n.T("seizedeliveryjobs.accepting"))
			if len(item.AcceptBox) < 4 {
				log.Error().Str("component", "SeizeDeliveryJobs").Str("step", "scan_action").Msg("accept box invalid")
				return false
			}
			override := map[string]any{
				"SeizeDeliveryJobsAcceptClick": map[string]any{
					"target": maa.Rect{
						item.AcceptBox[0],
						item.AcceptBox[1],
						item.AcceptBox[2],
						item.AcceptBox[3],
					},
				},
			}
			_, err := ctx.RunTask("SeizeDeliveryJobsAcceptClick", override)
			if err != nil {
				log.Error().Err(err).Str("component", "SeizeDeliveryJobs").Str("step", "scan_action").Msg("accept click failed")
				return false
			}
			return true
		}
	}

	// Endpoint not matched - ESC back and try next
	maafocus.Print(ctx, i18n.T("seizedeliveryjobs.endpoint_not_matched"))
	_, _ = ctx.RunTask("SeizeDeliveryJobsPressEsc", nil)

	currentIndex++
	return true
}

// Compile-time interface checks
var (
	_ maa.CustomActionRunner      = &SeizeDeliveryJobsMainAction{}
	_ maa.CustomRecognitionRunner = &SeizeDeliveryJobsScanTargetRecognition{}
	_ maa.CustomActionRunner      = &SeizeDeliveryJobsScanTargetAction{}
)
