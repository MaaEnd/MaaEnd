package essencefilter

import (
	"os"
	"strings"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
)

// TestCollectionLevelsBetter 覆盖保留偏好比较链的每一级，以及比较链无法区分的场合。
func TestCollectionLevelsBetter(t *testing.T) {
	cases := []struct {
		name string
		a, b collectionLevels
		want int // >0: a 更优；<0: b 更优；0: 比较链无法区分
	}{
		// ① slot1/slot2 中 6 的个数优先于 slot3，也优先于三槽之和。
		{"六的个数压过技能槽", collectionLevels{6, 1, 3}, collectionLevels{5, 5, 3}, 1},
		{"六的个数压过技能槽(反向)", collectionLevels{5, 5, 3}, collectionLevels{6, 1, 3}, -1},
		{"两个六胜过技能槽满级", collectionLevels{6, 6, 1}, collectionLevels{6, 1, 3}, 1},
		// ② slot3 等级。
		{"技能槽等级", collectionLevels{6, 1, 3}, collectionLevels{6, 1, 2}, 1},
		// ③ slot1+slot2 之和。
		{"十二槽之和", collectionLevels{5, 4, 1}, collectionLevels{4, 4, 1}, 1},
		// ④ 三槽最大数。
		{"最大数", collectionLevels{3, 4, 1}, collectionLevels{2, 5, 1}, -1},
		// ⑤ 比较链无法区分。
		{"完全并列", collectionLevels{3, 4, 1}, collectionLevels{4, 3, 1}, 0},
		{"同一签名", collectionLevels{6, 6, 3}, collectionLevels{6, 6, 3}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectionLevelsBetter(tc.a, tc.b)
			switch {
			case tc.want > 0 && got <= 0:
				t.Fatalf("collectionLevelsBetter(%v, %v)=%d, want >0", tc.a, tc.b, got)
			case tc.want < 0 && got >= 0:
				t.Fatalf("collectionLevelsBetter(%v, %v)=%d, want <0", tc.a, tc.b, got)
			case tc.want == 0 && got != 0:
				t.Fatalf("collectionLevelsBetter(%v, %v)=%d, want 0", tc.a, tc.b, got)
			}
			// 反对称性。
			if rev := collectionLevelsBetter(tc.b, tc.a); rev != -got {
				t.Fatalf("collectionLevelsBetter 不满足反对称: f(a,b)=%d, f(b,a)=%d", got, rev)
			}
		})
	}
}

// TestCollectionLevelsRankIsDeterministic 覆盖比较链并列时的确定性兜底，
// 这保证同一份库存无论 map 遍历顺序如何都得到同一个保留对象。
func TestCollectionLevelsRankIsDeterministic(t *testing.T) {
	a, b := collectionLevels{3, 4, 1}, collectionLevels{4, 3, 1}
	if d := collectionLevelsRank(a, b); d == 0 {
		t.Fatalf("比较链并列时 rank 必须给出非零结果")
	}
	if collectionLevelsRank(a, b) != -collectionLevelsRank(b, a) {
		t.Fatalf("rank 不满足反对称")
	}
	// 同一签名 rank 为 0。
	same := collectionLevels{2, 2, 2}
	if d := collectionLevelsRank(same, same); d != 0 {
		t.Fatalf("同一签名 rank=%d, want 0", d)
	}
}

func collectionComboFor(slot1, slot2, slot3 int) collectionCombo {
	return collectionCombo{slot1, slot2, slot3}
}

// TestCollectionKeepModeA 覆盖模式 A：每类只保留 1 份比较链最优的，
// 且「盘点到过但被淘汰」的签名必须判定为丢弃，而不是走未知签名的跳过分支。
func TestCollectionKeepModeA(t *testing.T) {
	st := newCollectionState()
	combo := collectionComboFor(1, 2, 3)
	// 盘点到：最优 (6,1,3) 两? 不 —— 关键用例是 (6,1,3) 与 (5,5,3) 并存。
	for i := 0; i < 2; i++ {
		st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{6, 1, 3}})
	}
	for i := 0; i < 3; i++ {
		st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{5, 5, 3}})
	}

	plan := buildCollectionPlan(st, collectionKeepModeA)

	if got := plan.quota[collectionKey{combo, collectionLevels{6, 1, 3}}]; got != 1 {
		t.Fatalf("最优签名配额=%d, want 1", got)
	}
	if got := plan.quota[collectionKey{combo, collectionLevels{5, 5, 3}}]; got != 0 {
		t.Fatalf("被淘汰签名配额=%d, want 0（但必须存在于配额表中）", got)
	}

	// 执行遍：第一份最优 -> 锁定；第二份最优 -> 配额用尽，丢弃。
	if got := plan.decide(combo, collectionLevels{6, 1, 3}); got != collectionLock {
		t.Fatalf("第 1 份最优签名决策=%v, want lock", got)
	}
	if got := plan.decide(combo, collectionLevels{6, 1, 3}); got != collectionDiscard {
		t.Fatalf("第 2 份最优签名决策=%v, want discard", got)
	}
	// 被淘汰的签名「盘点到过」，必须丢弃而不是跳过。
	if got := plan.decide(combo, collectionLevels{5, 5, 3}); got != collectionDiscard {
		t.Fatalf("被淘汰签名决策=%v, want discard（这是防止漏丢的回归用例）", got)
	}
	// 从未见过的签名 -> 跳过，绝不丢弃。
	if got := plan.decide(combo, collectionLevels{1, 1, 1}); got != collectionSkip {
		t.Fatalf("未知签名决策=%v, want skip", got)
	}
	if plan.locked != 1 || plan.discarded != 2 || plan.skipped != 1 {
		t.Fatalf("计数 locked=%d discarded=%d skipped=%d, want 1/2/1",
			plan.locked, plan.discarded, plan.skipped)
	}
}

