package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestParseConfig(t *testing.T) {
	nodeJSON := `{
		"recognition": "DirectHit",
		"action": "DoNothing",
		"attach": {
			"webhook_enabled": true,
			"webhook_url": "https://example.com/hook",
			"webhook_method": "POST",
			"webhook_headers": "X-A: 1",
			"webhook_body": "{\"a\":1}",
			"bark_enabled": true,
			"bark_key": "barkkey",
			"bark_title": "渠道标题",
			"bark_body": "渠道正文",
			"bark_subtitle": "副标题",
			"bark_group": "日常",
			"bark_level": "critical",
			"serverchan_enabled": true,
			"serverchan_key": "sctp12345tabc",
			"serverchan_title": "SC渠道标题",
			"serverchan_body": "SC渠道正文",
			"serverchan_tags": "日常|重要",
			"serverchan_short": "简短",
			"serverchan_noip": true,
			"serverchan_channel": "weixin",
			"serverchan_openid": "openid1",
			"on_fail": true,
			"fail_title": "任务失败",
			"fail_body": "任务 {{task_name}} 失败于 {{datetime}}",
			"task_title": "通知标题",
			"task_body": "通知正文",
			"task_notify_key": "monthly_card",
			"task_notify.monthly_card": false,
			"task_notify.survey": true
		}
	}`
	config, err := ParseConfig(nodeJSON)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if !config.WebhookEnabled || config.WebhookURL != "https://example.com/hook" || config.WebhookMethod != "POST" ||
		config.WebhookHeaders != "X-A: 1" || config.WebhookBody != `{"a":1}` {
		t.Errorf("webhook parse mismatch: %+v", config)
	}
	if !config.BarkEnabled || config.BarkKey != "barkkey" ||
		config.BarkTitle != "渠道标题" || config.BarkBody != "渠道正文" ||
		config.BarkSubtitle != "副标题" || config.BarkGroup != "日常" || config.BarkLevel != "critical" {
		t.Errorf("bark parse mismatch: %+v", config)
	}
	if !config.ServerChanEnabled || config.ServerChanKey != "sctp12345tabc" || config.ServerChanTags != "日常|重要" ||
		config.ServerChanTitle != "SC渠道标题" || config.ServerChanBody != "SC渠道正文" ||
		config.ServerChanShort != "简短" || !config.ServerChanNoIP || config.ServerChanChannel != "weixin" ||
		config.ServerChanOpenID != "openid1" {
		t.Errorf("serverchan parse mismatch: %+v", config)
	}
	if !config.OnFail || config.FailTitle != "任务失败" || !strings.Contains(config.FailBody, "{{task_name}}") {
		t.Errorf("fail notify parse mismatch: %+v", config)
	}
	if config.TaskTitle != "通知标题" || config.TaskBody != "通知正文" {
		t.Errorf("task template parse mismatch: %+v", config)
	}
	if config.TaskNotifyKey != "monthly_card" {
		t.Errorf("task_notify_key mismatch: %+v", config)
	}
	if v, ok := config.TaskNotifyToggles["monthly_card"]; !ok || v {
		t.Errorf("task_notify.monthly_card toggle mismatch: %+v", config.TaskNotifyToggles)
	}
	if v, ok := config.TaskNotifyToggles["survey"]; !ok || !v {
		t.Errorf("task_notify.survey toggle mismatch: %+v", config.TaskNotifyToggles)
	}
	if !config.Enabled() {
		t.Errorf("expected enabled")
	}
}

func TestParseConfigEmpty(t *testing.T) {
	config, err := ParseConfig(`{"action": "DoNothing"}`)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if config.Enabled() {
		t.Errorf("expected disabled for empty config")
	}
}

