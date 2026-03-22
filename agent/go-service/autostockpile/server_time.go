package autostockpile

import "time"

const autoStockpileDailyResetOffset = 4 * time.Hour

var defaultAutoStockpileServerLocation = time.FixedZone("GMT+8", 8*60*60)

func resolveAutoStockpileServerLocation(loc *time.Location) *time.Location {
	if loc != nil {
		return loc
	}
	return defaultAutoStockpileServerLocation
}

// 服务器日界线为 04:00，因此将时刻减去 4 小时后再取 Weekday，
// 即可把“周日 04:00 ~ 周一 03:59”映射为自然周日。
func isServerSundayAt(now time.Time, loc *time.Location) bool {
	serverNow := now.In(resolveAutoStockpileServerLocation(loc))
	return serverNow.Add(-autoStockpileDailyResetOffset).Weekday() == time.Sunday
}

func isServerSundayNow() bool {
	return isServerSundayAt(time.Now(), nil)
}
