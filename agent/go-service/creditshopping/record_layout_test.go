package creditshopping

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func box(x, y int) maa.Rect {
	return maa.Rect{x, y, 10, 10}
}

func TestRecordOrderHitsByPosition(t *testing.T) {
	t.Run("5+5", func(t *testing.T) {
		var hits []itemPositionHit
		for i := 0; i < 5; i++ {
			hits = append(hits, itemPositionHit{Box: box(100+i*80, 200), Name: "t"})
		}
		for i := 0; i < 5; i++ {
			hits = append(hits, itemPositionHit{Box: box(100+i*80, 400), Name: "b"})
		}
		got := recordOrderHitsByPosition(hits)
		if len(got) != 10 {
			t.Fatalf("len = %d, want 10", len(got))
		}
		if recordRectCenterY(got[4].Box) >= recordRectCenterY(got[5].Box) {
			t.Fatal("expected top row before bottom row")
		}
	})
}

func TestRecordMatchItemsToSlots(t *testing.T) {
	t.Run("aligned pair", func(t *testing.T) {
		slots := []maa.Rect{
			box(100, 200),
			box(200, 200),
		}
		items := []itemPositionHit{
			{Box: box(102, 205), Name: "A", ID: "A"},
			{Box: box(198, 198), Name: "B", ID: "B"},
		}
		got := recordMatchItemsToSlots(slots, items)
		if got[0].ID != "A" || got[1].ID != "B" {
			t.Fatalf("match = %+v", got)
		}
	})

	t.Run("sparse single middle hit", func(t *testing.T) {
		var slots []maa.Rect
		for i := 0; i < 5; i++ {
			slots = append(slots, box(100+i*80, 200))
		}
		items := []itemPositionHit{
			{Box: box(100+2*80+2, 205), Name: "M", ID: "M"},
		}
		got := recordMatchItemsToSlots(slots, items)
		for i, want := range []string{"", "", "M", "", ""} {
			if got[i].ID != want {
				t.Fatalf("slot %d ID = %q, want %q; full=%+v", i, got[i].ID, want, got)
			}
		}
	})

	t.Run("sparse trailing hits only", func(t *testing.T) {
		var slots []maa.Rect
		for i := 0; i < 5; i++ {
			slots = append(slots, box(100+i*80, 200))
		}
		items := []itemPositionHit{
			{Box: box(100+3*80+1, 202), Name: "D", ID: "D"},
			{Box: box(100+4*80+3, 198), Name: "E", ID: "E"},
		}
		got := recordMatchItemsToSlots(slots, items)
		for i, want := range []string{"", "", "", "D", "E"} {
			if got[i].ID != want {
				t.Fatalf("slot %d ID = %q, want %q", i, got[i].ID, want)
			}
		}
	})

	t.Run("reject far orphan hit", func(t *testing.T) {
		slots := []maa.Rect{box(100, 200), box(200, 200)}
		items := []itemPositionHit{
			{Box: box(900, 500), Name: "X", ID: "X"},
		}
		got := recordMatchItemsToSlots(slots, items)
		if got[0].ID != "" || got[1].ID != "" {
			t.Fatalf("expected no assignment, got %+v", got)
		}
	})
}
