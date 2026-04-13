package autofight

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var screenAnalyzer = NewScreenAnalyzer()

type AutoFightEntryRecognition struct{}

func (r *AutoFightEntryRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		return nil, false
	}

	if !screenAnalyzer.UpdateScreenDetail(ctx, arg.Img) {
		return nil, false
	}

	if screenAnalyzer.GetEnergyLevel(false) < 0 {
		return nil, false
	}

	comboFull := screenAnalyzer.GetCharacterComboFull()
	if len(comboFull) == 0 {
		return nil, false
	}

	if screenAnalyzer.GetCharacterLevel() {
		return nil, false
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

// saveExitImage 将当前画面保存到 debug/autofight_exit 目录，用于排查退出时的画面。
func saveExitImage(img image.Image, reason string) {
	if img == nil {
		return
	}
	dir := filepath.Join("debug", "autofight_exit")
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Debug().Err(err).Str("component", "AutoFight").Str("dir", dir).Msg("failed to create debug dir for exit image")
		return
	}
	name := fmt.Sprintf("%s_%s.png", reason, time.Now().Format("20060102_150405"))
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		log.Debug().Err(err).Str("component", "AutoFight").Str("path", path).Msg("failed to create file for exit image")
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Debug().Err(err).Str("component", "AutoFight").Str("path", path).Msg("failed to encode exit image")
		return
	}
	log.Info().Str("component", "AutoFight").Str("path", path).Str("reason", reason).Msg("saved exit frame to disk")
}

type ActionType int

const (
	ActionAttack ActionType = iota
	ActionCombo
	ActionSkill
	ActionEndSkill
	ActionLockTarget
	ActionDodge
	ActionSleep
	ActionSwitchCharacter
)

func (t ActionType) String() string {
	switch t {
	case ActionAttack:
		return "Attack"
	case ActionCombo:
		return "Combo"
	case ActionSkill:
		return "Skill"
	case ActionEndSkill:
		return "EndSkill"
	case ActionLockTarget:
		return "LockTarget"
	case ActionDodge:
		return "Dodge"
	case ActionSwitchCharacter:
		return "SwitchCharacter"
	default:
		return "Unknown"
	}
}

type fightAction struct {
	executeAt time.Time
	action    ActionType
	operator  int
}

var actionQueue []fightAction

func enqueueAction(a fightAction) {
	actionQueue = append(actionQueue, a)
	sort.Slice(actionQueue, func(i, j int) bool {
		return actionQueue[i].executeAt.Before(actionQueue[j].executeAt)
	})
	// log.Debug().
	// 	Str("action", a.action.String()).
	// 	Int("operator", a.operator).
	// 	Str("executeAt", a.executeAt.Format("15:04:05.000")).
	// 	Int("queueLen", len(actionQueue)).
	// 	Msg("AutoFight enqueue action")
}

func dequeueAction() (fightAction, bool) {
	if len(actionQueue) == 0 {
		return fightAction{}, false
	}

	a := actionQueue[0]
	actionQueue = actionQueue[1:]
	// log.Debug().
	// 	Str("action", a.action.String()).
	// 	Int("operator", a.operator).
	// 	Str("executeAt", a.executeAt.Format("15:04:05.000")).
	// 	Int("queueLen", len(actionQueue)).
	// 	Msg("AutoFight dequeue action")
	return a, true
}

// Compile-time interface checks
var (
	_ maa.CustomRecognitionRunner = &AutoFightEntryRecognition{}
	_ maa.CustomActionRunner      = &AutoFightMainAction{}
)

type AutoFightMainAction struct{}

