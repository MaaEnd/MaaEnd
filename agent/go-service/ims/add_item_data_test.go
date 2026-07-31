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

func TestAddItemDataNeedsContextWhenNotInitialized(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	a := &AddItemData{}
	arg := &maa.CustomActionArg{
		CustomActionParam: `{"items":{"PROTODISK":"PROTODISK"}}`,
	}
	// Uninitialized cache still runs recognition; nil context cannot capture image.
	if a.Run(nil, arg) {
		t.Fatal("expected failure without context when recognition is required")
	}
	if got := globalCache.quantity("PROTODISK"); got != 0 {
		t.Fatalf("quantity=%d, want 0", got)
	}
}
