package goods

import (
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	sellstrategy "github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/goods/strategy"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// TestParseCurrentGoodsRecognitionParam 校验地点和 ROI 必填，且 ROI 必须为正方形。
func TestParseCurrentGoodsRecognitionParam(t *testing.T) {
	if _, err := parseCurrentGoodsRecognitionParam(`{"location":"InfraStation"}`); err == nil {
		t.Fatal("缺少 roi 应校验失败")
	}

	param, err := parseCurrentGoodsRecognitionParam(`{"location":"InfraStation","roi":[1177,450,54,54]}`)
	if err != nil || param.ROI[0] != 1177 || param.ROI[2] != 54 {
		t.Fatalf("Pipeline ROI 参数 = %+v，错误 = %v", param, err)
	}

	if _, err = parseCurrentGoodsRecognitionParam(`{"location":" ","roi":[1177,450,54,54]}`); err == nil {
		t.Fatal("空 location 应校验失败")
	}
	if _, err = parseCurrentGoodsRecognitionParam(`{"location":"InfraStation","roi":[1177,450,54,60]}`); err == nil {
		t.Fatal("非正方形 ROI 应校验失败")
	}
	if _, err = parseCurrentGoodsRecognitionParam(`{"location":"InfraStation","roi":[1177,450]}`); err == nil {
		t.Fatal("长度不足的 ROI 应校验失败")
	}
}

// TestIsCurrentGoodsNoMatch 验证仅把 IconRecognition 的正常未命中视为回落条件。
func TestIsCurrentGoodsNoMatch(t *testing.T) {
	if isCurrentGoodsNoMatch(nil) {
		t.Fatal("空错误不应视为 no_match")
	}
	if !isCurrentGoodsNoMatch(&iconrecognition.DetailError{Code: iconrecognition.ErrorCodeNoMatch}) {
		t.Fatal("no_match 应视为正常未命中")
	}
	if isCurrentGoodsNoMatch(&iconrecognition.DetailError{Code: iconrecognition.ErrorCodeException}) {
		t.Fatal("exception 不应视为正常未命中")
	}
}

// TestCurrentGoodsMatchesSelectionFollowsStrategy 验证仅沿用当前策略的下一候选；
// 没有优先货品时，库存策略进入货品列表读取实时库存。
func TestCurrentGoodsMatchesSelectionFollowsStrategy(t *testing.T) {
	groups := []itemPriorityGroup{
		{ItemID: "high_rarity", Rarity: 4, UnitPrice: 30},
		{ItemID: "high_price", Rarity: 3, UnitPrice: 70},
	}
	tests := []struct {
		name         string
		strategy     sellstrategy.Kind
		current      string
		wantSellable bool
	}{
		{name: "rarity best", strategy: sellstrategy.KindRarity, current: "high_rarity", wantSellable: true},
		{name: "rarity lower", strategy: sellstrategy.KindRarity, current: "high_price"},
		{name: "price best", strategy: sellstrategy.KindPrice, current: "high_price", wantSellable: true},
		{name: "price lower", strategy: sellstrategy.KindPrice, current: "high_rarity"},
		{name: "stock always scans", strategy: sellstrategy.KindStock, current: "high_rarity"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetReserveSession()
			policy := prioritySelectionPolicy{Strategy: test.strategy}
			if got := currentGoodsMatchesSelection("Outpost", test.current, groups, policy); got != test.wantSellable {
				t.Fatalf("当前货品沿用 = %v，期望 %v", got, test.wantSellable)
			}
		})
	}
	resetReserveSession()
}

