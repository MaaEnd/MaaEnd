package exprcoord

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestResolveRect(t *testing.T) {
	tests := []struct {
		name    string
		raw     []any
		want    maa.Rect
		wantErr bool
	}{
		{name: "all expressions", raw: []any{"WIDTH/2-100", "HEIGHT/2-50", "200", "100"}, want: maa.Rect{540, 310, 200, 100}},
		{name: "all numeric", raw: []any{10.0, 20.0, 30.0, 40.0}, want: maa.Rect{10, 20, 30, 40}},
		{name: "mixed", raw: []any{"WIDTH-200", 0.0, "200", 200}, want: maa.Rect{1080, 0, 200, 200}},
		{name: "wrong length", raw: []any{1.0, 2.0, 3.0}, wantErr: true},
		{name: "bad expr", raw: []any{"WIDTH/", 0.0, 1.0, 1.0}, wantErr: true},
	}
	for _, tt := range tests {
		got, err := ResolveRect(tt.raw, 1280, 720)
		if tt.wantErr {
			if err == nil {
				t.Errorf("%s: expected error", tt.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tt.name, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestResolvePoint(t *testing.T) {
	tests := []struct {
		name    string
		raw     []any
		wantX   int
		wantY   int
		wantErr bool
	}{
		{name: "all expressions", raw: []any{"WIDTH/2", "HEIGHT/2"}, wantX: 640, wantY: 360},
		{name: "all numeric", raw: []any{100.0, 200.0}, wantX: 100, wantY: 200},
		{name: "mixed", raw: []any{"WIDTH-1", 30}, wantX: 1279, wantY: 30},
		{name: "bad expr", raw: []any{"FOO", 0.0}, wantErr: true},
	}
	for _, tt := range tests {
		x, y, err := ResolvePoint(tt.raw, 1280, 720)
		if tt.wantErr {
			if err == nil {
				t.Errorf("%s: expected error", tt.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tt.name, err)
			continue
		}
		if x != tt.wantX || y != tt.wantY {
			t.Errorf("%s: got (%d,%d), want (%d,%d)", tt.name, x, y, tt.wantX, tt.wantY)
		}
	}
}
