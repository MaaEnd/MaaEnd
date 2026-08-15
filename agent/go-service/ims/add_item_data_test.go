package ims

import (
	"testing"
)

func TestParseAddItemDataParamEmpty(t *testing.T) {
	params, err := parseAddItemDataParam("")
	if err != nil {
		t.Fatal(err)
	}
	if params.GridType != "" || len(params.ItemFilters) != 0 {
		t.Fatalf("params=%+v", params)
	}

	params, err = parseAddItemDataParam(`{"item_filters":["Isolate:*"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(params.ItemFilters) != 1 || params.ItemFilters[0] != "Isolate:*" {
		t.Fatalf("params=%+v", params)
	}
}
