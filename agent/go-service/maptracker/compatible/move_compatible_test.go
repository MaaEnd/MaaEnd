// Copyright (c) 2026 Harry Huang
package maptrackercompatible

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func moveCompatibleTestRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
}

func chdirCompatibleTestRepoRoot(t *testing.T) {
	t.Helper()
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(moveCompatibleTestRepoRoot(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldCwd); err != nil {
			t.Fatal(err)
		}
	})
}

func mustRawMessages(t *testing.T, values ...any) []json.RawMessage {
	t.Helper()
	result := make([]json.RawMessage, 0, len(values))
	for _, value := range values {
		content, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, content)
	}
	return result
}

func TestResolveCompatibleSourceMapPadsTierLevel(t *testing.T) {
	source, err := resolveCompatibleSourceMap("ValleyIV_L1_114", "")
	if err != nil {
		t.Fatal(err)
	}
	if source.LocatorFile != "Lv001Tier114.png" {
		t.Fatalf("got locator file %q", source.LocatorFile)
	}
	if source.CandidateMap != "map01_lv001_tier_114" {
		t.Fatalf("got candidate map %q", source.CandidateMap)
	}
}

func TestMapTrackerMoveCompatibleRejectsEmptyOrUnknownRoutes(t *testing.T) {
	chdirCompatibleTestRepoRoot(t)

	tests := []struct {
		name  string
		param mapNavigateCompatibleParam
	}{
		{
			name: "NavmeshOnly",
			param: mapNavigateCompatibleParam{
				Path: mustRawMessages(t,
					map[string]any{"action": "ZONE", "zone_id": "Wuling_Base"},
					map[string]any{"action": "NAVMESH", "target": []any{942.0, 723.0}},
				),
			},
		},
		{
			name: "HeadingOnly",
			param: mapNavigateCompatibleParam{
				Path: mustRawMessages(t, map[string]any{"action": "HEADING", "angle": 90.0}),
			},
		},
		{
			name: "UnknownMap",
			param: mapNavigateCompatibleParam{
				Path: mustRawMessages(t,
					map[string]any{"action": "ZONE", "zone_id": "Unknown_Base"},
					[]any{1.0, 2.0},
				),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (&MapTrackerMoveCompatible{}).convertParam(&tt.param); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestMapTrackerMoveCompatibleConvertsRegionalSamples(t *testing.T) {
	chdirCompatibleTestRepoRoot(t)

	tests := []struct {
		name    string
		param   mapNavigateCompatibleParam
		expects [][2]float64
	}{
		{
			name: "ValleyIV",
			param: mapNavigateCompatibleParam{
				Path: mustRawMessages(t,
					map[string]any{"action": "ZONE", "zone_id": "ValleyIV_Base"},
					[]any{532.0, 697.0},
					[]any{574.0, 735.0},
				),
			},
			expects: [][2]float64{
				{478.960, 267.960},
				{524.845, 309.475},
			},
		},
		{
			name: "Wuling",
			param: mapNavigateCompatibleParam{
				MapName: "map02_lv002",
				Path: mustRawMessages(t,
					map[string]any{"action": "ZONE", "zone_id": "Wuling_Base"},
					[]any{942.0, 723.0},
					[]any{944.0, 723.0},
				),
			},
			expects: [][2]float64{
				{664.200, 734.200},
				{666.275, 734.200},
			},
		},
		{
			name: "OMVBase",
			param: mapNavigateCompatibleParam{
				Path: mustRawMessages(t,
					map[string]any{"action": "ZONE", "zone_id": "OMVBase01"},
					[]any{187.0, 141.0},
					[]any{187.0, 131.0},
				),
			},
			expects: [][2]float64{
				{190.200, 143.200},
				{190.200, 132.825},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converted, err := (&MapTrackerMoveCompatible{}).convertParam(&tt.param)
			if err != nil {
				t.Fatal(err)
			}
			if len(converted.Path) != len(tt.expects) {
				t.Fatalf("got %d points, want %d", len(converted.Path), len(tt.expects))
			}
			for i, expect := range tt.expects {
				got := converted.Path[i]
				dist := math.Hypot(got[0]-expect[0], got[1]-expect[1])
				if dist > 2.0 {
					t.Fatalf("point %d got [%.3f, %.3f], want [%.3f, %.3f], dist %.3f", i, got[0], got[1], expect[0], expect[1], dist)
				}
			}
		})
	}
}

func TestParseCompatibleTrackerMapName(t *testing.T) {
	tests := []struct {
		mapName         string
		wantPrefix      string
		wantRegion      string
		wantLocatorFile string
		wantCandidate   string
		wantIsBase      bool
		wantOK          bool
	}{
		{
			mapName:         "map01_lv001",
			wantPrefix:      "map01",
			wantRegion:      "ValleyIV",
			wantLocatorFile: "Base.png",
			wantCandidate:   "map01_lv001",
			wantIsBase:      true,
			wantOK:          true,
		},
		{
			mapName:         "map01_lv001_tier_114",
			wantPrefix:      "map01",
			wantRegion:      "ValleyIV",
			wantLocatorFile: "Lv001Tier114.png",
			wantCandidate:   "map01_lv001_tier_114",
			wantIsBase:      false,
			wantOK:          true,
		},
		{
			mapName:         "map02_lv005_tier_321",
			wantPrefix:      "map02",
			wantRegion:      "Wuling",
			wantLocatorFile: "Lv005Tier321.png",
			wantCandidate:   "map02_lv005_tier_321",
			wantIsBase:      false,
			wantOK:          true,
		},
		{
			mapName:         "base01_lv003",
			wantPrefix:      "base01",
			wantRegion:      "OMVBase",
			wantLocatorFile: "Base.png",
			wantCandidate:   "base01_lv003",
			wantIsBase:      true,
			wantOK:          true,
		},
		{
			mapName:    "unknown_lv001",
			wantPrefix: "",
			wantOK:     false,
		},
		{
			mapName: "",
			wantOK:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.mapName, func(t *testing.T) {
			got, ok := parseCompatibleTrackerMapName(tt.mapName)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got.MapPrefix != tt.wantPrefix {
				t.Fatalf("prefix = %q, want %q", got.MapPrefix, tt.wantPrefix)
			}
			if got.Region != tt.wantRegion {
				t.Fatalf("region = %q, want %q", got.Region, tt.wantRegion)
			}
			if got.LocatorFile != tt.wantLocatorFile {
				t.Fatalf("locator_file = %q, want %q", got.LocatorFile, tt.wantLocatorFile)
			}
			if got.CandidateMap != tt.wantCandidate {
				t.Fatalf("candidate = %q, want %q", got.CandidateMap, tt.wantCandidate)
			}
			if got.IsBase != tt.wantIsBase {
				t.Fatalf("is_base = %v, want %v", got.IsBase, tt.wantIsBase)
			}
		})
	}
}
