// Copyright (c) 2026 Harry Huang
package maptrackercompatible

import (
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
