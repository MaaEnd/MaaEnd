package autostockpile

import (
	"fmt"
	"time"
)

const (
	serverDayBoundaryHour  = 4
	defaultServerUTCOffset = 8 * 60 * 60
)

var defaultServerLocation = time.FixedZone("UTC+8", defaultServerUTCOffset)

func locationFromUTCOffset(offset *int) *time.Location {
	if offset == nil {
		return defaultServerLocation
	}

	name := fmt.Sprintf("UTC%+d", *offset)
	return time.FixedZone(name, *offset*60*60)
}

func resolveServerWeekday(now time.Time, loc *time.Location) time.Weekday {
	return adjustedServerTime(now, loc).Weekday()
}

// adjustedServerTime 返回按服务器跨天边界（凌晨 4 点）调整后的服务器时间。
// loc 必须非 nil：唯一来源 locationFromUTCOffset 永不返回 nil。
func adjustedServerTime(now time.Time, loc *time.Location) time.Time {
	serverTime := now.In(loc)
	if serverTime.Hour() < serverDayBoundaryHour {
		serverTime = serverTime.AddDate(0, 0, -1)
	}

	return serverTime
}
