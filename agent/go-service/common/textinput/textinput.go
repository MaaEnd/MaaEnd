package textinput

import (
	"encoding/json"
	"regexp"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentName = "TextInput"

var splitRegex = regexp.MustCompile(`[,，;；、\s]+`)

var state textInputState

type textInputState struct {
	queue []string
	total int
}

// InitAction 解析文本列表并重建输入队列，由调用方在任务启动时执行一次
type InitAction struct{}

// InputAction 弹出队列中下一段文本，输入当前聚焦的输入框
type InputAction struct{}

// ExhaustedRecognition 在输入队列耗尽时命中
type ExhaustedRecognition struct{}

var (
	_ maa.CustomActionRunner      = &InitAction{}
	_ maa.CustomActionRunner      = &InputAction{}
	_ maa.CustomRecognitionRunner = &ExhaustedRecognition{}
)

type initParam struct {
	Texts string `json:"texts"`
}

func (a *InitAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params initParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName+".InitAction").
			Msg("failed to parse params")
		return false
	}

	parts := splitRegex.Split(params.Texts, -1)
	seen := make(map[string]struct{}, len(parts))
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		text := strings.TrimSpace(part)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		texts = append(texts, text)
	}

	state = textInputState{
		queue: texts,
		total: len(texts),
	}
	if len(texts) == 0 {
		log.Warn().
			Str("component", componentName+".InitAction").
			Msg("text list is empty")
		return true
	}
	log.Info().
		Int("total", state.total).
		Str("component", componentName+".InitAction").
		Msg("input queue initialized")
	return true
}

func (a *InputAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if len(state.queue) == 0 {
		log.Error().
			Str("component", componentName+".InputAction").
			Msg("text queue is empty")
		return false
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return false
	}
	controller := tasker.GetController()
	if controller == nil {
		return false
	}

	text := state.queue[0]
	state.queue = state.queue[1:]
	if job := controller.PostInputText(text).Wait(); job == nil || !job.Success() {
		log.Warn().
			Str("component", componentName+".InputAction").
			Msg("controller rejected text input, falling back to key-by-key input")
		// 部分控制器不支持发送字符串 所以靠按键输入 ascii 码
		for _, r := range text {
			upper := r >= 'A' && r <= 'Z'
			if r >= 'a' && r <= 'z' {
				r -= 'a' - 'A'
			}
			if (r < '0' || r > '9') && (r < 'A' || r > 'Z') {
				log.Error().
					Str("component", componentName+".InputAction").
					Msg("key fallback only supports ascii digits and letters")
				return false
			}
			if upper {
				// shift
				controller.PostKeyDown(0x10).Wait()
			}
			if job := controller.PostClickKey(int32(r)).Wait(); job == nil || !job.Success() {
				if upper {
					controller.PostKeyUp(0x10).Wait()
				}
				log.Error().
					Str("component", componentName+".InputAction").
					Msg("failed to click key")
				return false
			}
			if upper {
				controller.PostKeyUp(0x10).Wait()
			}
		}
	}
	_ = ctx.OverridePipeline(map[string]any{
		arg.CurrentTaskName: map[string]any{"attach": map[string]any{"current": text, "total": state.total}},
	})
	log.Debug().
		Str("text", text).
		Int("remaining", len(state.queue)).
		Int("total", state.total).
		Str("component", componentName+".InputAction").
		Msg("input text")
	return true
}

func (r *ExhaustedRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if len(state.queue) > 0 {
		return nil, false
	}
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: "queue empty",
	}, true
}
