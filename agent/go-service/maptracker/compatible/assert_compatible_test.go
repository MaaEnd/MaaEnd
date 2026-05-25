// Copyright (c) 2026 Harry Huang
package maptrackercompatible

import (
	"math"
	"testing"
)

func TestMapTrackerAssertLocationCompatibleRejectsInvalidParams(t *testing.T) {
	action := &MapTrackerAssertLocationCompatible{}
	tests := []struct {
		name  string
		param mapLocateAssertCompatibleParam
	}{
		{
			name:  "MissingZone",
			param: mapLocateAssertCompatibleParam{Target: [4]float64{1, 2, 3, 4}},
		},
		{
			name:  "InvalidRect",
			param: mapLocateAssertCompatibleParam{ZoneID: "Wuling_Base", Target: [4]float64{1, 2, 0, 4}},
		},
		{
			name:  "UnknownZone",
			param: mapLocateAssertCompatibleParam{ZoneID: "Unknown_Base", Target: [4]float64{1, 2, 3, 4}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := action.convertParam(&tt.param); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestMapTrackerAssertLocationCompatibleConvertsRegionalSamples(t *testing.T) {
	chdirCompatibleTestRepoRoot(t)

	tests := []struct {
		name       string
		param      mapLocateAssertCompatibleParam
		expectMap  string
		expectRect [4]float64
	}{
		{
			name:       "Wuling",
			param:      mapLocateAssertCompatibleParam{ZoneID: "Wuling_Base", Target: [4]float64{942, 723, 2, 1}},
			expectMap:  "map02_lv002",
			expectRect: [4]float64{664.200, 734.200, 2.075, 1.038},
		},
		{
			name:       "OMVBase",
			param:      mapLocateAssertCompatibleParam{ZoneID: "OMVBase01", Target: [4]float64{187, 131, 1, 10}},
			expectMap:  "base01_lv003",
			expectRect: [4]float64{190.200, 132.825, 1.038, 10.375},
		},
	}

	action := &MapTrackerAssertLocationCompatible{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converted, err := action.convertParam(&tt.param)
			if err != nil {
				t.Fatal(err)
			}
			if len(converted.Expected) != 1 {
				t.Fatalf("got %d conditions, want 1", len(converted.Expected))
			}
			condition := converted.Expected[0]
			if condition.MapName != tt.expectMap {
				t.Fatalf("got map %q, want %q", condition.MapName, tt.expectMap)
			}
			for i, expect := range tt.expectRect {
				if math.Abs(condition.Target[i]-expect) > 2.0 {
					t.Fatalf("target[%d] got %.3f, want %.3f", i, condition.Target[i], expect)
				}
			}
			if !converted.FastMode {
				t.Fatal("expected fast_mode")
			}
		})
	}
}