func TestReplaceVars(t *testing.T) {
	vars := BuildVars("ExampleTask", "成功", time.Date(2026, 8, 21, 9, 30, 0, 0, time.Local), time.Date(2026, 8, 21, 9, 28, 0, 0, time.Local))
	got := ReplaceVars("任务 {{task_name}} {{task_status}}，时间 {{datetime}}，未知 {{unknown}}", vars)
	want := "任务 ExampleTask 成功，时间 " + vars["datetime"] + "，未知 {{unknown}}"
	if got != want {
		t.Errorf("ReplaceVars = %q, want %q", got, want)
	}
	if vars["time"] != "09:30:00" || vars["date"] != "2026-08-21" {
		t.Errorf("time/date mismatch: %+v", vars)
	}
	if vars["task_name"] != "ExampleTask" {
		t.Errorf("task_name mismatch: %+v", vars)
	}
	if vars["duration"] != "2m0s" {
		t.Errorf("duration mismatch: %q", vars["duration"])
	}
}

func TestParseHeaders(t *testing.T) {
	// JSON 对象
	headers := ParseHeaders(`{"Content-Type": "application/json", "Authorization": "Bearer abc"}`)
	if headers["Content-Type"] != "application/json" || headers["Authorization"] != "Bearer abc" {
		t.Errorf("JSON headers mismatch: %+v", headers)
	}
	// 旧格式（换行 / | 分隔）
	headers = ParseHeaders("X-A: 1|X-B: 2\nX-C: 3")
	if headers["X-A"] != "1" || headers["X-B"] != "2" || headers["X-C"] != "3" {
		t.Errorf("text headers mismatch: %+v", headers)
	}
	// 值含冒号：按第一个冒号切分
	headers = ParseHeaders("X-Url: https://a/b:c")
	if headers["X-Url"] != "https://a/b:c" {
		t.Errorf("colon in value mismatch: %+v", headers)
	}
	// 坏 JSON（缺尾引号）：回退文本解析，不 panic
	headers = ParseHeaders(`{"Content-Type": "application/json"`)
	if v := headers[`{"Content-Type"`]; v != `"application/json"` {
		t.Errorf("bad JSON fallback mismatch: %+v", headers)
	}
	// 全角冒号行：无法切分则跳过，不产生垃圾 header
	headers = ParseHeaders("X-A：1")
	if len(headers) != 0 {
		t.Errorf("full-width colon line should be skipped: %+v", headers)
	}
}

