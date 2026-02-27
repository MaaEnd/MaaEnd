package autofight

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func getCharactorLevelShow(ctx *maa.Context, arg *maa.CustomRecognitionArg) bool {
	detail, err := ctx.RunRecognition("__AutoFightRecognitionCharactorLevelShow", arg.Img)
	if err != nil || detail == nil {
		log.Error().Err(err).Msg("Failed to run recognition for combo notice")
		return false
	}
	return detail.Hit
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
		"__AutoFightRecognitionComboUsable": map[string]any{
			"roi": maa.Rect{roiX, 657, 56, 4},
		},
	}
	detail, err := ctx.RunRecognition("__AutoFightRecognitionComboUsable", arg.Img, override)
	if err != nil {
		log.Error().Err(err).Int("index", index).Msg("Failed to run recognition for combo usable")
		return false
	}
	return detail != nil && detail.Hit
}

func getEndSkillUsable(ctx *maa.Context, arg *maa.CustomRecognitionArg) []int {
	usableIndexes := []int{}
	rois := []maa.Rect{
		{1027, 535, 47, 65},
		{1091, 535, 47, 65},
		{1155, 535, 47, 65},
		{1219, 535, 47, 65},
	}

	for i, roi := range rois {
		override := map[string]any{
			"__AutoFightRecognitionEndSkill": map[string]any{
				"roi": roi,
			},
		}
		detail, err := ctx.RunRecognition("__AutoFightRecognitionEndSkill", arg.Img, override)
		if err != nil {
			log.Error().Err(err).Int("operator", i+1).Msg("Failed to run recognition for end skill")
			continue
		}
		if detail != nil && detail.Hit {
			usableIndexes = append(usableIndexes, i+1)
		}
	}
	return usableIndexes
}

func hasComboShow(ctx *maa.Context, arg *maa.CustomRecognitionArg) bool {
	detail, err := ctx.RunRecognition("__AutoFightRecognitionComboNotice", arg.Img)
	if err != nil || detail == nil {
		log.Error().Err(err).Msg("Failed to run recognition for combo notice")
		return false
	}
	return detail.Hit
}

func hasEnemyAttack(ctx *maa.Context, arg *maa.CustomRecognitionArg) bool {
	detail, err := ctx.RunRecognition("__AutoFightRecognitionEnemyAttack", arg.Img)
	if err != nil || detail == nil {
		log.Error().Err(err).Msg("Failed to run recognition for enemy attack")
		return false
	}
	return detail.Hit
}

func hasEnemyInScreen(ctx *maa.Context, arg *maa.CustomRecognitionArg) bool {
	detail, err := ctx.RunRecognition("__AutoFightRecognitionEnemyInScreen", arg.Img)
	if err != nil || detail == nil {
		log.Error().Err(err).Msg("Failed to run recognition for enemy in screen")
		return false
	}
	return detail.Hit
}

func getEnergyLevel(ctx *maa.Context, arg *maa.CustomRecognitionArg) int {
	// 从高到低检测：3格 → 2格 → 1格 → 0格
	detail, err := ctx.RunRecognition("__AutoFightRecognitionEnergyLevel3", arg.Img)
	if err == nil && detail != nil && detail.Hit {
		return 3
	}

	detail, err = ctx.RunRecognition("__AutoFightRecognitionEnergyLevel2", arg.Img)
	if err == nil && detail != nil && detail.Hit {
		return 2
	}

	detail, err = ctx.RunRecognition("__AutoFightRecognitionEnergyLevel1", arg.Img)
	if err != nil {
		log.Error().Err(err).Msg("Failed to run recognition for __AutoFightRecognitionEnergyLevel1")
		return -1
	}
	if detail != nil && detail.Hit {
		return 1
	}

	// 第一格能量空
	detail, err = ctx.RunRecognition("__AutoFightRecognitionEnergyLevel0", arg.Img)
	if err != nil {
		return -1
	}
	if detail != nil && detail.Hit {
		return 0
	}
	return -1
}

