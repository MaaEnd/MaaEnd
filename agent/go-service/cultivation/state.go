package cultivation

import (
	"errors"
	"image"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
)

// decide stores an observation. Pipeline guards own the corresponding edges.
func (s *session) decide(value string) bool {
	s.Decision = value
	return true
}

// resetList starts independent scrollbar evidence for the newly selected tab.
func (s *session) resetList(img image.Image) {
	s.Previous = nil
	s.Pages = 0
	s.Observed = false
	_, _, s.ScrollbarSeen = core.Scrollbar(img)
}

// observeScrollbar retains evidence even when Previous is cleared at the top.
func (s *session) observeScrollbar(img image.Image) error {
	_, _, bar := core.Scrollbar(img)
	if s.ScrollbarSeen && !bar {
		return errors.New("previously observed scrollbar disappeared")
	}
	s.ScrollbarSeen = s.ScrollbarSeen || bar
	return nil
}

// listTop requires either the measured top or proof of upward movement.
// Stationarity away from an endpoint fails instead of repeating the swipe.
func listTop(previous, current image.Image) (string, error) {
	start, _, bar := core.Scrollbar(current)
	if bar && start <= 275 {
		return "done", nil
	}
	if previous == nil {
		return "again", nil
	}
	if _, _, seen := core.Scrollbar(previous); seen && !bar {
		return "", errors.New("scrollbar disappeared while finding top")
	}
	if core.Stationary(previous, current) {
		if !bar {
			return "done", nil
		}
		return "", errors.New("list is stationary away from top")
	}
	shift, err := core.ScrollShift(current, previous, core.ScrollArea, 340)
	if err != nil || shift == 0 {
		return "", errors.New("upward navigation made no measurable progress")
	}
	return "again", nil
}

// listBottom completes only a read endpoint or a stationary short list.
func listBottom(previous, current image.Image, observed bool) (string, error) {
	if !observed {
		return "", errors.New("no material rows observed")
	}
	if _, end, bar := core.Scrollbar(previous); bar && end >= 652 {
		return "done", nil
	}
	if _, _, seen := core.Scrollbar(previous); seen {
		if _, _, bar := core.Scrollbar(current); !bar {
			return "", errors.New("scrollbar disappeared while finding bottom")
		}
	}
	if core.Stationary(previous, current) {
		if _, end, bar := core.Scrollbar(current); bar && end < 652 {
			return "", errors.New("list is stationary away from bottom")
		}
		return "done", nil
	}
	shift, err := core.ScrollShift(previous, current, core.ScrollArea, 340)
	if err != nil || shift <= 0 || shift > core.ReadBand {
		return "", errors.New("downward navigation made no measurable progress")
	}
	return "again", nil
}