func (a *AutoFightMainAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	raw, err := ctx.GetNodeJSON(arg.CurrentTaskName)
	if err != nil || raw == "" {
		log.Error().Err(err).Str("component", "AutoFight").Str("step", "get node json").Msg("get node json for custom action param")
		return false
	}

	var nodeWithAttach struct {
		Attach struct {
			EnableAttack                 bool `json:"enable_attack"`
			EnableCombo                  bool `json:"enable_combo"`
			EnableDodge                  bool `json:"enable_dodge"`
			EnableHealthDangerousSwitch  bool `json:"enable_health_dangerous_switch"`
			EnableBreakAccumulatingPower bool `json:"enable_break_accumulating_power"`
			EnableSkill                  bool `json:"enable_skill"`
			EnableEndSkill               bool `json:"enable_end_skill"`
			EnableLockTarget             bool `json:"enable_lock_target"`
			ReserveSkillLevel            int  `json:"reserve_skill_level"`
		} `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &nodeWithAttach); err != nil {
		log.Error().Err(err).Str("component", "AutoFight").Str("step", "parse node attach").Msg("parse node attach for auto fight action")
		return false
	}
	params := nodeWithAttach.Attach
	log.Debug().Str("component", "AutoFight").Str("step", "parse params").Interface("params", params).Msg("parsed action attach parameters")
	var pauseStart time.Time
	characterCount := -1
	skillCycleIndex := 1

	if params.EnableAttack {
		ctx.RunAction("__AutoFightActionAttackTouchDown", maa.Rect{600, 320, 80, 80}, "", nil)
	}

	result := false
	for {
		if ctx.GetTasker().Stopping() {
			log.Info().Str("component", "AutoFight").Msg("task stopping signal received, exiting fight")
			maafocus.Print(ctx, i18n.T("autofight.exit_fight"))
			result = true
			break
		}

		// 因DirectHit耗时50ms，因此在action里直接截图
		ctx.GetTasker().GetController().PostScreencap().Wait()
		img, err := ctx.GetTasker().GetController().CacheImage()
		if err != nil {
			log.Error().Err(err).Str("component", "AutoFight").Msg("failed to cache image")
			result = false
			break
		}

		if !screenAnalyzer.UpdateScreenDetail(ctx, img) {
			log.Error().Str("component", "AutoFight").Msg("failed to update screen detail")
			result = false
			break
		}

		// 暂停判定：检查是否在战斗空间内
		inFightSpace := (screenAnalyzer.GetMenuList() || screenAnalyzer.GetMenuOperators())

		if inFightSpace {
			pauseStart = time.Time{}
		} else {
			if pauseStart.IsZero() {
				pauseStart = time.Now()
				log.Info().Str("component", "AutoFight").Msg("not in fight space, start pause timer")
			}
			if time.Since(pauseStart) >= 10*time.Second {
				log.Info().Str("component", "AutoFight").Dur("elapsed", time.Since(pauseStart)).Msg("pause timeout, exiting fight")
				maafocus.Print(ctx, i18n.T("autofight.exit_fight"))
				result = true
				break
			}
			continue
		}

		// 退出判定
		comboFull := screenAnalyzer.GetCharacterComboFull()
		if screenAnalyzer.GetCharacterLevel() {
			log.Info().Str("component", "AutoFight").Msg("character level detected, exiting fight")
			maafocus.Print(ctx, i18n.T("autofight.exit_fight"))
			result = true
			break
		}
		healthNormal := screenAnalyzer.GetCharacterHealthNormal()
		healthDangerous := screenAnalyzer.GetCharacterHealthDangerous()

		// 按第一帧
		if characterCount == -1 {
			characterCount = max(len(healthNormal)+len(healthDangerous), len(comboFull))
			log.Info().
				Str("component", "AutoFight").
				Int("characterCount", characterCount).
				Any("healthNormal", healthNormal).
				Any("comboFull", comboFull).
				Msg("initial character count detected")
			maafocus.Print(ctx, i18n.T("autofight.character_count", characterCount))
		}

		// 战斗决策
		if params.EnableLockTarget &&
			(!screenAnalyzer.GetEnemyTargetCenter() && !screenAnalyzer.GetEnemyBossHealth()) {
			enqueueAction(fightAction{
				executeAt: time.Now().Add(time.Millisecond),
				action:    ActionLockTarget,
			})
			// if params.EnableAttack {
			// 	enqueueAction(fightAction{
			// 		executeAt: time.Now().Add(time.Millisecond),
			// 		action:    ActionAttack,
			// 	})
			// }
		} else {
			if params.EnableHealthDangerousSwitch {
				charSelect := screenAnalyzer.GetCharacterSelect()
				if charSelect > 0 && slices.Contains(healthDangerous, charSelect) && len(healthNormal) > 0 {
					switchTo := healthNormal[0]
					maafocus.Print(ctx, i18n.T("autofight.health_dangerous_switch", charSelect, switchTo))
					enqueueAction(fightAction{
						executeAt: time.Now().Add(time.Millisecond),
						action:    ActionSwitchCharacter,
						operator:  switchTo,
					})
				}
			}
			if params.EnableDodge && screenAnalyzer.GetEnemyDodge() {
				enqueueAction(fightAction{
					executeAt: time.Now().Add(time.Millisecond),
					action:    ActionDodge,
				})
			}
			// } else if params.EnableAttack {
			// 	enqueueAction(fightAction{
			// 		executeAt: time.Now(),
			// 		action:    ActionAttack,
			// 	})
			// }
			if params.EnableCombo && screenAnalyzer.GetCharacterComboActive() {
				enqueueAction(fightAction{
					executeAt: time.Now(),
					action:    ActionCombo,
				})
			} else if endSkillFull := screenAnalyzer.GetEndSkillFull(true); params.EnableEndSkill && len(endSkillFull) > 0 {
				screenAnalyzer.MarkLabelUsed(LabelEndSkillFull)
				for _, idx := range endSkillFull {
					enqueueAction(fightAction{
						executeAt: time.Now(),
						action:    ActionEndSkill,
						operator:  idx,
					})
					break
				}
			} else if params.EnableSkill && screenAnalyzer.GetEnergyLevel(true) >= 1 {
				if params.EnableBreakAccumulatingPower && screenAnalyzer.GetEnemyAccumulatingPower(true) {
					maafocus.Print(ctx, i18n.T("autofight.enemy_accumulating_power"))
					idx := skillCycleIndex
					enqueueAction(fightAction{
						executeAt: time.Now(),
						action:    ActionSkill,
						operator:  idx,
					})
					skillCycleIndex = idx + 1
				} else if screenAnalyzer.GetEnergyLevel(true) > params.ReserveSkillLevel {
					log.Debug().
						Str("component", "AutoFight").
						Int("energyLevel", screenAnalyzer.GetEnergyLevel(true)).
						Int("reserveLevel", params.ReserveSkillLevel).
						Msg("energy level above reserve, using skill")
					idx := skillCycleIndex
					enqueueAction(fightAction{
						executeAt: time.Now(),
						action:    ActionSkill,
						operator:  idx,
					})
					skillCycleIndex = idx + 1
				}
				screenAnalyzer.MarkLabelUsed(LabelEnergyLevelFull)
			}
		}

		// 执行队列中已到期的动作
		now := time.Now()
		for len(actionQueue) > 0 && !actionQueue[0].executeAt.After(now) {
			fa, ok := dequeueAction()
			if !ok {
				break
			}
			switch fa.action {
			case ActionAttack:
				ctx.RunAction("__AutoFightActionAttackClick", maa.Rect{600, 320, 80, 80}, "", nil)
			case ActionCombo:
				ctx.RunAction("__AutoFightActionComboClick", maa.Rect{600, 320, 80, 80}, "", nil)
			case ActionSkill:
				op := fa.operator
				if characterCount > 0 {
					op = ((op - 1) % characterCount) + 1
				}
				ctx.RunAction(fmt.Sprintf("__AutoFightActionSkillOperators%d", op), maa.Rect{600, 320, 80, 80}, "", nil)
			case ActionEndSkill:
				if fa.operator < 5-characterCount {
					break
				}
				op := fa.operator + characterCount - 4
				ctx.RunAction("__AutoFightActionEndSkillAltKeyDown", maa.Rect{600, 320, 80, 80}, "", nil)
				ctx.RunAction(fmt.Sprintf("__AutoFightActionEndSkillOperators%d", op), maa.Rect{600, 320, 80, 80}, "", nil)
				ctx.RunAction("__AutoFightActionEndSkillAltKeyUp", maa.Rect{600, 320, 80, 80}, "", nil)
			case ActionLockTarget:
				ctx.RunAction("__AutoFightActionLockTarget", maa.Rect{600, 320, 80, 80}, "", nil)
			case ActionDodge:
				ctx.RunAction("__AutoFightActionDodge", maa.Rect{600, 320, 80, 80}, "", nil)
			case ActionSwitchCharacter:
				ctx.RunAction(fmt.Sprintf("__AutoFightActionSwitchCharacterOperators%d", fa.operator), maa.Rect{600, 320, 80, 80}, "", nil)
			}
		}
	}
	if params.EnableAttack {
		ctx.RunAction("__AutoFightActionAttackTouchUp", maa.Rect{600, 320, 80, 80}, "", nil)
	}
	return result
}