func hasCharacterBar(ctx *maa.Context, arg *maa.CustomRecognitionArg) bool {
	detail, err := ctx.RunRecognition("__AutoFightRecognitionSwitchOperatorsTip", arg.Img)
	if err != nil || detail == nil {
		log.Error().Err(err).Msg("Failed to run recognition for AutoFightRecognitionSwitchOperatorsTip")
		return false
	}
	return detail.Hit
}

func inFightSpace(ctx *maa.Context, arg *maa.CustomRecognitionArg) bool {
	detail, err := ctx.RunRecognition("__AutoFightRecognitionFightSpace", arg.Img)
	if err != nil || detail == nil {
		log.Error().Err(err).Msg("Failed to run recognition for AutoFightRecognitionFightSpace")
		return false
	}
	return detail.Hit
}

func isEntryFightScene(ctx *maa.Context, arg *maa.CustomRecognitionArg) bool {
	// 先找左下角角色上方选中图标，表示进入操控状态
	// hasCharacterBar := hasCharacterBar(ctx, arg)

	// if !hasCharacterBar {
	// 	return false
	// }
	energyLevel := getEnergyLevel(ctx, arg)
	if energyLevel < 0 {
		return false
	}

	characterLevelShow := getCharactorLevelShow(ctx, arg)
	if characterLevelShow {
		return false
	}

	return true
}

type AutoFightEntryRecognition struct{}

func (r *AutoFightEntryRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		return nil, false
	}
	if !isEntryFightScene(ctx, arg) {
		return nil, false
	}

	detail, err := ctx.RunRecognition("__AutoFightRecognitionFightSkill", arg.Img)
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

	// 尝试加载出招配置数据码（如果有）
	dataCode := ""
	if arg.CustomRecognitionParam != "" {
		dataCode = strings.TrimSpace(strings.Trim(strings.TrimSpace(arg.CustomRecognitionParam), `"`))
	}

	// 判定是否需要彻底重置调度器
	// 为了防止视觉抖动造成的短暂 "退出战斗"，结合已存在的退战计时器来判断
	shouldReset := false
	if !pauseNotInFightSince.IsZero() && time.Since(pauseNotInFightSince) > 10*time.Second {
		shouldReset = true
	} else if !HasConfig() {
		// 如果还没加载过配置，也是全新战斗
		shouldReset = true
	}

	if dataCode != "" {
		if err := LoadConfig(dataCode); err != nil {
			log.Warn().Err(err).Msg("Failed to load AutoFight data code from custom params, falling back to default strategy")
		} else {
			if cfg := GetConfig(); cfg != nil {
				log.Info().Str("scenario", cfg.ScenarioName).Msg("Loaded AutoFight config from data code")
				if shouldReset {
					ResetScheduler()
					actionQueue = nil
					skillCycleIndex = 1
					log.Info().Msg("[AutoFight] Timeout or new battle, completely reset scheduler state")
				}
			}
		}
	} else {
		if shouldReset {
			ResetScheduler()
			ClearConfig()
			actionQueue = nil
			skillCycleIndex = 1
			log.Info().Msg("[AutoFight] Timeout or new battle without config, completely clear scheduler and config")
		}
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

var pauseNotInFightSince time.Time

// saveExitImage 将当前画面保存到 debug/autofight_exit 目录，用于排查退出时的画面。
func saveExitImage(img image.Image, reason string) {
	if img == nil {
		return
	}
	dir := filepath.Join("debug", "autofight_exit")
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Debug().Err(err).Str("dir", dir).Msg("Failed to create debug dir for exit image")
		return
	}
	name := fmt.Sprintf("%s_%s.png", reason, time.Now().Format("20060102_150405"))
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		log.Debug().Err(err).Str("path", path).Msg("Failed to create file for exit image")
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Debug().Err(err).Str("path", path).Msg("Failed to encode exit image")
		return
	}
	log.Info().Str("path", path).Str("reason", reason).Msg("Saved exit frame to disk")
}

