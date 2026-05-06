package schedule

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

type scheduleParam struct {
	Task string `json:"task"`
}

// weekdayFlags is read from the pipeline node's attach for the Schedule action node.
type weekdayFlags struct {
	Monday    bool `json:"monday"`
	Tuesday   bool `json:"tuesday"`
	Wednesday bool `json:"wednesday"`
	Thursday  bool `json:"thursday"`
	Friday    bool `json:"friday"`
	Saturday  bool `json:"saturday"`
	Sunday    bool `json:"sunday"`
}

// ScheduleAction runs the configured entry task only on weekdays where the
// matching flag is enabled, and emits a localized notice on skipped days.
type ScheduleAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &ScheduleAction{}

func (a *ScheduleAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		log.Error().
			Str("component", "ScheduleAction").
			Msg("got nil context")
		return false
	}
	if arg == nil {
		log.Error().
			Str("component", "ScheduleAction").
			Msg("got nil custom action arg")
		return false
	}

	var params scheduleParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "ScheduleAction").
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse custom action param")
		return false
	}

	if params.Task == "" {
		log.Error().
			Str("component", "ScheduleAction").
			Msg("Schedule requires non-empty custom_action_param.task")
		return false
	}

	flags, err := loadWeekdayFlagsFromAttach(ctx, arg)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "ScheduleAction").
			Str("node", strings.TrimSpace(arg.CurrentTaskName)).
			Msg("failed to load weekday flags from node attach")
		return false
	}

	weekday := time.Now().Weekday()
	weekdayName := i18n.T(weekdayKey(weekday))

	if !isEnabledOn(&flags, weekday) {
		log.Info().
			Str("component", "ScheduleAction").
			Str("weekday", weekday.String()).
			Str("task", params.Task).
			Msg("today is not in schedule, skip task")
		maafocus.Print(ctx, i18n.T("schedule.skip_today", weekdayName))
		return true
	}

	detail, err := ctx.RunTask(params.Task)
	if err != nil || detail == nil {
		log.Error().
			Err(err).
			Str("component", "ScheduleAction").
			Str("task", params.Task).
			Msg("failed to run scheduled task")
		return false
	}

	if !detail.Status.Success() {
		return false
	}

	return true
}

func loadWeekdayFlagsFromAttach(ctx *maa.Context, arg *maa.CustomActionArg) (weekdayFlags, error) {
	if ctx == nil || arg == nil {
		return weekdayFlags{}, fmt.Errorf("context or arg is nil")
	}
	raw, err := ctx.GetNodeJSON(arg.CurrentTaskName)
	if err != nil {
		return weekdayFlags{}, err
	}
	var wrapper struct {
		Attach weekdayFlags `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return weekdayFlags{}, err
	}
	return wrapper.Attach, nil
}

// isEnabledOn reports whether attach enables the given weekday.
func isEnabledOn(p *weekdayFlags, w time.Weekday) bool {
	switch w {
	case time.Sunday:
		return p.Sunday
	case time.Monday:
		return p.Monday
	case time.Tuesday:
		return p.Tuesday
	case time.Wednesday:
		return p.Wednesday
	case time.Thursday:
		return p.Thursday
	case time.Friday:
		return p.Friday
	case time.Saturday:
		return p.Saturday
	}
	return false
}

// weekdayKey maps a time.Weekday to its i18n message key.
func weekdayKey(w time.Weekday) string {
	switch w {
	case time.Sunday:
		return "schedule.weekday_sunday"
	case time.Monday:
		return "schedule.weekday_monday"
	case time.Tuesday:
		return "schedule.weekday_tuesday"
	case time.Wednesday:
		return "schedule.weekday_wednesday"
	case time.Thursday:
		return "schedule.weekday_thursday"
	case time.Friday:
		return "schedule.weekday_friday"
	case time.Saturday:
		return "schedule.weekday_saturday"
	}
	return ""
}
