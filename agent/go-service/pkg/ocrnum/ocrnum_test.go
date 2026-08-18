package ocrnum

import (
	"math"
	"testing"
)

func TestParse(t *testing.T) {
	testCases := []struct {
		name    string
		text    string
		want    int
		wantErr bool
	}{
		{
			name: "plain integer",
			text: "138",
			want: 138,
		},
		{
			name: "embedded digits",
			text: "x12",
			want: 12,
		},
		{
			name: "chinese ten thousand suffix",
			text: "1.38万",
			want: 13800,
		},
		{
			name: "traditional chinese ten thousand suffix",
			text: "1.38萬",
			want: 13800,
		},
		{
			name: "korean ten thousand suffix",
			text: "1.38만",
			want: 13800,
		},
		{
			name: "traditional chinese hundred million suffix",
			text: "1.2億",
			want: 120000000,
		},
		{
			name: "korean hundred million suffix",
			text: "1.2억",
			want: 120000000,
		},
		{
			name: "western thousand suffix",
			text: "1.8k",
			want: 1800,
		},
		{
			name: "western million suffix",
			text: "12m",
			want: 12000000,
		},
		{
			name: "western thousand suffix uppercase",
			text: "13.8K",
			want: 13800,
		},
		{
			name: "western million suffix uppercase",
			text: "22.01M",
			want: 22010000,
		},
		{
			name: "decimal comma suffix",
			text: "13,8K",
			want: 13800,
		},
		{
			name:    "unsupported w suffix",
			text:    "1.2W",
			wantErr: true,
		},
		{
			name: "embedded numeric token",
			text: "约 1.38万",
			want: 13800,
		},
		{
			name:    "invalid text",
			text:    "abc",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.text)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Parse(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}

func TestParseClampsOverflow(t *testing.T) {
	got, err := Parse("99999999999999999999999999999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != math.MaxInt {
		t.Fatalf("Parse() = %d, want %d", got, math.MaxInt)
	}
}
