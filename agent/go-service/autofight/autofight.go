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
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// autoFightAttach 是 AutoFight 节点 attach 字段的反序列化目标，
// 字段含义参考 assets/resource/pipeline/Interface/AutoFight.json。
type autoFightAttach struct {
	EnableAttack                 bool   `json:"enable_attack"`
	EnableCombo                  bool   `json:"enable_combo"`
	EnableDodge                  bool   `json:"enable_dodge"`
	EnableHealthDangerousSwitch  bool   `json:"enable_health_dangerous_switch"`
	EnableBreakAccumulatingPower bool   `json:"enable_break_accumulating_power"`
	EnableSkill                  bool   `json:"enable_skill"`
	EnableEndSkill               bool   `json:"enable_end_skill"`
	EnableLockTarget             bool   `json:"enable_lock_target"`
	ReserveSkillLevel            int    `json:"reserve_skill_level"`
	ComboKeymap                  string `json:"combo_keymap"`
	SkillKeymap1                 string `json:"skill_keymap1"`
	SkillKeymap2                 string `json:"skill_keymap2"`
	SkillKeymap3                 string `json:"skill_keymap3"`
	SkillKeymap4                 string `json:"skill_keymap4"`
	SwitchOperatorKeymap1        string `json:"switch_operator_keymap1"`
	SwitchOperatorKeymap2        string `json:"switch_operator_keymap2"`
	SwitchOperatorKeymap3        string `json:"switch_operator_keymap3"`
	SwitchOperatorKeymap4        string `json:"switch_operator_keymap4"`
}

// keymapOverrides 是预先生成好的 pipeline override JSON，
// 直接传给 ctx.RunAction，仅覆盖对应节点的 key 字段。
type keymapOverrides struct {
	combo           string
	skill           [4]string
	endSkill        [4]string
	switchCharacter [4]string
}

// keyOverride 解析 attach 中的按键字符串，失败或为空时回退到 fallback，
// 然后生成形如 {"<entry>":{"key":<code>}} 的 pipeline override JSON。
func keyOverride(entry, raw, fallback string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		s = fallback
	}
	code, err := VirtualKeyCode(s)
	if err != nil {
		log.Warn().Err(err).
			Str("component", "AutoFight").
			Str("entry", entry).
			Str("value", raw).
			Str("fallback", fallback).
			Msg("invalid keymap, fallback to default")
		code, _ = VirtualKeyCode(fallback)
	}
	return fmt.Sprintf(`{%q:{"key":%d}}`, entry, code)
}

var screenAnalyzer = NewScreenAnalyzer()

func getCharactorLevelShow(ctx *maa.Context, img image.Image) bool {
	detail, err := ctx.RunRecognition("__AutoFightRecognitionCharactorLevelShow", img)
	if err != nil || detail == nil {
		log.Error().
			Err(err).
			Str("component", "AutoFight").
			Str("step", "getCharactorLevelShow").
			Str("recognition", "__AutoFightRecognitionCharactorLevelShow").
			Msg("failed to run recognition for character level show")
		return false
	}
	return detail.Hit
}

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
	ActionSkill1
	ActionSkill2
	ActionSkill3
	ActionSkill4
	ActionEndSkill1
	ActionEndSkill2
	ActionEndSkill3
	ActionEndSkill4
	ActionLockTarget
	ActionDodge
	ActionSleepSecond
	ActionSwitchCharacter1
	ActionSwitchCharacter2
	ActionSwitchCharacter3
	ActionSwitchCharacter4
	ActionMoveBack
	ActionMoveForward
	ActionMoveLeft
	ActionMoveRight
)

func skillAction(idx int) ActionType {
	return ActionSkill1 + ActionType(idx-1)
}

func endSkillAction(idx int) ActionType {
	return ActionEndSkill1 + ActionType(idx-1)
}

func switchCharacterAction(idx int) ActionType {
	return ActionSwitchCharacter1 + ActionType(idx-1)
}

type fightAction struct {
	executeAt time.Time
	action    ActionType
}

