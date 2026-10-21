package batchaddfriends

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type batchAddConfig struct {
	DefaultMaxCount    int `json:"default_max_count"`
	DefaultMaxAttempts int `json:"default_max_attempts"`
	MaxFailStreak      int `json:"max_fail_streak"`
}

var (
	// defaultConfig 用于在 pipeline 侧未传参时提供安全默认值。
	defaultConfig = batchAddConfig{
		DefaultMaxCount:    20,
		DefaultMaxAttempts: 30,
		MaxFailStreak:      5,
	}

	// state 保存当前 BatchAddFriends 的运行状态。
	state batchAddState
)

type BatchAddFriendsAction struct{}
type BatchAddFriendsUIDLoopTopAction struct{}
type BatchAddFriendsUIDEnterAction struct{}
type BatchAddFriendsUIDOnAddAction struct{}
type BatchAddFriendsUIDOnEmptyAction struct{}
type BatchAddFriendsUIDFinishAction struct{}
type BatchAddFriendsStrangersOnAddAction struct{}
type BatchAddFriendsStrangersFinishAction struct{}
type BatchAddFriendsFriendListFullAction struct{}

// Compile-time interface checks
var (
	_ maa.CustomActionRunner = &BatchAddFriendsAction{}
	_ maa.CustomActionRunner = &BatchAddFriendsUIDLoopTopAction{}
	_ maa.CustomActionRunner = &BatchAddFriendsUIDEnterAction{}
	_ maa.CustomActionRunner = &BatchAddFriendsUIDOnAddAction{}
	_ maa.CustomActionRunner = &BatchAddFriendsUIDOnEmptyAction{}
	_ maa.CustomActionRunner = &BatchAddFriendsUIDFinishAction{}
	_ maa.CustomActionRunner = &BatchAddFriendsStrangersOnAddAction{}
	_ maa.CustomActionRunner = &BatchAddFriendsStrangersFinishAction{}
	_ maa.CustomActionRunner = &BatchAddFriendsFriendListFullAction{}
)

type batchAddState struct {
	mode string

	// UID 列表模式
	uidQueue         []string
	uidTotal         int
	uidProcessed     int
	uidSuccess       int
	uidFail          int
	uidFailStreak    int
	uidMaxFailStreak int
	uidCurrent       string

	// 添加陌生人模式
	strangersProcessed   int
	strangersMaxCount    int
	strangersAttempts    int
	strangersMaxAttempts int
}

