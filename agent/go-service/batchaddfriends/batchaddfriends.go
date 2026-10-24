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
	DefaultMaxCount int `json:"default_max_count"`
	MaxFailStreak   int `json:"max_fail_streak"`
}

var (
	// defaultConfig 用于在 pipeline 侧未传参时提供安全默认值。
	defaultConfig = batchAddConfig{
		DefaultMaxCount: 20,
		MaxFailStreak:   5,
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
	strangersProcessed int
	strangersMaxCount  int
}

// BatchAddFriendsAction 是批量添加好友任务的入口动作：解析参数，决定分支，并回写 pipeline 的动态参数/跳转。
func (a *BatchAddFriendsAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	cfg := defaultConfig
	var params struct {
		UidList  string      `json:"uid_list"`
		MaxCount interface{} `json:"max_count"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("[BatchAddFriends]参数解析失败")
		return false
	}
	maxCount := parseMaxCount(params.MaxCount, cfg.DefaultMaxCount)
	uidList := strings.TrimSpace(params.UidList)

	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[BatchAddFriends]无法获取控制器")
		return false
	}

	if len(uidList) > 0 {
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
		mode:               "strangers",
		strangersProcessed: 0,
		strangersMaxCount:  maxCount,
	}
	_ = ctx.OverridePipeline(map[string]any{
		"BatchAddFriendsAddStrangersLoop": map[string]any{
			"max_hit": maxCount,
		},
	})
	// 立即跳转到添加陌生人分支。
	_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
		{Name: "BatchAddFriendsStrangersStart"},
	})
	log.Info().Int("maxCount", maxCount).Msg("[BatchAddFriends]陌生人模式开始")
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

func (a *BatchAddFriendsStrangersOnAddAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	state.strangersProcessed++
	maafocus.Print(
		ctx,
		i18n.T("batchaddfriends.strangers_progress", state.strangersProcessed, state.strangersMaxCount),
	)
	return true
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
