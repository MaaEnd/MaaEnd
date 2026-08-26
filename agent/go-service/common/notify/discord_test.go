package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendDiscord(t *testing.T) {
	var gotPayload map[string]any
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	orig := discordEndpoint
	discordEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { discordEndpoint = orig }()

	runtime := testRuntime(map[string]any{
		"discord_enabled":     true,
		"discord_webhook_url": "https://discord.com/api/webhooks/123/tok",
		"discord_username":    "MaaEnd {{task_name}}",
		"discord_avatar_url":  "https://example.com/avatar.png",
		"discord_title":       "通知 {{task_name}}",
		"discord_body":        "正文 {{task_name}}",
	})
	if !Send(runtime, map[string]string{"task_name": "ExampleTask", "title": "标题 {{task_name}}", "body": "正文 {{task_name}}"}) {
		t.Fatalf("Send returned false")
	}
	if gotPath != "/" {
		t.Errorf("request path = %q, want / (injected endpoint)", gotPath)
	}
	if gotPayload["content"] != "通知 ExampleTask\n\n正文 ExampleTask" {
		t.Errorf("content = %q, want 通知 ExampleTask\\n\\n正文 ExampleTask", gotPayload["content"])
	}
	if gotPayload["username"] != "MaaEnd ExampleTask" {
		t.Errorf("username = %v, want MaaEnd ExampleTask", gotPayload["username"])
	}
	if gotPayload["avatar_url"] != "https://example.com/avatar.png" {
		t.Errorf("avatar_url = %v, want https://example.com/avatar.png", gotPayload["avatar_url"])
	}
}

func TestSendDiscordMinimal(t *testing.T) {
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	orig := discordEndpoint
	discordEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { discordEndpoint = orig }()

	// username/avatar_url 留空 → 不携带这两个字段；标题用渠道配置、正文回退通知项
	runtime := testRuntime(map[string]any{
		"discord_enabled":     true,
		"discord_webhook_url": "https://discord.com/api/webhooks/123/tok",
		"discord_title":       "标题",
	})
	if !Send(runtime, map[string]string{"title": "通知标题", "body": "通知正文"}) {
		t.Fatalf("Send returned false")
	}
	if gotPayload["content"] != "标题\n\n通知正文" {
		t.Errorf("content = %v, want 标题\\n\\n通知正文", gotPayload["content"])
	}
	if _, ok := gotPayload["username"]; ok {
		t.Errorf("username should be omitted when empty: %v", gotPayload)
	}
	if _, ok := gotPayload["avatar_url"]; ok {
		t.Errorf("avatar_url should be omitted when empty: %v", gotPayload)
	}
}

func TestSendDiscordErrors(t *testing.T) {
	// webhook url 为空 → 报错
	ch := discordChannel{cfg: discordConfig{Enabled: true, Title: "标题"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("empty webhook url should error")
	}
	// 非法协议 → 报错
	ch = discordChannel{cfg: discordConfig{Enabled: true, WebhookURL: "ftp://x", Title: "标题"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("invalid webhook url should error")
	}
	// 空内容 → 报错
	orig := discordEndpoint
	discordEndpoint = func(string) (string, error) { return "https://example.invalid", nil }
	defer func() { discordEndpoint = orig }()
	ch = discordChannel{cfg: discordConfig{Enabled: true, WebhookURL: "https://discord.com/api/webhooks/1/t"}}
	if err := ch.Send(testCtx()); err == nil || !strings.Contains(err.Error(), "content") {
		t.Errorf("empty content should error, got %v", err)
	}

	// HTTP 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	discordEndpoint = func(string) (string, error) { return server.URL, nil }
	ch = discordChannel{cfg: discordConfig{Enabled: true, WebhookURL: "https://discord.com/api/webhooks/1/t", Title: "标题"}}
	if err := ch.Send(testCtx()); err == nil {
		t.Errorf("HTTP 500 should error")
	}
}

func TestDiscordEndpointDefault(t *testing.T) {
	// 合法 URL → 原样返回
	got, err := discordEndpointDefault("  https://discord.com/api/webhooks/123/tok  ")
	if err != nil || got != "https://discord.com/api/webhooks/123/tok" {
		t.Errorf("valid = %q, err=%v", got, err)
	}
	// 缺省协议 → 自动补 https://
	got, err = discordEndpointDefault("discord.com/api/webhooks/123/tok")
	if err != nil || got != "https://discord.com/api/webhooks/123/tok" {
		t.Errorf("no-scheme = %q, err=%v; want https:// prefix", got, err)
	}
	// 空 → 报错
	if _, err := discordEndpointDefault(""); err == nil {
		t.Errorf("empty should error")
	}
	// 非法协议 → 报错
	if _, err := discordEndpointDefault("ftp://x"); err == nil {
		t.Errorf("bad scheme should error")
	}
	if _, err := discordEndpointDefault("not a url"); err == nil {
		t.Errorf("bad url should error")
	}
}
