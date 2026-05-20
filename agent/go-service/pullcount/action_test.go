package pullcount

import "testing"

// TestCalculatePullCount verifies the formula from issue #2147.
func TestCalculatePullCount(t *testing.T) {
	param, err := parseActionParam("")
	if err != nil {
		t.Fatalf("parseActionParam() error = %v", err)
	}

	got := calculatePullCount(resourceValues{
		SingleTicket: 3,
		TenTicket:    2,
		Originium:    2925,
		Oroberyl:     20770,
	}, param)

	if got.UsableOriginium != 2896 {
		t.Fatalf("usable originium = %d, want 2896", got.UsableOriginium)
	}
	if got.ConvertedOroberyl != 217200 {
		t.Fatalf("converted oroberyl = %d, want 217200", got.ConvertedOroberyl)
	}
	if got.ResourcePulls != 475 {
		t.Fatalf("resource pulls = %d, want 475", got.ResourcePulls)
	}
	if got.TicketPulls != 23 {
		t.Fatalf("ticket pulls = %d, want 23", got.TicketPulls)
	}
	if got.TotalPulls != 498 {
		t.Fatalf("total pulls = %d, want 498", got.TotalPulls)
	}
}

// TestCalculatePullCountClampsReservedOriginium verifies reserved originium never goes negative.
func TestCalculatePullCountClampsReservedOriginium(t *testing.T) {
	param, err := parseActionParam("")
	if err != nil {
		t.Fatalf("parseActionParam() error = %v", err)
	}

	got := calculatePullCount(resourceValues{
		SingleTicket: 0,
		TenTicket:    0,
		Originium:    20,
		Oroberyl:     499,
	}, param)

	if got.UsableOriginium != 0 {
		t.Fatalf("usable originium = %d, want 0", got.UsableOriginium)
	}
	if got.TotalPulls != 0 {
		t.Fatalf("total pulls = %d, want 0", got.TotalPulls)
	}
}

// TestParseIntegerText accepts compact OCR noise around a counter.
func TestParseIntegerText(t *testing.T) {
	got, err := parseIntegerText(" 20,770 |")
	if err != nil {
		t.Fatalf("parseIntegerText() error = %v", err)
	}
	if got != 20770 {
		t.Fatalf("parseIntegerText() = %d, want 20770", got)
	}
}