func TestPipeSeparated(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"日常,重要", "日常|重要"},
		{"日常, 重要", "日常|重要"},
		{"A，B、C", "A|B|C"},
		{"A，B、C、", "A|B|C"},
		{"日常 ， 重要", "日常|重要"},
		{"A,B|C", "A|B|C"},
		{"A|B", "A|B"},
		{"A,,B", "A|B"},
		{" A , B ", "A|B"},
		{"", ""},
		{"  ", ""},
	}
	for _, c := range cases {
		if got := pipeSeparated(c.raw); got != c.want {
			t.Errorf("pipeSeparated(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestShouldNotifyFailDedup(t *testing.T) {
	notifiedTaskIDs = sync.Map{}
	if !shouldNotifyFail(10) {
		t.Errorf("first occurrence of 10 should send")
	}
	if shouldNotifyFail(10) {
		t.Errorf("duplicate 10 should be deduped")
	}
	if !shouldNotifyFail(11) {
		t.Errorf("new task_id 11 should send")
	}
	if shouldNotifyFail(11) {
		t.Errorf("duplicate 11 should be deduped")
	}
	// 不同 task_id 不误伤
	if !shouldNotifyFail(12) {
		t.Errorf("new task_id 12 should send")
	}
	notifiedTaskIDs = sync.Map{}
}

func TestShouldNotifyFailConcurrent(t *testing.T) {
	notifiedTaskIDs = sync.Map{}
	const taskID = uint64(42)
	const goroutines = 20
	var wg sync.WaitGroup
	results := make(chan bool, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- shouldNotifyFail(taskID)
		}()
	}
	wg.Wait()
	close(results)
	sendCount := 0
	for r := range results {
		if r {
			sendCount++
		}
	}
	if sendCount != 1 {
		t.Errorf("expected exactly 1 sender for same task_id, got %d", sendCount)
	}
	notifiedTaskIDs = sync.Map{}
}

func TestConfigByTaskIsolation(t *testing.T) {
	configByTaskID = map[uint64]Config{}

	// 两个并行任务各自缓存不同的渠道配置，互不覆盖
	setConfigByTask(1, Config{OnFail: true, FailTitle: "A", WebhookEnabled: true, WebhookURL: "https://a", WebhookBody: "bodyA"})
	setConfigByTask(2, Config{OnFail: true, FailTitle: "B", BarkEnabled: true, BarkKey: "bkey"})

	gotA, okA := getConfigByTask(1)
	if !okA || gotA.FailTitle != "A" || !gotA.WebhookEnabled || gotA.WebhookURL != "https://a" || gotA.WebhookBody != "bodyA" || gotA.BarkEnabled {
		t.Errorf("task 1 config leaked/mixed: ok=%v %+v", okA, gotA)
	}
	gotB, okB := getConfigByTask(2)
	if !okB || gotB.FailTitle != "B" || !gotB.BarkEnabled || gotB.BarkKey != "bkey" || gotB.WebhookEnabled {
		t.Errorf("task 2 config leaked/mixed: ok=%v %+v", okB, gotB)
	}

	// 未知 task_id：未缓存 → 返回 false，绝不回退其他任务的配置
	if _, ok := getConfigByTask(999); ok {
		t.Errorf("uncached task should not fall back to other tasks' config")
	}
	configByTaskID = map[uint64]Config{}
}

func TestSendWebhook(t *testing.T) {
	var gotMethod, gotBody, gotHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Custom")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := Config{
		WebhookEnabled: true,
		WebhookURL:     server.URL,
		WebhookMethod:  "POST",
		WebhookHeaders: "X-Custom: {{title}}",
		WebhookBody:    `{"title":"{{title}}","body":"{{body}}","name":"{{task_name}}"}`,
	}
	vars := map[string]string{
		"task_name": "ExampleTask",
		"title":     "通知标题",
		"body":      "通知正文",
	}
	if !Send(config, vars) {
		t.Fatalf("Send returned false")
	}
	if gotMethod != "POST" || gotHeader != "通知标题" || gotBody != `{"title":"通知标题","body":"通知正文","name":"ExampleTask"}` {
		t.Errorf("webhook result mismatch: method=%q header=%q body=%q", gotMethod, gotHeader, gotBody)
	}
}

func TestSendWebhookError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := Config{WebhookEnabled: true, WebhookURL: server.URL, WebhookMethod: "GET"}
	if Send(config, map[string]string{}) {
		t.Errorf("Send should return false on 500")
	}
}

func TestSendNoChannel(t *testing.T) {
	config := Config{OnFail: true}
	if !Send(config, map[string]string{}) {
		t.Errorf("Send with no enabled channel should return true")
	}
}

func TestSendWebhookDisabledChannelSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := Config{WebhookEnabled: false, WebhookURL: server.URL, WebhookMethod: "GET"}
	if !Send(config, map[string]string{}) {
		t.Errorf("Send with disabled channel should return true")
	}
}

