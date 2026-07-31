package goods

import (
	"reflect"
	"testing"

	sellstrategy "github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/goods/strategy"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// TestPriorityItemRegistrationKeepsFirstSlotAndResetClearsItems 验证优先物品按首次登记顺序保存，重置后全部清空。
func TestPriorityItemRegistrationKeepsFirstSlotAndResetClearsItems(t *testing.T) {
	resetPrioritySelectionSession()
	configurePrioritySession(true, false)
	resetPreferredPriorityItems(true)
	if !registerPriorityItem("item_a") || !registerPriorityItem("item_b") {
		t.Fatal("新优先物品应登记成功")
	}
	if registerPriorityItem("item_a") {
		t.Fatal("重复的优先物品应被忽略")
	}
	if got := priorityPolicySnapshot().Preferred; !reflect.DeepEqual(got, []string{"item_a", "item_b"}) {
		t.Fatalf("优先物品 = %v，期望 [item_a item_b]", got)
	}
	resetPrioritySelectionSession()
	if got := priorityPolicySnapshot().Preferred; len(got) != 0 {
		t.Fatalf("重置后仍残留优先物品：%v", got)
	}
}

// TestPriorityConfigurationAndRegionResetAreIndependent 验证总开关只控制优先表是否生效，
// 地区切换只清空优先表，不会清除据点尝试状态或任务级缺货记录。
func TestPriorityConfigurationAndRegionResetAreIndependent(t *testing.T) {
	resetPrioritySelectionSession()
	resetPreferredPriorityItems(true)
	registerPriorityItem("item_a")
	if got := priorityPolicySnapshot().Preferred; len(got) != 0 {
		t.Fatalf("总开关关闭时不应应用优先物品：%v", got)
	}

	configurePrioritySession(true, true)
	policy := priorityPolicySnapshot()
	if !policy.OnlyPreferred || !reflect.DeepEqual(policy.Preferred, []string{"item_a"}) {
		t.Fatalf("严格优先策略未生效：%+v", policy)
	}

	configurePrioritySession(true, false)
	prioritySelectionSetPending("OutpostA", "item_a")
	if _, ok := prioritySelectionCommit("OutpostA"); !ok {
		t.Fatal("据点 A 的待选物品应提交成功")
	}
	if _, _, ok := prioritySelectionMarkOutOfStock("OutpostA"); !ok {
		t.Fatal("据点 A 的已提交物品应可标记为缺货")
	}

	resetPreferredPriorityItems(true)
	registerPriorityItem("item_b")
	if got := priorityPolicySnapshot().Preferred; !reflect.DeepEqual(got, []string{"item_b"}) {
		t.Fatalf("地区切换后的优先物品 = %v，期望 [item_b]", got)
	}
	attempted, outOfStock, _ := prioritySelectionSnapshot("OutpostA")
	if _, ok := attempted["item_a"]; !ok {
		t.Fatalf("地区切换后丢失据点尝试状态：%v", attempted)
	}
	if _, ok := outOfStock["item_a"]; !ok {
		t.Fatalf("地区切换后丢失任务级缺货状态：%v", outOfStock)
	}

	resetPreferredPriorityItems(false)
	if policy := priorityPolicySnapshot(); policy.OnlyPreferred || len(policy.Preferred) != 0 {
		t.Fatalf("未启用地区优先配置时应回退默认售卖策略：%+v", policy)
	}
}

// TestParsePrioritySessionActionParamByOperation 验证登记、提交和缺货操作分别校验所需参数。
func TestParsePrioritySessionActionParamByOperation(t *testing.T) {
	configure, err := parsePrioritySessionActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"operation":"configure","enabled":true,"only_preferred":true}`,
	})
	if err != nil || !configure.Enabled || !configure.OnlyPreferred {
		t.Fatalf("总开关参数 = %+v，错误 = %v", configure, err)
	}
	stock, err := parsePrioritySessionActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"operation":"configure_strategy","strategy":"stock","minimum_unit_price":3}`,
	})
	if err != nil || stock.Strategy != sellstrategy.KindStock || stock.MinimumPrice != 3 {
		t.Fatalf("库存优先参数 = %+v，错误 = %v", stock, err)
	}
	price, err := parsePrioritySessionActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"operation":"configure_strategy","strategy":"price"}`,
	})
	if err != nil || price.Strategy != sellstrategy.KindPrice {
		t.Fatalf("单价优先参数 = %+v，错误 = %v", price, err)
	}
	if _, err := parsePrioritySessionActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"operation":"configure_strategy","strategy":"unknown"}`,
	}); err == nil {
		t.Fatal("未知选品策略应校验失败")
	}
	resetItems, err := parsePrioritySessionActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"operation":"reset_preferred","enabled":true}`,
	})
	if err != nil || resetItems.Operation != priorityOperationResetItems || !resetItems.Enabled {
		t.Fatalf("地区切换参数 = %+v，错误 = %v", resetItems, err)
	}
	resetSelection, err := parsePrioritySessionActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"operation":"reset_goods_selection"}`,
	})
	if err != nil || resetSelection.Operation != priorityOperationResetSelection {
		t.Fatalf("选品重置参数 = %+v，错误 = %v", resetSelection, err)
	}
	register, err := parsePrioritySessionActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"operation":"register","item_id":"item_a"}`,
	})
	if err != nil || register.ItemID != "item_a" {
		t.Fatalf("登记参数 = %+v，错误 = %v", register, err)
	}
	commit, err := parsePrioritySessionActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"operation":"commit","location":"Outpost"}`,
	})
	if err != nil || commit.Location != "Outpost" {
		t.Fatalf("提交参数 = %+v，错误 = %v", commit, err)
	}
	outOfStock, err := parsePrioritySessionActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"operation":"out_of_stock","location":"Outpost"}`,
	})
	if err != nil || outOfStock.Location != "Outpost" {
		t.Fatalf("缺货参数 = %+v，错误 = %v", outOfStock, err)
	}
	empty, err := parsePrioritySessionActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"operation":"register"}`,
	})
	if err != nil || empty.ItemID != "" {
		t.Fatalf("空 item_id 应表示未配置槽位：参数 = %+v，错误 = %v", empty, err)
	}
	resetPrioritySelectionSession()
	if ok := (&PrioritySessionAction{}).Run(nil, &maa.CustomActionArg{
		CustomActionParam: `{"operation":"register"}`,
	}); !ok {
		t.Fatal("未配置优先物品槽位应成功跳过")
	}
	if got := priorityPolicySnapshot().Preferred; len(got) != 0 {
		t.Fatalf("跳过未配置槽位后不应登记空物品：%v", got)
	}
}

