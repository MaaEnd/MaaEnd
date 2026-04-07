package visitfriends

import (
	"encoding/json"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type friendItem struct {
	Name                string
	ClueExchange        bool
	ControlNexusAssist  bool
	MFGCabinAssist      bool
	GrowthChamberAssist bool
}

var (
	scannedFriendItems       []friendItem
	maxAssistCount           = 5
	maxClueExchangeCount     = 5
	currentAssistCount       = 0
	currentClueExchangeCount = 0
	lastScrollItemName       string
)

func isFriendNameExist(name string) bool {
	if name == "" {
		return false
	}
	for _, item := range scannedFriendItems {
		if item.Name == name {
			return true
		}
	}
	return false
}

func upsertScannedFriendItem(item friendItem) {
	for i := range scannedFriendItems {
		if scannedFriendItems[i].Name != item.Name {
			continue
		}
		if item.ClueExchange {
			scannedFriendItems[i].ClueExchange = true
		}
		if item.ControlNexusAssist {
			scannedFriendItems[i].ControlNexusAssist = true
		}
		if item.MFGCabinAssist {
			scannedFriendItems[i].MFGCabinAssist = true
		}
		if item.GrowthChamberAssist {
			scannedFriendItems[i].GrowthChamberAssist = true
		}
		return
	}
	scannedFriendItems = append(scannedFriendItems, item)
}

type VisitFriendsMainAction struct{}

func (a *VisitFriendsMainAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().
		Str("component", "VisitFriends").
		Str("step", "main_run").
		Msg("start")
	scannedFriendItems = []friendItem{}
	currentAssistCount = 0
	currentClueExchangeCount = 0
	lastScrollItemName = ""
	return true
}

type scanResultItem struct {
	ButtonBox []int  `json:"button_box"`
	NameText  string `json:"name_text"`
}

type VisitFriendsMenuScanTargetFriendOpenRecognition struct{}

func (r *VisitFriendsMenuScanTargetFriendOpenRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	var params struct {
		OnlyRemarkFriends bool `json:"only_remark_friends"`
	}

	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().
			Err(err).
			Msg("failed to parse CustomRecognitionParam")
		return nil, false
	}

	detail, recoErr := ctx.RunRecognition("VisitFriendsRecognitionItemWithName", arg.Img)
	if recoErr != nil || detail == nil {
		log.Error().Err(recoErr).Str("component", "VisitFriends").Str("step", "scan_item_name").Msg("run recognition")
		return nil, false
	}

	if !detail.Hit || detail.CombinedResult == nil || len(detail.CombinedResult) < 2 {
		log.Warn().Str("component", "VisitFriends").Str("step", "scan_item_name").Msg("recognition miss")
		return nil, false
	}

	var detailNameJson, detailButtonJson struct {
		Filtered []struct {
			Box   []int   `json:"box"`
			Score float64 `json:"score"`
			Text  string  `json:"text"`
		} `json:"filtered"`
	}
	// Results.Best是空，暂时只能这样获取
	if detailJsonErr := json.Unmarshal([]byte(detail.CombinedResult[0].DetailJson), &detailButtonJson); detailJsonErr != nil {
		log.Error().Err(detailJsonErr).Str("component", "VisitFriends").Str("step", "scan_item_name").Msg("parse detail json")
		return nil, false
	}
	if detailJsonErr := json.Unmarshal([]byte(detail.CombinedResult[1].DetailJson), &detailNameJson); detailJsonErr != nil {
		log.Error().Err(detailJsonErr).Str("component", "VisitFriends").Str("step", "scan_item_name").Msg("parse detail json")
		return nil, false
	}

	if len(detailNameJson.Filtered) != len(detailButtonJson.Filtered) {
		log.Warn().Str("component", "VisitFriends").Str("step", "scan_item_name").Msg("name recognition count not equal button recognition count")
		return nil, false
	}

	var targetItem scanResultItem
	hasTarget := false

	for i := range detailNameJson.Filtered {
		if len(detailNameJson.Filtered[i].Text) == 0 {
			log.Warn().Str("component", "VisitFriends").Str("step", "scan_item_name").Int("index", i).Msg("name recognition text is empty")
			continue
		}

		if params.OnlyRemarkFriends {
			// 如果只助力备注好友，且这个好友没有备注，则跳过
			if !strings.Contains(detailNameJson.Filtered[i].Text, "(") && !strings.Contains(detailNameJson.Filtered[i].Text, "（") {
				log.Debug().Str("name", detailNameJson.Filtered[i].Text).Msg("friend has no remark, skip")
				continue
			}
		}

		exist := isFriendNameExist(detailNameJson.Filtered[i].Text)
		if exist {
			log.Debug().Str("name", detailNameJson.Filtered[i].Text).Msg("friend item already exist, skip")
			continue
		}

		hasTarget = true
		targetItem.NameText = detailNameJson.Filtered[i].Text
		targetItem.ButtonBox = detailButtonJson.Filtered[i].Box
		break
	}

	if !hasTarget {
		return nil, false
	}

	resultJson, err := json.Marshal(targetItem)
	if err != nil {
		log.Error().Err(err).Str("component", "VisitFriends").Str("step", "scan_item_name").Msg("marshal result json")
		return nil, false
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(resultJson),
	}, true
}

