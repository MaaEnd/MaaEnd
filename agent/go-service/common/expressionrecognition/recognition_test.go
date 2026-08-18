package expressionrecognition

import "testing"

func TestParseParamsTrimsBoxNode(t *testing.T) {
	params, err := parseParams(`{"expression":"{NodeA}<{NodeB}","box_node":"  NodeA  "}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.BoxNode != "NodeA" {
		t.Fatalf("parseParams() boxNode = %q, want %q", params.BoxNode, "NodeA")
	}
}
