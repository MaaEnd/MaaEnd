package cultivation

import (
	"image"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// NextCard finds the next operator card in the list that this task has not
// clicked yet. It is the fallback when no portrait template matches (skins,
// operators newer than the templates): the Pipeline clicks the returned card
// and confirms the header name, then asks for the next one.
type NextCard struct{}

var _ maa.CustomRecognitionRunner = &NextCard{}

// sweep remembers clicked cards by portrait hash; it holds plain data only and
// resets when a new task starts.
var sweepMu sync.Mutex
var sweep struct {
	TaskID int64
	Seen   map[uint64]bool
}

// Run returns the first unvisited card in roi, top to bottom.
func (r *NextCard) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	sweepMu.Lock()
	defer sweepMu.Unlock()
	if sweep.TaskID != arg.TaskID || sweep.Seen == nil {
		sweep.TaskID = arg.TaskID
		sweep.Seen = map[uint64]bool{}
	}
	area := image.Rect(arg.Roi[0], arg.Roi[1], arg.Roi[0]+arg.Roi[2], arg.Roi[1]+arg.Roi[3])
	for _, card := range core.FindCards(arg.Img, area) {
		// Hash the portrait part above the bar; the same operator can appear in
		// both the recent row and the full list, and is clicked once.
		face := image.Rect(card.Min.X+card.Dx()/5, card.Max.Y-card.Dy()*4/5, card.Max.X-card.Dx()/5, card.Max.Y-card.Dy()/5)
		hash := core.IconHash(arg.Img, face)
		if sweep.Seen[hash] {
			continue
		}
		sweep.Seen[hash] = true
		// Click the lower half of the card, which stays visible near the list top.
		target := image.Rect(card.Min.X, card.Max.Y-card.Dy()/2, card.Max.X, card.Max.Y-8)
		return &maa.CustomRecognitionResult{Box: maa.Rect{target.Min.X, target.Min.Y, target.Dx(), target.Dy()}}, true
	}
	return nil, false
}
