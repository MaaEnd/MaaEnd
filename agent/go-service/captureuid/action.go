package captureuid

import (
	"encoding/json"
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type captureUidParam struct {
	UseCache            *bool  `json:"use_cache,omitempty"`
	StayOnCurrentScreen *bool  `json:"stay_on_current_screen,omitempty"`
	AllowUnknown        *bool  `json:"allow_unknown,omitempty"`
	ClearCache          *bool  `json:"clear_cache,omitempty"`
	OutputType          string `json:"output_type,omitempty"`
}

// CaptureUidAction captures or clears the current player's UID for Pipeline callers.
type CaptureUidAction struct{}

var _ maa.CustomActionRunner = &CaptureUidAction{}

const accountIdentityNode = "MapNavigatorAccountIdentity"

// publishAccountIdentity 将当前账号的伪匿名标识写入 Resource 级隐藏节点，供独立的
// cpp-algo 进程读取。原始 UID 仍只留在本进程内存中，跨进程传递的只有加盐哈希。
func publishAccountIdentity(ctx *maa.Context, accountID string) error {
	if ctx == nil || ctx.GetTasker() == nil {
		return fmt.Errorf("nil context or tasker")
	}
	resource := ctx.GetTasker().GetResource()
	if resource == nil {
		return fmt.Errorf("nil resource")
	}
	return resource.OverridePipeline(map[string]any{
		accountIdentityNode: map[string]any{
			"attach": map[string]any{
				"account_id": accountID,
			},
		},
	})
}

func (a *CaptureUidAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || ctx.GetTasker() == nil {
		log.Error().Str("component", component).Msg("CaptureUid: nil context or tasker")
		return false
	}

	ctrl := ctx.GetTasker().GetController()
	if ctrl == nil {
		log.Error().Str("component", component).Msg("CaptureUid: nil controller")
		return false
	}

	useCache := true
	stayOnCurrentScreen := true
	allowUnknown := true
	clearCache := false
	outputType := OutputTypeHashed

	if arg != nil && arg.CustomActionParam != "" {
		var params captureUidParam
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
			log.Error().Err(err).Str("component", component).Str("param", arg.CustomActionParam).
				Msg("CaptureUid: failed to parse custom_action_param")
			return false
		}
		if params.UseCache != nil {
			useCache = *params.UseCache
		}
		if params.StayOnCurrentScreen != nil {
			stayOnCurrentScreen = *params.StayOnCurrentScreen
		}
		if params.AllowUnknown != nil {
			allowUnknown = *params.AllowUnknown
		}
		if params.ClearCache != nil {
			clearCache = *params.ClearCache
		}
		normalized, err := normalizeOutputType(params.OutputType)
		if err != nil {
			log.Error().Err(err).Str("component", component).Str("param", arg.CustomActionParam).
				Msg("CaptureUid: invalid output_type")
			return false
		}
		outputType = normalized
	}

	if clearCache {
		ClearCache()
		if err := publishAccountIdentity(ctx, ""); err != nil {
			log.Error().Err(err).Str("component", component).Msg("CaptureUid: failed to clear account identity")
			return false
		}
		return true
	}

	uid, err := Capture(ctx, ctrl, useCache, stayOnCurrentScreen, allowUnknown, outputType)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("CaptureUid: capture failed")
		return false
	}

	accountID := ""
	if uid != "unknown" {
		accountID = GetCachedUID(OutputTypeHashed)
	}
	if err := publishAccountIdentity(ctx, accountID); err != nil {
		log.Error().Err(err).Str("component", component).Msg("CaptureUid: failed to publish account identity")
		return false
	}

	log.Info().Str("component", component).Str("uid", safeUIDForLog(uid, outputType)).
		Str("account_id", accountID).
		Bool("use_cache", useCache).
		Bool("stay_on_current_screen", stayOnCurrentScreen).
		Bool("allow_unknown", allowUnknown).
		Str("output_type", string(outputType)).
		Msg("CaptureUid: done")
	return true
}
