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

func TestResolveTelegramProxyManual(t *testing.T) {
	// 手动代理：trim 空白后返回
	got, err := resolveTelegramProxy(Config{TelegramProxyURL: "  http://127.0.0.1:7890  "})
	if err != nil || got != "http://127.0.0.1:7890" {
		t.Errorf("manual proxy = %q, err=%v; want http://127.0.0.1:7890", got, err)
	}
	// 手动代理为空 → 报错
	if _, err := resolveTelegramProxy(Config{}); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty manual proxy should error, got %v", err)
	}
}

func TestResolveTelegramProxyUpdate(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mxu-Test.json")
	writeMXUConfig(t, cfgPath, "http://127.0.0.1:1080")

	orig := mxuProxyConfigPath
	mxuProxyConfigPath = func() string { return cfgPath }
	defer func() { mxuProxyConfigPath = orig }()

	// 复用更新设置的代理
	got, err := resolveTelegramProxy(Config{TelegramUseUpdateProxy: true})
	if err != nil || got != "http://127.0.0.1:1080" {
		t.Errorf("update proxy = %q, err=%v; want http://127.0.0.1:1080", got, err)
	}

	// 更新设置未配置代理 → 报错（不再回退手动代理）
	noProxyPath := filepath.Join(dir, "mxu-NoProxy.json")
	if err := os.WriteFile(noProxyPath, []byte(`{"version":"1.0","settings":{"theme":"dark"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mxuProxyConfigPath = func() string { return noProxyPath }
	if _, err := resolveTelegramProxy(Config{TelegramUseUpdateProxy: true, TelegramProxyURL: "http://x:1"}); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("unconfigured update proxy should error, got %v", err)
	}

	// 文件不存在 → 报错
	mxuProxyConfigPath = func() string { return "" }
	if _, err := resolveTelegramProxy(Config{TelegramUseUpdateProxy: true}); err == nil {
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

func TestSendTelegramWithProxy(t *testing.T) {
	// 把 httptest 服务器当作代理目标：proxyClient 会把请求发给该地址，
	// 能收到请求即证明走了配置的代理链路。
	var gotRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true, "result": {"message_id": 1}}`))
	}))
	defer server.Close()

	origEndpoint := telegramEndpoint
	telegramEndpoint = func(string) (string, error) { return server.URL, nil }
	defer func() { telegramEndpoint = origEndpoint }()

	config := Config{
		TelegramEnabled:  true,
		TelegramToken:    "t",
		TelegramChatID:   "user1",
		TelegramTitle:    "标题",
		TelegramUseProxy: true,
		TelegramProxyURL: server.URL,
	}
	if !Send(config, map[string]string{}) {
		t.Fatalf("Send returned false")
	}
	if !gotRequest {
		t.Errorf("no request received through proxy")
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
