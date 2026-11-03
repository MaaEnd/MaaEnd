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

func durationFormat(d time.Duration, min, sec string) (fm string, fs string) {
	fm = min
	fs = sec

	totalSeconds := int64(d.Seconds())

	minutes := totalSeconds / 60
	seconds := totalSeconds % 60

	m := strconv.Itoa(int(minutes))
	mm := m
	if minutes < 10 {
		mm = "0" + mm
	}
	fm = strings.ReplaceAll(fm, mmFormat, mm)
	fm = strings.ReplaceAll(fm, mFormat, m)

	s := strconv.Itoa(int(seconds))
	ss := s
	if seconds < 10 {
		ss = "0" + ss
	}
	fs = strings.ReplaceAll(fs, ssFormat, ss)
	fs = strings.ReplaceAll(fs, sFormat, s)

	return
}
