package cultivation

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Action handles one data operation; all clicks/swipes remain in Pipeline.
type Action struct{}

var _ maa.CustomActionRunner = &Action{}

type stepParam struct {
	Step  string `json:"step"`
	Tab   int    `json:"tab"`
	Pass  int    `json:"pass"`
	Again string `json:"again"`
	Done  string `json:"done"`
}
type session struct {
	TaskID       int64
	Header       core.Header
	Revision     uint64
	Started      time.Time
	Passes       [2][]core.Material
	Rows         []core.Material
	PreviousRows []core.Material
	Previous     image.Image
	Stable       int
	Pages        int
	Empty        bool
	Weapons      [2]*core.Weapon
}

var scanMu sync.Mutex
var scan *session
var tabX = []int{568, 750, 952, 1143}

// Run executes a single parser/storage step without directly operating the controller.
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
	if p.Step == "clear" {
		scan = nil
		return finishOrFail(ctx, clearPlan(), "cultivation.cleared")
	}
	if p.Step == "preview" || p.Step == "export" {
		return finishOrFail(ctx, preview(ctx, p.Step == "export"), "")
	}
	if pienv.ControllerName() != "CloudADB" {
		return fail(ctx, errors.New("CloudADB controller required for prototype"))
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return fail(ctx, errors.New("missing tasker"))
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		return fail(ctx, errors.New("missing controller"))
	}
	img, err := ctrl.CacheImage()
	if err != nil {
		return fail(ctx, err)
	}
	h, err := header(ctx, img)
	if err != nil {
		return fail(ctx, err)
	}
	if p.Step == "begin" {
		planMu.Lock()
		stored, err := loadPlan()
		planMu.Unlock()
		if err != nil {
			return fail(ctx, err)
		}
		scan = &session{TaskID: arg.TaskID, Header: h, Revision: stored.Revision, Started: time.Now()}
		return true
	}
	if scan == nil || scan.TaskID != arg.TaskID || scan.Header != h {
		return fail(ctx, errors.New("scan identity or target changed"))
	}
	if p.Step == "finish" {
		plan, err := core.Confirm(scan.Header, scan.Passes[0], scan.Passes[1], scan.Revision, time.Now())
		if err != nil {
			return fail(ctx, err)
		}
		if scan.Weapons[0] == nil || scan.Weapons[1] == nil || scan.Weapons[0].Name != scan.Weapons[1].Name || !slices.Equal(scan.Weapons[0].CandidateIDs, scan.Weapons[1].CandidateIDs) {
			return fail(ctx, errors.New("weapon identity differs between complete passes"))
		}
		plan.Weapon = scan.Weapons[0]
		err = savePlan(plan, scan.Revision)
		scan = nil
		if err != nil {
			return fail(ctx, err)
		}
		return finishOrFail(ctx, preview(ctx, false), "")
	}
	if p.Tab < 0 || p.Tab >= 4 || p.Pass < 0 || p.Pass > 1 {
		return fail(ctx, errors.New("invalid scan phase"))
	}
	if !core.TabSelected(img, tabX[p.Tab], 207) {
		return fail(ctx, errors.New("selected tab not confirmed"))
	}
	switch p.Step {
	case "reset":
		scan.Rows = nil
		scan.PreviousRows = nil
		scan.Previous = nil
		scan.Stable = 0
		scan.Pages = 0
		scan.Empty = false
		return true
	case "top":
		start, _, hasBar := core.Scrollbar(img)
		if hasBar && start <= 275 {
			scan.Previous = nil
			scan.Stable = 0
			scan.Pages = 0
			return choose(ctx, arg, p.Done)
		}
		if core.Stationary(img, scan.Previous) {
			scan.Stable++
		} else {
			scan.Stable = 0
		}
		if scan.Stable >= 2 {
			if hasBar {
				return fail(ctx, errors.New("list is stationary but not at top"))
			}
			scan.Previous = nil
			scan.Stable = 0
			scan.Pages = 0
			return choose(ctx, arg, p.Done)
		}
		scan.Previous = img
		scan.Pages++
		if scan.Pages > 12 {
			return fail(ctx, errors.New("cannot prove list top within measured traversal bound"))
		}
		return choose(ctx, arg, p.Again)
	case "read":
		if p.Tab == 3 && scan.Weapons[p.Pass] == nil {
			weapon, err := readWeapon(ctx, img)
			if err != nil {
				return fail(ctx, err)
			}
			scan.Weapons[p.Pass] = &weapon
		}
		rows, empty, err := readMaterialPage(ctx, img)
		if err != nil {
			return fail(ctx, err)
		}
		if scan.PreviousRows != nil && slices.Equal(scan.PreviousRows, rows) && !core.Stationary(img, scan.Previous) {
			return fail(ctx, errors.New("repeated material sequence on a changed viewport"))
		}
		scan.Rows = core.MergePage(scan.Rows, rows)
		scan.PreviousRows = rows
		scan.Previous = img
		scan.Empty = scan.Empty || empty
		scan.Pages++
		if scan.Pages > 16 {
			return fail(ctx, errors.New("list end not confirmed within measured traversal bound"))
		}
		return true
	case "bottom":
		if core.Stationary(img, scan.Previous) {
			scan.Stable++
		} else {
			scan.Stable = 0
		}
		if scan.Stable >= 2 {
			_, end, hasBar := core.Scrollbar(img)
			if hasBar && end < 652 {
				return fail(ctx, errors.New("list stopped before scrollbar bottom"))
			}
			if len(scan.Rows) == 0 && !scan.Empty {
				return fail(ctx, errors.New("empty list not confirmed"))
			}
			scan.Passes[p.Pass] = append(scan.Passes[p.Pass], scan.Rows...)
			return choose(ctx, arg, p.Done)
		}
		return choose(ctx, arg, p.Again)
	default:
		return fail(ctx, errors.New("unknown cultivation step"))
	}
}

