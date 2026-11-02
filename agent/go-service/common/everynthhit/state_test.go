package everynthhit

import "testing"

func TestAdvanceSequence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		every int
		want  []bool
	}{
		{
			name:  "every 3 hits on the third evaluation",
			every: 3,
			want:  []bool{false, false, true, false, false, true},
		},
		{
			name:  "every 1 hits on every evaluation",
			every: 1,
			want:  []bool{true, true, true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(ResetAll)
			for i, hit := range tc.want {
				if got := Advance("route", tc.every); got != hit {
					t.Fatalf("evaluation %d = %v; want %v", i+1, got, hit)
				}
			}
		})
	}
}

func TestAdvanceNonPositiveNeverCounts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		every int
	}{
		{name: "zero", every: 0},
		{name: "negative", every: -3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(ResetAll)
			for i := 0; i < 5; i++ {
				if Advance("route", tc.every) {
					t.Fatalf("call %d hit with every=%d; want never", i+1, tc.every)
				}
			}
			// 非正数 every 不计数，也不能污染同一 key 之后的正常计数。
			for i, hit := range []bool{false, false, true} {
				if got := Advance("route", 3); got != hit {
					t.Fatalf("after every=%d, call %d = %v; want %v", tc.every, i+1, got, hit)
				}
			}
		})
	}
}

func TestAdvanceKeysAreIsolated(t *testing.T) {
	t.Cleanup(ResetAll)
	// 两个 key 交替推进，各自的计数互不影响。
	for i := 0; i < 2; i++ {
		if Advance("a", 3) {
			t.Fatalf("a call %d hit early", i+1)
		}
		if Advance("b", 3) {
			t.Fatalf("b call %d hit early", i+1)
		}
	}
	if !Advance("a", 3) {
		t.Fatal("a third call did not hit")
	}
	// a 命中后清零重新从 1 计，此时 b 仍停在 2。
	if Advance("a", 3) {
		t.Fatal("a restarted from zero")
	}
	if !Advance("b", 3) {
		t.Fatal("b third call did not hit")
	}
}

func TestResetStartsOver(t *testing.T) {
	t.Cleanup(ResetAll)
	for i := 0; i < 2; i++ {
		if Advance("route", 3) {
			t.Fatalf("call %d hit early", i+1)
		}
	}
	Reset("route")
	// 复位后重新从 1 计：第 2 次仍不命中，第 3 次才命中。
	if Advance("route", 3) {
		t.Fatal("hit right after Reset")
	}
	if Advance("route", 3) {
		t.Fatal("hit on the second call after Reset")
	}
	if !Advance("route", 3) {
		t.Fatal("did not hit on the third call after Reset")
	}
}

func TestResetAllClearsEveryKey(t *testing.T) {
	t.Cleanup(ResetAll)
	// 先把两个 key 都推进到 2 次。
	for i := 0; i < 2; i++ {
		Advance("a", 3)
		Advance("b", 3)
	}
	ResetAll()
	for _, key := range []string{"a", "b"} {
		for i, hit := range []bool{false, false, true} {
			if got := Advance(key, 3); got != hit {
				t.Fatalf("%s call %d after ResetAll = %v; want %v", key, i+1, got, hit)
			}
		}
	}
}
