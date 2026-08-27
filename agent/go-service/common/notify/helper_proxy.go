package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// 全局代理模块：所有渠道统一使用（由调度层解析并构造 client）。
// 支持手动代理地址，或复用「更新设置」里配置的代理（读取 install/config/mxu-{项目名}.json）。
// 渠道自身不感知代理——Send() 统一调用 resolveProxy + proxyClient 后传入 SendContext.Client。

// mxuProxyConfigPath 返回更新设置的配置文件路径；包级变量便于测试注入。
var mxuProxyConfigPath = findMxuProxyConfigPath

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
			} else {
				return "", fmt.Errorf("read update proxy config failed: %w", err)
			}
		}
		return "", fmt.Errorf("update proxy config not found (install/config/mxu-*.json)")
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
// 用最小扫描器跳过字符串值、// 与 /* */ 注释，只认顶层对象（depth==1）的 "name" 键，
// 避免嵌套字段（controller/task 的 name）与注释里的 "name" 干扰取错项目名。
func interfaceProjectName(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return topLevelName(string(data))
}

// topLevelName 从 JSONC 文本中提取顶层对象（depth==1）的 "name" 字符串值。
// 逐字符扫描：跳过字符串字面量、// 行注释、/* */ 块注释；遇到顶层 "name" 键时读取其值。
func topLevelName(s string) string {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
		case '"':
			// 读取一个字符串字面量（键或值）
			j := i + 1
			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' {
					j++ // 跳过转义字符
				}
				j++
			}
			str := s[i+1 : j]
			i = j // 推进到闭合引号
			// 顶层 "name" 键：跳过空白与冒号，读取其字符串值
			if depth == 1 && str == "name" {
				k := i + 1
				for k < len(s) && (s[k] == ' ' || s[k] == '\t' || s[k] == '\n' || s[k] == '\r') {
					k++
				}
				if k < len(s) && s[k] == ':' {
					k++
					for k < len(s) && (s[k] == ' ' || s[k] == '\t' || s[k] == '\n' || s[k] == '\r') {
						k++
					}
					if k < len(s) && s[k] == '"' {
						m := k + 1
						for m < len(s) && s[m] != '"' {
							if s[m] == '\\' {
								m++
							}
							m++
						}
						return s[k+1 : m]
					}
				}
			}
		case '/':
			// 跳过 // 行注释与 /* */ 块注释
			if i+1 < len(s) && s[i+1] == '/' {
				for i < len(s) && s[i] != '\n' {
					i++
				}
			} else if i+1 < len(s) && s[i+1] == '*' {
				i += 2
				for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
					i++
				}
				i++
			}
		}
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
// 仅支持 http/https 代理（http.Transport.Proxy 原生支持）；其他 scheme（如 socks5）明确报错。
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
			"unsupported proxy scheme %q: only http/https supported",
			u.Scheme,
		)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(u)}
	client := &http.Client{Timeout: defaultTimeout, Transport: transport}
	actual, _ := proxyClients.LoadOrStore(proxyURL, client)
	return actual.(*http.Client), nil
}