// TestCollectionKeepModeB 覆盖模式 B：额外保留三槽之和严格大于 9 的，边界 9 不保留。
func TestCollectionKeepModeB(t *testing.T) {
	st := newCollectionState()
	combo := collectionComboFor(1, 1, 1)
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{6, 6, 3}}) // T=15
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{5, 5, 3}}) // T=13
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{6, 2, 2}}) // T=10
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{6, 2, 1}}) // T=9 边界
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{1, 1, 1}}) // T=3
	// 让 T=9 的那条再多几份，确认「不因为数量多而被保留」。
	for i := 0; i < 4; i++ {
		st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{6, 2, 1}})
	}

	plan := buildCollectionPlan(st, collectionKeepModeB)

	want := map[collectionLevels]int{
		{6, 6, 3}: 1, // T=15 > 9
		{5, 5, 3}: 1, // T=13 > 9
		{6, 2, 2}: 1, // T=10 > 9
		{6, 2, 1}: 0, // T=9 不满足「严格大于」
		{1, 1, 1}: 0,
	}
	for levels, wantQuota := range want {
		if got := plan.quota[collectionKey{combo, levels}]; got != wantQuota {
			t.Fatalf("模式 B 签名 %v 配额=%d, want %d", levels, got, wantQuota)
		}
	}
}

// TestCollectionKeepModeC 覆盖模式 C：额外保留第三词条满级(3)的。
func TestCollectionKeepModeC(t *testing.T) {
	st := newCollectionState()
	combo := collectionComboFor(2, 3, 4)
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{6, 6, 1}}) // 比较链最优
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{1, 1, 3}}) // slot3 满级
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{1, 1, 3}}) // slot3 满级（多份）
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{1, 1, 2}})

	plan := buildCollectionPlan(st, collectionKeepModeC)

	want := map[collectionLevels]int{
		{6, 6, 1}: 1, // 模式 A 的最优
		{1, 1, 3}: 2, // 额外保留，且保留其全部份数
		{1, 1, 2}: 0,
	}
	for levels, wantQuota := range want {
		if got := plan.quota[collectionKey{combo, levels}]; got != wantQuota {
			t.Fatalf("模式 C 签名 %v 配额=%d, want %d", levels, got, wantQuota)
		}
	}
}

// TestCollectionKeepModeBTakesLowerChainThanSum 固化「链优先」的语义：
// 同类中 (6,1,3) 与 (5,5,3) 并存时，即使后者三槽之和更大，模式 A 仍保留前者。
func TestCollectionKeepModeBTakesLowerChainThanSum(t *testing.T) {
	st := newCollectionState()
	combo := collectionComboFor(3, 3, 3)
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{6, 1, 3}}) // T=10
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{5, 5, 3}}) // T=13

	plan := buildCollectionPlan(st, collectionKeepModeA)
	if got := plan.quota[collectionKey{combo, collectionLevels{6, 1, 3}}]; got != 1 {
		t.Fatalf("链优先：(6,1,3) 配额=%d, want 1", got)
	}
	if got := plan.quota[collectionKey{combo, collectionLevels{5, 5, 3}}]; got != 0 {
		t.Fatalf("链优先：(5,5,3) 配额=%d, want 0（尽管三槽之和更大）", got)
	}
}

