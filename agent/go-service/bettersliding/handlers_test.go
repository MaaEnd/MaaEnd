package bettersliding

import (
	"testing"
)

// TestResolveMaxQuantityNext 验证 maxQuantity 为零及边界值时，resolveMaxQuantityNext 能正确返回 Done，避免零库存进入 FindEnd 崩溃
// 简言之就是测试零库存是否正确返回 Done，是否被破坏，能否正常执行、实现逻辑
func TestResolveMaxQuantityNext(t *testing.T) {
	cases := []struct {
		name        string
		maxQuantity int
		target      int
		wantNode    string
		wantErr     bool
	}{
		{
			name:        "zero stock returns done",
			maxQuantity: 0,
			target:      0,
			wantNode:    nodeBetterSlidingDone,
			wantErr:     false,
		},
		{
			name:        "zero stock with non-zero target returns error",
			maxQuantity: 0,
			target:      5,
			wantNode:    "",
			wantErr:     true,
		},
		{
			name:        "single item matching target returns done",
			maxQuantity: 1,
			target:      1,
			wantNode:    nodeBetterSlidingDone,
			wantErr:     false,
		},
		{
			name:        "max equals target greater than one returns empty",
			maxQuantity: 5,
			target:      5,
			wantNode:    "",
			wantErr:     false,
		},
		{
			name:        "max greater than target returns empty",
			maxQuantity: 10,
			target:      3,
			wantNode:    "",
			wantErr:     false,
		},
		{
			name:        "max less than target returns error",
			maxQuantity: 3,
			target:      5,
			wantNode:    "",
			wantErr:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotNode, gotErr := resolveMaxQuantityNext(tc.maxQuantity, tc.target)
			if tc.wantErr && gotErr == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && gotErr != nil {
				t.Fatalf("unexpected error: %v", gotErr)
			}
			if gotNode != tc.wantNode {
				t.Fatalf("node mismatch: want %q, got %q", tc.wantNode, gotNode)
			}
		})
	}
}
