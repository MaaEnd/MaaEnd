package bettersliding

import (
	"reflect"
	"testing"
)

// 云终末地实测：「+」模板在「−」按钮上反而得分更高（0.904 对 0.851），按分数挑会把增加点成减少
func TestResolveButtonPlacementPicksBySideOfSlider(t *testing.T) {
	start := []int{487, 521, 39, 37}

	inc := resolveButtonPlacement(start, "right", true)
	dec := resolveButtonPlacement(start, "right", false)
	if inc == nil || dec == nil {
		t.Fatal("placement should be resolved for a known direction")
	}
	wantROI := []int{0, 484, screenWidth, 111}
	if !reflect.DeepEqual(inc.roi, wantROI) || inc.orderBy != "Horizontal" || inc.index != -1 {
		t.Fatalf("increase placement = %+v, want roi %v, Horizontal, last", inc, wantROI)
	}
	if dec.orderBy != "Horizontal" || dec.index != 0 {
		t.Fatalf("decrease placement = %+v, want Horizontal, first", dec)
	}

	// 反向滑条：增加在左侧
	if p := resolveButtonPlacement(start, "left", true); p.index != 0 {
		t.Fatalf("left increase index = %d, want 0", p.index)
	}
	// 竖向：up 的终点在上方，增加取最上面的
	up := resolveButtonPlacement(start, "up", true)
	if up.orderBy != "Vertical" || up.index != 0 || !reflect.DeepEqual(up.roi, []int{448, 0, 117, screenHeight}) {
		t.Fatalf("up increase placement = %+v", up)
	}
	if p := resolveButtonPlacement(start, "down", true); p.index != -1 {
		t.Fatalf("down increase index = %d, want -1", p.index)
	}

	if resolveButtonPlacement(nil, "right", true) != nil || resolveButtonPlacement(start, "diagonal", true) != nil {
		t.Fatal("missing start box or unknown direction must keep score ordering")
	}
}

func TestButtonHelperOverrideCarriesPlacement(t *testing.T) {
	placement := resolveButtonPlacement([]int{487, 521, 39, 37}, "right", true)
	param := buildTemplateMatchButtonHelperOverride("AutoStockpile/IncreaseButton.png", placement)["recognition"].(map[string]any)["param"].(map[string]any)
	if param["order_by"] != "Horizontal" || param["index"] != -1 || param["roi"] == nil {
		t.Fatalf("param = %v, want roi/order_by/index from placement", param)
	}

	plain := buildTemplateMatchButtonHelperOverride("x.png", nil)["recognition"].(map[string]any)["param"].(map[string]any)
	if _, ok := plain["order_by"]; ok {
		t.Fatalf("param without placement = %v, must not force an order", plain)
	}
}
