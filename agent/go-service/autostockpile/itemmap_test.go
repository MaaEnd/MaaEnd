package autostockpile

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestParseTierFromID(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want string
	}{
		{"普通两级 ID", "ValleyIV/OriginiumSaplings.Tier3", "ValleyIV.Tier3"},
		{"武陵二级 ID", "Wuling/XiraniteBridge.Tier2", "Wuling.Tier2"},
		{"没有地区前缀", "OriginiumSaplings.Tier3", ""},
		{"没有 Tier 后缀", "Wuling/QingboRafts", ""},
		{"后缀不是 Tier", "Wuling/QingboRafts.Rare", ""},
		{"最后一段不是 Tier", "Wuling/QingboRafts.Tier2.Extra", ""},
		{"空字符串", "", ""},
		{"只有斜杠", "/", ""},
		{"地区为空", "/Item.Tier1", ".Tier1"},
	}

	for _, tc := range cases {
		if got := parseTierFromID(tc.id); got != tc.want {
			t.Errorf("%s：parseTierFromID(%q) = %q，期望 %q", tc.name, tc.id, got, tc.want)
		}
	}
}

func TestBuildTemplatePath(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"Wuling/XiraniteBridge.Tier2", "AutoStockpile/Goods/Wuling/XiraniteBridge.Tier2.png"},
		{"ValleyIV/AnkhorillingKitchenware.Tier1", "AutoStockpile/Goods/ValleyIV/AnkhorillingKitchenware.Tier1.png"},
	}

	for _, tc := range cases {
		if got := buildTemplatePath(tc.id); got != tc.want {
			t.Errorf("buildTemplatePath(%q) = %q，期望 %q", tc.id, got, tc.want)
		}
	}
}

// TestItemMapRealDataIsConsistent 拿仓库里真实的 item_map.json 校验：
// 每个 ID 都能解析出档位、拼出合法路径，且中英文名不重复。
func TestItemMapRealDataIsConsistent(t *testing.T) {
	raw, err := os.ReadFile("item_map.json")
	if err != nil {
		t.Skipf("读不到 item_map.json，跳过：%v", err)
	}

	var byLang map[string]map[string]string
	if err := json.Unmarshal(raw, &byLang); err != nil {
		t.Fatalf("item_map.json 解析失败：%v", err)
	}
	if len(byLang) == 0 {
		t.Fatal("item_map.json 是空的")
	}

	for lang, mapping := range byLang {
		if len(mapping) == 0 {
			t.Fatalf("%s 分组没有条目", lang)
		}

		seenIDs := map[string]string{}
		for name, id := range mapping {
			if strings.TrimSpace(name) == "" {
				t.Errorf("%s 里有空名称", lang)
			}
			if strings.TrimSpace(id) == "" {
				t.Errorf("%s 里 %q 的 ID 是空的", lang, name)
			}
			if prev, dup := seenIDs[id]; dup {
				t.Errorf("%s 里 ID %q 被 %q 和 %q 重复引用", lang, id, prev, name)
			}
			seenIDs[id] = name

			if tier := parseTierFromID(id); tier == "" {
				t.Errorf("%s 里 %q 的 ID %q 解析不出档位", lang, name, id)
			}
			path := buildTemplatePath(id)
			if !strings.HasPrefix(path, "AutoStockpile/Goods/") || !strings.HasSuffix(path, ".png") {
				t.Errorf("%s 里 %q 拼出的路径不合法：%q", lang, name, path)
			}
			if strings.Contains(path, "//") {
				t.Errorf("%s 里 %q 拼出的路径有双斜杠：%q", lang, name, path)
			}
		}
	}
}