// TestCurrentGoodsMatchesSelectionStockPreferred 验证库存策略仍优先处理用户配置的
// 优先货品，但不会越过更靠前且可用的优先槽位沿用当前货品。
func TestCurrentGoodsMatchesSelectionStockPreferred(t *testing.T) {
	groups := []itemPriorityGroup{
		{ItemID: "item_a", Rarity: 2, UnitPrice: 10},
		{ItemID: "item_b", Rarity: 3, UnitPrice: 20},
	}

	resetReserveSession()
	configurePrioritySession(true, false)
	configureSelectionStrategy(sellstrategy.KindStock, sellstrategy.Config{})
	resetPreferredPriorityItems(true)
	registerPriorityItem("item_a")
	registerPriorityItem("item_b")
	policy := priorityPolicySnapshot()
	if !currentGoodsMatchesSelection("Outpost", "item_a", groups, policy) {
		t.Fatal("库存策略应沿用第一可用优先货品")
	}
	if currentGoodsMatchesSelection("Outpost", "item_b", groups, policy) {
		t.Fatal("库存策略不应越过第一可用优先货品")
	}

	prioritySelectionAdopt("Outpost", "item_a")
	if !currentGoodsMatchesSelection("Outpost", "item_b", groups, priorityPolicySnapshot()) {
		t.Fatal("第一优先货品已尝试后应沿用第二优先货品")
	}
	resetReserveSession()
}

// TestCurrentGoodsMatchesSelectionPolicy 验证沿用判断复用正式选品的优先顺序和排除规则。
func TestCurrentGoodsMatchesSelectionPolicy(t *testing.T) {
	groups := []itemPriorityGroup{
		{ItemID: "item_a", Rarity: 2, UnitPrice: 10},
		{ItemID: "item_b", Rarity: 3, UnitPrice: 20},
	}

	resetReserveSession()
	configurePrioritySession(false, false)
	policy := priorityPolicySnapshot()
	if !currentGoodsMatchesSelection("Outpost", "item_b", groups, policy) {
		t.Fatal("默认稀有度策略应沿用排序第一的物品")
	}
	if currentGoodsMatchesSelection("Outpost", "item_a", groups, policy) {
		t.Fatal("默认稀有度策略不应沿用排序靠后的物品")
	}

	resetReserveSession()
	prioritySelectionAdopt("Outpost", "item_b")
	if currentGoodsMatchesSelection("Outpost", "item_b", groups, priorityPolicySnapshot()) {
		t.Fatal("已尝试物品不应沿用")
	}
	if !currentGoodsMatchesSelection("Outpost", "item_a", groups, priorityPolicySnapshot()) {
		t.Fatal("应沿用排除已尝试物品后的下一候选")
	}

	resetReserveSession()
	prioritySelectionAdopt("Outpost", "item_b")
	if _, _, ok := prioritySelectionMarkOutOfStock("Outpost"); !ok {
		t.Fatal("沿用后的当前物品应可标记缺货")
	}
	if currentGoodsMatchesSelection("Outpost", "item_b", groups, priorityPolicySnapshot()) {
		t.Fatal("缺货物品不应沿用")
	}

	resetReserveSession()
	registerReserveRule("item_b", reserveBlacklistQuantity)
	if currentGoodsMatchesSelection("Outpost", "item_b", groups, priorityPolicySnapshot()) {
		t.Fatal("黑名单物品不应沿用")
	}

	resetReserveSession()
	registerReserveRule("item_b", 10)
	setSelectedReserveItem("item_b")
	if _, _, _, ok := markSelectedReserveSatisfied(); !ok {
		t.Fatal("保留规则应可标记满足")
	}
	if currentGoodsMatchesSelection("Outpost", "item_b", groups, priorityPolicySnapshot()) {
		t.Fatal("保留量已满足的物品不应沿用")
	}

	resetReserveSession()
	configurePrioritySession(true, false)
	resetPreferredPriorityItems(true)
	registerPriorityItem("item_a")
	registerPriorityItem("item_b")
	policy = priorityPolicySnapshot()
	if !currentGoodsMatchesSelection("Outpost", "item_a", groups, policy) {
		t.Fatal("应沿用优先槽位中的第一候选")
	}
	if currentGoodsMatchesSelection("Outpost", "item_b", groups, policy) {
		t.Fatal("不应越过更靠前的优先物品沿用当前货品")
	}

	resetReserveSession()
	configurePrioritySession(true, true)
	resetPreferredPriorityItems(true)
	registerPriorityItem("item_b")
	policy = priorityPolicySnapshot()
	if currentGoodsMatchesSelection("Outpost", "item_a", groups, policy) {
		t.Fatal("严格优先模式下未配置的物品不应沿用")
	}
	if !currentGoodsMatchesSelection("Outpost", "item_b", groups, policy) {
		t.Fatal("严格优先模式应沿用唯一配置的物品")
	}
	resetReserveSession()
}

