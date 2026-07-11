package failurecollector

import (
	"reflect"
	"testing"
)

func TestCollectorLifecycle(t *testing.T) {
	const key = "test-lifecycle"
	Reset(key)
	SetCurrent(key, "RouteA")
	if got := RecordCurrent(key); got != "RouteA" {
		t.Fatalf("RecordCurrent() = %q, want RouteA", got)
	}
	Record(key, "RouteB")

	if got, want := Finish(key), []string{"RouteA", "RouteB"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Finish() = %v, want %v", got, want)
	}
	if got := Finish(key); len(got) != 0 {
		t.Fatalf("second Finish() = %v, want empty result", got)
	}
}

func TestRecordCurrentWithoutCurrentItem(t *testing.T) {
	const key = "test-empty-current"
	Reset(key)
	if got := RecordCurrent(key); got != "" {
		t.Fatalf("RecordCurrent() = %q, want empty string", got)
	}
}
