//go:build linux

package ziplineimport

import (
	"bytes"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/rs/zerolog/log"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/captureuid"
)

// 需要解密取数的目标主机：提供 mark/list 的就是这个 API 域名（页面壳是 game.skland.com，
// 真正的取数接口 /web/v1/game/endfield/map/mark/list 在 zonai.skland.com 上，经实测确认）。
//
// 隐私收窄的关键点：登录在其它域名上进行（那些连接经 PAC 一律 DIRECT，根本不经过本机
// 代理，本机 CA 也永远解不到它们的密）；只有这里列出的主机会被 MITM，取值面被压到
// 「地图接口本身的已认证流量」。若部署发生变化导致 mark/list 换验，把新主机加进这里。
var mitmHosts = []string{
	"zonai.skland.com",
}

// mitmProxy 是本地受限 HTTPS 中间人代理：浏览器持登录态并自行发出带签名的 mark/list
// 请求，代理只把响应体抄下来——浏览器代发、我们抄响应，不碰凭据也不复刻签名。
// 只对白名单主机（mitmHosts）解密，其余一律透传（配合 PAC 后这些连接甚至不会到代理）。
type mitmProxy struct {
	port   int
	server *http.Server
	pacSrv *http.Server

	mu        sync.Mutex
	responses []capturedResponse
	covered   map[string]bool
	lastEvent time.Time
	seenHosts map[string]bool
}

func newMitmProxy(port int) *mitmProxy {
	return &mitmProxy{port: port, covered: make(map[string]bool), seenHosts: make(map[string]bool)}
}

// start 装载一次性根 CA，启动受限 MITM 代理，返回监听端口（port<=0 时用随机空闲端口）。
func (m *mitmProxy) start(ca tls.Certificate) (int, error) {
	goproxy.GoproxyCa = ca

	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = false
	// 只对白名单主机解密；其它主机走普通 CONNECT 隧道（本就解不到，防御纵深）。
	proxy.OnRequest().HandleConnectFunc(func(host string, _ *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		m.recordConnectHost(host) // 诊断：浏览器到底把哪些主机送进了代理
		if isMitmHost(host) {
			return goproxy.MitmConnect, host
		}
		return goproxy.OkConnect, host
	})
	proxy.OnResponse().DoFunc(m.captureResponse)

	listenPort := m.port
	if listenPort <= 0 {
		listenPort = 0
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", listenPort))
	if err != nil {
		return 0, fmt.Errorf("proxy listen: %w", err)
	}
	m.port = ln.Addr().(*net.TCPAddr).Port

	m.server = &http.Server{Handler: proxy}
	go func() {
		_ = m.server.Serve(ln)
	}()
	log.Info().Int("port", m.port).Str("component", componentName).
		Msg("zipline import: mitm proxy listening")
	return m.port, nil
}

func (m *mitmProxy) stop() {
	if m.server != nil {
		_ = m.server.Close()
	}
	if m.pacSrv != nil {
		_ = m.pacSrv.Close()
	}
}

// startPAC 起一个仅服务于本机 PAC 的小型 HTTP 端点，返回其 http://127.0.0.1:port/proxy.pac
// 地址。PAC 用 http:// 而非 file:// 提供——firefox 对 file:// PAC 的加载兼容性差，
// 而 localhost http PAC 是稳定做法。PAC 只把 mitmHost 指向本机代理，其余一律 DIRECT。
// startPAC 起一个仅服务于本机 PAC 的小型 HTTP 端点，返回其 http://127.0.0.1:port/proxy.pac
// 地址。PAC 用 http:// 而非 file:// 提供——firefox 对 file:// PAC 的加载兼容性差，
// 而 localhost http PAC 是稳定做法。PAC 只把白名单主机（mitmHosts）指向本机代理，
// 其余一律 DIRECT：登录/浏览其它站点的连接根本不经过代理，连域名元数据都不留。
func (m *mitmProxy) startPAC() (string, error) {
	content := fmt.Sprintf(`function FindProxyForURL(url, host) {
    if (%s) {
        return "PROXY 127.0.0.1:%d";
    }
    return "DIRECT";
}
`, pacHostsExpr(), m.port)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("pac listen: %w", err)
	}
	lnAddr := ln.Addr().(*net.TCPAddr)
	mux := http.NewServeMux()
	mux.HandleFunc("/proxy.pac", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		_, _ = io.WriteString(w, content)
	})
	m.pacSrv = &http.Server{Handler: mux}
	go func() { _ = m.pacSrv.Serve(ln) }()
	return fmt.Sprintf("http://127.0.0.1:%d/proxy.pac", lnAddr.Port), nil
}

