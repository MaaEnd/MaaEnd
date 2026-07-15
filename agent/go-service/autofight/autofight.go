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

// autoFightAttach 是 AutoFight 节点 attach 字段的反序列化目标，
// 字段含义参考 assets/resource/pipeline/Interface/AutoFight.json。
type autoFightAttach struct {
	EnableAttack                 bool   `json:"enable_attack"`
	EnableCombo                  bool   `json:"enable_combo"`
	EnableDodge                  bool   `json:"enable_dodge"`
	EnableDodgeCompat            bool   `json:"enable_dodge_compat"`
	EnableHealthDangerousSwitch  bool   `json:"enable_health_dangerous_switch"`
	EnableBreakAccumulatingPower bool   `json:"enable_break_accumulating_power"`
	EnableSkill                  bool   `json:"enable_skill"`
	EnableEndSkill               bool   `json:"enable_end_skill"`
	EnableLockTarget             bool   `json:"enable_lock_target"`
	ReserveSkillLevel            int    `json:"reserve_skill_level"`
	EndAxisTimelineCode          string `json:"end_axis_timeline_code"`
	SkipComboCooldown            bool   `json:"skip_combo_cooldown"`
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

// captureDurationTracker 统计截图识别的平均耗时：每 reportWindow 计算一次窗口内平均值并重置，
// 若平均耗时超过 alertThreshold，则通过 maafocus 提醒用户降低显卡资源占用。
type captureDurationTracker struct {
	reportWindow   time.Duration
	alertThreshold time.Duration
	total          time.Duration
	count          int
	windowStart    time.Time
}

// record 累计一次耗时，并在窗口结束时返回该窗口的平均耗时（毫秒）；
// 仅当窗口刚结束时 ok 为 true，否则 ok 为 false。
func (t *captureDurationTracker) record(d time.Duration) (avgMs int64, ok bool) {
	now := time.Now()
	if t.windowStart.IsZero() {
		t.windowStart = now
	}
	t.total += d
	t.count++

	if now.Sub(t.windowStart) < t.reportWindow {
		return 0, false
	}

	avg := t.total / time.Duration(t.count)
	t.total = 0
	t.count = 0
	t.windowStart = now
	return avg.Milliseconds(), true
}

var capturePerf = &captureDurationTracker{
	reportWindow:   5 * time.Second,
	alertThreshold: 500 * time.Millisecond,
}

// captureAndUpdateScreenDetail 因 DirectHit 耗时 50ms，在 action 里直接截图并更新屏幕分析状态。
func captureAndUpdateScreenDetail(ctx *maa.Context) (image.Image, bool) {
	start := time.Now()
	defer func() {
		if avgMs, ok := capturePerf.record(time.Since(start)); ok &&
			time.Duration(avgMs)*time.Millisecond > capturePerf.alertThreshold {
			maafocus.Print(ctx, i18n.T("autofight.capture_slow", avgMs))
		}
	}()

	ctx.GetTasker().GetController().PostScreencap().Wait()
	img, err := ctx.GetTasker().GetController().CacheImage()
	if err != nil {
		log.Error().Err(err).Str("component", "AutoFight").Msg("failed to cache image")
		return nil, false
	}
	if !screenAnalyzer.UpdateScreenDetail(ctx, img) {
		log.Error().Str("component", "AutoFight").Msg("failed to update screen detail")
		return nil, false
	}
	return img, true
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

type lockStage int

const (
	lockStageLocked  lockStage = -1
	lockStageInitial lockStage = 0
	lockStageRetry   lockStage = 1
	lockStageRecover lockStage = 2
)

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

// lastDodgeAt 记录最近一次入队闪避/移动动作的时间（含移动动作内置的闪避），
// 用于在 EnableLockTarget 中判断一段时间内是否发生过闪避。
var lastDodgeAt time.Time

func enqueueAction(a fightAction) {
	actionQueue = append(actionQueue, a)
	sort.Slice(actionQueue, func(i, j int) bool {
		return actionQueue[i].executeAt.Before(actionQueue[j].executeAt)
	})
	switch a.action {
	case ActionDodge, ActionMoveBack, ActionMoveForward, ActionMoveLeft, ActionMoveRight:
		lastDodgeAt = time.Now()
	}
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

	// 尝试加载 EndAxis 时间轴：成功则在循环里替换 endSkill / skill 决策，失败则回退到原逻辑。
	var timeline *EndAxisTimeline
	if params.EndAxisTimelineCode != "" {
		tl := NewEndAxisTimeline()
		if tl.SetTimelineCode(params.EndAxisTimelineCode) {
			timeline = tl
			log.Info().Str("component", "AutoFight").Msg("endaxis timeline enabled")
			maafocus.Print(ctx, i18n.T("autofight.endaxis.timeline_enabled"))
		} else {
			log.Info().Str("component", "AutoFight").Msg("endaxis timeline code invalid, fallback to default skill logic")
			maafocus.Print(ctx, i18n.T("autofight.endaxis.timeline_invalid_fallback"))
		}
	}

	log.Debug().Str("component", "AutoFight").Interface("params", params).Msg("parsed action attach parameters")
	var pauseStart time.Time
	var lastLevelShowCheck time.Time
	var noLockStart time.Time
	var lockTargetStage lockStage
	lastDodgeAt = time.Now()
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
		img, ok := captureAndUpdateScreenDetail(ctx)
		if !ok {
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

		if params.EnableDodge {
			// 地面攻击必闪；否则按是否启用兼容模式选择普通/兼容闪避检测
			dodgeCheck := screenAnalyzer.GetEnemyDodge
			if params.EnableDodgeCompat {
				dodgeCheck = screenAnalyzer.GetEnemyDodgeCompat
			}
			if screenAnalyzer.GetEnemyAttackGroundDodge() || dodgeCheck() {
				enqueueAction(fightAction{
					executeAt: time.Now().Add(time.Millisecond),
					action:    ActionDodge,
				})
			}
		}

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
				lockTargetStage = lockStageLocked
				firstNoLockIteration = false
				// 5秒内没有闪避/冲刺则向前冲刺一次，防止怪跑远
				if time.Since(lastDodgeAt) >= 5*time.Second {
					maafocus.Print(ctx, i18n.T("autofight.approach_enemy"))
					enqueueAction(fightAction{
						executeAt: time.Now().Add(time.Millisecond),
						action:    ActionMoveForward,
					})
				}
			} else {
				if noLockStart.IsZero() {
					noLockStart = time.Now()
					lockTargetStage = lockStageLocked
				}
				if time.Since(noLockStart) >= 9*time.Second {
					noLockStart = time.Now()
					lockTargetStage = lockStageLocked
				}
				elapsed := time.Since(noLockStart)

				switch {
				case elapsed < 3*time.Second:
					if firstNoLockIteration {
						if lockTargetStage < lockStageInitial {
							maafocus.Print(ctx, i18n.T("autofight.start_combat_lock_target"))
							enqueueAction(fightAction{
								executeAt: time.Now().Add(time.Millisecond),
								action:    ActionLockTarget,
							})
							lockTargetStage = lockStageInitial
						}
					}
				case elapsed < 6*time.Second:
					if lockTargetStage < lockStageRetry {
						maafocus.Print(ctx, i18n.T("autofight.lock_target"))
						enqueueAction(fightAction{
							executeAt: time.Now().Add(time.Millisecond),
							action:    ActionLockTarget,
						})
						lockTargetStage = lockStageRetry
					}
				default:
					if lockTargetStage < lockStageRecover {
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
						lockTargetStage = lockStageRecover
					}
				}
			}
		}

		hasEnemyTarget := false
		if params.EnableLockTarget && screenAnalyzer.GetEnemyLocked() {
			hasEnemyTarget = true
		} else if !params.EnableLockTarget {
			hasEnemyTarget = true
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

		endSkillFull := screenAnalyzer.GetEndSkillFull(true)
		energyLevel := screenAnalyzer.GetEnergyLevel(true)
		if timeline == nil {
			if params.EnableCombo && screenAnalyzer.GetCharacterComboActive() {
				enqueueAction(fightAction{
					executeAt: time.Now(),
					action:    ActionCombo,
				})
			}

			if params.EnableEndSkill && hasEnemyTarget {
				if len(endSkillFull) > 0 {
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
				}
			}
			if params.EnableSkill && energyLevel >= 1 {
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
				} else if energyLevel > params.ReserveSkillLevel && hasEnemyTarget {
					log.Debug().
						Str("component", "AutoFight").
						Int("energyLevel", energyLevel).
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
		} else {
			if hasEnemyTarget && timeline.ActionFinish() {
				maafocus.PrintThrottle(ctx, 3*time.Second, i18n.T("autofight.endaxis.retry_timeline"))
				timeline.SelectScenario(ctx, characterCount, comboFull, endSkillFull, energyLevel, params.SkipComboCooldown)
			}

			if screenAnalyzer.GetCharacterComboActive() && !timeline.ActionFinish() {
				if screenAnalyzer.GetCharacterComboActive() {
					enqueueAction(fightAction{
						executeAt: time.Now(),
						action:    ActionCombo,
					})
				}
			}

			action := timeline.FrontAction()
			if action != nil {
				op := action.TrackIdx + 1
				if op < 1 || op > characterCount {
					// timeline 设计的 track 在当前队伍里没有对应角色，直接丢弃这个动作
					log.Warn().
						Str("component", "AutoFight").
						Str("step", "timelineDecision").
						Int("trackIdx", action.TrackIdx).
						Int("characterCount", characterCount).
						Msg("timeline action targets non-existent character, skip")
					timeline.PopFrontAction()
				} else {
					screenSlot := op + 4 - characterCount

					switch action.Type {
					case "ultimate":
						if slices.Contains(endSkillFull, screenSlot) && hasEnemyTarget {
							enqueueAction(fightAction{
								executeAt: time.Now(),
								action:    endSkillAction(op),
							})
							screenAnalyzer.MarkLabelUsed(LabelEndSkillFull)
							timeline.PopFrontAction()
						}
					case "skill":
						if energyLevel >= 1 && hasEnemyTarget {
							enqueueAction(fightAction{
								executeAt: time.Now(),
								action:    skillAction(op),
							})
							screenAnalyzer.MarkLabelUsed(LabelEnergyLevelFull)
							timeline.PopFrontAction()
						}
					}
				}
			}
		}

		drainActionQueue(ctx)
	}
	if params.EnableAttack {
		ctx.RunAction("__AutoFightActionAttackTouchUp", maa.Rect{600, 320, 80, 80}, "", nil)
	}

	// 确保最后的攻击动作执行完毕，避免还在位移时进入下一个pipeline
	waitStart := time.Now()
	for time.Since(waitStart) < 3*time.Second {
		if ctx.GetTasker().Stopping() {
			break
		}
		_, ok := captureAndUpdateScreenDetail(ctx)
		if !ok {
			break
		}
	}

	if !ctx.GetTasker().Stopping() && screenAnalyzer.GetEnemyLockedReliable() {
		enqueueAction(fightAction{
			executeAt: time.Now().Add(time.Millisecond),
			action:    ActionLockTarget,
		})
		drainActionQueue(ctx)
	}
	return result
}

func drainActionQueue(ctx *maa.Context) {
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
			ctx.RunAction("__AutoFightActionComboClick", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionSkill1:
			maafocus.Print(ctx, i18n.T("autofight.skill", 1))
			ctx.RunAction("__AutoFightActionSkillOperators1", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionSkill2:
			maafocus.Print(ctx, i18n.T("autofight.skill", 2))
			ctx.RunAction("__AutoFightActionSkillOperators2", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionSkill3:
			maafocus.Print(ctx, i18n.T("autofight.skill", 3))
			ctx.RunAction("__AutoFightActionSkillOperators3", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionSkill4:
			maafocus.Print(ctx, i18n.T("autofight.skill", 4))
			ctx.RunAction("__AutoFightActionSkillOperators4", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionEndSkill1:
			maafocus.Print(ctx, i18n.T("autofight.end_skill", 1))
			ctx.RunAction("__AutoFightActionEndSkillOperators1", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionEndSkill2:
			maafocus.Print(ctx, i18n.T("autofight.end_skill", 2))
			ctx.RunAction("__AutoFightActionEndSkillOperators2", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionEndSkill3:
			maafocus.Print(ctx, i18n.T("autofight.end_skill", 3))
			ctx.RunAction("__AutoFightActionEndSkillOperators3", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionEndSkill4:
			maafocus.Print(ctx, i18n.T("autofight.end_skill", 4))
			ctx.RunAction("__AutoFightActionEndSkillOperators4", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionLockTarget:
			ctx.RunAction("__AutoFightActionLockTarget", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionDodge:
			maafocus.Print(ctx, i18n.T("autofight.dodge"))
			ctx.RunAction("__AutoFightActionDodge", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionSleepSecond:
			time.Sleep(1000 * time.Millisecond)
		case ActionSwitchCharacter1:
			maafocus.Print(ctx, i18n.T("autofight.switch_character", 1))
			ctx.RunAction("__AutoFightActionSwitchCharacterOperators1", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionSwitchCharacter2:
			maafocus.Print(ctx, i18n.T("autofight.switch_character", 2))
			ctx.RunAction("__AutoFightActionSwitchCharacterOperators2", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionSwitchCharacter3:
			maafocus.Print(ctx, i18n.T("autofight.switch_character", 3))
			ctx.RunAction("__AutoFightActionSwitchCharacterOperators3", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionSwitchCharacter4:
			maafocus.Print(ctx, i18n.T("autofight.switch_character", 4))
			ctx.RunAction("__AutoFightActionSwitchCharacterOperators4", maa.Rect{600, 320, 80, 80}, "", nil)
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
