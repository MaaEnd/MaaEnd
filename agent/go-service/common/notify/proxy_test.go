package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProxyManual(t *testing.T) {
	// 手动代理：trim 空白后返回
	got, err := resolveProxy(false, "  http://127.0.0.1:7890  ")
	if err != nil || got != "http://127.0.0.1:7890" {
		t.Errorf("manual proxy = %q, err=%v; want http://127.0.0.1:7890", got, err)
	}
	// 手动代理为空 → 报错
	if _, err := resolveProxy(false, ""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty manual proxy should error, got %v", err)
	}
}

func TestResolveProxyUpdate(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mxu-Test.json")
	writeMXUConfig(t, cfgPath, "http://127.0.0.1:1080")

	orig := mxuProxyConfigPath
	mxuProxyConfigPath = func() string { return cfgPath }
	defer func() { mxuProxyConfigPath = orig }()

	// 复用更新设置的代理
	got, err := resolveProxy(true, "")
	if err != nil || got != "http://127.0.0.1:1080" {
		t.Errorf("update proxy = %q, err=%v; want http://127.0.0.1:1080", got, err)
	}

	// 更新设置未配置代理 → 报错（不再回退手动代理）
	noProxyPath := filepath.Join(dir, "mxu-NoProxy.json")
	if err := os.WriteFile(noProxyPath, []byte(`{"version":"1.0","settings":{"theme":"dark"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mxuProxyConfigPath = func() string { return noProxyPath }
	if _, err := resolveProxy(true, "http://x:1"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("unconfigured update proxy should error, got %v", err)
	}

	// 文件不存在 → 报错
	mxuProxyConfigPath = func() string { return "" }
	if _, err := resolveProxy(true, ""); err == nil {
		t.Errorf("missing mxu config should error")
	}
}

func TestReadMxuProxyURL(t *testing.T) {
	dir := t.TempDir()
	// 无代理字段
	plain := filepath.Join(dir, "plain.json")
	if err := os.WriteFile(plain, []byte(`{"version":"1.0","settings":{"theme":"dark"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readMxuProxyURL(plain); err == nil {
		t.Errorf("config without proxy should error")
	}
	// 文件不存在
	if _, err := readMxuProxyURL(filepath.Join(dir, "missing.json")); err == nil {
		t.Errorf("missing file should error")
	}
}

func TestInterfaceProjectName(t *testing.T) {
	dir := t.TempDir()
	// JSONC：带注释也能取到顶层 name
	path := filepath.Join(dir, "interface.json")
	content := "{\n  // 顶层注释\n  \"interface_version\": 2,\n  \"name\": \"MaaEnd\",\n  \"version\": \"v0.1.0\"\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := interfaceProjectName(path); got != "MaaEnd" {
		t.Errorf("project name = %q, want MaaEnd", got)
	}
	if got := interfaceProjectName(filepath.Join(dir, "nope.json")); got != "" {
		t.Errorf("missing interface.json should return empty, got %q", got)
	}
}

func TestProxyClient(t *testing.T) {
	// http 代理 → 正常构造，套用默认超时
	client, err := proxyClient("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("http proxy: %v", err)
	}
	if client.Timeout != defaultTimeout {
		t.Errorf("timeout = %v, want %v", client.Timeout, defaultTimeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Errorf("expected transport with Proxy set")
	}

	// socks5 → 明确报错（未引入额外依赖）
	if _, err := proxyClient("socks5://127.0.0.1:7891"); err == nil || !strings.Contains(err.Error(), "unsupported proxy scheme") {
		t.Errorf("socks5 should error, got %v", err)
	}

	// 非法地址 → 报错
	if _, err := proxyClient("://bad"); err == nil {
		t.Errorf("invalid proxy url should error")
	}
}

func TestSendWithProxy(t *testing.T) {
	// 把 httptest 服务器当作代理目标：proxyClient 会把请求发给该地址，
	// 能收到请求即证明走了配置的代理链路。telegramEndpoint 指向一个
	// 不会真实可达的地址——若没走代理，请求会直连该地址而失败。
	var gotRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true, "result": {"message_id": 1}}`))
	}))
	defer server.Close()

	origEndpoint := telegramEndpoint
	telegramEndpoint = func(string, string) (string, error) { return "http://127.0.0.1:1/botX/sendMessage", nil }
	defer func() { telegramEndpoint = origEndpoint }()

	runtime := testRuntime(map[string]any{
		"telegram_enabled":   true,
		"telegram_use_proxy": true,
		"telegram_token":     "t",
		"telegram_chat_id":   "user1",
		"telegram_title":     "标题",
		"use_proxy":          true,
		"proxy_url":          server.URL,
	})
	if !Send(runtime, map[string]string{}) {
		t.Fatalf("Send returned false")
	}
	if !gotRequest {
		t.Errorf("no request received through proxy")
	}
}

func TestSendProxyPerChannel(t *testing.T) {
	// 每渠道独立开关：telegram_use_proxy=false 时，即使全局 use_proxy=true，
	// telegram 仍直连（telegramEndpoint 指向不可达地址 → 发送失败），代理服务器不命中。
	var gotRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true, "result": {"message_id": 1}}`))
	}))
	defer server.Close()

	origEndpoint := telegramEndpoint
	telegramEndpoint = func(string, string) (string, error) { return "http://127.0.0.1:1/botX/sendMessage", nil }
	defer func() { telegramEndpoint = origEndpoint }()

	// 渠道关闭代理 → 直连不可达地址 → Send false，代理服务器无请求
	runtime := testRuntime(map[string]any{
		"telegram_enabled":   true,
		"telegram_use_proxy": false,
		"telegram_token":     "t",
		"telegram_chat_id":   "user1",
		"telegram_title":     "标题",
		"use_proxy":          true,
		"proxy_url":          server.URL,
	})
	if Send(runtime, map[string]string{}) {
		t.Errorf("Send should return false when channel proxy disabled and endpoint unreachable")
	}
	if gotRequest {
		t.Errorf("proxy server should not be hit when channel proxy disabled")
	}
}

// writeMXUConfig 写一个带 settings.proxy.url 的 MXU 配置测试文件。
func writeMXUConfig(t *testing.T, path, proxyURL string) {
	t.Helper()
	cfg := map[string]any{
		"version": "1.0",
		"settings": map[string]any{
			"proxy": map[string]any{"url": proxyURL},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTelegramEndpointDefault(t *testing.T) {
	// 留空 API URL → 官方地址
	got, err := telegramEndpointDefault("123456:ABC-DEF", "")
	if err != nil || got != "https://api.telegram.org/bot123456:ABC-DEF/sendMessage" {
		t.Errorf("official endpoint = %q, err=%v", got, err)
	}
	// 第三方 API 地址（带尾斜杠）→ 去斜杠后拼接 /bot{token}/sendMessage
	got, err = telegramEndpointDefault("123456:ABC-DEF", "https://tg-proxy.example.com/")
	if err != nil || got != "https://tg-proxy.example.com/bot123456:ABC-DEF/sendMessage" {
		t.Errorf("third-party endpoint = %q, err=%v", got, err)
	}
	// 缺省协议 → 自动补 https://
	got, err = telegramEndpointDefault("123456:ABC-DEF", "tg-proxy.example.com")
	if err != nil || got != "https://tg-proxy.example.com/bot123456:ABC-DEF/sendMessage" {
		t.Errorf("no-scheme endpoint = %q, err=%v; want https:// prefix", got, err)
	}
	// token 为空 → 报错
	if _, err := telegramEndpointDefault("  ", "https://tg-proxy.example.com"); err == nil {
		t.Errorf("empty token should error")
	}
}
