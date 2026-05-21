package pullcount

import (
	"reflect"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// TestCalculatePullCount verifies the recruitment screen resource formula from issue #2147.
func TestCalculatePullCount(t *testing.T) {
	param, err := parseActionParam("")
	if err != nil {
		t.Fatalf("parseActionParam() error = %v", err)
	}

	summary := voucherSummary{
		CurrentOnlyPulls: 2,
		CarryToNextPulls: 3,
		NextOnlyPulls:    10,
	}
	got := calculatePullCount(resourceValues{
		ConvertedOriginiumOroberyl: 2925,
		Oroberyl:                   20770,
	}, summary, param)

	if got.ReservedOriginiumOroberyl != 2175 {
		t.Fatalf("reserved originium oroberyl = %d, want 2175", got.ReservedOriginiumOroberyl)
	}
	if got.UsableOriginiumOroberyl != 750 {
		t.Fatalf("usable originium oroberyl = %d, want 750", got.UsableOriginiumOroberyl)
	}
	if got.ResourcePulls != 43 {
		t.Fatalf("resource pulls = %d, want 43", got.ResourcePulls)
	}
	if got.CurrentPoolTotal != 48 {
		t.Fatalf("current pool total = %d, want 48", got.CurrentPoolTotal)
	}
	if got.NextPoolTotal != 66 {
		t.Fatalf("next pool total = %d, want 66", got.NextPoolTotal)
	}
}

// TestCalculatePullCountClampsReservedOriginium verifies reserved originium never goes negative.
func TestCalculatePullCountClampsReservedOriginium(t *testing.T) {
	param, err := parseActionParam("")
	if err != nil {
		t.Fatalf("parseActionParam() error = %v", err)
	}

	got := calculatePullCount(resourceValues{
		ConvertedOriginiumOroberyl: 2000,
		Oroberyl:                   499,
	}, voucherSummary{}, param)

	if got.UsableOriginiumOroberyl != 0 {
		t.Fatalf("usable originium oroberyl = %d, want 0", got.UsableOriginiumOroberyl)
	}
	if got.ResourcePulls != 0 {
		t.Fatalf("resource pulls = %d, want 0", got.ResourcePulls)
	}
	if got.NextPoolTotal != 10 {
		t.Fatalf("next pool total = %d, want fixed 10", got.NextPoolTotal)
	}
}

// TestSummarizeVouchers verifies voucher weights and pool scopes.
func TestSummarizeVouchers(t *testing.T) {
	config := &voucherConfig{Vouchers: []voucherDef{
		{Name: "当前单抽券", Names: []string{"当前单抽券别名1", "当前单抽券别名2"}, PullValue: 1, PoolScope: "current_only"},
		{Name: "通用单抽券", PullValue: 1, PoolScope: "carry_to_next"},
		{Name: "基础寻访凭证", PullValue: 1, PoolScope: "ignore"},
		{Name: "寻访情报书", Names: []string{"尋訪情報書"}, PullValue: 10, PoolScope: "next_only"},
	}}

	got, err := summarizeVouchers([]scannedVoucher{
		{Name: "当前单抽券别名2", Quantity: 2},
		{Name: "通用单抽券", Quantity: 3},
		{Name: "基础寻访凭证", Quantity: 99},
		{Name: "尋訪情報書", Quantity: 1},
		{Name: "未配置十连寻访凭证", Quantity: 1},
		{Name: "未配置寻访凭证", Quantity: 7},
		{Name: "无关物品", Quantity: 99},
	}, config)
	if err != nil {
		t.Fatalf("summarizeVouchers() error = %v", err)
	}

	if got.CurrentOnlyPulls != 2 {
		t.Fatalf("current only pulls = %d, want 2", got.CurrentOnlyPulls)
	}
	if got.CarryToNextPulls != 3 {
		t.Fatalf("carry to next pulls = %d, want 3", got.CarryToNextPulls)
	}
	if got.NextOnlyPulls != 10 {
		t.Fatalf("next only pulls = %d, want 10", got.NextOnlyPulls)
	}
	if len(got.Matches) != 4 {
		t.Fatalf("matches length = %d, want 4", len(got.Matches))
	}
	assertVoucherMatch(t, got.Matches, voucherMatch{
		CanonicalName: "当前单抽券",
		DisplayName:   "当前单抽券别名2",
		PullValue:     1,
		PoolScope:     "current_only",
		Quantity:      2,
		Pulls:         2,
	})
	assertVoucherMatch(t, got.Matches, voucherMatch{
		CanonicalName: "通用单抽券",
		DisplayName:   "通用单抽券",
		PullValue:     1,
		PoolScope:     "carry_to_next",
		Quantity:      3,
		Pulls:         3,
	})
	assertVoucherMatch(t, got.Matches, voucherMatch{
		CanonicalName: "基础寻访凭证",
		DisplayName:   "基础寻访凭证",
		PullValue:     1,
		PoolScope:     "ignore",
		Quantity:      99,
		Pulls:         0,
	})
	assertVoucherMatch(t, got.Matches, voucherMatch{
		CanonicalName: "寻访情报书",
		DisplayName:   "尋訪情報書",
		PullValue:     10,
		PoolScope:     "next_only",
		Quantity:      1,
		Pulls:         10,
	})
}

// TestBuildVoucherIndexRejectsDuplicateAliases verifies conflicting voucher aliases fail fast.
func TestBuildVoucherIndexRejectsDuplicateAliases(t *testing.T) {
	config := &voucherConfig{Vouchers: []voucherDef{
		{Name: "通用单抽券", Names: []string{"寻访券"}, PullValue: 1, PoolScope: "carry_to_next"},
		{Name: "限时单抽券", Names: []string{"寻访券"}, PullValue: 1, PoolScope: "current_only"},
	}}

	if _, err := buildVoucherIndex(config); err == nil {
		t.Fatal("buildVoucherIndex() error = nil, want duplicate alias error")
	}
}

// TestBuildVoucherIndexAllowsDuplicateAliasesWithinSameVoucher verifies repeated names on one voucher are harmless.
func TestBuildVoucherIndexAllowsDuplicateAliasesWithinSameVoucher(t *testing.T) {
	config := &voucherConfig{Vouchers: []voucherDef{
		{Name: "通用单抽券", Names: []string{"通用单抽券", " 通 用 单 抽 券 "}, PullValue: 1, PoolScope: "carry_to_next"},
	}}

	index, err := buildVoucherIndex(config)
	if err != nil {
		t.Fatalf("buildVoucherIndex() error = %v", err)
	}
	if _, ok := index[normalizeName("通用单抽券")]; !ok {
		t.Fatal("buildVoucherIndex() missing canonical voucher key")
	}
}

// TestParseIntegerText accepts compact OCR noise around a counter.
func TestParseIntegerText(t *testing.T) {
	cases := map[string]int{
		" 20,770 |": 20770,
		"20770 1":   20770,
		"x 123 y":   123,
		"abc 456":   456,
		"987654321": 987654321,
	}

	for text, want := range cases {
		got, err := parseIntegerText(text)
		if err != nil {
			t.Fatalf("parseIntegerText(%q) error = %v", text, err)
		}
		if got != want {
			t.Fatalf("parseIntegerText(%q) = %d, want %d", text, got, want)
		}
	}
}

// TestParseIntegerTextRejectsInputsWithoutDigits verifies OCR text must contain a counter.
func TestParseIntegerTextRejectsInputsWithoutDigits(t *testing.T) {
	cases := []string{
		"abc",
		" | ",
		"",
	}

	for _, text := range cases {
		if got, err := parseIntegerText(text); err == nil {
			t.Fatalf("parseIntegerText(%q) = %d, want error", text, got)
		}
	}
}

// TestCurrentPageCellsUsesOneRecordPerCell verifies repeated OCR on one cell is de-duplicated.
func TestCurrentPageCellsUsesOneRecordPerCell(t *testing.T) {
	session := newTestSession()
	recordPageQuantity(session, 1, 7)
	recordPageItem(session, 1, "普通物品")
	recordPageItem(session, 1, "普通物品噪声")

	items := currentPageItems(session)
	if len(items) != 1 {
		t.Fatalf("currentPageItems() length = %d, want 1: %#v", len(items), items)
	}
	if items[0].Cell != 1 || items[0].Quantity != 7 || !items[0].HasQuantity {
		t.Fatalf("currentPageItems()[0] = %+v, want cell 1 quantity 7", items[0])
	}
}

// TestGeometryForCell verifies 720p warehouse grid coordinates.
func TestGeometryForCell(t *testing.T) {
	config := testWarehouseScanConfig()
	cases := map[int]cellGeometry{
		1: {
			Cell:        1,
			PresentROI:  []int{44, 127, 82, 82},
			ClickTarget: []int{85, 168, 1, 1},
			QuantityROI: []int{61, 186, 58, 32},
			TitleROI:    []int{980, 82, 250, 48},
		},
		9: {
			Cell:        9,
			PresentROI:  []int{868, 127, 82, 82},
			ClickTarget: []int{909, 168, 1, 1},
			QuantityROI: []int{885, 186, 58, 32},
			TitleROI:    []int{980, 82, 250, 48},
		},
		10: {
			Cell:        10,
			PresentROI:  []int{44, 230, 82, 82},
			ClickTarget: []int{85, 271, 1, 1},
			QuantityROI: []int{61, 289, 58, 32},
			TitleROI:    []int{980, 82, 250, 48},
		},
		45: {
			Cell:        45,
			PresentROI:  []int{868, 539, 82, 82},
			ClickTarget: []int{909, 580, 1, 1},
			QuantityROI: []int{885, 598, 58, 32},
			TitleROI:    []int{980, 82, 250, 48},
		},
	}

	for cell, want := range cases {
		got, err := geometryForCell(config, cell)
		if err != nil {
			t.Fatalf("geometryForCell(%d) error = %v", cell, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("geometryForCell(%d) = %+v, want %+v", cell, got, want)
		}
	}
}

// TestGeometryForCellRejectsOutOfRange verifies invalid cell indices fail fast.
func TestGeometryForCellRejectsOutOfRange(t *testing.T) {
	config := testWarehouseScanConfig()
	for _, cell := range []int{0, 46} {
		if got, err := geometryForCell(config, cell); err == nil {
			t.Fatalf("geometryForCell(%d) = %+v, want error", cell, got)
		}
	}
}

// TestBuildCellScanOverride verifies one concrete cell is patched into generic Pipeline nodes.
func TestBuildCellScanOverride(t *testing.T) {
	config := testWarehouseScanConfig()
	geometry, err := geometryForCell(config, 10)
	if err != nil {
		t.Fatalf("geometryForCell() error = %v", err)
	}

	got := buildCellScanOverride(geometry, config.TitleExpected)
	assertMapValue(t, got[nodeCellPresent], "roi", []int{44, 230, 82, 82})
	assertMapValue(t, got[nodeCellClick], "target", []int{85, 271, 1, 1})
	assertMapValue(t, got[nodeCellQuantityOCR], "roi", []int{61, 289, 58, 32})
	assertMapValue(t, got[nodeCellTitleOCR], "roi", []int{980, 82, 250, 48})
	assertMapValue(t, got[nodeCellTitleOCR], "expected", []string{".*(寻访|recruit).*"})
	assertMapValue(t, got[nodeCellQuantityOCR], "custom_action_param", map[string]any{"stage": "record_quantity", "cell": 10})
	assertMapValue(t, got[nodeCellTitleOCR], "custom_action_param", map[string]any{"stage": "record_item", "cell": 10})
}

// TestBuildProbeOverride verifies the scroll probe reuses cell quantity ROI with probe params.
func TestBuildProbeOverride(t *testing.T) {
	geometry, err := geometryForCell(testWarehouseScanConfig(), 9)
	if err != nil {
		t.Fatalf("geometryForCell() error = %v", err)
	}

	got := buildProbeOverride(geometry)
	assertMapValue(t, got[nodeProbeQuantity], "roi", []int{885, 186, 58, 32})
	assertMapValue(t, got[nodeProbeQuantity], "custom_action_param", map[string]any{"stage": "record_probe_quantity", "cell": 9})
}

// TestScrollProbeUnchangedStopsWhenTopQuantitiesMatch verifies bottom detection from Pipeline OCR probes.
func TestScrollProbeUnchangedStopsWhenTopQuantitiesMatch(t *testing.T) {
	rule := testWarehouseScanConfig().Probe
	before := map[int]int{1: 30, 2: 80, 3: 80, 4: 135}
	after := map[int]int{1: 30, 2: 80, 3: 80, 4: 135, 5: 358}

	unchanged, comparable, matches := scrollProbeUnchanged(rule, before, after)
	if !unchanged {
		t.Fatalf("scrollProbeUnchanged() unchanged = false, want true")
	}
	if comparable != 4 || matches != 4 {
		t.Fatalf("scrollProbeUnchanged() comparable/matches = %d/%d, want 4/4", comparable, matches)
	}
}

// TestScrollProbeUnchangedAllowsOneOcrNoise verifies a single OCR mismatch still stops at bottom.
func TestScrollProbeUnchangedAllowsOneOcrNoise(t *testing.T) {
	rule := testWarehouseScanConfig().Probe
	before := map[int]int{1: 30, 2: 80, 3: 80, 4: 135, 5: 358, 6: 4, 7: 10, 8: 2, 9: 2}
	after := map[int]int{1: 30, 2: 8, 3: 80, 4: 135, 5: 358, 6: 4, 7: 10, 8: 2, 9: 2}

	unchanged, comparable, matches := scrollProbeUnchanged(rule, before, after)
	if !unchanged {
		t.Fatalf("scrollProbeUnchanged() unchanged = false, want true for 8/9 match")
	}
	if comparable != 9 || matches != 8 {
		t.Fatalf("scrollProbeUnchanged() comparable/matches = %d/%d, want 9/8", comparable, matches)
	}
}

// TestScrollProbeUnchangedRejectsTooManyMismatches verifies noisy but changed pages keep scanning.
func TestScrollProbeUnchangedRejectsTooManyMismatches(t *testing.T) {
	rule := testWarehouseScanConfig().Probe
	before := map[int]int{1: 30, 2: 80, 3: 80, 4: 135, 5: 358, 6: 4, 7: 10, 8: 2, 9: 2}
	after := map[int]int{1: 30, 2: 8, 3: 8, 4: 135, 5: 358, 6: 4, 7: 10, 8: 2, 9: 2}

	unchanged, comparable, matches := scrollProbeUnchanged(rule, before, after)
	if unchanged {
		t.Fatalf("scrollProbeUnchanged() unchanged = true, want false for 7/9 match")
	}
	if comparable != 9 || matches != 7 {
		t.Fatalf("scrollProbeUnchanged() comparable/matches = %d/%d, want 9/7", comparable, matches)
	}
}

// TestScrollProbeUnchangedNeedsEnoughSignal verifies weak probes fall back to full scanning.
func TestScrollProbeUnchangedNeedsEnoughSignal(t *testing.T) {
	rule := testWarehouseScanConfig().Probe
	before := map[int]int{1: 30, 2: 80, 3: 80}
	after := map[int]int{1: 30, 2: 80, 3: 80}

	unchanged, comparable, matches := scrollProbeUnchanged(rule, before, after)
	if unchanged {
		t.Fatalf("scrollProbeUnchanged() unchanged = true, want false for weak signal")
	}
	if comparable != 3 || matches != 3 {
		t.Fatalf("scrollProbeUnchanged() comparable/matches = %d/%d, want 3/3", comparable, matches)
	}
}

// assertVoucherMatch verifies one expected voucher classification result.
func assertVoucherMatch(t *testing.T, matches []voucherMatch, want voucherMatch) {
	t.Helper()
	for _, got := range matches {
		if got.CanonicalName != want.CanonicalName {
			continue
		}
		if got != want {
			t.Fatalf("voucher match for %q = %+v, want %+v", want.CanonicalName, got, want)
		}
		return
	}
	t.Fatalf("voucher match for %q not found in %+v", want.CanonicalName, matches)
}

// assertMapValue verifies a nested override node value.
func assertMapValue(t *testing.T, node any, key string, want any) {
	t.Helper()
	values, ok := node.(map[string]any)
	if !ok {
		t.Fatalf("node type = %T, want map[string]any", node)
	}
	if got := values[key]; !reflect.DeepEqual(got, want) {
		t.Fatalf("node[%q] = %#v, want %#v", key, got, want)
	}
}

// TestReadIntegerFromRecognitionDetailJSON verifies Pipeline OCR text can be parsed from raw detail JSON.
func TestReadIntegerFromRecognitionDetailJSON(t *testing.T) {
	detail := &maa.RecognitionDetail{
		Hit:        true,
		DetailJson: `{"best":{"text":"20,770"}}`,
	}

	got, err := readIntegerFromRecognition(detail)
	if err != nil {
		t.Fatalf("readIntegerFromRecognition() error = %v", err)
	}
	if got != 20770 {
		t.Fatalf("readIntegerFromRecognition() = %d, want 20770", got)
	}
}

// TestOCRTextCandidatesReadsNestedDetails verifies And-node child OCR results are visible to actions.
func TestOCRTextCandidatesReadsNestedDetails(t *testing.T) {
	detail := &maa.RecognitionDetail{
		Hit: true,
		CombinedResult: []*maa.RecognitionDetail{
			{Name: "quantity", Hit: true, DetailJson: `{"best":{"text":"12"}}`},
			{Name: "title", Hit: true, DetailJson: `{"best":{"text":" 寻访情报书 "}}`},
		},
	}

	texts := ocrTextCandidates(detail)
	if len(texts) != 2 {
		t.Fatalf("ocrTextCandidates() length = %d, want 2: %#v", len(texts), texts)
	}
	if texts[0] != "12" || texts[1] != "寻访情报书" {
		t.Fatalf("ocrTextCandidates() = %#v, want [12 寻访情报书]", texts)
	}
}

// TestRecordVisiblePageAccumulatesVouchers verifies page recording leaves flow decisions to Pipeline.
func TestRecordVisiblePageAccumulatesVouchers(t *testing.T) {
	session := newTestSession()
	recordPageQuantity(session, 1, 1)
	recordPageItem(session, 1, "寻访情报书")
	recordPageQuantity(session, 2, 3)
	recordPageItem(session, 2, "普通物品")

	got := recordVisiblePage(session)
	if session.PageCount != 1 {
		t.Fatalf("page count = %d, want 1", session.PageCount)
	}
	if got != 2 {
		t.Fatalf("recordVisiblePage() = %d, want 2", got)
	}
	if session.VoucherQuantities["寻访情报书"] != 1 {
		t.Fatalf("voucher quantity = %d, want 1", session.VoucherQuantities["寻访情报书"])
	}
}

// TestRecordVisiblePageStopsOnRepeatedQuantitySignature verifies the full-page fallback catches repeated pages.
func TestRecordVisiblePageStopsOnRepeatedQuantitySignature(t *testing.T) {
	session := newTestSession()
	session.LastPageSignature = map[int]int{1: 30, 2: 80, 3: 80, 4: 135, 5: 358, 6: 4, 7: 10, 8: 2}
	session.CurrentPageCells = map[int]scannedCell{
		1: {Quantity: 30, HasQuantity: true},
		2: {Quantity: 80, HasQuantity: true},
		3: {Quantity: 80, HasQuantity: true},
		4: {Quantity: 135, HasQuantity: true},
		5: {Quantity: 358, HasQuantity: true},
		6: {Quantity: 4, HasQuantity: true},
		7: {Quantity: 10, HasQuantity: true},
		8: {Quantity: 2, HasQuantity: true},
	}

	recordVisiblePage(session)
	if !session.StopAfterPageDone {
		t.Fatal("StopAfterPageDone = false, want true for repeated page signature")
	}
}

// TestRecordVisiblePageContinuesOnChangedQuantitySignature verifies clearly changed pages keep scanning.
func TestRecordVisiblePageContinuesOnChangedQuantitySignature(t *testing.T) {
	session := newTestSession()
	session.LastPageSignature = map[int]int{1: 30, 2: 80, 3: 80, 4: 135, 5: 358, 6: 4, 7: 10, 8: 2}
	session.CurrentPageCells = map[int]scannedCell{
		1: {Quantity: 30, HasQuantity: true},
		2: {Quantity: 8, HasQuantity: true},
		3: {Quantity: 8, HasQuantity: true},
		4: {Quantity: 135, HasQuantity: true},
		5: {Quantity: 358, HasQuantity: true},
		6: {Quantity: 4, HasQuantity: true},
		7: {Quantity: 10, HasQuantity: true},
		8: {Quantity: 2, HasQuantity: true},
	}

	recordVisiblePage(session)
	if session.StopAfterPageDone {
		t.Fatal("StopAfterPageDone = true, want false for changed page signature")
	}
}

// TestRecordVisiblePageStoresHeadProbe verifies scroll comparison can use top-row quantity OCR.
func TestRecordVisiblePageStoresHeadProbe(t *testing.T) {
	session := newTestSession()
	session.CurrentPageCells = map[int]scannedCell{
		1: {Name: "探测器", Quantity: 30, HasQuantity: true},
		2: {Name: "探测器", Quantity: 80, HasQuantity: true},
		3: {Name: "刻写券", Quantity: 135, HasQuantity: true},
		4: {Name: "沉积具象物", Quantity: 4, HasQuantity: true},
		5: {Name: "礼物", Quantity: 358, HasQuantity: true},
	}

	recordVisiblePage(session)
	if len(session.LastHeadProbe) != 5 || session.LastHeadProbe[1] != 30 || session.LastHeadProbe[5] != 358 {
		t.Fatalf("last head probe = %#v, want top-row quantities", session.LastHeadProbe)
	}
}

// TestLoadWarehouseScanConfig verifies the committed resource config is loadable.
func TestLoadWarehouseScanConfig(t *testing.T) {
	config, err := loadWarehouseScanConfig("../../../assets/data/PullCountCalculator/warehouse_scan.json")
	if err != nil {
		t.Fatalf("loadWarehouseScanConfig() error = %v", err)
	}
	if config.CellCount() != 45 {
		t.Fatalf("CellCount() = %d, want 45", config.CellCount())
	}
	geometry, err := geometryForCell(config, 45)
	if err != nil {
		t.Fatalf("geometryForCell(45) error = %v", err)
	}
	if !reflect.DeepEqual(geometry.QuantityROI, []int{885, 598, 58, 32}) {
		t.Fatalf("cell 45 quantity ROI = %#v, want [885 598 58 32]", geometry.QuantityROI)
	}
}

// TestWarehouseScanConfigRejectsMissingFields verifies config errors are explicit.
func TestWarehouseScanConfigRejectsMissingFields(t *testing.T) {
	config := testWarehouseScanConfig()
	config.TitleROI = nil
	if err := config.validate(); err == nil {
		t.Fatal("validate() error = nil, want missing title_roi error")
	}
}

// newTestSession builds the minimal state needed by page-decision unit tests.
func newTestSession() *runSession {
	config := &voucherConfig{Vouchers: []voucherDef{
		{Name: "寻访情报书", PullValue: 10, PoolScope: "next_only"},
	}}
	index, err := buildVoucherIndex(config)
	if err != nil {
		panic(err)
	}
	return &runSession{
		Param:             actionParam{},
		Config:            config,
		ScanConfig:        testWarehouseScanConfig(),
		VoucherIndex:      index,
		VoucherQuantities: make(map[string]int),
		CurrentPageCells:  make(map[int]scannedCell),
	}
}

// testWarehouseScanConfig returns the production-like warehouse geometry used by unit tests.
func testWarehouseScanConfig() *warehouseScanConfig {
	return &warehouseScanConfig{
		Grid: warehouseGridConfig{
			Columns:  9,
			Rows:     5,
			Start:    []int{44, 127},
			Step:     []int{103, 103},
			CellSize: []int{82, 82},
		},
		QuantityROIOffset: []int{17, 59, 58, 32},
		TitleROI:          []int{980, 82, 250, 48},
		TitleExpected:     []string{".*(寻访|recruit).*"},
		Probe: warehouseSimilarityRule{
			CellLimit:     9,
			MinComparable: 4,
			MaxMismatches: 1,
			MinMatchRatio: 0.85,
		},
		RepeatPage: warehouseSimilarityRule{
			CellLimit:     45,
			MinComparable: 8,
			MaxMismatches: 1,
			MinMatchRatio: 0.85,
		},
		ScanMaxPages: 8,
	}
}
