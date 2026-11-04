package countdown

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/maa-framework-go/v4"
)

const (
	defStepMessage         = `countdown.message.step`
	defDoneMessage         = `countdown.message.done`
	defStopMessage         = `countdown.message.stop`
	defDurationFormatHMS   = `countdown.format.hms`
	defDurationFormatMS    = `countdown.format.ms`
	defDurationFormatS     = `countdown.format.s`
	defLongDurationStepMax = 20 * time.Minute
)

const (
	DurationTypeMs int = iota
	DurationTypeSecond
	DurationTypeMinute
)

const (
	DurationTypeHMS1 = iota + 10 //xhxmxs
	DurationTypeHMS2             //hh:mm:ss/mm:ss
)

const (
	ComplexModeOff = iota
	ComplexModeSkip
	ComplexModeFull
)

var (
	mu         sync.RWMutex
	startTimes map[string]time.Time
)

type params struct {
	UseOCRResult      bool            `json:"useOCRResult,omitempty"`
	Duration          json.RawMessage `json:"duration,omitempty"`
	duration          time.Duration
	DurationType      int         `json:"duration_type,omitempty"`
	DurationFormatHMS string      `json:"duration_format_hms,omitempty"`
	DurationFormatMS  string      `json:"duration_format_ms,omitempty"`
	DurationFormatS   string      `json:"duration_format_s,omitempty"`
	StepMessage       string      `json:"step_message,omitempty"`
	DoneMessage       string      `json:"done_message,omitempty"`
	StopMessage       string      `json:"stop_message,omitempty"`
	ComplexConf       complexConf `json:"complex_conf,omitempty"`
}

type complexConf struct {
	//0:不启用复杂模式；1:启用，Duration超过LongDurationMax时不执行；2:启用，Duration超过LongDurationMax时最多等待LongDurationMax
	ComplexMode         int             `json:"complex_mode,omitempty"`
	LongDurationMax     json.RawMessage `json:"long_duration_max,omitempty"` //最大总等待时间
	longDurationMax     time.Duration
	LongDurationType    int `json:"long_duration_type,omitempty"`
	LongDurationStepMax int `json:"long_duration_step_max,omitempty"` //单次最大等待时间，单位：秒
	longDurationStepMax time.Duration
	ResetStartTime      bool `json:"reset_start_time,omitempty"` //是否清除累积等待时间
}

func init() {
	startTimes = make(map[string]time.Time)
}

func (action *Action) parseParams(arg *maa.CustomActionArg) (*params, error) {
	// 构建带有默认值的实例
	p := &params{
		StepMessage:       defStepMessage,
		DoneMessage:       defDoneMessage,
		StopMessage:       defStopMessage,
		DurationFormatHMS: defDurationFormatHMS,
		DurationFormatMS:  defDurationFormatMS,
		DurationFormatS:   defDurationFormatS,
		ComplexConf: complexConf{
			LongDurationStepMax: int(defLongDurationStepMax.Seconds()),
		},
	}

	// 反序列化参数
	if err := json.Unmarshal([]byte(arg.CustomActionParam), p); err != nil {
		return nil, fmt.Errorf("failed to parse param: %w", err)
	}

	if arg.CurrentTaskName == "" {
		// 没有节点名时，可能是直接通过run的方式来运行或者一些边界情况，不使用复杂模式
		p.ComplexConf.ComplexMode = ComplexModeOff
	}

	var err error

	// 解析复杂模式的duration
	switch p.ComplexConf.ComplexMode {
	case ComplexModeOff:
	case ComplexModeSkip, ComplexModeFull:
		p.ComplexConf.longDurationMax, err = parseDuration(p.ComplexConf.LongDurationMax, p.ComplexConf.LongDurationType)
		if err != nil {
			return nil, fmt.Errorf("failed to parse duration, field 'complex_conf.long_duration_max': %w", err)
		}
		p.ComplexConf.longDurationStepMax = time.Duration(p.ComplexConf.LongDurationStepMax) * time.Second
		if p.ComplexConf.longDurationStepMax <= 0 {
			p.ComplexConf.longDurationStepMax = defLongDurationStepMax
		}
	}

	// 解析本次等待时长
	dRaw := p.Duration
	if p.UseOCRResult {
		dRaw, err = transOCRText2JSONRaw(bestOCRText(arg), p.DurationType)
		if err != nil {
			return nil, err
		}
	}
	p.duration, err = parseDuration(dRaw, p.DurationType)
	if err != nil {
		return nil, fmt.Errorf("failed to parse duration, field 'duration': %w", err)
	}

	// 复杂模式的非跳过模式时，还需要处理本次倒计时时长
	if p.ComplexConf.ComplexMode > ComplexModeOff {
		// 根据参数决定是否清空累积等待时长
		mu.Lock()
		if p.ComplexConf.ResetStartTime {
			delete(startTimes, arg.CurrentTaskName)
		}
		if _, ok := startTimes[arg.CurrentTaskName]; !ok {
			startTimes[arg.CurrentTaskName] = time.Now()
		}
		mu.Unlock()

		// skip模式下，总等待时间超时，直接跳过
		if p.ComplexConf.ComplexMode == ComplexModeSkip && p.duration > p.ComplexConf.longDurationMax {
			return nil, nil
		}

		mu.RLock()
		// 本次时长不能比总最大时长的剩余时长长
		st := startTimes[arg.CurrentTaskName]
		diff := max(st.Add(p.ComplexConf.longDurationMax).Sub(time.Now()), 0)
		mu.RUnlock()
		// 本次时长不能比单次最大时长长
		p.duration = min(p.duration, p.ComplexConf.longDurationStepMax, diff)
	}

	// 读取几个format的i18n文本
	p.DurationFormatHMS = i18n.T(p.DurationFormatHMS)
	p.DurationFormatMS = i18n.T(p.DurationFormatMS)
	p.DurationFormatS = i18n.T(p.DurationFormatS)

	return p, nil
}

