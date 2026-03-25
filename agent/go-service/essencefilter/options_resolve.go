package essencefilter

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type EssenceFilterOptionsPatch struct {
	Rarity6Weapon   *bool `json:"rarity6_weapon"`
	Rarity5Weapon   *bool `json:"rarity5_weapon"`
	Rarity4Weapon   *bool `json:"rarity4_weapon"`
	FlawlessEssence *bool `json:"flawless_essence"`
	PureEssence     *bool `json:"pure_essence"`

	KeepFuturePromising     *bool `json:"keep_future_promising"`
	FuturePromisingMinTotal *int  `json:"future_promising_min_total"`
	LockFuturePromising     *bool `json:"lock_future_promising"`

	KeepSlot3Level3Practical *bool `json:"keep_slot3_level3_practical"`
	Slot3MinLevel            *int  `json:"slot3_min_level"`
	LockSlot3Practical       *bool `json:"lock_slot3_practical"`

	DiscardUnmatched       *bool   `json:"discard_unmatched"`
	ExportCalculatorScript *bool   `json:"export_calculator_script"`
	SkipLockedRow          *bool   `json:"skip_locked_row"`
	InputLanguage          *string `json:"input_language"`
}

func defaultEssenceFilterOptions() EssenceFilterOptions {
	return EssenceFilterOptions{
		Rarity6Weapon:            true,
		Rarity5Weapon:            true,
		Rarity4Weapon:            false,
		FlawlessEssence:          true,
		PureEssence:              false,
		KeepFuturePromising:      false,
		FuturePromisingMinTotal:  6,
		LockFuturePromising:      false,
		KeepSlot3Level3Practical: false,
		Slot3MinLevel:            3,
		LockSlot3Practical:       false,
		DiscardUnmatched:         false,
		ExportCalculatorScript:   false,
		SkipLockedRow:            true,
		InputLanguage:            "CN",
	}
}

func resolveOptions(ctx *maa.Context, arg *maa.CustomActionArg, legacyNodeNames ...string) (*EssenceFilterOptions, error) {
	opts := defaultEssenceFilterOptions()

	// 1) 优先读取 custom_action_param，适合“通用接口”调用
	if patch, err := decodeOptionsPatch(arg.CustomActionParam); err != nil {
		return nil, fmt.Errorf("parse custom_action_param: %w", err)
	} else {
		applyOptionsPatch(&opts, patch)
	}

	// 2) 再读取当前节点 attach，适合原 task / 原 pipeline 的兼容模式
	if patch, err := loadOptionsPatchFromNodeJSON(ctx, safeTaskName(arg)); err != nil {
		return nil, fmt.Errorf("load attach from current node: %w", err)
	} else {
		applyOptionsPatch(&opts, patch)
	}

	// 3) 最后按明确传入的额外节点叠加（调用方自行决定是否传）
	for _, nodeName := range legacyNodeNames {
		if strings.TrimSpace(nodeName) == "" {
			continue
		}
		patch, err := loadOptionsPatchFromNodeJSON(ctx, nodeName)
		if err != nil {
			return nil, fmt.Errorf("load attach from %s: %w", nodeName, err)
		}
		applyOptionsPatch(&opts, patch)
	}

	return &opts, nil
}

func decodeOptionsPatch(raw string) (EssenceFilterOptionsPatch, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return EssenceFilterOptionsPatch{}, nil
	}

	// 常规情况：custom_action_param 直接就是 JSON 对象
	var patch EssenceFilterOptionsPatch
	if err := json.Unmarshal([]byte(raw), &patch); err == nil {
		return patch, nil
	}

	return EssenceFilterOptionsPatch{}, fmt.Errorf("invalid essence filter options JSON")
}

func loadOptionsPatchFromNodeJSON(ctx *maa.Context, nodeName string) (EssenceFilterOptionsPatch, error) {
	if ctx == nil || strings.TrimSpace(nodeName) == "" {
		return EssenceFilterOptionsPatch{}, nil
	}

	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return EssenceFilterOptionsPatch{}, err
	}

	var wrapper struct {
		Attach EssenceFilterOptionsPatch `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return EssenceFilterOptionsPatch{}, err
	}

	return wrapper.Attach, nil
}

func applyOptionsPatch(dst *EssenceFilterOptions, patch EssenceFilterOptionsPatch) {
	if patch.Rarity6Weapon != nil {
		dst.Rarity6Weapon = *patch.Rarity6Weapon
	}
	if patch.Rarity5Weapon != nil {
		dst.Rarity5Weapon = *patch.Rarity5Weapon
	}
	if patch.Rarity4Weapon != nil {
		dst.Rarity4Weapon = *patch.Rarity4Weapon
	}
	if patch.FlawlessEssence != nil {
		dst.FlawlessEssence = *patch.FlawlessEssence
	}
	if patch.PureEssence != nil {
		dst.PureEssence = *patch.PureEssence
	}

	if patch.KeepFuturePromising != nil {
		dst.KeepFuturePromising = *patch.KeepFuturePromising
	}
	if patch.FuturePromisingMinTotal != nil {
		dst.FuturePromisingMinTotal = *patch.FuturePromisingMinTotal
	}
	if patch.LockFuturePromising != nil {
		dst.LockFuturePromising = *patch.LockFuturePromising
	}

	if patch.KeepSlot3Level3Practical != nil {
		dst.KeepSlot3Level3Practical = *patch.KeepSlot3Level3Practical
	}
	if patch.Slot3MinLevel != nil {
		dst.Slot3MinLevel = *patch.Slot3MinLevel
	}
	if patch.LockSlot3Practical != nil {
		dst.LockSlot3Practical = *patch.LockSlot3Practical
	}

	if patch.DiscardUnmatched != nil {
		dst.DiscardUnmatched = *patch.DiscardUnmatched
	}
	if patch.ExportCalculatorScript != nil {
		dst.ExportCalculatorScript = *patch.ExportCalculatorScript
	}
	if patch.SkipLockedRow != nil {
		dst.SkipLockedRow = *patch.SkipLockedRow
	}
	if patch.InputLanguage != nil {
		dst.InputLanguage = *patch.InputLanguage
	}
}

func safeTaskName(arg *maa.CustomActionArg) string {
	if arg == nil {
		return ""
	}
	return arg.CurrentTaskName
}