// TestCollectionUniverseAndMissing 覆盖全集构造、缺失统计与稀缺度排序。
func TestCollectionUniverseAndMissing(t *testing.T) {
	pools := matchapi.SkillPools{
		Slot1: []matchapi.SkillPool{{ID: 1}, {ID: 2}},
		Slot2: []matchapi.SkillPool{{ID: 10}, {ID: 11}},
		Slot3: []matchapi.SkillPool{{ID: 20}, {ID: 21}},
	}
	universe := collectionUniverse(pools)
	if len(universe) != 8 {
		t.Fatalf("全集大小=%d, want 8", len(universe))
	}

	st := newCollectionState()
	st.record(&matchapi.CollectionMatch{SkillIDs: collectionCombo{1, 10, 20}, Levels: collectionLevels{1, 1, 1}})
	st.record(&matchapi.CollectionMatch{SkillIDs: collectionCombo{2, 11, 21}, Levels: collectionLevels{1, 1, 1}})

	missing := st.missing(universe)
	if len(missing) != 6 {
		t.Fatalf("缺失组合数=%d, want 6", len(missing))
	}

	// 稀缺度：只在一个地点出现的组合应排在前面。
	locations := []matchapi.Location{
		{Name: "locA", Slot2IDs: []int{10}, Slot3IDs: []int{20}},
		{Name: "locB", Slot2IDs: []int{10, 11}, Slot3IDs: []int{20, 21}},
	}
	sortCollectionMissing(missing, locations)
	first := missing[0]
	if got := collectionScarcity(locations, first); got != 1 {
		t.Fatalf("排序后首个组合稀缺度=%d, want 1", got)
	}
	for i := 1; i < len(missing); i++ {
		if collectionScarcity(locations, missing[i-1]) > collectionScarcity(locations, missing[i]) {
			t.Fatalf("稀缺度排序不单调: index %d", i)
		}
	}
	// 组合 -> 地点名。
	if got := collectionLocationsFor(locations, collectionCombo{1, 10, 20}); len(got) != 2 || got[0] != "locA" {
		t.Fatalf("collectionLocationsFor=%v, want [locA locB]", got)
	}
	if got := collectionLocationsFor(locations, collectionCombo{2, 11, 21}); len(got) != 1 || got[0] != "locB" {
		t.Fatalf("collectionLocationsFor=%v, want [locB]", got)
	}
}

// TestParseCollectionKeepMode 覆盖选项字符串到模式的映射，未知值回落到 A。
func TestParseCollectionKeepMode(t *testing.T) {
	cases := map[string]collectionKeepMode{
		"A": collectionKeepModeA,
		"B": collectionKeepModeB,
		"C": collectionKeepModeC,
		"":  collectionKeepModeA,
		"X": collectionKeepModeA,
	}
	for in, want := range cases {
		if got := parseCollectionKeepMode(in); got != want {
			t.Fatalf("parseCollectionKeepMode(%q)=%v, want %v", in, got, want)
		}
	}
}

// TestCollectionRecordIgnoresNil 覆盖 record 对 nil（OCR 未解析成三个不同槽位）的容错。
func TestCollectionRecordIgnoresNil(t *testing.T) {
	st := newCollectionState()
	st.record(nil)
	if st.scanned != 0 || st.classCount() != 0 {
		t.Fatalf("nil 记录不应计入: scanned=%d classes=%d", st.scanned, st.classCount())
	}
}

// newCollectionTestEngine 加载真实数据；不可用时跳过。
func newCollectionTestEngine(t *testing.T) *matchapi.Engine {
	t.Helper()
	engine, err := matchapi.NewDefaultEngine()
	if err != nil {
		t.Skipf("EssenceFilter 数据不可用，跳过: %v", err)
	}
	return engine
}