func choose(ctx *maa.Context, arg *maa.CustomActionArg, next string) bool {
	if !strings.HasPrefix(next, "Cultivation") {
		return fail(ctx, errors.New("invalid cultivation continuation"))
	}
	return finishOrFail(ctx, ctx.OverridePipeline(map[string]any{arg.CurrentTaskName: map[string]any{"next": []string{next}}}), "")
}

func fail(ctx *maa.Context, err error) bool {
	log.Error().Err(err).Str("component", "Cultivation").Msg("cultivation step failed; old plan retained")
	maafocus.Print(ctx, i18n.T("cultivation.failed"))
	return false
}
func finishOrFail(ctx *maa.Context, err error, key string) bool {
	if err != nil {
		return fail(ctx, err)
	}
	if key != "" {
		maafocus.Print(ctx, i18n.T(key))
	}
	return true
}

func preview(ctx *maa.Context, export bool) error {
	planMu.Lock()
	stored, err := loadPlan()
	planMu.Unlock()
	if err != nil {
		return err
	}
	if stored.Plan == nil {
		return errors.New("no complete cultivation plan")
	}
	goals, manual, err := core.BuildTargets(*stored.Plan)
	if err != nil {
		return err
	}
	var lines []string
	for _, m := range stored.Plan.Materials {
		lines = append(lines, fmt.Sprintf("%s: %d / %d (-%d)", m.Name, m.Owned, m.Need, max(m.Need-m.Owned, 0)))
	}
	maafocus.Print(ctx, i18n.T("cultivation.preview", stored.Plan.Header.Operator, stored.Plan.Header.Expectation, len(manual))+"\n"+strings.Join(lines, "\n"))
	if stored.Plan.Weapon != nil {
		maafocus.Print(ctx, i18n.T("cultivation.weapon", stored.Plan.Weapon.Name))
	}
	if !export {
		return nil
	}
	var source struct {
		Option map[string]struct {
			PipelineOverride json.RawMessage `json:"pipeline_override"`
		} `json:"option"`
	}
	if err = resource.ReadJsonResource("tasks/ProtocolSpace.json", &source); err != nil {
		return err
	}
	template := source.Option["SupplyPlanLimits"].PipelineOverride
	if len(template) == 0 {
		return errors.New("missing SupplyPlan option template")
	}
	patch, err := core.RenderOverrides(template, goals)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(map[string]any{"source": core.SourceVersion, "revision": stored.Revision, "executable": false, "goals": goals, "manual": manual, "pipeline_override": patch}, "", "  ")
	if err != nil {
		return err
	}
	if err = fsutil.WriteFileAtomic(fsutil.OutputPath("debug", "record", "CultivationSupplyPlan.preview.json"), data, 0600); err != nil {
		return err
	}
	maafocus.Print(ctx, i18n.T("cultivation.exported"))
	return nil
}
