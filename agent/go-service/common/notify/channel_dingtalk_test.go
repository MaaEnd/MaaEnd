package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendDingTalk(t *testing.T) {
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode": 0, "errmsg": "ok"}`))
	}))
	defer server.Close()

	orig := dingtalkEndpoint
	dingtalkEndpoint = func(string, string) (string, error) { return server.URL, nil }
	defer func() { dingtalkEndpoint = orig }()

	runtime := testRuntime(map[string]any{
		"channel_dingtalk_enabled": true,
		"channel_dingtalk_url":     "https://oapi.dingtalk.com/robot/send?access_token=xxx",
		"channel_dingtalk_title":   "通知 {{task_name}}",
		"channel_dingtalk_body":    "正文 {{task_name}}",
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
	if textObj["content"] != "通知 ExampleTask\n正文 ExampleTask" {
		t.Errorf("content = %q, want 通知 ExampleTask\\n正文 ExampleTask", textObj["content"])
	}
	if _, ok := gotPayload["at"]; ok {
		t.Errorf("at should be absent when at_all is false, got %v", gotPayload["at"])
	}
}

func TestSendDingTalkMarkdown(t *testing.T) {
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode": 0, "errmsg": "ok"}`))
	}))
	defer server.Close()

	orig := dingtalkEndpoint
	dingtalkEndpoint = func(string, string) (string, error) { return server.URL, nil }
	defer func() { dingtalkEndpoint = orig }()

	runtime := testRuntime(map[string]any{
		"channel_dingtalk_enabled": true,
		"channel_dingtalk_url":     "https://oapi.dingtalk.com/robot/send?access_token=xxx",
		"channel_dingtalk_msgtype": "markdown",
		"channel_dingtalk_title":   "标题",
		"channel_dingtalk_body":    "正文",
	})
	if !Send(runtime, map[string]string{"title": "标题", "body": "正文"}) {
		t.Fatalf("Send returned false")
	}
	if gotPayload["msgtype"] != "markdown" {
		t.Errorf("msgtype = %v, want markdown", gotPayload["msgtype"])
	}
	md, ok := gotPayload["markdown"].(map[string]any)
	if !ok {
		t.Fatalf("markdown = %v (%T), want object", gotPayload["markdown"], gotPayload["markdown"])
	}
	if md["title"] != "标题" || md["text"] != "正文" {
		t.Errorf("markdown = %v, want title=标题 text=正文", md)
	}
	if _, ok := gotPayload["text"]; ok {
		t.Errorf("text field should not be present for markdown: %v", gotPayload)
	}
}

func TestSendDingTalkAtAll(t *testing.T) {
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode": 0, "errmsg": "ok"}`))
	}))
	defer server.Close()

	orig := dingtalkEndpoint
	dingtalkEndpoint = func(string, string) (string, error) { return server.URL, nil }
	defer func() { dingtalkEndpoint = orig }()

	runtime := testRuntime(map[string]any{
		"channel_dingtalk_enabled": true,
		"channel_dingtalk_url":     "https://oapi.dingtalk.com/robot/send?access_token=xxx",
		"channel_dingtalk_body":    "正文",
		"channel_dingtalk_at_all":  true,
	})
	if !Send(runtime, map[string]string{}) {
		t.Fatalf("Send returned false")
	}
	atObj, ok := gotPayload["at"].(map[string]any)
	if !ok {
		t.Fatalf("at = %v (%T), want object", gotPayload["at"], gotPayload["at"])
	}
	if atObj["isAtAll"] != true {
		t.Errorf("at.isAtAll = %v, want true", atObj["isAtAll"])
	}
}

