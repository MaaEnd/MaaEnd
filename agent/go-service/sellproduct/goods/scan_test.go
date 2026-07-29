package goods

import (
	"image"
	"reflect"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/internal/ocrmatch"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestParseStockQuantity(t *testing.T) {
	tests := []struct {
		raw  string
		want int64
	}{
		{raw: "0", want: 0},
		{raw: "4510", want: 4510},
		{raw: "1,234", want: 1234},
		{raw: "6.76万", want: 67600},
		{raw: "1.47萬", want: 14700},
		{raw: "4.87만", want: 48700},
		{raw: "14.7K", want: 14700},
		{raw: "1.2M", want: 1200000},
	}
	for _, test := range tests {
		got, ok := parseStockQuantity(test.raw)
		if !ok || got != test.want {
			t.Fatalf("parseStockQuantity(%q) = %d, %v, want %d, true", test.raw, got, ok, test.want)
		}
	}
	if _, ok := parseStockQuantity("库存未知"); ok {
		t.Fatal("text without a number should fail")
	}
}

func TestParsePriorityItemRecognitionParamRequiresSelectOffsets(t *testing.T) {
	param, err := parsePriorityItemRecognitionParam(`{
		"location":"Outpost",
		"result":"select",
		"stock_name_offset":[42,-60,100,40],
		"stock_quantity_offset":[42,-4,100,40],
		"stock_click_offset":[42,-58,100,34]
	}`)
	if err != nil {
		t.Fatalf("parsePriorityItemRecognitionParam: %v", err)
	}
	if param.stockCellOffsets.Quantity != (maa.Rect{42, -4, 100, 40}) {
		t.Fatalf("quantity offset = %v", param.stockCellOffsets.Quantity)
	}
	if _, err := parsePriorityItemRecognitionParam(`{"location":"Outpost","result":"select"}`); err == nil {
		t.Fatal("select recognition without platform offsets must be rejected")
	}
	if _, err := parsePriorityItemRecognitionParam(`{"location":"Outpost","result":"exhausted"}`); err != nil {
		t.Fatalf("exhausted recognition does not consume platform offsets: %v", err)
	}
}

func TestBuildStockPageItemsAssociatesOneOCRResultSetByAnchor(t *testing.T) {
	anchors := []maa.Rect{
		{240, 235, 40, 40},
		{550, 235, 40, 40},
	}
	ocrItems := []ocrmatch.Item{
		{Text: "物品甲", Box: maa.Rect{290, 190, 120, 30}},
		{Text: "4803", Box: maa.Rect{290, 240, 70, 25}},
		{Text: "70", Box: maa.Rect{390, 232, 30, 25}},
		{Text: "物品乙", Box: maa.Rect{600, 190, 120, 30}},
		{Text: "1.47", Box: maa.Rect{600, 240, 45, 25}},
		{Text: "万", Box: maa.Rect{650, 240, 20, 25}},
		{Text: "70", Box: maa.Rect{700, 232, 30, 25}},
	}
	groups := []itemPriorityGroup{
		{ItemID: "item_a", Candidates: []string{"物品甲"}},
		{ItemID: "item_b", Candidates: []string{"物品乙"}},
	}
	offsets := stockCellOffsets{
		Name:     maa.Rect{42, -60, 100, 40},
		Quantity: maa.Rect{42, -4, 100, 40},
		Click:    maa.Rect{42, -58, 100, 34},
	}

	items, err := buildStockPageItems(ocrItems, anchors, groups, offsets, image.Rect(0, 0, 1280, 720))
	if err != nil {
		t.Fatalf("buildStockPageItems: %v", err)
	}
	want := []stockPageItem{
		{
			ItemID:     "item_a",
			StockBox:   maa.Rect{282, 231, 100, 40},
			ClickBox:   maa.Rect{282, 177, 100, 34},
			Quantity:   4803,
			StockKnown: true,
		},
		{
			ItemID:     "item_b",
			StockBox:   maa.Rect{592, 231, 100, 40},
			ClickBox:   maa.Rect{592, 177, 100, 34},
			Quantity:   14700,
			StockKnown: true,
		},
	}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("stock page items = %#v, want %#v", items, want)
	}
}

