package autofight

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 时间单位与游戏一致：60fps，prepDuration=300，因此战斗 t=0 对应 frame 300。
// 上层只看 ultimate / skill 两类动作；Endaxis 的 battleSkill 与 skill 等价派发。
// 其余 type（link / attack / comboSkill 等）不进入派发队列。
const (
	timelineFrameBase = 300
	timelineFPS       = 60.0
)

// EndAxisAction 是从时间轴中提取出的一个待派发的 ultimate/skill 动作。
// requisites / statusEffects 为内部字段：requisites 用于在派发前判断条件是否满足，
// statusEffects 用于在动作实际执行后更新干员状态模型（如开关型战技的姿态）。
type EndAxisAction struct {
	Type      string // "ultimate" | "skill"（battleSkill 收集时已规范化为 skill）
	TrackIdx  int    // 0..3，对应 scenario.data.tracks 的下标
	StartTime int    // 帧（与 JSON 里 startTime 一致，基准 300）
	Duration  int    // 帧
	Name      string
	ID        string

	requisites    []timelineRequisiteRaw
	statusEffects []timelineStatusEffect
}

// 以下是仅用于反序列化 JSON 的精简结构，业务上只关心这几个字段，
// 其余字段（damageTicks/equip/stats 等）均忽略。
type timelineActionRaw struct {
	Type       string                 `json:"type"`
	StartTime  int                    `json:"startTime"`
	Duration   int                    `json:"duration"`
	Name       string                 `json:"name"`
	ID         string                 `json:"id"`
	Requisites []timelineRequisiteRaw `json:"requisites"`
	Hits       []timelineHitRaw       `json:"hits"`
	Payload    *timelinePayloadRaw    `json:"payload"`
}

// timelineRequisiteRaw 是动作的释放条件（与 end-axis 数据语义一致）：
// 条件不满足时该动作应被跳过。condition 为条件树，支持
// operatorStatus / not / or / and 以及数组（全部满足）。
type timelineRequisiteRaw struct {
	ID         string          `json:"id"`
	Condition  json.RawMessage `json:"condition"`
	MessageKey string          `json:"messageKey"`
	Params     json.RawMessage `json:"params"`
}

type timelinePayloadRaw struct {
	Hits []timelineHitRaw `json:"hits"`
}

type timelineHitRaw struct {
	Effects []json.RawMessage `json:"effects"`
}

// timelineConditionRaw 是 requisites 条件树节点的精简结构。
// 无法在运行时求值的条件种类（skillCooldownReady / ultimateCooldownReady /
// enemyStatus / enemyHp 等）一律视为满足，避免误跳过动作；
// 只有 operatorStatus 类条件参与实际跳过判定。
type timelineConditionRaw struct {
	Kind       string             `json:"kind"`
	Status     json.RawMessage    `json:"status"`
	Stacks     *timelineStacksRaw `json:"stacks"`
	Condition  json.RawMessage    `json:"condition"`
	Conditions []json.RawMessage  `json:"conditions"`
}

type timelineStacksRaw struct {
	Compare string `json:"compare"` // "exact" | "atLeast" | "atMost"
	Count   int    `json:"count"`
}

// timelineStatusEffect 是动作对干员状态的一次修改，取自动作 hits 中的
// status / consume 效果，仅在动作执行后用于更新状态模型。
// status 效果按 duration 过期（如莉诺演唱姿态持续 60 秒，到期后游戏内
// 姿态自动结束，requisites 会重新允许施法）；consume 效果提前清除状态。
type timelineStatusEffect struct {
	kind           string // "status" | "consume"
	id             string // status 效果的 id
	target         string // "self" | "team"
	duration       int    // 帧；0 表示不自动过期（直到被 consume）
	stacks         int
	operatorStatus []string        // consume 效果要清除的状态 id 列表
	condition      json.RawMessage // 可选的生效条件
}

type timelineTrackRaw struct {
	ID      string              `json:"id"`
	Actions []timelineActionRaw `json:"actions"`
}

type timelineDataRaw struct {
	Tracks []timelineTrackRaw `json:"tracks"`
}

type timelineScenarioRaw struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Data timelineDataRaw `json:"data"`
}

type timelineRootRaw struct {
	ScenarioList []timelineScenarioRaw `json:"scenarioList"`
}

