package cultivationcalc

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/ims"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentEvaluateCultivationBundle = "EvaluateCultivationBundle"

const (
	modeCapacity = "capacity"
	modeDeficit  = "deficit"

	costKeyAdvancedMaterial = "ADVANCED_MATERIAL"
	costKeyCombatEXP        = "COMBAT_RECORD_EXP"
	costKeyCognitiveEXP     = "COGNITIVE_CARRIER_EXP"
	costKeyWeaponEXP        = "WEAPON_EXP"
)

var _ maa.CustomActionRunner = &EvaluateCultivationBundle{}

const (
	partLevel       = "level"
	partBasicAttack = "basic_attack"
	partSkill       = "skill"
	partCombo       = "combo"
	partUltimate    = "ultimate"
	partTrust       = "trust"
	partTalent      = "talent"
	partBase        = "base"
	partWeapon      = "weapon"
)

var advancedMaterialIDs = []string{
	"TRIPHASIC_NANOFLAKE",
	"QUADRANT_FITTING_FLUID",
	"TACHYON_SCREENING_LATTICE",
	"D96_STEEL_SAMPLE_4",
	"METADIASTIMA_PHOTOEMISSION_TUBE",
}

// defaultCultivationCosts is the per-part material table (IMS item IDs / synthetic keys).
// Skill-like parts share the same unit cost.
var defaultCultivationCosts = map[string]map[string]int{
	partLevel: {
		"T_CREDS":               537620,
		costKeyCombatEXP:        747060,
		costKeyCognitiveEXP:     1045180,
		"PROTOSET":              60,
		"PROTODISK":             33,
		costKeyAdvancedMaterial: 20,
	},
	partBasicAttack: skillUnitCost(),
	partSkill:       skillUnitCost(),
	partCombo:       skillUnitCost(),
	partUltimate:    skillUnitCost(),
	partTrust: {
		"T_CREDS":     20800,
		"PROTOHEDRON": 30,
		"PROTOPRISM":  15,
	},
	partTalent: {
		"T_CREDS":     45000,
		"PROTOHEDRON": 28,
		"PROTOPRISM":  100,
	},
	partBase: {
		"T_CREDS":     32600,
		"PROTOHEDRON": 32,
		"PROTOPRISM":  18,
	},
	partWeapon: {
		"T_CREDS":               467090,
		costKeyWeaponEXP:        2524080,
		costKeyAdvancedMaterial: 16,
		"CAST_DIE":              23,
		"HEAVY_CAST_DIE":        50,
	},
}

func skillUnitCost() map[string]int {
	return map[string]int{
		"T_CREDS":               172200,
		costKeyAdvancedMaterial: 58,
		"PROTOHEDRON":           118,
		"PROTOPRISM":            82,
		"MARK_OF_PERSEVERANCE":  6,
	}
}

type evaluateCultivationBundleParam struct {
	BundleCosts map[string]map[string]int `json:"bundle_costs"`
}

type cultivationAttach struct {
	Level       *bool  `json:"level"`
	BasicAttack *bool  `json:"basic_attack"`
	Skill       *bool  `json:"skill"`
	Combo       *bool  `json:"combo"`
	Ultimate    *bool  `json:"ultimate"`
	Trust       *bool  `json:"trust"`
	Talent      *bool  `json:"talent"`
	Base        *bool  `json:"base"`
	Weapon      *bool  `json:"weapon"`
	Mode        string `json:"mode"`
	Target      *int   `json:"target"`
}

// EvaluateCultivationBundle sums selected cultivation parts and reports capacity or deficit.
type EvaluateCultivationBundle struct{}

