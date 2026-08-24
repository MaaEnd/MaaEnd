package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

const defaultTimeout = 10 * time.Second

var httpClient = &http.Client{Timeout: defaultTimeout}

var urlInErrRegexp = regexp.MustCompile(`https?://[^\s"']+`)

// sanitizeError 把错误文本中的 URL 整体打码：http.Client 的错误串包含完整请求 URL，
// 可能携带渠道凭据（Bark key / ServerChan sendkey），不直接写入日志。
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

// postJSON 发送 JSON POST 并检查状态码；expectedCode >= 0 时解析 body 的 code 字段对比。
func postJSON(endpoint string, payload map[string]any, expectedCode int) error {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := httpClient.Post(endpoint, "application/json;charset=utf-8", bytes.NewReader(jsonBody))
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