func TestServerChanEndpoint(t *testing.T) {
	cases := []struct {
		key     string
		want    string
		wantErr bool
	}{
		// ServerChan3（SC3）：sctp 前缀，uid 作子域，sendkey 在路径
		{"sctp12345tabcdef", "https://12345.push.ft07.com/send/sctp12345tabcdef.send", false},
		{"sctp42tabc", "https://42.push.ft07.com/send/sctp42tabc.send", false},
		// ServerChan Turbo：SCT 前缀
		{"SCTabc123", "https://sctapi.ftqq.com/SCTabc123.send", false},
		// 畸形 SC3：uid 非纯数字，不符合官方正则 /^sctp(\d+)t/
		{"sctp12ab34tcde", "", true},
		// 空
		{"", "", true},
		{"  ", "", true},
	}
	for _, c := range cases {
		got, err := serverChanEndpoint(c.key)
		if c.wantErr {
			if err == nil {
				t.Errorf("serverChanEndpoint(%q) expected error, got %q", c.key, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("serverChanEndpoint(%q) unexpected error: %v", c.key, err)
			continue
		}
		if got != c.want {
			t.Errorf("serverChanEndpoint(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

func TestSC3UID(t *testing.T) {
	cases := []struct {
		key  string
		want string
		ok   bool
	}{
		{"sctp12345tabcdef", "12345", true},
		{"sctp42tabc", "42", true},
		{"sctp1t", "1", true},
		{"sctp12345", "", false},      // 无 t 分隔符，不符合官方格式
		{"sctp12ab34tcde", "", false}, // uid 非纯数字
	}
	for _, c := range cases {
		got, ok := sc3UID(c.key)
		if got != c.want || ok != c.ok {
			t.Errorf("sc3UID(%q) = (%q, %v), want (%q, %v)", c.key, got, ok, c.want, c.ok)
		}
	}
}

func TestSendServerChanJSON(t *testing.T) {
	var gotPayload map[string]any
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 0}`))
	}))
	defer server.Close()

	origEndpoint := serverChanEndpoint
	serverChanEndpoint = func(key string) (string, error) { return server.URL, nil }
	defer func() { serverChanEndpoint = origEndpoint }()

	config := Config{
		ServerChanEnabled: true,
		ServerChanKey:     "sctp12345tabcdef",
		ServerChanTags:    "日常,重要", // 界面按逗号输入，发送时转为 |
		ServerChanShort:   "简短描述",
		ServerChanNoIP:    true,
		ServerChanChannel: "weixin",
		ServerChanOpenID:  "openid1",
	}
	if !Send(config, map[string]string{"task_name": "ExampleTask", "title": "标题 {{task_name}}", "body": "正文 {{task_name}}"}) {
		t.Fatalf("Send returned false")
	}
	if !strings.Contains(gotContentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotPayload["title"] != "标题 ExampleTask" {
		t.Errorf("title = %v, want channel-level override", gotPayload["title"])
	}
	if gotPayload["desp"] != "正文 ExampleTask" {
		t.Errorf("desp = %v", gotPayload["desp"])
	}
	if gotPayload["tags"] != "日常|重要" || gotPayload["short"] != "简短描述" ||
		gotPayload["channel"] != "weixin" || gotPayload["openid"] != "openid1" {
		t.Errorf("params mismatch: %v", gotPayload)
	}
	if gotPayload["noip"] != float64(1) {
		t.Errorf("noip = %v (%T), want 1", gotPayload["noip"], gotPayload["noip"])
	}
}

func TestSendChannelTitleOverride(t *testing.T) {
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 200}`))
	}))
	defer server.Close()

	orig := barkEndpoint
	barkEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { barkEndpoint = orig }()

	// 1. 渠道配置了标题/正文 → 渠道优先（高于通知项 vars 内容）
	config := Config{BarkEnabled: true, BarkKey: "key", BarkTitle: "渠道标题", BarkBody: "渠道正文"}
	if !Send(config, map[string]string{"title": "通知标题", "body": "通知正文"}) {
		t.Fatalf("Send returned false")
	}
	if gotPayload["title"] != "渠道标题" || gotPayload["body"] != "渠道正文" {
		t.Errorf("bark channel config should take priority: %v", gotPayload)
	}

	// 2. 渠道模板含 {{title}}/{{body}} → 复用通知项预填内容
	config = Config{BarkEnabled: true, BarkKey: "key", BarkTitle: "【{{title}}】", BarkBody: "-{{body}}-"}
	if !Send(config, map[string]string{"title": "通知标题", "body": "通知正文"}) {
		t.Fatalf("Send returned false")
	}
	if gotPayload["title"] != "【通知标题】" || gotPayload["body"] != "-通知正文-" {
		t.Errorf("bark should reuse item title/body via variables: %v", gotPayload)
	}

	// 3. 渠道留空 → 回退通知项（vars）内容
	config = Config{BarkEnabled: true, BarkKey: "key"}
	if !Send(config, map[string]string{"title": "通知标题", "body": "通知正文"}) {
		t.Fatalf("Send returned false")
	}
	if gotPayload["title"] != "通知标题" || gotPayload["body"] != "通知正文" {
		t.Errorf("bark should fall back to vars title/body: %v", gotPayload)
	}

	// 4. 渠道与通知项都空 → 默认标题
	if !Send(config, map[string]string{}) {
		t.Fatalf("Send returned false")
	}
	if gotPayload["title"] != i18n.T("notify.default_title") {
		t.Errorf("bark should fall back to default title: %v", gotPayload)
	}
}

