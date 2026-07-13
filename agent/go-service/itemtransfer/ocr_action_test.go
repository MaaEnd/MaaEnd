package itemtransfer

import "testing"

func TestSelectBagRankingTemplateNodePrefersBagThenRepo(t *testing.T) {
	bagRuntime := `{"template":"__MaaEndRuntimeImageCacheV1__/ItemTransfer/Items/A/bag.png"}`
	repoRuntime := `{"recognition":{"param":{"template":["__MaaEndRuntimeImageCacheV1__/ItemTransfer/Items/A/repo.png"]}}}`
	static := `{"template":"ItemTransfer/All.png"}`

	if got := selectBagRankingTemplateNode(bagRuntime, repoRuntime); got != itemCacheBagNode {
		t.Fatalf("bag runtime selection=%q want=%q", got, itemCacheBagNode)
	}
	if got := selectBagRankingTemplateNode(static, repoRuntime); got != itemCacheRepoNode {
		t.Fatalf("repo fallback selection=%q want=%q", got, itemCacheRepoNode)
	}
	if got := selectBagRankingTemplateNode(static, `{}`); got != "" {
		t.Fatalf("no runtime template selection=%q want empty", got)
	}
}

func TestRuntimeItemCacheImageNameSupportsFlatAndNestedNodeJSON(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{
			raw:  `{"template":"__MaaEndRuntimeImageCacheV1__/ItemTransfer/Items/A/bag.png"}`,
			want: "__MaaEndRuntimeImageCacheV1__/ItemTransfer/Items/A/bag.png",
		},
		{
			raw:  `{"recognition":{"param":{"template":["__MaaEndRuntimeImageCacheV1__/ItemTransfer/Items/A/repo.png"]}}}`,
			want: "__MaaEndRuntimeImageCacheV1__/ItemTransfer/Items/A/repo.png",
		},
		{raw: `{"template":"ItemTransfer/All.png"}`, want: ""},
		{raw: `{invalid`, want: ""},
	}
	for _, tt := range tests {
		if got := runtimeItemCacheImageName(tt.raw); got != tt.want {
			t.Fatalf("runtimeItemCacheImageName(%s)=%q want=%q", tt.raw, got, tt.want)
		}
	}
}

func TestRankGridItemsByScoresUsesStableDescendingOrder(t *testing.T) {
	items := []gridItem{
		{CenterX: 1},
		{CenterX: 2},
		{CenterX: 3},
		{CenterX: 4},
	}
	got := rankGridItemsByScores(items, []float64{0.5, 0.9, 0.9, -1})
	want := []int{2, 3, 1, 4}
	for i, item := range got {
		if item.CenterX != want[i] {
			t.Fatalf("ranked[%d].CenterX=%d want=%d", i, item.CenterX, want[i])
		}
	}
	if items[0].CenterX != 1 {
		t.Fatal("rankGridItemsByScores modified input slice")
	}
}

func TestBuildSyntheticBagGridCoversFiveByFourFromFirstColumn(t *testing.T) {
	grid := buildSyntheticGrid("bag", bagCols)
	if len(grid) != 20 {
		t.Fatalf("bag grid count=%d want=20", len(grid))
	}
	if grid[0].CenterX != 802 || grid[0].CenterY != 247 {
		t.Fatalf("bag first center=(%d,%d) want=(802,247)", grid[0].CenterX, grid[0].CenterY)
	}
	last := grid[len(grid)-1]
	if last.CenterX != 1078 || last.CenterY != 454 {
		t.Fatalf("bag last center=(%d,%d) want=(1078,454)", last.CenterX, last.CenterY)
	}
}

func TestBuildOCRSearchGridAlwaysUsesTwentyBagCells(t *testing.T) {
	raw := []gridItem{
		{CenterX: 802, CenterY: 247},
		{CenterX: 871, CenterY: 316},
	}
	grid := buildOCRSearchGrid(raw, bagCols, "bag")
	if len(grid) != 20 {
		t.Fatalf("bag OCR grid count=%d want=20", len(grid))
	}
}

func TestItemCacheGridMatchROIAllowsFourPixelAlignment(t *testing.T) {
	got := itemCacheGridMatchROI(gridItem{CenterX: 802, CenterY: 247})
	want := [4]int{772, 218, 60, 48}
	if got != want {
		t.Fatalf("itemCacheGridMatchROI()=%v want=%v", got, want)
	}
}
