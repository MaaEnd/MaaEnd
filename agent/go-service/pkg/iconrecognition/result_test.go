package iconrecognition

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestParseDetailObjectBoxes(t *testing.T) {
	detail, err := ParseDetail(`{
		"detail_version": 1,
		"matched": true,
		"grid_type": "rewards",
		"roi": {"x": 39, "y": 82, "width": 1205, "height": 511},
		"matches": [{
			"item_id": "item_gold",
			"score": 0.91,
			"cell_box": {"x": 100, "y": 200, "width": 96, "height": 96},
			"item_box": {"x": 108, "y": 208, "width": 80, "height": 80}
		}]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ROI != (maa.Rect{39, 82, 1205, 511}) {
		t.Fatalf("roi=%v", detail.ROI)
	}
	if len(detail.Matches) != 1 {
		t.Fatalf("matches=%d", len(detail.Matches))
	}
	m := detail.Matches[0]
	if m.ItemID != "item_gold" || m.CellBox != (maa.Rect{100, 200, 96, 96}) {
		t.Fatalf("match=%+v", m)
	}
	if m.ItemBox != (maa.Rect{108, 208, 80, 80}) {
		t.Fatalf("item_box=%v", m.ItemBox)
	}
}

func TestParseDetailArrayBoxes(t *testing.T) {
	detail, err := ParseDetail(`{
		"detail_version": 1,
		"matched": true,
		"grid_type": "valuables",
		"roi": [24, 76, 950, 570],
		"matches": [{
			"item_id": "item_gold",
			"score": 0.91,
			"cell_box": [100, 200, 96, 96],
			"item_box": [108, 208, 80, 80]
		}]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ROI != (maa.Rect{24, 76, 950, 570}) {
		t.Fatalf("roi=%v", detail.ROI)
	}
	m := detail.Matches[0]
	if m.CellBox != (maa.Rect{100, 200, 96, 96}) || m.ItemBox != (maa.Rect{108, 208, 80, 80}) {
		t.Fatalf("match=%+v", m)
	}
}
