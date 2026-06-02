package achievement

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type TrackerAction struct{}

type PendingRecognition struct{}

type ConsumePendingAction struct{}

type ReportAction struct{}

var (
	_ maa.CustomActionRunner      = &TrackerAction{}
	_ maa.CustomRecognitionRunner = &PendingRecognition{}
	_ maa.CustomActionRunner      = &ConsumePendingAction{}
	_ maa.CustomActionRunner      = &ReportAction{}
)

func (a *TrackerAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	_ = ctx
	if arg == nil {
		log.Error().Str("component", "AchievementTrackerAction").Msg("got nil custom action arg")
		return false
	}

	var params trackerParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "AchievementTrackerAction").
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse custom action param")
		return false
	}
	params.Event = strings.TrimSpace(params.Event)
	params.DedupeKey = strings.TrimSpace(params.DedupeKey)
	if params.Event == "" {
		log.Error().Str("component", "AchievementTrackerAction").Msg("custom_action_param.event is required")
		return false
	}
	if params.Increment <= 0 {
		params.Increment = 1
	}

	if _, err := recordEvent(resolveStoragePathFunc(), params.Event, params.Increment, params.DedupeKey, time.Now()); err != nil {
		log.Error().
			Err(err).
			Str("component", "AchievementTrackerAction").
			Str("event", params.Event).
			Msg("failed to record achievement event")
		return false
	}
	return true
}

func (r *PendingRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	_ = ctx
	if arg == nil {
		log.Error().Str("component", "AchievementPendingRecognition").Msg("got nil custom recognition arg")
		return nil, false
	}

	storage, err := readStorageFile(resolveStoragePathFunc())
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "AchievementPendingRecognition").
			Msg("failed to read achievement storage")
		return nil, false
	}
	ensureStorageDefaults(&storage)
	if len(storage.PendingNotifications) == 0 {
		return nil, false
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: fmt.Sprintf(`{"pending_count":%d}`, len(storage.PendingNotifications)),
	}, true
}

func (a *ConsumePendingAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	_ = ctx
	_ = arg
	if _, err := consumePendingNotifications(resolveStoragePathFunc(), time.Now()); err != nil {
		log.Error().
			Err(err).
			Str("component", "AchievementConsumePendingAction").
			Msg("failed to consume pending achievement notifications")
		return false
	}
	return true
}

func (a *ReportAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	limit := 5
	if arg != nil && strings.TrimSpace(arg.CustomActionParam) != "" {
		var params reportParam
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
			log.Error().
				Err(err).
				Str("component", "AchievementReportAction").
				Str("custom_action_param", arg.CustomActionParam).
				Msg("failed to parse custom action param")
			return false
		}
		if params.Limit > 0 {
			limit = params.Limit
		}
	}

	storage, err := readStorageFile(resolveStoragePathFunc())
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "AchievementReportAction").
			Msg("failed to read achievement storage")
		return false
	}
	maafocus.Print(ctx, buildReport(storage, limit))
	return true
}

func buildReport(storage storageFile, limit int) string {
	ensureStorageDefaults(&storage)
	r := getRules()
	total := len(r)
	unlocked := 0
	for _, rule := range r {
		if achievement, ok := storage.Achievements[rule.ID]; ok && achievement.UnlockedAt != "" {
			unlocked++
		}
	}

	lines := []string{
		i18n.T("achievement.report.summary", unlocked, total),
	}

	recent := storage.RecentUnlocks
	if len(recent) > limit {
		recent = recent[len(recent)-limit:]
	}
	if len(recent) == 0 {
		lines = append(lines, i18n.T("achievement.report.no_unlocks"))
	} else {
		lines = append(lines, i18n.T("achievement.report.recent"))
		for i := len(recent) - 1; i >= 0; i-- {
			lines = append(lines, fmt.Sprintf("- %s", recent[i].Title))
		}
	}

	return strings.Join(lines, "\n")
}
