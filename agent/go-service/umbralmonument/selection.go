package umbralmonument

import (
	"encoding/json"
	"fmt"
	"image"
	"sort"
	"strings"
	"unicode"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Selection selects an unattempted season/stage or verifies the selected title.
// Each call uses the Pipeline screenshot; Pipeline performs clicks and scrolling.
type Selection struct{}

var _ maa.CustomRecognitionRunner = &Selection{}

func (r *Selection) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	var p params
	err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &p)
	var result *maa.CustomRecognitionResult
	if err == nil {
		result, err = selectItem(ctx, arg.Img, p.Phase)
	}
	if err != nil {
		log.Error().Err(err).Str("component", prefix).Msg("selection stopped; no blind clicks")
		if patchErr := ctx.OverridePipeline(map[string]any{prefix + "Abort": map[string]any{"enabled": true}}); patchErr != nil {
			log.Error().Err(patchErr).Str("component", prefix).Msg("cannot enable abort node")
		}
	}
	return result, result != nil && err == nil
}

func selectItem(ctx *maa.Context, img image.Image, phase string) (*maa.CustomRecognitionResult, error) {
	if img == nil || img.Bounds().Dy() != 720 || img.Bounds().Dx() < 1250 || img.Bounds().Dx() > 1310 {
		return nil, fmt.Errorf("expected a 720p 16:9 screenshot")
	}
	s, err := load(ctx)
	if err != nil {
		return nil, err
	}
	if s.Seasons == nil || s.Attempts == nil || s.Passed == nil {
		return nil, fmt.Errorf("progress not initialized")
	}
	page := "Mode"
	if phase == "season" {
		page = "SeasonPage"
	}
	check, err := ctx.RunRecognition(prefix+page, img)
	if err != nil {
		return nil, err
	}
	if check == nil || !check.Hit {
		return nil, nil
	}
	if phase == "selected" || phase == "ready" {
		items, err := read(ctx, img, "Title")
		if err != nil {
			return nil, err
		}
		title := ""
		for _, item := range items {
			title += clean(item.Text)
		}
		if s.Stage == "" || !sameTitle(title, s.Stage) {
			return nil, nil
		}
		if phase == "ready" && strings.HasSuffix(title, "苦难") != s.Hard {
			return nil, nil
		}
		return &maa.CustomRecognitionResult{Box: check.Box, Detail: s.Stage}, nil
	}
	if phase != "season" && phase != "stage" {
		return nil, fmt.Errorf("unknown selection phase %q", phase)
	}
	node := "Stages"
	if phase == "season" {
		node = "Seasons"
	}
	items, err := read(ctx, img, node)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		name := clean(item.Text)
		if phase == "season" {
			// Partial cards become fully visible after the next overlapping swipe.
			if item.Box[0] < 35 || item.Box[0]+item.Box[2] > 1160 || s.Seasons[name] {
				continue
			}
			s.Seasons[name] = true
			s.Attempts = map[string]uint8{}
			s.Passed = map[string]int{}
			s.Stage = ""
			if err = save(ctx, s, "SeasonsEnd", "StagesStart", "StagesEnd"); err != nil {
				return nil, err
			}
		} else {
			completed := stageState(img, item.Box)
			if completed < 0 {
				return nil, fmt.Errorf("unrecognized completion icon for %q", name)
			}
			hard, found := nextDifficulty(max(completed, s.Passed[name]), s.Attempts[name], s.IncludeHard)
			if !found {
				continue
			}
			s.Stage, s.Hard = name, hard
			bit := uint8(1)
			if hard {
				bit = 2
			}
			s.Attempts[name] |= bit
			if err = save(ctx, s, "StagesEnd"); err != nil {
				return nil, err
			}
			err = ctx.OverridePipeline(map[string]any{
				prefix + "Normal": map[string]any{"enabled": !hard},
				prefix + "Hard":   map[string]any{"enabled": hard},
			})
			if err != nil {
				return nil, err
			}
		}
		log.Info().Str("component", prefix).Str("phase", phase).Str("name", name).Bool("hard", s.Hard).Msg("selected item")
		return &maa.CustomRecognitionResult{Box: item.Box, Detail: name}, nil
	}
	return nil, nil
}

func read(ctx *maa.Context, img image.Image, node string) ([]maa.OCRResult, error) {
	d, err := ctx.RunRecognition(prefix+node, img)
	if err != nil {
		return nil, err
	}
	if d == nil || !d.Hit || d.Results == nil {
		return nil, fmt.Errorf("cannot read %s", node)
	}
	items := []maa.OCRResult{}
	for _, v := range d.Results.Filtered {
		if o, ok := v.AsOCR(); ok && chineseCount(o.Text) >= 2 {
			items = append(items, *o)
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no readable names in %s", node)
	}
	sort.Slice(items, func(i, j int) bool {
		if node == "Seasons" {
			return items[i].Box[0] < items[j].Box[0]
		}
		return items[i].Box[1] < items[j].Box[1]
	})
	return items, nil
}

// Allow one OCR substitution in a four-character name, but not a different length.
func sameTitle(title, name string) bool {
	title = strings.TrimSuffix(title, "苦难")
	if strings.Contains(title, name) {
		return true
	}
	a, b := []rune(title), []rune(name)
	if len(a) != len(b) || len(a) < 4 {
		return false
	}
	diff := 0
	for i := range a {
		if a[i] != b[i] {
			diff++
		}
	}
	return diff <= 1
}

func chineseCount(s string) int {
	n := 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			n++
		}
	}
	return n
}

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

func stageState(img image.Image, box maa.Rect) int {
	if img == nil {
		return -1
	}
	// ponytail: 16:9 stage icon column; add template anchors if the game changes this layout.
	y := box[1] + box[3]/2
	red, yellow, white := 0, 0, 0
	for yy := y - 11; yy <= y+11; yy++ {
		for x := 46; x < 68; x++ {
			rr, gg, bb, _ := img.At(x, yy).RGBA()
			r, g, b := int(rr>>8), int(gg>>8), int(bb>>8)
			if r > 130 && r > g*3/2 && r > b*3/2 {
				red++
			}
			if r > 145 && g > 125 && b*3 < min(r, g)*2 {
				yellow++
			}
			if min(r, min(g, b)) > 135 && max(r, max(g, b))-min(r, min(g, b)) < 35 {
				white++
			}
		}
	}
	if yellow >= 8 {
		return 2
	}
	if red >= 10 {
		return 1
	}
	if white >= 8 {
		return 0
	}
	return -1
}
