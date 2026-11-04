package countdown

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	hhFormat = `{hh}`
	hFormat  = `{h}`
	mmFormat = `{mm}`
	mFormat  = `{m}`
	ssFormat = `{ss}`
	sFormat  = `{s}`
)

func (p *params) durationFormat() string {
	totalSeconds := int(p.duration.Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	result := p.DurationFormatS
	var replacer *strings.Replacer
	switch {
	case hours > 0:
		result = p.DurationFormatHMS
		replacer = strings.NewReplacer(
			hhFormat, fmt.Sprintf("%02d", hours),
			hFormat, strconv.Itoa(hours),
			mmFormat, fmt.Sprintf("%02d", minutes),
			mFormat, strconv.Itoa(minutes),
			ssFormat, fmt.Sprintf("%02d", seconds),
			sFormat, strconv.Itoa(seconds),
		)
	case minutes > 0:
		result = p.DurationFormatMS
		replacer = strings.NewReplacer(
			mmFormat, fmt.Sprintf("%02d", minutes),
			mFormat, strconv.Itoa(minutes),
			ssFormat, fmt.Sprintf("%02d", seconds),
			sFormat, strconv.Itoa(seconds),
		)
	default:
		replacer = strings.NewReplacer(
			ssFormat, fmt.Sprintf("%02d", seconds),
			sFormat, strconv.Itoa(seconds),
		)
	}

	return replacer.Replace(result)
}
