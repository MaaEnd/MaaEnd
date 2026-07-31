package sellproduct

import (
	"strings"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/goods"
	sellstrategy "github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/goods/strategy"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestParseLocationPlanActionParam(t *testing.T) {
	param, err := parseLocationPlanActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"location":" RefugeeCamp "}`,
	})
	if err != nil || param.Location != "RefugeeCamp" {
		t.Fatalf("location plan param = %+v, error = %v", param, err)
	}
	if _, err := parseLocationPlanActionParam(&maa.CustomActionArg{
		CustomActionParam: `{}`,
	}); err == nil {
		t.Fatal("empty location should fail")
	}
}

func TestRuntimeLocationPlanMessage(t *testing.T) {
	i18n.Init()
	message := runtimeLocationPlanMessage(runtimeLocationPlan{
		LocationName:      "测试据点",
		TargetOperator:    "售卖干员",
		RestoreOperator:   "恢复干员",
		SelectionStrategy: sellstrategy.KindRarity,
		Items: []goods.LocationPlanItem{
			{Name: "物品甲"},
			{Name: "物品乙", ReserveQuantity: 10},
		},
		ExcludedOutOfStock: []string{"物品丙"},
		ReserveSatisfied: []goods.LocationPlanItem{
			{Name: "物品戊", ReserveQuantity: 20},
		},
		ExcludedByUser: []string{"物品丁"},
	})

	for _, expected := range []string{
		"测试据点",
		"售卖干员",
		"恢复干员",
		"物品甲 → 物品乙",
		"缺货排除：物品丙",
		"保留量已满足：物品戊",
		"用户排除：物品丁",
		"物品乙保留 10",
		"物品戊保留 20",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("据点计划 %q 不包含 %q", message, expected)
		}
	}
	if strings.Contains(message, "物品甲保留") {
		t.Fatalf("据点计划错误显示了未配置的保留规则：%q", message)
	}
}

func TestRuntimeLocationPlanMessageWithoutReserve(t *testing.T) {
	i18n.Init()
	message := runtimeLocationPlanMessage(runtimeLocationPlan{
		LocationName: "测试据点",
		Items:        []goods.LocationPlanItem{{Name: "物品甲"}},
	})

	for _, expected := range []string{"无", "全部售卖"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("无保留计划 %q 不包含 %q", message, expected)
		}
	}
}

func TestRuntimeLocationPlanMessageShowsStockStrategyInsteadOfStaticOrder(t *testing.T) {
	i18n.Init()
	message := runtimeLocationPlanMessage(runtimeLocationPlan{
		LocationName:      "测试据点",
		SelectionStrategy: sellstrategy.KindStock,
		Items: []goods.LocationPlanItem{
			{Name: "物品甲"},
			{Name: "物品乙"},
		},
	})
	if !strings.Contains(message, "按库存优先售卖") {
		t.Fatalf("库存策略计划 %q 未显示库存优先提示", message)
	}
	if strings.Contains(message, "物品甲 → 物品乙") {
		t.Fatalf("库存策略计划不应显示静态货品顺序：%q", message)
	}
}
