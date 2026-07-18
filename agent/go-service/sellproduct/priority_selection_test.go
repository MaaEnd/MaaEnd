package sellproduct

import (
	"reflect"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// TestPriorityItemRegistrationKeepsFirstSlotAndResetClearsItems 验证优先物品按首次登记顺序保存，重置后全部清空。
func TestPriorityItemRegistrationKeepsFirstSlotAndResetClearsItems(t *testing.T) {
	resetPrioritySelectionSession()
	if !registerPriorityItem("item_a") || !registerPriorityItem("item_b") {
		t.Fatal("新优先物品应登记成功")
	}
	if registerPriorityItem("item_a") {
		t.Fatal("重复的优先物品应被忽略")
	}
	if got := priorityItemsSnapshot(); !reflect.DeepEqual(got, []string{"item_a", "item_b"}) {
		t.Fatalf("优先物品 = %v，期望 [item_a item_b]", got)
	}
	resetPrioritySelectionSession()
	if got := priorityItemsSnapshot(); len(got) != 0 {
		t.Fatalf("重置后仍残留优先物品：%v", got)
	}
}

// TestParsePrioritySessionActionParamByOperation 验证登记、提交和缺货操作分别校验所需参数。
func TestParsePrioritySessionActionParamByOperation(t *testing.T) {
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
	if got := priorityItemsSnapshot(); len(got) != 0 {
		t.Fatalf("跳过未配置槽位后不应登记空物品：%v", got)
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
