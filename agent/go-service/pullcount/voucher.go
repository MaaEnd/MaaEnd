package pullcount

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
)

// --- Voucher Config And Classification --- //

type voucherConfig struct {
	Vouchers []voucherDef `json:"vouchers"`
}

type voucherDef struct {
	Name      string   `json:"name"`
	Names     []string `json:"names"`
	PullValue int      `json:"pull_value"`
	PoolScope string   `json:"pool_scope"`
}

type voucherMatch struct {
	CanonicalName string
	DisplayName   string
	PullValue     int
	PoolScope     string
	Quantity      int
	Pulls         int
}

type voucherSummary struct {
	CurrentOnlyPulls int
	CarryToNextPulls int
	NextOnlyPulls    int
	Matches          []voucherMatch
}

// loadVoucherConfig reads voucher definitions from assets data.
func loadVoucherConfig(path string) (*voucherConfig, error) {
	var config voucherConfig
	if err := resource.ReadJsonResource(path, &config); err != nil {
		return nil, err
	}
	for i := range config.Vouchers {
		if err := validateVoucherDef(config.Vouchers[i]); err != nil {
			return nil, fmt.Errorf("voucher %d: %w", i, err)
		}
	}
	if _, err := buildVoucherIndex(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

// validateVoucherDef checks a voucher definition before it is used for totals.
func validateVoucherDef(def voucherDef) error {
	if strings.TrimSpace(def.Name) == "" && len(def.Names) == 0 {
		return fmt.Errorf("name or names is required")
	}
	if def.PullValue != 1 && def.PullValue != 10 {
		return fmt.Errorf("pull_value must be 1 or 10")
	}
	switch def.PoolScope {
	case "current_only", "carry_to_next", "next_only", "ignore":
		return nil
	default:
		return fmt.Errorf("pool_scope must be current_only, carry_to_next, next_only, or ignore")
	}
}

// summarizeVouchers classifies scanned items and totals pull values by pool scope.
func summarizeVouchers(scanned []scannedVoucher, config *voucherConfig) (voucherSummary, error) {
	index, err := buildVoucherIndex(config)
	if err != nil {
		return voucherSummary{}, err
	}
	summary := voucherSummary{}

	for _, item := range scanned {
		if item.Name == "" || item.Quantity <= 0 {
			continue
		}
		def, ok := index[normalizeName(item.Name)]
		if !ok {
			continue
		}
		pulls := item.Quantity * def.PullValue
		if def.PoolScope == "ignore" {
			pulls = 0
		}
		match := voucherMatch{
			CanonicalName: def.Name,
			DisplayName:   item.Name,
			PullValue:     def.PullValue,
			PoolScope:     def.PoolScope,
			Quantity:      item.Quantity,
			Pulls:         pulls,
		}
		summary.Matches = append(summary.Matches, match)
		switch def.PoolScope {
		case "current_only":
			summary.CurrentOnlyPulls += pulls
		case "carry_to_next":
			summary.CarryToNextPulls += pulls
		case "next_only":
			summary.NextOnlyPulls += pulls
		}
	}

	sort.Slice(summary.Matches, func(i, j int) bool {
		if summary.Matches[i].PoolScope != summary.Matches[j].PoolScope {
			return summary.Matches[i].PoolScope < summary.Matches[j].PoolScope
		}
		return summary.Matches[i].DisplayName < summary.Matches[j].DisplayName
	})
	return summary, nil
}

// buildVoucherIndex maps every configured alias to its voucher definition.
func buildVoucherIndex(config *voucherConfig) (map[string]voucherDef, error) {
	index := make(map[string]voucherDef)
	if config == nil {
		return index, nil
	}
	owners := make(map[string]int)
	for i, def := range config.Vouchers {
		aliases := append([]string{def.Name}, def.Names...)
		for _, alias := range aliases {
			key := normalizeName(alias)
			if key == "" {
				continue
			}
			if owner, exists := owners[key]; exists {
				if owner == i {
					continue
				}
				return nil, fmt.Errorf(
					"duplicate voucher alias %q normalized as %q for voucher %d (%s) and voucher %d (%s)",
					alias,
					key,
					owner,
					voucherDefDisplayName(config.Vouchers[owner]),
					i,
					voucherDefDisplayName(def),
				)
			}
			owners[key] = i
			index[key] = def
		}
	}
	return index, nil
}

// voucherDefDisplayName returns a stable label for config validation errors.
func voucherDefDisplayName(def voucherDef) string {
	if strings.TrimSpace(def.Name) != "" {
		return def.Name
	}
	for _, name := range def.Names {
		if strings.TrimSpace(name) != "" {
			return name
		}
	}
	return "<unnamed>"
}

// normalizeName strips OCR noise for exact item-name matching.
func normalizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '[', ']', '|', '(', ')', '-', '_', '.', ',', '、', '·', '/', '\\', '：', ':', '；', ';', '。':
			continue
		}
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