// EndAxisTimeline 解析 Endaxis 时间轴数据码，并按时间轴派发 ultimate/skill 动作。
//
// 输入格式为 Endaxis（www.end-axis.com）网站"复制数据码"按钮生成的字符串：
// 内层是与"导出 JSON"完全一致的项目数据，外层经过 gzip 压缩并使用 URL-safe
// base64（'+'→'-'、'/'→'_'、去掉尾随 '='）编码。
//
// 用法：
//
//	t := NewEndAxisTimeline()
//	t.SetTimelineCode(code)
//	if t.SelectScenario(ctx, characterCount, comboFull, endSkillFull, energy, skipComboCooldown) {
//	    for !t.ActionFinish() {
//	        if a, ok := t.FrontAction(); ok {
//	            // ... 在外部执行该动作 ...
//	            t.PopFrontAction()
//	        }
//	    }
//	}
//
// 时序：SelectScenario 成功后内部计时即开始；当 FrontAction 返回一个动作时，
// 计时被自动暂停，直到 PopFrontAction 调用后再恢复。
type EndAxisTimeline struct {
	root        *timelineRootRaw
	selectedID  string
	queue       []EndAxisAction
	energyLevel int

	started        bool          // SelectScenario 是否成功启动了时间轴
	paused         bool          // 当前是否处于暂停（等待 PopFrontAction）
	startReal      time.Time     // 时间轴起点的真实时间（即 SelectScenario 成功的瞬间，对应 frame=300）
	pausedAt       time.Time     // 当前这一段暂停的起始真实时间，仅在 paused=true 时有意义
	pausedDuration time.Duration // 截至上次 resume，已累计的暂停总时长
	endFrame       int           // 当前 scenario 内所有 action 的最晚结束帧 max(startTime + duration)

	// 干员状态模型：仅在 end-axis 数据中同时被 requisites 引用、又被某个动作
	// 的效果置位/清除的状态才会被跟踪（见 trackedStatuses）。
	// 状态跨 scenario 轮次持续存在（开关型战技的姿态不会因重新选择方案而消失），
	// 因此 reset() 不清空该模型，只在 SetTimelineCode 更换数据码时重建。
	statuses        map[int]map[string]*timelineStatusState // trackIdx -> statusID -> 状态
	trackedStatuses map[string]struct{}                     // 需要跟踪的状态 id 集合
	lastSkippedName string                                  // 最近一次因条件不满足被跳过的动作名（供上层提示）
}

// timelineStatusState 描述一个干员状态的运行时快照。
// 状态按动作效果携带的 duration 过期（如莉诺演唱姿态持续 60 秒），
// 到期后视为不存在，requisites 会重新允许施法；也可被 consume 提前清除。
type timelineStatusState struct {
	stacks int       // 层数；0 表示不存在
	expiry time.Time // 过期时刻；零值表示不自动过期（直到被 consume）
}

// NewEndAxisTimeline 返回一个空的时间轴对象，使用前需先调用 SetTimelineCode。
func NewEndAxisTimeline() *EndAxisTimeline {
	return &EndAxisTimeline{}
}

// SetTimelineCode 解析传入的 Endaxis 数据码（base64url(gzip(JSON))）。
// 成功返回 true，失败返回 false。失败时会清空已有的时间轴数据。
func (t *EndAxisTimeline) SetTimelineCode(code string) bool {
	jsonBytes, err := decodeEndAxisShareCode(code)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "EndAxisTimeline").
			Str("step", "SetTimelineCode").
			Msg("failed to decode timeline share code")
		t.root = nil
		t.reset()
		t.statuses = nil
		t.trackedStatuses = nil
		t.lastSkippedName = ""
		return false
	}

	var root timelineRootRaw
	if err := json.Unmarshal(jsonBytes, &root); err != nil {
		log.Error().
			Err(err).
			Str("component", "EndAxisTimeline").
			Str("step", "SetTimelineCode").
			Msg("failed to parse timeline json")
		t.root = nil
		t.reset()
		t.statuses = nil
		t.trackedStatuses = nil
		t.lastSkippedName = ""
		return false
	}

	t.root = &root
	t.reset()
	t.statuses = nil
	t.trackedStatuses = collectTrackedStatuses(&root)
	t.lastSkippedName = ""
	log.Info().
		Str("component", "EndAxisTimeline").
		Int("scenarioCount", len(root.ScenarioList)).
		Int("trackedStatusCount", len(t.trackedStatuses)).
		Msg("timeline share code loaded")
	return true
}

