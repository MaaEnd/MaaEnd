package schedule

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type scheduleParam struct {
	Task         string `json:"task"`
	IntervalDays int    `json:"interval_days"`
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

// lastRunState is persisted to track the last successful run date for interval mode.
type lastRunState struct {
	Task            string `json:"task"`
	LastSuccessDate string `json:"last_success_date"`
}

// ScheduleAction runs the configured entry task based on scheduling rules:
// - If interval_days > 0, use interval mode (skip if not enough days since last success).
// - Otherwise fall back to weekday mode (skip if today's weekday flag is not enabled).
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

	if days, ok := loadIntervalDaysFromAttach(ctx, arg); ok && days > 0 {
		params.IntervalDays = days
	}

	if params.IntervalDays > 0 {
		return a.runInterval(ctx, params)
	}

	return a.runWeekday(ctx, arg, params)
}

func (a *ScheduleAction) runInterval(ctx *maa.Context, params scheduleParam) bool {
	today := time.Now().Format("2006-01-02")

	lastDate, err := loadLastRunDate(params.Task)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", "ScheduleAction").
			Str("task", params.Task).
			Msg("failed to load last run date, treating as first run")
		lastDate = ""
	}

	if lastDate != "" {
		lastTime, parseErr := time.Parse("2006-01-02", lastDate)
		todayTime, _ := time.Parse("2006-01-02", today)
		if parseErr == nil {
			daysSince := int(todayTime.Sub(lastTime).Hours() / 24)
			if daysSince < params.IntervalDays {
				nextDate := lastTime.AddDate(0, 0, params.IntervalDays).Format("2006-01-02")
				log.Info().
					Str("component", "ScheduleAction").
					Str("task", params.Task).
					Int("interval_days", params.IntervalDays).
					Int("days_since_last", daysSince).
					Str("last_run", lastDate).
					Str("next_allowed", nextDate).
					Msg("interval not met, skip task")
				maafocus.Print(ctx, i18n.T("schedule.skip_interval", daysSince, params.IntervalDays))
				return true
			}
		}
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

	if err := saveLastRunDate(params.Task, today); err != nil {
		log.Warn().
			Err(err).
			Str("component", "ScheduleAction").
			Str("task", params.Task).
			Msg("failed to save last run date")
		// non-fatal: task succeeded, just can't persist
	}

	return true
}

func (a *ScheduleAction) runWeekday(ctx *maa.Context, arg *maa.CustomActionArg, params scheduleParam) bool {
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

func lastRunFilePath(task string) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, task)
	return filepath.Join("debug", "record", "schedule_"+safe+"_last_run.json")
}

func loadLastRunDate(task string) (string, error) {
	path := lastRunFilePath(task)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var state lastRunState
	if err := json.Unmarshal(data, &state); err != nil {
		return "", err
	}
	return state.LastSuccessDate, nil
}

func saveLastRunDate(task string, date string) error {
	path := lastRunFilePath(task)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	state := lastRunState{
		Task:            task,
		LastSuccessDate: date,
	}
	data, err := json.MarshalIndent(state, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
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

// loadIntervalDaysFromAttach reads an optional interval_days override from
// the pipeline node's attach. Returns (0, false) when not present or invalid.
func loadIntervalDaysFromAttach(ctx *maa.Context, arg *maa.CustomActionArg) (int, bool) {
	if ctx == nil || arg == nil {
		return 0, false
	}
	raw, err := ctx.GetNodeJSON(arg.CurrentTaskName)
	if err != nil {
		return 0, false
	}
	var wrapper struct {
		Attach struct {
			IntervalDays any `json:"interval_days"`
		} `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return 0, false
	}
	val := wrapper.Attach.IntervalDays
	switch v := val.(type) {
	case float64:
		return int(v), true
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
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
