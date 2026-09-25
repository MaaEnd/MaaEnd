package essencefilter

import (
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
)

func TestExplicitInputLanguage(t *testing.T) {
	cases := []struct {
		in      string
		wantLoc string
		wantOK  bool
	}{
		{"", "", false},
		{"AUTO", "", false},
		{"auto", "", false},
		{"CN", matchapi.LocaleCN, true},
		{" cn ", matchapi.LocaleCN, true},
		{"TC", matchapi.LocaleTC, true},
		{"EN", matchapi.LocaleEN, true},
		{"JP", matchapi.LocaleJP, true},
		{"KR", matchapi.LocaleKR, true},
		{"xx", "", false},
	}
	for _, tc := range cases {
		loc, ok := explicitInputLanguage(tc.in)
		if ok != tc.wantOK || loc != tc.wantLoc {
			t.Fatalf("explicitInputLanguage(%q)=(%q,%v), want (%q,%v)", tc.in, loc, ok, tc.wantLoc, tc.wantOK)
		}
	}
}