// decodeEndAxisShareCode 把 Endaxis "复制数据码" 生成的字符串还原成项目 JSON。
// 网站侧的生成流程见 src/utils/gzipUtils.js：JSON.stringify → gzip → base64
// 后再做 '+'→'-'、'/'→'_'、去掉尾随 '=' 三项替换；这里执行其逆操作。
func decodeEndAxisShareCode(code string) ([]byte, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(code), "=")
	if trimmed == "" {
		return nil, fmt.Errorf("empty share code")
	}

	compressed, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("base64url decode: %w", err)
	}

	gr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	const maxPlainSize = 5 << 20 // 5 MiB
	lr := io.LimitReader(gr, maxPlainSize+1)
	plain, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("gzip read: %w", err)
	}
	if len(plain) > maxPlainSize {
		return nil, fmt.Errorf("share code payload too large (>%d bytes)", maxPlainSize)
	}
	return plain, nil
}

// SelectScenario 根据当前队伍状态挑选一个匹配的 scenario，并启动时间轴。
//
// 参数：
//   - characterCount：当前队伍的角色数量（1..4）；track 0..characterCount-1 对应队伍里
//     的 1..characterCount 号角色；
//   - characterComboFull：连携已就绪的角色编号列表（1..characterCount 中的子集），
//     例如 [1, 3] 表示 1 号、3 号角色连携满；
//   - endSkillFull：终结技已充能完毕的角色编号列表（1..characterCount 中的子集）；
//   - energyLevel：当前能量条等级，目前仅作为状态保留，未参与匹配；
//   - skipComboCooldown：为 true 时跳过全员连携就绪检测，轴完立刻尝试匹配下一 scenario。
//
// 匹配规则：
//  1. 若 skipComboCooldown 为 false，且 1..characterCount 任一角色不在 characterComboFull 中
//     （即有人连携没满），直接返回 false，不进入 scenario 匹配，并通过 maafocus.PrintThrottle（3s）
//     输出"等待连携技冷却完成"的提示；
//  2. 对每个 scenario，逐个 track i ∈ [0, characterCount) 检查：若该 track 含 type==ultimate
//     的 action，则对应角色编号 i+1 必须在 endSkillFull 列表中；任一项不满足则跳过该
//     scenario，并通过 maafocus.PrintThrottle（3s）输出"终结技未充能完毕"的提示；
//  3. scenario 内若没有任何 type==ultimate / skill / battleSkill 的 action（即没有可派发的动作），
//     也跳过该 scenario，并通过 maafocus.PrintThrottle（3s）输出"没有战技或终结技"的提示；
//  4. 所有 scenario 都不满足时返回 false。
//
// 选中 scenario 时通过 maafocus.Print 输出多语言提示；跳过提示限频，ctx 为 nil 时仅记录日志。
func (t *EndAxisTimeline) SelectScenario(ctx *maa.Context, characterCount int, characterComboFull, endSkillFull []int, energyLevel int, skipComboCooldown bool) bool {
	t.reset()

	if t.root == nil {
		return false
	}

	if !skipComboCooldown {
		for op := 1; op <= characterCount; op++ {
			if !slices.Contains(characterComboFull, op) {
				log.Debug().
					Str("component", "EndAxisTimeline").
					Str("step", "SelectScenario").
					Int("waitingOperator", op).
					Msg("combo not ready for all operators")
				maafocus.PrintThrottle(ctx, 3*time.Second, i18n.T("autofight.endaxis.waiting_combo_cooldown"))
				return false
			}
		}
	}

	for i := range t.root.ScenarioList {
		sc := &t.root.ScenarioList[i]
		if !scenarioMatchesEndSkill(sc, endSkillFull, characterCount) {
			log.Debug().
				Str("component", "EndAxisTimeline").
				Str("step", "SelectScenario").
				Str("scenarioId", sc.ID).
				Str("scenarioName", sc.Name).
				Msg("scenario skipped: ultimate gauge not full")
			maafocus.PrintThrottle(ctx, 3*time.Second, i18n.T("autofight.endaxis.scenario_skipped_endskill", sc.Name))
			continue
		}

		actions := collectTimelineActions(sc)
		if len(actions) == 0 {
			log.Debug().
				Str("component", "EndAxisTimeline").
				Str("step", "SelectScenario").
				Str("scenarioId", sc.ID).
				Str("scenarioName", sc.Name).
				Msg("scenario skipped: no skill/ultimate actions")
			maafocus.PrintThrottle(ctx, 3*time.Second, i18n.T("autofight.endaxis.scenario_skipped_no_action", sc.Name))
			continue
		}

		t.selectedID = sc.ID
		t.queue = actions
		t.energyLevel = energyLevel
		t.started = true
		t.paused = false
		t.startReal = time.Now()
		t.pausedDuration = 0
		t.endFrame = computeTimelineEndFrame(sc)

		log.Info().
			Str("component", "EndAxisTimeline").
			Str("step", "SelectScenario").
			Str("scenarioId", sc.ID).
			Str("scenarioName", sc.Name).
			Int("actionCount", len(t.queue)).
			Int("endFrame", t.endFrame).
			Int("energyLevel", energyLevel).
			Msg("scenario selected")
		maafocus.Print(ctx, i18n.T("autofight.endaxis.scenario_selected", sc.Name))
		return true
	}

	log.Debug().
		Str("component", "EndAxisTimeline").
		Str("step", "SelectScenario").
		Int("scenarioCount", len(t.root.ScenarioList)).
		Msg("no matching scenario")
	maafocus.PrintThrottle(ctx, 3*time.Second, i18n.T("autofight.endaxis.no_matching_scenario"))
	return false
}

