package ims

import (
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestEvaluateReady(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		hasData     bool
		lastSync    time.Time
		refreshDays int
		wantReady   bool
		wantReason  string
	}{
		{
			name:        "no data",
			hasData:     false,
			refreshDays: 7,
			wantReady:   false,
			wantReason:  "no_data",
		},
		{
			name:        "fresh within 7 days",
			hasData:     true,
			lastSync:    now.Add(-3 * 24 * time.Hour),
			refreshDays: 7,
			wantReady:   true,
		},
		{
			name:        "stale after 7 days",
			hasData:     true,
			lastSync:    now.Add(-8 * 24 * time.Hour),
			refreshDays: 7,
			wantReady:   false,
			wantReason:  "stale",
		},
		{
			name:        "never expire with data",
			hasData:     true,
			lastSync:    now.Add(-365 * 24 * time.Hour),
			refreshDays: 0,
			wantReady:   true,
		},
		{
			name:        "never expire without data",
			hasData:     false,
			refreshDays: 0,
			wantReady:   false,
			wantReason:  "no_data",
		},
		{
			name:        "boundary exactly ttl still ready",
			hasData:     true,
			lastSync:    now.Add(-7 * 24 * time.Hour),
			refreshDays: 7,
			wantReady:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ready, reason := evaluateReady(tt.hasData, tt.lastSync, tt.refreshDays, now)
			if ready != tt.wantReady {
				t.Fatalf("ready=%v, want %v", ready, tt.wantReady)
			}
			if reason != tt.wantReason {
				t.Fatalf("reason=%q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestResolveRefreshDays(t *testing.T) {
	if got, err := resolveRefreshDays(nil); err != nil || got != 7 {
		t.Fatalf("nil default: got=%d err=%v", got, err)
	}
	zero := 0
	if got, err := resolveRefreshDays(&zero); err != nil || got != 0 {
		t.Fatalf("zero: got=%d err=%v", got, err)
	}
	bad := 14
	if _, err := resolveRefreshDays(&bad); err == nil {
		t.Fatal("expected error for refresh_days=14")
	}
}

func TestItemDataReadyRun(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	r := &ItemDataReady{}
	arg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"refresh_days":7}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}
	if _, ok := r.Run(nil, arg); ok {
		t.Fatal("expected not ready without sync")
	}

	MarkSynced(time.Now(), map[string]int{"dummy": 1})
	result, ok := r.Run(nil, arg)
	if !ok {
		t.Fatal("expected ready after sync")
	}
	if result == nil || result.Detail == "" {
		t.Fatal("expected detail json")
	}
}
