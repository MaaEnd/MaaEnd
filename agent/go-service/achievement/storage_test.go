package achievement

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func init() {
	i18n.Init()
	rulesMu.Lock()
	defer rulesMu.Unlock()
	rules = []rule{
		{
			ID:     firstOpenMXUAchievementID,
			Title:  "小试牛刀",
			Event:  eventOpenMXU,
			Target: 1,
		},
	}
}

func setRulesForTest(t *testing.T, testRules []rule) {
	t.Helper()

	rulesMu.Lock()
	oldRules := rules
	rules = append([]rule(nil), testRules...)
	rulesMu.Unlock()

	t.Cleanup(func() {
		rulesMu.Lock()
		defer rulesMu.Unlock()
		rules = oldRules
	})
}

func TestRecordStartupAchievementUnlocksOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), achievementStorageFileName)
	now := time.Date(2026, 5, 31, 1, 2, 3, 0, time.UTC)

	unlocks, err := recordEvent(path, eventOpenMXU, 1, "", now)
	if err != nil {
		t.Fatalf("record first startup: %v", err)
	}
	if len(unlocks) != 1 {
		t.Fatalf("first startup unlock count = %d, want 1", len(unlocks))
	}
	if unlocks[0].ID != firstOpenMXUAchievementID {
		t.Fatalf("unlock id = %q, want %q", unlocks[0].ID, firstOpenMXUAchievementID)
	}

	unlocks, err = recordEvent(path, eventOpenMXU, 1, "", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("record duplicate startup: %v", err)
	}
	if len(unlocks) != 0 {
		t.Fatalf("duplicate startup unlock count = %d, want 0", len(unlocks))
	}

	storage, err := readStorageFile(path)
	if err != nil {
		t.Fatalf("read storage: %v", err)
	}
	achievement := storage.Achievements[firstOpenMXUAchievementID]
	if achievement.Progress != 2 {
		t.Fatalf("progress = %d, want 2", achievement.Progress)
	}
	if len(storage.PendingNotifications) != 1 {
		t.Fatalf("pending notifications = %d, want 1", len(storage.PendingNotifications))
	}
}

func TestConsumePendingNotificationsClearsPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), achievementStorageFileName)
	now := time.Date(2026, 5, 31, 1, 2, 3, 0, time.UTC)
	if _, err := recordEvent(path, eventOpenMXU, 1, "", now); err != nil {
		t.Fatalf("record startup: %v", err)
	}

	pending, err := consumePendingNotifications(path, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("consume pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending count = %d, want 1", len(pending))
	}

	storage, err := readStorageFile(path)
	if err != nil {
		t.Fatalf("read storage: %v", err)
	}
	if len(storage.PendingNotifications) != 0 {
		t.Fatalf("pending after consume = %d, want 0", len(storage.PendingNotifications))
	}
}

func TestWriteFileAtomicReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), achievementStorageFileName)
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}
	if err := writeFileAtomic(path, []byte("new"), 0644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replaced file: %v", err)
	}
	if string(content) != "new" {
		t.Fatalf("content = %q, want %q", content, "new")
	}
}

func TestTrackerActionKeepsPendingNotifications(t *testing.T) {
	path := filepath.Join(t.TempDir(), achievementStorageFileName)
	oldResolve := resolveStoragePathFunc
	resolveStoragePathFunc = func() string {
		return path
	}
	defer func() {
		resolveStoragePathFunc = oldResolve
	}()

	action := &TrackerAction{}
	if !action.Run(nil, &maa.CustomActionArg{
		CustomActionParam: `{"event":"mxu_open"}`,
	}) {
		t.Fatal("tracker action returned false")
	}

	storage, err := readStorageFile(path)
	if err != nil {
		t.Fatalf("read storage: %v", err)
	}
	if len(storage.PendingNotifications) != 1 {
		t.Fatalf("pending notifications = %d, want 1", len(storage.PendingNotifications))
	}
}

func TestPendingRecognitionOnlyHitsWithPendingNotifications(t *testing.T) {
	path := filepath.Join(t.TempDir(), achievementStorageFileName)
	oldResolve := resolveStoragePathFunc
	resolveStoragePathFunc = func() string {
		return path
	}
	defer func() {
		resolveStoragePathFunc = oldResolve
	}()

	recognition := &PendingRecognition{}
	if _, hit := recognition.Run(nil, &maa.CustomRecognitionArg{}); hit {
		t.Fatal("pending recognition hit with empty storage")
	}

	now := time.Date(2026, 5, 31, 1, 2, 3, 0, time.UTC)
	if _, err := recordEvent(path, eventOpenMXU, 1, "", now); err != nil {
		t.Fatalf("record startup: %v", err)
	}

	result, hit := recognition.Run(nil, &maa.CustomRecognitionArg{})
	if !hit {
		t.Fatal("pending recognition missed with pending notification")
	}
	if result == nil || result.Detail != `{"pending_count":1}` {
		t.Fatalf("recognition result = %#v, want pending_count detail", result)
	}
}

