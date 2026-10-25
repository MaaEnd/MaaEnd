package core

import (
	"image"
	"sort"
)

// RedOwned detects red missing-stock text inside an OCR owned-label box only.
// Thresholds cover preserved CloudADB samples; new-device validation is pending.
func RedOwned(img image.Image, box [4]int) bool {
	if img == nil || box[2] <= 0 || box[3] <= 0 {
		return false
	}
	area := image.Rect(box[0], box[1], box[0]+box[2], box[1]+box[3])
	if !area.In(img.Bounds()) {
		return false
	}
	red := 0
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			r, g, b = r>>8, g>>8, b>>8
			hi, lo := max(r, g, b), min(r, g, b)
			// HSV red/magenta band: hue 330..360 or 0..15 degrees,
			// saturation at least 25%, value at least 120/255.
			if hi == r && hi >= 120 && hi-lo >= hi/4 &&
				float64(int(g)-int(b))/float64(max(hi-lo, 1)) >= -0.5 &&
				float64(int(g)-int(b))/float64(max(hi-lo, 1)) <= 0.25 {
				red++
			}
		}
	}
	return red >= 4
}

// ItemRoutes maps missing operator materials to upstream protocol routes.
// Experience denominations share a route; no experience arithmetic is performed.
var ItemRoutes = map[string]string{
	"item_gold": "T_CREDS", "item_char_skill_level_7_12": "PROTOHEDRON", "item_char_skill_level_1_6": "PROTOPRISM",
	"item_expcard_stage2_high": "COGNITIVE_CARRIER_EXP", "item_expcard_stage2_low": "COGNITIVE_CARRIER_EXP",
	"item_expcard_stage1_high": "COMBAT_RECORD_EXP", "item_expcard_stage1_mid": "COMBAT_RECORD_EXP", "item_expcard_stage1_low": "COMBAT_RECORD_EXP",
	"item_char_break_stage_3_4": "PROTOSET", "item_char_break_stage_1_2": "PROTODISK",
	"item_char_skill_specialize_6": "WARP_PLASMA_COATED_SLIDE", "item_char_skill_specialize_5": "TRIPHASIC_NANOFLAKE",
	"item_char_skill_specialize_4": "QUADRANT_FITTING_FLUID", "item_char_skill_specialize_3": "TACHYON_SCREENING_LATTICE",
	"item_char_skill_specialize_2": "D96_STEEL_SAMPLE_4", "item_char_skill_specialize_1": "METADIASTIMA_PHOTOEMISSION_TUBE",
}

// Routes deduplicates this run's missing materials; unsupported items are skipped.
func Routes(missing []Material) []string {
	seen := map[string]bool{}
	for _, row := range missing {
		if route := ItemRoutes[row.ID]; route != "" {
			seen[route] = true
		}
	}
	var out []string
	for route := range seen {
		out = append(out, route)
	}
	sort.Strings(out)
	return out
}
