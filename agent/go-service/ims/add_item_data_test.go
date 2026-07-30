package ims

import (
	"testing"
)

func TestParseAddItemDataParam(t *testing.T) {
	if _, err := parseAddItemDataParam(""); err == nil {
		t.Fatal("expected error for empty param")
	}
	params, err := parseAddItemDataParam(`{
		"items": {
			"PROTODISK": "PROTODISK",
			"CAST_DIE": "CAST_DIE"
		}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if params.Items["PROTODISK"] != "PROTODISK" || params.Items["CAST_DIE"] != "CAST_DIE" {
		t.Fatalf("items=%v", params.Items)
	}
}
