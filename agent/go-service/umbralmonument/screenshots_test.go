package umbralmonument

import (
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// Fixture files stay outside the source tree: raw player screenshots may contain UIDs.
// Set MAAEND_SCREENSHOT_ROOT to an installation containing maafw/, resource/, temp/.
type screenshotController struct {
	maa.BlankController
	img          image.Image
	afterPrepare image.Image
	afterLeave   image.Image
	clicks       int
}

func (c *screenshotController) Screencap() (image.Image, bool) { return c.img, c.img != nil }
func (c *screenshotController) Click(x, y int32) bool {
	w, h := c.img.Bounds().Dx(), c.img.Bounds().Dy()
	px, py := int(x)*1280/w, int(y)*720/h
	if c.afterPrepare != nil && px >= 1040 && px <= 1250 && py >= 640 && py <= 690 {
		c.img, c.afterPrepare = c.afterPrepare, nil
	} else if c.afterPrepare == nil && c.afterLeave != nil && px >= 790 && px <= 910 && py >= 595 && py <= 690 {
		c.img, c.afterLeave = c.afterLeave, nil
	} else {
		return false
	}
	c.clicks++
	return true
}

func TestScreenshots(t *testing.T) {
	root := os.Getenv("MAAEND_SCREENSHOT_ROOT")
	if root == "" {
		t.Skip("set MAAEND_SCREENSHOT_ROOT for supplied-screenshot OCR and settlement checks")
	}
	if err := maa.Init(maa.WithLibDir(filepath.Join(root, "maafw"))); err != nil {
		t.Fatal(err)
	}
	defer maa.Release()
	res, err := maa.NewResource()
	if err != nil {
		t.Fatal(err)
	}
	defer res.Destroy()
	if !res.PostBundle(filepath.Join(root, "resource")).Wait().Success() {
		t.Fatal("installed resource load failed")
	}
	// Overlay only this feature, never modify the installation under test.
	bundle := t.TempDir()
	if err = os.Mkdir(filepath.Join(bundle, "pipeline"), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "assets", "resource", "pipeline", prefix, prefix+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(bundle, "pipeline", prefix+".json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if !res.PostBundle(bundle).Wait().Success() {
		t.Fatal("feature resource load failed")
	}
	if err = res.RegisterCustomAction("UmbralMonumentState", &StateAction{}); err != nil {
		t.Fatal(err)
	}
	if err = res.RegisterCustomRecognition("UmbralMonumentSelect", &Selection{}); err != nil {
		t.Fatal(err)
	}
	fixture := &screenshotController{}
	ctrl, err := maa.NewCustomController(fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Destroy()
	if !ctrl.PostConnect().Wait().Success() {
		t.Fatal("fixture connect failed")
	}
	tasker, err := maa.NewTasker()
	if err != nil {
		t.Fatal(err)
	}
	defer tasker.Destroy()
	if err = tasker.BindResource(res); err != nil {
		t.Fatal(err)
	}
	if err = tasker.BindController(ctrl); err != nil {
		t.Fatal(err)
	}
	loadImage := func(file string) image.Image {
		t.Helper()
		f, err := os.Open(filepath.Join(root, "temp", file))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	run := func(t *testing.T, callback func(*maa.Context) bool, overrides map[string]any) {
		t.Helper()
		if err := res.RegisterCustomAction("MonumentTest", maa.CustomActionFunc(func(ctx *maa.Context, _ *maa.CustomActionArg) bool { return callback(ctx) })); err != nil {
			t.Fatal(err)
		}
		defer res.UnregisterCustomAction("MonumentTest")
		overrides["MonumentTest"] = map[string]any{"action": "Custom", "custom_action": "MonumentTest"}
		if !tasker.PostTask("MonumentTest", overrides).Wait().Success() {
			t.Fatal("fixture task failed")
		}
	}
	for _, tc := range []struct {
		file         string
		hits, misses []string
		states       []int
	}{
		{"1.png", []string{"Permanent", "Entrance"}, []string{"Prepare"}, nil},
		{"2.png", []string{"SeasonPage", "Seasons"}, []string{"Prepare"}, nil},
		{"3-普通.png", []string{"Mode", "Challenge"}, []string{"Prepare"}, []int{1, 1, 1, 1, 1, 0}},
		{"3-苦难.png", []string{"Mode", "Challenge"}, []string{"Prepare"}, []int{1, 1, 1, 1, 1, 0}},
		{"3-通关.png", []string{"Mode", "Challenge"}, []string{"Prepare"}, []int{2, 2, 2, 1}},
		{"4.png", []string{"VictoryTitle", "LeaveButton"}, []string{"Prepare", "Failure"}, nil},
		{"5.png", []string{"Prepare"}, []string{"Mode", "Challenge"}, nil},
		{"6.png", []string{"Failure", "LeaveButton"}, []string{"VictoryTitle", "Prepare"}, nil},
	} {
		t.Run(tc.file, func(t *testing.T) {
			fixture.img = loadImage(tc.file)
			run(t, func(ctx *maa.Context) bool {
				if !ctrl.PostScreencap().Wait().Success() {
					t.Error("capture failed")
					return false
				}
				img, err := ctrl.CacheImage()
				if err != nil {
					t.Error(err)
					return false
				}
				for _, want := range []bool{true, false} {
					names := tc.hits
					if !want {
						names = tc.misses
					}
					for _, n := range names {
						d, err := ctx.RunRecognition(prefix+n, img)
						if err != nil || d == nil || d.Hit != want {
							t.Errorf("%s hit want=%v result=%v err=%v", n, want, d, err)
							return false
						}
					}
				}
				if tc.states != nil {
					items, err := read(ctx, img, "Stages")
					if err != nil {
						t.Error(err)
						return false
					}
					states := []int{}
					for _, item := range items {
						states = append(states, stageState(img, item.Box))
					}
					if !reflect.DeepEqual(states, tc.states) {
						t.Errorf("states=%v want=%v", states, tc.states)
						return false
					}
				}
				return true
			}, map[string]any{})
		})
	}
	fixture.img = loadImage("2.png")
	run(t, func(ctx *maa.Context) bool {
		if err := update(ctx, "reset"); err != nil {
			t.Error(err)
			return false
		}
		s, err := load(ctx)
		if err != nil {
			t.Error(err)
			return false
		}
		s.Seasons["old"] = true
		s.Attempts["old"] = 3
		s.Passed["old"] = 2
		if err = save(ctx, s); err != nil {
			t.Error(err)
			return false
		}
		// Changing seasons must discard stage progress even if attach objects merge.
		s.Attempts = map[string]uint8{}
		s.Passed = map[string]int{}
		if err = save(ctx, s); err != nil {
			t.Error(err)
			return false
		}
		s, err = load(ctx)
		if err != nil || len(s.Attempts) != 0 || len(s.Passed) != 0 {
			t.Errorf("season reset: %+v %v", s, err)
			return false
		}
		if err = update(ctx, "reset"); err != nil {
			t.Error(err)
			return false
		}
		s, err = load(ctx)
		if err != nil || len(s.Seasons) != 0 {
			t.Errorf("task reset: %+v %v", s, err)
			return false
		}
		return true
	}, map[string]any{})
	for _, hard := range []bool{false, true} {
		fixture.img = loadImage("3-普通.png")
		if err := res.RegisterCustomAction("MonumentSelected", maa.CustomActionFunc(func(ctx *maa.Context, _ *maa.CustomActionArg) bool {
			s, err := load(ctx)
			if err != nil {
				t.Error(err)
				return false
			}
			want := "忿鼓咆声"
			if hard {
				want = "刺痛盾阵"
			}
			if s.Stage != want || s.Hard != hard {
				t.Errorf("selection=%+v want stage=%s hard=%v", s, want, hard)
				return false
			}
			// The supplied screenshot still shows the last normal stage. A selected
			// first hard stage must not be considered ready until the title changes.
			img, err := ctrl.CacheImage()
			if err != nil {
				t.Error(err)
				return false
			}
			d, err := ctx.RunRecognition(prefix+"Ready", img)
			if err != nil || d == nil || d.Hit == hard {
				t.Errorf("ready guard: hard=%v result=%v err=%v", hard, d, err)
				return false
			}
			return true
		})); err != nil {
			t.Fatal(err)
		}
		run(t, func(ctx *maa.Context) bool {
			if err := update(ctx, "reset"); err != nil {
				t.Error(err)
				return false
			}
			s, err := load(ctx)
			if err != nil {
				t.Error(err)
				return false
			}
			s.IncludeHard = hard
			if err = save(ctx, s); err != nil {
				t.Error(err)
				return false
			}
			d, err := ctx.RunTask(prefix + "SelectStage")
			if err != nil || d == nil || !d.Status.Success() {
				t.Errorf("selection task: %v %v", d, err)
				return false
			}
			return true
		}, map[string]any{
			prefix + "SelectStage": map[string]any{"action": "DoNothing", "next": []string{"MonumentSelected"}},
			"MonumentSelected":     map[string]any{"action": "Custom", "custom_action": "MonumentSelected"},
		})
		res.UnregisterCustomAction("MonumentSelected")
	}
	for _, failure := range []bool{false, true} {
		fixture.img = loadImage("5.png")
		fixture.afterPrepare = loadImage("4.png")
		fixture.afterLeave = loadImage("3-普通.png")
		fixture.clicks = 0
		if err := res.RegisterCustomAction("MonumentVerify", maa.CustomActionFunc(func(ctx *maa.Context, _ *maa.CustomActionArg) bool {
			s, err := load(ctx)
			if err != nil {
				t.Error(err)
				return false
			}
			if failure && s.Attempts[s.Stage] != 3 || !failure && s.Passed[s.Stage] != 1 {
				t.Errorf("wrong progress: %+v", s)
				return false
			}
			return true
		})); err != nil {
			t.Fatal(err)
		}
		overrides := map[string]any{
			prefix + "BackToStages": map[string]any{"action": "Custom", "custom_action": "MonumentVerify", "next": []string{}},
			prefix + "Combat":       map[string]any{"enabled": false},
		}
		if failure {
			fixture.afterPrepare = loadImage("6.png")
		}
		run(t, func(ctx *maa.Context) bool {
			if err := update(ctx, "reset"); err != nil {
				t.Error(err)
				return false
			}
			s, err := load(ctx)
			if err != nil {
				t.Error(err)
				return false
			}
			s.Stage = "测试关卡"
			s.Attempts[s.Stage] = 1
			if err = save(ctx, s); err != nil {
				t.Error(err)
				return false
			}
			result, err := ctx.RunTask(prefix + "Prepare")
			if err != nil || result == nil || !result.Status.Success() {
				t.Errorf("settlement result=%v err=%v", result, err)
				return false
			}
			return true
		}, overrides)
		res.UnregisterCustomAction("MonumentVerify")
		if fixture.clicks != 2 {
			t.Fatalf("prepare/leave clicks=%d want=2", fixture.clicks)
		}
	}
}
