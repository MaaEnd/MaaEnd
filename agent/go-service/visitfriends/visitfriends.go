package visitfriends

import (
	"encoding/json"

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
	menuKeyMap               = make(map[int]friendItem)
	menuKeyNext              int
	maxAssistCount           = 5
	maxClueExchangeCount     = 5
	currentAssistCount       = 0
	currentClueExchangeCount = 0
	lastScrollItemName       string
)

func getFriendItem(key int) (friendItem, bool) {
	item, ok := menuKeyMap[key]
	return item, ok
}

func registerFriendItem(name string) int {
	menuKeyNext++
	key := menuKeyNext
	menuKeyMap[key] = friendItem{Name: name}
	return key
}

func setFriendClueExchange(key int, enabled bool) bool {
	item, ok := menuKeyMap[key]
	if !ok {
		return false
	}
	item.ClueExchange = enabled
	menuKeyMap[key] = item
	return true
}

func setFriendControlNexusAssist(key int, enabled bool) bool {
	item, ok := menuKeyMap[key]
	if !ok {
		return false
	}
	item.ControlNexusAssist = enabled
	menuKeyMap[key] = item
	return true
}

func setFriendMFGCabinAssist(key int, enabled bool) bool {
	item, ok := menuKeyMap[key]
	if !ok {
		return false
	}
	item.MFGCabinAssist = enabled
	menuKeyMap[key] = item
	return true
}

func setFriendGrowthChamberAssist(key int, enabled bool) bool {
	item, ok := menuKeyMap[key]
	if !ok {
		return false
	}
	item.GrowthChamberAssist = enabled
	menuKeyMap[key] = item
	return true
}

func isFriendNameExist(name string) bool {
	if name == "" {
		return false
	}
	for _, item := range menuKeyMap {
		if item.Name == name {
			return true
		}
	}
	return false
}

func getFriendItemsRoi(ctx *maa.Context, arg *maa.CustomRecognitionArg) ([]maa.Rect, bool) {
	detail_items, err := ctx.RunRecognition("VisitFriendsRecognitionItemEnterButton", arg.Img)
	if err != nil || detail_items == nil {
		log.Error().Err(err).Msg("Failed to run recognition VisitFriendsRecognitionItemEnterButton")
		return nil, false
	}
	var friendItemRoiOffset = maa.Rect{-1140, -30, 1175, 65} // 按钮映射到整个item的偏移
	var rois []maa.Rect
	for _, m := range detail_items.Results.Filtered {
		detail, ok := m.AsTemplateMatch()
		if !ok {
			continue
		}
		box := detail.Box
		rois = append(rois, maa.Rect{
			box.X() + friendItemRoiOffset.X(),
			box.Y() + friendItemRoiOffset.Y(),
			box.Width() + friendItemRoiOffset.Width(),
			box.Height() + friendItemRoiOffset.Height(),
		})
	}
	return rois, true
}

func getFriendItemsName(ctx *maa.Context, arg *maa.CustomRecognitionArg, itemRoi maa.Rect) (string, bool) {
	override := map[string]any{
		"VisitFriendsRecognitionItemName": map[string]any{
			"roi": maa.Rect{itemRoi.X() + 80, itemRoi.Y(), 300, 35},
		},
	}
	detail, err := ctx.RunRecognition("VisitFriendsRecognitionItemName", arg.Img, override)
	if err != nil || detail == nil {
		log.Error().
			Str("component", "VisitFriends").
			Str("step", "getFriendItemsName").
			Str("recognition", "VisitFriendsRecognitionItemName").
			Err(err).
			Msg("run recognition failed")
		return "", false
	}
	if detail.Results.Best == nil {
		log.Error().
			Str("component", "VisitFriends").
			Str("step", "getFriendItemsName").
			Str("recognition", "VisitFriendsRecognitionItemName").
			Msg("no best result")
		return "", false
	}
	name, ok := detail.Results.Best.AsOCR()
	if !ok {
		return "", false
	}
	return name.Text, true
}

func getFriendClueExchangeEnable(ctx *maa.Context, arg *maa.CustomRecognitionArg, itemRoi maa.Rect) (bool, bool) {
	override := map[string]any{
		"VisitFriendsRecognitionItemClueExchange": map[string]any{
			"roi": maa.Rect{itemRoi.X() + itemRoi.Width() - 100, itemRoi.Y(), 100, itemRoi.Height()},
		},
	}
	detail, err := ctx.RunRecognition("VisitFriendsRecognitionItemClueExchange", arg.Img, override)
	if err != nil || detail == nil {
		log.Error().
			Str("component", "VisitFriends").
			Str("step", "getFriendClueExchangeEnable").
			Str("recognition", "VisitFriendsRecognitionItemClueExchange").
			Err(err).
			Msg("run recognition failed")
		return false, false
	}
	return detail.Hit, true
}