// FrontAction 返回当前帧应触发的队首 ultimate/skill 动作。
// 命中时会自动暂停内部计时，直到 PopFrontAction 调用后再恢复。
// 未到时间或队列为空时返回 nil, false。
//
// 队首动作已到触发帧但 requisites 条件不满足时（例如开关型战技的姿态已生效），
// 该动作会被直接丢弃并继续检查下一个动作，与 end-axis 数据语义一致；
// 被跳过的动作名可通过 TakeSkippedActionName 取回用于用户提示。
func (t *EndAxisTimeline) FrontAction() *EndAxisAction {
	if !t.started || len(t.queue) == 0 {
		return nil
	}
	frame := t.currentFrame()
	for len(t.queue) > 0 {
		head := &t.queue[0]
		if head.StartTime > frame {
			return nil
		}

		if unmet := t.firstUnmetRequisite(head); unmet != nil {
			t.lastSkippedName = head.Name
			if t.lastSkippedName == "" {
				t.lastSkippedName = head.ID
			}
			log.Info().
				Str("component", "EndAxisTimeline").
				Str("step", "FrontAction").
				Str("actionId", head.ID).
				Str("actionName", head.Name).
				Int("trackIdx", head.TrackIdx).
				Str("requisiteId", unmet.ID).
				Str("messageKey", unmet.MessageKey).
				Msg("action skipped: requisite condition not met")
			t.queue = t.queue[1:]
			continue
		}

		t.pause()
		h := *head
		return &h
	}
	return nil
}

// TakeSkippedActionName 返回并清空最近一次因 requisites 条件不满足而被跳过的
// 动作名；没有发生过跳过时返回空字符串。
func (t *EndAxisTimeline) TakeSkippedActionName() string {
	name := t.lastSkippedName
	t.lastSkippedName = ""
	return name
}

// PopFrontAction 删除当前队首动作并恢复计时。队列为空或时间轴未启动时为空操作。
// 动作被派发后，其 hits 中的 status / consume 效果会同步更新干员状态模型。
func (t *EndAxisTimeline) PopFrontAction() {
	if !t.started || len(t.queue) == 0 {
		return
	}

	popped := t.queue[0]
	t.queue = t.queue[1:]
	t.applyActionStatusChanges(&popped)
	t.resume()

	log.Debug().
		Str("component", "EndAxisTimeline").
		Str("step", "PopFrontAction").
		Str("type", popped.Type).
		Int("trackIdx", popped.TrackIdx).
		Int("startTime", popped.StartTime).
		Int("remaining", len(t.queue)).
		Msg("action popped")
}

