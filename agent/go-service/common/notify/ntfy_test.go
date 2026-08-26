package notify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendNtfy(t *testing.T) {
	var gotMethod, gotBody, gotTitle, gotPriority, gotTags, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotTitle = r.Header.Get("Title")
		gotPriority = r.Header.Get("Priority")
		gotTags = r.Header.Get("Tags")
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	orig := ntfyEndpoint
	ntfyEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { ntfyEndpoint = orig }()

	runtime := testRuntime(map[string]any{
		"ntfy_enabled":  true,
		"ntfy_url":      "https://ntfy.sh/mytopic",
		"ntfy_title":    "通知 {{task_name}}",
		"ntfy_body":     "正文 {{task_name}}",
		"ntfy_priority": "high",
		"ntfy_tags":     "warning,{{task_name}}",
	})
	if !Send(runtime, map[string]string{"task_name": "ExampleTask", "title": "标题 {{task_name}}", "body": "正文 {{task_name}}"}) {
		t.Fatalf("Send returned false")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotTitle != "通知 ExampleTask" {
		t.Errorf("Title = %q, want 通知 ExampleTask", gotTitle)
	}
	if gotBody != "正文 ExampleTask" {
		t.Errorf("body = %q, want 正文 ExampleTask", gotBody)
	}
	if gotPriority != "high" {
		t.Errorf("Priority = %q, want high", gotPriority)
	}
	if gotTags != "warning,ExampleTask" {
		t.Errorf("Tags = %q, want warning,ExampleTask", gotTags)
	}
	if gotAuth != "" {
		t.Errorf("Authorization should be empty without token, got %q", gotAuth)
	}
}

func TestSendNtfyAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	orig := ntfyEndpoint
	ntfyEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { ntfyEndpoint = orig }()

	runtime := testRuntime(map[string]any{
		"ntfy_enabled": true,
		"ntfy_url":     "https://ntfy.sh/mytopic",
		"ntfy_body":    "正文",
		"ntfy_token":   "tk_abc123",
	})
	if !Send(runtime, map[string]string{}) {
		t.Fatalf("Send returned false")
	}
	if gotAuth != "Bearer tk_abc123" {
		t.Errorf("Authorization = %q, want Bearer tk_abc123", gotAuth)
	}
}

func TestSendNtfyErrors(t *testing.T) {
	// url 为空 → 报错
	ch := ntfyChannel{cfg: ntfyConfig{Enabled: true, Body: "正文"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("empty url should error")
	}
	// 非法协议 → 报错
	ch = ntfyChannel{cfg: ntfyConfig{Enabled: true, URL: "ftp://x", Body: "正文"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("invalid url should error")
	}
	// 空内容 → 报错
	orig := ntfyEndpoint
	ntfyEndpoint = func(string) (string, error) { return "https://example.invalid", nil }
	defer func() { ntfyEndpoint = orig }()
	ch = ntfyChannel{cfg: ntfyConfig{Enabled: true, URL: "https://ntfy.sh/mytopic"}}
	if err := ch.Send(testCtx()); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty body should error, got %v", err)
	}

	// HTTP 401（私有 topic 未认证）
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	ntfyEndpoint = func(string) (string, error) { return server.URL, nil }
	ch = ntfyChannel{cfg: ntfyConfig{Enabled: true, URL: "https://ntfy.sh/mytopic", Body: "正文"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("HTTP 401 should error")
	}
}

func TestNtfyEndpointDefault(t *testing.T) {
	// 合法 URL（含 topic）→ 原样返回
	got, err := ntfyEndpointDefault("  https://ntfy.sh/mytopic  ")
	if err != nil || got != "https://ntfy.sh/mytopic" {
		t.Errorf("valid = %q, err=%v", got, err)
	}
	// 缺省协议 → 自动补 https://
	got, err = ntfyEndpointDefault("ntfy.sh/mytopic")
	if err != nil || got != "https://ntfy.sh/mytopic" {
		t.Errorf("no-scheme = %q, err=%v; want https:// prefix", got, err)
	}
	// 空 → 报错
	if _, err := ntfyEndpointDefault(""); err == nil {
		t.Errorf("empty should error")
	}
	// 非法协议 → 报错
	if _, err := ntfyEndpointDefault("ftp://x"); err == nil {
		t.Errorf("bad scheme should error")
	}
	if _, err := ntfyEndpointDefault("not a url"); err == nil {
		t.Errorf("bad url should error")
	}
}
