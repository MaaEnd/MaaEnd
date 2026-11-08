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

	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/charactercontroller"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// autoFightAttach 是 AutoFight 节点 attach 字段的反序列化目标，
// 字段含义参考 assets/resource/pipeline/Interface/AutoFight.json。
type autoFightAttach struct {
	EnableAttack                 bool     `json:"enable_attack"`
	EnableCombo                  bool     `json:"enable_combo"`
	EnableDodge                  bool     `json:"enable_dodge"`
	EnableDodgeCompat            bool     `json:"enable_dodge_compat"`
	EnableHealthDangerousSwitch  bool     `json:"enable_health_dangerous_switch"`
	EnableBreakAccumulatingPower bool     `json:"enable_break_accumulating_power"`
	EnableSkill                  bool     `json:"enable_skill"`
	EnableEndSkill               bool     `json:"enable_end_skill"`
	EnableLockTarget             bool     `json:"enable_lock_target"`
	ReserveSkillLevel            int      `json:"reserve_skill_level"`
	EndAxisTimelineCode          string   `json:"end_axis_timeline_code"`
	SkipComboCooldown            bool     `json:"skip_combo_cooldown"`
	PauseExitNodes               []string `json:"pause_exit_nodes"`
	SkillMode1                   string   `json:"skill_mode_1"`
	SkillMode2                   string   `json:"skill_mode_2"`
	SkillMode3                   string   `json:"skill_mode_3"`
	SkillMode4                   string   `json:"skill_mode_4"`
}

// 每名干员的战技释放方式；空值按 cycle 处理。
const (
	skillModeCycle = "cycle"
	skillModeOnce  = "once"
	skillModeNever = "never"
)

func (p *autoFightAttach) skillModes() [4]string {
	return [4]string{p.SkillMode1, p.SkillMode2, p.SkillMode3, p.SkillMode4}
}

// pickSkillOperator 从 cycleIndex 起轮转查找下一个可释放战技的干员（1-based），
// 跳过 never 以及已释放过的 once；返回选中的干员与下一轮的起点，找不到时 ok 为 false。
func pickSkillOperator(modes [4]string, usedOnce *[4]bool, cycleIndex, characterCount int) (op, next int, ok bool) {
	n := characterCount
	if n <= 0 || n > 4 {
		n = 4
	}
	for i := range n {
		candidate := ((cycleIndex-1+i)%n + n) % n
		switch modes[candidate] {
		case skillModeNever:
			continue
		case skillModeOnce:
			if usedOnce[candidate] {
				continue
			}
			usedOnce[candidate] = true
		}
		return candidate + 1, candidate + 2, true
	}
	return 0, cycleIndex, false
}

var screenAnalyzer = NewScreenAnalyzer()

func getCharactorLevelShow(ctx *maa.Context, img image.Image) bool {
	var override map[string]any
	if !screenAnalyzer.mobileLayout {
		box, ok := screenAnalyzer.GetCharacterSelectBox()
		if !ok {
			return false
		}
		override = map[string]any{
			"__AutoFightRecognitionCharactorLevelShow": map[string]any{
				"roi":        []int{box[0], box[1], box[2], box[3]},
				"roi_offset": []int{-25, box[3] + 35, 20, 4},
			},
		}
	}
	detail, err := ctx.RunRecognition("__AutoFightRecognitionCharactorLevelShow", img, override)
	if err != nil || detail == nil {
		log.Error().Err(err).Str("component", "AutoFight").Msg("failed to run recognition for character level show")
		return false
	}
	return detail.Hit
}