func TestSendBarkJSON(t *testing.T) {
	var gotPayload map[string]any
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 200}`))
	}))
	defer server.Close()

	orig := barkEndpoint
	barkEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { barkEndpoint = orig }()

	config := Config{
		BarkEnabled:   true,
		BarkKey:       "key",
		BarkSubtitle:  "副标题",
		BarkGroup:     "日常",
		BarkLevel:     "critical",
		BarkSound:     "minuet",
		BarkIcon:      "https://example.com/icon.png",
		BarkImage:     "https://example.com/img.png",
		BarkURL:       "https://example.com",
		BarkBadge:     "3",
		BarkMarkdown:  "# 标题",
		BarkCopy:      "复制内容",
		BarkIsArchive: "1",
		BarkTTL:       "86400",
		BarkCall:      "1",
	}
	if !Send(config, map[string]string{"task_name": "ExampleTask", "title": "标题 {{task_name}}", "body": "正文 {{task_name}}"}) {
		t.Fatalf("Send returned false")
	}
	if !strings.Contains(gotContentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotPayload["title"] != "标题 ExampleTask" || gotPayload["body"] != "正文 ExampleTask" {
		t.Errorf("title/body mismatch: %v", gotPayload)
	}
	if gotPayload["subtitle"] != "副标题" || gotPayload["group"] != "日常" || gotPayload["level"] != "critical" ||
		gotPayload["sound"] != "minuet" || gotPayload["icon"] != "https://example.com/icon.png" ||
		gotPayload["image"] != "https://example.com/img.png" || gotPayload["url"] != "https://example.com" ||
		gotPayload["markdown"] != "# 标题" || gotPayload["copy"] != "复制内容" ||
		gotPayload["isArchive"] != "1" || gotPayload["ttl"] != "86400" || gotPayload["call"] != "1" {
		t.Errorf("bark params mismatch: %v", gotPayload)
	}
	if gotPayload["badge"] != float64(3) {
		t.Errorf("badge = %v (%T), want 3", gotPayload["badge"], gotPayload["badge"])
	}
}

func TestSendBarkBatch(t *testing.T) {
	var gotPayload map[string]any
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 200}`))
	}))
	defer server.Close()

	// 只注入批量端点：若走了单设备端点（barkEndpoint 未注入）会请求真实 api.day.app 而失败
	orig := barkBatchEndpoint
	barkBatchEndpoint = func() string { return server.URL }
	defer func() { barkBatchEndpoint = orig }()

	config := Config{
		BarkEnabled:    true,
		BarkKey:        "single-key",
		BarkDeviceKeys: "key1, key2\nkey3",
	}
	if !Send(config, map[string]string{}) {
		t.Fatalf("Send returned false")
	}
	if gotPath != "/" {
		t.Errorf("request path = %q, want / (batch endpoint)", gotPath)
	}
	keys, ok := gotPayload["device_keys"].([]any)
	if !ok {
		t.Fatalf("device_keys = %v (%T), want array", gotPayload["device_keys"], gotPayload["device_keys"])
	}
	if len(keys) != 3 || keys[0] != "key1" || keys[1] != "key2" || keys[2] != "key3" {
		t.Errorf("device_keys = %v, want [key1 key2 key3]", keys)
	}
}

func TestParseDeviceKeys(t *testing.T) {
	// 空串 → nil
	if got := parseDeviceKeys("", map[string]string{}); got != nil {
		t.Errorf("empty = %v, want nil", got)
	}
	// 逗号/换行分隔 + 去空白 + 跳过空段
	if got := parseDeviceKeys(" key1 , key2\n\nkey3 ", map[string]string{}); len(got) != 3 || got[0] != "key1" || got[1] != "key2" || got[2] != "key3" {
		t.Errorf("parsed = %v, want [key1 key2 key3]", got)
	}
	// 支持模板变量
	if got := parseDeviceKeys("{{a}},key2", map[string]string{"a": "K1"}); len(got) != 2 || got[0] != "K1" || got[1] != "key2" {
		t.Errorf("vars parsed = %v, want [K1 key2]", got)
	}
}

func TestSendServerChanHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	orig := serverChanEndpoint
	serverChanEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { serverChanEndpoint = orig }()

	config := Config{ServerChanEnabled: true, ServerChanKey: "key"}
	if Send(config, map[string]string{}) {
		t.Errorf("Send should return false on 500")
	}
}

func TestSendServerChanBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 40000, "message": "bad key"}`))
	}))
	defer server.Close()

	orig := serverChanEndpoint
	serverChanEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { serverChanEndpoint = orig }()

	config := Config{ServerChanEnabled: true, ServerChanKey: "key"}
	if Send(config, map[string]string{}) {
		t.Errorf("Send should return false on non-zero api code")
	}
}

func TestSendServerChanBadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	orig := serverChanEndpoint
	serverChanEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { serverChanEndpoint = orig }()

	config := Config{ServerChanEnabled: true, ServerChanKey: "key"}
	if Send(config, map[string]string{}) {
		t.Errorf("Send should return false on non-JSON response body")
	}
}

func TestSendBarkBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 400, "message": "device key not found"}`))
	}))
	defer server.Close()

	orig := barkEndpoint
	barkEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { barkEndpoint = orig }()

	config := Config{BarkEnabled: true, BarkKey: "key"}
	if Send(config, map[string]string{}) {
		t.Errorf("Send should return false on Bark code != 200")
	}
}

func waitForRequests(t *testing.T, counter *atomic.Int32, want int32) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if counter.Load() == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %d requests, got %d", want, counter.Load())
}

// waitForNoMoreRequests 等待 settled 窗口（连续 100ms 无新请求），断言计数不变。
func waitForNoMoreRequests(t *testing.T, counter *atomic.Int32, want int32) {
	t.Helper()
	waitForRequests(t, counter, want)
	// 再等 100ms，确认无新请求到达
	time.Sleep(100 * time.Millisecond)
	if got := counter.Load(); got != want {
		t.Errorf("expected no more requests after settled, want %d got %d", want, got)
	}
}

func TestSinkFailedNotify(t *testing.T) {
	configByTaskID = map[uint64]Config{}
	notifiedTaskIDs = sync.Map{}
	defer func() {
		configByTaskID = map[uint64]Config{}
		notifiedTaskIDs = sync.Map{}
	}()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sink := &Sink{}

	// 未缓存配置的任务失败（节点事件前失败）：不发送，绝不使用其他任务的配置
	sink.OnTaskerTask(nil, maa.EventStatusFailed, maa.TaskerTaskDetail{TaskID: 99, Entry: "TaskX"})
	waitForNoMoreRequests(t, &requests, 0)

	// on_fail 开启但无渠道：无效配置，不发送
	setConfigByTask(1, Config{OnFail: true})
	sink.OnTaskerTask(nil, maa.EventStatusFailed, maa.TaskerTaskDetail{TaskID: 1, Entry: "TaskA"})
	waitForNoMoreRequests(t, &requests, 0)

	// 渠道开启但 on_fail 关闭：不发送（主动关闭属正常）
	setConfigByTask(2, Config{WebhookEnabled: true, WebhookURL: server.URL, WebhookMethod: "GET"})
	sink.OnTaskerTask(nil, maa.EventStatusFailed, maa.TaskerTaskDetail{TaskID: 2, Entry: "TaskB"})
	waitForNoMoreRequests(t, &requests, 0)

	// on_fail + 渠道：发送失败通知
	setConfigByTask(3, Config{OnFail: true, FailTitle: "失败啦", WebhookEnabled: true, WebhookURL: server.URL, WebhookMethod: "GET"})
	sink.OnTaskerTask(nil, maa.EventStatusFailed, maa.TaskerTaskDetail{TaskID: 3, Entry: "TaskC"})
	waitForRequests(t, &requests, 1)

	// 同一 taskID 重复广播：标记还在，不会发送第二次
	sink.OnTaskerTask(nil, maa.EventStatusFailed, maa.TaskerTaskDetail{TaskID: 3, Entry: "TaskC"})
	waitForNoMoreRequests(t, &requests, 1)
	// 不同 taskID 不受影响
	setConfigByTask(4, Config{OnFail: true, FailTitle: "另一个", WebhookEnabled: true, WebhookURL: server.URL, WebhookMethod: "GET"})
	sink.OnTaskerTask(nil, maa.EventStatusFailed, maa.TaskerTaskDetail{TaskID: 4, Entry: "TaskD"})
	waitForRequests(t, &requests, 2)
}

