package accountswitch

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	emailPatternActionName = "AccountSwitchEmailPatternAction"

	clickDropdownNode  = "__AccountSwitchClickDropdown"
	selectAccountNode  = "__AccountSwitchOCRSelectAccount"
	checkAccountNode   = "__AccountSwitchCheckAccount"
	verifySelectedNode = "__AccountSwitchVerifySelected"

	maskedEmailPattern = `(?i).*\*{5}\S*@\S+.*`
)

var _ maa.CustomActionRunner = &EmailPatternAction{}

type emailPatternParam struct {
	Email string `json:"email"`
}

// EmailPatternAction converts a full email address to the masked form shown by
// the international client and applies task-scoped OCR patterns for selection.
type EmailPatternAction struct{}

func (a *EmailPatternAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		log.Error().
			Str("component", emailPatternActionName).
			Msg("got nil context")
		return false
	}
	if arg == nil {
		log.Error().
			Str("component", emailPatternActionName).
			Msg("got nil custom action arg")
		return false
	}

	var param emailPatternParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().
			Err(err).
			Str("component", emailPatternActionName).
			Msg("failed to parse params")
		return false
	}

	override, err := buildEmailPatternOverride(param.Email)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", emailPatternActionName).
			Msg("invalid email param")
		return false
	}
	if err := ctx.OverridePipeline(override); err != nil {
		log.Error().
			Err(err).
			Str("component", emailPatternActionName).
			Msg("failed to override account switch OCR patterns")
		return false
	}

	return true
}

func buildEmailPatternOverride(email string) (map[string]any, error) {
	maskedEmail, err := maskEmail(email)
	if err != nil {
		return nil, err
	}

	targetPattern := `(?i).*` + regexp.QuoteMeta(maskedEmail) + `.*`
	return map[string]any{
		clickDropdownNode:  expectedPatternOverride(maskedEmailPattern),
		selectAccountNode:  expectedPatternOverride(targetPattern),
		checkAccountNode:   expectedPatternOverride(maskedEmailPattern),
		verifySelectedNode: expectedPatternOverride(targetPattern),
	}, nil
}

func expectedPatternOverride(pattern string) map[string]any {
	return map[string]any{
		"recognition": map[string]any{
			"param": map[string]any{
				"expected": []string{pattern},
			},
		},
	}
}

func maskEmail(email string) (string, error) {
	local, domain, err := splitEmail(email)
	if err != nil {
		return "", err
	}

	var prefix, suffix string
	switch len(local) {
	case 1:
		prefix = local[:1]
	case 2, 3:
		prefix = local[:1]
		suffix = local[len(local)-1:]
	case 4:
		prefix = local[:2]
		suffix = local[len(local)-1:]
	default:
		prefix = local[:2]
		suffix = local[len(local)-2:]
	}

	return prefix + "*****" + suffix + "@" + domain, nil
}

func splitEmail(email string) (string, string, error) {
	if strings.Count(email, "@") != 1 {
		return "", "", fmt.Errorf("email must contain exactly one @")
	}
	for i := 0; i < len(email); i++ {
		if email[i] < '!' || email[i] > '~' {
			return "", "", fmt.Errorf("email must contain only printable ASCII characters")
		}
	}

	local, domain, _ := strings.Cut(email, "@")
	if local == "" || domain == "" {
		return "", "", fmt.Errorf("email local part and domain must not be empty")
	}
	return local, domain, nil
}
