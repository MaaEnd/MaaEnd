package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendGotify(t *testing.T) {
	var gotMethod, gotKey string
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotKey = r.Header.Get("X-Gotify-Key")
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	orig := gotifyEndpoint
	gotifyEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { gotifyEndpoint = orig }()

	runtime := testRuntime(map[string]any{
		"channel_gotify_enabled":  true,
		"channel_gotify_url":      "https://push.example.de",
		"channel_gotify_token":    "apptoken123",
		"channel_gotify_title":    "通知 {{task_name}}",
		"channel_gotify_body":     "正文 {{task_name}}",
		"channel_gotify_priority": "5",
	})
	if !Send(runtime, map[string]string{"task_name": "ExampleTask", "title": "标题 {{task_name}}", "body": "正文 {{task_name}}"}) {
		t.Fatalf("Send returned false")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotKey != "apptoken123" {
		t.Errorf("X-Gotify-Key = %q, want apptoken123", gotKey)
	}
	if gotPayload["message"] != "正文 ExampleTask" {
		t.Errorf("message = %v, want 正文 ExampleTask", gotPayload["message"])
	}
	if gotPayload["title"] != "通知 ExampleTask" {
		t.Errorf("title = %v, want 通知 ExampleTask", gotPayload["title"])
	}
	if gotPayload["priority"] != float64(5) {
		t.Errorf("priority = %v, want 5", gotPayload["priority"])
	}
}

func TestSendGotifyMarkdown(t *testing.T) {
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	orig := gotifyEndpoint
	gotifyEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { gotifyEndpoint = orig }()

	// 开启 → 携带 extras contentType
	runtime := testRuntime(map[string]any{
		"channel_gotify_enabled":  true,
		"channel_gotify_url":      "https://push.example.de",
		"channel_gotify_body":     "正文",
		"channel_gotify_markdown": true,
	})
	if !Send(runtime, map[string]string{}) {
		t.Fatalf("Send returned false")
	}
	extras, ok := gotPayload["extras"].(map[string]any)
	if !ok {
		t.Fatalf("extras missing or wrong type: %v", gotPayload["extras"])
	}
	display, ok := extras["client::display"].(map[string]any)
	if !ok || display["contentType"] != "text/markdown" {
		t.Errorf("extras = %v, want client::display.contentType=text/markdown", extras)
	}
}

func TestSendGotifyMinimal(t *testing.T) {
	// 仅 message（body），priority 未配置应省略（用 application 默认）
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	orig := gotifyEndpoint
	gotifyEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { gotifyEndpoint = orig }()

	runtime := testRuntime(map[string]any{
		"channel_gotify_enabled": true,
		"channel_gotify_url":     "https://push.example.de",
		"channel_gotify_body":    "正文",
	})
	if !Send(runtime, map[string]string{}) {
		t.Fatalf("Send returned false")
	}
	if gotPayload["message"] != "正文" {
		t.Errorf("message = %v, want 正文", gotPayload["message"])
	}
	if _, ok := gotPayload["priority"]; ok {
		t.Errorf("priority should be omitted, got %v", gotPayload["priority"])
	}
}

func TestSendGotifyErrors(t *testing.T) {
	// url 为空 → 报错
	ch := gotifyChannel{cfg: gotifyConfig{Enabled: true, Body: "正文"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("empty url should error")
	}
	// 非法协议 → 报错
	ch = gotifyChannel{cfg: gotifyConfig{Enabled: true, URL: "ftp://x", Body: "正文"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("invalid url should error")
	}
	// 空内容 → 报错
	orig := gotifyEndpoint
	gotifyEndpoint = func(string) (string, error) { return "https://example.invalid", nil }
	defer func() { gotifyEndpoint = orig }()
	ch = gotifyChannel{cfg: gotifyConfig{Enabled: true, URL: "https://push.example.de"}}
	if err := ch.Send(testCtx()); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty body should error, got %v", err)
	}

	// HTTP 401（token 无效）
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	gotifyEndpoint = func(string) (string, error) { return server.URL, nil }
	ch = gotifyChannel{cfg: gotifyConfig{Enabled: true, URL: "https://push.example.de", Body: "正文"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("HTTP 401 should error")
	}
}

func TestGotifyEndpointDefault(t *testing.T) {
	// 合法 URL → 拼 /message
	got, err := gotifyEndpointDefault("  https://push.example.de  ")
	if err != nil || got != "https://push.example.de/message" {
		t.Errorf("valid = %q, err=%v", got, err)
	}
	// 缺省协议 → 自动补 https://
	got, err = gotifyEndpointDefault("push.example.de")
	if err != nil || got != "https://push.example.de/message" {
		t.Errorf("no-scheme = %q, err=%v; want https:// prefix", got, err)
	}
	// 尾斜杠 → 去掉再拼
	got, err = gotifyEndpointDefault("https://push.example.de/")
	if err != nil || got != "https://push.example.de/message" {
		t.Errorf("trailing slash = %q, err=%v", got, err)
	}
	// 子路径 → 保留
	got, err = gotifyEndpointDefault("https://example.com/gotify")
	if err != nil || got != "https://example.com/gotify/message" {
		t.Errorf("subpath = %q, err=%v", got, err)
	}
	// 空 → 报错
	if _, err := gotifyEndpointDefault(""); err == nil {
		t.Errorf("empty should error")
	}
	// 非法协议 → 报错
	if _, err := gotifyEndpointDefault("ftp://x"); err == nil {
		t.Errorf("bad scheme should error")
	}
}
