package arenaofgames

import (
	"encoding/json"
	"strconv"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestDecideNextRoutesStatus(t *testing.T) {
	for _, tc := range []struct {
		status string
		count  int
		want   string
	}{
		{"complete", 3, nodeStop},
		{"rejected", 5, nodeAbandon},
		{"continuing", 0, nodeDrawCardPrefix + "1"},
		{"continuing", 2, nodeDrawCardPrefix + "3"},
		{"continuing", 4, nodeDrawCardPrefix + "5"},
	} {
		got, err := decideNext(tc.status, tc.count)
		if err != nil || got != tc.want {
			t.Fatalf("decideNext(%q, %d) = %q, %v; want %q", tc.status, tc.count, got, err, tc.want)
		}
	}
	// continuing 只会在 0-4 张时出现（第 5 张后可达牌型为空），越界与未知状态都报错。
	for _, tc := range []struct {
		status string
		count  int
	}{
		{"continuing", 5},
		{"continuing", -1},
		{"", 0},
		{"draw", 1},
	} {
		if got, err := decideNext(tc.status, tc.count); err == nil {
			t.Fatalf("decideNext(%q, %d) accepted: %q", tc.status, tc.count, got)
		}
	}
}

func TestUnwrapHandDetailAcceptsRawWrappedAndStringForms(t *testing.T) {
	detail := handDetail{Count: 3, Category: "pair"}
	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]string{
		"raw":         string(raw),
		"wrapped":     `{"best":{"detail":` + string(raw) + `}}`,
		"stringified": jsonQuote(string(raw)),
	} {
		got, ok := unwrapHandDetail(&maa.RecognitionDetail{DetailJson: payload})
		if !ok || got.Count != detail.Count || got.Category != detail.Category {
			t.Fatalf("%s: unwrapHandDetail = %+v, %v", name, got, ok)
		}
	}
	// 部分核心版本把 wrapped detail 再序列化成 JSON 字符串。
	wrappedString := `{"best":{"detail":` + jsonQuote(string(raw)) + `}}`
	got, ok := unwrapHandDetail(&maa.RecognitionDetail{DetailJson: wrappedString})
	if !ok || got.Count != detail.Count {
		t.Fatalf("wrapped stringified: unwrapHandDetail = %+v, %v", got, ok)
	}
	for name, payload := range map[string]string{
		"empty":   "",
		"garbage": "{",
		"null":    "null",
		"scalar":  `"plain string"`,
	} {
		if got, ok := unwrapHandDetail(&maa.RecognitionDetail{DetailJson: payload}); ok {
			t.Fatalf("%s: unwrapHandDetail accepted %q: %+v", name, payload, got)
		}
	}
	if got, ok := unwrapHandDetail(nil); ok {
		t.Fatalf("unwrapHandDetail accepted nil detail: %+v", got)
	}
}

// jsonQuote 把文本序列化成 JSON 字符串字面量（含两侧引号）。
func jsonQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestDrawCardNodesCoverEverySlot(t *testing.T) {
	// 抽牌节点按槽位 1-5 预定义，Decide 只做路由；名字连续性由测试钉住，
	// 与 ArenaOfGames.json 的静态节点定义同步维护。
	for count := 0; count <= 4; count++ {
		node, err := decideNext("continuing", count)
		if err != nil {
			t.Fatal(err)
		}
		if want := nodeDrawCardPrefix + strconv.Itoa(count+1); node != want {
			t.Fatalf("count=%d: routed to %q, want %q", count, node, want)
		}
	}
}
