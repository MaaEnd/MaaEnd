// Copyright (c) 2026 MaaEnd Contributors
package maptrackerbigmap

import "testing"

func TestBackfillMatchMapNamesPreservesViewportMap(t *testing.T) {
	matches := []MapTrackerBigMapFindImageMatch{
		{MapName: "map01_lv001"},
		{},
	}

	backfillMatchMapNames(matches, "map02_lv002")

	if matches[0].MapName != "map01_lv001" {
		t.Fatalf("existing MapName = %q, want %q", matches[0].MapName, "map01_lv001")
	}
	if matches[1].MapName != "map02_lv002" {
		t.Fatalf("backfilled MapName = %q, want %q", matches[1].MapName, "map02_lv002")
	}
}

func TestFindImageExpectedLocationUsesMatchMapName(t *testing.T) {
	expected := mapTrackerBigMapFindImageExpected{
		mode:    findImageExpectedModeLocation,
		mapName: "map02_lv002",
		target:  [4]float64{100, 200, 20, 20},
	}

	wrongMapMatch := []MapTrackerBigMapFindImageMatch{
		{MapName: "map02_lv005", MapX: 110, MapY: 210},
	}
	if expected.isSatisfied("map02_lv002", wrongMapMatch) {
		t.Fatal("match from another map unexpectedly satisfied the location condition")
	}

	correctMapMatch := []MapTrackerBigMapFindImageMatch{
		{MapName: "map02_lv002", MapX: 110, MapY: 210},
	}
	if !expected.isSatisfied("map02_lv005", correctMapMatch) {
		t.Fatal("match from the expected map did not satisfy the location condition")
	}
}

func TestDeduplicateMatchesKeepsSameScreenPositionOnDifferentMaps(t *testing.T) {
	matches := []MapTrackerBigMapFindImageMatch{
		{MapName: "map01_lv001", ScreenX: 100, ScreenY: 200, Conf: 0.9},
		{MapName: "map02_lv002", ScreenX: 100, ScreenY: 200, Conf: 0.8},
	}

	result := deduplicateMatches(matches)
	if len(result) != 2 {
		t.Fatalf("deduplicated match count = %d, want 2", len(result))
	}
}
