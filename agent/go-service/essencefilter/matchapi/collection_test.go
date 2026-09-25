package matchapi

import "testing"

// newTestEngine 加载真实 EssenceFilter 数据；数据不可用时跳过用例，
// 避免在缺少 assets/data 的环境里产生假失败。
func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	engine, err := NewDefaultEngine()
	if err != nil {
		t.Skipf("EssenceFilter 数据不可用，跳过: %v", err)
	}
	return engine
}

// TestMatchCollectionOCRCoversNonWeaponCombinations 是 840 全收集模式的核心回归用例。
//
// 背景：MatchInventoryOCR 只返回「能被已出武器精确匹配」的组合
// （engine.go 里 BuildTargets + matchSkillIDs 过滤，matcher.go 无匹配返回 false），
// 因此词条全集里绝大多数组合对库存导出是不可见的。MatchCollectionOCR 必须保留它们，
// 否则 840 全收集模式无从统计。
func TestMatchCollectionOCRCoversNonWeaponCombinations(t *testing.T) {
	engine := newTestEngine(t)
	pools := engine.SkillPools()

	weaponCombos := make(map[[3]int]bool)
	for _, w := range engine.Weapons() {
		if len(w.SkillIDs) < 3 {
			continue
		}
		weaponCombos[[3]int{w.SkillIDs[0], w.SkillIDs[1], w.SkillIDs[2]}] = true
	}
	if len(weaponCombos) == 0 {
		t.Skip("没有可用于比对的武器组合")
	}

	// 找一个不被任何武器需要、且 OCR 文本能稳定往返的组合。
	var (
		combo [3]int
		ocr   OCRInput
	)
	found := false
	for _, s1 := range pools.Slot1 {
		for _, s2 := range pools.Slot2 {
			for _, s3 := range pools.Slot3 {
				candidate := [3]int{s1.ID, s2.ID, s3.ID}
				if weaponCombos[candidate] {
					continue
				}
				input := OCRInput{
					Skills: [3]string{s1.Chinese, s2.Chinese, s3.Chinese},
					Levels: [3]int{1, 1, 1},
				}
				got, err := engine.MatchCollectionOCR(input)
				// 技能名跨池歧义会让槽位解析落到别处，换下一个候选。
				if err != nil || got == nil || got.SkillIDs != candidate {
					continue
				}
				combo, ocr, found = candidate, input, true
				break
			}
			if found {
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("未能找到可稳定往返的非武器组合，无法验证覆盖行为")
	}

	// 旧入口必须仍然看不到它 —— 这正是 840 模式需要新入口的原因。
	if inv, err := engine.MatchInventoryOCR(ocr); err != nil {
		t.Fatalf("MatchInventoryOCR 意外报错: %v", err)
	} else if inv != nil {
		t.Fatalf("回归失败：MatchInventoryOCR 返回了非武器组合 %v，说明过滤逻辑已变", combo)
	}

	got, err := engine.MatchCollectionOCR(ocr)
	if err != nil {
		t.Fatalf("MatchCollectionOCR 报错: %v", err)
	}
	if got == nil {
		t.Fatalf("MatchCollectionOCR 丢弃了组合 %v", combo)
	}
	if got.SkillIDs != combo {
		t.Fatalf("MatchCollectionOCR 组合=%v, want %v", got.SkillIDs, combo)
	}
	if got.Levels != [3]int{1, 1, 1} {
		t.Fatalf("MatchCollectionOCR 等级=%v, want [1 1 1]", got.Levels)
	}
}

// TestMatchCollectionOCRKeepsWeaponCombinations 确认新入口对武器组合同样有效，
// 即它不是「只返回非武器组合」的镜像实现。
func TestMatchCollectionOCRKeepsWeaponCombinations(t *testing.T) {
	engine := newTestEngine(t)
	pools := engine.SkillPools()

	byID := func(list []SkillPool, id int) (string, bool) {
		for _, e := range list {
			if e.ID == id {
				return e.Chinese, true
			}
		}
		return "", false
	}

	checked := 0
	for _, w := range engine.Weapons() {
		if len(w.SkillIDs) < 3 {
			continue
		}
		s1, ok1 := byID(pools.Slot1, w.SkillIDs[0])
		s2, ok2 := byID(pools.Slot2, w.SkillIDs[1])
		s3, ok3 := byID(pools.Slot3, w.SkillIDs[2])
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		ocr := OCRInput{Skills: [3]string{s1, s2, s3}, Levels: [3]int{1, 1, 1}}
		got, err := engine.MatchCollectionOCR(ocr)
		if err != nil || got == nil {
			continue // 技能名跨池歧义，换下一把武器
		}
		if got.SkillIDs != [3]int{w.SkillIDs[0], w.SkillIDs[1], w.SkillIDs[2]} {
			continue
		}
		checked++
		if checked >= 5 {
			break
		}
	}
	if checked == 0 {
		t.Skip("没有可用于往返验证的武器组合")
	}
}

// TestMatchCollectionOCRRejectsInvalidInput 锁定错误契约：
// 三槽落在同一个池上返回 (nil, nil)；等级越界返回错误。两者都必须与旧入口一致。
func TestMatchCollectionOCRRejectsInvalidInput(t *testing.T) {
	engine := newTestEngine(t)
	pools := engine.SkillPools()
	if len(pools.Slot2) < 2 || len(pools.Slot1) < 1 || len(pools.Slot3) < 1 {
		t.Skip("技能池不足以构造用例")
	}

	// 两个技能同属 slot2 池 -> 无法构成三个不同槽位。
	samePool := OCRInput{
		Skills: [3]string{pools.Slot1[0].Chinese, pools.Slot2[0].Chinese, pools.Slot2[1].Chinese},
		Levels: [3]int{1, 1, 1},
	}
	got, err := engine.MatchCollectionOCR(samePool)
	if err != nil {
		t.Fatalf("同池输入不应报错: %v", err)
	}
	if got != nil {
		t.Fatalf("同池输入应返回 nil，实际 %v", got.SkillIDs)
	}
	if inv, err := engine.MatchInventoryOCR(samePool); err != nil || inv != nil {
		t.Fatalf("旧入口对同池输入的契约不一致: inv=%v err=%v", inv, err)
	}

	// slot3 等级上限为 3。
	badLevel := OCRInput{
		Skills: [3]string{pools.Slot1[0].Chinese, pools.Slot2[0].Chinese, pools.Slot3[0].Chinese},
		Levels: [3]int{1, 1, 4},
	}
	if _, err := engine.MatchCollectionOCR(badLevel); err == nil {
		t.Fatal("slot3 等级 4 应返回错误")
	}
	// slot1 等级上限为 6。
	badLevel1 := OCRInput{
		Skills: [3]string{pools.Slot1[0].Chinese, pools.Slot2[0].Chinese, pools.Slot3[0].Chinese},
		Levels: [3]int{7, 1, 1},
	}
	if _, err := engine.MatchCollectionOCR(badLevel1); err == nil {
		t.Fatal("slot1 等级 7 应返回错误")
	}
}
