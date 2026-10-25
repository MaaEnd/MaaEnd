package cultivation

import (
	"encoding/json"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Guard verifies the overview identity before each Pipeline click or swipe.
type Guard struct{}

var _ maa.CustomRecognitionRunner = &Guard{}

// Run never retains a borrowed Context; only plain scan data crosses callbacks.
func (r *Guard) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		return nil, false
	}
	var p struct {
		Tab      *int   `json:"tab"`
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &p); err != nil {
		return nil, false
	}
	scanMu.Lock()
	defer scanMu.Unlock()
	if scan == nil || scan.TaskID != arg.TaskID || (p.Decision != "" && scan.Decision != p.Decision) {
		return nil, false
	}
	// Decision-only guards are task-bound data and skip the overview check.
	if p.Decision == "" || p.Tab != nil {
		h, err := header(ctx, arg.Img)
		if err != nil {
			log.Debug().Err(err).Str("component", "Cultivation").Msg("overview identity unavailable")
			return nil, false
		}
		if scan.Header != h {
			log.Debug().Str("component", "Cultivation").Interface("expected", scan.Header).Interface("observed", h).Msg("overview identity changed")
			return nil, false
		}
	}
	if p.Tab != nil {
		if *p.Tab < 0 || *p.Tab >= len(tabX) || !core.TabSelected(arg.Img, tabX[*p.Tab], 207) {
			return nil, false
		}
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}
