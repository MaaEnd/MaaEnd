package ims

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
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

func TestAddItemDataSkipsWhenNotInitialized(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	a := &AddItemData{}
	arg := &maa.CustomActionArg{
		CustomActionParam: `{"items":{"PROTODISK":"PROTODISK"}}`,
	}
	if !a.Run(nil, arg) {
		t.Fatal("expected success when ims data is not initialized")
	}
	if got := globalCache.quantity("PROTODISK"); got != 0 {
		t.Fatalf("quantity=%d, want 0 (no add without init)", got)
	}
}