// ActionFinish 返回当前 scenario 的时间轴是否已经结束。
// 同时满足两个条件才视为结束：
//  1. 派发队列已空（所有 ultimate/skill 都已 Pop）；
//  2. 当前逻辑帧已经走到该 scenario 内所有 action 的最晚结束帧。
//
// 这样可以避免最后一个 ultimate/skill Pop 完后立刻进入下一次 SelectScenario，
// 留出 scenario 末尾普攻/位移等动作所占用的时间窗口。
func (t *EndAxisTimeline) ActionFinish() bool {
	if !t.started {
		return true
	}
	if len(t.queue) > 0 {
		return false
	}
	return t.currentFrame() >= t.endFrame
}

// reset 清空运行时状态，但不清空 root（已加载的 JSON）。
// 干员状态模型（statuses / trackedStatuses）属于整场战斗的持续状态，
// 不会被清空；更换数据码时由 SetTimelineCode 显式重建。
func (t *EndAxisTimeline) reset() {
	t.selectedID = ""
	t.queue = nil
	t.energyLevel = 0
	t.started = false
	t.paused = false
	t.startReal = time.Time{}
	t.pausedAt = time.Time{}
	t.pausedDuration = 0
	t.endFrame = 0
}

// currentFrame 返回当前逻辑帧。
// 逻辑帧 = 基准帧(300) + (真实流逝时间 - 累计暂停时长) 换算成的帧数。
func (t *EndAxisTimeline) currentFrame() int {
	if !t.started {
		return 0
	}
	elapsed := time.Since(t.startReal) - t.pausedDuration
	if t.paused {
		elapsed -= time.Since(t.pausedAt)
	}
	return timelineFrameBase + int(elapsed.Seconds()*timelineFPS)
}

func (t *EndAxisTimeline) pause() {
	if t.paused {
		return
	}
	t.pausedAt = time.Now()
	t.paused = true
}

func (t *EndAxisTimeline) resume() {
	if !t.paused {
		return
	}
	t.pausedDuration += time.Since(t.pausedAt)
	t.paused = false
}

// scenarioMatchesEndSkill 检查 scenario 的 track 与终结技就绪情况的对应关系：
// 只要 track i（i ∈ [0, characterCount)）内含 type==ultimate 的 action，
// 对应的角色编号 i+1 就必须出现在 endSkillFull 列表中。
// 超出 characterCount 的 track（队伍里没有对应角色）一律忽略。
func scenarioMatchesEndSkill(sc *timelineScenarioRaw, endSkillFull []int, characterCount int) bool {
	for i := 0; i < characterCount; i++ {
		if i >= len(sc.Data.Tracks) {
			continue
		}
		if !trackHasUltimate(&sc.Data.Tracks[i]) {
			continue
		}
		if !slices.Contains(endSkillFull, i+1) {
			return false
		}
	}
	return true
}

func trackHasUltimate(track *timelineTrackRaw) bool {
	for i := range track.Actions {
		if track.Actions[i].Type == "ultimate" {
			return true
		}
	}
	return false
}

// computeTimelineEndFrame 扫描 scenario 内所有 track 的所有 action（不限制 type），
// 返回 max(startTime + duration)，即整条时间轴的最晚结束帧。
// 没有任何 action 时返回 timelineFrameBase，使 ActionFinish 立即可结束。
func computeTimelineEndFrame(sc *timelineScenarioRaw) int {
	end := timelineFrameBase
	for _, track := range sc.Data.Tracks {
		for _, a := range track.Actions {
			if stop := a.StartTime + a.Duration; stop > end {
				end = stop
			}
		}
	}
	return end
}