func parseDuration(raw json.RawMessage, durationType int) (time.Duration, error) {
	def := time.Duration(0)
	if len(raw) == 0 {
		return def, errors.New("duration must be greater than zero")
	}
	var duration time.Duration
	unit := time.Nanosecond
	if durationType < DurationTypeHMS1 {
		// 常规模式（指定分钟/秒/毫秒）
		var value int
		if err := json.Unmarshal(raw, &value); err != nil {
			return def, fmt.Errorf("failed to parse duration value: %w", err)
		}

		duration = time.Duration(value)
		switch durationType {
		case DurationTypeMs:
			unit = time.Millisecond
		case DurationTypeSecond:
			unit = time.Second
		case DurationTypeMinute:
			unit = time.Minute
		default:
			return def, errors.New("invalid duration type")
		}
	} else {
		// 文本模式
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return def, fmt.Errorf("failed to parse duration value: %w", err)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return def, errors.New("invalid duration value")
		}

		switch durationType {
		case DurationTypeHMS1:
		case DurationTypeHMS2:
			// golang的duration原生支持xhxmxs模式，直接将hh:mm:ss模式转换为xhxmxs模式即可直接解析
			value = strings.ReplaceAll(value, "：", ":")
			arr := strings.Split(value, ":")
			switch len(arr) {
			case 2:
				// mm:ss
				value = arr[0] + "m" + arr[1] + "s"
			case 3:
				// hh:mm:ss
				value = arr[0] + "h" + arr[1] + "m" + arr[2] + "s"
			default:
				return def, fmt.Errorf("invalid duration %q: expected hh:mm:ss or mm:ss", value)
			}
		default:
			return def, fmt.Errorf("unsupported duration type: %d", durationType)
		}

		var err error
		duration, err = time.ParseDuration(value)
		if err != nil {
			return def, fmt.Errorf("failed to parse duration value: %w", err)
		}
	}

	// 超过24小时直接报错
	if duration > time.Hour*24/unit {
		return def, errors.New("duration must not exceed 24 hours")
	}
	// 不大于0的也直接报错
	if duration <= 0 {
		return def, errors.New("duration must be greater than zero")
	}
	return duration * unit, nil
}

func bestOCRText(arg *maa.CustomActionArg) string {
	if arg.RecognitionDetail == nil {
		return ""
	}
	detail := arg.RecognitionDetail
	if detail == nil || detail.Results == nil {
		return ""
	}
	if detail.Results.Best != nil {
		if o, ok := detail.Results.Best.AsOCR(); ok {
			return strings.TrimSpace(o.Text)
		}
	}
	for _, r := range detail.Results.Filtered {
		if r == nil {
			continue
		}
		if o, ok := r.AsOCR(); ok {
			return strings.TrimSpace(o.Text)
		}
	}
	return ""
}

func transOCRText2JSONRaw(text string, durationType int) (json.RawMessage, error) {
	if text == "" {
		return nil, errors.New("OCR duration text is empty")
	}

	var value any
	switch durationType {
	case DurationTypeMs, DurationTypeSecond, DurationTypeMinute:
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid OCR duration integer %q: %w", text, err)
		}
		value = number
	case DurationTypeHMS1, DurationTypeHMS2:
		value = text
	default:
		return nil, fmt.Errorf("unsupported duration type: %d", durationType)
	}

	// OCR 是普通文本，需要编码成合法 JSON 后再交给公共解析方法。
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode OCR duration: %w", err)
	}

	return raw, nil
}
