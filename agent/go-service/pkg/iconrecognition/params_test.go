package iconrecognition

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNewParamsSerializesRegionRestrictedCandidateOptions(t *testing.T) {
	params := NewParams(
		WithGridType(GridTypeTransfer),
		WithAdditionalItemFilters(
			ItemFilter(" Normal:Product "),
			ItemFilter("Normal:Usable"),
		),
		WithExcludedItemIDs(" item_a ", "item_b"),
		WithRecognizeRegionUnavailable(true),
	)

	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if got := decoded["additional_item_filters"]; !reflect.DeepEqual(got, []any{"Normal:Product", "Normal:Usable"}) {
		t.Fatalf("additional_item_filters = %#v", got)
	}
	if got := decoded["excluded_item_ids"]; !reflect.DeepEqual(got, []any{"item_a", "item_b"}) {
		t.Fatalf("excluded_item_ids = %#v", got)
	}
	if got := decoded["recognize_region_unavailable"]; got != true {
		t.Fatalf("recognize_region_unavailable = %#v", got)
	}
}

func TestParseParamsNormalizesRegionRestrictedCandidateOptions(t *testing.T) {
	params, err := ParseParams(`{
		"grid_type":"transfer",
		"additional_item_filters":[" Normal:Ore "],
		"excluded_item_ids":[" item_a "],
		"recognize_region_unavailable":false
	}`)
	if err != nil {
		t.Fatalf("ParseParams: %v", err)
	}

	if !reflect.DeepEqual(params.AdditionalItemFilters, []ItemFilter{"Normal:Ore"}) {
		t.Fatalf("AdditionalItemFilters = %#v", params.AdditionalItemFilters)
	}
	if !reflect.DeepEqual(params.ExcludedItemIDs, []string{"item_a"}) {
		t.Fatalf("ExcludedItemIDs = %#v", params.ExcludedItemIDs)
	}
	if params.RecognizeRegionUnavailable == nil || *params.RecognizeRegionUnavailable {
		t.Fatalf("RecognizeRegionUnavailable = %#v", params.RecognizeRegionUnavailable)
	}
}

func TestNewParamsStableDeduplicatesCandidateOptions(t *testing.T) {
	params := NewParams(
		WithItemIDs(" item_b ", "item_a", "item_b"),
		WithItemFilters(ItemFilter("Normal:Product"), ItemFilter(" Normal:Ore "), ItemFilter("Normal:Product")),
		WithAdditionalItemFilters(ItemFilter("Isolate:*"), ItemFilter("Isolate:*")),
		WithExcludedItemIDs("item_c", " item_c "),
		WithItemRecheckFilters(ItemFilter("Normal:*"), ItemFilter("Normal:*")),
	)

	if !reflect.DeepEqual(params.ItemIDs, []string{"item_b", "item_a"}) {
		t.Fatalf("ItemIDs = %#v", params.ItemIDs)
	}
	if !reflect.DeepEqual(params.ItemFilters, []ItemFilter{"Normal:Product", "Normal:Ore"}) {
		t.Fatalf("ItemFilters = %#v", params.ItemFilters)
	}
	if !reflect.DeepEqual(params.AdditionalItemFilters, []ItemFilter{"Isolate:*"}) {
		t.Fatalf("AdditionalItemFilters = %#v", params.AdditionalItemFilters)
	}
	if !reflect.DeepEqual(params.ExcludedItemIDs, []string{"item_c"}) {
		t.Fatalf("ExcludedItemIDs = %#v", params.ExcludedItemIDs)
	}
	if !reflect.DeepEqual(params.ItemRecheckFilters, []ItemFilter{"Normal:*"}) {
		t.Fatalf("ItemRecheckFilters = %#v", params.ItemRecheckFilters)
	}
}

func TestParseParamsStableDeduplicatesCandidateOptions(t *testing.T) {
	params, err := ParseParams(`{
		"grid_type":"transfer",
		"item_ids":[" item_b ","item_a","item_b"],
		"item_filters":["Normal:Product"," Normal:Ore ","Normal:Product"],
		"additional_item_filters":["Isolate:*","Isolate:*"],
		"excluded_item_ids":["item_c"," item_c "],
		"item_recheck_filters":["Normal:*","Normal:*"]
	}`)
	if err != nil {
		t.Fatalf("ParseParams: %v", err)
	}

	if !reflect.DeepEqual(params.ItemIDs, []string{"item_b", "item_a"}) {
		t.Fatalf("ItemIDs = %#v", params.ItemIDs)
	}
	if !reflect.DeepEqual(params.ItemFilters, []ItemFilter{"Normal:Product", "Normal:Ore"}) {
		t.Fatalf("ItemFilters = %#v", params.ItemFilters)
	}
	if !reflect.DeepEqual(params.AdditionalItemFilters, []ItemFilter{"Isolate:*"}) {
		t.Fatalf("AdditionalItemFilters = %#v", params.AdditionalItemFilters)
	}
	if !reflect.DeepEqual(params.ExcludedItemIDs, []string{"item_c"}) {
		t.Fatalf("ExcludedItemIDs = %#v", params.ExcludedItemIDs)
	}
	if !reflect.DeepEqual(params.ItemRecheckFilters, []ItemFilter{"Normal:*"}) {
		t.Fatalf("ItemRecheckFilters = %#v", params.ItemRecheckFilters)
	}
}

func TestWithTuningFromDoesNotCopyRegionAvailabilityMode(t *testing.T) {
	source := NewParams(
		WithAdditionalItemFilters(ItemFilter("Normal:Product")),
		WithExcludedItemIDs("item_a"),
		WithRecognizeRegionUnavailable(true),
	)
	params := NewParams(WithTuningFrom(source))

	if params.RecognizeRegionUnavailable != nil {
		t.Fatalf("RecognizeRegionUnavailable = %#v, want nil", params.RecognizeRegionUnavailable)
	}
	if len(params.AdditionalItemFilters) != 0 || len(params.ExcludedItemIDs) != 0 {
		t.Fatalf(
			"candidate options must not be copied: additional=%#v excluded=%#v",
			params.AdditionalItemFilters,
			params.ExcludedItemIDs,
		)
	}
}

func TestParseDetailPreservesOptionalRegionAvailabilityState(t *testing.T) {
	detail, err := ParseDetail(`{
		"detail_version":3,
		"matched":true,
		"grid_type":"transfer",
		"roi":[0,0,64,64],
		"matches":[{
			"item_id":"item_a",
			"name":"iconRecognition.name.item_a",
			"category":"产物",
			"storage_kind":"Normal",
			"category_type":"Product",
			"rarity":3,
			"cell_box":[0,0,64,64],
			"item_box":[0,0,64,64],
			"score":0.9,
			"region_unavailable":true
		}]
	}`)
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	if len(detail.Matches) != 1 || !detail.Matches[0].RegionUnavailable {
		t.Fatalf("matches = %#v", detail.Matches)
	}
}

func TestMatchMarshalsRegionAvailabilityOnlyWhenTrue(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		match       Match
		wantPresent bool
	}{
		{name: "ordinary", match: Match{}},
		{
			name:        "unavailable",
			match:       Match{RegionUnavailable: true},
			wantPresent: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			raw, err := json.Marshal(testCase.match)
			if err != nil {
				t.Fatalf("marshal match: %v", err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("decode match: %v", err)
			}
			value, present := decoded["region_unavailable"]
			if present != testCase.wantPresent {
				t.Fatalf("field presence = %t, want %t", present, testCase.wantPresent)
			}
			if present && value != true {
				t.Fatalf("region_unavailable = %#v", value)
			}
		})
	}
}
