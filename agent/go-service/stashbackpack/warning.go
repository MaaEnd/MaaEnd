package stashbackpack

import (
	"encoding/json"
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	warningDuplicateFull           = "duplicate_full"
	warningMissingFullSnapshot     = "missing_full_snapshot"
	warningMissingRetrieveSnapshot = "missing_retrieve_snapshot"
	warningUnsupportedPlatform     = "unsupported_platform"
)

type warningActionParam struct {
	Reason string `json:"reason"`
}

// WarningAction reports a recoverable StashBackpack precondition failure and lets Pipeline skip safely.
type WarningAction struct{}

var _ maa.CustomActionRunner = &WarningAction{}

// Run prints the localized warning selected by Pipeline.
func (a *WarningAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", componentName).Msg("warning action received nil context or arg")
		return false
	}
	key, err := warningMessageKey(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to resolve warning action params")
		return false
	}
	maafocus.Print(ctx, i18n.T(key))
	return true
}

func warningMessageKey(raw string) (string, error) {
	var param warningActionParam
	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		return "", fmt.Errorf("parse warning params: %w", err)
	}
	switch param.Reason {
	case warningDuplicateFull:
		return "stashbackpack.warning.duplicate_full", nil
	case warningMissingFullSnapshot:
		return "stashbackpack.warning.missing_full_snapshot", nil
	case warningMissingRetrieveSnapshot:
		return "stashbackpack.warning.missing_retrieve_snapshot", nil
	case warningUnsupportedPlatform:
		return "stashbackpack.warning.unsupported_platform", nil
	default:
		return "", fmt.Errorf("unsupported warning reason %q", param.Reason)
	}
}