// recordConnectHost 记录每个经代理 CONNECT 的主机（去端口），首见打一条日志。
// 用于确认 firefox 是否真的把流量送进了代理，以及 reveal 到底哪些域名被路由/解密。
func (m *mitmProxy) recordConnectHost(host string) {
	h := host
	if i := strings.LastIndex(h, ":"); i >= 0 {
		h = h[:i]
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.seenHosts[h] {
		m.seenHosts[h] = true
		log.Info().Str("component", componentName).Str("host", h).
			Msg("zipline import: connect via proxy")
	}
}

// captureResponse 在 mark/list 响应到达时抄下完整响应体（解 gzip），供解析；不落盘。
func (m *mitmProxy) captureResponse(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	if resp == nil || resp.Body == nil || ctx == nil || ctx.Req == nil {
		return resp
	}
	if !isMarkListResponse(ctx.Req.URL.Path) {
		return resp
	}

	m.touch()

	// 诊断：mark/list 命中时记录主机与路径，便于定位路由/白名单问题。
	reqHost := ctx.Req.URL.Host
	log.Debug().Str("component", componentName).Str("host", reqHost).Str("path", ctx.Req.URL.Path).
		Msg("zipline import: mark/list request seen")

	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	content := raw
	if strings.Contains(resp.Header.Get("Content-Encoding"), "gzip") {
		if gz, err := gzip.NewReader(bytes.NewReader(raw)); err == nil {
			content, _ = io.ReadAll(gz)
			_ = gz.Close()
		}
	}
	restored := io.NopCloser(bytes.NewReader(raw))

	if len(content) == 0 || !saveMarksPresent(content) {
		return respWith(resp, restored)
	}
	requestURL := ctx.Req.URL.String()
	m.mu.Lock()
	// 只保留真正的响应（带 data.saveMarks 结构）。未登录也是空数组，仍算一条捕获，
	// 用于在日志里提示「是否已登录」。
	m.responses = append(m.responses, capturedResponse{url: requestURL, body: content})
	// 增量维护 covered：抓到即解析一次，避免 launch 轮询时反复反序列化全部响应。
	// roleId 缺失或非法的响应（未登录、页面初始化阶段、关卡子列表）不能归属账号，
	// 不推进完成判据，否则未登录时官方点位会把 covered 撑满、提前判成抓齐。
	if captureuid.IsValidRawUID(queryValue(requestURL, "roleId")) {
		for id := range marksByMap(content, nil, queryValue(requestURL, "mapId")) {
			m.covered[id] = true
		}
	}
	m.mu.Unlock()
	log.Info().Str("component", componentName).Str("path", ctx.Req.URL.Path).
		Msg("zipline import: captured mark/list")
	return respWith(resp, restored)
}

// touch 记录一次 mark/list 活动（无论是否为可解析数据），供完成判据算「静默时长」。
func (m *mitmProxy) touch() {
	m.mu.Lock()
	m.lastEvent = time.Now()
	m.mu.Unlock()
}

func respWith(r *http.Response, body io.ReadCloser) *http.Response {
	r.Body = body
	return r
}

// responsesSnapshot 返回已捕获 mark/list 响应的拷贝（供最终归集落盘）。
func (m *mitmProxy) responsesSnapshot() []capturedResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]capturedResponse, len(m.responses))
	copy(out, m.responses)
	return out
}

// coveredMapSnapshot 返回已出现真实标记的地图集合拷贝（完成判据用）。
func (m *mitmProxy) coveredMapSnapshot() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]bool, len(m.covered))
	for k, v := range m.covered {
		out[k] = v
	}
	return out
}

// capturedCount 返回已捕获 mark/list 响应的条数（用于「是否有响应但无覆盖」的提示）。
func (m *mitmProxy) capturedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.responses)
}

// lastEventAt 返回最近一次 mark/list 活动时刻。
func (m *mitmProxy) lastEventAt() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastEvent
}

// isMitmHost 判断主机是否在解密白名单内（忽略端口后缀）。
func isMitmHost(host string) bool {
	h := host
	if i := strings.LastIndex(h, ":"); i >= 0 {
		h = h[:i]
	}
	for _, m := range mitmHosts {
		if h == m {
			return true
		}
	}
	return false
}

// pacHostsExpr 生成 PAC 里「命中白名单任一主机」的 JS 布尔表达式。
func pacHostsExpr() string {
	exprs := make([]string, 0, len(mitmHosts))
	for _, h := range mitmHosts {
		exprs = append(exprs, fmt.Sprintf("host === %q", h))
	}
	return strings.Join(exprs, " || ")
}

// generateRootCA 生成一次性自签根 CA。证书写盘（供 certutil 注入），私钥只在内存里，
// 进程结束即失——不落盘、不跨运行留存。
func generateRootCA(certPemPath string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	// 一次性 CA 只存活于当次运行。NotBefore 往前留时钟偏差余量，NotAfter 指向未来
	// （必须覆盖整轮抓取窗口），避免 firefox 报 SEC_ERROR_EXPIRED_ISSUER_CERTIFICATE。
	// 私钥只在内存、进程结束即失，有效期长短不构成留存问题，故取 2 小时放宽。
	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(2 * time.Hour)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "MaaEnd ZiplineImport CA"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPemPath, certPem, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	keyDer, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPem := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer})
	return tls.X509KeyPair(certPem, keyPem)
}
