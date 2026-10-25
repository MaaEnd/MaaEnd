package cultivation

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func useTempCache(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "debug", "record", "CultivationRed.json")
	old := cachePathFunc
	cachePathFunc = func() string { return path }
	resultMu.Lock()
	current = taskResults{}
	resultMu.Unlock()
	t.Cleanup(func() { cachePathFunc = old })
	return path
}

func TestGameDayBoundary(t *testing.T) {
	for _, test := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 10, 25, 3, 59, 59, 0, time.Local), "2026-10-24"},
		{time.Date(2026, 10, 25, 4, 0, 0, 0, time.Local), "2026-10-25"},
		{time.Date(2026, 10, 25, 23, 59, 0, 0, time.Local), "2026-10-25"},
		{time.Date(2026, 11, 1, 0, 30, 0, 0, time.Local), "2026-10-31"},
		{time.Date(2027, 1, 1, 2, 0, 0, 0, time.Local), "2026-12-31"},
	} {
		if got := GameDay(test.at); got != test.want {
			t.Fatalf("%v: got %s, want %s", test.at, got, test.want)
		}
	}
}

func TestRecordWritesCacheAndCachedExpiresNextGameDay(t *testing.T) {
	useTempCache(t)
	now := time.Date(2026, 10, 25, 10, 0, 0, 0, time.Local)
	want := Red{Routes: []string{"PROTOSET", "T_CREDS"}, Unrecognized: 1, Day: GameDay(now)}
	Record(7, "Si", want)
	got, ok := Cached("Si", now)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("same-day cache: %+v %v", got, ok)
	}
	if _, ok := Cached("Si", time.Date(2026, 10, 26, 3, 0, 0, 0, time.Local)); !ok {
		t.Fatal("cache expired before the 04:00 boundary")
	}
	if _, ok := Cached("Si", time.Date(2026, 10, 26, 4, 0, 0, 0, time.Local)); ok {
		t.Fatal("cache survived into the next game day")
	}
	if _, ok := Cached("Other", now); ok {
		t.Fatal("unrecorded operator reported cached")
	}
}

func TestFailedReadingIsNotCached(t *testing.T) {
	path := useTempCache(t)
	now := time.Now()
	Record(1, "Si", Red{Failed: true, Day: GameDay(now)})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed reading wrote the cache file: %v", err)
	}
	if _, ok := Cached("Si", now); ok {
		t.Fatal("failed reading served from cache")
	}
	// A failed reading never replaces an earlier good one on disk.
	Record(1, "Si", Red{Routes: []string{"T_CREDS"}, Day: GameDay(now)})
	Record(2, "Si", Red{Failed: true, Day: GameDay(now)})
	if got, ok := Cached("Si", now); !ok || !reflect.DeepEqual(got.Routes, []string{"T_CREDS"}) {
		t.Fatalf("good cache lost: %+v %v", got, ok)
	}
	if !Checked(2)["Si"].Failed {
		t.Fatal("failed reading missing from this task's results")
	}
}

func TestCorruptCacheIsNoCacheAndIsReplaced(t *testing.T) {
	path := useTempCache(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, ok := Cached("Si", now); ok {
		t.Fatal("corrupt file reported a cache hit")
	}
	Record(1, "Si", Red{Day: GameDay(now)})
	if _, ok := Cached("Si", now); !ok {
		t.Fatal("cache not rewritten after corruption")
	}
}

func TestCachedDayMismatchInFile(t *testing.T) {
	path := useTempCache(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := `{"Si":{"routes":["T_CREDS"],"unrecognized":0,"failed":false,"day":"2000-01-01"}}`
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Cached("Si", time.Now()); ok {
		t.Fatal("stale day served")
	}
}

func TestCheckedIsPerTaskCopyAndKeepsLatestTaskOnly(t *testing.T) {
	useTempCache(t)
	now := time.Now()
	SetTarget(1, "Si")
	if name, ok := target(1); !ok || name != "Si" {
		t.Fatalf("target: %q %v", name, ok)
	}
	if _, ok := target(2); ok {
		t.Fatal("target leaked to another task")
	}
	Record(1, "Si", Red{Routes: []string{"T_CREDS"}, Day: GameDay(now)})
	got := Checked(1)
	got["Si"].Routes[0] = "mutated"
	delete(got, "Si")
	if again := Checked(1); len(again) != 1 || again["Si"].Routes[0] != "T_CREDS" {
		t.Fatalf("Checked returned shared state: %+v", again)
	}
	Record(2, "Ember", Red{Day: GameDay(now)})
	if len(Checked(1)) != 0 || len(Checked(2)) != 1 {
		t.Fatalf("older task retained: %v %v", Checked(1), Checked(2))
	}
	if _, ok := target(1); ok {
		t.Fatal("older task target retained")
	}
}
