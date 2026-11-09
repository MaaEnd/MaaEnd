package batchaddfriends

import (
	"encoding/json"
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

type (
	BatchAddFriendsAction                struct{}
	BatchAddFriendsUIDOnAddAction        struct{}
	BatchAddFriendsUIDOnEmptyAction      struct{}
	BatchAddFriendsUIDFinishAction       struct{}
	BatchAddFriendsStrangersOnAddAction  struct{}
	BatchAddFriendsStrangersFinishAction struct{}
	BatchAddFriendsFriendListFullAction  struct{}
)

// Compile-time interface checks
var (
	_ maa.CustomActionRunner = &BatchAddFriendsAction{}
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
	uidProcessed     int
	uidSuccess       int
	uidFail          int
	uidFailStreak    int
	uidMaxFailStreak int
	uidMaxCount      int

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
	uidList := strings.TrimSpace(params.UidList)

	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[BatchAddFriends]无法获取控制器")
		return false
	}

	if params.Mode == "uid" {
		if uidList == "" {
			// 选了 UID 模式却没填列表：直接结束，不回退到陌生人模式
			log.Warn().Msg("[BatchAddFriends]UID 列表为空，直接结束")
			_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
				{Name: "BatchAddFriendsUIDEnd"},
			})
			return true
		}
		state = batchAddState{
			mode:             "uid",
			uidMaxFailStreak: cfg.MaxFailStreak,
			uidMaxCount:      maxCount,
		}
		// 立即跳转到 UID 分支。
		_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "BatchAddFriendsUIDInit"},
		})
		log.Info().
			Int("maxCount", maxCount).
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
	// 拒绝和换批走的是 Scan，不会回到 Loop，所以 Loop 的命中次数就等于添加人数。
	// 尝试次数另在 Go 里独立计数。
	_ = ctx.OverridePipeline(map[string]any{
		"BatchAddFriendsAddStrangersLoop": map[string]any{
			"max_hit": maxCount,
		},
	})
	// 先跑一次残留菜单清理，再进陌生人分支。
	// ResetMenuProbe 只在列表存在残留二级菜单时命中，未命中会自然落到 StrangersStart。
	_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
		{Name: "BatchAddFriendsStrangersResetMenuProbe"},
		{Name: "BatchAddFriendsStrangersStart"},
	})
	log.Info().
		Int("maxCount", maxCount).
		Int("maxAttempts", maxAttempts).
		Msg("[BatchAddFriends]陌生人模式开始")
	return true
}

func (a *BatchAddFriendsUIDOnAddAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	state.uidProcessed++
	state.uidSuccess++
	state.uidFailStreak = 0
	uid, total := readAttach(ctx)
	log.Info().
		Int("processed", state.uidProcessed).
		Int("total", total).
		Int("success", state.uidSuccess).
		Int("fail", state.uidFail).
		Str("uid", uid).
		Msg("[BatchAddFriends]已点击添加好友")
	maafocus.Print(
		ctx,
		i18n.T("batchaddfriends.uid_sent", uid, state.uidSuccess, total),
	)
	if state.uidProcessed >= state.uidMaxCount {
		_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "BatchAddFriendsUIDEnd"},
		})
	}
	return true
}

func (a *BatchAddFriendsUIDOnEmptyAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	state.uidProcessed++
	state.uidFail++
	state.uidFailStreak++
	uid, total := readAttach(ctx)
	log.Warn().
		Int("processed", state.uidProcessed).
		Int("total", total).
		Int("success", state.uidSuccess).
		Int("fail", state.uidFail).
		Int("failStreak", state.uidFailStreak).
		Str("uid", uid).
		Msg("[BatchAddFriends]未搜索到相关玩家")
	if state.uidMaxFailStreak > 0 && state.uidFailStreak >= state.uidMaxFailStreak {
		log.Error().
			Int("maxFailStreak", state.uidMaxFailStreak).
			Msg("[BatchAddFriends]连续失败次数过多，终止 UID 列表模式")
		_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "BatchAddFriendsUIDEnd"},
		})
		return true
	}
	if state.uidProcessed >= state.uidMaxCount {
		_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "BatchAddFriendsUIDEnd"},
		})
	}
	return true
}

func (a *BatchAddFriendsUIDFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	_, total := readAttach(ctx)
	log.Info().
		Int("total", total).
		Int("processed", state.uidProcessed).
		Int("success", state.uidSuccess).
		Int("fail", state.uidFail).
		Msg("[BatchAddFriends]UID 列表模式结束")
	if state.mode == "uid" {
		state = batchAddState{}
	}
	return true
}

func readAttach(ctx *maa.Context) (current string, total int) {
	var w struct {
		Attach struct {
			Current string `json:"current"`
			Total   int    `json:"total"`
		} `json:"attach"`
	}
	raw, err := ctx.GetNodeJSON("TextInput")
	if err == nil {
		err = json.Unmarshal([]byte(raw), &w)
	}
	if err != nil {
		log.Error().Err(err).Msg("[BatchAddFriends]读取输入进度失败")
		return "", 0
	}
	return w.Attach.Current, w.Attach.Total
}

// BatchAddFriendsStrangersOnAddAction 记一次陌生人添加成功，输出进度，并检查尝试次数上限。
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

// BatchAddFriendsStrangersFinishAction 输出陌生人模式结束日志，并重置任务状态。
func (a *BatchAddFriendsStrangersFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Int("maxCount", state.strangersMaxCount).Msg("[BatchAddFriends]陌生人模式结束")
	if state.mode == "strangers" {
		state = batchAddState{}
	}
	return true
}

// BatchAddFriendsFriendListFullAction 好友列表已满时的收尾：提示后按当前模式结束任务。
func (a *BatchAddFriendsFriendListFullAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Warn().Msg("[BatchAddFriends]好友列表已满，提前结束")
	maafocus.Print(ctx, i18n.T("batchaddfriends.friend_list_full"))
	if state.mode == "uid" {
		_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "BatchAddFriendsUIDEnd"},
		})
		return true
	}
	// 好友列表已满：提示后直接结束，不做界面收尾（与其它任务一致）。
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
