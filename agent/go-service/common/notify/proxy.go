package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// 全局代理模块：所有渠道统一使用（由调度层解析并构造 client）。
// 支持手动代理地址，或复用「更新设置」里配置的代理（读取 install/config/mxu-{项目名}.json）。
// 渠道自身不感知代理——Send() 统一调用 resolveProxy + proxyClient 后传入 SendContext.Client。

// mxuProxyConfigPath 返回更新设置的配置文件路径；包级变量便于测试注入。
var mxuProxyConfigPath = findMxuProxyConfigPath

// interfaceNameRegexp 从 interface.json（JSONC）中取顶层 name 字段。
// 只需 name 一个值，用正则跳过注释解析，避免引入 JSONC 解析器。
var interfaceNameRegexp = regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)

// proxyClients 按解析出的代理地址缓存 client，避免每次发送都重建 Transport。
var proxyClients sync.Map // proxyURL → *http.Client

// resolveProxy 解析全局代理地址：
//   - useUpdate 开启时复用「更新设置」里的代理（读取失败/未配置则报错）；
//   - 否则使用手动填写的 manualURL。
func resolveProxy(useUpdate bool, manualURL string) (string, error) {
	if useUpdate {
		if path := mxuProxyConfigPath(); path != "" {
			if proxyURL, err := readMxuProxyURL(path); err == nil {
				return proxyURL, nil
			}
		}
		return "", fmt.Errorf("update proxy not configured (settings.proxy.url in config/mxu-*.json)")
	}
	proxyURL := strings.TrimSpace(manualURL)
	if proxyURL == "" {
		return "", fmt.Errorf("proxy url is empty")
	}
	return proxyURL, nil
}

// findMxuProxyConfigPath 定位更新设置的配置文件 install/config/mxu-{项目名}.json：
// 从 cwd 与可执行文件目录向上找 install 根（含 config/ 与 interface.json），
// 再从 interface.json 取项目名拼出配置文件名。
func findMxuProxyConfigPath() string {
	root := findInstallRoot()
	if root == "" {
		return ""
	}
	name := interfaceProjectName(filepath.Join(root, "interface.json"))
	if name == "" {
		return ""
	}
	path := filepath.Join(root, "config", "mxu-"+name+".json")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

// findInstallRoot 从 cwd 与可执行文件目录向上逐级查找 install 根目录：
// 满足存在 config/ 目录（MXU 配置文件目录）与 interface.json 的目录即视为根。
func findInstallRoot() string {
	var candidates []string
	seen := make(map[string]struct{})
	addChain := func(start string) {
		if start == "" {
			return
		}
		dir := filepath.Clean(start)
		for depth := 0; depth < 8; depth++ {
			if _, ok := seen[dir]; !ok {
				seen[dir] = struct{}{}
				candidates = append(candidates, dir)
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		addChain(cwd)
	}
	if exePath, err := os.Executable(); err == nil {
		addChain(filepath.Dir(exePath))
	}
	for _, dir := range candidates {
		if info, err := os.Stat(filepath.Join(dir, "config")); err == nil && info.IsDir() {
			if info, err := os.Stat(filepath.Join(dir, "interface.json")); err == nil && !info.IsDir() {
				return dir
			}
		}
	}
	return ""
}

// interfaceProjectName 读取 interface.json 的顶层 name 字段（JSONC，允许注释）。
func interfaceProjectName(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if m := interfaceNameRegexp.FindSubmatch(data); len(m) == 2 {
		return string(m[1])
	}
	return ""
}

// mxuConfigFile 是更新设置配置文件（install/config/mxu-*.json）中与代理相关的字段结构。
type mxuConfigFile struct {
	Settings struct {
		Proxy struct {
			URL string `json:"url"`
		} `json:"proxy"`
	} `json:"settings"`
}

// readMxuProxyURL 读取更新设置配置文件中的代理地址（settings.proxy.url）。
func readMxuProxyURL(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var cfg mxuConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", err
	}
	proxyURL := strings.TrimSpace(cfg.Settings.Proxy.URL)
	if proxyURL == "" {
		return "", fmt.Errorf("update proxy not configured")
	}
	return proxyURL, nil
}

// proxyClient 构造（或按地址复用）走代理的 HTTP 客户端，与默认客户端同样套用 defaultTimeout。
// 仅支持 http/https 代理（http.Transport.Proxy 原生支持）；socks5 未引入额外依赖，
// 报错提示改用对应 http 代理端口。
func proxyClient(proxyURL string) (*http.Client, error) {
	if c, ok := proxyClients.Load(proxyURL); ok {
		return c.(*http.Client), nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf(
			"unsupported proxy scheme %q: only http/https supported, use the http port of your socks5 proxy",
			u.Scheme,
		)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(u)}
	client := &http.Client{Timeout: defaultTimeout, Transport: transport}
	actual, _ := proxyClients.LoadOrStore(proxyURL, client)
	return actual.(*http.Client), nil
}