type VisitFriendsMenuScanTargetFriendOpenAction struct{}

func (a *VisitFriendsMenuScanTargetFriendOpenAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	customResult, ok := arg.RecognitionDetail.Results.Best.AsCustom()
	if !ok {
		log.Error().Str("component", "VisitFriends").Str("step", "open_item").Msg("get custom result")
		return false
	}

	var resultItem scanResultItem
	if err := json.Unmarshal([]byte(customResult.Detail), &resultItem); err != nil {
		log.Error().
			Err(err).
			Str("component", "VisitFriends").Str("step", "open_item_text").
			Msg("parse custom result")
		return false
	}

	var actionParams struct {
		ParamAttachNode string `json:"param_attach_node"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &actionParams); err != nil {
		log.Error().
			Err(err).
			Msg("failed to parse CustomActionParam")
		return false
	}

	raw, err := ctx.GetNodeJSON(actionParams.ParamAttachNode)
	if err != nil || raw == "" {
		log.Error().Err(err).Str("component", "VisitFriends").Str("step", "open_item").Msg("get node json for custom action param")
		return false
	}

	var nodeWithAttach struct {
		Attach struct {
			ControlNexusAssist  bool `json:"control_nexus_assist"`
			MFGCabinAssist      bool `json:"mfg_cabin_assist"`
			GrowthChamberAssist bool `json:"growth_chamber_assist"`
		} `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &nodeWithAttach); err != nil {
		log.Error().Err(err).Str("component", "VisitFriends").Str("step", "open_item").Msg("parse node attach for visit friends open action")
		return false
	}
	params := nodeWithAttach.Attach

	if !params.ControlNexusAssist && !params.MFGCabinAssist && !params.GrowthChamberAssist {
		log.Error().Str("component", "VisitFriends").Str("step", "open_item").Msg("no assist enabled, skip open item action")
		return false
	}

	if len(resultItem.ButtonBox) < 4 {
		log.Error().Str("component", "VisitFriends").Str("step", "open_item").Msg("button box length error")
		return false
	}

	{
		// 检查该好友有哪些可以助力
		maafocus.Print(ctx, i18n.T("visitfriends.check_friend", resultItem.NameText))
		enableOpen := currentAssistCount < maxAssistCount
		override := map[string]any{
			"VisitFriendsMenuScanDetailClueExchange": map[string]any{
				"custom_recognition_param": map[string]any{
					"friend_name": resultItem.NameText,
					"enter_button_box": maa.Rect{
						resultItem.ButtonBox[0],
						resultItem.ButtonBox[1],
						resultItem.ButtonBox[2],
						resultItem.ButtonBox[3],
					},
				},
			},
			"VisitFriendsMenuScanDetailOpen": map[string]any{
				"enabled": enableOpen,
				"target": maa.Rect{
					resultItem.ButtonBox[0],
					resultItem.ButtonBox[1],
					resultItem.ButtonBox[2],
					resultItem.ButtonBox[3],
				},
			},
			"VisitFriendsMenuScanDetailSaveAssist": map[string]any{
				"custom_recognition_param": map[string]any{
					"friend_name": resultItem.NameText,
				},
			},
		}
		_, err := ctx.RunTask("VisitFriendsMenuScanDetail", override)
		if err != nil {
			log.Error().Err(err).Msg("VisitFriendsMenuScanTargetFriendOpenAction: failed to run task")
			return false
		}
	}

	{
		// 取出上一步检查的结果
		foundTarget := false
		var targetFriendItem friendItem
		for i := range scannedFriendItems {
			if scannedFriendItems[i].Name != resultItem.NameText {
				continue
			}

			foundTarget = true
			targetFriendItem = scannedFriendItems[i]
		}

		if !foundTarget {
			log.Error().
				Str("component", "VisitFriends").
				Str("step", "open_item").
				Str("friend_name", resultItem.NameText).
				Msg("opened friend not found in scanned items")
			return false
		}

		hasTarget := false
		missControlNexusAssist := false
		missMFGCabinAssist := false
		missGrowthChamberAssist := false
		missClueExchange := false
		needControlNexusAssist := false
		needMFGCabinAssist := false
		needGrowthChamberAssist := false
		needClueExchange := false

		log.Debug().Any("targetFriendItem", targetFriendItem).Any("params", params).Msg("check target friend item and params")

		if currentClueExchangeCount < maxClueExchangeCount {
			if targetFriendItem.ClueExchange {
				needClueExchange = true
				hasTarget = true
			} else {
				missClueExchange = true
			}
		}
		if currentAssistCount < maxAssistCount {
			if params.ControlNexusAssist {
				if targetFriendItem.ControlNexusAssist {
					needControlNexusAssist = true
					hasTarget = true
				} else {
					missControlNexusAssist = true
				}
			}
			if params.MFGCabinAssist {
				if targetFriendItem.MFGCabinAssist {
					needMFGCabinAssist = true
					hasTarget = true
				} else {
					missMFGCabinAssist = true
				}
			}
			if params.GrowthChamberAssist {
				if targetFriendItem.GrowthChamberAssist {
					needGrowthChamberAssist = true
					hasTarget = true
				} else {
					missGrowthChamberAssist = true
				}
			}
		}

		if !hasTarget {
			var missParts []string
			if missClueExchange {
				missParts = append(missParts, i18n.T("visitfriends.clue_exchange"))
			}
			if missControlNexusAssist {
				missParts = append(missParts, i18n.T("visitfriends.control_nexus_assist"))
			}
			if missMFGCabinAssist {
				missParts = append(missParts, i18n.T("visitfriends.mfg_cabin_assist"))
			}
			if missGrowthChamberAssist {
				missParts = append(missParts, i18n.T("visitfriends.growth_chamber_assist"))
			}
			message := i18n.T("visitfriends.friend_missing", strings.Join(missParts, i18n.Separator()))
			maafocus.Print(ctx, message)
			return true
		}

		var canParts []string
		if currentClueExchangeCount < maxClueExchangeCount && targetFriendItem.ClueExchange {
			canParts = append(canParts, i18n.T("visitfriends.can_clue_exchange"))
		}
		if params.ControlNexusAssist && targetFriendItem.ControlNexusAssist {
			canParts = append(canParts, i18n.T("visitfriends.can_control_nexus"))
		}
		if params.MFGCabinAssist && targetFriendItem.MFGCabinAssist {
			canParts = append(canParts, i18n.T("visitfriends.can_mfg_cabin"))
		}
		if params.GrowthChamberAssist && targetFriendItem.GrowthChamberAssist {
			canParts = append(canParts, i18n.T("visitfriends.can_growth_chamber"))
		}
		if len(canParts) > 0 {
			message := i18n.T("visitfriends.found_target_with", strings.Join(canParts, i18n.Separator()))
			maafocus.Print(ctx, message)
		} else {
			maafocus.Print(ctx, i18n.T("visitfriends.found_target"))
		}

		lastScrollItemName = "" // 打开好友后重置，避免上次滚动的好友和这次打开的好友名字一样导致误判滚动结束
		override := map[string]any{
			"VisitFriendsEnterShip": map[string]any{
				"target": maa.Rect{
					resultItem.ButtonBox[0],
					resultItem.ButtonBox[1],
					resultItem.ButtonBox[2],
					resultItem.ButtonBox[3],
				},
			},
			"VisitFriendsMenuClueExchange": map[string]any{
				"enabled": needClueExchange,
			},
			"VisitFriendsMenuAssistControlNexus": map[string]any{
				"enabled": needControlNexusAssist,
			},
			"VisitFriendsMenuAssistMFGCabin1": map[string]any{
				"enabled": needMFGCabinAssist,
			},
			"VisitFriendsMenuAssistMFGCabin2": map[string]any{
				"enabled": needMFGCabinAssist,
			},
			"VisitFriendsMenuAssistGrowthChamberSwipe": map[string]any{
				"enabled": needGrowthChamberAssist,
			},
		}
		_, err := ctx.RunTask("VisitFriendsEnterShip", override)
		if err != nil {
			log.Error().Err(err).Msg("VisitFriendsMenuScanTargetFriendOpenAction: failed to run task")
			return false
		}
	}

	return true
}

