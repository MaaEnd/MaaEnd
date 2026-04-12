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
	if loc == nil {
		loc = defaultServerLocation
	}

	serverTime := now.In(loc)
	if serverTime.Hour() < serverDayBoundaryHour {
		serverTime = serverTime.AddDate(0, 0, -1)
	}

	return serverTime.Weekday()
}
