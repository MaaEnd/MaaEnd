package cultivation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Action reads red missing-material labels of one operator; Pipeline operates the game.
type Action struct{}

var _ maa.CustomActionRunner = &Action{}

type stepParam struct {
	Step string `json:"step"`
	Tab  int    `json:"tab"`
}

// session holds only this task's observations. No controller or inventory survives a run.
type session struct {
	TaskID        int64
	Header        core.Header
	Name          string
	Missing       map[string]core.Material
	Unrecognized  int
	Decision      string
	Previous      image.Image
	Pages         int
	Observed      bool
	ScrollbarSeen bool
}

var scanMu sync.Mutex
var scan *session
var tabX = []int{568, 750}

// Run processes one observation step, never launching nested tasks.
func (a *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	var p stepParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil {
		return fail(ctx, err)
	}
	scanMu.Lock()
	defer scanMu.Unlock()
	if p.Step == "operator_missing" || p.Step == "expectation_missing" {
		scan = nil
		if name, ok := target(arg.TaskID); ok {
			Record(arg.TaskID, name, Red{Failed: true, Day: GameDay(time.Now())})
		} else {
			log.Warn().Str("component", "Cultivation").Str("step", p.Step).Msg("no scan target set; nothing recorded")
		}
		maafocus.Print(ctx, i18n.T("cultivation."+p.Step))
		return true
	}
	if p.Step == "end" {
		scan = nil
		return true
	}
	if p.Step == "begin" {
		// A new run always discards unfinished observations from any prior task.
		scan = nil
		name, ok := target(arg.TaskID)
		if !ok {
			return fail(ctx, errors.New("no scan target set for this task"))
		}
		if pienv.ControllerName() != "CloudADB" {
			// The caller must not filter on this operator; Pipeline ends the scan.
			Record(arg.TaskID, name, Red{Failed: true, Day: GameDay(time.Now())})
			scan = &session{TaskID: arg.TaskID, Name: name, Decision: "skip"}
			log.Warn().Str("component", "Cultivation").Str("operator", name).Msg("cultivation supports CloudADB only; skipped")
			return true
		}
		tasker := ctx.GetTasker()
		if tasker == nil {
			return fail(ctx, errors.New("missing tasker"))
		}
		ctrl := tasker.GetController()
		if ctrl == nil {
			return fail(ctx, errors.New("missing controller"))
		}
		img, err := capture(ctrl)
		if err != nil {
			return fail(ctx, err)
		}
		h, err := header(ctx, img)
		if err != nil {
			return fail(ctx, err)
		}
		scan = &session{TaskID: arg.TaskID, Header: h, Name: name, Missing: map[string]core.Material{}}
		return true
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return fail(ctx, errors.New("missing tasker"))
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		return fail(ctx, errors.New("missing controller"))
	}
	img, err := capture(ctrl)
	if err != nil {
		return fail(ctx, err)
	}
	h, err := header(ctx, img)
	if err != nil {
		return fail(ctx, err)
	}
	if scan == nil || scan.TaskID != arg.TaskID || scan.Header != h {
		return fail(ctx, errors.New("operator or expectation changed during this run"))
	}
	if p.Step == "finish" {
		var rows []core.Material
		for _, row := range scan.Missing {
			rows = append(rows, row)
		}
		routes := core.Routes(rows)
		Record(arg.TaskID, scan.Name, Red{Routes: routes, Unrecognized: scan.Unrecognized, Day: GameDay(time.Now())})
		log.Info().Str("component", "Cultivation").Str("operator", scan.Name).Strs("routes", routes).Int("unrecognized", scan.Unrecognized).Msg("red material labels read")
		maafocus.Print(ctx, i18n.T("cultivation.finish", scan.Header.Operator, len(routes)))
		return scan.decide("done")
	}
	if p.Tab < 0 || p.Tab >= len(tabX) || !core.TabSelected(img, tabX[p.Tab], 207) {
		return fail(ctx, errors.New("material tab not confirmed"))
	}
	if p.Step == "reset" {
		scan.resetList(img)
		return true
	}
	if err := scan.observeScrollbar(img); err != nil {
		return fail(ctx, err)
	}
	switch p.Step {
	case "top":
		decision, err := listTop(scan.Previous, img)
		if err != nil {
			return fail(ctx, err)
		}
		scan.Previous = img
		if decision == "done" {
			scan.Previous = nil
		}
		return scan.decide(decision)
	case "read":
		texts, err := ocrTexts(ctx, img, [4]int{420, 245, 820, 420})
		if err != nil {
			return fail(ctx, err)
		}
		if scan.Pages > 0 {
			// Match visual edges on these exact frames. Numeric stock labels
			// are not identities, and a new screenshot would invalidate texts.
			// The preceding bottom check rejects stationary non-endpoints.
			// An unchanged frame contributes no offset to row alignment.
			shift := 0
			if !core.Stationary(scan.Previous, img) {
				measured, err := core.ScrollShift(scan.Previous, img, core.ScrollArea, 340)
				if err != nil {
					dumpScrollFailure(scan.Previous, img)
					return fail(ctx, err)
				}
				shift = measured
			}
			if shift < 0 || shift > core.ReadBand {
				return fail(ctx, fmt.Errorf("scroll %d exceeds visible read band", shift))
			}
		}
		missing, unrecognized, observed, err := readMaterialPage(ctx, img, texts, scan.Pages > 0)
		if err != nil {
			return fail(ctx, err)
		}
		for _, row := range missing {
			scan.Missing[row.ID] = row
		}
		scan.Unrecognized += unrecognized
		scan.Observed = scan.Observed || observed
		// Keep the same frame for OCR, red labels, icon identities and the
		// next alignment. Stability is handled by the preceding Pipeline node.
		scan.Previous = img
		scan.Pages++
		log.Info().Str("component", "Cultivation").Int("tab", p.Tab).Int("page", scan.Pages).Interface("missing", missing).Int("unrecognized", unrecognized).Msg("read red material labels")
		return true
	case "bottom":
		decision, err := listBottom(scan.Previous, img, scan.Observed)
		if err != nil {
			dumpScrollFailure(scan.Previous, img)
			return fail(ctx, err)
		}
		return scan.decide(decision)
	default:
		return fail(ctx, errors.New("unknown cultivation step"))
	}
}