type VisitFriendsMainAction struct{}

func (a *VisitFriendsMainAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().
		Str("component", "VisitFriends").
		Str("step", "main_run").
		Msg("start")
	menuKeyMap = make(map[int]friendItem)
	menuKeyNext = 0
	currentAssistCount = 0
	currentClueExchangeCount = 0
	lastScrollItemName = ""
	return true
}

type scanTargetResult struct {
	HasTarget           bool     `json:"has_target"`
	Roi                 maa.Rect `json:"roi"`
	ClueExchange        bool     `json:"clue_exchange"`
	ControlNexusAssist  bool     `json:"control_nexus_assist"`
	MFGCabinAssist      bool     `json:"mfg_cabin_assist"`
	GrowthChamberAssist bool     `json:"growth_chamber_assist"`
}

type VisitFriendsMenuScanTargetFriendOpenRecognition struct{}

func (r *VisitFriendsMenuScanTargetFriendOpenRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	var params struct {
		AssistMode string `json:"assist_mode"`
	}
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().
			Err(err).
			Msg("failed to parse CustomRecognitionParam")
		return nil, false
	}
	itemsRoi, ok := getFriendItemsRoi(ctx, arg)
	if !ok {
		log.Error().Msg("Failed to get friend items roi")
		return nil, false
	}

	result := scanTargetResult{
		HasTarget: false,
	}
	for _, roi := range itemsRoi {
		result.Roi = roi
		name, ok := getFriendItemsName(ctx, arg, roi)
		if !ok {
			log.Error().Msg("Failed to get friend item name")
			continue
		}
		exist := isFriendNameExist(name)
		if exist {
			log.Debug().Str("name", name).Msg("friend item already exist, skip")
			continue
		}
		index := registerFriendItem(name)
		clueExchange, ok := getFriendClueExchangeEnable(ctx, arg, roi)
		if !ok {
			log.Error().Msg("Failed to get friend item clue exchange enable")
			continue
		}
		setFriendClueExchange(index, clueExchange)

		if currentAssistCount < maxAssistCount {
			override := map[string]any{
				"VisitFriendsMenuScanDetailOpen": map[string]any{
					"roi": maa.Rect{roi.X() + roi.Width() - 100, roi.Y(), 100, roi.Height()},
				},
				"VisitFriendsMenuScanDetailSave": map[string]any{
					"custom_recognition_param": map[string]any{
						"index": index,
					},
				},
			}
			ctx.RunTask("VisitFriendsMenuScanDetailOpen", override)
		}

		if item, ok := menuKeyMap[index]; ok {
			itemJSON, err := json.Marshal(item)
			if err != nil {
				log.Error().Err(err).Int("index", index).Msg("Failed to marshal friend item for logging")
			} else {
				log.Info().
					Int("index", index).
					RawJSON("item", itemJSON).
					Msg("added friend item")
			}
		}

		item, ok := getFriendItem(index)
		switch params.AssistMode {
		case "only_growth_chamber":
			if item.GrowthChamberAssist && currentAssistCount < maxAssistCount {
				result.GrowthChamberAssist = true
				result.HasTarget = true
			}
		case "without_control_nexus":
			if item.MFGCabinAssist && currentAssistCount < maxAssistCount {
				result.MFGCabinAssist = true
				result.HasTarget = true
			}
			if item.GrowthChamberAssist && currentAssistCount < maxAssistCount {
				result.GrowthChamberAssist = true
				result.HasTarget = true
			}
		default:
			if item.ControlNexusAssist && currentAssistCount < maxAssistCount {
				result.ControlNexusAssist = true
				result.HasTarget = true
			}
			if item.MFGCabinAssist && currentAssistCount < maxAssistCount {
				result.MFGCabinAssist = true
				result.HasTarget = true
			}
			if item.GrowthChamberAssist && currentAssistCount < maxAssistCount {
				result.GrowthChamberAssist = true
				result.HasTarget = true
			}
		}
		if item.ClueExchange && currentClueExchangeCount < maxClueExchangeCount {
			result.ClueExchange = true
			result.HasTarget = true
		}
		if result.HasTarget {
			break
		}
	}

	if !result.HasTarget {
		return nil, false
	}

	detailJSON, err := json.Marshal(result)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal scan target result")
		return nil, false
	}
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

type VisitFriendsMenuScanTargetFriendOpenAction struct{}

