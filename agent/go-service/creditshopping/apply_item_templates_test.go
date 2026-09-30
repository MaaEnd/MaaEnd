package creditshopping

import "testing"

func TestItemTemplatesFromAttach(t *testing.T) {
	raw := map[string]any{
		"ready":        true,
		"visited":      true,
		"item_diamond": true,
		"alias":        "CreditShopping/Item/item_diamond.png",
		"item_gold":    "CreditShopping/Item/item_gold.png",
		"names": []any{
			"武库配额",
			"CreditShopping/Item/item_gachabyproducts_weapongold.png",
		},
	}
	got := itemTemplatesFromAttach(raw)
	want := []string{
		"CreditShopping/Item/item_diamond.png",
		"CreditShopping/Item/item_gold.png",
		"CreditShopping/Item/item_gachabyproducts_weapongold.png",
	}
	if len(got) != len(want) {
		t.Fatalf("templates = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("templates = %#v, want %#v", got, want)
		}
	}
}

func TestItemTemplatesFromAttachEmpty(t *testing.T) {
	if got := itemTemplatesFromAttach(map[string]any{"ready": true}); len(got) != 0 {
		t.Fatalf("templates = %#v", got)
	}
}

func TestItemTemplatesFromNode(t *testing.T) {
	got, err := itemTemplatesFromNode(`{"attach":{"item_gold":true}}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "CreditShopping/Item/item_gold.png" {
		t.Fatalf("templates = %#v", got)
	}
}
