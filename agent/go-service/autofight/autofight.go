package autofight

import (
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func getCharactorShowLevel(ctx *maa.Context, arg *maa.CustomRecognitionArg, index int) bool {
	var roiX int
	switch index {
	case 1:
		roiX = 25
	case 2:
		roiX = 102
	case 3:
		roiX = 180
	case 4:
		roiX = 258
	default:
		log.Warn().Int("index", index).Msg("Invalid combo index")
		return false
	}

	override := map[string]any{
		"AutoFightRecognitionCharactorLevelShow": map[string]any{
			"roi": maa.Rect{roiX, 644, 32, 16},
		},
	}
	detail, err := ctx.RunRecognition("AutoFightRecognitionCharactorLevelShow", arg.Img, override)
	if err != nil {
		log.Error().Err(err).Int("index", index).Msg("Failed to run recognition for charactor level show")
		return false
	}
	return detail != nil && detail.Hit
}

func getComboUsable(ctx *maa.Context, arg *maa.CustomRecognitionArg, index int) bool {
	var roiX int
	switch index {
	case 1:
		roiX = 28
	case 2:
		roiX = 105
	case 3:
		roiX = 184
	case 4:
		roiX = 262
	default:
		log.Warn().Int("index", index).Msg("Invalid combo index")
		return false
	}

	override := map[string]any{
		"AutoFightRecognitionComboUsable": map[string]any{
			"roi": maa.Rect{roiX, 657, 56, 4},
		},
	}
	detail, err := ctx.RunRecognition("AutoFightRecognitionComboUsable", arg.Img, override)
	if err != nil {
		log.Error().Err(err).Int("index", index).Msg("Failed to run recognition for combo usable")
		return false
	}
	return detail != nil && detail.Hit
}

func getComboCooldown(ctx *maa.Context, arg *maa.CustomRecognitionArg, index int) bool {
	var roiX int
	switch index {
	case 1:
		roiX = 28
	case 2:
		roiX = 105
	case 3:
		roiX = 184
	case 4:
		roiX = 262
	default:
		log.Warn().Int("index", index).Msg("Invalid combo index")
		return false
	}

	roiOverride := map[string]any{
		"roi": maa.Rect{roiX, 657, 56, 4},
	}
	override := map[string]any{
		"AutoFightRecognitionComboCooldown1": roiOverride,
		"AutoFightRecognitionComboCooldown2": roiOverride,
	}

	detail, err := ctx.RunRecognition("AutoFightRecognitionComboCooldown1", arg.Img, override)
	if err != nil || detail == nil {
		log.Error().Err(err).Int("index", index).Msg("Failed to run recognition for combo cooldown 1")
		return false
	}
	if detail.Hit {
		return true
	}

	detail, err = ctx.RunRecognition("AutoFightRecognitionComboCooldown2", arg.Img, override)
	if err != nil || detail == nil {
		log.Error().Err(err).Int("index", index).Msg("Failed to run recognition for combo cooldown 2")
		return false
	}
	if detail.Hit {
		return true
	}

	return false
}

func getEnergyLevel(ctx *maa.Context, arg *maa.CustomRecognitionArg) int {
	// 第一格能量满
	detail, err := ctx.RunRecognition("AutoFightRecognitionEnergyLevel1", arg.Img)
	if err != nil {
		log.Error().Err(err).Msg("Failed to run recognition for AutoFightRecognitionEnergyLevel1")
		return -1
	}
	if detail != nil && detail.Hit {
		return 1
	}

	// 第一格能量空
	detail, err = ctx.RunRecognition("AutoFightRecognitionEnergyLevel0", arg.Img)
	if err != nil {
		return -1
	}
	if detail != nil && detail.Hit {
		return 0
	}
	return -1
}

func hasCharacterBar(ctx *maa.Context, arg *maa.CustomRecognitionArg) bool {
	detail, err := ctx.RunRecognition("AutoFightRecognitionCharacterBar", arg.Img)
	if err != nil || detail == nil {
		log.Error().Err(err).Msg("Failed to run recognition for AutoFightRecognitionCharacterBar")
		return false
	}
	return detail.Hit
}

func inFightSpace(ctx *maa.Context, arg *maa.CustomRecognitionArg) bool {
	detail, err := ctx.RunRecognition("AutoFightRecognitionFightSpace", arg.Img)
	if err != nil || detail == nil {
		log.Error().Err(err).Msg("Failed to run recognition for AutoFightRecognitionFightSpace")
		return false
	}
	return detail.Hit
}

func isEntryFightScene(ctx *maa.Context, arg *maa.CustomRecognitionArg) bool {
	// 先找左下角角色上方选中图标，表示进入操控状态
	hasCharacterBar := hasCharacterBar(ctx, arg)

	if !hasCharacterBar {
		return false
	}

	characterLevelShow := false
	if getCharactorShowLevel(ctx, arg, 1) ||
		getCharactorShowLevel(ctx, arg, 2) ||
		getCharactorShowLevel(ctx, arg, 3) ||
		getCharactorShowLevel(ctx, arg, 4) {
		characterLevelShow = true
	}
	if characterLevelShow {
		return false
	}

	return hasCharacterBar && !characterLevelShow

	// 先尝试用简单逻辑判断是否在战斗中，实在没办法再启用以下复杂逻辑
	// // 左上角菜单折叠，进入战斗模式
	// {
	// 	detail, err := ctx.RunRecognition("AutoFightLeftMenuHide", arg.Img)
	// 	if err != nil {
	// 		log.Error().Err(err).Msg("Failed to run recognition for AutoFightLeftMenuHide")
	// 		return false
	// 	}
	// 	if detail != nil && detail.Hit {
	// 		return true
	// 	}
	// }

	// hasEnemy := false
	// {
	// 	// 屏幕中央找敌人
	// 	detail, err := ctx.RunRecognition("AutoFightRecognitionHasEnemy", arg.Img)
	// 	if err != nil {
	// 		log.Error().Err(err).Msg("Failed to run recognition for AutoFightRecognitionHasEnemy")
	// 		return false
	// 	}
	// 	if detail != nil && detail.Hit && detail.Results != nil && len(detail.Results.Filtered) > 0 {
	// 		for _, item := range detail.Results.Filtered {
	// 			result, ok := item.AsColorMatch()
	// 			if !ok {
	// 				continue
	// 			}
	// 			width := result.Box[2]
	// 			height := result.Box[3]
	// 			if width > 10 && height < 20 {
	// 				hasEnemy = true
	// 				break
	// 			}
	// 		}
	// 	}
	// }
	// if !hasEnemy {
	// 	return false
	// }
}

type AutoFightEntryRecognition struct{}

func (r *AutoFightEntryRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if !isEntryFightScene(ctx, arg) {
		return nil, false
	}

	detail, err := ctx.RunRecognition("AutoFightRecognitionFightSkill", arg.Img)
	if err != nil {
		log.Error().Err(err).Msg("Failed to run recognition for AutoFightRecognitionFightSkill")
		return nil, false
	}
	if detail == nil || !detail.Hit || detail.Results == nil || len(detail.Results.Filtered) == 0 {
		return nil, false
	}

	// 4名干员才能自动战斗
	if len(detail.Results.Filtered) != 4 {
		log.Warn().Int("matchCount", len(detail.Results.Filtered)).Msg("Unexpected match count for AutoFightRecognitionFightSkill, expected 4")
		return nil, false
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

var pauseNotInFightSince time.Time

type AutoFightExitRecognition struct{}

func (r *AutoFightExitRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// 暂停超时（不在战斗空间超过 10 秒），直接退出
	if !pauseNotInFightSince.IsZero() && time.Since(pauseNotInFightSince) >= 10*time.Second {
		log.Info().Dur("elapsed", time.Since(pauseNotInFightSince)).Msg("Pause timeout, exiting fight")
		pauseNotInFightSince = time.Time{}
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{"custom": "exit pause timeout"}`,
		}, true
	}

	// 角色没有显示，退出战斗
	hasCharacterBar := hasCharacterBar(ctx, arg)
	if !hasCharacterBar {
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{"custom": "exit no character bar"}`,
		}, true
	}

	// 显示角色等级，退出战斗
	if getCharactorShowLevel(ctx, arg, 1) ||
		getCharactorShowLevel(ctx, arg, 2) ||
		getCharactorShowLevel(ctx, arg, 3) ||
		getCharactorShowLevel(ctx, arg, 4) {
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{"custom": "charactor level show"}`,
		}, true
	}

	return nil, false
}

type AutoFightPauseRecognition struct{}

func (r *AutoFightPauseRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if inFightSpace(ctx, arg) {
		pauseNotInFightSince = time.Time{}
		return nil, false
	}

	if pauseNotInFightSince.IsZero() {
		pauseNotInFightSince = time.Now()
		log.Info().Msg("Not in fight space, start pause timer")
	}

	if time.Since(pauseNotInFightSince) >= 10*time.Second {
		log.Info().Dur("elapsed", time.Since(pauseNotInFightSince)).Msg("Pause timeout, falling through to exit")
		return nil, false
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "pausing, not in fight space"}`,
	}, true
}

type AutoFightExecuteRecognition struct{}

func (r *AutoFightExecuteRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

type AutoFightExecuteAction struct{}

func (a *AutoFightExecuteAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// count := autoFightCharacterCount
	// if count == 0 || count > 4 {
	// 	return true
	// }

	// var keycode int
	// if count == 1 {
	// 	// 只有 1 个角色时，只按 1
	// 	keycode = 49
	// } else {
	// 	// 多个角色时，轮换 2、3、4...（跳过 1）
	// 	// 例如 4 个角色时，轮换 keycode 50, 51, 52（键 '2', '3', '4'）
	// 	keycode = 50 + (autoFightSkillLastIndex % (count - 1))
	// }

	// ctx.GetTasker().GetController().PostClickKey(int32(keycode))
	// log.Info().Int("skillIndex", autoFightSkillLastIndex).Int("keycode", keycode).Msg("AutoFightSkillAction triggered")

	// if count > 1 {
	// 	autoFightSkillLastIndex = (autoFightSkillLastIndex + 1) % (count - 1)
	// }
	return true
}

type AutoFightEndSkillRecognition struct{}

func (r *AutoFightEndSkillRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// // ROI 定义
	// const roiX = 1010
	// const roiWidth = 270

	// detail, err := ctx.RunRecognitionDirect("TemplateMatch", maa.NodeTemplateMatchParam{
	// 	Threshold: []float64{0.7},
	// 	Template:  []string{"RealTimeTask/AutoFightEndSkill.png"},
	// 	ROI:       maa.NewTargetRect(maa.Rect{roiX, 535, roiWidth, 65}),
	// 	GreenMask: true,
	// }, arg.Img)
	// if err != nil {
	// 	log.Error().
	// 		Err(err).
	// 		Msg("Failed to run recognition for TemplateMatch end skill")
	// 	return nil, false
	// }
	// if detail == nil || !detail.Hit {
	// 	return nil, false
	// }

	// // 解析模板匹配结果
	// var templateMatchDetail struct {
	// 	Filtered []struct {
	// 		Box [4]int `json:"box"` // [x, y, w, h]
	// 	} `json:"filtered"`
	// }
	// if err := json.Unmarshal([]byte(detail.DetailJson), &templateMatchDetail); err != nil {
	// 	log.Error().Err(err).Msg("Failed to parse TemplateMatch detail for EndSkill")
	// 	return nil, false
	// }

	// if len(templateMatchDetail.Filtered) == 0 {
	// 	return nil, false
	// }

	// // 取第一个匹配结果
	// firstMatch := templateMatchDetail.Filtered[0]
	// x := firstMatch.Box[0]

	// // 计算相对于 ROI 的位置，确定长按哪个键
	// // x 在 0-1/4 范围内：长按 1
	// // x 在 1/4-2/4 范围内：长按 2
	// // x 在 2/4-3/4 范围内：长按 3
	// // x 在 3/4-4/4 范围内：长按 4
	// relativeX := x - roiX
	// quarterWidth := roiWidth / 4

	// var keyIndex int
	// switch {
	// case relativeX < quarterWidth:
	// 	keyIndex = 1
	// case relativeX < quarterWidth*2:
	// 	keyIndex = 2
	// case relativeX < quarterWidth*3:
	// 	keyIndex = 3
	// default:
	// 	keyIndex = 4
	// }
	// // 将按键索引传递给 Action
	// autoFightEndSkillIndex = keyIndex

	// return &maa.CustomRecognitionResult{
	// 	Box:    detail.Box,
	// 	Detail: `{"custom": "fake result"}`,
	// }, true
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

type AutoFightEndSkillAction struct{}

func (a *AutoFightEndSkillAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// // 记录触发时间，用于 ExitRecognition 冷却判断
	// autoFightEndSkillLastTime = time.Now()

	// if autoFightEndSkillIndex < 1 || autoFightEndSkillIndex > 4 {
	// 	log.Error().Int("keyIndex", autoFightEndSkillIndex).Msg("Invalid keyIndex")
	// 	return true
	// }

	// // keycode: 1->49, 2->50, 3->51, 4->52
	// keycode := int(48 + autoFightEndSkillIndex)
	// ctx.RunActionDirect("LongPressKey", maa.NodeLongPressKeyParam{
	// 	Key:      []int{keycode},
	// 	Duration: 1000, // 长按 1 秒
	// }, maa.Rect{0, 0, 0, 0}, arg.RecognitionDetail)

	// log.Info().
	// 	Int("keycode", keycode).
	// 	Msg("AutoFightEndSkillAction long press 1s")

	return true
}
