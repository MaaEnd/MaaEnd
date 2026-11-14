package autostockpile

import (
	"fmt"
	"testing"
	"time"
)

func TestMaaWeekdayMapsSundayToSeven(t *testing.T) {
	cases := map[time.Weekday]int{
		time.Sunday:    7,
		time.Monday:    1,
		time.Tuesday:   2,
		time.Wednesday: 3,
		time.Thursday:  4,
		time.Friday:    5,
		time.Saturday:  6,
	}

	for weekday, want := range cases {
		if got := maaWeekday(weekday); got != want {
			t.Errorf("maaWeekday(%v) = %d，期望 %d", weekday, got, want)
		}
	}
}

func TestMaaWeekdayCoversWholeWeek(t *testing.T) {
	seen := map[int]bool{}
	for day := time.Sunday; day <= time.Saturday; day++ {
		value := maaWeekday(day)
		if value < 1 || value > 7 {
			t.Fatalf("%v 映射出 %d，超出 1 到 7", day, value)
		}
		if seen[value] {
			t.Fatalf("%v 映射到 %d，与前面的星期撞了", day, value)
		}
		seen[value] = true
	}
	if len(seen) != 7 {
		t.Fatalf("七天应对应七个不同值，实际 %d 个", len(seen))
	}
}

func TestRetainRecentDailyStorageDatesKeepsLatestDates(t *testing.T) {
	records := []dailyStorageRecord{
		{ServerDate: "2026-11-10", Region: "Wuling"},
		{ServerDate: "2026-11-12", Region: "Wuling"},
		{ServerDate: "2026-11-11", Region: "Wuling"},
		{ServerDate: "2026-11-12", Region: "ValleyIV"},
	}

	kept := retainRecentDailyStorageDates(records, 2)

	if len(kept) != 3 {
		t.Fatalf("保留两天应剩 3 条（11-11 一条、11-12 两条），实际 %d 条：%+v", len(kept), kept)
	}
	for _, record := range kept {
		if record.ServerDate == "2026-11-10" {
			t.Fatalf("最旧的一天应被裁掉，实际仍在：%+v", kept)
		}
	}
}

func TestRetainRecentDailyStorageDatesLeavesShortHistoryUntouched(t *testing.T) {
	records := []dailyStorageRecord{
		{ServerDate: "2026-11-11"},
		{ServerDate: "2026-11-12"},
	}

	if kept := retainRecentDailyStorageDates(records, 5); len(kept) != 2 {
		t.Fatalf("天数不足上限时不该裁剪，期望 2 条，实际 %d 条", len(kept))
	}
}

func TestRetainRecentDailyStorageDatesEmptyInput(t *testing.T) {
	if kept := retainRecentDailyStorageDates(nil, 3); kept != nil {
		t.Fatalf("空输入应返回 nil，实际 %+v", kept)
	}
	if kept := retainRecentDailyStorageDates([]dailyStorageRecord{}, 3); len(kept) != 0 {
		t.Fatalf("空切片应返回空，实际 %+v", kept)
	}
}

// TestRetainRecentDailyStorageBounds 随机造记录，保留后的条数和日期种类都不该超限。
func TestRetainRecentDailyStorageBounds(t *testing.T) {
	for _, maxDate := range []int{1, 2, 3, 7} {
		records := make([]dailyStorageRecord, 0, 40)
		for day := 1; day <= 20; day++ {
			for region := 0; region < 2; region++ {
				records = append(records, dailyStorageRecord{
					ServerDate: fmt.Sprintf("2026-11-%02d", day),
					Region:     fmt.Sprintf("R%d", region),
				})
			}
		}
		input := append([]dailyStorageRecord(nil), records...)

		kept := retainRecentDailyStorageDates(records, maxDate)

		if len(kept) > len(input) {
			t.Fatalf("保留 %d 天时结果变多了：%d > %d", maxDate, len(kept), len(input))
		}

		dates := map[string]struct{}{}
		for _, record := range kept {
			dates[record.ServerDate] = struct{}{}
		}
		if len(dates) > maxDate {
			t.Fatalf("保留 %d 天却留下了 %d 种日期", maxDate, len(dates))
		}

		// 保留的必须是最新的那几天。
		latest := fmt.Sprintf("2026-11-%02d", 20)
		if _, ok := dates[latest]; !ok {
			t.Fatalf("最新的一天 %s 被裁掉了，留下的是 %v", latest, dates)
		}
	}
}

// TestRetainRecentDailyStorageKeepsOrder 过滤不该打乱原有顺序。
func TestRetainRecentDailyStorageKeepsOrder(t *testing.T) {
	records := []dailyStorageRecord{
		{ServerDate: "2026-11-12", Region: "A"},
		{ServerDate: "2026-11-09", Region: "B"},
		{ServerDate: "2026-11-12", Region: "C"},
		{ServerDate: "2026-11-11", Region: "D"},
	}

	kept := retainRecentDailyStorageDates(records, 2)

	wantRegions := []string{"A", "C", "D"}
	if len(kept) != len(wantRegions) {
		t.Fatalf("期望 %d 条，实际 %d 条：%+v", len(wantRegions), len(kept), kept)
	}
	for i, want := range wantRegions {
		if kept[i].Region != want {
			t.Fatalf("第 %d 条应为 %s，实际 %s（原顺序被打乱）", i, want, kept[i].Region)
		}
	}
}

func TestCloneGoodsItemsCopiesInsteadOfSharing(t *testing.T) {
	original := []GoodsItem{{ID: "Wuling/QingboRafts.Tier2", Price: 2100}}

	cloned := cloneGoodsItems(original)
	if len(cloned) != 1 {
		t.Fatalf("克隆结果长度应为 1，实际 %d", len(cloned))
	}

	cloned[0].Price = 1
	if original[0].Price != 2100 {
		t.Fatalf("改副本不该影响原切片，原价已变成 %d", original[0].Price)
	}

	if cloneGoodsItems(nil) != nil {
		t.Fatal("空输入应返回 nil")
	}
	if got := cloneGoodsItems([]GoodsItem{}); got != nil {
		t.Fatalf("空切片应返回 nil，实际 %+v", got)
	}
}