// TestCollectionTwoPassInvariants 用真实数据跑「盘点 -> 决策 -> 执行」，断言两遍协议的不变量。
//
// 这是整个功能最关键的一层保护：第一遍记录、第二遍处置，两边必须严格对齐。
// 一旦配额表漏掉某个盘点到过的签名，执行遍会走「未知签名 -> 跳过」分支，
// 该丢弃的基质就会原样留在仓库里（静默失效，不会报错）。
func TestCollectionTwoPassInvariants(t *testing.T) {
	engine := newCollectionTestEngine(t)
	universe := collectionUniverse(engine.SkillPools())

	state := newCollectionState()
	for i, combo := range universe {
		if i%3 == 0 {
			continue // 留出「完全缺失」的组合，供板块建议刷取
		}
		switch i % 4 {
		case 0: // 两档签名，比较链可分辨
			state.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{6, 6, 3}})
			state.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{5, 5, 3}})
		case 1: // 单档
			state.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{4, 4, 2}})
		case 2: // 同签名多份 + 一档更高的
			state.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{1, 1, 1}})
			state.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{1, 1, 1}})
			state.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{2, 2, 1}})
		default: // 技能槽 3，模式 C 的额外保留对象
			state.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{3, 3, 3}})
		}
	}
	if state.scanned == 0 {
		t.Fatal("盘点结果为空，用例无效")
	}

	for _, mode := range []collectionKeepMode{collectionKeepModeA, collectionKeepModeB, collectionKeepModeC} {
		t.Run(mode.String(), func(t *testing.T) {
			plan := buildCollectionPlan(state, mode)

			// 执行遍：按盘点结果遍历每一份基质。
			for combo, class := range state.classes {
				for levels, n := range class.counts {
					for k := 0; k < n; k++ {
						if got := plan.decide(combo, levels); got == collectionSkip {
							t.Fatalf("组合 %v 的签名 %v 在盘点中出现过，却返回 skip（会漏丢）", combo, levels)
						}
					}
				}
			}
			if plan.skipped != 0 {
				t.Fatalf("skipped=%d, want 0：盘点见过的签名不应进入跳过分支", plan.skipped)
			}
			if plan.locked != plan.keepTotal() {
				t.Fatalf("locked=%d, keepTotal=%d：锁定数应恰好用尽配额", plan.locked, plan.keepTotal())
			}
			if plan.locked+plan.discarded != state.scanned {
				t.Fatalf("locked+discarded=%d, scanned=%d：每份基质都必须有处置",
					plan.locked+plan.discarded, state.scanned)
			}
			// 模式 A：每类恰好一个签名被保留。
			if mode == collectionKeepModeA {
				for combo, class := range state.classes {
					kept := 0
					for levels := range class.counts {
						if plan.quota[collectionKey{combo, levels}] > 0 {
							kept++
						}
					}
					if kept != 1 {
						t.Fatalf("模式 A 组合 %v 保留了 %d 个签名, want 1", combo, kept)
					}
				}
			}
			// 模式 B/C 的额外保留只会放宽模式 A，不会收紧。
			if mode != collectionKeepModeA {
				if plan.keepTotal() < state.classCount() {
					t.Fatalf("%s: keepTotal=%d 少于类数 %d", mode, plan.keepTotal(), state.classCount())
				}
			}

			board := buildCollectionBoard(state, plan, mode, engine, false)
			if board.Total != len(universe) {
				t.Fatalf("板块总数=%d, want %d", board.Total, len(universe))
			}
			if board.Collected+len(board.Missing) != board.Total {
				t.Fatalf("已收集 %d + 缺失 %d != 总数 %d",
					board.Collected, len(board.Missing), board.Total)
			}
			// 缺失组合按稀缺度升序（可刷地点少的排前面）。
			for i := 1; i < len(board.Missing); i++ {
				if board.Missing[i-1].Scarcity > board.Missing[i].Scarcity {
					t.Fatalf("稀缺度排序不单调: index %d (%d > %d)",
						i, board.Missing[i-1].Scarcity, board.Missing[i].Scarcity)
				}
			}

			// 未知签名（盘点中从未出现）必须仍然跳过，绝不能误丢。
			if got := plan.decide(collectionCombo{99, 99, 99}, collectionLevels{9, 9, 9}); got != collectionSkip {
				t.Fatalf("未知签名决策=%v, want skip", got)
			}
		})
	}
}

