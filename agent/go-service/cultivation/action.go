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
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
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
	// Weapon is the CultivationWeapon option: 0 none, n the n-th usable weapon,
	// absent keeps the stored choice.
	Weapon *int `json:"weapon"`
}
type session struct {
	TaskID   int64
	Header   core.Header
	Revision uint64
	Started  time.Time
	Passes   [2][]core.Material
	Rows     []core.Material
	// Placed holds this tab's cells by list position; Offset is how far the
	// list has scrolled since the top, measured page to page.
	Placed core.PlacedRows
	Offset int
	// Headers are the weapon section titles seen in this tab, by list position.
	Headers     []placedHeader
	Previous    image.Image
	Stable      int
	Pages       int
	Empty       bool
	Section     string
	WeaponNames [2][]string
	Essence     [2]map[string]core.Essence
	Icons       [2]map[uint64]iconIdentity
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
	if p.Step == "confirm" {
		// Invalidate an in-flight scan through the storage revision as well.
		scan = nil
		if err := confirmPlan(); err != nil {
			return fail(ctx, err)
		}
		return finishOrFail(ctx, preview(ctx, false, nil), "")
	}
	if p.Step == "operator_missing" {
		// The whole operator list was searched without a portrait match.
		maafocus.Print(ctx, i18n.T("cultivation.operator_missing"))
		return false
	}
	if p.Step == "expectation_missing" {
		// The 培养期望 panel or the chosen level could not be confirmed; reading
		// under another level would give the wrong gap.
		maafocus.Print(ctx, i18n.T("cultivation.expectation_missing"))
		return false
	}
	if p.Step == "farm" {
		// Consumes sanity through the upstream ProtocolSpace task; runs only from
		// the CultivationFarm task the user starts.
		return finishOrFail(ctx, farm(ctx, arg.CurrentTaskName, p.Weapon), "")
	}
	if p.Step == "preview" || p.Step == "export" {
		return finishOrFail(ctx, preview(ctx, p.Step == "export", p.Weapon), "")
	}
	if pienv.ControllerName() != "CloudADB" {
		return fail(ctx, errors.New("the cultivation scan supports CloudADB only"))
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return fail(ctx, errors.New("missing tasker"))
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		return fail(ctx, errors.New("missing controller"))
	}
	// DirectHit entry nodes do not capture, so always take a fresh frame
	ctrl.PostScreencap().Wait()
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
		// Every tab finished only with rows or the page's own "nothing needed"
		// statement, so empty passes here mean the operator needs nothing.
		plan, err := core.Confirm(scan.Header, scan.Passes[0], scan.Passes[1], scan.Revision, time.Now(), true)
		if err != nil {
			return fail(ctx, err)
		}
		if !slices.Equal(scan.WeaponNames[0], scan.WeaponNames[1]) {
			return fail(ctx, errors.New("usable weapons differ between complete passes"))
		}
		catalog, err := weaponCatalog()
		if err != nil {
			return fail(ctx, err)
		}
		for _, name := range scan.WeaponNames[0] {
			first, ok1 := scan.Essence[0][name]
			second, ok2 := scan.Essence[1][name]
			if !ok1 || !ok2 || first != second {
				return fail(ctx, fmt.Errorf("essence panel of %s missing or differs between passes", name))
			}
			weapon := core.Weapon{Name: name, CandidateIDs: catalog[name], EssenceStatus: "unverified_manual", Essence: &first}
			if len(weapon.CandidateIDs) == 1 {
				weapon.ID = weapon.CandidateIDs[0]
			}
			plan.Weapons = append(plan.Weapons, weapon)
		}
		err = savePlanWithWeapon(plan, scan.Revision, p.Weapon)
		scan = nil
		if err != nil {
			return fail(ctx, err)
		}
		if err = preview(ctx, false, nil); err != nil {
			log.Error().Err(err).Str("component", "Cultivation").Msg("scan saved; progress preview failed")
			maafocus.Print(ctx, i18n.T("cultivation.scan_saved_preview_failed"))
			return false
		}
		return true
	}
	if p.Tab < 0 || p.Tab >= 4 || p.Pass < 0 || p.Pass > 1 {
		return fail(ctx, errors.New("invalid scan phase"))
	}
	if !core.TabSelected(img, tabX[p.Tab], 207) {
		return fail(ctx, errors.New("selected tab not confirmed"))
	}
	switch p.Step {
	case "reset":
		scan.Section = ""
		scan.Rows = nil
		scan.Placed = core.PlacedRows{}
		scan.Offset = 0
		scan.Headers = nil
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
		catalog, err := weaponCatalog()
		if err != nil {
			return fail(ctx, err)
		}
		if p.Tab == 3 {
			return readEssenceStep(ctx, img, p, catalog)
		}
		if scan.Icons[p.Pass] == nil {
			scan.Icons[p.Pass] = map[uint64]iconIdentity{}
		}
		if scan.Pages > 0 {
			// A swipe scrolls the list by a varying distance (0.5x-1.6x the
			// finger on CloudADB), so measure it instead of guessing overlap.
			shift, err := core.ScrollShift(scan.Previous, img, core.ListArea, 340)
			if err != nil {
				return fail(ctx, err)
			}
			if shift > core.ReadBand {
				return fail(ctx, fmt.Errorf("list scrolled %d px, past rows between pages", shift))
			}
			scan.Offset += shift
		}
		// The section a page starts in is the last weapon title above its top,
		// not the one at the previous page's bottom: that title can still be
		// below rows that belong to the earlier weapon.
		top := ""
		for _, h := range scan.Headers {
			if h.Y <= scan.Offset+core.ListArea.Min.Y {
				top = h.Name
			}
		}
		rows, points, empty, section, headers, err := readMaterialPage(ctx, img, top, scan.Pages > 0, catalog, scan.Icons[p.Pass])
		if err != nil {
			return fail(ctx, err)
		}
		if slices.ContainsFunc(rows, func(m core.Material) bool { return m.Unknown }) {
			// A single blurred frame can miss a catalogued icon; an unknown item
			// must survive a re-read on a fresh frame.
			ctrl.PostScreencap().Wait()
			if img, err = ctrl.CacheImage(); err != nil {
				return fail(ctx, err)
			}
			if rows, points, empty, section, headers, err = readMaterialPage(ctx, img, top, scan.Pages > 0, catalog, scan.Icons[p.Pass]); err != nil {
				return fail(ctx, err)
			}
		}
		scan.Section = section
		for _, h := range headers {
			if !slices.Contains(scan.WeaponNames[p.Pass], h.Text) {
				scan.WeaponNames[p.Pass] = append(scan.WeaponNames[p.Pass], h.Text)
			}
			at := h.Box[1] + scan.Offset
			if !slices.ContainsFunc(scan.Headers, func(q placedHeader) bool { return q.Name == h.Text && q.Y-at <= 14 && at-q.Y <= 14 }) {
				scan.Headers = append(scan.Headers, placedHeader{Y: at, Name: h.Text})
				slices.SortFunc(scan.Headers, func(a, b placedHeader) int { return a.Y - b.Y })
			}
		}
		log.Info().Str("component", "Cultivation").Int("tab", p.Tab).Int("pass", p.Pass).Int("page", scan.Pages).
			Interface("rows", rows).Bool("empty", empty).Msg("cultivation page read")
		if err = scan.Placed.Add(rows, points, scan.Offset); err != nil {
			return fail(ctx, err)
		}
		scan.Rows = scan.Placed.Rows
		scan.Previous = img
		scan.Empty = scan.Empty || empty
		scan.Pages++
		if scan.Pages > 32 {
			return fail(ctx, errors.New("list end not confirmed within measured traversal bound"))
		}
		return true
	case "bottom":
		// The page just read already shows the scrollbar at the bottom: the
		// list is complete without another identical read. Lists without a
		// visible scrollbar still need the stationary proof below.
		if _, end, hasBar := scrollbarOf(scan.Previous); hasBar && end >= 652 && (len(scan.Rows) > 0 || scan.Empty) {
			scan.Passes[p.Pass] = append(scan.Passes[p.Pass], scan.Rows...)
			return choose(ctx, arg, p.Done)
		}
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
	log.Error().Err(err).Str("component", "Cultivation").Msg("cultivation step failed")
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

func preview(ctx *maa.Context, export bool, choice *int) error {
	planMu.Lock()
	stored, err := loadPlan()
	planMu.Unlock()
	if err != nil {
		return err
	}
	if stored.Plan == nil {
		return errors.New("no complete cultivation plan")
	}
	plan := *stored.Plan
	if choice != nil {
		if err = applyWeaponChoice(ctx, &plan, *choice); err != nil {
			return err
		}
	}
	return showModules(ctx, plan, export)
}

// applyWeaponChoice stores the CultivationWeapon option: 0 none, n the n-th usable weapon.
func applyWeaponChoice(ctx *maa.Context, plan *core.Plan, choice int) error {
	name, err := weaponChoiceName(*plan, choice)
	if err != nil {
		maafocus.Print(ctx, i18n.T("cultivation.weapon_choice_invalid", choice, len(plan.Weapons)))
		return err
	}
	if name != plan.WeaponChoice {
		if err := setWeaponChoice(plan.Revision, name); err != nil {
			return err
		}
		plan.WeaponChoice = name
		plan.Completion = nil
	}
	return nil
}

func weaponChoiceName(plan core.Plan, choice int) (string, error) {
	if choice < 0 || choice > len(plan.Weapons) {
		return "", fmt.Errorf("weapon choice %d out of %d usable weapons", choice, len(plan.Weapons))
	}
	if choice == 0 {
		return "", nil
	}
	return plan.Weapons[choice-1].Name, nil
}

// readEssenceStep reads one 武器基质 page: per-weapon essence counts, no
// inventory materials.
func readEssenceStep(ctx *maa.Context, img image.Image, p stepParam, catalog map[string][]string) bool {
	texts, err := ocrTexts(ctx, img, [4]int{420, 245, 820, 420})
	if err != nil {
		return fail(ctx, err)
	}
	headers := weaponHeaders(texts, catalog)
	lines, section, err := core.ReadEssencePanels(texts, headers, scan.Section)
	if err != nil {
		return fail(ctx, err)
	}
	if scan.Essence[p.Pass] == nil {
		scan.Essence[p.Pass] = map[string]core.Essence{}
	}
	for _, line := range lines {
		if !slices.Contains(scan.WeaponNames[p.Pass], line.Weapon) {
			return fail(ctx, fmt.Errorf("weapon %s on the essence tab was not listed on the weapon tab", line.Weapon))
		}
		if line.Injected != "" {
			scan.Essence[p.Pass][line.Weapon] = core.Essence{Injected: line.Injected}
			continue
		}
		// The flawless count sits at a fixed offset in the panel; single digits
		// are often missed by full-page OCR, so read it in its own box.
		value, ok, err := iconqty.RecognizeQuantityInROI(ctx, img, maa.Rect{672, line.Y - 4, 40, 28})
		if err != nil {
			return fail(ctx, err)
		}
		if !ok {
			return fail(ctx, fmt.Errorf("unreadable flawless essence count for %s", line.Weapon))
		}
		scan.Essence[p.Pass][line.Weapon] = core.Essence{Adapted: line.Adapted, Flawless: int64(value)}
	}
	log.Info().Str("component", "Cultivation").Int("tab", p.Tab).Int("pass", p.Pass).Int("page", scan.Pages).
		Interface("essence", scan.Essence[p.Pass]).Msg("cultivation essence page read")
	scan.Section = section
	scan.Previous = img
	// The tab has no material rows; a read panel or the page's own "maxed"
	// statement stands for a confirmed list.
	scan.Empty = scan.Empty || len(lines) > 0 || core.NothingNeeded(texts)
	scan.Pages++
	if scan.Pages > 32 {
		return fail(ctx, errors.New("list end not confirmed within measured traversal bound"))
	}
	return true
}

// scrollbarOf is core.Scrollbar for a possibly missing frame.
func scrollbarOf(img image.Image) (int, int, bool) {
	if img == nil {
		return 0, 0, false
	}
	return core.Scrollbar(img)
}

// placedHeader is a weapon section title at its position in the whole list.
type placedHeader struct {
	Y    int
	Name string
}
