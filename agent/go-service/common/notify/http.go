package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const defaultTimeout = 10 * time.Second

var httpClient = &http.Client{Timeout: defaultTimeout}

var urlInErrRegexp = regexp.MustCompile(`https?://[^\s"']+`)

// ensureHTTPS 给缺省协议的地址补 https:// 前缀：已含 scheme（如 http://、https://）的原样返回，
// 否则默认使用 https://（用户只填主机名/路径时保底，避免请求失败）。
func ensureHTTPS(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "://") {
		return raw
	}
	return "https://" + raw
}

// sanitizeError 把错误文本中的 URL 整体打码：http.Client 的错误串包含完整请求 URL，
// 可能携带渠道凭据（Bark key / ServerChan sendkey / Webhook token），不直接写入日志。
func sanitizeError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", urlInErrRegexp.ReplaceAllString(err.Error(), "<redacted url>"))
}

// checkStatus 检查响应状态码，>= 400 视为失败。
func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}

// postJSON 用指定 client 发送 JSON POST 并检查状态码；expectedCode >= 0 时解析 body 的 code 字段对比。
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
		// 限制响应体大小（1MB），防止服务端异常返回超大 body 吃内存
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
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
