package failurecollector

import (
	"reflect"
	"testing"
)

func TestCollectorLifecycle(t *testing.T) {
	const key = "test-lifecycle"
	Reset(key)
	Record(key, "RouteA")
	Record(key, "RouteB")

	if got, want := Finish(key), []string{"RouteA", "RouteB"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Finish() = %v, want %v", got, want)
	}
	if got := Finish(key); len(got) != 0 {
		t.Fatalf("second Finish() = %v, want empty result", got)
	}
}
