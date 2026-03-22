package autostockpile

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

type abortReasonCatalog struct {
	ZHCN map[AbortReason]string `json:"zh_cn"`
}

//go:embed abort_reason.json
var abortReasonJSON []byte

var abortReasonCatalogOnce = sync.OnceValues(func() (abortReasonCatalog, error) {
	var catalog abortReasonCatalog
	if err := json.Unmarshal(abortReasonJSON, &catalog); err != nil {
		return abortReasonCatalog{}, fmt.Errorf("parse embedded abort_reason.json: %w", err)
	}

	if len(catalog.ZHCN) == 0 {
		return abortReasonCatalog{}, fmt.Errorf("abort_reason.json missing zh_cn entries")
	}

	for _, reason := range knownAbortReasons {
		message, ok := catalog.ZHCN[reason]
		if !ok {
			return abortReasonCatalog{}, fmt.Errorf("abort_reason.json missing zh_cn for %q", reason)
		}
		if message == "" {
			return abortReasonCatalog{}, fmt.Errorf("abort_reason.json zh_cn for %q is empty", reason)
		}
	}

	for reason := range catalog.ZHCN {
		if !isKnownAbortReason(reason) {
			return abortReasonCatalog{}, fmt.Errorf("abort_reason.json contains unknown key %q", reason)
		}
	}

	return catalog, nil
})

// InitAbortReasonCatalog 初始化并校验中止原因文案表。
func InitAbortReasonCatalog() error {
	_, err := abortReasonCatalogOnce()
	return err
}

// ValidateAbortReason 校验给定原因键是否合法。
func ValidateAbortReason(reason AbortReason) error {
	if !isKnownAbortReason(reason) {
		return fmt.Errorf("unknown abort reason %q", reason)
	}

	return nil
}

// LookupAbortReasonZHCN 返回指定原因键对应的中文文案。
func LookupAbortReasonZHCN(reason AbortReason) (string, error) {
	if err := ValidateAbortReason(reason); err != nil {
		return "", err
	}

	catalog, err := abortReasonCatalogOnce()
	if err != nil {
		return "", err
	}

	message, ok := catalog.ZHCN[reason]
	if !ok {
		return "", fmt.Errorf("abort reason %q missing zh_cn message", reason)
	}

	return message, nil
}
