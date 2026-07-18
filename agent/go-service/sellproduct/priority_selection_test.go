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

// TestParsePrioritySessionActionParamByOperation 验证登记和提交操作分别校验所需参数。
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
	if _, err := parsePrioritySessionActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"operation":"register"}`,
	}); err == nil {
		t.Fatal("登记操作缺少 item_id 时应校验失败")
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
	attempted, pending := prioritySelectionSnapshot("Outpost")
	if _, ok := attempted["item_a"]; !ok || pending != "" {
		t.Fatalf("提交后的状态不符合预期：已尝试 = %v，待选 = %q", attempted, pending)
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