// TestCollectionReportRenders 校验 840 模块的输出契约。
//
// 与原版 plan_recommend 一致：同一份 HTML 既进日志也落文件，靠「内容有界」保证
// 日志可读（分段按地点 + 每地点一张卡片 + 卡片内组合数封顶）。这里同时守住有界性，
// 防止以后又把上千个组合倒进日志。
func TestCollectionReportRenders(t *testing.T) {
	i18n.Init()
	if i18n.T("essencefilter.collection.report.title") == "essencefilter.collection.report.title" {
		t.Skip("locale 未加载，跳过渲染校验")
	}
	makeBoard := func() *collectionBoard {
		return &collectionBoard{
			Collected:    812,
			Total:        840,
			Percent:      "96.7",
			KeepMode:     collectionKeepModeB,
			MissingCount: 28,
			Scanned:      48, Signatures: 45,
			Locations: []collectionLocationPlan{
				{
					Name: "重度能量淤积点·源石研究园", ShortName: "源石研究园",
					MissingCount: 12, ScarceCount: 4, MinScarcity: 3,
					Slot2Text: "攻击、寒冷", Slot3Text: "强攻、压制",
					PreviewText: "力量 | 攻击 | 强攻、意志 | 寒冷 | 压制",
				},
				{
					Name: "重度能量淤积点·应龙关", ShortName: "应龙关",
					MissingCount: 9, ScarceCount: 0, MinScarcity: 3,
					Slot2Text: "物理", Slot3Text: "追袭",
					PreviewText: "智识 | 物理 | 追袭",
				},
			},
			Locked: 100, Spared: 0, Discarded: 1200, Skipped: 2,
		}
	}

	out := collectionReportHTML(makeBoard())
	assertRendered(t, out)
	for _, want := range []string{
		"源石研究园", "应龙关",
		"力量 | 攻击 | 强攻", "意志 | 寒冷 | 压制",
		"攻击、寒冷", "强攻、压制",
		"96.7%", "48",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("输出缺少 %q:\n%s", want, out)
		}
	}
	// 分段按地点（原版 plan_recommend 的结构），不能用 <details>：日志里不会折叠。
	if strings.Contains(out, "<details") {
		t.Fatalf("日志与网页共用同一份 HTML，不能依赖 <details> 折叠:\n%s", out)
	}
	// 每个地点一张卡片
	if got := strings.Count(out, "border-left:3px solid"); got != 2 {
		t.Fatalf("应为每个地点渲染一张卡片（2 张），实际 %d:\n%s", got, out)
	}

	// ---- 有界性：卡片内组合数封顶，超出走 MoreCount ----
	long := makeBoard()
	long.Locations = []collectionLocationPlan{{
		Name: "重度能量淤积点·源石研究园", ShortName: "源石研究园",
		MissingCount: 296, ScarceCount: 12, MinScarcity: 3,
		Slot2Text: "攻击", Slot3Text: "强攻",
		PreviewText: "a | b | c、d | e | f", MoreCount: 294,
	}}
	longOut := collectionReportHTML(long)
	assertRendered(t, longOut)
	if !strings.Contains(longOut, "294") {
		t.Fatalf("超出上限时应提示剩余条数:\n%s", longOut)
	}

	// ---- 全部收集完成 ----
	done := makeBoard()
	done.Locations, done.MissingCount = nil, 0
	done.Collected, done.Percent = 840, "100.0"
	doneOut := collectionReportHTML(done)
	assertRendered(t, doneOut)
	if !strings.Contains(doneOut, i18n.T("essencefilter.collection.report.missing_none")) {
		t.Fatalf("无缺失时应显示完成文案:\n%s", doneOut)
	}

	// ---- 预演分支 ----
	dry := makeBoard()
	dry.DryRun = true
	dryOut := collectionReportHTML(dry)
	assertRendered(t, dryOut)
	if !strings.Contains(dryOut, i18n.T("essencefilter.collection.report.dry_run_note")) {
		t.Fatalf("预演分支缺少预演提示:\n%s", dryOut)
	}
	if strings.Contains(dryOut, i18n.T("essencefilter.collection.report.skip_note")) {
		t.Fatalf("预演分支不应出现执行遍的跳过说明:\n%s", dryOut)
	}

	// ---- nil 必须安全 ----
	if got := collectionReportHTML(nil); got != "" {
		t.Fatalf("nil board 渲染结果应为空，实际 %q", got)
	}

	// ---- 可选：导出预览（贴近真实状态：12 个地点 + 预演分支）----
	if path := os.Getenv("MAAEND_COLLECTION_PREVIEW"); path != "" {
		board := makeBoard()
		board.DryRun = true
		board.Collected, board.Total, board.Percent = 40, 840, "4.8"
		board.MissingCount, board.Locked, board.Spared, board.Discarded = 800, 41, 0, 7
		board.Locations = previewLocations()
		board.ScarceTotal = 48
		doc := wrapHTMLDocument(i18n.T("essencefilter.collection.report.title"),
			i18n.T("essencefilter.collection.html.notice"), collectionReportHTML(board))
		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
			t.Fatalf("写入预览失败: %v", err)
		}
		t.Logf("预览已写入 %s", path)
	}
}

// TestCollectionCardIsBounded 守住「每地点卡片最多列 collectionCombosPerCard 条」。
func TestCollectionCardIsBounded(t *testing.T) {
	engine := newCollectionTestEngine(t)
	locations := engine.Locations()
	pools := engine.SkillPools()

	missing := collectionUniverse(pools) // 全部缺失 -> 每个地点都远超上限
	plans := buildLocationPlans(newCollectionState(), engine, collectionUniverse(pools), missing)
	if len(plans) == 0 {
		t.Skip("测试数据没有可用地点")
	}
	for _, plan := range plans {
		shown := 0
		if plan.PreviewText != "" {
			shown = strings.Count(plan.PreviewText, i18n.Separator()) + 1
		}
		if shown > collectionCombosPerCard {
			t.Fatalf("%s 卡片列出 %d 条，超过上限 %d", plan.ShortName, shown, collectionCombosPerCard)
		}
		if shown+plan.MoreCount != plan.MissingCount {
			t.Fatalf("%s 展示 %d + 截断 %d != 可补 %d",
				plan.ShortName, shown, plan.MoreCount, plan.MissingCount)
		}
		// 短名不能带公共前缀
		if strings.Contains(plan.ShortName, "·") {
			t.Fatalf("%s 的短名仍带分隔符", plan.ShortName)
		}
		_ = locations
	}
}