func TestSanitizeError(t *testing.T) {
	if sanitizeError(nil) != nil {
		t.Errorf("nil should stay nil")
	}
	err := fmt.Errorf(`Post "https://api.day.app/SECRETKEY": dial tcp: connection refused`)
	got := sanitizeError(err).Error()
	if strings.Contains(got, "SECRETKEY") || strings.Contains(got, "api.day.app") {
		t.Errorf("sanitized error still contains URL/key: %q", got)
	}
	if !strings.Contains(got, "<redacted url>") {
		t.Errorf("sanitized error should mark redaction: %q", got)
	}
	// 不含 URL 的错误原样保留
	plain := fmt.Errorf("unexpected status code: 500")
	if sanitizeError(plain).Error() != "unexpected status code: 500" {
		t.Errorf("plain error should be unchanged: %q", sanitizeError(plain).Error())
	}
}

func TestMergeConfig(t *testing.T) {
	global := Config{
		WebhookEnabled: true, BarkEnabled: true, ServerChanEnabled: true,
		TaskTitle: "全局标题", TaskBody: "全局正文",
		TaskNotifyToggles: map[string]bool{"monthly_card": true},
	}
	local := Config{TaskTitle: "本地标题", TaskNotifyKey: "monthly_card"}

	merged := mergeConfig(global, local)
	// 渠道字段以全局为准
	if !merged.WebhookEnabled || !merged.BarkEnabled || !merged.ServerChanEnabled {
		t.Errorf("channel fields should come from global: %+v", merged)
	}
	// 内容字段调用节点优先
	if merged.TaskTitle != "本地标题" {
		t.Errorf("content should prefer local: %+v", merged)
	}
	// 通知项 ID 本地优先
	if merged.TaskNotifyKey != "monthly_card" {
		t.Errorf("task_notify_key should prefer local: %+v", merged)
	}
	// 本地未写的字段回退全局
	if merged.TaskBody != "全局正文" {
		t.Errorf("content should fall back to global: %+v", merged)
	}

	// 本地内容全空：不覆盖全局
	if merged2 := mergeConfig(global, Config{}); merged2.TaskTitle != "全局标题" {
		t.Errorf("empty local should not override global: %+v", merged2)
	}
}

func TestTaskNotifySkipped(t *testing.T) {
	// 未声明 task_notify_key：不判断，发送
	if taskNotifySkipped(Config{TaskNotifyToggles: map[string]bool{"monthly_card": false}}) {
		t.Errorf("no key should not skip")
	}
	// 声明了 key 但设置页未配置：默认启用，发送
	if taskNotifySkipped(Config{TaskNotifyKey: "monthly_card"}) {
		t.Errorf("unconfigured item should default to enabled")
	}
	if taskNotifySkipped(Config{TaskNotifyKey: "monthly_card", TaskNotifyToggles: map[string]bool{}}) {
		t.Errorf("empty toggles should default to enabled")
	}
	// 设置页显式启用：发送
	if taskNotifySkipped(Config{TaskNotifyKey: "monthly_card", TaskNotifyToggles: map[string]bool{"monthly_card": true}}) {
		t.Errorf("enabled item should not skip")
	}
	// 设置页显式关闭：跳过
	if !taskNotifySkipped(Config{TaskNotifyKey: "monthly_card", TaskNotifyToggles: map[string]bool{"monthly_card": false}}) {
		t.Errorf("disabled item should skip")
	}
	// 其他通知项开关不影响本通知项
	if taskNotifySkipped(Config{TaskNotifyKey: "monthly_card", TaskNotifyToggles: map[string]bool{"survey": false}}) {
		t.Errorf("other item toggle should not affect this item")
	}
}