func TestConsumePendingActionClearsPendingNotifications(t *testing.T) {
	path := filepath.Join(t.TempDir(), achievementStorageFileName)
	oldResolve := resolveStoragePathFunc
	resolveStoragePathFunc = func() string {
		return path
	}
	defer func() {
		resolveStoragePathFunc = oldResolve
	}()

	now := time.Date(2026, 5, 31, 1, 2, 3, 0, time.UTC)
	if _, err := recordEvent(path, eventOpenMXU, 1, "", now); err != nil {
		t.Fatalf("record startup: %v", err)
	}

	action := &ConsumePendingAction{}
	if !action.Run(nil, nil) {
		t.Fatal("consume pending action returned false")
	}

	storage, err := readStorageFile(path)
	if err != nil {
		t.Fatalf("read storage: %v", err)
	}
	if len(storage.PendingNotifications) != 0 {
		t.Fatalf("pending notifications = %d, want 0", len(storage.PendingNotifications))
	}
}

func TestDedupeKeySkipsDuplicateEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), achievementStorageFileName)
	now := time.Date(2026, 5, 31, 1, 2, 3, 0, time.UTC)
	if _, err := recordEvent(path, "unknown_event", 2, "same-key", now); err != nil {
		t.Fatalf("record first event: %v", err)
	}
	if _, err := recordEvent(path, "unknown_event", 2, "same-key", now.Add(time.Minute)); err != nil {
		t.Fatalf("record duplicate event: %v", err)
	}

	storage, err := readStorageFile(path)
	if err != nil {
		t.Fatalf("read storage: %v", err)
	}
	if len(storage.Events) != 1 {
		t.Fatalf("event count = %d, want 1", len(storage.Events))
	}
}

func TestPendingNotificationsAreCapped(t *testing.T) {
	setRulesForTest(t, []rule{
		{
			ID:     "repeat-achievement",
			Title:  "Repeat Achievement",
			Event:  eventOpenMXU,
			Target: 1,
		},
	})

	path := filepath.Join(t.TempDir(), achievementStorageFileName)
	base := time.Date(2026, 5, 31, 1, 2, 3, 0, time.UTC)
	for i := 0; i < maxPendingNotifications+5; i++ {
		unlocks, err := recordEvent(path, eventOpenMXU, 1, "", base.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("record event %d: %v", i, err)
		}
		if len(unlocks) != 1 {
			t.Fatalf("record event %d unlock count = %d, want 1", i, len(unlocks))
		}
		storage, err := readStorageFile(path)
		if err != nil {
			t.Fatalf("read storage %d: %v", i, err)
		}
		delete(storage.Achievements, "repeat-achievement")
		if err := writeStorageFile(path, storage); err != nil {
			t.Fatalf("reset achievement %d: %v", i, err)
		}
	}

	storage, err := readStorageFile(path)
	if err != nil {
		t.Fatalf("read storage: %v", err)
	}
	if len(storage.PendingNotifications) != maxPendingNotifications {
		t.Fatalf("pending notifications = %d, want %d", len(storage.PendingNotifications), maxPendingNotifications)
	}
}

func TestBuildReportIncludesRecentUnlock(t *testing.T) {
	setRulesForTest(t, []rule{
		{
			ID:     firstOpenMXUAchievementID,
			Title:  "小试牛刀",
			Event:  eventOpenMXU,
			Target: 1,
		},
	})

	storage := storageFile{
		Achievements: map[string]achievementLog{
			firstOpenMXUAchievementID: {
				ID:         firstOpenMXUAchievementID,
				Title:      "小试牛刀",
				Event:      eventOpenMXU,
				Progress:   1,
				Target:     1,
				UnlockedAt: "2026-05-31T01:02:03Z",
			},
		},
		RecentUnlocks: []unlockLog{
			{
				ID:         firstOpenMXUAchievementID,
				Title:      "小试牛刀",
				UnlockedAt: "2026-05-31T01:02:03Z",
			},
		},
	}

	got := buildReport(storage, 5)
	if got == "" {
		t.Fatal("buildReport returned empty string")
	}
	if !strings.Contains(got, "成就进度：1/1") {
		t.Fatalf("report missing summary count 1/1: %q", got)
	}
	if !strings.Contains(got, "最近解锁：") {
		t.Fatalf("report missing recent unlock header: %q", got)
	}
	if !strings.Contains(got, "- 小试牛刀") {
		t.Fatalf("report missing recent unlock title: %q", got)
	}
}

