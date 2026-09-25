package autostockpile

import (
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
)

// LookupAbortReason 返回指定原因键对应的本地化文案。
func LookupAbortReason(reason AbortReason) (string, error) {
	if !isKnownAbortReason(reason) {
		return "", fmt.Errorf("unknown abort reason %q", reason)
	}
	return i18n.T("autostockpile.abort." + string(reason)), nil
}