type AutoFightExitRecognition struct{}

func (r *AutoFightExitRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		return nil, false
	}
	// 暂停超时（不在战斗空间超过 10 秒），直接退出
	if !pauseNotInFightSince.IsZero() && time.Since(pauseNotInFightSince) >= 10*time.Second {
		log.Info().Dur("elapsed", time.Since(pauseNotInFightSince)).Msg("Pause timeout, exiting fight")
		pauseNotInFightSince = time.Time{}
		enemyInScreen = false // 下次进入 entry 后首次 Execute 再执行 LockTarget
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{"custom": "exit pause timeout"}`,
		}, true
	}

	// 显示角色等级，退出战斗
	// 只要在战斗，一定会显示左下角干员条
	if getCharactorLevelShow(ctx, arg) {
		// saveExitImage(arg.Img, "character_level_show")
		enemyInScreen = false // 下次进入 entry 后首次 Execute 再执行 LockTarget
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{"custom": "charactor level show"}`,
		}, true
	}

	return nil, false
}

type AutoFightPauseRecognition struct{}

func (r *AutoFightPauseRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		return nil, false
	}
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

type ActionType int

const (
	ActionAttack ActionType = iota
	ActionCombo
	ActionSkill
	ActionEndSkillKeyDown
	ActionEndSkillKeyUp
	ActionLockTarget
	ActionDodge
	ActionSleep
	ActionSwitchOperator
)

func (t ActionType) String() string {
	switch t {
	case ActionAttack:
		return "Attack"
	case ActionCombo:
		return "Combo"
	case ActionSkill:
		return "Skill"
	case ActionEndSkillKeyDown:
		return "EndSkillKeyDown"
	case ActionEndSkillKeyUp:
		return "EndSkillKeyUp"
	case ActionLockTarget:
		return "LockTarget"
	case ActionDodge:
		return "Dodge"
	case ActionSwitchOperator:
		return "SwitchOperator"
	default:
		return "Unknown"
	}
}

type fightAction struct {
	executeAt time.Time
	action    ActionType
	operator  int
}

var (
	actionQueue     []fightAction
	skillCycleIndex = 1
	enemyInScreen   = false // 检查敌人是是否首次出现在屏幕
)

func enqueueAction(a fightAction) {
	actionQueue = append(actionQueue, a)
	sort.Slice(actionQueue, func(i, j int) bool {
		return actionQueue[i].executeAt.Before(actionQueue[j].executeAt)
	})
	log.Debug().
		Str("action", a.action.String()).
		Int("operator", a.operator).
		Str("executeAt", a.executeAt.Format("15:04:05.000")).
		Int("queueLen", len(actionQueue)).
		Msg("AutoFight enqueue action")
}

func dequeueAction() (fightAction, bool) {
	if len(actionQueue) == 0 {
		return fightAction{}, false
	}

	a := actionQueue[0]
	actionQueue = actionQueue[1:]
	log.Debug().
		Str("action", a.action.String()).
		Int("operator", a.operator).
		Str("executeAt", a.executeAt.Format("15:04:05.000")).
		Int("queueLen", len(actionQueue)).
		Msg("AutoFight dequeue action")
	return a, true
}

// 识别干员技能释放
func recognitionSkill(ctx *maa.Context, arg *maa.CustomRecognitionArg) {
	if hasComboShow(ctx, arg) {
		// 连携技能
		enqueueAction(fightAction{
			executeAt: time.Now(),
			action:    ActionCombo,
		})
	} else if endSkillUsable := getEndSkillUsable(ctx, arg); len(endSkillUsable) > 0 {
		// 终结技可用
		for _, idx := range endSkillUsable {
			enqueueAction(fightAction{
				executeAt: time.Now(),
				action:    ActionEndSkillKeyDown,
				operator:  idx,
			})
			enqueueAction(fightAction{
				executeAt: time.Now().Add(1500 * time.Millisecond),
				action:    ActionEndSkillKeyUp,
				operator:  idx,
			})
			break
		}
	} else if getEnergyLevel(ctx, arg) >= 1 {
		idx := skillCycleIndex
		enqueueAction(fightAction{
			executeAt: time.Now(),
			action:    ActionSkill,
			operator:  idx,
		})
		if idx >= 4 {
			skillCycleIndex = 1
		} else {
			skillCycleIndex = idx + 1
		}
	}
}