func TestBuildStockPageItemsKeepsNameOnlyItemBelowCompleteRows(t *testing.T) {
	anchors := []maa.Rect{
		{240, 235, 40, 40},
		{550, 235, 40, 40},
	}
	nameOnlyBox := maa.Rect{290, 330, 120, 30}
	ocrItems := []ocrmatch.Item{
		{Text: "物品甲", Box: maa.Rect{290, 190, 120, 30}},
		{Text: "4803", Box: maa.Rect{290, 240, 70, 25}},
		{Text: "物品乙", Box: maa.Rect{600, 190, 120, 30}},
		{Text: "14700", Box: maa.Rect{600, 240, 70, 25}},
		{Text: "物品丙", Box: nameOnlyBox},
	}
	groups := []itemPriorityGroup{
		{ItemID: "item_a", Candidates: []string{"物品甲"}},
		{ItemID: "item_b", Candidates: []string{"物品乙"}},
		{ItemID: "item_c", Candidates: []string{"物品丙"}},
	}
	offsets := stockCellOffsets{
		Name:     maa.Rect{42, -60, 100, 40},
		Quantity: maa.Rect{42, -4, 100, 40},
		Click:    maa.Rect{42, -58, 100, 34},
	}

	items, err := buildStockPageItems(ocrItems, anchors, groups, offsets, image.Rect(0, 0, 1280, 720))
	if err != nil {
		t.Fatalf("buildStockPageItems: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("stock page item count = %d, want 3: %+v", len(items), items)
	}
	nameOnly := items[2]
	if nameOnly.ItemID != "item_c" || nameOnly.StockKnown || nameOnly.Quantity != 0 ||
		nameOnly.StockBox != (maa.Rect{}) || nameOnly.ClickBox != nameOnlyBox {
		t.Fatalf("name-only item = %+v", nameOnly)
	}
}

func TestSortRectsByGridKeepsRowsBeforeColumns(t *testing.T) {
	boxes := []maa.Rect{
		{900, 200, 30, 30},
		{100, 210, 30, 30},
		{100, 340, 30, 30},
	}
	sortRectsByGrid(boxes)
	want := []maa.Rect{
		{100, 210, 30, 30},
		{900, 200, 30, 30},
		{100, 340, 30, 30},
	}
	if !reflect.DeepEqual(boxes, want) {
		t.Fatalf("sorted boxes = %v, want %v", boxes, want)
	}
}

func TestStockCellBoxUsesPipelineOffset(t *testing.T) {
	box := stockCellBox(
		maa.Rect{240, 235, 40, 40},
		maa.Rect{42, -60, 100, 40},
		image.Rect(0, 0, 1280, 720),
	)
	if want := (maa.Rect{282, 175, 100, 40}); box != want {
		t.Fatalf("stock cell box = %v, want %v", box, want)
	}
}

func TestBuildStockPageItemsRejectsUnitPriceWhenStockOCRIsMissing(t *testing.T) {
	anchors := []maa.Rect{{240, 235, 40, 40}}
	ocrItems := []ocrmatch.Item{
		{Text: "物品甲", Box: maa.Rect{290, 190, 120, 30}},
		// 单价框位于 Pipeline 配置的库存框右侧，不得作为库存兜底。
		{Text: "70", Box: maa.Rect{390, 232, 30, 25}},
	}
	groups := []itemPriorityGroup{{ItemID: "item_a", Candidates: []string{"物品甲"}}}
	offsets := stockCellOffsets{
		Name:     maa.Rect{42, -60, 100, 40},
		Quantity: maa.Rect{42, -4, 100, 40},
		Click:    maa.Rect{42, -58, 100, 34},
	}

	if _, err := buildStockPageItems(
		ocrItems,
		anchors,
		groups,
		offsets,
		image.Rect(0, 0, 1280, 720),
	); err == nil {
		t.Fatal("missing stock OCR must fail instead of using the unit price")
	}
}

func TestParseStockCellOffsetsValidatesPipelineGeometry(t *testing.T) {
	offsets, err := parseStockCellOffsets(
		[]int{42, -60, 100, 40},
		[]int{42, -4, 100, 40},
		[]int{42, -58, 100, 34},
	)
	if err != nil {
		t.Fatalf("parseStockCellOffsets: %v", err)
	}
	if offsets.Name != (maa.Rect{42, -60, 100, 40}) ||
		offsets.Quantity != (maa.Rect{42, -4, 100, 40}) ||
		offsets.Click != (maa.Rect{42, -58, 100, 34}) {
		t.Fatalf("unexpected offsets: %+v", offsets)
	}
	if _, err := parseStockCellOffsets(
		[]int{42, -60, 0, 40},
		[]int{42, -4, 100, 40},
		[]int{42, -58, 100, 34},
	); err == nil {
		t.Fatal("zero-width stock name offset must be rejected")
	}
}

func TestGoodsCellAnchorUsesAndBoxIndexInsteadOfFirstOCRChild(t *testing.T) {
	ocrResult := new(maa.RecognitionResult)
	templateResult := new(maa.RecognitionResult)
	detail := &maa.RecognitionDetail{
		Algorithm: string(maa.RecognitionTypeAnd),
		CombinedResult: []*maa.RecognitionDetail{
			{
				Algorithm: string(maa.RecognitionTypeOCR),
				Results:   &maa.RecognitionResults{Filtered: []*maa.RecognitionResult{ocrResult}},
			},
			{
				Algorithm: string(maa.RecognitionTypeTemplateMatch),
				Results:   &maa.RecognitionResults{Filtered: []*maa.RecognitionResult{templateResult}},
			},
		},
	}
	selected, err := recogtarget.SelectDetailFromJSON([]byte(`{
        "recognition": "And",
        "all_of": ["SellProductCheckSelectGoodsText", "SellProductCheckGoodsCellAnchor"],
        "box_index": 1
    }`), detail)
	if err != nil {
		t.Fatalf("select template child: %v", err)
	}
	results := templateMatchResults(selected)
	if len(results) != 1 || results[0] != templateResult {
		t.Fatalf("template results = %v, want the box_index child", results)
	}
}