func TestSendDingTalkErrors(t *testing.T) {
	// url 为空 → 报错
	ch := dingtalkChannel{cfg: dingtalkConfig{Enabled: true, Body: "正文"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("empty url should error")
	}
	// 非法协议 → 报错
	ch = dingtalkChannel{cfg: dingtalkConfig{Enabled: true, URL: "ftp://x", Body: "正文"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("invalid url should error")
	}
	// 空内容 → 报错
	orig := dingtalkEndpoint
	dingtalkEndpoint = func(string, string) (string, error) { return "https://example.invalid", nil }
	defer func() { dingtalkEndpoint = orig }()
	ch = dingtalkChannel{cfg: dingtalkConfig{Enabled: true, URL: "https://oapi.dingtalk.com/robot/send?access_token=x"}}
	if err := ch.Send(testCtx()); err == nil || !strings.Contains(err.Error(), "content") {
		t.Errorf("empty content should error, got %v", err)
	}

	// errcode != 0 的业务错误（HTTP 仍 200）
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode": 310000, "errmsg": "sign not match"}`))
	}))
	defer server.Close()
	dingtalkEndpoint = func(string, string) (string, error) { return server.URL, nil }
	ch = dingtalkChannel{cfg: dingtalkConfig{Enabled: true, URL: "https://oapi.dingtalk.com/robot/send?access_token=x", Body: "正文"}}
	if err := ch.Send(testCtx()); err == nil || !strings.Contains(err.Error(), "sign not match") {
		t.Errorf("errcode!=0 should error with errmsg, got %v", err)
	}

	// HTTP 500
	server500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server500.Close()
	dingtalkEndpoint = func(string, string) (string, error) { return server500.URL, nil }
	ch = dingtalkChannel{cfg: dingtalkConfig{Enabled: true, URL: "https://oapi.dingtalk.com/robot/send?access_token=x", Body: "正文"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("HTTP 500 should error")
	}
}

func TestDingTalkSign(t *testing.T) {
	// 固定 secret + timestamp 的加签期望值（HMAC-SHA256 + Base64 + URL 编码）
	got := dingtalkSign("SECabc123", 1700000000000)
	want := "N5P09a4%2Bp1AMJIJWnIvQd2Yxw9%2Bfu%2FoEBnPrjCcsLXk%3D"
	if got != want {
		t.Errorf("sign = %q, want %q", got, want)
	}
}

func TestDingTalkEndpointDefault(t *testing.T) {
	// 合法 URL → 原样返回（含 access_token）
	got, err := dingtalkEndpointDefault("  https://oapi.dingtalk.com/robot/send?access_token=abc  ", "")
	if err != nil || got != "https://oapi.dingtalk.com/robot/send?access_token=abc" {
		t.Errorf("valid = %q, err=%v", got, err)
	}
	// 缺省协议 → 自动补 https://
	got, err = dingtalkEndpointDefault("oapi.dingtalk.com/robot/send?access_token=abc", "")
	if err != nil || got != "https://oapi.dingtalk.com/robot/send?access_token=abc" {
		t.Errorf("no-scheme = %q, err=%v; want https:// prefix", got, err)
	}
	// 加签：追加 timestamp 与 sign 参数（URL 已含 ?access_token=，用 & 分隔）
	got, err = dingtalkEndpointDefault("https://oapi.dingtalk.com/robot/send?access_token=abc", "SECsecret")
	if err != nil || !strings.Contains(got, "&timestamp=") || !strings.Contains(got, "&sign=") {
		t.Errorf("signed = %q, err=%v; want &timestamp=&sign=", got, err)
	}
	// 加签（URL 无查询串）：用 ? 分隔
	got, err = dingtalkEndpointDefault("https://oapi.dingtalk.com/robot/send", "SECsecret")
	if err != nil || !strings.Contains(got, "?timestamp=") || !strings.Contains(got, "&sign=") {
		t.Errorf("signed no-query = %q, err=%v; want ?timestamp=&sign=", got, err)
	}
	// 空 → 报错
	if _, err := dingtalkEndpointDefault("", ""); err == nil {
		t.Errorf("empty should error")
	}
	// 非法协议 → 报错
	if _, err := dingtalkEndpointDefault("ftp://x", ""); err == nil {
		t.Errorf("bad scheme should error")
	}
}
