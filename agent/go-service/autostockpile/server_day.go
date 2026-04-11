package autostockpile

import "time"

const (
	serverDayBoundaryHour  = 4
	defaultServerUTCOffset = 8 * 60 * 60
)

var defaultServerLocation = time.FixedZone("GMT+8", defaultServerUTCOffset)

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

func currentServerWeekday() time.Weekday {
	return resolveServerWeekday(time.Now(), defaultServerLocation)
}
