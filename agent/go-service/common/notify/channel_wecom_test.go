package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendWeCom(t *testing.T) {
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode": 0, "errmsg": "ok"}`))
	}))
	defer server.Close()

	orig := wecomEndpoint
	wecomEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { wecomEndpoint = orig }()

	runtime := testRuntime(map[string]any{
		"channel_wecom_enabled":     true,
		"channel_wecom_webhook_url": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=KEY",
		"channel_wecom_title":       "通知 {{task_name}}",
		"channel_wecom_body":        "正文 {{task_name}}",
	})
	if !Send(runtime, map[string]string{"task_name": "ExampleTask", "title": "标题 {{task_name}}", "body": "正文 {{task_name}}"}) {
		t.Fatalf("Send returned false")
	}
	if gotPayload["msgtype"] != "text" {
		t.Errorf("msgtype = %v, want text (default)", gotPayload["msgtype"])
	}
	textObj, ok := gotPayload["text"].(map[string]any)
	if !ok {
		t.Fatalf("text = %v (%T), want object", gotPayload["text"], gotPayload["text"])
	}
	if textObj["content"] != "通知 ExampleTask\n\n正文 ExampleTask" {
		t.Errorf("content = %q, want 通知 ExampleTask\\n\\n正文 ExampleTask", textObj["content"])
	}
}

func TestSendWeComMarkdown(t *testing.T) {
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode": 0, "errmsg": "ok"}`))
	}))
	defer server.Close()

	orig := wecomEndpoint
	wecomEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { wecomEndpoint = orig }()

	runtime := testRuntime(map[string]any{
		"channel_wecom_enabled":     true,
		"channel_wecom_webhook_url": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=KEY",
		"channel_wecom_msgtype":     "markdown",
		"channel_wecom_title":       "标题",
	})
	if !Send(runtime, map[string]string{"title": "标题", "body": "正文"}) {
		t.Fatalf("Send returned false")
	}
	if gotPayload["msgtype"] != "markdown" {
		t.Errorf("msgtype = %v, want markdown", gotPayload["msgtype"])
	}
	if _, ok := gotPayload["markdown"]; !ok {
		t.Errorf("markdown field missing: %v", gotPayload)
	}
	if _, ok := gotPayload["text"]; ok {
		t.Errorf("text field should not be present for markdown: %v", gotPayload)
	}
}

func TestSendWeComErrors(t *testing.T) {
	// webhook url 为空 → 报错
	ch := wecomChannel{cfg: wecomConfig{Enabled: true, Title: "标题"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("empty webhook url should error")
	}
	// 非法协议 → 报错
	ch = wecomChannel{cfg: wecomConfig{Enabled: true, WebhookURL: "ftp://x", Title: "标题"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("invalid webhook url should error")
	}
	// 空内容 → 报错
	orig := wecomEndpoint
	wecomEndpoint = func(string) (string, error) { return "https://example.invalid", nil }
	defer func() { wecomEndpoint = orig }()
	ch = wecomChannel{cfg: wecomConfig{Enabled: true, WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=K"}}
	if err := ch.Send(testCtx()); err == nil || !strings.Contains(err.Error(), "content") {
		t.Errorf("empty content should error, got %v", err)
	}

	// errcode != 0 的业务错误（HTTP 仍 200）
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode": 93000, "errmsg": "invalid webhook url"}`))
	}))
	defer server.Close()
	wecomEndpoint = func(string) (string, error) { return server.URL, nil }
	ch = wecomChannel{cfg: wecomConfig{Enabled: true, WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=K", Title: "标题"}}
	if err := ch.Send(testCtx()); err == nil || !strings.Contains(err.Error(), "invalid webhook url") {
		t.Errorf("errcode!=0 should error with errmsg, got %v", err)
	}

	// HTTP 500
	server500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server500.Close()
	wecomEndpoint = func(string) (string, error) { return server500.URL, nil }
	ch = wecomChannel{cfg: wecomConfig{Enabled: true, WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=K", Title: "标题"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("HTTP 500 should error")
	}
}

func TestWeComEndpointDefault(t *testing.T) {
	// 合法 URL → 原样返回（含 key query）
	got, err := wecomEndpointDefault("  https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc  ")
	if err != nil || got != "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc" {
		t.Errorf("valid = %q, err=%v", got, err)
	}
	// 缺省协议 → 自动补 https://
	got, err = wecomEndpointDefault("qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc")
	if err != nil || got != "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc" {
		t.Errorf("no-scheme = %q, err=%v; want https:// prefix", got, err)
	}
	// 空 → 报错
	if _, err := wecomEndpointDefault(""); err == nil {
		t.Errorf("empty should error")
	}
	// 非法协议 → 报错
	if _, err := wecomEndpointDefault("ftp://x"); err == nil {
		t.Errorf("bad scheme should error")
	}
}
