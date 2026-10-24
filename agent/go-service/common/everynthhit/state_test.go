package everynthhit

import "testing"

func TestAdvance(t *testing.T) {
	tests := []struct {
		name  string
		every int
		want  []bool
	}{
		{name: "hits every other call when every is one", every: 1, want: []bool{true, false, true, false}},
		{name: "hits every third counted call", every: 3, want: []bool{false, false, true, false, false, false, true}},
		{name: "never hits when every is zero", every: 0, want: []bool{false, false, false}},
		{name: "never hits when every is negative", every: -2, want: []bool{false, false, false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := tt.name
			t.Cleanup(func() { ResetAll() })

			for i, want := range tt.want {
				if got := Advance(key, tt.every); got != want {
					t.Fatalf("call %d: Advance() = %v, want %v", i+1, got, want)
				}
			}
		})
	}
}

func TestAdvanceSkipsCallAfterHit(t *testing.T) {
	const key = "after-hit"
	t.Cleanup(func() { ResetAll() })

	for i, want := range []bool{false, false, true, false} {
		if got := Advance(key, 3); got != want {
			t.Fatalf("call %d: Advance() = %v, want %v", i+1, got, want)
		}
	}
}

func TestStartRunResetsCountAndSkipsFirstCall(t *testing.T) {
	const key = "restart"
	t.Cleanup(func() { ResetAll() })

	if Advance(key, 3) {
		t.Fatal("first call should miss")
	}

	startRun()

	for i, want := range []bool{false, false, false, true} {
		if got := Advance(key, 3); got != want {
			t.Fatalf("call %d after startRun: Advance() = %v, want %v", i+1, got, want)
		}
	}
}

func TestAdvanceLoopRhythmSkipsFirstAndJumpBackScan(t *testing.T) {
	const key = "loop"
	t.Cleanup(func() { ResetAll() })

	startRun()

	steps := []struct {
		stage string
		want  bool
	}{
		{stage: "task start scan", want: false},
		{stage: "route 1 scan", want: false},
		{stage: "route 2 scan", want: false},
		{stage: "route 3 scan", want: false},
		{stage: "route 4 scan", want: false},
		{stage: "route 5 scan", want: false},
		{stage: "route 6 scan", want: true},
		{stage: "stash jump back scan", want: false},
		{stage: "route 7 scan", want: false},
		{stage: "route 8 scan", want: false},
		{stage: "route 9 scan", want: false},
		{stage: "route 10 scan", want: false},
		{stage: "route 11 scan", want: false},
		{stage: "route 12 scan", want: true},
		{stage: "stash jump back scan", want: false},
	}

	for i, step := range steps {
		if got := Advance(key, 6); got != step.want {
			t.Fatalf("step %d (%s): Advance() = %v, want %v", i+1, step.stage, got, step.want)
		}
	}
}

func TestAdvanceKeysAreIndependent(t *testing.T) {
	t.Cleanup(func() { ResetAll() })

	calls := []struct {
		key  string
		want bool
	}{
		{key: "first", want: false},
		{key: "second", want: false},
		{key: "first", want: true},
		{key: "first", want: false},
		{key: "second", want: true},
		{key: "second", want: false},
	}

	for i, call := range calls {
		if got := Advance(call.key, 2); got != call.want {
			t.Fatalf("call %d with key %q: Advance() = %v, want %v", i+1, call.key, got, call.want)
		}
	}
}

func TestResetAllClearsCountsAndSkips(t *testing.T) {
	const key = "reset-all"
	t.Cleanup(func() { ResetAll() })

	if Advance(key, 2) {
		t.Fatal("first call should miss")
	}
	if !Advance(key, 2) {
		t.Fatal("second call should hit")
	}

	ResetAll()

	if got := Advance(key, 2); got {
		t.Fatal("call after ResetAll should miss because the counter restarts")
	}
	if !Advance(key, 2) {
		t.Fatal("second call after ResetAll should hit")
	}

	ResetAll()

	if got := Advance(key, 1); !got {
		t.Fatal("call after ResetAll should hit because the skip marker is cleared")
	}
}

func TestAdvanceDisabledEveryDoesNotAccumulate(t *testing.T) {
	const key = "disabled"
	t.Cleanup(func() { ResetAll() })

	for i := 0; i < 5; i++ {
		if Advance(key, 0) {
			t.Fatalf("call %d: Advance(0) should never hit", i+1)
		}
	}

	if got := Advance(key, 2); got {
		t.Fatal("calls with every <= 0 should not be counted")
	}
	if !Advance(key, 2) {
		t.Fatal("second counted call should hit")
	}
}

func TestParseEvery(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		want   int
		wantOK bool
	}{
		{name: "json number", raw: "3", want: 3, wantOK: true},
		{name: "integral float", raw: "3.0", want: 3, wantOK: true},
		{name: "numeric string", raw: `"3"`, want: 3, wantOK: true},
		{name: "numeric string with spaces", raw: `" 3 "`, want: 3, wantOK: true},
		{name: "fractional number", raw: "3.5", want: 0, wantOK: false},
		{name: "non numeric string", raw: `"three"`, want: 0, wantOK: false},
		{name: "empty string", raw: `""`, want: 0, wantOK: false},
		{name: "boolean", raw: "true", want: 0, wantOK: false},
		{name: "empty raw", raw: "", want: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseEvery([]byte(tt.raw))
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("parseEvery(%s) = (%d, %v), want (%d, %v)", tt.raw, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