// Run implements maa.CustomActionRunner.
func (a *EvaluateCultivationBundle) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().
			Str("component", componentEvaluateCultivationBundle).
			Msg("nil context or arg")
		return false
	}

	params, err := parseEvaluateCultivationBundleParam(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentEvaluateCultivationBundle).
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse params")
		return false
	}

	attach, err := loadCultivationAttach(ctx, arg.CurrentTaskName)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentEvaluateCultivationBundle).
			Str("node", arg.CurrentTaskName).
			Msg("failed to load attach")
		return false
	}

	selected := selectedCultivationParts(attach)
	if len(selected) == 0 {
		maafocus.Print(ctx, i18n.T("cultivation.no_parts"))
		log.Warn().
			Str("component", componentEvaluateCultivationBundle).
			Msg("no cultivation parts selected")
		return false
	}

	costs := params.BundleCosts
	if len(costs) == 0 {
		costs = defaultCultivationCosts
	}
	need, err := sumCultivationNeeds(costs, selected)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentEvaluateCultivationBundle).
			Msg("failed to sum cultivation needs")
		return false
	}

	if err := ims.EnsureHydrated(); err != nil {
		log.Error().
			Err(err).
			Str("component", componentEvaluateCultivationBundle).
			Msg("failed to hydrate ims cache")
		return false
	}

	mode := strings.TrimSpace(attach.Mode)
	if mode == "" {
		mode = modeCapacity
	}

	switch mode {
	case modeCapacity:
		sets := cultivationCapacity(need)
		maafocus.Print(ctx, i18n.T("cultivation.capacity", sets))
		log.Info().
			Str("component", componentEvaluateCultivationBundle).
			Str("mode", mode).
			Strs("parts", selected).
			Interface("need", need).
			Int("sets", sets).
			Msg("cultivation capacity evaluated")
		return true
	case modeDeficit:
		target := 0
		if attach.Target != nil {
			target = *attach.Target
		}
		if target <= 0 {
			maafocus.Print(ctx, i18n.T("cultivation.invalid_target"))
			log.Warn().
				Str("component", componentEvaluateCultivationBundle).
				Int("target", target).
				Msg("deficit target must be positive")
			return false
		}
		missing := cultivationDeficit(need, target)
		maafocus.Print(ctx, formatCultivationDeficitFocus(target, missing))
		log.Info().
			Str("component", componentEvaluateCultivationBundle).
			Str("mode", mode).
			Strs("parts", selected).
			Interface("need", need).
			Int("target", target).
			Interface("missing", missing).
			Msg("cultivation deficit evaluated")
		return true
	default:
		log.Error().
			Str("component", componentEvaluateCultivationBundle).
			Str("mode", mode).
			Msg("unknown mode")
		return false
	}
}

func parseEvaluateCultivationBundleParam(raw string) (evaluateCultivationBundleParam, error) {
	var params evaluateCultivationBundleParam
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "null" {
		return params, nil
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return evaluateCultivationBundleParam{}, err
	}
	return params, nil
}

