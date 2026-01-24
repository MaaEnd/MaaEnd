package realtime

import (
	"encoding/json"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

type RealTimeAutoFightEndSkillAction struct{}

func (a *RealTimeAutoFightEndSkillAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// log.Info().Msg("Entry RealTimeAutoFightAction")

	// for true {
	// 	controller := ctx.GetTasker().GetController()
	// 	img, ok := a.captureScreen(controller)
	// 	if !ok {
	// 		break
	// 	}

	// 	energyLevel := a.getEnergyLevel(ctx, img)
	// 	if energyLevel == -1 {
	// 		log.Info().Msg("energyLevel unknown, exit")
	// 		break
	// 	}
	// }

	// log.Info().Msg("Leave RealTimeAutoFightAction")
	return true
}

// 技能切换状态
var (
	skillIndex    int       // 当前技能索引 (0-based)
	skillLastTime time.Time // 上次触发时间
)

type RealTimeAutoFightSkillAction struct{}

func (a *RealTimeAutoFightSkillAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	detailJson := arg.RecognitionDetail.DetailJson

	// And 识别返回的是数组，需要找到 AutoFightSkill 的结果
	var andResults []struct {
		Algorithm string `json:"algorithm"`
		Name      string `json:"name"`
		Detail    struct {
			All []struct {
				Box [4]int `json:"box"` // [x, y, w, h]
			} `json:"all"`
			Filtered []struct {
				Box [4]int `json:"box"`
			} `json:"filtered"`
			Best *struct {
				Box [4]int `json:"box"`
			} `json:"best"`
		} `json:"detail"`
	}

	if err := json.Unmarshal([]byte(detailJson), &andResults); err != nil {
		log.Error().Err(err).Msg("Failed to parse And recognition detail")
		return true
	}

	var filteredCount int
	for _, result := range andResults {
		if result.Name == "AutoFightSkill" {
			filteredCount = len(result.Detail.Filtered)
			break
		}
	}

	count := filteredCount
	if count == 0 || count > 4 {
		return true
	}

	// 检查时间间隔，不足1秒则跳过
	if time.Since(skillLastTime) < time.Second {
		return true
	}

	// 每次使用不同角色放技能1、2、3、4交替
	keycode := 49 + (skillIndex % count)

	ctx.GetTasker().GetController().PostClickKey(int32(keycode))
	log.Info().Int("skillIndex", skillIndex).Int("keycode", keycode).Msg("AutoFightSkillAction triggered")

	skillIndex = (skillIndex + 1) % count
	skillLastTime = time.Now()
	return true
}