func assertRendered(t *testing.T, out string) {
	t.Helper()
	if strings.Contains(out, "%!") {
		t.Fatalf("占位符契约不匹配（fmt 报错）:\n%s", out)
	}
	if strings.Contains(out, "essencefilter.") {
		t.Fatalf("模板残留未解析的键:\n%s", out)
	}
}

// TestCollectionDryRunIsTheSafeDefault 覆盖三态解析。
// 字段缺失必须按「预演」处理：标记丢弃不可逆，配置没传到就真执行是不可接受的。
func TestCollectionDryRunIsTheSafeDefault(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		opts *EssenceFilterOptions
		want bool
	}{
		{"nil 选项", nil, true},
		{"字段缺失", &EssenceFilterOptions{}, true},
		{"显式预演", &EssenceFilterOptions{CollectionDryRun: &yes}, true},
		{"显式执行", &EssenceFilterOptions{CollectionDryRun: &no}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opts.collectionDryRun(); got != tc.want {
				t.Fatalf("collectionDryRun()=%v, want %v", got, tc.want)
			}
		})
	}
}

// TestCollectionBoardDryRunUsesPlannedCounts 覆盖预演板块：
// 预演不进执行遍，所以锁定/丢弃必须取计划值，跳过数归零，且 plan 的实际计数保持 0。
func TestCollectionBoardDryRunUsesPlannedCounts(t *testing.T) {
	engine := newCollectionTestEngine(t)
	universe := collectionUniverse(engine.SkillPools())

	state := newCollectionState()
	for i, combo := range universe {
		if i%2 == 0 {
			continue
		}
		state.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{6, 6, 3}})
		state.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{1, 1, 1}})
	}

	plan := buildCollectionPlan(state, collectionKeepModeA)
	board := buildCollectionBoard(state, plan, collectionKeepModeA, engine, true)

	if !board.DryRun {
		t.Fatal("board.DryRun 应为 true")
	}
	if board.Locked != plan.keepTotal() {
		t.Fatalf("预演应展示计划锁定数 %d，实际 %d", plan.keepTotal(), board.Locked)
	}
	if want := state.scanned - plan.keepTotal(); board.Discarded != want {
		t.Fatalf("预演应展示计划丢弃数 %d，实际 %d", want, board.Discarded)
	}
	if board.Skipped != 0 {
		t.Fatalf("预演没有执行遍，跳过数应为 0，实际 %d", board.Skipped)
	}
	if plan.locked != 0 || plan.discarded != 0 {
		t.Fatalf("预演不应产生实际计数：locked=%d discarded=%d", plan.locked, plan.discarded)
	}
}

// TestCollectionPolicySwitchesAreSafeByDefault 覆盖两个策略开关的三态解析。
func TestCollectionPolicySwitchesAreSafeByDefault(t *testing.T) {
	yes, no := true, false
	if (&EssenceFilterOptions{}).collectionDiscardLocked() {
		t.Fatal("collection_discard_locked 缺失时必须按 false（不动已锁定）")
	}
	if !(&EssenceFilterOptions{CollectionDiscardLocked: &yes}).collectionDiscardLocked() {
		t.Fatal("显式 true 应生效")
	}
	if (&EssenceFilterOptions{CollectionDiscardLocked: &no}).collectionDiscardLocked() {
		t.Fatal("显式 false 应生效")
	}
	if !(&EssenceFilterOptions{}).collectionLockKeepers() {
		t.Fatal("collection_lock_keepers 缺失时必须按 true")
	}
	if (&EssenceFilterOptions{CollectionLockKeepers: &no}).collectionLockKeepers() {
		t.Fatal("显式 false 应关闭锁定")
	}
}

// TestCollectionGridOverridesCoverLocked 覆盖两遍的网格过滤差异：
// 盘点遍必须包含已锁定（否则进度与配额都算错），执行遍按开关决定是否纳入已锁定。
func TestCollectionGridOverridesCoverLocked(t *testing.T) {
	scan := collectionScanGridOverride()["EssenceGridAdvance"].(map[string]any)["attach"].(map[string]any)
	if scan["skip_thumb_lock"] != false {
		t.Fatal("盘点遍必须包含已锁定基质")
	}
	if scan["skip_thumb_discard"] != true {
		t.Fatal("盘点遍必须跳过已标记弃置")
	}
	applyKeep := collectionApplyGridOverride(true)["EssenceGridAdvance"].(map[string]any)["attach"].(map[string]any)
	if applyKeep["skip_thumb_lock"] != false {
		t.Fatal("discardLocked=true 时执行遍应纳入已锁定")
	}
	applySkip := collectionApplyGridOverride(false)["EssenceGridAdvance"].(map[string]any)["attach"].(map[string]any)
	if applySkip["skip_thumb_lock"] != true {
		t.Fatal("discardLocked=false 时执行遍应跳过已锁定")
	}
}