var actionQueue []fightAction

func enqueueAction(a fightAction) {
	actionQueue = append(actionQueue, a)
	sort.Slice(actionQueue, func(i, j int) bool {
		return actionQueue[i].executeAt.Before(actionQueue[j].executeAt)
	})
}

func dequeueAction() (fightAction, bool) {
	if len(actionQueue) == 0 {
		return fightAction{}, false
	}

	a := actionQueue[0]
	actionQueue = actionQueue[1:]
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
		Attach autoFightAttach `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &nodeWithAttach); err != nil {
		log.Error().Err(err).Str("component", "AutoFight").Str("step", "parse node attach").Msg("parse node attach for auto fight action")
		return false
	}
	params := nodeWithAttach.Attach
	overrides := keymapOverrides{
		combo: keyOverride("__AutoFightActionComboClick", params.ComboKeymap, "E"),
		skill: [4]string{
			keyOverride("__AutoFightActionSkillOperators1", params.SkillKeymap1, "1"),
			keyOverride("__AutoFightActionSkillOperators2", params.SkillKeymap2, "2"),
			keyOverride("__AutoFightActionSkillOperators3", params.SkillKeymap3, "3"),
			keyOverride("__AutoFightActionSkillOperators4", params.SkillKeymap4, "4"),
		},
		endSkill: [4]string{
			keyOverride("__AutoFightActionEndSkillOperators1", params.SkillKeymap1, "1"),
			keyOverride("__AutoFightActionEndSkillOperators2", params.SkillKeymap2, "2"),
			keyOverride("__AutoFightActionEndSkillOperators3", params.SkillKeymap3, "3"),
			keyOverride("__AutoFightActionEndSkillOperators4", params.SkillKeymap4, "4"),
		},
		switchCharacter: [4]string{
			keyOverride("__AutoFightActionSwitchCharacterOperators1", params.SwitchOperatorKeymap1, "F1"),
			keyOverride("__AutoFightActionSwitchCharacterOperators2", params.SwitchOperatorKeymap2, "F2"),
			keyOverride("__AutoFightActionSwitchCharacterOperators3", params.SwitchOperatorKeymap3, "F3"),
			keyOverride("__AutoFightActionSwitchCharacterOperators4", params.SwitchOperatorKeymap4, "F4"),
		},
	}
	log.Debug().Str("component", "AutoFight").Interface("params", params).Interface("overrides", overrides).Msg("parsed action attach parameters and built keymap overrides")
	var pauseStart time.Time
	var lastLevelShowCheck time.Time
	var noLockStart time.Time
	var lockTargetStage int
	firstNoLockIteration := true
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
		charSelect := screenAnalyzer.GetCharacterSelect()
		inFightSpace := charSelect > 0
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
		// comboEmpty := screenAnalyzer.GetCharacterComboEmpty()
		if screenAnalyzer.GetCharacterLevel() &&
			!screenAnalyzer.GetEnemyTarget() &&
			!screenAnalyzer.GetEnemyFacingLeft() &&
			!screenAnalyzer.GetEnemyFacingRight() &&
			!screenAnalyzer.GetEnemyFacingBack() &&
			len(comboFull) == 0 {
			log.Info().Str("component", "AutoFight").Msg("exiting fight")
			maafocus.Print(ctx, i18n.T("autofight.exit_fight"))
			// saveExitImage(img, "character_level")
			result = true
			break
		}

		if time.Since(lastLevelShowCheck) >= 5*time.Second {
			lastLevelShowCheck = time.Now()
			if getCharactorLevelShow(ctx, img) {
				log.Info().Str("component", "AutoFight").Msg("character level show detected, exiting fight")
				maafocus.Print(ctx, i18n.T("autofight.exit_fight"))
				// saveExitImage(img, "character_level_show")
				result = true
				break
			}
		}
		// CharacterLevel小概率识别不到，comboEmpty大概率不显示了依然命中，双重保险
		// if len(comboFull) == 0 && len(comboEmpty) == 0 {
		// 	log.Info().Str("component", "AutoFight").Msg("no combo detected, exiting fight")
		// 	maafocus.Prin
		// t(ctx, i18n.T("autofight.exit_fight"))
		// 	result = true
		// 	break
		// }
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
		if params.EnableLockTarget {
			// 锁定目标时序状态机（按距上次检测到 EnemyLocked 的累计时长划分）：
			//   首次未锁定的那一帧               -> 直接 continue，过滤瞬时识别抖动
			//   阶段 0 [0, 3s)    -> 宽限期，不特殊处理，正常进入战斗决策
			//   阶段 1 [3s, 6s)   -> 进入时发一次 ActionLockTarget
			//   阶段 2 [6s, 9s)   -> 进入时根据 EnemyFacing 方向升级动作：
			//                        左/右/后 -> 对应方向移动 + Sleep + Sleep + ActionLockTarget
			//                        无 facing -> 前进 + ActionLockTarget
			//   阶段 3 [9s, ∞)    -> 重置 noLockStart 重新进入阶段 0（含首帧 continue），循环重试
			//   任意时刻检测到 EnemyLocked     -> 把 noLockStart 推回当前时刻、回到阶段 0，并重置首帧标记
			if screenAnalyzer.GetEnemyLocked() {
				noLockStart = time.Now()
				lockTargetStage = -1
				firstNoLockIteration = false
			} else {
				if noLockStart.IsZero() {
					noLockStart = time.Now()
					lockTargetStage = -1
				}
				if time.Since(noLockStart) >= 9*time.Second {
					noLockStart = time.Now()
					lockTargetStage = -1
				}
				elapsed := time.Since(noLockStart)

				switch {
				case elapsed < 3*time.Second:
					if firstNoLockIteration {
						if lockTargetStage < 0 {
							maafocus.Print(ctx, i18n.T("autofight.start_combat_lock_target"))
							enqueueAction(fightAction{
								executeAt: time.Now().Add(time.Millisecond),
								action:    ActionLockTarget,
							})
							lockTargetStage = 0
						}
						drainActionQueue(ctx, overrides)
						continue
					}
				case elapsed < 6*time.Second:
					if lockTargetStage < 1 {
						maafocus.Print(ctx, i18n.T("autofight.lock_target"))
						enqueueAction(fightAction{
							executeAt: time.Now().Add(time.Millisecond),
							action:    ActionLockTarget,
						})
						lockTargetStage = 1
					}
					drainActionQueue(ctx, overrides)
					continue
				default:
					if lockTargetStage < 2 {
						facingBack := screenAnalyzer.GetEnemyFacingBack()
						facingLeft := screenAnalyzer.GetEnemyFacingLeft()
						facingRight := screenAnalyzer.GetEnemyFacingRight()
						switch {
						case facingBack:
							maafocus.Print(ctx, i18n.T("autofight.move_back"))
							enqueueAction(fightAction{
								executeAt: time.Now().Add(time.Millisecond),
								action:    ActionMoveBack,
							})
						case facingLeft:
							maafocus.Print(ctx, i18n.T("autofight.move_left"))
							enqueueAction(fightAction{
								executeAt: time.Now().Add(time.Millisecond),
								action:    ActionMoveLeft,
							})
						case facingRight:
							maafocus.Print(ctx, i18n.T("autofight.move_right"))
							enqueueAction(fightAction{
								executeAt: time.Now().Add(time.Millisecond),
								action:    ActionMoveRight,
							})
						default:
							maafocus.Print(ctx, i18n.T("autofight.move_forward"))
							enqueueAction(fightAction{
								executeAt: time.Now().Add(time.Millisecond),
								action:    ActionMoveForward,
							})
						}
						if facingBack || facingLeft || facingRight {
							enqueueAction(fightAction{
								executeAt: time.Now().Add(time.Millisecond),
								action:    ActionSleepSecond,
							})
							enqueueAction(fightAction{
								executeAt: time.Now().Add(time.Millisecond),
								action:    ActionSleepSecond,
							})
						}
						enqueueAction(fightAction{
							executeAt: time.Now().Add(time.Millisecond),
							action:    ActionLockTarget,
						})
						lockTargetStage = 2
					}
					drainActionQueue(ctx, overrides)
					continue
				}
			}
		}
		if params.EnableHealthDangerousSwitch {
			if charSelect > 0 && slices.Contains(healthDangerous, charSelect) && len(healthNormal) > 0 {
				switchTo := healthNormal[0]
				maafocus.Print(ctx, i18n.T("autofight.health_dangerous_switch", charSelect, switchTo))
				enqueueAction(fightAction{
					executeAt: time.Now().Add(time.Millisecond),
					action:    switchCharacterAction(switchTo),
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
				if idx >= 5-characterCount {
					op := idx + characterCount - 4
					enqueueAction(fightAction{
						executeAt: time.Now(),
						action:    endSkillAction(op),
					})
				}
				break
			}
		} else if params.EnableSkill && screenAnalyzer.GetEnergyLevel(true) >= 1 {
			if params.EnableBreakAccumulatingPower && screenAnalyzer.GetEnemyAccumulatingPower(true) {
				maafocus.Print(ctx, i18n.T("autofight.enemy_accumulating_power"))
				op := skillCycleIndex
				if characterCount > 0 {
					op = ((op - 1) % characterCount) + 1
				}
				enqueueAction(fightAction{
					executeAt: time.Now(),
					action:    skillAction(op),
				})
				skillCycleIndex++
			} else if screenAnalyzer.GetEnergyLevel(true) > params.ReserveSkillLevel {
				log.Debug().
					Str("component", "AutoFight").
					Int("energyLevel", screenAnalyzer.GetEnergyLevel(true)).
					Int("reserveLevel", params.ReserveSkillLevel).
					Msg("energy level above reserve, using skill")
				op := skillCycleIndex
				if characterCount > 0 {
					op = ((op - 1) % characterCount) + 1
				}
				enqueueAction(fightAction{
					executeAt: time.Now(),
					action:    skillAction(op),
				})
				skillCycleIndex++
			}
			screenAnalyzer.MarkLabelUsed(LabelEnergyLevelFull)
		}

		drainActionQueue(ctx, overrides)
	}
	if params.EnableAttack {
		ctx.RunAction("__AutoFightActionAttackTouchUp", maa.Rect{600, 320, 80, 80}, "", nil)
	}
	if !ctx.GetTasker().Stopping() {
		// 确保最后的攻击动作执行完毕，避免还在位移时进入下一个pipeline
		time.Sleep(3 * time.Second)
	}
	return result
}

func drainActionQueue(ctx *maa.Context, overrides keymapOverrides) {
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
			maafocus.Print(ctx, i18n.T("autofight.combo"))
			ctx.RunAction("__AutoFightActionComboClick", maa.Rect{600, 320, 80, 80}, "", overrides.combo)
		case ActionSkill1:
			maafocus.Print(ctx, i18n.T("autofight.skill", 1))
			ctx.RunAction("__AutoFightActionSkillOperators1", maa.Rect{600, 320, 80, 80}, "", overrides.skill[0])
		case ActionSkill2:
			maafocus.Print(ctx, i18n.T("autofight.skill", 2))
			ctx.RunAction("__AutoFightActionSkillOperators2", maa.Rect{600, 320, 80, 80}, "", overrides.skill[1])
		case ActionSkill3:
			maafocus.Print(ctx, i18n.T("autofight.skill", 3))
			ctx.RunAction("__AutoFightActionSkillOperators3", maa.Rect{600, 320, 80, 80}, "", overrides.skill[2])
		case ActionSkill4:
			maafocus.Print(ctx, i18n.T("autofight.skill", 4))
			ctx.RunAction("__AutoFightActionSkillOperators4", maa.Rect{600, 320, 80, 80}, "", overrides.skill[3])
		case ActionEndSkill1:
			maafocus.Print(ctx, i18n.T("autofight.end_skill", 1))
			ctx.RunAction("__AutoFightActionEndSkillOperators1", maa.Rect{600, 320, 80, 80}, "", overrides.endSkill[0])
		case ActionEndSkill2:
			maafocus.Print(ctx, i18n.T("autofight.end_skill", 2))
			ctx.RunAction("__AutoFightActionEndSkillOperators2", maa.Rect{600, 320, 80, 80}, "", overrides.endSkill[1])
		case ActionEndSkill3:
			maafocus.Print(ctx, i18n.T("autofight.end_skill", 3))
			ctx.RunAction("__AutoFightActionEndSkillOperators3", maa.Rect{600, 320, 80, 80}, "", overrides.endSkill[2])
		case ActionEndSkill4:
			maafocus.Print(ctx, i18n.T("autofight.end_skill", 4))
			ctx.RunAction("__AutoFightActionEndSkillOperators4", maa.Rect{600, 320, 80, 80}, "", overrides.endSkill[3])
		case ActionLockTarget:
			ctx.RunAction("__AutoFightActionLockTarget", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionDodge:
			maafocus.Print(ctx, i18n.T("autofight.dodge"))
			ctx.RunAction("__AutoFightActionDodge", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionSleepSecond:
			time.Sleep(1000 * time.Millisecond)
		case ActionSwitchCharacter1:
			maafocus.Print(ctx, i18n.T("autofight.switch_character", 1))
			ctx.RunAction("__AutoFightActionSwitchCharacterOperators1", maa.Rect{600, 320, 80, 80}, "", overrides.switchCharacter[0])
		case ActionSwitchCharacter2:
			maafocus.Print(ctx, i18n.T("autofight.switch_character", 2))
			ctx.RunAction("__AutoFightActionSwitchCharacterOperators2", maa.Rect{600, 320, 80, 80}, "", overrides.switchCharacter[1])
		case ActionSwitchCharacter3:
			maafocus.Print(ctx, i18n.T("autofight.switch_character", 3))
			ctx.RunAction("__AutoFightActionSwitchCharacterOperators3", maa.Rect{600, 320, 80, 80}, "", overrides.switchCharacter[2])
		case ActionSwitchCharacter4:
			maafocus.Print(ctx, i18n.T("autofight.switch_character", 4))
			ctx.RunAction("__AutoFightActionSwitchCharacterOperators4", maa.Rect{600, 320, 80, 80}, "", overrides.switchCharacter[3])
		case ActionMoveBack:
			ctx.RunAction("__AutoFightActionMoveBackKeyDown", maa.Rect{600, 320, 80, 80}, "", nil)
			ctx.RunAction("__AutoFightActionDodge", maa.Rect{600, 320, 80, 80}, "", nil)
			ctx.RunAction("__AutoFightActionMoveBackKeyUp", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionMoveForward:
			ctx.RunAction("__AutoFightActionMoveForwardKeyDown", maa.Rect{600, 320, 80, 80}, "", nil)
			ctx.RunAction("__AutoFightActionDodge", maa.Rect{600, 320, 80, 80}, "", nil)
			ctx.RunAction("__AutoFightActionMoveForwardKeyUp", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionMoveLeft:
			ctx.RunAction("__AutoFightActionMoveLeftKeyDown", maa.Rect{600, 320, 80, 80}, "", nil)
			ctx.RunAction("__AutoFightActionDodge", maa.Rect{600, 320, 80, 80}, "", nil)
			ctx.RunAction("__AutoFightActionMoveLeftKeyUp", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionMoveRight:
			ctx.RunAction("__AutoFightActionMoveRightKeyDown", maa.Rect{600, 320, 80, 80}, "", nil)
			ctx.RunAction("__AutoFightActionDodge", maa.Rect{600, 320, 80, 80}, "", nil)
			ctx.RunAction("__AutoFightActionMoveRightKeyUp", maa.Rect{600, 320, 80, 80}, "", nil)
		}
	}
}