func (a *VisitFriendsMenuScanTargetFriendOpenAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	lastScrollItemName = "" // 打开好友后重置，避免上次滚动的好友和这次打开的好友名字一样导致误判滚动结束
	if arg.RecognitionDetail == nil {
		log.Error().Msg("VisitFriendsMenuScanTargetFriendOpenAction: RecognitionDetail is nil")
		return false
	}
	if arg.RecognitionDetail.Results == nil || arg.RecognitionDetail.Results.Best == nil {
		log.Error().Msg("VisitFriendsMenuScanTargetFriendOpenAction: Results or Best is nil")
		return false
	}
	customResult, ok := arg.RecognitionDetail.Results.Best.AsCustom()
	if !ok {
		log.Error().Msg("VisitFriendsMenuScanTargetFriendOpenAction: failed to get custom recognition result")
		return false
	}
	var result scanTargetResult
	if err := json.Unmarshal([]byte(customResult.Detail), &result); err != nil {
		log.Error().Err(err).Msg("VisitFriendsMenuScanTargetFriendOpenAction: failed to parse recognition detail")
		return false
	}
	override := map[string]any{
		"VisitFriendsEnterShip": map[string]any{
			"roi": result.Roi,
		},
		"VisitFriendsMenuClueExchange": map[string]any{
			"enabled": result.ClueExchange,
		},
		"VisitFriendsMenuAssistControlNexus": map[string]any{
			"enabled": result.ControlNexusAssist,
		},
		"VisitFriendsMenuAssistMFGCabin1": map[string]any{
			"enabled": result.MFGCabinAssist,
		},
		"VisitFriendsMenuAssistMFGCabin2": map[string]any{
			"enabled": result.MFGCabinAssist,
		},
		"VisitFriendsMenuAssistGrowthChamberSwipe": map[string]any{
			"enabled": result.GrowthChamberAssist,
		},
	}
	ctx.RunTask("VisitFriendsEnterShip", override)

	return true
}

type VisitFriendsMenuScanDetailSaveRecognition struct{}

func (r *VisitFriendsMenuScanDetailSaveRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	var params struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().
			Err(err).
			Msg("failed to parse CustomRecognitionParam")
		return nil, false
	}
	index := params.Index

	{
		detail, err := ctx.RunRecognition("VisitFriendsRecognitionItemDetailControlNexusAssist", arg.Img)
		if err != nil || detail == nil {
			log.Error().Err(err).Msg("Failed to run recognition VisitFriendsRecognitionItemDetailControlNexusAssist")
			return nil, false
		}
		enabled := detail.Hit
		setFriendControlNexusAssist(index, enabled)
	}
	{
		detail, err := ctx.RunRecognition("VisitFriendsRecognitionItemDetailMFGCabinAssist", arg.Img)
		if err != nil || detail == nil {
			log.Error().Err(err).Msg("Failed to run recognition VisitFriendsRecognitionItemDetailMFGCabinAssist")
			return nil, false
		}
		enabled := detail.Hit
		setFriendMFGCabinAssist(index, enabled)
	}
	{
		detail, err := ctx.RunRecognition("VisitFriendsRecognitionItemDetailGrowthChamberAssist", arg.Img)
		if err != nil || detail == nil {
			log.Error().Err(err).Msg("Failed to run recognition VisitFriendsRecognitionItemDetailGrowthChamberAssist")
			return nil, false
		}
		enabled := detail.Hit
		setFriendGrowthChamberAssist(index, enabled)
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

type VisitFriendsMenuScanScrollFinishRecognition struct{}

func (r *VisitFriendsMenuScanScrollFinishRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	itemsRoi, ok := getFriendItemsRoi(ctx, arg)
	if !ok {
		log.Error().Msg("Failed to get friend items roi")
		return nil, false
	}

	if len(itemsRoi) == 0 {
		log.Error().Msg("No friend items roi found")
		return nil, false
	}

	lastRoi := itemsRoi[len(itemsRoi)-1]
	name, ok := getFriendItemsName(ctx, arg, lastRoi)
	if !ok {
		log.Error().Msg("Failed to get last friend item name")
		return nil, false
	}

	if lastScrollItemName != name {
		lastScrollItemName = name
		return nil, false
	}

	log.Info().Str("name", name).Msg("last friend item name is same as previous, scroll finish")
	detailJSON, _ := json.Marshal(map[string]string{"last_name": name})
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

type VisitFriendsMenuScanScrollFullRecognition struct{}

func (r *VisitFriendsMenuScanScrollFullRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if currentAssistCount < maxAssistCount {
		log.Info().
			Int("currentAssistCount", currentAssistCount).
			Int("maxAssistCount", maxAssistCount).
			Msg("assist count not reach max, scroll not full")
		return nil, false
	}
	if currentClueExchangeCount < maxClueExchangeCount {
		log.Info().
			Int("currentClueExchangeCount", currentClueExchangeCount).
			Int("maxClueExchangeCount", maxClueExchangeCount).
			Msg("clue exchange count not reach max, scroll not full")
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
	_ maa.CustomRecognitionRunner = &VisitFriendsMenuScanDetailSaveRecognition{}
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
