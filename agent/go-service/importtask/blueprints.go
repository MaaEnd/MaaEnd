package importtask

import (
	"encoding/json"
	"regexp"
	"sync"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

var (
	blueprintCodes []string
	blueprintMutex sync.Mutex
	blueprintRegex = regexp.MustCompile(`EF[a-zA-Z0-9]+`)
)

func parseBlueprintCodes(text string) []string {
	matches := blueprintRegex.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}

	// 去重
	seen := make(map[string]bool)
	result := make([]string, 0, len(matches))
	for _, code := range matches {
		if !seen[code] {
			seen[code] = true
			result = append(result, code)
		}
	}
	return result
}

type ImportBluePrintsInitTextAction struct{}

func (a *ImportBluePrintsInitTextAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	blueprintMutex.Lock()
	blueprintCodes = nil
	blueprintMutex.Unlock()

	var params struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse CustomActionParam")
		return false
	}

	text := params.Text
	log.Info().Str("text", text).Msg("Input blueprint text")

	codes := parseBlueprintCodes(text)
	if len(codes) == 0 {
		log.Warn().Msg("No blueprint codes found in text")
		return false
	}

	blueprintMutex.Lock()
	blueprintCodes = codes
	blueprintMutex.Unlock()

	log.Info().Int("count", len(codes)).Strs("codes", codes).Msg("Parsed blueprint codes")

	return true
}

type ImportBluePrintsFinishAction struct{}

func (a *ImportBluePrintsFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	blueprintMutex.Lock()
	remaining := len(blueprintCodes)
	blueprintMutex.Unlock()

	if remaining == 0 {
		log.Info().Msg("All blueprint codes processed")
		ctx.GetTasker().PostStop()
		return true
	}

	log.Info().Int("remaining", remaining).Msg("Blueprint codes remaining")
	return true
}

type ImportBluePrintsEnterCodeAction struct{}

func (a *ImportBluePrintsEnterCodeAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	blueprintMutex.Lock()
	if len(blueprintCodes) == 0 {
		blueprintMutex.Unlock()
		log.Warn().Msg("No more blueprint codes to process")
		return false
	}

	code := blueprintCodes[0]
	blueprintCodes = blueprintCodes[1:]
	remaining := len(blueprintCodes)
	blueprintMutex.Unlock()

	log.Info().Str("code", code).Int("remaining", remaining).Msg("Processing blueprint code")
	ctx.GetTasker().GetController().PostInputText(code)

	return true
}
