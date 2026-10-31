package autofight

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 时间单位与旧格式一致：60fps，prepDuration=300，因此战斗 t=0 对应 frame 300。
// 新版项目格式（30fps，战斗 t=0 对应 frame 0）解析时换算到这套单位。
// 上层只看 ultimate / skill 两类动作；Endaxis 的 battleSkill 与 skill 等价派发。
// 其余 type（link / attack / comboSkill 等）不进入派发队列。
const (
	timelineFrameBase = 300
	timelineFPS       = 60.0
)

// endAxisProjectKind 是 Endaxis 新版项目格式 JSON 顶层 kind 字段的取值。
const endAxisProjectKind = "EndaxisProject"

// EndAxisAction 是从时间轴中提取出的一个待派发的 ultimate/skill 动作。
type EndAxisAction struct {
	Type         string // "ultimate" | "skill"（battleSkill 收集时已规范化为 skill）
	TrackIdx     int    // 0..3，对应 scenario.data.tracks 的下标
	StartTime    int    // 帧（与 JSON 里 startTime 一致，基准 300）
	Duration     int    // 帧
	Name         string
	ID           string
	NoEnergyCost bool // 不消耗技力（如取消架势），派发时跳过能量检查
}

// 以下是仅用于反序列化 JSON 的精简结构，业务上只关心这几个字段，
// 其余字段（damageTicks/equip/stats 等）均忽略。
// 新版项目格式会先转换成这套结构，json:"-" 的字段只由转换填充。
type timelineActionRaw struct {
	Type         string `json:"type"`
	StartTime    int    `json:"startTime"`
	Duration     int    `json:"duration"`
	Name         string `json:"name"`
	ID           string `json:"id"`
	NoEnergyCost bool   `json:"-"`
}

type timelineTrackRaw struct {
	ID      string              `json:"id"`
	Actions []timelineActionRaw `json:"actions"`
}

type timelineDataRaw struct {
	Tracks []timelineTrackRaw `json:"tracks"`
}

type timelineScenarioRaw struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Data         timelineDataRaw `json:"data"`
	EndFrame     *int            `json:"-"` // 模拟终止线换算后的帧，nil 表示未设置
	SkippedCasts int             `json:"-"` // 无法计算开始帧而跳过的技能数
}

type timelineRootRaw struct {
	ScenarioList []timelineScenarioRaw `json:"scenarioList"`
}

// 以下是 Endaxis 新版项目格式（kind == "EndaxisProject"）的精简结构。
// 每个技能只保存 startFrame，或连续组后段的 afterCastId（接在同轨前一个技能之后），
// 不保存时长；时长按 skillKey 从 endaxis_block_frames.json 查块宽。
type endAxisProjectRaw struct {
	Kind      string                      `json:"kind"`
	FPS       int                         `json:"fps"`
	Scenarios []endAxisProjectScenarioRaw `json:"scenarios"`
}

type endAxisProjectScenarioRaw struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Battle struct {
		SimulationRange *struct {
			EndFrame *int `json:"endFrame"`
		} `json:"simulationRange"`
	} `json:"battle"`
	Tracks []*endAxisProjectTrackRaw `json:"tracks"` // 空槽位为 null
}

type endAxisProjectTrackRaw struct {
	SkillCasts []endAxisSkillCastRaw `json:"skillCasts"`
}

type endAxisSkillCastRaw struct {
	ID     string `json:"id"`
	Source struct {
		SkillGroupKey string `json:"skillGroupKey"`
		SkillKey      string `json:"skillKey"`
		Action        string `json:"action"` // 玩家操作；旧项目转换来的数据没有
	} `json:"source"`
	Placement struct {
		StartFrame  *int   `json:"startFrame"`
		AfterCastID string `json:"afterCastId"`
	} `json:"placement"`
}

// endaxis_block_frames.json：skillKey → Endaxis 时间轴上的技能块宽（30fps）。
// 按 Endaxis 的块宽规则从游戏技能数据生成，游戏更新后需要重新生成。
// 只收录战技、终结技、普攻、连携；下落攻击、重击、闪避等不收录，按查不到处理。
//
//go:embed endaxis_block_frames.json
var endAxisBlockFramesJSON []byte

var loadEndAxisBlockFrames = sync.OnceValue(func() map[string]int {
	var frames map[string]int
	if err := json.Unmarshal(endAxisBlockFramesJSON, &frames); err != nil {
		log.Error().
			Err(err).
			Str("component", "EndAxisTimeline").
			Msg("failed to parse endaxis_block_frames.json")
		return nil
	}
	return frames
})

// endAxisDefaultBlockFrames 是块宽表查不到时（如新干员）按动作类型使用的默认块宽（30fps），
// 取表中同类技能的中位数。只用于计算时长，连续组的开始帧仍要求查到真实块宽。
var endAxisDefaultBlockFrames = map[string]int{
	"basicAttack": 21,
	"skill":       39,
	"comboSkill":  43,
	"ultimate":    86,
}

