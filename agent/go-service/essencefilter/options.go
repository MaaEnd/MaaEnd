package essencefilter

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func inventoryPreset(opts EssenceFilterOptions) EssenceFilterOptions {
	if !opts.ExportInventory {
		return opts
	}
	return EssenceFilterOptions{
		ExportInventory: true,
		InputLanguage:   opts.InputLanguage,
		FlawlessEssence: true,
		Rarity4Weapon:   true,
		Rarity5Weapon:   true,
		Rarity6Weapon:   true,
	}
}

// collectionPreset forces the read-only collection basis: only flawless essence is
// scanned, and the weapon-oriented selection is ignored because the decision basis
// becomes the whole 840 combination universe.
func collectionPreset(opts EssenceFilterOptions) EssenceFilterOptions {
	return EssenceFilterOptions{
		CollectionMode:          true,
		CollectionKeepMode:      opts.CollectionKeepMode,
		CollectionPhase:         opts.CollectionPhase,
		CollectionDryRun:        opts.CollectionDryRun,
		CollectionDiscardLocked: opts.CollectionDiscardLocked,
		CollectionLockKeepers:   opts.CollectionLockKeepers,
		InputLanguage:           opts.InputLanguage,
		FlawlessEssence:         true,
		Rarity4Weapon:           true,
		Rarity5Weapon:           true,
		Rarity6Weapon:           true,
	}
}

// collectionScanGridOverride returns the grid filter of the read-only scan pass.
//
// 盘点遍**必须**包含已锁定的基质：它们同样属于「已收集」，漏掉会让 840 进度与配额都算错。
// 已标记弃置的照旧跳过。
func collectionScanGridOverride() map[string]any {
	return map[string]any{
		"EssenceGridAdvance": map[string]any{
			"attach": map[string]any{
				"flawless_essence":   true,
				"pure_essence":       false,
				"skip_thumb_lock":    false,
				"skip_thumb_discard": true,
			},
		},
	}
}

// collectionApplyGridOverride returns the grid filter of the apply pass.
//
// discardLocked=true 时已锁定基质照常进入队列（不保留的会被弃置）；
// false 时跳过已锁定基质，完全不动它们。
func collectionApplyGridOverride(discardLocked bool) map[string]any {
	return map[string]any{
		"EssenceGridAdvance": map[string]any{
			"attach": map[string]any{
				"flawless_essence":   true,
				"pure_essence":       false,
				"skip_thumb_lock":    !discardLocked,
				"skip_thumb_discard": true,
			},
		},
	}
}

func inventoryGridOverride() map[string]any {
	return map[string]any{
		"EssenceGridAdvance": map[string]any{
			"attach": map[string]any{
				"flawless_essence":   true,
				"pure_essence":       false,
				"skip_thumb_lock":    false,
				"skip_thumb_discard": true,
			},
		},
	}
}

// matchOptsFromPipeline maps pipeline attach options to the match engine subset.
func matchOptsFromPipeline(opts *EssenceFilterOptions) matchapi.EssenceFilterOptions {
	if opts == nil {
		return matchapi.EssenceFilterOptions{}
	}
	return matchapi.EssenceFilterOptions{
		Rarity6Weapon:            opts.Rarity6Weapon,
		Rarity5Weapon:            opts.Rarity5Weapon,
		Rarity4Weapon:            opts.Rarity4Weapon,
		KeepFuturePromising:      opts.KeepFuturePromising,
		FuturePromisingMinTotal:  opts.FuturePromisingMinTotal,
		LockFuturePromising:      opts.LockFuturePromising,
		KeepSlot3Level3Practical: opts.KeepSlot3Level3Practical,
		Slot3MinLevel:            opts.Slot3MinLevel,
		LockSlot3Practical:       opts.LockSlot3Practical,
		DiscardUnmatched:         opts.DiscardUnmatched,
	}
}

func getOptionsFromAttach(ctx *maa.Context, nodeName string) (*EssenceFilterOptions, error) {
	raw, err := ctx.GetNodeJSON(nodeName)

	if err != nil {
		log.Error().Err(err).Str("node", nodeName).Msg("failed to get options from node")
		return nil, err
	}

	// unmarshal into wrapper struct to extract Attach field
	var wrapper struct {
		Attach EssenceFilterOptions `json:"attach"`
	}

	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		log.Error().Err(err).Str("node", nodeName).Msg("failed to unmarshal options")
		return nil, err
	}

	return &wrapper.Attach, nil
}

func rarityListToString(rarities []int) string {
	switch len(rarities) {
	case 1:
		return strconv.Itoa(rarities[0])
	case 2:
		return i18n.T("essencefilter.rarity_join_2", rarities[0], rarities[1])
	case 3:
		return i18n.T("essencefilter.rarity_join_3", rarities[0], rarities[1], rarities[2])
	case 4:
		return i18n.T("essencefilter.rarity_join_4", rarities[0], rarities[1], rarities[2], rarities[3])
	default:
		return fmt.Sprintf("%d+", len(rarities))
	}
}

func essenceListToString(essenceTypes []string) string {
	return strings.Join(essenceTypes, i18n.Separator())
}

// collectionDryRun reports whether the collection run is a rehearsal.
// 字段缺失（nil）视为预演：标记丢弃不可逆，安全默认应当是「不动任何东西」。
func (o *EssenceFilterOptions) collectionDryRun() bool {
	return o == nil || o.CollectionDryRun == nil || *o.CollectionDryRun
}

// collectionDiscardLocked reports whether the policy also applies to locked essences.
// 字段缺失（nil）视为「不动已锁定」：弃置不可逆，安全默认是不碰。
func (o *EssenceFilterOptions) collectionDiscardLocked() bool {
	return o != nil && o.CollectionDiscardLocked != nil && *o.CollectionDiscardLocked
}

// collectionLockKeepers reports whether keepers should be locked.
// 字段缺失（nil）视为 true —— 锁定是非破坏性操作。
func (o *EssenceFilterOptions) collectionLockKeepers() bool {
	return o == nil || o.CollectionLockKeepers == nil || *o.CollectionLockKeepers
}