func absIntLocal(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// hitExitNode 在 img 上依次运行 nodes 中各节点的识别，返回首个命中的节点名，未命中返回空字符串。
func hitExitNode(ctx *maa.Context, img image.Image, nodes []string) string {
	if len(nodes) == 0 {
		return ""
	}
	for _, node := range nodes {
		detail, err := ctx.RunRecognition(node, img)
		if err != nil {
			log.Error().Err(err).Str("component", "AutoFight").Str("recognition", node).Msg("failed to run exit node recognition")
			continue
		}
		if detail == nil || !detail.Hit {
			continue
		}
		return node
	}
	return ""
}

func screencapImage(ctx *maa.Context) (image.Image, bool) {
	ctx.GetTasker().GetController().PostScreencap().Wait()
	img, err := ctx.GetTasker().GetController().CacheImage()
	if err != nil {
		log.Error().Err(err).Str("component", "AutoFight").Msg("failed to cache image")
		return nil, false
	}
	return img, true
}

// 竞技大会活动，触摸光球时游戏判断开始战斗，接着321倒计时，之后出现怪物。在321倒计时时按下锁定怪物按键会导致普攻松开，因此等待倒计时结束。
func waitProtocolSpaceRestartIfNeeded(ctx *maa.Context) {
	img, ok := screencapImage(ctx)
	if !ok {
		return
	}

	gate, err := ctx.RunRecognition("__AutoFightRecognitionProtocolSpaceMechanicsGate", img)
	if err != nil || gate == nil {
		log.Error().
			Err(err).
			Str("component", "AutoFight").
			Str("step", "protocolSpaceMechanicsGate").
			Str("recognition", "__AutoFightRecognitionProtocolSpaceMechanicsGate").
			Msg("failed to run protocol space mechanics gate recognition")
		return
	}
	if !gate.Hit {
		return
	}

	log.Info().Str("component", "AutoFight").Msg("protocol space mechanics gate hit, waiting for SpaceResart")
	maafocus.Print(ctx, i18n.T("autofight.wait_battle_start"))
	deadline := time.Now().Add(5 * time.Second)
	for {
		restart, err := ctx.RunRecognition("__AutoFightRecognitionProtocolSpaceRestart", img)
		if err != nil || restart == nil {
			log.Error().
				Err(err).
				Str("component", "AutoFight").
				Str("step", "protocolSpaceRestart").
				Str("recognition", "__AutoFightRecognitionProtocolSpaceRestart").
				Msg("failed to run protocol space restart recognition")
			return
		}
		if restart.Hit {
			log.Info().Str("component", "AutoFight").Msg("protocol space SpaceResart found, continue fight")
			maafocus.Print(ctx, i18n.T("autofight.wait_battle_start_found"))
			return
		}

		if time.Now().After(deadline) {
			log.Warn().Str("component", "AutoFight").Msg("protocol space SpaceResart timeout, continue fight")
			maafocus.Print(ctx, i18n.T("autofight.wait_battle_start_timeout"))
			return
		}

		time.Sleep(1 * time.Second)
		img, ok = screencapImage(ctx)
		if !ok {
			return
		}
	}
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

	// 回调入口刷新一次控制器布局，下游的识别区域与 label 都按它取。
	screenAnalyzer.RefreshLayout(ctx)

	if !screenAnalyzer.UpdateScreenDetail(ctx, arg.Img) {
		return nil, false
	}

	// 间隔很久才识别一次时（如寻路到怪旁边后识别一次），历史里的旧非战斗画面会在多帧投票里
	// 压过当前的战斗画面。最新一帧已有能量条却没判进战斗时，补截至多 2 帧，让投票按最新的画面来。
	var ctrl *maa.Controller
	for extra := 0; ; extra++ {
		if screenAnalyzer.GetEnergyLevel(false) >= 0 &&
			len(screenAnalyzer.GetCharacterComboFull()) > 0 &&
			!screenAnalyzer.GetCharacterLevel() {
			return &maa.CustomRecognitionResult{
				Box:    arg.Roi,
				Detail: `{"custom": "fake result"}`,
			}, true
		}
		if extra == 2 ||
			!screenAnalyzer.hasLabelInFrames(LabelEnergyLevelFull, 1, false) &&
				!screenAnalyzer.hasLabelInFrames(LabelEnergyLevelEmpty, 1, false) {
			return nil, false
		}
		if ctrl == nil {
			ctrl = ctx.GetTasker().GetController()
		}
		ctrl.PostScreencap().Wait()
		img, err := ctrl.CacheImage()
		if err != nil || img == nil || !screenAnalyzer.UpdateScreenDetail(ctx, img) {
			return nil, false
		}
	}
}

// saveExitImage 将当前画面保存到 debug/autofight_exit 目录，用于排查退出时的画面。
func saveExitImage(img image.Image, reason string) {
	if img == nil {
		return
	}
	dir := fsutil.UserDataPath("debug", "autofight_exit")
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
	lockStageRecover lockStage = 2
)

// 丢失锁定后的找敌时序（从最后一次识别到 EnemyLocked 开始计时），整个周期 lockCycleDuration 后重来。
// 锁定键在已锁定时会取消锁定，重试间隔必须覆盖游戏响应与识别延迟，避免把刚锁上的目标按掉。
const (
	lockLostConfirmDuration = 1 * time.Second // 持续未锁定超过该时长才视为真正丢失；EnemyLocked 实测漏检空窗最长约 0.55s
	lockRetryInterval       = 1 * time.Second // 两次锁定尝试的最小间隔
	lockMoveAfter           = 3 * time.Second // 仍未锁定则按 EnemyFacing 移动找敌
	lockCycleDuration       = 5 * time.Second
)

// attackReassertInterval 是 ADB 普攻的补按周期。ADB 的普攻靠整场按住的触点流维持，
// 模拟器层面的中断（例如拖动模拟器窗口）会把它掐断且不会自行恢复；周期补按把
// "整场失去普攻"压缩成最多丢失这个时长。PC 端按住的是键鼠状态，不参与补按。
const attackReassertInterval = 5 * time.Second

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
	// ActionReassertAttack 先抬起再按下普攻触点，用于补回被外部掐断的按住状态。
	// 必须留在末尾：上面的战技 / 终结技 / 切人分组靠 skillAction 等函数做下标加法。
	ActionReassertAttack
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
	// 跨轮不残留：每场战斗重置识别帧缓冲与动作队列（上一场残留帧会污染
	// 前 5 帧投票，残留动作会开场误触发）。
	screenAnalyzer = NewScreenAnalyzer()
	actionQueue = nil
	// 回调入口刷新一次控制器布局，下游的识别区域与 label 都按它取。
	screenAnalyzer.RefreshLayout(ctx)

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
	waitProtocolSpaceRestartIfNeeded(ctx)

	var pauseStart time.Time
	var lastLevelShowCheck time.Time
	var noLockStart time.Time
	var lastLockAttempt time.Time
	var lastAttackReassert time.Time
	var lockTargetStage lockStage
	lastDodgeAt = time.Now()
	lastAttackReassert = time.Now()
	firstNoLockIteration := true
	characterCount := -1
	skillCycleIndex := 1
	skillModes := params.skillModes()
	var skillUsedOnce [4]bool
	// 终结技按操作者的释放验证窗：窗内不重施（防连点），窗后仍就绪则重试（防漏放）
	var endSkillCastAt [4]time.Time
	const endSkillRecastWindow = 3 * time.Second
	// 持续对准敌人的上次转向时刻（300ms 冷却，防止每帧狂转）
	var lastEnemyAlignAt time.Time
	// 防站桩看门狗状态：每秒采样一次检测特征，连续 5 次不变判定停滞
	stallFingerprint := ""
	stallCount := 0
	lastStallSample := time.Now()
	lastAntiStall := time.Now()

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
		// 波次刷新后干员指示会间歇漏检（识别率约半数，被 GetCharacterSelect 的
		// 5 帧可靠性否决拦下），仅凭当前帧会让暂停分支反复进出：锁敌、战技、
		// 终结技全被跳过，表现为站桩，直到暂停超时或等级徽记误判提前退出。
		// 用 2 秒时长窗兜底：最近 2 秒内见过干员指示即视为仍在战斗空间；
		// 终结技动画等真遮挡在窗口耗尽后照旧进入暂停，保留动画期保护。
		charSelect := screenAnalyzer.GetCharacterSelect()
		inFightSpace := charSelect > 0 ||
			screenAnalyzer.hasLabelInDuration(LabelCharacterSelect, 2*time.Second)

		// 敌人存在旁路：boss 战/过场动画期间 HUD 可能被特效遮挡，
		// CharacterSelect 会整段漏检。只要画面上仍有敌人目标、锁定态、
		// 闪避预警或 Boss 血条，就强制视为仍在战斗空间，绝不进暂停，
		// 避免「boss 在眼前、角色持续挨揍却不闪避/不普攻」的站桩死锁。
		// 用短时长窗而非单帧，防止单帧误检把暂停态硬拖成战斗态。
		if !inFightSpace {
			inFightSpace = screenAnalyzer.hasLabelInDuration(LabelEnemyTarget, 2*time.Second) ||
				screenAnalyzer.hasLabelInDuration(LabelEnemyLocked, 2*time.Second) ||
				screenAnalyzer.hasLabelInDuration(LabelEnemyDodge, 2*time.Second) ||
				screenAnalyzer.hasLabelInDuration(LabelEnemyBossHealth, 2*time.Second)
		}
		if inFightSpace {
			pauseStart = time.Time{}
		} else {
			// 暂停期间（终结技动画等）看不到锁定状态，不计入丢失锁定的确认时长
			noLockStart = time.Now()
			lockTargetStage = lockStageLocked
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
			// 退出判定：暂停态下 attach 配置的 pause_exit_nodes 任一命中（如选剑演武战斗胜利界面）立即退出
			if exitNode := hitExitNode(ctx, img, params.PauseExitNodes); exitNode != "" {
				log.Info().Str("component", "AutoFight").Str("exitNode", exitNode).Msg("exit node recognized, exiting fight")
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

		// 防站桩看门狗：战斗状态下每秒采样一次检测特征，连续 5 次无变化判定
		// 状态机停滞（锁定重试/决策循环失去推进力）。保存现场、重置锁定状态
		// 机并强制闪避打破僵局；15 秒冷却防止闪避连发。
		if time.Since(lastStallSample) >= time.Second {
			lastStallSample = time.Now()
			fp := screenAnalyzer.DetectionFingerprint()
			if fp == stallFingerprint {
				stallCount++
			} else {
				stallFingerprint = fp
				stallCount = 0
			}
			if stallCount >= 5 && time.Since(lastAntiStall) >= 15*time.Second {
				log.Info().
					Str("component", "AutoFight").
					Int("stallSeconds", stallCount).
					Msg("combat stalled, resetting lock state and dodging")
				saveExitImage(img, "anti_stall")
				maafocus.Print(ctx, i18n.T("autofight.anti_stall"))
				noLockStart = time.Now()
				lockTargetStage = lockStageLocked
				firstNoLockIteration = true
				lastDodgeAt = time.Time{}
				enqueueAction(fightAction{
					executeAt: time.Now().Add(time.Millisecond),
					action:    ActionDodge,
				})
				lastAntiStall = time.Now()
				stallCount = 0
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

		// 持续对准敌人：YOLO 的 EnemyDodge 预警图标只有在敌人位于画面正方向时
		// 才稳定出现在可识别区域。怪物从侧面/背后攻击时预警框会跑到视野边缘
		// 被漏检，表现为「不掉血提示就挨揍」。这里用 EnemyTarget 检测框的 X 偏移
		// 微调视角，把敌人保持在屏幕中央带（GetEnemyTargetCenter 的 600px 竖区）。
		// 敌人存在即可对准：boss 刚刷新在画面边缘时游戏尚未锁定，
		// 若坚持要求 GetEnemyLocked()，这一整段不会执行，角色就会保持
		// 歪视角挨揍、也不去锁。放宽为「已锁定 或 画面里有敌人目标/Boss 血条」。
		// 用短时长窗判定，避免单帧误检导致无意义狂转。
		enemyVisible := screenAnalyzer.GetEnemyLocked() ||
			screenAnalyzer.GetEnemyTarget() ||
			screenAnalyzer.GetEnemyBossHealth()
		if params.EnableLockTarget && enemyVisible && time.Since(lastEnemyAlignAt) >= 300*time.Millisecond {
			if !screenAnalyzer.GetEnemyTargetCenter() {
				if box, ok := screenAnalyzer.LatestEnemyTargetBox(); ok {
					offset := box.X() + box.Width()/2 - 640
					if absIntLocal(offset) > 40 {
						charactercontroller.RotateView(ctx, offset/3, 0)
						lastEnemyAlignAt = time.Now()
						log.Debug().
							Int("offsetX", offset).
							Int("dx", offset/3).
							Msg("aligning view to keep enemy centered")
					}
				}
			}
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
			//   开局第一帧未锁定                         -> 立即发一次 ActionLockTarget
			//   [0, lockLostConfirmDuration)             -> 等待确认，过滤单帧识别抖动
			//   [lockLostConfirmDuration, lockMoveAfter) -> 每隔 lockRetryInterval 发一次 ActionLockTarget
			//   [lockMoveAfter, lockCycleDuration)       -> 进入时根据 EnemyFacing 方向移动一次：
			//                                               左/右/后 -> 对应方向移动 + Sleep + Sleep（阻塞队列）+ ActionLockTarget
			//                                               无 facing -> 前进 + ActionLockTarget
			//   [lockCycleDuration, ∞)                   -> 重置 noLockStart，从头循环重试
			//   任意时刻检测到 EnemyLocked               -> 把 noLockStart 推回当前时刻
			//   当前帧 HUD 不可见（终结技等动画）        -> 无法判断锁定，把 noLockStart 推回当前时刻并重置阶段，不发锁定
			if !screenAnalyzer.hasLabelInFrames(LabelCharacterSelect, 1, false) {
				noLockStart = time.Now()
				lockTargetStage = lockStageLocked
			} else if screenAnalyzer.GetEnemyLocked() {
				noLockStart = time.Now()
				lockTargetStage = lockStageLocked
				firstNoLockIteration = false
				// 5秒内没有闪避/冲刺则向前冲刺一次，防止怪跑远
				if time.Since(lastDodgeAt) >= 5*time.Second {
					enqueueAction(fightAction{
						executeAt: time.Now().Add(time.Millisecond),
						action:    ActionMoveForward,
					})
				}
			} else {
				now := time.Now()
				if noLockStart.IsZero() || now.Sub(noLockStart) >= lockCycleDuration {
					noLockStart = now
					lockTargetStage = lockStageLocked
				}
				elapsed := now.Sub(noLockStart)

				switch {
				case elapsed < lockMoveAfter:
					if firstNoLockIteration {
						maafocus.Print(ctx, i18n.T("autofight.start_combat_lock_target"))
						enqueueAction(fightAction{
							executeAt: now.Add(time.Millisecond),
							action:    ActionLockTarget,
						})
						lastLockAttempt = now
						firstNoLockIteration = false
					} else if elapsed >= lockLostConfirmDuration && now.Sub(lastLockAttempt) >= lockRetryInterval {
						enqueueAction(fightAction{
							executeAt: now.Add(time.Millisecond),
							action:    ActionLockTarget,
						})
						lastLockAttempt = now
					}
				default:
					if lockTargetStage < lockStageRecover {
						facingBack := screenAnalyzer.GetEnemyFacingBack()
						facingLeft := screenAnalyzer.GetEnemyFacingLeft()
						facingRight := screenAnalyzer.GetEnemyFacingRight()
						switch {
						case facingBack:
							enqueueAction(fightAction{
								executeAt: now.Add(time.Millisecond),
								action:    ActionMoveBack,
							})
						case facingLeft:
							enqueueAction(fightAction{
								executeAt: now.Add(time.Millisecond),
								action:    ActionMoveLeft,
							})
						case facingRight:
							enqueueAction(fightAction{
								executeAt: now.Add(time.Millisecond),
								action:    ActionMoveRight,
							})
						default:
							enqueueAction(fightAction{
								executeAt: now.Add(time.Millisecond),
								action:    ActionMoveForward,
							})
						}
						// 侧向/向后移动后阻塞整个动作队列等待转身完成，再锁定。
						// 队列按 executeAt 排序，用递增偏移保证 移动 -> Sleep -> Sleep -> 锁定 的顺序
						lockAt := now.Add(2 * time.Millisecond)
						if facingBack || facingLeft || facingRight {
							enqueueAction(fightAction{
								executeAt: now.Add(2 * time.Millisecond),
								action:    ActionSleepSecond,
							})
							enqueueAction(fightAction{
								executeAt: now.Add(3 * time.Millisecond),
								action:    ActionSleepSecond,
							})
							lockAt = now.Add(4 * time.Millisecond)
						}
						enqueueAction(fightAction{
							executeAt: lockAt,
							action:    ActionLockTarget,
						})
						lastLockAttempt = lockAt
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
					screenAnalyzer.MarkLabelUsed(endSkillFullLabel())
					for _, idx := range endSkillFull {
						if idx < 5-characterCount {
							// 槽位不在可操作干员范围内：继续尝试下一个就绪槽位，
							// 而不是无条件放弃整轮（旧逻辑 break 会漏放后面的干员）
							continue
						}
						op := idx + characterCount - 4
						// 释放验证窗：入队后 endSkillRecastWindow 内不重复施放，
						// 防止长按动画期 HUD 仍显示就绪导致连点；窗口过后槽位
						// 仍被识别为就绪则视为上次长按未生效，自动重试
						if time.Since(endSkillCastAt[op-1]) < endSkillRecastWindow {
							continue
						}
						enqueueAction(fightAction{
							executeAt: time.Now(),
							action:    endSkillAction(op),
						})
						endSkillCastAt[op-1] = time.Now()
						break
					}
				}
			}
			if params.EnableSkill && energyLevel >= 1 {
				if params.EnableBreakAccumulatingPower && screenAnalyzer.GetEnemyAccumulatingPower(true) {
					if op, next, ok := pickSkillOperator(skillModes, &skillUsedOnce, skillCycleIndex, characterCount); ok {
						maafocus.Print(ctx, i18n.T("autofight.enemy_accumulating_power"))
						enqueueAction(fightAction{
							executeAt: time.Now(),
							action:    skillAction(op),
						})
						skillCycleIndex = next
					}
				} else if energyLevel > params.ReserveSkillLevel && hasEnemyTarget {
					log.Debug().
						Str("component", "AutoFight").
						Int("energyLevel", energyLevel).
						Int("reserveLevel", params.ReserveSkillLevel).
						Msg("energy level above reserve, using skill")
					if op, next, ok := pickSkillOperator(skillModes, &skillUsedOnce, skillCycleIndex, characterCount); ok {
						enqueueAction(fightAction{
							executeAt: time.Now(),
							action:    skillAction(op),
						})
						skillCycleIndex = next
					}
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
							screenAnalyzer.MarkLabelUsed(endSkillFullLabel())
							timeline.PopFrontAction()
						}
					case "skill":
						// 不消耗技力的动作（如取消架势）不检查能量，也不标记能量已用
						if (energyLevel >= 1 || action.NoEnergyCost) && hasEnemyTarget {
							enqueueAction(fightAction{
								executeAt: time.Now(),
								action:    skillAction(op),
							})
							if !action.NoEnergyCost {
								screenAnalyzer.MarkLabelUsed(LabelEnergyLevelFull)
							}
							timeline.PopFrontAction()
						}
					}
				}
			}
		}

		// ADB 断触兜底：按住中的普攻触点被模拟器层面的中断掐断后不会自行恢复，
		// 按周期补按一次。PC 端的按住是键鼠状态，不需要也不应该重按。
		if params.EnableAttack && screenAnalyzer.mobileLayout && time.Since(lastAttackReassert) >= attackReassertInterval {
			lastAttackReassert = time.Now()
			enqueueAction(fightAction{
				executeAt: time.Now(),
				action:    ActionReassertAttack,
			})
		}

		drainActionQueue(ctx)
	}
	// 战斗退出瞬间：先清空动作队列，再抬起普攻触点。
	// 队列里残留的 ActionMoveForward / ActionDodge / ActionCombo 会在退出后
	// 被下一轮 drainActionQueue 执行，让角色「战斗结束了还在往前跑」——
	// 出口就在旁边也被冲过去。抬触点放在最前面，杜绝退出后普攻继续把角色
	// 带向更远处。
	actionQueue = nil
	lastDodgeAt = time.Time{}
	if params.EnableAttack {
		ctx.RunAction("__AutoFightActionAttackTouchUp", maa.Rect{600, 320, 80, 80}, "", nil)
	}

	// 等待最后一帧动作收敛，但不再固定空等 3 秒：
	// 上游原版在这里固定耗 3 秒只截图不做决策，导致「怪杀完了还在点、
	// 不赶紧转视角找出口」。改为条件退出——只要结算/出口画面已出现
	// （pause_exit_nodes 命中）或画面已无敌人，立刻把控制权交回 pipeline
	// 去转视角找出口；最坏情况仍等满 3 秒，保证截图状态收敛。
	waitStart := time.Now()
	for time.Since(waitStart) < 3*time.Second {
		if ctx.GetTasker().Stopping() {
			break
		}
		img, ok := captureAndUpdateScreenDetail(ctx)
		if !ok {
			break
		}
		if time.Since(waitStart) < 300*time.Millisecond {
			continue // 至少给一帧缓冲，避免刚退出就抢跑
		}
		if len(params.PauseExitNodes) > 0 && hitExitNode(ctx, img, params.PauseExitNodes) != "" {
			log.Info().Str("component", "AutoFight").Msg("settlement detected — skipping post-fight wait")
			break
		}
		if !screenAnalyzer.GetEnemyTarget() && !screenAnalyzer.GetEnemyLocked() &&
			!screenAnalyzer.GetEnemyDodge() && !screenAnalyzer.GetEnemyBossHealth() {
			log.Info().Str("component", "AutoFight").Msg("no enemy on screen — skipping post-fight wait")
			break
		}
	}

	// 退出后不再补发 LockTarget：战斗已经结束，残留的 EnemyLocked 判定
	// 只是上一场画面尾部，补发会额外触发一次点击/转向，干扰后续走位。
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
			ctx.RunAction("__AutoFightActionComboClick", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionSkill1:
			ctx.RunAction("__AutoFightActionSkillOperators1", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionSkill2:
			ctx.RunAction("__AutoFightActionSkillOperators2", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionSkill3:
			ctx.RunAction("__AutoFightActionSkillOperators3", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionSkill4:
			ctx.RunAction("__AutoFightActionSkillOperators4", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionEndSkill1:
			ctx.RunAction("__AutoFightActionEndSkillOperators1", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionEndSkill2:
			ctx.RunAction("__AutoFightActionEndSkillOperators2", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionEndSkill3:
			ctx.RunAction("__AutoFightActionEndSkillOperators3", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionEndSkill4:
			ctx.RunAction("__AutoFightActionEndSkillOperators4", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionLockTarget:
			ctx.RunAction("__AutoFightActionLockTarget", maa.Rect{600, 320, 80, 80}, "", nil)
		case ActionDodge:
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
		case ActionReassertAttack:
			// 先抬起再按下：对已经按住的触点重复 TouchDown 在后端行为不一致，
			// 而触点已断时 TouchUp 只是空操作，错误无需处理。
			ctx.RunAction("__AutoFightActionAttackTouchUp", maa.Rect{600, 320, 80, 80}, "", nil)
			ctx.RunAction("__AutoFightActionAttackTouchDown", maa.Rect{600, 320, 80, 80}, "", nil)
		}
	}
}
