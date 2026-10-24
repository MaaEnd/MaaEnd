package mapoverview

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// transportFunc 用本地响应替代网络请求，使解析测试不依赖远端服务和 CDN 状态。
type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// TestDownloadTimeBuckets 验证同一五分钟内复用 URL，跨边界时切换缓存参数，并确认 polygon 被忽略。
func TestDownloadTimeBuckets(t *testing.T) {
	start := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for _, offset := range []time.Duration{0, 4*time.Minute + 59*time.Second, 5 * time.Minute} {
		now := start.Add(offset)
		client := &http.Client{Transport: transportFunc(func(request *http.Request) (*http.Response, error) {
			want := start.Unix()
			if offset >= 5*time.Minute {
				want += 300
			}
			if got := request.URL.Query().Get("refresh"); got != strconv.FormatInt(want, 10) {
				t.Fatalf("refresh = %s, want %d", got, want)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{
				"region01":{"name":"四号谷地","area":{"map01_lv001":{"click":[455,478],"polygon":[[1,2]]}}}
			}`))}, nil
		})}
		points, err := download(client, now)
		if err != nil || len(points) != 1 || points[nodePrefix+"region01_EnterMap_map01_lv001"] != [2]int{455, 478} {
			t.Fatalf("download = %v, %v", points, err)
		}
	}
}

// TestDownloadRejectsInvalidData 区分整份解析失败与单项坐标不可用，防止意外容忍字段类型错误。
func TestDownloadRejectsInvalidData(t *testing.T) {
	for _, body := range []string{
		`{`, `[]`, `42`, `"invalid"`,
		`{"region01":[]}`, `{"region01":42}`, `{"region01":{"area":[]}}`,
		`{"region01":{"area":{"bad":[]}}}`, `{"region01":{"area":{"bad":42}}}`,
		`{"region01":{"area":{"bad":{"click":"1,2"}}}}`,
		`{"region01":{"area":{"bad":{"click":["1",2]}}}}`,
		// 类型错误时拒绝整份数据，即使其中存在有效坐标。
		`{"region01":{"area":{"good":{"click":[455,478]},"bad":{"click":[1.5,2]}}}}`,
	} {
		t.Run(body, func(t *testing.T) {
			client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			if points, err := download(client, time.Now()); err == nil || points != nil {
				t.Fatalf("invalid response accepted: %v, %v", points, err)
			}
		})
	}
	for _, status := range []int{0, http.StatusServiceUnavailable} {
		client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			if status == 0 {
				return nil, errors.New("offline")
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
		})}
		if points, err := download(client, time.Now()); err == nil || points != nil {
			t.Fatalf("failed request accepted: %v, %v", points, err)
		}
	}
}

// TestDownloadSkipsInvalidEntries 确认空地区、坐标缺失或越界不会连带丢弃同批有效地图。
func TestDownloadSkipsInvalidEntries(t *testing.T) {
	valid := `"region01":{"area":{"map01_lv001":{"click":[455,478]}}}`
	want := map[string][2]int{nodePrefix + "region01_EnterMap_map01_lv001": {455, 478}}
	check := func(t *testing.T, body string) {
		t.Helper()
		client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		points, err := download(client, time.Now())
		if err != nil || !reflect.DeepEqual(points, want) {
			t.Fatalf("download = %v, %v; want %v", points, err, want)
		}
	}
	for _, region := range []string{`null`, `{}`, `{"area":{}}`} {
		t.Run("region/"+region, func(t *testing.T) {
			check(t, fmt.Sprintf(`{%s,"broken":%s}`, valid, region))
		})
	}
	for _, entry := range []string{
		`null`, `{}`, `{"click":null}`,
		`{"click":[1280,2]}`, `{"click":[-1,2]}`, `{"click":[1,-1]}`, `{"click":[1,720]}`,
		`{"click":[1]}`, `{"click":[1,2,3]}`, `{"click":[null,2]}`,
	} {
		t.Run("map/"+entry, func(t *testing.T) {
			// 同时覆盖地区内局部错误和另一个地区全部无可用坐标的情况。
			body := fmt.Sprintf(`{"region01":{"area":{"map01_lv001":{"click":[455,478]},"bad":%s}},"region02":{"area":{"bad":%s}}}`, entry, entry)
			check(t, body)
		})
	}
}

// TestDownloadAcceptsKeysAndEmptyData 确认 key 不受固定命名约束，空数据只产生空更新。
func TestDownloadAcceptsKeysAndEmptyData(t *testing.T) {
	for _, test := range []struct {
		body string
		want map[string][2]int
	}{
		{body: `{}`, want: map[string][2]int{}},
		{body: `null`, want: map[string][2]int{}},
		{body: `{"region01":{"area":{"bad":{"click":[1]}}}}`, want: map[string][2]int{}},
		{body: `{"奇怪-Region":{"name":123,"area":{"Map.with-特殊key":{"click":[0,719]}}},"":{"area":{"":{"click":[1279,0]}}}}`,
			want: map[string][2]int{nodePrefix + "奇怪-Region_EnterMap_Map.with-特殊key": {0, 719}, nodePrefix + "_EnterMap_": {1279, 0}}},
	} {
		t.Run(test.body, func(t *testing.T) {
			client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})}
			points, err := download(client, time.Now())
			if err != nil || !reflect.DeepEqual(points, test.want) {
				t.Fatalf("download = %v, %v; want %v", points, err, test.want)
			}
		})
	}
}

// clickRecorder 由框架调用并记录最终点击坐标，验证偏移已应用；不向真实游戏发送操作。
type clickRecorder struct {
	maa.BlankController
	clicks chan [2]int
}

func (c *clickRecorder) Click(x, y int32) bool {
	c.clicks <- [2]int{int(x), int(y)}
	return true
}

// TestRuntimeCoordinates 用真实框架确认 Pipeline 引用新坐标，资源的其他字段保持不变。
// 显式设置 MAAFW_BINARY_PATH 后运行；普通单元测试不要求安装动态库。
func TestRuntimeCoordinates(t *testing.T) {
	libDir := os.Getenv("MAAFW_BINARY_PATH")
	if libDir == "" {
		t.Skip("set MAAFW_BINARY_PATH to run the native integration test")
	}
	if err := maa.Init(maa.WithLibDir(libDir), maa.WithLogDir(t.TempDir()), maa.WithStdoutLevel(maa.LoggingLevelError)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := maa.Release(); err != nil {
			t.Error(err)
		}
	})
	resource, err := maa.NewResource()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resource.Destroy() })
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "pipeline"), 0755); err != nil {
		t.Fatal(err)
	}
	name := nodePrefix + "region01_EnterMap_map01_lv001"
	// Entry 通过 next 引用总览节点，验证普通 Pipeline 调用；Success 用于确认后继流程未被改写。
	// OverridePipeline 是增量合并，因此夹具显式给出零偏移，便于每个用例清除前一用例的偏移。
	fixture := `{
		"Entry":{"recognition":"DirectHit","pre_delay":0,"post_delay":0,"next":["` + name + `"]},
		"` + name + `":{"recognition":"DirectHit","action":{"type":"Click","param":{"target":[10,20],"target_offset":[0,0,0,0]}},"pre_delay":0,"post_delay":0,"next":["Success"]},
		"Success":{"recognition":"DirectHit","pre_delay":0,"post_delay":0},
		"OverrideSuccess":{"recognition":"DirectHit","pre_delay":0,"post_delay":0}
	}`
	if err := os.WriteFile(filepath.Join(dir, "pipeline", "nodes.json"), []byte(fixture), 0644); err != nil {
		t.Fatal(err)
	}
	if !resource.PostBundle(dir).Wait().Success() {
		t.Fatal("failed to load fixture")
	}
	recorder := &clickRecorder{clicks: make(chan [2]int, 1)}
	controller, err := maa.NewCustomController(recorder)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { controller.Destroy() })
	if !controller.PostConnect().Wait().Success() {
		t.Fatal("failed to connect controller")
	}
	tasker, err := maa.NewTasker()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tasker.Destroy() })
	if err := tasker.BindResource(resource); err != nil {
		t.Fatal(err)
	}
	if err := tasker.BindController(controller); err != nil {
		t.Fatal(err)
	}
	s := &sink{}
	tasker.AddSink(s)
	completed := make(chan string, 3)
	// Context 事件仅作为测试观察手段；生产代码通过上面的 Tasker sink 更新资源。
	// 三个动作完成事件对应 Entry、总览点击和 Success，缓冲队列避免回调等待断言代码。
	tasker.OnNodeActionInContext(func(_ *maa.Context, event maa.EventStatus, detail maa.NodeActionDetail) {
		if event == maa.EventStatusSucceeded {
			completed <- detail.Name
		}
	})
	// 分别验证资源字段保留、显式任务覆盖的优先级，以及离线、未知地图和坐标未变时的行为。
	for _, test := range []struct {
		name             string
		points           map[string][2]int
		resourceOverride any
		override         any
		want             [2]int
		success          string
	}{
		{name: "pipeline-reference", points: map[string][2]int{name: {455, 478}}, want: [2]int{455, 478}, success: "Success"},
		{name: "resource-fields-preserved", points: map[string][2]int{name: {455, 478}}, want: [2]int{458, 482}, success: "OverrideSuccess",
			resourceOverride: map[string]any{name: map[string]any{"action": map[string]any{"param": map[string]any{"target_offset": [4]int{3, 4, 0, 0}}}, "next": []string{"OverrideSuccess"}}}},
		{name: "explicit-task-override", points: map[string][2]int{name: {455, 478}}, want: [2]int{73, 84}, success: "OverrideSuccess",
			override: map[string]any{name: map[string]any{"action": map[string]any{"param": map[string]any{"target": [2]int{70, 80}, "target_offset": [4]int{3, 4, 0, 0}}}, "next": []string{"OverrideSuccess"}}}},
		{name: "offline", want: [2]int{10, 20}, success: "Success"},
		{name: "missing-map", points: map[string][2]int{nodePrefix + "region99_EnterMap_unknown": {1, 2}}, want: [2]int{10, 20}, success: "Success"},
		{name: "unchanged", points: map[string][2]int{name: {10, 20}}, want: [2]int{10, 20}, success: "Success"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// 各用例在任务结束后顺序执行，重新建立事件队列与资源基线，避免上次失败污染后续用例。
			completed = make(chan string, 3)
			if err := resource.OverridePipeline(fixture); err != nil {
				t.Fatal(err)
			}
			if test.resourceOverride != nil {
				if err := resource.OverridePipeline(test.resourceOverride); err != nil {
					t.Fatal(err)
				}
			}
			before, err := resource.GetNode(name)
			if err != nil {
				t.Fatal(err)
			}
			s.load = func() map[string][2]int { return test.points }
			// 仅替换下载结果；资源读取、局部覆盖、Pipeline 执行和控制器点击仍走真实框架。
			job := tasker.PostTask("Entry", test.override).Wait()
			if !job.Success() {
				t.Fatal("task failed")
			}
			select {
			case got := <-recorder.clicks:
				if got != test.want {
					t.Fatalf("click = %v, want %v", got, test.want)
				}
			default:
				t.Fatal("click was not executed")
			}
			// 从执行完成事件核对实际路径，不依赖额外的运行详情缓存查询。
			for _, want := range []string{"Entry", name, test.success} {
				select {
				case got := <-completed:
					if got != want {
						t.Fatalf("completed node = %s, want %s", got, want)
					}
				default:
					t.Fatalf("node did not complete: %s", want)
				}
			}
			local, err := resource.GetNode(name)
			if err != nil {
				t.Fatal(err)
			}
			target, err := local.Action.Param.(*maa.ClickParam).Target.AsRect()
			wantTarget := [2]int{10, 20}
			if point, exists := test.points[name]; exists {
				wantTarget = point
			}
			if err != nil || target.X() != wantTarget[0] || target.Y() != wantTarget[1] {
				t.Fatalf("resource target = %v, %v; want %v", target, err, wantTarget)
			}
			// 排除唯一允许更新的 target 后，完整比较节点，防止误改其他配置。
			local.Action.Param.(*maa.ClickParam).Target = before.Action.Param.(*maa.ClickParam).Target
			if !reflect.DeepEqual(local, before) {
				t.Fatalf("fields other than target changed:\nbefore: %+v\nafter: %+v", before, local)
			}
		})
	}
}
