package importtask

import (
	"encoding/json"
	"regexp"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

// blueprintCodes 蓝图码队列
var blueprintCodes []string

func parseBlueprintCodes(text string) []string {
	re := regexp.MustCompile(`EF[a-zA-Z0-9]+`)
	return re.FindAllString(text, -1)
}

type ImportBluePrintsInitTextAction struct{}

func (a *ImportBluePrintsInitTextAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse CustomActionParam")
		return false
	}

	text := params.Text
	log.Info().Str("text", text).Msg("Input blueprint text")

	// 解析蓝图码
	codes := parseBlueprintCodes(text)
	if len(codes) == 0 {
		log.Warn().Msg("No blueprint codes found in text")
		return false
	}

	blueprintCodes = codes
	log.Info().Int("count", len(codes)).Strs("codes", codes).Msg("Parsed blueprint codes")

	return true
}

type ImportBluePrintsFinishAction struct{}

func (a *ImportBluePrintsFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if len(blueprintCodes) == 0 {
		log.Info().Msg("All blueprint codes processed")
		ctx.GetTasker().PostStop()
		return true
	}

	log.Info().Int("remaining", len(blueprintCodes)).Msg("Blueprint codes remaining")
	return true
}

type ImportBluePrintsEnterCodeAction struct{}

func (a *ImportBluePrintsEnterCodeAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if len(blueprintCodes) == 0 {
		log.Warn().Msg("No more blueprint codes to process")
		return false
	}

	// 取出第一个 code
	code := blueprintCodes[0]
	blueprintCodes = blueprintCodes[1:]

	log.Info().Str("code", code).Int("remaining", len(blueprintCodes)).Msg("Processing blueprint code")
	ctx.GetTasker().GetController().PostInputText(code)
	return true
}