// lookupEndAxisBlockFrames 返回技能在 Endaxis 时间轴上的块宽（30fps），查不到返回 false。
func lookupEndAxisBlockFrames(skillKey string) (int, bool) {
	frames, ok := loadEndAxisBlockFrames()[skillKey]
	return frames, ok
}

// EndAxisTimeline 解析 Endaxis 时间轴数据码，并按时间轴派发 ultimate/skill 动作。
//
// 输入格式为 Endaxis（www.end-axis.com）网站"复制数据码"按钮生成的字符串：
// 内层是与"导出 JSON"完全一致的项目数据，外层经过 gzip 压缩并使用 URL-safe
// base64（'+'→'-'、'/'→'_'、去掉尾随 '='）编码。项目数据支持旧格式（scenarioList）
// 与新版项目格式（kind == "EndaxisProject"），后者在解析时转换为旧格式结构。
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
		return false
	}

	root, err := parseTimelineJSON(jsonBytes)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "EndAxisTimeline").
			Str("step", "SetTimelineCode").
			Msg("failed to parse timeline json")
		t.root = nil
		t.reset()
		return false
	}

	t.root = root
	t.reset()
	skippedCasts := 0
	for i := range root.ScenarioList {
		skippedCasts += root.ScenarioList[i].SkippedCasts
	}
	log.Info().
		Str("component", "EndAxisTimeline").
		Int("scenarioCount", len(root.ScenarioList)).
		Int("skippedCastCount", skippedCasts).
		Msg("timeline share code loaded")
	return true
}

// parseTimelineJSON 解析项目 JSON：新版项目格式转换为旧格式结构，其余按旧格式解析。
func parseTimelineJSON(jsonBytes []byte) (*timelineRootRaw, error) {
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(jsonBytes, &head); err != nil {
		return nil, err
	}

	if head.Kind != endAxisProjectKind {
		var root timelineRootRaw
		if err := json.Unmarshal(jsonBytes, &root); err != nil {
			return nil, err
		}
		return &root, nil
	}

	var project endAxisProjectRaw
	if err := json.Unmarshal(jsonBytes, &project); err != nil {
		return nil, err
	}
	return convertEndAxisProject(&project)
}

// convertEndAxisProject 把新版项目格式转换为旧格式结构：
//   - 帧从项目帧（fps 帧/秒，战斗 t=0 为 0）换算到 60fps、基准 300；
//   - duration 取块宽，查不到为 0；
//   - 设置了模拟终止线时记为 scenario 的结束帧；
//   - 无法计算开始帧的技能（连续组后段的前驱块宽查不到）跳过并计入 SkippedCasts。
func convertEndAxisProject(project *endAxisProjectRaw) (*timelineRootRaw, error) {
	if project.FPS <= 0 {
		return nil, fmt.Errorf("invalid project fps: %d", project.FPS)
	}
	toFrames := func(projectFrames int) int {
		return int(math.Round(float64(projectFrames) * timelineFPS / float64(project.FPS)))
	}

	root := &timelineRootRaw{ScenarioList: make([]timelineScenarioRaw, 0, len(project.Scenarios))}
	for _, ps := range project.Scenarios {
		sc := timelineScenarioRaw{ID: ps.ID, Name: ps.Name}
		if r := ps.Battle.SimulationRange; r != nil && r.EndFrame != nil {
			endFrame := timelineFrameBase + toFrames(*r.EndFrame)
			sc.EndFrame = &endFrame
		}

		sc.Data.Tracks = make([]timelineTrackRaw, len(ps.Tracks))
		for trackIdx, track := range ps.Tracks {
			if track == nil {
				continue
			}
			starts := resolveSkillCastStartFrames(track.SkillCasts)
			for i := range track.SkillCasts {
				cast := &track.SkillCasts[i]
				start, ok := starts[cast.ID]
				if !ok {
					sc.SkippedCasts++
					log.Warn().
						Str("component", "EndAxisTimeline").
						Str("step", "SetTimelineCode").
						Str("scenarioId", ps.ID).
						Int("trackIdx", trackIdx).
						Str("castId", cast.ID).
						Str("skillKey", cast.Source.SkillKey).
						Str("afterCastId", cast.Placement.AfterCastID).
						Msg("skill cast start frame unresolved, skip")
					continue
				}

				actionType, noEnergyCost := endAxisCastType(cast)
				blockFrames, ok := lookupEndAxisBlockFrames(cast.Source.SkillKey)
				if !ok {
					if blockFrames, ok = endAxisDefaultBlockFrames[actionType]; ok {
						log.Warn().
							Str("component", "EndAxisTimeline").
							Str("step", "SetTimelineCode").
							Str("skillKey", cast.Source.SkillKey).
							Int("defaultBlockFrames", blockFrames).
							Msg("block frames not found, use default")
					}
				}
				sc.Data.Tracks[trackIdx].Actions = append(sc.Data.Tracks[trackIdx].Actions, timelineActionRaw{
					Type:         actionType,
					StartTime:    timelineFrameBase + toFrames(start),
					Duration:     toFrames(blockFrames),
					Name:         cast.Source.SkillKey,
					ID:           cast.ID,
					NoEnergyCost: noEnergyCost,
				})
			}
		}
		root.ScenarioList = append(root.ScenarioList, sc)
	}
	return root, nil
}

