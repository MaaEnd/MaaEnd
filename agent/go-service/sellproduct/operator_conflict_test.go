package sellproduct

import "testing"

// TestFindOperatorConflictSourceSupportsLocalizedPrompt 验证派驻弹窗可从多语言完整文本中定位来源据点。
func TestFindOperatorConflictSourceSupportsLocalizedPrompt(t *testing.T) {
	data := &sellProductSelectionDataFile{
		LocationOrder: []string{"RefugeeCamp", "ReconstructionHQ"},
		Locations: map[string]selectionDataLocation{
			"RefugeeCamp": {
				Names: map[string]string{"zh_cn": "难民暂居处", "en_us": "Refugee Camp"},
			},
			"ReconstructionHQ": {
				Names: map[string]string{"zh_cn": "重建指挥部", "en_us": "Reconstruction HQ"},
			},
		},
	}
	tests := []struct {
		name string
		text string
	}{
		{name: "简体中文", text: "干员莱万汀已派驻至重建指挥部，是否确认更改派驻至当前据点？"},
		{name: "英文", text: "Operator Laevatain already assigned to Reconstruction HQ. Re-assign the operator to this outpost?"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			location, prompt, ok := findOperatorConflictSource([]ocrItem{{text: test.text}}, data)
			if !ok || location != "ReconstructionHQ" {
				t.Fatalf("来源据点 = %q，命中状态 = %v，期望 ReconstructionHQ", location, ok)
			}
			if prompt != test.text {
				t.Fatalf("弹窗文本 = %q，期望保留原始文本 %q", prompt, test.text)
			}
		})
	}
}

// TestFindOperatorConflictSourceProtectsUnknownLocation 验证无法识别来源时不会误判为本次可接管据点。
func TestFindOperatorConflictSourceProtectsUnknownLocation(t *testing.T) {
	data := &sellProductSelectionDataFile{
		LocationOrder: []string{"RefugeeCamp"},
		Locations: map[string]selectionDataLocation{
			"RefugeeCamp": {Names: map[string]string{"zh_cn": "难民暂居处"}},
		},
	}
	text := "干员莱万汀已派驻至未知据点，是否确认更改派驻至当前据点？"
	location, prompt, ok := findOperatorConflictSource([]ocrItem{{text: text}}, data)
	if ok || location != "" {
		t.Fatalf("来源据点 = %q，命中状态 = %v，未知来源应保持受保护", location, ok)
	}
	if prompt != text {
		t.Fatalf("弹窗文本 = %q，期望 %q", prompt, text)
	}
}

// TestOperatorConflictSourceManagedUsesActiveBoundary 验证只接管本次任务启用据点中的已识别来源。
func TestOperatorConflictSourceManagedUsesActiveBoundary(t *testing.T) {
	active := map[string]struct{}{"ReconstructionHQ": {}}
	tests := []struct {
		name       string
		source     string
		recognized bool
		managed    bool
	}{
		{name: "启用据点", source: "ReconstructionHQ", recognized: true, managed: true},
		{name: "未启用据点", source: "RefugeeCamp", recognized: true, managed: false},
		{name: "未知来源", source: "", recognized: false, managed: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			managed := operatorConflictSourceManaged(test.source, test.recognized, active)
			if managed != test.managed {
				t.Fatalf("接管状态 = %v，期望 %v", managed, test.managed)
			}
		})
	}
}

// TestParseOperatorConflictParam 验证冲突识别参数只接受明确的用途、结果和据点。
func TestParseOperatorConflictParam(t *testing.T) {
	p, err := parseOperatorConflictParam(`{"result":"managed","usage":"target","location":"RefugeeCamp"}`)
	if err != nil {
		t.Fatalf("解析有效参数失败：%v", err)
	}
	if p.Result != operatorConflictResultManaged || p.Usage != operatorActionUsageTarget || p.Location != "RefugeeCamp" {
		t.Fatalf("解析结果不符合预期：%#v", p)
	}
	if _, err := parseOperatorConflictParam(`{"result":"unknown","usage":"target","location":"RefugeeCamp"}`); err == nil {
		t.Fatal("未知冲突结果必须返回错误")
	}
}
