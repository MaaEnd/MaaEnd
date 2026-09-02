package captureuid

import "testing"

func TestFormatUIDHashedUsesStableSaltedSha256(t *testing.T) {
	original := loadSaltFunc
	loadSaltFunc = func() (string, error) { return "0123456789abcdef0123456789abcdef", nil }
	t.Cleanup(func() { loadSaltFunc = original })

	got, err := formatUID("1234567890", OutputTypeHashed)
	if err != nil {
		t.Fatalf("formatUID returned error: %v", err)
	}
	const want = "052184dd51ec6feb"
	if got != want {
		t.Fatalf("formatUID = %q, want %q", got, want)
	}
}

func TestSafeUIDForLogMasksRawUID(t *testing.T) {
	if got, want := safeUIDForLog("1234567890", OutputTypeRaw), "123****890"; got != want {
		t.Fatalf("safeUIDForLog = %q, want %q", got, want)
	}
}