// resolveSkillCastStartFrames 计算同一轨道内每个技能的开始帧（项目帧）。
// 独立技能与连续组组首直接取 startFrame；连续组后段（afterCastId）取前驱的开始帧加前驱的块宽。
// 前驱块宽查不到或前驱本身无法计算时，该技能无法计算，不出现在返回结果中。
func resolveSkillCastStartFrames(casts []endAxisSkillCastRaw) map[string]int {
	byID := make(map[string]*endAxisSkillCastRaw, len(casts))
	for i := range casts {
		byID[casts[i].ID] = &casts[i]
	}

	starts := make(map[string]int, len(casts))
	failed := make(map[string]bool)
	var resolve func(id string, depth int) (int, bool)
	resolve = func(id string, depth int) (int, bool) {
		if start, ok := starts[id]; ok {
			return start, true
		}
		cast, ok := byID[id]
		// depth 超过技能数说明引用成环，按无法计算处理
		if !ok || failed[id] || depth > len(casts) {
			return 0, false
		}

		start, resolved := 0, false
		if cast.Placement.StartFrame != nil {
			start, resolved = *cast.Placement.StartFrame, true
		} else if prevID := cast.Placement.AfterCastID; prevID != "" {
			if prevStart, ok := resolve(prevID, depth+1); ok {
				if blockFrames, ok := lookupEndAxisBlockFrames(byID[prevID].Source.SkillKey); ok {
					start, resolved = prevStart+blockFrames, true
				}
			}
		}

		if !resolved {
			failed[id] = true
			return 0, false
		}
		starts[id] = start
		return start, true
	}

	for i := range casts {
		resolve(casts[i].ID, 0)
	}
	return starts
}

// endAxisCastType 把新版技能来源映射为旧格式的 type：先看玩家操作 action，
// 旧项目转换来的数据没有 action 时再看技能库分组 skillGroupKey。
// 取消架势（stanceTermination）按战技派发，但不消耗技力。
func endAxisCastType(cast *endAxisSkillCastRaw) (actionType string, noEnergyCost bool) {
	noEnergyCost = cast.Source.SkillGroupKey == "stanceTermination"
	switch cast.Source.Action {
	case "ultimate":
		return "ultimate", noEnergyCost
	case "battleSkill":
		return "skill", noEnergyCost
	case "":
	default:
		return cast.Source.Action, noEnergyCost
	}

	switch cast.Source.SkillGroupKey {
	case "ultimate":
		return "ultimate", noEnergyCost
	case "battleSkill", "enhancedBattleSkill", "stanceTermination":
		return "skill", noEnergyCost
	default:
		return cast.Source.SkillGroupKey, noEnergyCost
	}
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
		if sc.SkippedCasts > 0 {
			maafocus.Print(ctx, i18n.T("autofight.endaxis.scenario_cast_skipped", sc.Name, sc.SkippedCasts))
		}
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
func (t *EndAxisTimeline) FrontAction() *EndAxisAction {
	if !t.started || len(t.queue) == 0 {
		return nil
	}
	if t.queue[0].StartTime > t.currentFrame() {
		return nil
	}

	t.pause()
	head := t.queue[0]
	return &head
}

// PopFrontAction 删除当前队首动作并恢复计时。队列为空或时间轴未启动时为空操作。
func (t *EndAxisTimeline) PopFrontAction() {
	if !t.started || len(t.queue) == 0 {
		return
	}

	popped := t.queue[0]
	t.queue = t.queue[1:]
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

// computeTimelineEndFrame 返回整条时间轴的最晚结束帧：
// 设置了模拟终止线（新版项目格式）时直接使用终止线；否则扫描 scenario 内所有 track 的
// 所有 action（不限制 type），返回 max(startTime + duration)。
// 没有任何 action 时返回 timelineFrameBase，使 ActionFinish 立即可结束。
func computeTimelineEndFrame(sc *timelineScenarioRaw) int {
	if sc.EndFrame != nil {
		return *sc.EndFrame
	}
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
		for _, a := range track.Actions {
			actionType := a.Type
			switch actionType {
			case "battleSkill":
				actionType = "skill"
			case "ultimate", "skill":
			default:
				continue
			}
			out = append(out, EndAxisAction{
				Type:         actionType,
				TrackIdx:     trackIdx,
				StartTime:    a.StartTime,
				Duration:     a.Duration,
				Name:         a.Name,
				ID:           a.ID,
				NoEnergyCost: a.NoEnergyCost,
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