type VisitFriendsMenuScanDetailClueExchangeRecognition struct{}

func (r *VisitFriendsMenuScanDetailClueExchangeRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	var params struct {
		FriendName     string `json:"friend_name"`
		EnterButtonBox []int  `json:"enter_button_box"`
	}
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().
			Err(err).
			Msg("failed to parse CustomRecognitionParam")
		return nil, false
	}
	var item = friendItem{
		Name:                params.FriendName,
		ClueExchange:        false,
		ControlNexusAssist:  false,
		MFGCabinAssist:      false,
		GrowthChamberAssist: false,
	}

	if len(params.EnterButtonBox) != 4 {
		log.Error().Msg("invalid EnterButtonBox in CustomRecognitionParam")
		return nil, false
	}

	{
		override := map[string]any{
			"VisitFriendsRecognitionItemClueExchangeByEnterButton": map[string]any{
				"roi": maa.Rect{
					params.EnterButtonBox[0],
					params.EnterButtonBox[1],
					params.EnterButtonBox[2],
					params.EnterButtonBox[3],
				},
			},
		}
		detail, err := ctx.RunRecognition("VisitFriendsRecognitionItemClueExchangeByEnterButton", arg.Img, override)
		if err != nil || detail == nil {
			log.Error().Err(err).Msg("Failed to run recognition VisitFriendsRecognitionItemClueExchangeByEnterButton")
			return nil, false
		}
		item.ClueExchange = detail.Hit
	}

	upsertScannedFriendItem(item)

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

