package listcomplete

import (
	"testing"
)

func TestParseParams(t *testing.T) {
	t.Parallel()

	p, err := parseParams(`{"node":"FooOCR"}`)
	if err != nil {
		t.Fatalf("parseParams returned error: %v", err)
	}
	if p.Node != "FooOCR" {
		t.Fatalf("node = %q, want FooOCR", p.Node)
	}

	if _, err := parseParams(`{}`); err == nil {
		t.Fatal("expected error for empty node")
	}
	if _, err := parseParams(""); err == nil {
		t.Fatal("expected error for empty param")
	}
}
