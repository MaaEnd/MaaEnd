package stashbackpack

import "testing"

func TestWarningMessageKey(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		reason string
		want   string
	}{
		{reason: warningDuplicateFull, want: "stashbackpack.warning.duplicate_full"},
		{reason: warningMissingFullSnapshot, want: "stashbackpack.warning.missing_full_snapshot"},
		{reason: warningMissingRetrieveSnapshot, want: "stashbackpack.warning.missing_retrieve_snapshot"},
		{reason: warningUnsupportedPlatform, want: "stashbackpack.warning.unsupported_platform"},
	} {
		raw := `{"reason":"` + testCase.reason + `"}`
		got, err := warningMessageKey(raw)
		if err != nil {
			t.Fatalf("warningMessageKey(%q): %v", testCase.reason, err)
		}
		if got != testCase.want {
			t.Errorf("warningMessageKey(%q) = %q, want %q", testCase.reason, got, testCase.want)
		}
	}
}

func TestWarningMessageKeyRejectsUnknownReason(t *testing.T) {
	t.Parallel()
	if _, err := warningMessageKey(`{"reason":"unknown"}`); err == nil {
		t.Fatal("warningMessageKey() accepted an unknown reason")
	}
}