func recognitionAttack(ctx *maa.Context, arg *maa.CustomRecognitionArg) {
	// 识别闪避、普攻
	if hasEnemyAttack(ctx, arg) {
		enqueueAction(fightAction{
			executeAt: time.Now().Add(100 * time.Millisecond),
			action:    ActionDodge,
		})
	} else {
		enqueueAction(fightAction{
			executeAt: time.Now(),
			action:    ActionAttack,
		})
	}
}

type AutoFightExecuteRecognition struct{}

func (r *AutoFightExecuteRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		return nil, false
	}

	// 如果有出招配置，使用调度器模式
	if HasConfig() {
		return r.runSchedulerMode(ctx, arg)
	}

	// 否则走原有默认逻辑
	if !enemyInScreen && hasEnemyInScreen(ctx, arg) {
		enemyInScreen = true
		enqueueAction(fightAction{
			executeAt: time.Now().Add(time.Millisecond),
			action:    ActionLockTarget,
		})
	}

	if enemyInScreen {
		recognitionSkill(ctx, arg)
		recognitionAttack(ctx, arg)
	} else {
		recognitionAttack(ctx, arg)
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

// runSchedulerMode 使用时间轴调度器的战斗模式
func (r *AutoFightExecuteRecognition) runSchedulerMode(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	cfg := GetConfig()
	if cfg == nil {
		return nil, false
	}

	sch := GetScheduler(cfg)

	// 识别当前状态
	energyLevel := getEnergyLevel(ctx, arg)
	if energyLevel < 0 {
		energyLevel = 0
	}
	comboAvailable := hasComboShow(ctx, arg)
	endSkillUsable := getEndSkillUsable(ctx, arg)

	// 调度器 tick
	result := sch.Tick(energyLevel, comboAvailable, endSkillUsable)

	// 将调度器的动作建议转换为 actionQueue
	for _, a := range result.Actions {
		enqueueAction(fightAction{
			executeAt: time.Now(),
			action:    a.Type,
			operator:  a.Operator,
		})
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "scheduler mode"}`,
	}, true
}

// actionName 根据动作类型和干员下标返回 Pipeline 中的 action 名称
func actionName(action ActionType, operator int) string {
	switch action {
	case ActionAttack:
		return "__AutoFightActionAttack"
	case ActionCombo:
		return "__AutoFightActionComboClick"
	case ActionSkill:
		return fmt.Sprintf("__AutoFightActionSkillOperators%d", operator)
	case ActionEndSkillKeyDown:
		return fmt.Sprintf("__AutoFightActionEndSkillOperators%dKeyDown", operator)
	case ActionEndSkillKeyUp:
		return fmt.Sprintf("__AutoFightActionEndSkillOperators%dKeyUp", operator)
	case ActionLockTarget:
		return "__AutoFightActionLockTarget"
	case ActionDodge:
		return "__AutoFightActionDodge"
	case ActionSwitchOperator:
		return fmt.Sprintf("__AutoFightActionSwitchOperator%d", operator)
	default:
		return ""
	}
}

type AutoFightExecuteAction struct{}

func (a *AutoFightExecuteAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	now := time.Now()

	// 取出已到期的队列动作并依次执行（按 executeAt 顺序）
	for len(actionQueue) > 0 && !actionQueue[0].executeAt.After(now) {
		fa, ok := dequeueAction()
		if !ok {
			break
		}
		name := actionName(fa.action, fa.operator)
		if name == "" {
			continue
		}

		ctx.RunTask(name)
	}

	return true
}
