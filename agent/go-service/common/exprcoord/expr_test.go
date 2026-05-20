package exprcoord

import "testing"

func TestEval(t *testing.T) {
	tests := []struct {
		expr    string
		want    float64
		wantErr bool
	}{
		{expr: "1280", want: 1280},
		{expr: "WIDTH", want: 1280},
		{expr: "WIDTH/2-110", want: 530},
		{expr: "HEIGHT/2-25", want: 335},
		{expr: "(WIDTH-200)/2", want: 540},
		{expr: "-50", want: -50},
		{expr: "WIDTH * 0.5", want: 640},
		{expr: "1/0", wantErr: true},
		{expr: "FOO", wantErr: true},
		{expr: "1+", wantErr: true},
		{expr: "", wantErr: true},
	}
	for _, tt := range tests {
		got, err := Eval(tt.expr, 1280, 720)
		if tt.wantErr {
			if err == nil {
				t.Errorf("Eval(%q) expected error", tt.expr)
			}
			continue
		}
		if err != nil {
			t.Errorf("Eval(%q) unexpected error: %v", tt.expr, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Eval(%q) = %v, want %v", tt.expr, got, tt.want)
		}
	}
}
