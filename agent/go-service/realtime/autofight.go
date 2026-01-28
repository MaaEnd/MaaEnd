package realtime

import (
	"encoding/json"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

var (
	autoFightCharacterCount   int
	autoFightSkillLastIndex   int
	autoFightEndSkillIndex    int
	autoFightEndSkillLastTime time.Time // EndSkillAction 最后触发时间，用于冷却判断
)

type RealTimeAutoFightEntryRecognition struct{}

func (r *RealTimeAutoFightEntryRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// 先找左下角角色上方选中图标，表示进入操控状态
	{
		detail := ctx.RunRecognitionDirect("TemplateMatch", maa.NodeTemplateMatchParam{
			Threshold: []float64{0.7},
			Template:  []string{"RealTimeTask/AutoFightBar.png"},
			ROI:       maa.NewTargetRect(maa.Rect{0, 580, 360, 60}),
		}, arg.Img)
		if detail == nil || !detail.Hit {
			return nil, false
		}
	}

	{
		// 第一格能量满（黄色 [255, 255, 0] - [255, 220, 0] 交替闪烁）
		detail_yellow := ctx.RunRecognitionDirect("ColorMatch", maa.NodeColorMatchParam{
			ROI:   maa.NewTargetRect(maa.Rect{533, 645, 70, 15}),
			Lower: [][]int{{220, 190, 0}},
			Upper: [][]int{{255, 255, 30}},
			Count: 100,
		}, arg.Img)

		// 第一格能量空（白色 [255, 255, 255]）
		detail_white := ctx.RunRecognitionDirect("ColorMatch", maa.NodeColorMatchParam{
			ROI:   maa.NewTargetRect(maa.Rect{533, 645, 70, 15}),
			Lower: [][]int{{240, 240, 240}},
			Upper: [][]int{{255, 255, 255}},
			Count: 10,
		}, arg.Img)
		if (detail_yellow == nil || !detail_yellow.Hit) && (detail_white == nil || !detail_white.Hit) {
			return nil, false
		}
	}

	var validEnemy int
	{
		// 找敌人血条 [255, 68, 101]
		detail := ctx.RunRecognitionDirect("ColorMatch", maa.NodeColorMatchParam{
			ROI:       maa.NewTargetRect(maa.Rect{280, 150, 750, 370}),
			Lower:     [][]int{{240, 40, 80}},
			Upper:     [][]int{{255, 80, 120}},
			Count:     100,
			Connected: true, // 血条区域必须相邻
		}, arg.Img)
		if detail == nil || !detail.Hit {
			return nil, false
		}

		var colorMatchDetail struct {
			Filtered []struct {
				Box   [4]int `json:"box"`
				Count int    `json:"count"`
			} `json:"filtered"`
		}
		if err := json.Unmarshal([]byte(detail.DetailJson), &colorMatchDetail); err != nil {
			log.Error().Err(err).Msg("Failed to parse ColorMatch detail")
			return nil, false
		}

		for _, item := range colorMatchDetail.Filtered {
			width := item.Box[2]
			height := item.Box[3]
			if width > 10 && height < 20 {
				validEnemy++
			}
		}

		if validEnemy == 0 {
			return nil, false
		}
		log.Debug().Int("validEnemy", validEnemy).Msg("Valid enemy health bar found")
	}

	{
		// 判断有几个角色
		detail := ctx.RunRecognitionDirect("TemplateMatch", maa.NodeTemplateMatchParam{
			ROI:       maa.NewTargetRect(maa.Rect{1010, 615, 265, 20}),
			Template:  []string{"RealTimeTask/AutoFightSkill.png"},
			Threshold: []float64{0.4},
		}, arg.Img)
		if detail == nil || !detail.Hit {
			return nil, false
		}
		var templateMatchDetail struct {
			Filtered []struct {
				Box [4]int `json:"box"` // [x, y, w, h]
			} `json:"filtered"`
		}
		if err := json.Unmarshal([]byte(detail.DetailJson), &templateMatchDetail); err != nil {
			log.Error().Err(err).Msg("Failed to parse TemplateMatch detail")
			return nil, false
		}
		autoFightCharacterCount = len(templateMatchDetail.Filtered)
		log.Debug().Int("characterCount", autoFightCharacterCount).Msg("Character count found")
	}
	log.Debug().Msg("Enter auto fight")
	autoFightSkillLastIndex = 0

	{
		var params struct {
			LockTarget bool `json:"LockTarget"`
		}
		if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
			log.Error().Err(err).Msg("Failed to parse CustomRecognitionParam")
		}
		if params.LockTarget {
			log.Info().Msg("LockTarget enabled, sending middle mouse click to lock target")
			ctx.GetTasker().GetController().PostClickV2(640, 360, 2, 1) // 按下鼠标中键锁定敌人
		}
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

type RealTimeAutoFightExitRecognition struct{}

func (r *RealTimeAutoFightExitRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// EndSkillAction 触发后 3 秒内不触发退出识别，避免大招动画误判
	if time.Since(autoFightEndSkillLastTime) < 3*time.Second {
		return nil, false
	}

	exit := false
	{
		// 找不到角色选中条，退出战斗界面
		detail := ctx.RunRecognitionDirect("TemplateMatch", maa.NodeTemplateMatchParam{
			Threshold: []float64{0.7},
			Template:  []string{"RealTimeTask/AutoFightBar.png"},
			ROI:       maa.NewTargetRect(maa.Rect{0, 580, 360, 60}),
		}, arg.Img)
		if detail == nil || !detail.Hit {
			exit = true
		}
	}

	{
		// 第一格能量满（黄色 [255, 255, 0] - [255, 220, 0] 交替闪烁）
		detail_yellow := ctx.RunRecognitionDirect("ColorMatch", maa.NodeColorMatchParam{
			ROI:   maa.NewTargetRect(maa.Rect{533, 645, 70, 15}),
			Lower: [][]int{{220, 190, 0}},
			Upper: [][]int{{255, 255, 30}},
			Count: 100,
		}, arg.Img)

		// 第一格能量空（白色 [255, 255, 255]）
		detail_white := ctx.RunRecognitionDirect("ColorMatch", maa.NodeColorMatchParam{
			ROI:   maa.NewTargetRect(maa.Rect{533, 645, 70, 15}),
			Lower: [][]int{{240, 240, 240}},
			Upper: [][]int{{255, 255, 255}},
			Count: 10,
		}, arg.Img)
		if (detail_yellow == nil || !detail_yellow.Hit) && (detail_white == nil || !detail_white.Hit) {
			exit = true
		}
	}
	if !exit {
		return nil, false
	}
	log.Debug().Msg("Exit auto fight")
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

type RealTimeAutoFightSkillRecognition struct{}

func (r *RealTimeAutoFightSkillRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {

	// 第二格能量满才会用技能，留一格用于兼容性识别，第一格刚用完恢复可能会误判
	detail := ctx.RunRecognitionDirect("ColorMatch", maa.NodeColorMatchParam{
		ROI:   maa.NewTargetRect(maa.Rect{600, 640, 80, 20}),
		Lower: [][]int{{220, 190, 0}},
		Upper: [][]int{{255, 255, 30}},
		Count: 100,
	}, arg.Img)
	if detail == nil || !detail.Hit {
		return nil, false
	}
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

type RealTimeAutoFightSkillAction struct{}

func (a *RealTimeAutoFightSkillAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	count := autoFightCharacterCount
	if count == 0 || count > 4 {
		return true
	}

	var keycode int
	if count == 1 {
		// 只有 1 个角色时，只按 1
		keycode = 49
	} else {
		// 多个角色时，轮换 2、3、4...（跳过 1）
		// 例如 4 个角色时，轮换 keycode 50, 51, 52（键 '2', '3', '4'）
		keycode = 50 + (autoFightSkillLastIndex % (count - 1))
	}

	ctx.GetTasker().GetController().PostClickKey(int32(keycode))
	log.Info().Int("skillIndex", autoFightSkillLastIndex).Int("keycode", keycode).Msg("AutoFightSkillAction triggered")

	if count > 1 {
		autoFightSkillLastIndex = (autoFightSkillLastIndex + 1) % (count - 1)
	}
	return true
}

type RealTimeAutoFightEndSkillRecognition struct{}

func (r *RealTimeAutoFightEndSkillRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// ROI 定义
	const roiX = 1010
	const roiWidth = 270

	detail := ctx.RunRecognitionDirect("TemplateMatch", maa.NodeTemplateMatchParam{
		Threshold: []float64{0.7},
		Template:  []string{"RealTimeTask/AutoFightEndSkill.png"},
		ROI:       maa.NewTargetRect(maa.Rect{roiX, 535, roiWidth, 65}),
		GreenMask: true,
	}, arg.Img)
	if detail == nil || !detail.Hit {
		return nil, false
	}

	// 解析模板匹配结果
	var templateMatchDetail struct {
		Filtered []struct {
			Box [4]int `json:"box"` // [x, y, w, h]
		} `json:"filtered"`
	}
	if err := json.Unmarshal([]byte(detail.DetailJson), &templateMatchDetail); err != nil {
		log.Error().Err(err).Msg("Failed to parse TemplateMatch detail for EndSkill")
		return nil, false
	}

	if len(templateMatchDetail.Filtered) == 0 {
		return nil, false
	}

	// 取第一个匹配结果
	firstMatch := templateMatchDetail.Filtered[0]
	x := firstMatch.Box[0]

	// 计算相对于 ROI 的位置，确定长按哪个键
	// x 在 0-1/4 范围内：长按 1
	// x 在 1/4-2/4 范围内：长按 2
	// x 在 2/4-3/4 范围内：长按 3
	// x 在 3/4-4/4 范围内：长按 4
	relativeX := x - roiX
	quarterWidth := roiWidth / 4

	var keyIndex int
	switch {
	case relativeX < quarterWidth:
		keyIndex = 1
	case relativeX < quarterWidth*2:
		keyIndex = 2
	case relativeX < quarterWidth*3:
		keyIndex = 3
	default:
		keyIndex = 4
	}
	// 将按键索引传递给 Action
	autoFightEndSkillIndex = keyIndex

	return &maa.CustomRecognitionResult{
		Box:    detail.Box,
		Detail: `{"custom": "fake result"}`,
	}, true
}

type RealTimeAutoFightEndSkillAction struct{}

func (a *RealTimeAutoFightEndSkillAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 记录触发时间，用于 ExitRecognition 冷却判断
	autoFightEndSkillLastTime = time.Now()

	if autoFightEndSkillIndex < 1 || autoFightEndSkillIndex > 4 {
		log.Error().Int("keyIndex", autoFightEndSkillIndex).Msg("Invalid keyIndex")
		return true
	}

	// keycode: 1->49, 2->50, 3->51, 4->52
	keycode := int(48 + autoFightEndSkillIndex)
	ctx.RunActionDirect("LongPressKey", maa.NodeLongPressKeyParam{
		Key:      []int{keycode},
		Duration: 1000, // 长按 1 秒
	}, maa.Rect{0, 0, 0, 0}, arg.RecognitionDetail)

	log.Info().
		Int("keycode", keycode).
		Msg("AutoFightEndSkillAction long press 1s")

	return true
}