// TestCollectionPlanWithoutLockingKeepers 覆盖「未锁定的进行锁定 = 否」：
// 保留配额照样消耗（决定哪些不被弃置），但命中配额的走跳过而不是锁定，并计入 spared。
func TestCollectionPlanWithoutLockingKeepers(t *testing.T) {
	st := newCollectionState()
	combo := collectionComboFor(1, 2, 3)
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{6, 6, 3}})
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{6, 6, 3}})
	st.record(&matchapi.CollectionMatch{SkillIDs: combo, Levels: collectionLevels{1, 1, 1}})

	plan := buildCollectionPlanWithPolicy(st, collectionKeepModeA, false)
	if plan.lockKeepers {
		t.Fatal("lockKeepers 应为 false")
	}
	if got := plan.decide(combo, collectionLevels{6, 6, 3}); got != collectionSkip {
		t.Fatalf("第 1 份最优应跳过（保留不锁），实际 %v", got)
	}
	if got := plan.decide(combo, collectionLevels{6, 6, 3}); got != collectionDiscard {
		t.Fatalf("第 2 份最优超出配额应丢弃，实际 %v", got)
	}
	if got := plan.decide(combo, collectionLevels{1, 1, 1}); got != collectionDiscard {
		t.Fatalf("被淘汰签名应丢弃，实际 %v", got)
	}
	if plan.locked != 0 || plan.spared != 1 || plan.discarded != 2 {
		t.Fatalf("计数 locked=%d spared=%d discarded=%d, want 0/1/2",
			plan.locked, plan.spared, plan.discarded)
	}
}

// previewLocations 只用于生成预览文件，模拟「差得还很多」时的真实观感。
func previewLocations() []collectionLocationPlan {
	type entry struct {
		name         string
		short        string
		missing      int
		scarce       int
		slot2, slot3 string
		combos       []string
	}
	raw := []entry{
		{"重度能量淤积点·源石研究园", "源石研究园", 296, 12, "攻击、灼热、电磁、寒冷、自然、源石技艺强度、终结技充能、法术", "强攻、压制、追袭、粉碎、巧技、迸发、流转、效益",
			[]string{"主能力 | 攻击 | 强攻", "主能力 | 攻击 | 压制", "力量 | 灼热 | 追袭", "意志 | 寒冷 | 粉碎"}},
		{"重度能量淤积点·应龙关", "应龙关", 290, 8, "攻击、物理、灼热、自然、生命、法术、暴击率、治疗", "强攻、压制、追袭、粉碎、巧技、迸发、残暴、昂扬",
			[]string{"主能力 | 物理 | 巧技", "敏捷 | 攻击 | 迸发", "力量 | 生命 | 昂扬"}},
		{"重度能量淤积点·供能高地", "供能高地", 285, 5, "攻击、寒冷、电磁、自然、生命、治疗、暴击率、终结技充能", "强攻、压制、切骨、医疗、夜幕、流转、附术、效益",
			[]string{"智识 | 治疗效果 | 医疗", "主能力 | 寒冷 | 夜幕"}},
		{"重度能量淤积点·矿脉源区", "矿脉源区", 280, 9, "物理、灼热、电磁、源石技艺强度、法术、攻击、生命、暴击率", "切骨、医疗、压制、夜幕、巧技、残暴、粉碎、附术",
			[]string{"力量 | 物理 | 切骨", "意志 | 电磁 | 残暴"}},
		{"重度能量淤积点·枢纽区", "枢纽区", 275, 6, "攻击、灼热、电磁、寒冷、自然、源石技艺强度、终结技充能、法术", "强攻、压制、追袭、粉碎、巧技、迸发、流转、效益",
			[]string{"主能力 | 终结技充能 | 效益", "敏捷 | 自然 | 流转"}},
		{"重度能量淤积点·武陵城", "武陵城", 268, 4, "攻击、寒冷、自然、生命、治疗、法术、暴击率、物理", "强攻、压制、追袭、巧技、迸发、昂扬、效益、流转",
			[]string{"智识 | 暴击率 | 昂扬", "力量 | 治疗 | 迸发"}},
		{"重度能量淤积点·清波寨", "清波寨", 262, 3, "攻击、物理、寒冷、电磁、自然、攻击、治疗、终结技充能", "切骨、医疗、压制、追袭、巧技、粉碎、附术、效益",
			[]string{"意志 | 电磁 | 附术", "主能力 | 物理 | 切骨"}},
		{"重度能量淤积点·首墩", "首墩", 255, 2, "物理、灼热、电磁、寒冷、源石技艺强度、攻击、生命、暴击率", "强攻、压制、追袭、昂扬、残暴、迸发、流转、效益",
			[]string{"敏捷 | 源石技艺强度 | 昂扬", "力量 | 灼热 | 残暴"}},
		{"重度能量淤积点·藏剑谷", "藏剑谷", 248, 1, "攻击、寒冷、电磁、自然、生命、治疗、法术、暴击率", "切骨、医疗、压制、夜幕、巧技、残暴、粉碎、附术",
			[]string{"智识 | 治疗 | 医疗", "主能力 | 寒冷 | 夜幕"}},
		{"重度能量淤积点·北部禁区", "北部禁区", 241, 0, "物理、灼热、电磁、攻击、源石技艺强度、生命、暴击率、法术", "切骨、医疗、压制、夜幕、巧技、残暴、粉碎、附术",
			[]string{"力量 | 物理 | 粉碎", "意志 | 生命 | 残暴"}},
		{"重度能量淤积点·试验园区", "试验园区", 233, 0, "攻击、寒冷、电磁、自然、源石技艺强度、法术、暴击率、治疗", "强攻、压制、切骨、医疗、夜幕、流转、附术、效益",
			[]string{"主能力 | 源石技艺强度 | 附术", "敏捷 | 自然 | 流转"}},
		{"重度能量淤积点·雪松林", "雪松林", 226, 0, "物理、灼热、寒冷、生命、治疗、攻击、暴击率、终结技充能", "强攻、压制、追袭、巧技、迸发、昂扬、效益、流转",
			[]string{"智识 | 终结技充能 | 效益", "力量 | 灼热 | 昂扬"}},
	}
	out := make([]collectionLocationPlan, 0, len(raw))
	for _, e := range raw {
		// 卡片内容有界：展示前 8 条，其余进 MoreCount（与 buildLocationPlans 的行为一致）。
		shown := e.combos
		if len(shown) > collectionCombosPerCard {
			shown = shown[:collectionCombosPerCard]
		}
		out = append(out, collectionLocationPlan{
			Name: e.name, ShortName: e.short, MissingCount: e.missing, ScarceCount: e.scarce,
			MinScarcity: 3, Slot2Text: e.slot2, Slot3Text: e.slot3, Combos: e.combos,
			PreviewText: strings.Join(shown, i18n.Separator()), MoreCount: e.missing - len(shown),
		})
	}
	return out
}