type VisitFriendsMenuScanDetailAssistRecognition struct{}

func (r *VisitFriendsMenuScanDetailAssistRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	var params struct {
		FriendName string `json:"friend_name"`
	}
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().
			Err(err).
			Msg("failed to parse CustomRecognitionParam")
		return nil, false
	}
	var item = friendItem{
		Name:                params.FriendName,
		ClueExchange:        false,
		ControlNexusAssist:  false,
		MFGCabinAssist:      false,
		GrowthChamberAssist: false,
	}

	{
		detail, err := ctx.RunRecognition("VisitFriendsRecognitionItemDetailControlNexusAssist", arg.Img)
		if err != nil || detail == nil {
			log.Error().Err(err).Msg("Failed to run recognition VisitFriendsRecognitionItemDetailControlNexusAssist")
			return nil, false
		}
		item.ControlNexusAssist = detail.Hit
	}
	{
		detail, err := ctx.RunRecognition("VisitFriendsRecognitionItemDetailMFGCabinAssist", arg.Img)
		if err != nil || detail == nil {
			log.Error().Err(err).Msg("Failed to run recognition VisitFriendsRecognitionItemDetailMFGCabinAssist")
			return nil, false
		}
		item.MFGCabinAssist = detail.Hit
	}
	{
		detail, err := ctx.RunRecognition("VisitFriendsRecognitionItemDetailGrowthChamberAssist", arg.Img)
		if err != nil || detail == nil {
			log.Error().Err(err).Msg("Failed to run recognition VisitFriendsRecognitionItemDetailGrowthChamberAssist")
			return nil, false
		}
		item.GrowthChamberAssist = detail.Hit
	}

	upsertScannedFriendItem(item)

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

type VisitFriendsMenuScanScrollFinishRecognition struct{}

