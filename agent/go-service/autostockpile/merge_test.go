package autostockpile

import (
	"reflect"
	"testing"
)

func TestMergeGoodsByID(t *testing.T) {
	t.Parallel()

	page0 := []GoodsItem{
		{ID: "A", Name: "a", Price: 100},
		{ID: "B", Name: "b", Price: 200},
	}
	page1 := []GoodsItem{
		{ID: "B", Name: "b-dup", Price: 210},
		{ID: "Z", Name: "z", Price: 50},
	}

	goods, page1Only := mergeGoodsByID(page0, page1)

	wantGoods := []GoodsItem{
		{ID: "A", Name: "a", Price: 100},
		{ID: "B", Name: "b", Price: 200},
		{ID: "Z", Name: "z", Price: 50},
	}
	if !reflect.DeepEqual(goods, wantGoods) {
		t.Fatalf("goods = %#v, want %#v", goods, wantGoods)
	}
	wantPage1Only := []string{"Z"}
	if !reflect.DeepEqual(page1Only, wantPage1Only) {
		t.Fatalf("page1Only = %#v, want %#v", page1Only, wantPage1Only)
	}
}

func TestMergeGoodsByIDEmptyPages(t *testing.T) {
	t.Parallel()

	goods, page1Only := mergeGoodsByID(nil, nil)
	if len(goods) != 0 || len(page1Only) != 0 {
		t.Fatalf("expected empty merge, got goods=%#v page1Only=%#v", goods, page1Only)
	}

	page1 := []GoodsItem{{ID: "Z", Name: "z", Price: 1}}
	goods, page1Only = mergeGoodsByID(nil, page1)
	if len(goods) != 1 || goods[0].ID != "Z" {
		t.Fatalf("unexpected goods %#v", goods)
	}
	if !reflect.DeepEqual(page1Only, []string{"Z"}) {
		t.Fatalf("unexpected page1Only %#v", page1Only)
	}
}

func TestMergeGoodsByIDSkipsEmptyID(t *testing.T) {
	t.Parallel()

	page0 := []GoodsItem{{ID: "", Name: "bad"}, {ID: "A", Name: "a"}}
	page1 := []GoodsItem{{ID: "", Name: "bad2"}, {ID: "B", Name: "b"}}
	goods, page1Only := mergeGoodsByID(page0, page1)
	if len(goods) != 2 || goods[0].ID != "A" || goods[1].ID != "B" {
		t.Fatalf("unexpected goods %#v", goods)
	}
	if !reflect.DeepEqual(page1Only, []string{"B"}) {
		t.Fatalf("unexpected page1Only %#v", page1Only)
	}
}

func TestIsPage1OnlyID(t *testing.T) {
	t.Parallel()

	data := RecognitionData{Page1OnlyIDs: []string{"Z", "Y"}}
	if !isPage1OnlyID(data, "Z") {
		t.Fatal("expected Z to be page1-only")
	}
	if isPage1OnlyID(data, "A") {
		t.Fatal("expected A not to be page1-only")
	}
}
