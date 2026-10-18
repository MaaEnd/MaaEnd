package everynthhit

import "testing"

func TestAdvance(t *testing.T) {
	tests := []struct {
		name  string
		every int
		want  []bool
	}{
		{name: "hits every call when every is one", every: 1, want: []bool{true, true, true}},
		{name: "hits every third call", every: 3, want: []bool{false, false, true, false, false, true}},
		{name: "never hits when every is zero", every: 0, want: []bool{false, false, false}},
		{name: "never hits when every is negative", every: -2, want: []bool{false, false, false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := tt.name
			t.Cleanup(func() { Reset(key) })

			for i, want := range tt.want {
				if got := Advance(key, tt.every); got != want {
					t.Fatalf("call %d: Advance() = %v, want %v", i+1, got, want)
				}
			}
		})
	}
}

func TestAdvanceResetRestartsCycle(t *testing.T) {
	const key = "restart"
	t.Cleanup(func() { ResetAll() })

	if Advance(key, 2) {
		t.Fatal("first call should miss")
	}

	Reset(key)

	for i, want := range []bool{false, true} {
		if got := Advance(key, 2); got != want {
			t.Fatalf("call %d after reset: Advance() = %v, want %v", i+1, got, want)
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
		{key: "second", want: true},
	}

	for i, call := range calls {
		if got := Advance(call.key, 2); got != call.want {
			t.Fatalf("call %d with key %q: Advance() = %v, want %v", i+1, call.key, got, call.want)
		}
	}
}

func TestResetAllClearsEveryKey(t *testing.T) {
	t.Cleanup(func() { ResetAll() })

	if Advance("restart", 2) {
		t.Fatal("first call should miss")
	}
	if Advance("other", 3) {
		t.Fatal("first call should miss")
	}

	ResetAll()

	for _, key := range []string{"restart", "other"} {
		if got := Advance(key, 2); got {
			t.Fatalf("key %q should restart counting after ResetAll", key)
		}
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
