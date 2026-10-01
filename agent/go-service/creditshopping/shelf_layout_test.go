package creditshopping

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func box(x, y int) maa.Rect {
	return maa.Rect{x, y, 10, 10}
}

func TestOrderHitsByShelfPosition(t *testing.T) {
	t.Run("5+5", func(t *testing.T) {
		var hits []ocrNameHit
		for i := 0; i < 5; i++ {
			hits = append(hits, ocrNameHit{Box: box(100+i*80, 200), Text: "t"})
		}
		for i := 0; i < 5; i++ {
			hits = append(hits, ocrNameHit{Box: box(100+i*80, 400), Text: "b"})
		}
		got := orderHitsByShelfPosition(hits)
		if len(got) != 10 {
			t.Fatalf("len = %d, want 10", len(got))
		}
		if rectCenterY(got[4].Box) >= rectCenterY(got[5].Box) {
			t.Fatal("expected top row before bottom row")
		}
	})

	t.Run("7+3", func(t *testing.T) {
		var hits []ocrNameHit
		for i := 0; i < 7; i++ {
			hits = append(hits, ocrNameHit{Box: box(50+i*60, 220), Text: "t"})
		}
		for i := 0; i < 3; i++ {
			hits = append(hits, ocrNameHit{Box: box(200+i*120, 450), Text: "b"})
		}
		got := orderHitsByShelfPosition(hits)
		if len(got) != 10 {
			t.Fatalf("len = %d, want 10", len(got))
		}
	})

	t.Run("9+1", func(t *testing.T) {
		var hits []ocrNameHit
		for i := 0; i < 9; i++ {
			hits = append(hits, ocrNameHit{Box: box(30+i*50, 200), Text: "t"})
		}
		hits = append(hits, ocrNameHit{Box: box(400, 420), Text: "solo"})
		got := orderHitsByShelfPosition(hits)
		if len(got) != 10 {
			t.Fatalf("len = %d, want 10", len(got))
		}
		if got[9].Text != "solo" {
			t.Fatalf("last slot = %q, want solo", got[9].Text)
		}
	})
}