func TestSelectionStrategyConfigurationIsIndependentFromRegionalPriority(t *testing.T) {
	resetPrioritySelectionSession()
	configureSelectionStrategy(sellstrategy.KindStock, sellstrategy.Config{MinimumUnitPrice: 3})
	policy := priorityPolicySnapshot()
	if policy.Strategy != sellstrategy.KindStock || policy.StrategyConfig.MinimumUnitPrice != 3 ||
		policy.OnlyPreferred || len(policy.Preferred) != 0 {
		t.Fatalf("独立库存优先策略 = %+v", policy)
	}
	configurePrioritySession(true, true)
	resetPreferredPriorityItems(true)
	registerPriorityItem("item_a")
	policy = priorityPolicySnapshot()
	if policy.Strategy != sellstrategy.KindStock || !policy.OnlyPreferred ||
		!reflect.DeepEqual(policy.Preferred, []string{"item_a"}) {
		t.Fatalf("库存与地区优先组合策略 = %+v", policy)
	}
	resetPrioritySelectionSession()
	if policy := priorityPolicySnapshot(); policy.Strategy != sellstrategy.KindRarity ||
		policy.StrategyConfig.MinimumUnitPrice != 0 {
		t.Fatalf("重置后仍残留库存策略：%+v", policy)
	}
}

// TestPrioritySelectionCommitMarksAttempted 验证提交待选物品后会记录为已尝试并清空待选状态。
func TestPrioritySelectionCommitMarksAttempted(t *testing.T) {
	resetPrioritySelectionSession()
	prioritySelectionSetPending("Outpost", "item_a")
	itemID, ok := prioritySelectionCommit("Outpost")
	if !ok || itemID != "item_a" {
		t.Fatalf("提交结果 = %q，成功状态 = %v", itemID, ok)
	}
	attempted, outOfStock, pending := prioritySelectionSnapshot("Outpost")
	if _, ok := attempted["item_a"]; !ok || pending != "" {
		t.Fatalf("提交后的状态不符合预期：已尝试 = %v，待选 = %q", attempted, pending)
	}
	if len(outOfStock) != 0 {
		t.Fatalf("提交物品不应直接标记为缺货：%v", outOfStock)
	}
}