func TestBuildReportUsesDefaultLimitForNonPositiveLimit(t *testing.T) {
	setRulesForTest(t, []rule{
		{
			ID:     firstOpenMXUAchievementID,
			Title:  "小试牛刀",
			Event:  eventOpenMXU,
			Target: 1,
		},
	})

	storage := storageFile{
		RecentUnlocks: []unlockLog{
			{ID: "1", Title: "One", UnlockedAt: "2026-05-31T01:02:03Z"},
			{ID: "2", Title: "Two", UnlockedAt: "2026-05-31T01:02:04Z"},
		},
	}

	got := buildReport(storage, 0)
	if !strings.Contains(got, "最近解锁：") {
		t.Fatalf("report missing recent unlock header: %q", got)
	}
	if !strings.Contains(got, "- Two") || !strings.Contains(got, "- One") {
		t.Fatalf("report missing recent unlocks with default limit: %q", got)
	}
}

func TestBuildReportIgnoresStoredAchievementsOutsideCurrentRules(t *testing.T) {
	setRulesForTest(t, []rule{
		{
			ID:     firstOpenMXUAchievementID,
			Title:  "小试牛刀",
			Event:  eventOpenMXU,
			Target: 1,
		},
	})

	storage := storageFile{
		Achievements: map[string]achievementLog{
			firstOpenMXUAchievementID: {
				ID:         firstOpenMXUAchievementID,
				Title:      "小试牛刀",
				Event:      eventOpenMXU,
				Progress:   1,
				Target:     1,
				UnlockedAt: "2026-05-31T01:02:03Z",
			},
			"removed-achievement": {
				ID:         "removed-achievement",
				Title:      "Removed Achievement",
				Event:      eventOpenMXU,
				Progress:   1,
				Target:     1,
				UnlockedAt: "2026-05-30T01:02:03Z",
			},
		},
	}

	got := buildReport(storage, 5)
	if !strings.Contains(got, "成就进度：1/1") {
		t.Fatalf("report should count only current rules: %q", got)
	}
}

func TestBuildReportRecentUnlocksOrderingAndLimit(t *testing.T) {
	setRulesForTest(t, []rule{
		{
			ID:     "old-achievement",
			Title:  "Old Achievement",
			Event:  eventOpenMXU,
			Target: 1,
		},
		{
			ID:     "middle-achievement",
			Title:  "Middle Achievement",
			Event:  eventOpenMXU,
			Target: 1,
		},
		{
			ID:     "new-achievement",
			Title:  "New Achievement",
			Event:  eventOpenMXU,
			Target: 1,
		},
	})

	storage := storageFile{
		Achievements: map[string]achievementLog{
			"old-achievement": {
				ID:         "old-achievement",
				Title:      "Old Achievement",
				Event:      eventOpenMXU,
				Progress:   1,
				Target:     1,
				UnlockedAt: "2026-05-30T01:02:03Z",
			},
			"middle-achievement": {
				ID:         "middle-achievement",
				Title:      "Middle Achievement",
				Event:      eventOpenMXU,
				Progress:   1,
				Target:     1,
				UnlockedAt: "2026-05-31T01:02:03Z",
			},
			"new-achievement": {
				ID:         "new-achievement",
				Title:      "New Achievement",
				Event:      eventOpenMXU,
				Progress:   1,
				Target:     1,
				UnlockedAt: "2026-06-01T01:02:03Z",
			},
		},
		RecentUnlocks: []unlockLog{
			{
				ID:         "old-achievement",
				Title:      "Old Achievement",
				UnlockedAt: "2026-05-30T01:02:03Z",
			},
			{
				ID:         "middle-achievement",
				Title:      "Middle Achievement",
				UnlockedAt: "2026-05-31T01:02:03Z",
			},
			{
				ID:         "new-achievement",
				Title:      "New Achievement",
				UnlockedAt: "2026-06-01T01:02:03Z",
			},
		},
	}

	got := buildReport(storage, 2)
	if !strings.Contains(got, "成就进度：3/3") {
		t.Fatalf("report missing summary count 3/3: %q", got)
	}

	newIndex := strings.Index(got, "New Achievement")
	middleIndex := strings.Index(got, "Middle Achievement")
	oldIndex := strings.Index(got, "Old Achievement")
	if newIndex == -1 || middleIndex == -1 {
		t.Fatalf("report missing expected recent unlocks (new=%d, middle=%d): %q", newIndex, middleIndex, got)
	}
	if newIndex > middleIndex {
		t.Fatalf("newest unlock should be listed first (new=%d, middle=%d): %q", newIndex, middleIndex, got)
	}
	if oldIndex != -1 {
		t.Fatalf("oldest unlock should be truncated by limit, found at %d: %q", oldIndex, got)
	}
}