// collectTimelineActions 从 scenario 的 4 条 track 中抽取所有 ultimate/skill 动作，
// 并按 startTime 升序合并为一个全局派发队列。battleSkill 与 skill 等价，统一为 skill。
func collectTimelineActions(sc *timelineScenarioRaw) []EndAxisAction {
	var out []EndAxisAction
	for trackIdx, track := range sc.Data.Tracks {
		for i := range track.Actions {
			a := &track.Actions[i]
			actionType := a.Type
			switch actionType {
			case "battleSkill":
				actionType = "skill"
			case "ultimate", "skill":
			default:
				continue
			}
			out = append(out, EndAxisAction{
				Type:          actionType,
				TrackIdx:      trackIdx,
				StartTime:     a.StartTime,
				Duration:      a.Duration,
				Name:          a.Name,
				ID:            a.ID,
				requisites:    a.Requisites,
				statusEffects: collectActionStatusEffects(a),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartTime != out[j].StartTime {
			return out[i].StartTime < out[j].StartTime
		}
		return out[i].TrackIdx < out[j].TrackIdx
	})
	return out
}

// collectActionStatusEffects 提取动作 hits 中会改变干员状态的效果（status / consume），
// 供动作执行后更新状态模型。优先读取顶层 hits（buildBaseAction 会把 payload.hits
// 克隆到 hits），旧数据只有 payload.hits 时退回读取 payload。
func collectActionStatusEffects(a *timelineActionRaw) []timelineStatusEffect {
	var out []timelineStatusEffect
	hits := a.Hits
	if len(hits) == 0 && a.Payload != nil {
		hits = a.Payload.Hits
	}
	for i := range hits {
		collectRawStatusEffects(hits[i].Effects, &out)
	}
	return out
}

// collectRawStatusEffects 递归遍历 effects 数组，收集 status / consume 效果；
// 嵌套结构（effect.hit.effects、effect.effects）会一并下钻。
func collectRawStatusEffects(nodes []json.RawMessage, out *[]timelineStatusEffect) {
	for _, raw := range nodes {
		var eff timelineEffectRaw
		if err := json.Unmarshal(raw, &eff); err != nil || eff.Kind == "" {
			// 不是标准效果对象：尝试按通用对象下钻嵌套字段，保证对结构变化健壮。
			var generic map[string]json.RawMessage
			if json.Unmarshal(raw, &generic) == nil {
				if nested, ok := generic["effects"]; ok {
					var arr []json.RawMessage
					if json.Unmarshal(nested, &arr) == nil {
						collectRawStatusEffects(arr, out)
					}
				}
				if nested, ok := generic["hit"]; ok {
					var h timelineHitRaw
					if json.Unmarshal(nested, &h) == nil {
						collectRawStatusEffects(h.Effects, out)
					}
				}
			}
			continue
		}

		switch eff.Kind {
		case "status":
			if eff.ID == "" {
				continue
			}
			*out = append(*out, timelineStatusEffect{
				kind:      "status",
				id:        eff.ID,
				target:    eff.Target,
				duration:  eff.Duration,
				stacks:    eff.Stacks,
				condition: eff.Condition,
			})
		case "consume":
			ids := parseStatusList(eff.OperatorStatus)
			if len(ids) == 0 {
				continue
			}
			team := eff.ConsumeTarget == "team" || eff.ConsumeScope == "team"
			target := "self"
			if team {
				target = "team"
			}
			*out = append(*out, timelineStatusEffect{
				kind:           "consume",
				target:         target,
				operatorStatus: ids,
				condition:      eff.Condition,
			})
		}

		// 递归下钻嵌套效果（如 hit 内嵌 effects）。
		if eff.Hit != nil {
			var h timelineHitRaw
			if json.Unmarshal(eff.Hit, &h) == nil {
				collectRawStatusEffects(h.Effects, out)
			}
		}
		collectRawStatusEffects(eff.Effects, out)
	}
}

// timelineEffectRaw 是 collectRawStatusEffects 使用的效果精简结构。
type timelineEffectRaw struct {
	Kind           string            `json:"kind"`
	ID             string            `json:"id"`
	Target         string            `json:"target"`
	Duration       int               `json:"duration"`
	Stacks         int               `json:"stacks"`
	OperatorStatus json.RawMessage   `json:"operatorStatus"`
	ConsumeTarget  string            `json:"consumeTarget"`
	ConsumeScope   string            `json:"consumeScope"`
	Condition      json.RawMessage   `json:"condition"`
	Hit            json.RawMessage   `json:"hit"`
	Effects        []json.RawMessage `json:"effects"`
}

// collectTrackedStatuses 扫描全部 scenario 中所有动作的 requisites，收集被
// operatorStatus 条件引用的状态 id；同时检查这些状态是否真的会被某个动作的
// 效果置位/清除。只有"既被引用、又被动作效果管理"的状态才纳入跟踪：
// 仅被引用但由被动/触发器（MaaEnd 运行时未建模）施加的状态视为不存在，
// 以保证 not(or(...)) 这类开关型战技守卫在部分成员未被动作管理时仍能正确求值。
func collectTrackedStatuses(root *timelineRootRaw) map[string]struct{} {
	referenced := make(map[string]struct{})
	managed := make(map[string]struct{})
	var walkCondition func(raw json.RawMessage)
	walkCondition = func(raw json.RawMessage) {
		if len(raw) == 0 || string(raw) == "null" {
			return
		}
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) == nil {
			for _, sub := range arr {
				walkCondition(sub)
			}
			return
		}
		var c timelineConditionRaw
		if err := json.Unmarshal(raw, &c); err != nil {
			return
		}
		switch c.Kind {
		case "operatorStatus":
			for _, s := range parseStatusList(c.Status) {
				referenced[s] = struct{}{}
			}
		case "not":
			walkCondition(c.Condition)
		case "or", "and":
			for _, sub := range c.Conditions {
				walkCondition(sub)
			}
		}
	}

	for i := range root.ScenarioList {
		sc := &root.ScenarioList[i]
		for ti := range sc.Data.Tracks {
			track := &sc.Data.Tracks[ti]
			for ai := range track.Actions {
				a := &track.Actions[ai]
				for ri := range a.Requisites {
					walkCondition(a.Requisites[ri].Condition)
				}
				for _, eff := range collectActionStatusEffects(a) {
					if eff.kind == "status" && eff.id != "" {
						managed[eff.id] = struct{}{}
					}
					for _, id := range eff.operatorStatus {
						managed[id] = struct{}{}
					}
				}
			}
		}
	}

	tracked := make(map[string]struct{})
	for id := range referenced {
		if _, ok := managed[id]; ok {
			tracked[id] = struct{}{}
		}
	}
	return tracked
}

// firstUnmetRequisite 返回动作第一个不满足的 requisites；全部满足时返回 nil。
func (t *EndAxisTimeline) firstUnmetRequisite(a *EndAxisAction) *timelineRequisiteRaw {
	for i := range a.requisites {
		req := &a.requisites[i]
		if !t.conditionMet(req.Condition, a.TrackIdx) {
			return req
		}
	}
	return nil
}

// 条件求值采用三值逻辑：除"满足/不满足"外，无法在运行时求值的条件种类
// （skillCooldownReady / ultimateCooldownReady / ultimateEnhancement /
// enemyStatus / enemyHp 等）标记为"未知"。未知在 or/not/and 中按真值表传播，
// 顶层未知一律视为满足（放行），避免误跳过动作；只有能确定性判定为
// 不满足的 requisites 才会导致动作被跳过。
const (
	condUnknown     = -1
	condUnsatisfied = 0
	condSatisfied   = 1
)

// conditionMet 求值一个条件树，返回该条件是否满足。
// 顶层未知（无法求值）视为满足。
func (t *EndAxisTimeline) conditionMet(raw json.RawMessage, trackIdx int) bool {
	return t.evalCondition(raw, trackIdx) != condUnsatisfied
}

// evalCondition 递归求值条件树，返回 condUnknown / condUnsatisfied / condSatisfied。
func (t *EndAxisTimeline) evalCondition(raw json.RawMessage, trackIdx int) int {
	if len(raw) == 0 || string(raw) == "null" {
		return condSatisfied
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		// 数组条件：全部满足才满足；任一不满足则不满足；否则未知。
		anyUnknown := false
		for _, sub := range arr {
			switch t.evalCondition(sub, trackIdx) {
			case condUnsatisfied:
				return condUnsatisfied
			case condUnknown:
				anyUnknown = true
			}
		}
		if anyUnknown {
			return condUnknown
		}
		return condSatisfied
	}
	var c timelineConditionRaw
	if err := json.Unmarshal(raw, &c); err != nil {
		return condSatisfied
	}
	switch c.Kind {
	case "operatorStatus":
		for _, s := range parseStatusList(c.Status) {
			if !t.isTracked(s) {
				// 未被任何动作效果管理的状态视为不存在（如该状态只由
				// 被动/触发器施加，MaaEnd 运行时未建模）。
				continue
			}
			st := t.statusState(trackIdx, s)
			if st == nil || st.stacks <= 0 || (!st.expiry.IsZero() && time.Now().After(st.expiry)) {
				continue
			}
			if c.Stacks != nil && !stacksMet(st.stacks, c.Stacks) {
				continue
			}
			return condSatisfied
		}
		return condUnsatisfied
	case "not":
		switch t.evalCondition(c.Condition, trackIdx) {
		case condSatisfied:
			return condUnsatisfied
		case condUnsatisfied:
			return condSatisfied
		default:
			return condUnknown
		}
	case "or":
		anyUnknown := false
		for _, sub := range c.Conditions {
			switch t.evalCondition(sub, trackIdx) {
			case condSatisfied:
				return condSatisfied
			case condUnknown:
				anyUnknown = true
			}
		}
		if anyUnknown {
			return condUnknown
		}
		return condUnsatisfied
	case "and":
		anyUnknown := false
		for _, sub := range c.Conditions {
			switch t.evalCondition(sub, trackIdx) {
			case condUnsatisfied:
				return condUnsatisfied
			case condUnknown:
				anyUnknown = true
			}
		}
		if anyUnknown {
			return condUnknown
		}
		return condSatisfied
	default:
		// 无法在运行时求值的条件种类（cooldown / enemy 等），标记为未知。
		return condUnknown
	}
}

// applyActionStatusChanges 在动作实际执行后更新干员状态模型：
// status 效果置位对应状态（按 duration 过期，可带层数），
// consume 效果清除对应状态。
func (t *EndAxisTimeline) applyActionStatusChanges(a *EndAxisAction) {
	if len(a.statusEffects) == 0 {
		return
	}
	for i := range a.statusEffects {
		e := &a.statusEffects[i]
		if !t.statusEffectConditionMet(e, a.TrackIdx) {
			continue
		}
		for _, tr := range statusTargetTracks(e.target, a.TrackIdx) {
			switch e.kind {
			case "status":
				t.setStatus(tr, e.id, e.duration, e.stacks)
			case "consume":
				for _, id := range e.operatorStatus {
					t.clearStatus(tr, id)
				}
			}
		}
	}
}

func (t *EndAxisTimeline) statusEffectConditionMet(e *timelineStatusEffect, trackIdx int) bool {
	if len(e.condition) == 0 || string(e.condition) == "null" {
		return true
	}
	return t.conditionMet(e.condition, trackIdx)
}

// statusTargetTracks 解析效果的 target 语义："team" 作用于全部 4 条 track，
// 其余（self 等）作用于动作自身的 track。
func statusTargetTracks(target string, trackIdx int) []int {
	if target == "team" {
		return []int{0, 1, 2, 3}
	}
	return []int{trackIdx}
}

func (t *EndAxisTimeline) isTracked(id string) bool {
	if t.trackedStatuses == nil {
		return false
	}
	_, ok := t.trackedStatuses[id]
	return ok
}

func (t *EndAxisTimeline) statusState(trackIdx int, id string) *timelineStatusState {
	if t.statuses == nil {
		return nil
	}
	return t.statuses[trackIdx][id]
}

func (t *EndAxisTimeline) setStatus(trackIdx int, id string, duration, stacks int) {
	if !t.isTracked(id) {
		return
	}
	if t.statuses == nil {
		t.statuses = make(map[int]map[string]*timelineStatusState)
	}
	m := t.statuses[trackIdx]
	if m == nil {
		m = make(map[string]*timelineStatusState)
		t.statuses[trackIdx] = m
	}
	st := m[id]
	if st == nil {
		st = &timelineStatusState{}
		m[id] = st
	}
	if stacks > 0 {
		st.stacks += stacks
	} else if st.stacks < 1 {
		st.stacks = 1
	}
	if duration > 0 {
		st.expiry = time.Now().Add(time.Duration(duration) * time.Second / time.Duration(timelineFPS))
	}
}

func (t *EndAxisTimeline) clearStatus(trackIdx int, id string) {
	m := t.statuses[trackIdx]
	if m == nil {
		return
	}
	if st := m[id]; st != nil {
		st.stacks = 0
	}
}

func stacksMet(stacks int, c *timelineStacksRaw) bool {
	switch c.Compare {
	case "exact":
		return stacks == c.Count
	case "atMost":
		return stacks <= c.Count
	default: // "atLeast" 及缺省
		return stacks >= c.Count
	}
}

// parseStatusList 解析 operatorStatus 的 status 字段：字符串或字符串数组；
// EffectStat 对象（如 {modifier: "atkPercent"}）无法在运行时求值，直接忽略。
func parseStatusList(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		var out []string
		for _, item := range arr {
			if json.Unmarshal(item, &s) == nil {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