// TestPrioritySelectionOutOfStockSharedAndReset 验证缺货标记跨据点共享、重复标记去重，并随新任务重置。
func TestPrioritySelectionOutOfStockSharedAndReset(t *testing.T) {
	resetPrioritySelectionSession()
	prioritySelectionSetPending("OutpostA", "item_a")
	if _, ok := prioritySelectionCommit("OutpostA"); !ok {
		t.Fatal("据点 A 的待选物品应提交成功")
	}
	itemID, marked, ok := prioritySelectionMarkOutOfStock("OutpostA")
	if !ok || !marked || itemID != "item_a" {
		t.Fatalf("首次缺货标记结果 = %q, %v, %v", itemID, marked, ok)
	}
	if _, marked, ok := prioritySelectionMarkOutOfStock("OutpostA"); !ok || marked {
		t.Fatalf("重复缺货标记应成功但不重复新增：marked = %v, ok = %v", marked, ok)
	}
	_, outOfStock, _ := prioritySelectionSnapshot("OutpostB")
	if _, exists := outOfStock["item_a"]; !exists {
		t.Fatalf("据点 B 未继承任务内缺货集合：%v", outOfStock)
	}
	resetPrioritySelectionSession()
	_, outOfStock, _ = prioritySelectionSnapshot("OutpostB")
	if len(outOfStock) != 0 {
		t.Fatalf("新任务仍残留缺货物品：%v", outOfStock)
	}
}

func TestZeroStockObservationMarksTaskOutOfStock(t *testing.T) {
	resetPrioritySelectionSession()
	prioritySelectionMarkScannedOutOfStock([]stockPageItem{
		{ItemID: "zero", Quantity: 0, StockKnown: true},
		{ItemID: "available", Quantity: 10, StockKnown: true},
		{ItemID: "unknown"},
	})

	if excluded := priorityOutOfStockSnapshot(); !reflect.DeepEqual(excluded, map[string]struct{}{"zero": {}}) {
		t.Fatalf("库存扫描后的缺货排除 = %v，期望仅包含 zero", excluded)
	}
	_, outOfStock, _ := prioritySelectionSnapshot("OutpostB")
	if _, exists := outOfStock["zero"]; !exists {
		t.Fatalf("下个据点未继承库存扫描的缺货集合：%v", outOfStock)
	}
	resetPrioritySelectionSession()
	if len(priorityOutOfStockSnapshot()) != 0 {
		t.Fatalf("新任务仍残留库存扫描缺货物品：%v", priorityOutOfStockSnapshot())
	}
}

// TestPrioritySelectionOutOfStockRequiresCommittedItem 验证没有已提交物品时不能误标缺货。
func TestPrioritySelectionOutOfStockRequiresCommittedItem(t *testing.T) {
	resetPrioritySelectionSession()
	if itemID, marked, ok := prioritySelectionMarkOutOfStock("Outpost"); ok || marked || itemID != "" {
		t.Fatalf("无已提交物品时不应标记缺货：%q, %v, %v", itemID, marked, ok)
	}
	if (&PrioritySessionAction{}).Run(nil, &maa.CustomActionArg{
		CustomActionParam: `{"operation":"out_of_stock","location":"Outpost"}`,
	}) {
		t.Fatal("缺少已提交物品时 Custom Action 应失败")
	}
}

// TestPriorityExhaustionRequiresStableObservation 验证连续两帧识别集合一致时才判定优先物品耗尽。
func TestPriorityExhaustionRequiresStableObservation(t *testing.T) {
	resetPrioritySelectionSession()
	if prioritySelectionObserveExhaustion("Outpost", []string{"b", "a"}) {
		t.Fatal("首次观察不应判定耗尽")
	}
	if !prioritySelectionObserveExhaustion("Outpost", []string{"a", "b"}) {
		t.Fatal("第二次观察到相同集合时应判定耗尽")
	}
	prioritySelectionResetExhaustion("Outpost")
	if prioritySelectionObserveExhaustion("Outpost", []string{"a", "b"}) {
		t.Fatal("重置后应重新等待两次稳定观察")
	}
}
