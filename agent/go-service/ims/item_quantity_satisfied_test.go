package ims

import (
	"encoding/json"
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestParseItemQuantitySatisfiedParam(t *testing.T) {
	if _, err := parseItemQuantitySatisfiedParam(""); err == nil {
		t.Fatal("expected error for empty param")
	}
	if _, err := parseItemQuantitySatisfiedParam(`{}`); err == nil {
		t.Fatal("expected error when expression missing")
	}
	if _, err := parseItemQuantitySatisfiedParam(`{"expression":"   "}`); err == nil {
		t.Fatal("expected error for blank expression")
	}

	params, err := parseItemQuantitySatisfiedParam(`{"expression":" ({item_char_break_stage_1_2}+{item_weapon_break_low}) >= 100 "}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.Expression != "({item_char_break_stage_1_2}+{item_weapon_break_low}) >= 100" {
		t.Fatalf("expression = %q", params.Expression)
	}
	if params.NotifyUI {
		t.Fatal("expected notify_ui default false when omitted")
	}
	if params.ReportOnly {
		t.Fatal("expected report_only default false when omitted")
	}

	params, err = parseItemQuantitySatisfiedParam(`{"expression":"{item_char_break_stage_1_2}>=1","notify_ui":true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !params.NotifyUI {
		t.Fatal("expected notify_ui true")
	}

	params, err = parseItemQuantitySatisfiedParam(`{"expression":"{item_gold}","report_only":true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !params.ReportOnly || params.Expression != "{item_gold}" {
		t.Fatalf("params=%+v", params)
	}
	if _, err := parseItemQuantitySatisfiedParam(`{"expression":"{a}+{b}","report_only":true}`); err == nil {
		t.Fatal("expected error for multi-item report_only expression")
	}
	if _, err := parseItemQuantitySatisfiedParam(`{"expression":"1>=0","report_only":true}`); err == nil {
		t.Fatal("expected error for report_only without item placeholder")
	}
}

func TestItemQuantitySatisfiedRun(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	r := &ItemQuantitySatisfied{}
	arg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"expression":"{item_char_break_stage_1_2}>=5"}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}

	if _, ok := r.Run(nil, arg); ok {
		t.Fatal("expected miss when item missing from empty cache")
	}

	MarkSynced(time.Now(), map[string]int{"item_char_break_stage_1_2": 4})
	if _, ok := r.Run(nil, arg); ok {
		t.Fatal("expected miss when current < required")
	}

	MarkSynced(time.Now(), map[string]int{"item_char_break_stage_1_2": 5})
	result, ok := r.Run(nil, arg)
	if !ok {
		t.Fatal("expected hit when current == required")
	}
	if result == nil || result.Detail == "" {
		t.Fatal("expected detail json")
	}

	MarkSynced(time.Now(), map[string]int{"item_char_break_stage_1_2": 9})
	if _, ok := r.Run(nil, arg); !ok {
		t.Fatal("expected hit when current > required")
	}

	zeroArg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"expression":"{MISSING}>=0"}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}
	if _, ok := r.Run(nil, zeroArg); !ok {
		t.Fatal("expected hit for >=0 even when item absent")
	}
}

func TestItemQuantitySatisfiedExpressionRun(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	r := &ItemQuantitySatisfied{}
	arg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"expression":"({item_char_break_stage_1_2}+{item_weapon_break_low})>=100"}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}

	if _, ok := r.Run(nil, arg); ok {
		t.Fatal("expected miss when cache empty (0+0 < 100)")
	}

	MarkSynced(time.Now(), map[string]int{
		"item_char_break_stage_1_2": 40,
		"item_weapon_break_low":     50,
	})
	if _, ok := r.Run(nil, arg); ok {
		t.Fatal("expected miss when sum < 100")
	}

	MarkSynced(time.Now(), map[string]int{
		"item_char_break_stage_1_2": 40,
		"item_weapon_break_low":     60,
	})
	result, ok := r.Run(nil, arg)
	if !ok {
		t.Fatal("expected hit when sum >= 100")
	}
	if result == nil || result.Detail == "" {
		t.Fatal("expected detail json")
	}

	var detail map[string]any
	if err := json.Unmarshal([]byte(result.Detail), &detail); err != nil {
		t.Fatalf("detail json: %v", err)
	}
	if detail["resolved_expression"] != "(40+60)>=100" {
		t.Fatalf("resolved_expression = %#v", detail["resolved_expression"])
	}

	andArg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"expression":"{item_char_break_stage_1_2}>=40 && {item_weapon_break_low}<70"}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}
	if _, ok := r.Run(nil, andArg); !ok {
		t.Fatal("expected hit for compound expression")
	}

	badArg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"expression":"{item_char_break_stage_1_2}+{item_weapon_break_low}"}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}
	if _, ok := r.Run(nil, badArg); ok {
		t.Fatal("expected miss when expression is not boolean")
	}
}

func TestItemQuantitySatisfiedReportOnly(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	r := &ItemQuantitySatisfied{}
	arg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"expression":"{item_char_break_stage_1_2}","report_only":true}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}

	result, ok := r.Run(nil, arg)
	if !ok {
		t.Fatal("report_only should hit even when cache empty")
	}
	var detail map[string]any
	if err := json.Unmarshal([]byte(result.Detail), &detail); err != nil {
		t.Fatalf("detail json: %v", err)
	}
	if detail["report_only"] != true {
		t.Fatalf("detail=%#v", detail)
	}
	if detail["item_id"] != "item_char_break_stage_1_2" {
		t.Fatalf("item_id=%#v", detail["item_id"])
	}
	if detail["quantity"] != float64(0) {
		t.Fatalf("quantity=%#v", detail["quantity"])
	}

	MarkSynced(time.Now(), map[string]int{"item_char_break_stage_1_2": 40})
	result, ok = r.Run(nil, arg)
	if !ok {
		t.Fatal("report_only should always hit")
	}
	if err := json.Unmarshal([]byte(result.Detail), &detail); err != nil {
		t.Fatalf("detail json: %v", err)
	}
	if detail["quantity"] != float64(40) {
		t.Fatalf("quantity=%#v", detail["quantity"])
	}

	multiArg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"expression":"{item_char_break_stage_1_2}+{item_weapon_break_low}","report_only":true}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}
	if _, ok := r.Run(nil, multiArg); ok {
		t.Fatal("report_only must reject multi-item expression")
	}
}