func TestResolveNotifyText(t *testing.T) {
	i18n.Init() // 幂等；首次以 zh_cn 初始化，使 notify.default_title 等翻译可用
	vars := map[string]string{"task_name": "T"}
	// 普通文本 → 原样 + 变量替换
	if got := resolveNotifyText("任务 {{task_name}}", vars); got != "任务 T" {
		t.Errorf("plain text = %q, want 任务 T", got)
	}
	// "$" 开头且查到翻译 → 用翻译 + 变量替换
	if got := resolveNotifyText("$notify.default_title", vars); got != "MaaEnd 通知" {
		t.Errorf("i18n found = %q, want MaaEnd 通知", got)
	}
	// "$" 开头但查不到翻译 → 显示去掉 $ 的 key 本身（与 i18n.T 回退一致）
	if got := resolveNotifyText("$notify.no_such_key_xyz", vars); got != "notify.no_such_key_xyz" {
		t.Errorf("i18n missing = %q, want notify.no_such_key_xyz", got)
	}
	// 翻译值本身含模板变量 → 变量替换（notify.default_body 含 {{datetime}}，构造缺失时回退 key）
	if got := resolveNotifyText("$notify.no_such_key_{{task_name}}", vars); got != "notify.no_such_key_T" {
		t.Errorf("i18n missing with vars = %q, want notify.no_such_key_T", got)
	}
	// 空串 → 空串（由 Send 的 prefill 兜底默认标题）
	if got := resolveNotifyText("", vars); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
	// 单独一个 $ → key 为空串，回退空串
	if got := resolveNotifyText("$", vars); got != "" {
		t.Errorf("bare dollar = %q, want empty", got)
	}
}

func TestResolveActionTaskName(t *testing.T) {
	i18n.Init() // 幂等；使 task.*.label 翻译可用
	getEntryOK := func(int64) string { return "AccountSwitchStart" }
	getEntryEmpty := func(int64) string { return "" } // GetTaskDetail 取不到 entry（node_ids 为空）

	// 反查成功：用入口名解析显示名
	if got := resolveActionTaskName(200000001, "NotifySend", getEntryOK); got != "🔑自动切换账号" {
		t.Errorf("entry resolved = %q, want 🔑自动切换账号", got)
	}
	// 反查取不到 entry：回退当前节点名解析（NotifyTask 节点名即入口名）
	if got := resolveActionTaskName(200000001, "NotifySend", getEntryEmpty); got != "🔔发送通知" {
		t.Errorf("current-task-name fallback = %q, want 🔔发送通知", got)
	}
	// 反查取不到且节点名不在任务映射：原样返回
	if got := resolveActionTaskName(200000001, "SomeCustomNode", getEntryEmpty); got != "SomeCustomNode" {
		t.Errorf("unknown node fallback = %q, want SomeCustomNode", got)
	}
	// taskID 无效：直接用当前节点名解析
	if got := resolveActionTaskName(0, "NotifySend", getEntryOK); got != "🔔发送通知" {
		t.Errorf("no task id = %q, want 🔔发送通知", got)
	}
}

func TestParseConfigAllowTaskNotify(t *testing.T) {
	// 未配置 → nil（默认允许）
	config, err := ParseConfig(`{"attach": {"webhook_enabled": true}}`)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if config.AllowTaskNotify != nil {
		t.Errorf("unset allow_task_notify should be nil, got %v", *config.AllowTaskNotify)
	}
	// 显式 false → 关闭自定义通知
	config, err = ParseConfig(`{"attach": {"allow_task_notify": false}}`)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if config.AllowTaskNotify == nil || *config.AllowTaskNotify {
		t.Errorf("allow_task_notify=false should parse to false, got %v", config.AllowTaskNotify)
	}
	// 显式 true → 允许
	config, err = ParseConfig(`{"attach": {"allow_task_notify": true}}`)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if config.AllowTaskNotify == nil || !*config.AllowTaskNotify {
		t.Errorf("allow_task_notify=true should parse to true, got %v", config.AllowTaskNotify)
	}
}
