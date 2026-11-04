package countdown

import (
	"strconv"
	"strings"
	"time"
)

const (
	mmFormat = `{mm}`
	mFormat  = `{m}`
	ssFormat = `{ss}`
	sFormat  = `{s}`
)

func durationFormat(d time.Duration, formatFull, formatLittle string) string {
	totalSeconds := int64(d.Seconds())
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60

	result := formatLittle
	if minutes > 0 {
		result = formatFull
		m := strconv.Itoa(int(minutes))
		mm := m
		if minutes < 10 {
			mm = "0" + mm
		}
		result = strings.ReplaceAll(result, mmFormat, mm)
		result = strings.ReplaceAll(result, mFormat, m)
	}

	s := strconv.Itoa(int(seconds))
	ss := s
	if seconds < 10 {
		ss = "0" + ss
	}
	result = strings.ReplaceAll(result, ssFormat, ss)
	result = strings.ReplaceAll(result, sFormat, s)

	return result
}