// TestPrioritySelectionAdoptMatchesCommitState 验证沿用与换货提交产生的会话状态一致。
func TestPrioritySelectionAdoptMatchesCommitState(t *testing.T) {
	resetPrioritySelectionSession()
	prioritySelectionSetPending("Outpost", "item_pending")
	prioritySelectionAdopt("Outpost", "item_a")
	attempted, _, pending := prioritySelectionSnapshot("Outpost")
	if _, ok := attempted["item_a"]; !ok || pending != "" {
		t.Fatalf("沿用后状态不符合预期：已尝试 = %v，待选 = %q", attempted, pending)
	}
	itemID, _, ok := prioritySelectionMarkOutOfStock("Outpost")
	if !ok || itemID != "item_a" {
		t.Fatalf("沿用物品应作为当前物品参与缺货标记：%q, %v", itemID, ok)
	}
	resetPrioritySelectionSession()
}

// TestPrioritySessionAdoptParamAndDetail 校验 adopt 操作的参数与缺失识别结果处理。
func TestPrioritySessionAdoptParamAndDetail(t *testing.T) {
	if _, err := parsePrioritySessionActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"operation":"adopt","location":"InfraStation"}`,
	}); err != nil {
		t.Fatalf("adopt 参数应解析成功：%v", err)
	}
	if _, err := parsePrioritySessionActionParam(&maa.CustomActionArg{
		CustomActionParam: `{"operation":"adopt"}`,
	}); err == nil {
		t.Fatal("缺少 location 的 adopt 应校验失败")
	}

	resetPrioritySelectionSession()
	if (&PrioritySessionAction{}).Run(nil, &maa.CustomActionArg{
		CustomActionParam: `{"operation":"adopt","location":"InfraStation"}`,
	}) {
		t.Fatal("缺少识别 detail 的 adopt 应失败")
	}
	resetPrioritySelectionSession()
}

// TestCurrentGoodsItemIDFromRecognition 校验识别结果缺失、为空或类型错误时均会失败。
func TestCurrentGoodsItemIDFromRecognition(t *testing.T) {
	tests := []struct {
		name   string
		detail *maa.RecognitionDetail
	}{
		{
			name: "nil detail",
		},
		{
			name:   "nil results",
			detail: &maa.RecognitionDetail{},
		},
		{
			name: "empty results",
			detail: &maa.RecognitionDetail{
				Results: &maa.RecognitionResults{},
			},
		},
		{
			name: "non-custom best",
			detail: &maa.RecognitionDetail{
				Results: &maa.RecognitionResults{
					Best: &maa.RecognitionResult{},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if itemID, err := currentGoodsItemIDFromRecognition(test.detail); err == nil {
				t.Fatalf("无效识别结果应解析失败：item_id = %q", itemID)
			}
		})
	}
}

// TestCurrentGoodsItemIDFromCustomResult 校验合法 Custom 结果的 item_id 成功路径。
func TestCurrentGoodsItemIDFromCustomResult(t *testing.T) {
	if _, err := currentGoodsItemIDFromCustomResult(nil); err == nil {
		t.Fatal("nil Custom 结果应解析失败")
	}
	if _, err := currentGoodsItemIDFromCustomResult(&maa.CustomRecognitionResult{
		Detail: `{"location":"InfraStation"}`,
	}); err == nil {
		t.Fatal("缺少 item_id 的 Custom 结果应解析失败")
	}

	itemID, err := currentGoodsItemIDFromCustomResult(&maa.CustomRecognitionResult{
		Detail: `{"location":"InfraStation","item_id":"item_a","score":0.91}`,
	})
	if err != nil || itemID != "item_a" {
		t.Fatalf("Custom 结果解析 = %q，错误 = %v", itemID, err)
	}
}