// BatchAddFriendsAction 是批量添加好友任务的入口动作：解析参数，决定分支，并回写 pipeline 的动态参数/跳转。
func (a *BatchAddFriendsAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	cfg := defaultConfig
	var params struct {
		Mode        string      `json:"mode"`
		UidList     string      `json:"uid_list"`
		MaxCount    interface{} `json:"max_count"`
		MaxAttempts interface{} `json:"max_attempts"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("[BatchAddFriends]参数解析失败")
		return false
	}
	maxCount := parseMaxCount(params.MaxCount, cfg.DefaultMaxCount)
	maxAttempts := parseMaxCount(params.MaxAttempts, cfg.DefaultMaxAttempts)
	uids := splitUIDs(params.UidList)

	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[BatchAddFriends]无法获取控制器")
		return false
	}

	if params.Mode == "uid" {
		if len(uids) == 0 {
			// 选了 UID 模式却没填列表：直接结束，不回退到陌生人模式
			log.Warn().Msg("[BatchAddFriends]UID 列表为空，直接结束")
			_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
				{Name: "BatchAddFriendsUIDEnd"},
			})
			return true
		}
		if maxCount > 0 && len(uids) > maxCount {
			uids = uids[:maxCount]
		}
		state = batchAddState{
			mode:             "uid",
			uidQueue:         uids,
			uidTotal:         len(uids),
			uidMaxFailStreak: cfg.MaxFailStreak,
		}
		_ = ctx.OverridePipeline(map[string]any{
			"BatchAddFriendsUIDLoopCounter": map[string]any{
				"max_hit": len(state.uidQueue),
			},
		})
		// 立即跳转到 UID 分支。
		_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "BatchAddFriendsUIDLoopTop"},
		})
		log.Info().
			Int("total", state.uidTotal).
			Int("maxFailStreak", state.uidMaxFailStreak).
			Msg("[BatchAddFriends]UID 列表模式开始")
		return true
	}

	state = batchAddState{
		mode:                 "strangers",
		strangersProcessed:   0,
		strangersMaxCount:    maxCount,
		strangersMaxAttempts: maxAttempts,
	}
	// 黑名单是包级状态，上一次任务结束后会残留；行的 y 只在同一批列表内有效，
	// 跨任务复用会误跳过新玩家。
	resetRejectedRows()
	consecutiveEmptyBatches = 0
	// max_hit 沿用「添加数量」：只有添加成功才会再次命中 AddStrangersLoop，
	// 所以它天然等价于「加够 N 个就停」。尝试次数另在 Go 里独立计数。
	// +1：流程进循环时 S.Start 会先命中一次 AddStrangersLoop，那次并不产生添加，
	// 所以想要「加够 maxCount 个」需要多给一轮配额，否则实际只能加 maxCount-1 个。
	_ = ctx.OverridePipeline(map[string]any{
		"BatchAddFriendsAddStrangersLoop": map[string]any{
			"max_hit": maxCount + 1,
		},
	})
	// 先跑一次残留菜单清理，再进陌生人分支。
	// ResetMenu 只在列表存在残留二级菜单时命中，未命中会自然落到 StrangersStart。
	_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
		{Name: "BatchAddFriendsStrangersResetMenu"},
		{Name: "BatchAddFriendsStrangersStart"},
	})
	log.Info().
		Int("maxCount", maxCount).
		Int("maxAttempts", maxAttempts).
		Msg("[BatchAddFriends]陌生人模式开始")
	return true
}

// BatchAddFriendsUIDLoopTopAction 是 UID 分支入口：根据队列是否为空决定继续或结束分支。
func (a *BatchAddFriendsUIDLoopTopAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// UID 队列为空则结束 UID 分支。
	if len(state.uidQueue) == 0 {
		_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "BatchAddFriendsUIDEnd"},
		})
		return true
	}
	// 否则继续处理下一个 UID。
	_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
		{Name: "BatchAddFriendsUIDLoopCounter"},
	})
	return true
}

func (a *BatchAddFriendsUIDEnterAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if len(state.uidQueue) == 0 {
		ctx.GetTasker().PostStop()
		return true
	}
	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[BatchAddFriends]无法获取控制器")
		return false
	}

	uid := state.uidQueue[0]
	state.uidQueue = state.uidQueue[1:]
	state.uidCurrent = uid
	controller.PostInputText(uid).Wait()
	log.Debug().
		Int("index", state.uidProcessed+1).
		Int("total", state.uidTotal).
		Int("remaining", len(state.uidQueue)).
		Str("uid", uid).
		Msg("[BatchAddFriends]开始处理 UID")
	return true
}

func (a *BatchAddFriendsUIDOnAddAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	state.uidProcessed++
	state.uidSuccess++
	state.uidFailStreak = 0
	log.Info().
		Int("processed", state.uidProcessed).
		Int("total", state.uidTotal).
		Int("success", state.uidSuccess).
		Int("fail", state.uidFail).
		Str("uid", state.uidCurrent).
		Msg("[BatchAddFriends]已点击添加好友")
	maafocus.Print(
		ctx,
		i18n.T("batchaddfriends.uid_sent", state.uidCurrent, state.uidSuccess, state.uidTotal),
	)
	return true
}

func (a *BatchAddFriendsUIDOnEmptyAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	state.uidProcessed++
	state.uidFail++
	state.uidFailStreak++
	log.Warn().
		Int("processed", state.uidProcessed).
		Int("total", state.uidTotal).
		Int("success", state.uidSuccess).
		Int("fail", state.uidFail).
		Int("failStreak", state.uidFailStreak).
		Str("uid", state.uidCurrent).
		Msg("[BatchAddFriends]未搜索到相关玩家")
	if state.uidMaxFailStreak > 0 && state.uidFailStreak >= state.uidMaxFailStreak {
		log.Error().
			Int("maxFailStreak", state.uidMaxFailStreak).
			Msg("[BatchAddFriends]连续失败次数过多，终止 UID 列表模式")
		_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "BatchAddFriendsUIDEnd"},
		})
	}
	return true
}

func (a *BatchAddFriendsUIDFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().
		Int("total", state.uidTotal).
		Int("processed", state.uidProcessed).
		Int("success", state.uidSuccess).
		Int("fail", state.uidFail).
		Msg("[BatchAddFriends]UID 列表模式结束")
	if state.mode == "uid" {
		state = batchAddState{}
	}
	return true
}

func (a *BatchAddFriendsStrangersOnAddAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	state.strangersProcessed++
	state.strangersAttempts++
	maafocus.Print(
		ctx,
		i18n.T("batchaddfriends.strangers_progress", state.strangersProcessed, state.strangersMaxCount),
	)
	endIfAttemptsExceeded(ctx, arg.CurrentTaskName)
	return true
}

// endIfAttemptsExceeded 在「判定过的玩家数」达到上限时立即结束陌生人模式。
// 不论上方「添加数量」是否达到，只要累计判定（添加成功 + 被拒绝）达到该次数就停，
// 避免一直换批判定拖长任务时间。
func endIfAttemptsExceeded(ctx *maa.Context, node string) {
	if state.strangersMaxAttempts <= 0 || state.strangersAttempts < state.strangersMaxAttempts {
		return
	}
	log.Info().
		Int("attempts", state.strangersAttempts).
		Int("maxAttempts", state.strangersMaxAttempts).
		Int("processed", state.strangersProcessed).
		Msg("[BatchAddFriends]尝试判定达到上限，结束陌生人模式")
	maafocus.Print(ctx, i18n.T("batchaddfriends.strangers_attempts_limit", state.strangersAttempts))
	_ = ctx.OverrideNext(node, []maa.NextItem{
		{Name: "BatchAddFriendsStrangersEnd"},
	})
}

func (a *BatchAddFriendsStrangersFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Int("maxCount", state.strangersMaxCount).Msg("[BatchAddFriends]陌生人模式结束")
	if state.mode == "strangers" {
		state = batchAddState{}
	}
	return true
}

func (a *BatchAddFriendsFriendListFullAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Warn().Msg("[BatchAddFriends]好友列表已满，提前结束")
	if state.mode == "uid" {
		_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "BatchAddFriendsUIDEnd"},
		})
		return true
	}
	_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
		{Name: "BatchAddFriendsStrangersEnd"},
	})
	return true
}

// parseMaxCount 解析 max_count 参数并回退到默认值（JSON 数字通常会反序列化成 float64）。
func parseMaxCount(v interface{}, def int) int {
	// max_count 来自 JSON 反序列化，数字通常会被解码为 float64。
	switch val := v.(type) {
	case float64:
		if int(val) > 0 {
			return int(val)
		}
	case int:
		if val > 0 {
			return val
		}
	case string:
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func splitUIDs(raw string) []string {
	// 按空白字符与中文顿号"、"拆分 UID。
	re := regexp.MustCompile(`[、\s]+`)
	parts := re.Split(strings.TrimSpace(raw), -1)
	uids := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			uids = append(uids, s)
		}
	}
	return uids
}