func fail(ctx *maa.Context, err error) bool {
	log.Error().Err(err).Str("component", "Cultivation").Msg("cultivation step failed")
	maafocus.Print(ctx, i18n.T("cultivation.failed"))
	return false
}

func capture(ctrl *maa.Controller) (image.Image, error) {
	if job := ctrl.PostScreencap().Wait(); job == nil || !job.Success() {
		return nil, errors.New("screenshot failed")
	}
	return ctrl.CacheImage()
}

// dumpScrollFailure saves the alignment pair for offline forensics when the
// scroll could not be measured. Best effort: never masks the
// original error.
func dumpScrollFailure(prev, cur image.Image) {
	dir, err := fsutil.StartupDir()
	if err != nil {
		return
	}
	target := filepath.Join(dir, "debug", "cultivation-scroll-failure")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return
	}
	stamp := time.Now().Format("20060102-150405.000000000")
	for name, img := range map[string]image.Image{"prev": prev, "cur": cur} {
		if img == nil {
			continue
		}
		file := filepath.Join(target, fmt.Sprintf("%s-%s.png", stamp, name))
		var buf bytes.Buffer
		if encErr := png.Encode(&buf, img); encErr != nil {
			continue
		}
		_ = os.WriteFile(file, buf.Bytes(), 0o644)
	}
	log.Warn().Str("component", "Cultivation").Msg("saved unmeasurable scroll frames for forensics")
}
