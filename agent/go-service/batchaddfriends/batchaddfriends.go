package batchaddfriends

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

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

type BatchAddFriendsAction struct{}
type BatchAddFriendsUIDLoopTopAction struct{}
type BatchAddFriendsUIDEnterAction struct{}
type BatchAddFriendsUIDOnAddAction struct{}
type BatchAddFriendsUIDOnEmptyAction struct{}
type BatchAddFriendsUIDFinishAction struct{}
type BatchAddFriendsStrangersOnAddAction struct{}
type BatchAddFriendsStrangersFinishAction struct{}
type BatchAddFriendsFriendListFullAction struct{}

var (
	_ maa.CustomActionRunner = &BatchAddFriendsAction{}
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
	uids := splitUIDs(params.UidList)

	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[BatchAddFriends]无法获取控制器")
		return false
	}

	if len(uids) > 0 {
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
	maafocus.NodeActionStarting(
		ctx,
		fmt.Sprintf("UID %s：已发送好友申请（%d/%d）", state.uidCurrent, state.uidSuccess, state.uidTotal),
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
	maafocus.NodeActionStarting(
		ctx,
		fmt.Sprintf("添加好友进度 [%d/%d]", state.strangersProcessed, state.strangersMaxCount),
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

func splitUIDs(raw string) []string {
	// 按空白字符与中文顿号“、”拆分 UID。
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
