package camerascan

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestAimDeltaCentersRecognitionBox(t *testing.T) {
	tests := []struct {
		name  string
		box   maa.Rect
		wantX int
		wantY int
		ok    bool
	}{
		{
			name:  "already centered",
			box:   maa.Rect{620, 340, 40, 40},
			wantX: 0,
			wantY: 0,
			ok:    true,
		},
		{
			name:  "target at lower right",
			box:   maa.Rect{900, 500, 100, 100},
			wantX: 310,
			wantY: 190,
			ok:    true,
		},
		{
			name: "empty box",
			box:  maa.Rect{},
			ok:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotX, gotY, gotOK := aimDelta(test.box)
			if gotX != test.wantX || gotY != test.wantY || gotOK != test.ok {
				t.Fatalf(
					"aimDelta(%v) = (%d, %d, %t), want (%d, %d, %t)",
					test.box,
					gotX,
					gotY,
					gotOK,
					test.wantX,
					test.wantY,
					test.ok,
				)
			}
		})
	}
}

func TestAimTargetDefaultsToFalse(t *testing.T) {
	param, ok := parseParam(`{"wait_nodes":["Target"]}`)
	if !ok {
		t.Fatal("parseParam returned false")
	}
	if param.AimTarget {
		t.Fatal("aim_target should default to false")
	}

	param, ok = parseParam(`{"wait_nodes":["Target"],"aim_target":true}`)
	if !ok {
		t.Fatal("parseParam returned false for aim_target=true")
	}
	if !param.AimTarget {
		t.Fatal("aim_target=true was not preserved")
	}
}

func TestMoveNodesDefaultAndOverride(t *testing.T) {
	param, ok := parseParam(`{"wait_nodes":["Target"]}`)
	if !ok {
		t.Fatal("parseParam returned false")
	}
	if param.MoveUp != defaultMoveUpNode ||
		param.MoveDown != defaultMoveDownNode ||
		param.MoveLeft != defaultMoveLeftNode ||
		param.MoveRight != defaultMoveRightNode {
		t.Fatalf("unexpected default move nodes: %+v", param)
	}

	param, ok = parseParam(`{
		"wait_nodes":["Target"],
		"move_up":"UpNode",
		"move_down":"DownNode",
		"move_left":"LeftNode",
		"move_right":"RightNode"
	}`)
	if !ok {
		t.Fatal("parseParam returned false for custom move nodes")
	}
	if param.MoveUp != "UpNode" ||
		param.MoveDown != "DownNode" ||
		param.MoveLeft != "LeftNode" ||
		param.MoveRight != "RightNode" {
		t.Fatalf("custom move nodes were not preserved: %+v", param)
	}
}
