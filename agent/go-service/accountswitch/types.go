package accountswitch

import (
	"encoding/json"
	"strings"
)

const (
	opFocus = "focus"
	opClose = "close"
	opClick = "click"

	windowMain  = "main"
	windowForm  = "form"
	windowCombo = "combo"
)

type windowSelector struct {
	ClassName  string `json:"class_name"`
	WindowName string `json:"window_name"`
}

type windowActionParam struct {
	Op       string `json:"op"`
	Window   string `json:"window"`
	Optional bool   `json:"optional"`
}

var defaultWindowSelectors = map[string]windowSelector{
	windowMain: {
		ClassName:  "UnityWndClass",
		WindowName: "Endfield",
	},
	windowForm: {
		ClassName:  "Qt5158QWindowToolSaveBits",
		WindowName: "Form",
	},
	windowCombo: {
		ClassName:  "Qt5158QWindowToolSaveBits",
		WindowName: "Endfield",
	},
}

func parseWindowActionParam(raw string) (windowActionParam, error) {
	param := windowActionParam{
		Op:     opFocus,
		Window: windowMain,
	}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &param); err != nil {
			return windowActionParam{}, err
		}
	}

	param.Op = strings.ToLower(strings.TrimSpace(param.Op))
	if param.Op == "" {
		param.Op = opFocus
	}
	param.Window = strings.ToLower(strings.TrimSpace(param.Window))
	if param.Window == "" {
		param.Window = windowMain
	}

	return param, nil
}
