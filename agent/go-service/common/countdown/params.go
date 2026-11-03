package countdown

import (
	"encoding/json"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	defStepMessage          = `countdown.message.step`
	defDoneMessage          = `countdown.message.done`
	defStopMessage          = `countdown.message.stop`
	defDurationFormatMinute = `countdown.format.minute` //需要i18n
	defDurationFormatSec    = `countdown.format.sec`
)

const (
	DurationTypeMs int = iota
	DurationTypeSec
	DurationTypeMinute
)

type params struct {
	DurationValue        int           `json:"duration"`
	DurationType         int           `json:"duration_type,omitempty"`
	Duration             time.Duration `json:"-"`
	DurationFormatMinute string        `json:"duration_format_minute,omitempty"`
	DurationFormatSec    string        `json:"duration_format_sec,omitempty"`
	StepMessage          string        `json:"step_message,omitempty"`
	DoneMessage          string        `json:"done_message,omitempty"`
	StopMessage          string        `json:"stop_message,omitempty"`
}

func (action *Action) parseParams(arg *maa.CustomActionArg) *params {
	p := &params{
		StepMessage:          defStepMessage,
		DoneMessage:          defDoneMessage,
		StopMessage:          defStopMessage,
		DurationFormatMinute: defDurationFormatMinute,
		DurationFormatSec:    defDurationFormatSec,
	}

	if err := json.Unmarshal([]byte(arg.CustomActionParam), p); err != nil {
		log.Error().
			Err(err).
			Str("component", "CountDown").
			Str("param", arg.CustomActionParam).
			Msg("parse param failed")
		return nil
	}

	switch p.DurationType {
	case DurationTypeMs:
		p.Duration *= time.Millisecond
	case DurationTypeSec:
		p.Duration *= time.Second
	case DurationTypeMinute:
		p.Duration *= time.Minute
	default:
		log.Error().
			Str("component", "CountDown").
			Str("param", p.StepMessage).
			Msg("invalid duration type")
		return nil
	}

	if p.Duration > time.Hour {
		log.Error().
			Str("component", "CountDown").
			Str("param", p.StepMessage).
			Msg("duration too big (must be greater than or equal to 1h)")
		return nil
	}
	if p.Duration == 0 {
		log.Error().
			Str("component", "CountDown").
			Str("param", p.StepMessage).
			Msg("duration cannot be zero")
		return nil
	}

	p.DurationFormatMinute = i18n.T(p.DurationFormatMinute)
	p.DurationFormatSec = i18n.T(p.DurationFormatSec)

	return p
}
