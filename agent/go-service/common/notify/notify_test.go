package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
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
			"send_task_webhook": true,
			"send_task_bark": false,
			"send_task_serverchan": true
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
	if !config.SendTaskWebhook || config.SendTaskBark || !config.SendTaskServerChan {
		t.Errorf("task toggle parse mismatch: %+v", config)
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
	vars := BuildVars("DijiangRewards", "成功", time.Date(2026, 8, 21, 9, 30, 0, 0, time.Local))
	got := ReplaceVars("任务 {{task_name}} {{task_status}}，时间 {{datetime}}，未知 {{unknown}}", vars)
	want := "任务 DijiangRewards 成功，时间 " + vars["datetime"] + "，未知 {{unknown}}"
	if got != want {
		t.Errorf("ReplaceVars = %q, want %q", got, want)
	}
	if vars["time"] != "09:30:00" || vars["date"] != "2026-08-21" {
		t.Errorf("time/date mismatch: %+v", vars)
	}
	// 任务级：name/task_name 均为任务名
	if vars["name"] != "DijiangRewards" || vars["task_name"] != "DijiangRewards" {
		t.Errorf("name/task_name mismatch: %+v", vars)
	}
	if ReplaceVars("{{name}}", vars) != "DijiangRewards" {
		t.Errorf("{{name}} should resolve to task name")
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
}

func TestPipeSeparated(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"日常,重要", "日常|重要"},
		{"日常, 重要", "日常|重要"},
		{"A，B、C", "A|B|C"},
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

func TestApplyTaskToggle(t *testing.T) {
	// 全默认 true → 保持设置启用
	c := Config{WebhookEnabled: true, BarkEnabled: true, ServerChanEnabled: true, SendTaskWebhook: true, SendTaskBark: true, SendTaskServerChan: true}
	applyTaskToggle(&c)
	if !c.Enabled() {
		t.Errorf("task toggles all true should keep channels: %+v", c)
	}

	// 关掉 webhook（其余任务开关默认 true=跟随设置）→ 仅 webhook 关闭
	c = Config{WebhookEnabled: true, BarkEnabled: true, ServerChanEnabled: true, SendTaskWebhook: false, SendTaskBark: true, SendTaskServerChan: true}
	applyTaskToggle(&c)
	if c.WebhookEnabled || !c.BarkEnabled || !c.ServerChanEnabled {
		t.Errorf("task webhook off should disable only webhook: %+v", c)
	}

	// SendTask* 全关（零值）→ 渠道全关，不发送
	c = Config{WebhookEnabled: true, BarkEnabled: true, ServerChanEnabled: true}
	applyTaskToggle(&c)
	if c.Enabled() {
		t.Errorf("all false toggles should disable: %+v", c)
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
	lastConfig = Config{}

	// 两个并行任务各自缓存不同的渠道配置，互不覆盖
	setConfigByTask(1, Config{OnFail: true, FailTitle: "A", WebhookEnabled: true, WebhookURL: "https://a", WebhookBody: "bodyA"})
	setConfigByTask(2, Config{OnFail: true, FailTitle: "B", BarkEnabled: true, BarkKey: "bkey"})

	gotA := getConfigByTask(1)
	if gotA.FailTitle != "A" || !gotA.WebhookEnabled || gotA.WebhookURL != "https://a" || gotA.WebhookBody != "bodyA" || gotA.BarkEnabled {
		t.Errorf("task 1 config leaked/mixed: %+v", gotA)
	}
	gotB := getConfigByTask(2)
	if gotB.FailTitle != "B" || !gotB.BarkEnabled || gotB.BarkKey != "bkey" || gotB.WebhookEnabled {
		t.Errorf("task 2 config leaked/mixed: %+v", gotB)
	}

	// 未知 task_id：回退全局最近配置（应为最后一次写入的 task 2 配置）
	lastConfig = Config{OnFail: true, FailTitle: "L"}
	gotFallback := getConfigByTask(999)
	if gotFallback.FailTitle != "L" {
		t.Errorf("fallback should use lastConfig: %+v", gotFallback)
	}
	configByTaskID = map[uint64]Config{}
	lastConfig = Config{}
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
		"task_name": "DijiangRewards",
		"title":     "通知标题",
		"body":      "通知正文",
	}
	if !Send(config, vars) {
		t.Fatalf("Send returned false")
	}
	if gotMethod != "POST" || gotHeader != "通知标题" || gotBody != `{"title":"通知标题","body":"通知正文","name":"DijiangRewards"}` {
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
		// ServerChan3（SC3）：sctp 前缀，sendkey 整体作子域（官方 SDK 格式）
		{"sctp12345tabcdef", "https://sctp12345tabcdef.push.ft07.com/send", false},
		{"sctp42tabc", "https://sctp42tabc.push.ft07.com/send", false},
		// ServerChan Turbo：SCT 前缀
		{"SCTabc123", "https://sctapi.ftqq.com/SCTabc123.send", false},
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

func TestSendServerChanJSON(t *testing.T) {
	var gotPayload map[string]any
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 0, "message": "success"}`))
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
	if !Send(config, map[string]string{"task_name": "DijiangRewards", "title": "标题 {{task_name}}", "body": "正文 {{task_name}}"}) {
		t.Fatalf("Send returned false")
	}
	if !strings.Contains(gotContentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotPayload["title"] != "标题 DijiangRewards" {
		t.Errorf("title = %v, want channel-level override", gotPayload["title"])
	}
	if gotPayload["desp"] != "正文 DijiangRewards" {
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
	if !Send(config, map[string]string{"task_name": "DijiangRewards", "title": "标题 {{task_name}}", "body": "正文 {{task_name}}"}) {
		t.Fatalf("Send returned false")
	}
	if !strings.Contains(gotContentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotPayload["title"] != "标题 DijiangRewards" || gotPayload["body"] != "正文 DijiangRewards" {
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
