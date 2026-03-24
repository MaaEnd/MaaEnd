package autostockpile

import (
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
)

// ValidateAbortReason 校验给定原因键是否合法。
func ValidateAbortReason(reason AbortReason) error {
	if !isKnownAbortReason(reason) {
		return fmt.Errorf("unknown abort reason %q", reason)
	}

	return nil
}

// LookupAbortReason 返回指定原因键对应的本地化文案。
func LookupAbortReason(reason AbortReason) (string, error) {
	if err := ValidateAbortReason(reason); err != nil {
		return "", err
	}
	return i18n.T("autostockpile.abort." + string(reason)), nil
}