func loadCultivationAttach(ctx *maa.Context, nodeName string) (cultivationAttach, error) {
	if ctx == nil || strings.TrimSpace(nodeName) == "" {
		return cultivationAttach{}, nil
	}
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return cultivationAttach{}, err
	}
	var wrapper struct {
		Attach cultivationAttach `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return cultivationAttach{}, err
	}
	return wrapper.Attach, nil
}

func selectedCultivationParts(attach cultivationAttach) []string {
	type flag struct {
		key string
		on  *bool
	}
	flags := []flag{
		{partLevel, attach.Level},
		{partBasicAttack, attach.BasicAttack},
		{partSkill, attach.Skill},
		{partCombo, attach.Combo},
		{partUltimate, attach.Ultimate},
		{partTrust, attach.Trust},
		{partTalent, attach.Talent},
		{partBase, attach.Base},
		{partWeapon, attach.Weapon},
	}
	out := make([]string, 0, len(flags))
	for _, f := range flags {
		if f.on != nil && *f.on {
			out = append(out, f.key)
		}
	}
	return out
}

func sumCultivationNeeds(costs map[string]map[string]int, selected []string) (map[string]int, error) {
	need := make(map[string]int)
	for _, part := range selected {
		row, ok := costs[part]
		if !ok {
			return nil, fmt.Errorf("unknown cultivation part %q", part)
		}
		for k, v := range row {
			if v < 0 {
				return nil, fmt.Errorf("negative cost %s.%s=%d", part, k, v)
			}
			need[k] += v
		}
	}
	return need, nil
}

// expTier maps a physical IMS item to its EXP contribution.
// Convention: advanced=10000, intermediate=1000, elementary=200
// (cognitive carrier has no 200-tier item in-game).
type expTier struct {
	id    string
	value int
}

func expTiersFor(costKey string) []expTier {
	switch costKey {
	case costKeyCombatEXP:
		return []expTier{
			{"ADVANCED_COMBAT_RECORD", 10000},
			{"INTERMEDIATE_COMBAT_RECORD", 1000},
			{"ELEMENTARY_COMBAT_RECORD", 200},
		}
	case costKeyCognitiveEXP:
		return []expTier{
			{"ADVANCED_COGNITIVE_CARRIER", 10000},
			{"ELEMENTARY_COGNITIVE_CARRIER", 1000},
		}
	case costKeyWeaponEXP:
		return []expTier{
			{"ARMS_INSP_SET", 10000},
			{"ARMS_INSP_KIT", 1000},
			{"ARMS_INSPECTOR", 200},
		}
	default:
		return nil
	}
}

func isExpCostKey(key string) bool {
	return key == costKeyCombatEXP || key == costKeyCognitiveEXP || key == costKeyWeaponEXP
}

func stockForCostKey(key string) int {
	if tiers := expTiersFor(key); len(tiers) > 0 {
		total := 0
		for _, t := range tiers {
			total += ims.ItemQuantity(t.id) * t.value
		}
		return total
	}
	if key == costKeyAdvancedMaterial {
		return 0
	}
	return ims.ItemQuantity(key)
}

// expandExpDeficit converts a missing EXP amount into item counts
// (greedy high→low; leftover rounds up on the lowest tier).
func expandExpDeficit(costKey string, missingExp int) map[string]int {
	if missingExp <= 0 {
		return nil
	}
	tiers := expTiersFor(costKey)
	if len(tiers) == 0 {
		return nil
	}
	out := make(map[string]int)
	rem := missingExp
	for i, t := range tiers {
		if t.value <= 0 {
			continue
		}
		if i == len(tiers)-1 {
			n := (rem + t.value - 1) / t.value
			if n > 0 {
				out[t.id] = n
			}
			break
		}
		n := rem / t.value
		if n > 0 {
			out[t.id] = n
		}
		rem %= t.value
		if rem == 0 {
			break
		}
	}
	return out
}

func cultivationCapacity(need map[string]int) int {
	sets := -1
	for key, req := range need {
		if req <= 0 {
			continue
		}
		var ratio int
		if key == costKeyAdvancedMaterial {
			ratio = advancedMaterialCapacity(req)
		} else {
			ratio = stockForCostKey(key) / req
		}
		if sets < 0 || ratio < sets {
			sets = ratio
		}
	}
	if sets < 0 {
		return 0
	}
	return sets
}

func advancedMaterialCapacity(req int) int {
	if req <= 0 {
		return 0
	}
	minRatio := -1
	for _, id := range advancedMaterialIDs {
		ratio := ims.ItemQuantity(id) / req
		if minRatio < 0 || ratio < minRatio {
			minRatio = ratio
		}
	}
	if minRatio < 0 {
		return 0
	}
	return minRatio
}

func cultivationDeficit(need map[string]int, target int) map[string]int {
	missing := make(map[string]int)
	for key, req := range need {
		if req <= 0 {
			continue
		}
		want := req * target
		if key == costKeyAdvancedMaterial {
			for _, id := range advancedMaterialIDs {
				stock := ims.ItemQuantity(id)
				if stock < want {
					missing[id] = want - stock
				}
			}
			continue
		}
		stock := stockForCostKey(key)
		if stock >= want {
			continue
		}
		gap := want - stock
		if isExpCostKey(key) {
			for id, n := range expandExpDeficit(key, gap) {
				missing[id] += n
			}
			continue
		}
		missing[key] = gap
	}
	return missing
}

func formatCultivationDeficitFocus(target int, missing map[string]int) string {
	if len(missing) == 0 {
		return i18n.T("cultivation.deficit_ok", target)
	}
	keys := make([]string, 0, len(missing))
	for k := range missing {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s: %d", costDisplayName(k), missing[k]))
	}
	return i18n.T("cultivation.deficit", target, strings.Join(lines, "\n"))
}

func costDisplayName(key string) string {
	name := i18n.T("ims.item." + key)
	if name == "ims.item."+key || strings.TrimSpace(name) == "" {
		return key
	}
	return name
}
