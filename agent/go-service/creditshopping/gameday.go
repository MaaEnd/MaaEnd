package creditshopping

import "time"

const gameDayBoundaryHour = 4

// gameDateLocal 返回本地「游戏日」日期（当日 04:00 至次日 04:00 为同一天）。
func gameDateLocal(now time.Time) string {
	t := now.Local()
	if t.Hour() < gameDayBoundaryHour {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format(time.DateOnly)
}
