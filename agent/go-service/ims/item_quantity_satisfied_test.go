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
	if _, err := parseItemQuantitySatisfiedParam(`{"item":"","quantity":1}`); err == nil {
		t.Fatal("expected error for empty item")
	}
	if _, err := parseItemQuantitySatisfiedParam(`{"item":"X","quantity":-1}`); err == nil {
		t.Fatal("expected error for negative quantity")
	}
	if _, err := parseItemQuantitySatisfiedParam(`{"item":"X","quantity":1,"expression":"{X}>=1"}`); err == nil {
		t.Fatal("expected error when item and expression both set")
	}
	if _, err := parseItemQuantitySatisfiedParam(`{}`); err == nil {
		t.Fatal("expected error when neither mode set")
	}

	params, err := parseItemQuantitySatisfiedParam(`{"item":" PROTODISK ","quantity":3}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.Item != "PROTODISK" || params.Quantity != 3 {
		t.Fatalf("got %+v", params)
	}
	if params.NotifyUI {
		t.Fatal("expected notify_ui default false when omitted")
	}
	if params.expressionMode() {
		t.Fatal("expected simple mode")
	}

	params, err = parseItemQuantitySatisfiedParam(`{"item":"PROTODISK","quantity":1,"notify_ui":true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !params.NotifyUI {
		t.Fatal("expected notify_ui true")
	}

	params, err = parseItemQuantitySatisfiedParam(`{"expression":" ({PROTODISK}+{CAST_DIE}) >= 100 "}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !params.expressionMode() {
		t.Fatal("expected expression mode")
	}
	if params.Expression != "({PROTODISK}+{CAST_DIE}) >= 100" {
		t.Fatalf("expression = %q", params.Expression)
	}
}

func TestItemQuantitySatisfiedRun(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	r := &ItemQuantitySatisfied{}
	arg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"item":"PROTODISK","quantity":5}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}

	if _, ok := r.Run(nil, arg); ok {
		t.Fatal("expected miss when item missing from empty cache")
	}

	MarkSynced(time.Now(), map[string]int{"PROTODISK": 4})
	if _, ok := r.Run(nil, arg); ok {
		t.Fatal("expected miss when current < required")
	}

	MarkSynced(time.Now(), map[string]int{"PROTODISK": 5})
	result, ok := r.Run(nil, arg)
	if !ok {
		t.Fatal("expected hit when current == required")
	}
	if result == nil || result.Detail == "" {
		t.Fatal("expected detail json")
	}

	MarkSynced(time.Now(), map[string]int{"PROTODISK": 9})
	if _, ok := r.Run(nil, arg); !ok {
		t.Fatal("expected hit when current > required")
	}

	zeroArg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"item":"MISSING","quantity":0}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}
	if _, ok := r.Run(nil, zeroArg); !ok {
		t.Fatal("expected hit for quantity 0 even when item absent")
	}
}

func TestItemQuantitySatisfiedExpressionRun(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	r := &ItemQuantitySatisfied{}
	arg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"expression":"({PROTODISK}+{CAST_DIE})>=100"}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}

	if _, ok := r.Run(nil, arg); ok {
		t.Fatal("expected miss when cache empty (0+0 < 100)")
	}

	MarkSynced(time.Now(), map[string]int{
		"PROTODISK": 40,
		"CAST_DIE":  50,
	})
	if _, ok := r.Run(nil, arg); ok {
		t.Fatal("expected miss when sum < 100")
	}

	MarkSynced(time.Now(), map[string]int{
		"PROTODISK": 40,
		"CAST_DIE":  60,
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
		CustomRecognitionParam: `{"expression":"{PROTODISK}>=40 && {CAST_DIE}<70"}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}
	if _, ok := r.Run(nil, andArg); !ok {
		t.Fatal("expected hit for compound expression")
	}

	badArg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"expression":"{PROTODISK}+{CAST_DIE}"}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}
	if _, ok := r.Run(nil, badArg); ok {
		t.Fatal("expected miss when expression is not boolean")
	}
}
