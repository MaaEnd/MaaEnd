package batchaddfriends

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type batchAddConfig struct {
	DefaultMaxCount int `json:"default_max_count"`
	MaxFailStreak   int `json:"max_fail_streak"`
}

var (
	defaultConfig = batchAddConfig{
		DefaultMaxCount: 20,
		MaxFailStreak:   5,
	}
	lastChangeBatchAt int64
)

type BatchAddFriendsAction struct{}
type BatchAddFriendsChangeBatchAction struct{}

var (
	_ maa.CustomActionRunner = &BatchAddFriendsAction{}
	_ maa.CustomActionRunner = &BatchAddFriendsChangeBatchAction{}
)

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

	added := 0
	failStreak := 0

	if len(uids) > 0 {
		for _, uid := range uids {
			if added >= maxCount {
				break
			}
			if failStreak >= cfg.MaxFailStreak {
				log.Error().Msg("[BatchAddFriends]连续失败次数过多，终止任务")
				return false
			}
			ok := addByUID(ctx, controller, uid)
			if ok {
				added++
				failStreak = 0
			} else {
				failStreak++
			}
		}
	} else {
		override := map[string]interface{}{
			"BatchAddFriendsAddStrangersLoop": map[string]interface{}{
				"max_hit": maxCount,
			},
		}
		ctx.RunTask("BatchAddFriendsStrangersStart", override)
		added = maxCount
	}

	log.Info().Int("added", added).Msg("[BatchAddFriends]任务结束")
	return true
}

func (a *BatchAddFriendsChangeBatchAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[BatchAddFriends]无法获取控制器")
		return false
	}
	now := time.Now()
	last := atomic.LoadInt64(&lastChangeBatchAt)
	if last > 0 && now.Sub(time.Unix(0, last)) < 10*time.Second {
		return true
	}
	atomic.StoreInt64(&lastChangeBatchAt, now.UnixNano())
	controller.PostClick(1173, 663)
	log.Info().Msg("[BatchAddFriends]换一批")
	return true
}

func parseMaxCount(v interface{}, def int) int {
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

func addByUID(ctx *maa.Context, controller *maa.Controller, uid string) bool {
	if strings.TrimSpace(uid) == "" {
		return false
	}
	ctx.RunTask("BatchAddFriendsFocusSearchBox", nil)
	controller.PostInputText(uid).Wait()
	ctx.RunTask("BatchAddFriendsSearchFlow", nil)
	log.Info().Str("uid", uid).Msg("[BatchAddFriends]已处理搜索结果")
	return true
}
