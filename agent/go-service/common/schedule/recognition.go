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

// weekdayFlags is read from the configured pipeline node's attach.
type weekdayFlags struct {
	Monday    bool `json:"monday"`
	Tuesday   bool `json:"tuesday"`
	Wednesday bool `json:"wednesday"`
	Thursday  bool `json:"thursday"`
	Friday    bool `json:"friday"`
	Saturday  bool `json:"saturday"`
	Sunday    bool `json:"sunday"`
}

const gameDayBoundaryHour = 4

// gameWeekday returns the weekday by game-day rules: each day starts at 04:00 local time.
func gameWeekday(now time.Time) time.Weekday {
	t := now.Local()
	if t.Hour() < gameDayBoundaryHour {
		t = t.AddDate(0, 0, -1)
	}
	return t.Weekday()
}

// ScheduleRecognition reports a hit only on weekdays enabled in attach.
type ScheduleRecognition struct{}

// Compile-time interface check
var _ maa.CustomRecognitionRunner = &ScheduleRecognition{}

func (r *ScheduleRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil {
		log.Error().
			Str("component", "ScheduleRecognition").
			Msg("got nil context")
		return nil, false
	}
	if arg == nil {
		log.Error().
			Str("component", "ScheduleRecognition").
			Msg("got nil custom recognition arg")
		return nil, false
	}

	node := strings.TrimSpace(arg.CurrentTaskName)
	if node == "" {
		log.Error().
			Str("component", "ScheduleRecognition").
			Msg("ScheduleRecognition requires a current task name")
		return nil, false
	}

	flags, err := loadWeekdayFlagsFromNode(ctx, node)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "ScheduleRecognition").
			Str("node", node).
			Msg("failed to load weekday flags from node attach")
		return nil, false
	}

	weekday := gameWeekday(time.Now())
	weekdayName := i18n.T(weekdayKey(weekday))

	if !isEnabledOn(&flags, weekday) {
		log.Info().
			Str("component", "ScheduleRecognition").
			Str("weekday", weekday.String()).
			Str("node", node).
			Msg("today is not in schedule, skip task")
		maafocus.Print(ctx, i18n.T("schedule.skip_today", weekdayName))
		return nil, false
	}

	detailJSON, _ := json.Marshal(map[string]any{
		"scheduled": true,
		"weekday":   weekday.String(),
	})

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

func loadWeekdayFlagsFromNode(ctx *maa.Context, node string) (weekdayFlags, error) {
	if ctx == nil {
		return weekdayFlags{}, fmt.Errorf("context is nil")
	}
	if node == "" {
		return weekdayFlags{}, fmt.Errorf("node is empty")
	}
	raw, err := ctx.GetNodeJSON(node)
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
