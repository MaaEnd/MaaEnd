package essencefilter

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type EssenceFilterAfterBattleSkillDecisionAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &EssenceFilterAfterBattleSkillDecisionAction{}

func (a *EssenceFilterAfterBattleSkillDecisionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 获取当前运行状态，如果状态为空则无法继续，直接返回
	st := getRunState()
	if st == nil {
		return false
	}

	// 将识别到的三个技能名称和等级存入ocr结构中给api调用
	ocr := matchapi.OCRInput{
		Skills: [3]string{st.CurrentSkills[0], st.CurrentSkills[1], st.CurrentSkills[2]},             // 这三条不要求严格按 slot1/slot2/slot3 顺序；引擎会基于 pool 自动重排（若能唯一推断）
		Levels: [3]int{st.CurrentSkillLevels[0], st.CurrentSkillLevels[1], st.CurrentSkillLevels[2]}, // 对应等级（1..6）
	}

	// 从 Context 中获取用户配置的选项（如是否开启未来可期等），若获取不到则使用默认配置

	attachs, _ := getOptionsFromAttach(ctx, "EssenceFilterAfterBattleInit")
	if attachs == nil {
		def := defaultEssenceFilterOptions()
		attachs = &def
	}

	opts := matchapi.EssenceFilterOptions{
		// exact 精确匹配只在你选择了稀有度时才启用
		Rarity6Weapon: attachs.Rarity6Weapon,

		KeepFuturePromising:      attachs.KeepFuturePromising,
		KeepSlot3Level3Practical: attachs.KeepSlot3Level3Practical,

		DiscardUnmatched: attachs.DiscardUnmatched,
	}

	//调用通用api接口识别
	engine, err := matchapi.NewDefaultEngine()
	if err != nil {
		// 例如无法定位 assets/data/EssenceFilter
		panic(err)
	}

	res, err := engine.MatchOCR(ocr, opts)
	if err != nil {
		panic(err)
	}

	report := res.Reason

	if res.ShouldLock {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterAfterBattleLockItemLog"}})
	} else if res.ShouldDiscard {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterAfterBattleDiscardItemLog"}})
	} else {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterAfterBattleCloseDetail"}})
	}

	maafocus.NodeActionStarting(ctx, report)

	// 清空当前识别缓存，准备处理下一个掉落物
	st.CurrentSkills = [3]string{}
	st.CurrentSkillLevels = [3]int{}
	return true
}
