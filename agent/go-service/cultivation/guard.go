package cultivation

import (
	"encoding/json"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
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
		Tab *int `json:"tab"`
	}
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &p); err != nil {
		return nil, false
	}
	h, err := header(ctx, arg.Img)
	if err != nil {
		return nil, false
	}
	scanMu.Lock()
	defer scanMu.Unlock()
	if scan == nil || scan.TaskID != arg.TaskID || scan.Header != h {
		return nil, false
	}
	if p.Tab != nil {
		if *p.Tab < 0 || *p.Tab >= 4 || !core.TabSelected(arg.Img, tabX[*p.Tab], 207) {
			return nil, false
		}
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}