// TestCollectionScannedCountIsSelfEvident 守住「盘点到多少无瑕基质」必须能自证。
//
// 曾经只有 classes / signatures / keep+discard 三个间接数字，读者容易把「类」当成
// 「我有几个基质」。现在板子显式给出 Scanned，并要求与明细相加一致。
func TestCollectionScannedCountIsSelfEvident(t *testing.T) {
	engine := newCollectionTestEngine(t)
	universe := collectionUniverse(engine.SkillPools())

	state := newCollectionState()
	// 造 6 个无瑕基质：4 个同组合不同等级 + 2 个另一组合
	comboA, comboB := universe[0], universe[1]
	for _, lv := range []collectionLevels{{6, 6, 3}, {6, 6, 1}, {1, 1, 1}, {6, 6, 3}} {
		state.record(&matchapi.CollectionMatch{SkillIDs: comboA, Levels: lv})
	}
	for _, lv := range []collectionLevels{{6, 6, 3}, {6, 6, 1}} {
		state.record(&matchapi.CollectionMatch{SkillIDs: comboB, Levels: lv})
	}
	if state.scanned != 6 {
		t.Fatalf("盘点数应为 6，实际 %d", state.scanned)
	}

	for _, mode := range []collectionKeepMode{collectionKeepModeA, collectionKeepModeB, collectionKeepModeC} {
		for _, lock := range []bool{true, false} {
			plan := buildCollectionPlanWithPolicy(state, mode, lock)
			board := buildCollectionBoard(state, plan, mode, engine, true)

			if board.Scanned != 6 {
				t.Fatalf("mode=%v lock=%v: board.Scanned=%d, want 6", mode, lock, board.Scanned)
			}
			if board.Collected != 2 {
				t.Fatalf("mode=%v lock=%v: 类数应为 2，实际 %d", mode, lock, board.Collected)
			}
			// 明细相加必须等于盘点数，否则面板自相矛盾
			sum := board.Locked + board.Spared + board.Discarded + board.Skipped
			if sum != board.Scanned {
				t.Fatalf("mode=%v lock=%v: 锁定 %d + 保留未锁 %d + 丢弃 %d + 跳过 %d = %d != 盘点 %d",
					mode, lock, board.Locked, board.Spared, board.Discarded, board.Skipped, sum, board.Scanned)
			}
		}
	}

	// 日志字段名必须显式（不是靠 classes 猜）
	src, err := os.ReadFile("ui.go")
	if err != nil {
		t.Fatalf("读取 ui.go 失败: %v", err)
	}
	if !strings.Contains(string(src), `Int("scanned_gold_essences"`) {
		t.Fatal("报告日志必须显式给出 scanned_gold_essences")
	}
}
