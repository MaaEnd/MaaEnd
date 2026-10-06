package autofight

import (
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 战斗决策面板的 i18n key 后缀，文案见 assets/locales/go-service/<lang>.json 的 autofight.decision.*。
const (
	decisionAttack               = "attack"
	decisionCombo                = "combo"
	decisionSkill                = "skill"
	decisionEndSkill             = "end_skill"
	decisionApproach             = "approach"
	decisionLock                 = "lock"
	decisionMoveForward          = "move_forward"
	decisionMoveBack             = "move_back"
	decisionMoveLeft             = "move_left"
	decisionMoveRight            = "move_right"
	decisionBreakPower           = "break_power"
	decisionWaitCombo            = "wait_combo"
	decisionTimelineRetry        = "timeline_retry"
	decisionTimelineSelected     = "timeline_selected"
	decisionTimelineNoMatch      = "timeline_no_match"
	decisionTimelineSkipEndSkill = "timeline_skip_endskill"
	decisionTimelineSkipNoAction = "timeline_skip_no_action"
	decisionPaused               = "paused"
)

// decisionMinInterval 是同一条面板的最小刷新间隔。决策在两三个值之间来回跳动时，
// 每次输出都会在客户端追加一个节点，因此除了内容去重，再限制一下刷新频率。
const decisionMinInterval = 500 * time.Millisecond

// decisionContextSeparator 是次要信息行内部的分隔符。
const decisionContextSeparator = " · "

// 决策面板走 stdout HTML：客户端把每条输出追加成一个节点，模板里的 :has 规则
// 让旧节点自我隐藏（见 assets/locales/go-service/HTML/autofight-decision.html），
// 视觉上就是"当前决策"这一条在原地刷新。
//
// 面板在调用方的循环内单线程使用，不做加锁。
var (
	decisionPendingKey       string
	decisionPendingArgs      []any
	decisionPendingSecondary string
	decisionPending          bool
	decisionLastContent      string
	decisionLastAt           time.Time
)

// setDecision 记录本帧的决策，帧末由 flushDecision 统一输出；同一帧多次调用以后者为准。
func setDecision(key string, args ...any) {
	decisionPendingKey = key
	decisionPendingArgs = args
	decisionPending = true
}

// setDecisionContext 记录随决策一起展示的次要信息（参战人数、锁定状态）。
// 它只是决策变化那一刻的快照，不单独触发刷新——否则战斗循环每帧都会追加节点。
func setDecisionContext(secondary string) {
	decisionPendingSecondary = secondary
}

// flushDecision 输出本帧决策。文案与上一条相同、或距上次输出不足 decisionMinInterval 时跳过，
// 因此一闪而过的状态可能不会出现在面板上——面板表达的是"当前"，不是事件流水。
//
// 每次真正输出时同步写一条 debug 日志：面板内容只走 stdout、不落盘，
// 这条日志是事后复盘（含 issue 日志包）里唯一能还原决策序列的地方。
func flushDecision(ctx *maa.Context) {
	if !decisionPending {
		return
	}
	key, args, secondary := decisionPendingKey, decisionPendingArgs, decisionPendingSecondary
	decisionPendingKey, decisionPendingArgs, decisionPendingSecondary, decisionPending = "", nil, "", false

	content := i18n.T("autofight.decision."+key, args...)
	if content == decisionLastContent || time.Since(decisionLastAt) < decisionMinInterval {
		return
	}
	decisionLastContent = content
	decisionLastAt = time.Now()

	entry := log.Debug().
		Str("component", "AutoFight").
		Str("step", "decision").
		Str("decision", key).
		Str("text", content).
		Str("context", secondary)
	if len(args) > 0 {
		entry = entry.Any("args", args)
	}
	entry.Msg("fight decision changed")

	maafocus.PrintLargeContentTrimNewline(
		i18n.RenderHTML("autofight.decision", map[string]any{
			"Decision":  content,
			"Secondary": secondary,
		}),
	)
}

// decisionContextText 组装面板的次要信息行：参战人数 + 当前锁定状态。
// 未启用自动锁定时不展示锁定状态，避免显示成"未锁定"误导排查。
func decisionContextText(characterCount int, lockTargetEnabled bool) string {
	parts := make([]string, 0, 2)
	if characterCount > 0 {
		parts = append(parts, i18n.T("autofight.decision.context.team", characterCount))
	}
	if lockTargetEnabled {
		if screenAnalyzer.GetEnemyLocked() {
			parts = append(parts, i18n.T("autofight.decision.context.locked"))
		} else {
			parts = append(parts, i18n.T("autofight.decision.context.unlocked"))
		}
	}
	return strings.Join(parts, decisionContextSeparator)
}

// resetDecision 清空面板状态，进入战斗时调用，避免沿用上一场的去重记录。
func resetDecision() {
	decisionPendingKey, decisionPendingArgs, decisionPendingSecondary, decisionPending = "", nil, "", false
	decisionLastContent = ""
	decisionLastAt = time.Time{}
}

// reportDecisionExit 输出收尾块，客户端据此隐藏整块决策面板。
func reportDecisionExit() {
	log.Debug().
		Str("component", "AutoFight").
		Str("step", "decision").
		Str("decision", "exit").
		Msg("fight decision panel closed")
	maafocus.PrintLargeContentTrimNewline(i18n.RenderHTML("autofight.decision_exit", map[string]any{}))
	resetDecision()
}
