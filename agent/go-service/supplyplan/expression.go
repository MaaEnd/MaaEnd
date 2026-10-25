package supplyplan

import (
	"strconv"
	"strings"
)

// term is one inventory item counted toward a route, with its XP weight.
type term struct {
	Item   string
	Weight int64
}

// route mirrors one SupplyPlanLimits input of the ProtocolSpace target-inventory mode.
type route struct {
	Key   string
	Label string
	Terms []term
}

// routes follows the input order of option SupplyPlanLimits in assets/tasks/ProtocolSpace.json.
// The dispatch expressions compare routes in this order, so it must not be re-sorted.
var routes = []route{
	{"T_CREDS", "option.SupplyPlanLimits.inputs.T_CREDS.label", []term{{"item_gold", 1}}},
	{"PROTOHEDRON", "option.SupplyPlanLimits.inputs.PROTOHEDRON.label", []term{{"item_char_skill_level_7_12", 1}}},
	{"PROTOPRISM", "option.SupplyPlanLimits.inputs.PROTOPRISM.label", []term{{"item_char_skill_level_1_6", 1}}},
	{"COGNITIVE_CARRIER_EXP", "option.SupplyPlanLimits.inputs.COGNITIVE_CARRIER_EXP.label", []term{{"item_expcard_stage2_high", 10000}, {"item_expcard_stage2_low", 1000}}},
	{"COMBAT_RECORD_EXP", "option.SupplyPlanLimits.inputs.COMBAT_RECORD_EXP.label", []term{{"item_expcard_stage1_high", 10000}, {"item_expcard_stage1_mid", 1000}, {"item_expcard_stage1_low", 200}}},
	{"PROTOSET", "option.SupplyPlanLimits.inputs.PROTOSET.label", []term{{"item_char_break_stage_3_4", 1}}},
	{"PROTODISK", "option.SupplyPlanLimits.inputs.PROTODISK.label", []term{{"item_char_break_stage_1_2", 1}}},
	{"WEAPON_EXP", "option.SupplyPlanLimits.inputs.WEAPON_EXP.label", []term{{"item_weapon_expcard_high", 10000}, {"item_weapon_expcard_mid", 1000}, {"item_weapon_expcard_low", 200}}},
	{"HEAVY_CAST_DIE", "option.SupplyPlanLimits.inputs.HEAVY_CAST_DIE.label", []term{{"item_weapon_break_high", 1}}},
	{"CAST_DIE", "option.SupplyPlanLimits.inputs.CAST_DIE.label", []term{{"item_weapon_break_low", 1}}},
	{"WARP_PLASMA_COATED_SLIDE", "option.SupplyPlanLimits.inputs.WARP_PLASMA_COATED_SLIDE.label", []term{{"item_char_skill_specialize_6", 1}}},
	{"TRIPHASIC_NANOFLAKE", "option.SupplyPlanLimits.inputs.TRIPHASIC_NANOFLAKE.label", []term{{"item_char_skill_specialize_5", 1}}},
	{"QUADRANT_FITTING_FLUID", "option.SupplyPlanLimits.inputs.QUADRANT_FITTING_FLUID.label", []term{{"item_char_skill_specialize_4", 1}}},
	{"TACHYON_SCREENING_LATTICE", "option.SupplyPlanLimits.inputs.TACHYON_SCREENING_LATTICE.label", []term{{"item_char_skill_specialize_3", 1}}},
	{"D96_STEEL_SAMPLE_4", "option.SupplyPlanLimits.inputs.D96_STEEL_SAMPLE_4.label", []term{{"item_char_skill_specialize_2", 1}}},
	{"METADIASTIMA_PHOTOEMISSION_TUBE", "option.SupplyPlanLimits.inputs.METADIASTIMA_PHOTOEMISSION_TUBE.label", []term{{"item_char_skill_specialize_1", 1}}},
}

// quantity renders a route's inventory amount as an ItemQuantitySatisfied expression.
func (r route) quantity() string {
	if len(r.Terms) == 1 && r.Terms[0].Weight == 1 {
		return "{" + r.Terms[0].Item + "}"
	}
	parts := make([]string, len(r.Terms))
	for i, t := range r.Terms {
		parts[i] = "{" + t.Item + "}*" + strconv.FormatInt(t.Weight, 10)
	}
	return "(" + strings.Join(parts, "+") + ")"
}

// owned evaluates the same quantity from an IMS snapshot.
func (r route) owned(items map[string]int) int64 {
	var sum int64
	for _, t := range r.Terms {
		sum += int64(items[t.Item]) * t.Weight
	}
	return sum
}

// expressions rebuilds the upstream SupplyPlanLimits overrides for the given limits.
// A route is insufficient when below its limit and no other route has a lower completion
// ratio (cross-multiplied); this is the same formula the task option ships with.
func expressions(limits map[string]string) map[string]string {
	out := make(map[string]string, 2*len(routes))
	for _, r := range routes {
		q := r.quantity()
		var b strings.Builder
		b.WriteString(q + "<" + limits[r.Key])
		for _, other := range routes {
			if other.Key == r.Key {
				continue
			}
			b.WriteString("&&" + q + "*" + limits[other.Key] + "<=" + other.quantity() + "*" + limits[r.Key])
		}
		out["SupplyPlanInsufficient_"+r.Key] = b.String()
		out["SupplyPlanRewardItemMet_"+r.Key] = q + ">=" + limits[r.Key]
	}
	return out
}
