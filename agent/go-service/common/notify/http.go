package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultTimeout = 10 * time.Second

var httpClient = &http.Client{Timeout: defaultTimeout}

var urlInErrRegexp = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"']+`)

// ensureHTTPS 给缺省协议的地址补 https:// 前缀。
// 精确判断 scheme：已含 scheme（如 http://、https://、ftp://）原样返回；
// 以 // 开头（协议相对 URL）补 https: 前缀；否则补 https://。
func ensureHTTPS(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if i := strings.Index(raw, "://"); i > 0 {
		return raw // 已含 scheme（scheme 非空）
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw // 协议相对 URL：补默认 scheme
	}
	return "https://" + raw
}

// sanitizedError 包装原始错误：Error() 只输出脱敏后的文本（不含 URL 凭据），
// Unwrap() 保留原始错误，使上层 errors.Is/As 能穿透到根因。
type sanitizedError struct {
	msg string
	err error
}

func (e *sanitizedError) Error() string { return e.msg }
func (e *sanitizedError) Unwrap() error { return e.err }

// sanitizeError 把错误文本中的 URL 整体打码：http.Client 的错误串包含完整请求 URL，
// 可能携带渠道凭据（Bark key / ServerChan sendkey / Webhook token），不直接写入日志。
func sanitizeError(err error) error {
	if err == nil {
		return nil
	}
	return &sanitizedError{
		msg: urlInErrRegexp.ReplaceAllString(err.Error(), "<redacted url>"),
		err: err,
	}
}

// validateHTTPURL 校验地址为 http/https：先 ensureHTTPS 补默认 scheme，再校验 scheme 白名单。
// 供 webhook / telegram api_url 等缺校验的渠道复用（其余渠道已各自校验）。
func validateHTTPURL(raw, field string) (string, error) {
	raw = ensureHTTPS(raw)
	if raw == "" {
		return "", fmt.Errorf("%s is empty", field)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid %s url", field)
	}
	return raw, nil
}

// checkStatus 检查响应状态码，>= 400 视为失败。
func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}

// noCodeCheck 表示 postJSON 不校验响应体业务 code（仅按 HTTP 状态判断，如 Discord 204 无 body）。
const noCodeCheck = -1

// readResponseBody 读响应体并限流 1MB，防止服务端异常返回超大 body 吃内存。
func readResponseBody(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return body, nil
}

// postJSON 用指定 client 发送 JSON POST 并检查状态码；expectedCode >= 0 时解析 body 的 code 字段对比，
// 传 noCodeCheck 表示跳过业务 code 校验（仅按 HTTP 状态判断）。
// client 由调度层统一提供（含全局代理），渠道不得自行构建代理。
func postJSON(client *http.Client, endpoint string, payload map[string]any, expectedCode int) error {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := client.Post(endpoint, "application/json;charset=utf-8", bytes.NewReader(jsonBody))
	if err != nil {
		return sanitizeError(err)
	}
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return err
	}
	if expectedCode >= 0 {
		respBody, err := readResponseBody(resp)
		if err != nil {
			return err
		}
		var result struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return fmt.Errorf("failed to parse response body: %w", err)
		}
		if result.Code != expectedCode {
			return fmt.Errorf("api code: %d, expected %d", result.Code, expectedCode)
		}
	}
	return nil
}