func (r *VisitFriendsMenuScanScrollFinishRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	detail, recoErr := ctx.RunRecognition("VisitFriendsRecognitionItemWithName", arg.Img)
	if recoErr != nil || detail == nil {
		log.Error().Err(recoErr).Str("component", "VisitFriends").Str("step", "scan_finish_name").Msg("run recognition")
		return nil, false
	}

	if !detail.Hit || detail.CombinedResult == nil || len(detail.CombinedResult) < 2 {
		log.Warn().Str("component", "VisitFriends").Str("step", "scan_finish_name").Msg("recognition miss")
		return nil, false
	}

	var detailJson struct {
		Filtered []struct {
			Box   []int   `json:"box"`
			Score float64 `json:"score"`
			Text  string  `json:"text"`
		} `json:"filtered"`
	}
	// Results.Best是空，暂时只能这样获取
	if detailJsonErr := json.Unmarshal([]byte(detail.CombinedResult[1].DetailJson), &detailJson); detailJsonErr != nil {
		log.Error().Err(detailJsonErr).Str("component", "VisitFriends").Str("step", "scan_finish_name").Msg("parse detail json")
		return nil, false
	}

	if len(detailJson.Filtered) == 0 {
		log.Info().Str("component", "VisitFriends").Str("step", "scan_finish_name").Msg("no item found")
		return nil, false
	}

	lastDetailItem := detailJson.Filtered[len(detailJson.Filtered)-1]
	if len(lastDetailItem.Text) == 0 {
		log.Info().Str("component", "VisitFriends").Str("step", "scan_finish_name").Msg("last item has no name")
		return nil, false
	}

	if lastScrollItemName != lastDetailItem.Text {
		lastScrollItemName = lastDetailItem.Text
		return nil, false
	}

	log.Info().Str("name", lastDetailItem.Text).Msg("last friend item name is same as previous, scroll finish")
	detailJSON, _ := json.Marshal(map[string]string{"last_name": lastDetailItem.Text})
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

type VisitFriendsMenuScanScrollFullRecognition struct{}

func (r *VisitFriendsMenuScanScrollFullRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	result := true
	if currentAssistCount < maxAssistCount {
		log.Info().
			Int("currentAssistCount", currentAssistCount).
			Int("maxAssistCount", maxAssistCount).
			Msg("assist count not reach max, scroll not full")
		result = false
	}
	if currentClueExchangeCount < maxClueExchangeCount {
		log.Info().
			Int("currentClueExchangeCount", currentClueExchangeCount).
			Int("maxClueExchangeCount", maxClueExchangeCount).
			Msg("clue exchange count not reach max, scroll not full")
		result = false
	}

	if !result {
		message := i18n.T("visitfriends.current_counts", currentAssistCount, currentClueExchangeCount)
		maafocus.Print(ctx, message)
		return nil, false
	}
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

type VisitFriendsMenuClueExchangeAction struct{}

func (a *VisitFriendsMenuClueExchangeAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	currentClueExchangeCount++
	ctx.RunAction("VisitFriendsMenuClickAction", arg.Box, "", nil)
	return true
}

type VisitFriendsMenuClueAssistAction struct{}

func (a *VisitFriendsMenuClueAssistAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	currentAssistCount++
	ctx.RunAction("VisitFriendsMenuClickAction", arg.Box, "", nil)
	return true
}

type VisitFriendsMenuClueExchangeFullAction struct{}

func (a *VisitFriendsMenuClueExchangeFullAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	currentClueExchangeCount = maxClueExchangeCount
	return true
}

type VisitFriendsMenuClueAssistFullAction struct{}

// Compile-time interface checks
var (
	_ maa.CustomActionRunner      = &VisitFriendsMainAction{}
	_ maa.CustomRecognitionRunner = &VisitFriendsMenuScanTargetFriendOpenRecognition{}
	_ maa.CustomActionRunner      = &VisitFriendsMenuScanTargetFriendOpenAction{}
	_ maa.CustomRecognitionRunner = &VisitFriendsMenuScanDetailAssistRecognition{}
	_ maa.CustomRecognitionRunner = &VisitFriendsMenuScanDetailClueExchangeRecognition{}
	_ maa.CustomRecognitionRunner = &VisitFriendsMenuScanScrollFinishRecognition{}
	_ maa.CustomRecognitionRunner = &VisitFriendsMenuScanScrollFullRecognition{}
	_ maa.CustomActionRunner      = &VisitFriendsMenuClueExchangeAction{}
	_ maa.CustomActionRunner      = &VisitFriendsMenuClueAssistAction{}
	_ maa.CustomActionRunner      = &VisitFriendsMenuClueExchangeFullAction{}
	_ maa.CustomActionRunner      = &VisitFriendsMenuClueAssistFullAction{}
)

func (a *VisitFriendsMenuClueAssistFullAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	currentAssistCount = maxAssistCount
	return true
}
