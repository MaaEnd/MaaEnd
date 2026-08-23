package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	defaultConfigNode = "__NotifyConfig"
	defaultTimeout    = 10 * time.Second
)

var httpClient = &http.Client{Timeout: defaultTimeout}

// 端点构造函数为包级变量，便于测试注入本地服务器。
var (
	serverChanEndpoint = serverChanEndpointDefault
	barkEndpoint       = barkEndpointDefault
)

// Config 是 __NotifyConfig 节点 attach 的解析结果，由用户在设置页配置、
// 经 global_option 的 pipeline_override 注入节点 attach 后由本包读取。
// 注意：各字段均为 attach 的顶层 key——MaaFramework 的 override 对 attach
// 按顶层 key 合并，多个设置项各自写入一个顶层 key 才不会互相覆盖。
type Config struct {
	WebhookEnabled bool   `json:"webhook_enabled"`
	WebhookURL     string `json:"webhook_url"`
	WebhookMethod  string `json:"webhook_method"`
	WebhookHeaders string `json:"webhook_headers"`
	WebhookBody    string `json:"webhook_body"`

	BarkEnabled bool   `json:"bark_enabled"`
	BarkKey     string `json:"bark_key"`
	BarkTitle   string `json:"bark_title"` // 渠道级标题，优先级高于通知项；支持 {{title}} 引用通知项预填，留空回退通知项
	BarkBody    string `json:"bark_body"`  // 渠道级正文，同标题语义；支持 {{body}} 引用通知项预填

	// Bark 官方参数（https://bark.day.app/#/tutorial）
	BarkSubtitle   string `json:"bark_subtitle"`
	BarkGroup      string `json:"bark_group"`
	BarkLevel      string `json:"bark_level"` // critical | active | timeSensitive | passive
	BarkSound      string `json:"bark_sound"`
	BarkIcon       string `json:"bark_icon"`
	BarkImage      string `json:"bark_image"`
	BarkURL        string `json:"bark_url"`
	BarkBadge      string `json:"bark_badge"`
	BarkMarkdown   string `json:"bark_markdown"`
	BarkCopy       string `json:"bark_copy"`
	BarkIsArchive  string `json:"bark_isarchive"`
	BarkTTL        string `json:"bark_ttl"`
	BarkDeviceKeys string `json:"bark_devicekeys"`
	BarkVolume     string `json:"bark_volume"`
	BarkCall       string `json:"bark_call"`
	BarkAutoCopy   string `json:"bark_autocopy"`
	BarkCiphertext string `json:"bark_ciphertext"`
	BarkAction     string `json:"bark_action"`
	BarkID         string `json:"bark_id"`
	BarkDelete     string `json:"bark_delete"`

	ServerChanEnabled bool   `json:"serverchan_enabled"`
	ServerChanKey     string `json:"serverchan_key"`
	ServerChanTags    string `json:"serverchan_tags"`    // SC3：标签列表，界面按逗号输入，发送时转 |
	ServerChanShort   string `json:"serverchan_short"`   // 消息卡片简短描述
	ServerChanNoIP    bool   `json:"serverchan_noip"`    // SCT：隐藏调用 IP
	ServerChanChannel string `json:"serverchan_channel"` // SCT：消息通道，界面按逗号输入，发送时转 |
	ServerChanOpenID  string `json:"serverchan_openid"`  // SCT：抄送 openid，官方接口即逗号分隔
	ServerChanTitle   string `json:"serverchan_title"`   // 渠道级标题，优先级高于通知项；支持 {{title}} 引用通知项预填，留空回退通知项
	ServerChanBody    string `json:"serverchan_body"`    // 渠道级正文，同标题语义；支持 {{body}} 引用通知项预填

	// 失败通知
	OnFail bool `json:"on_fail"`

	FailTitle string `json:"fail_title"`
	FailBody  string `json:"fail_body"`

	// 发送通知任务注入（任务 option 经 attach 顶层键合并写入）
	TaskTitle          string `json:"task_title"`           // 通知任务的标题模板
	TaskBody           string `json:"task_body"`            // 通知任务的内容模板
	SendTaskWebhook    bool   `json:"send_task_webhook"`    // 任务级渠道开关，默认 true=跟随设置
	SendTaskBark       bool   `json:"send_task_bark"`       // 任务级渠道开关，默认 true=跟随设置
	SendTaskServerChan bool   `json:"send_task_serverchan"` // 任务级渠道开关，默认 true=跟随设置
}

// ParseConfig 从节点 JSON（含 attach）解析通知配置。
func ParseConfig(nodeJSON string) (Config, error) {
	var node struct {
		Attach Config `json:"attach"`
	}
	if err := json.Unmarshal([]byte(nodeJSON), &node); err != nil {
		return Config{}, err
	}
	return node.Attach, nil
}

// Enabled 返回配置中是否有至少一个已启用的渠道。
func (c *Config) Enabled() bool {
	return c.WebhookEnabled || c.BarkEnabled || c.ServerChanEnabled
}

// BuildVars 构造内置模板变量。taskName 为任务入口名（entry），status 为本地化状态文本（如 失败/Failure）。
// startTime 为零值时跳过 duration 变量。
func BuildVars(taskName, status string, now, startTime time.Time) map[string]string {
	vars := map[string]string{
		"time":        now.Format("15:04:05"),
		"date":        now.Format("2006-01-02"),
		"datetime":    now.Format("2006-01-02 15:04:05"),
		"task_name":   taskName,
		"task_status": status,
	}
	if !startTime.IsZero() {
		vars["duration"] = now.Sub(startTime).Truncate(time.Second).String()
	}
	if name := pienv.ControllerName(); name != "" {
		vars["controller"] = name
	}
	if name := pienv.ResourceName(); name != "" {
		vars["resource"] = name
	}
	return vars
}

// ReplaceVars 把模板中的 {{key}} 替换为 vars 中的值，未识别的变量原样保留。
// 多轮替换直到收敛，避免 map 迭代顺序随机导致交叉引用结果不确定。
func ReplaceVars(template string, vars map[string]string) string {
	for i := 0; i < 10; i++ {
		prev := template
		for key, value := range vars {
			template = strings.ReplaceAll(template, "{{"+key+"}}", value)
		}
		if template == prev {
			return template
		}
	}
	return template
}

// ParseHeaders 解析请求头：优先按 JSON 对象（map[string]string）解析；
// 失败时回退按换行或 | 分隔的 "名称: 值" 文本格式。
func ParseHeaders(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}
	}
	var jsonHeaders map[string]string
	if err := json.Unmarshal([]byte(raw), &jsonHeaders); err == nil {
		return jsonHeaders
	}
	// JSON 解析失败：若输入意图明显是 JSON（以 { 开头），提示后回退文本格式
	if strings.HasPrefix(raw, "{") {
		log.Warn().Str("component", "Notify").Msg("headers JSON invalid, fall back to text format")
	}
	headers := make(map[string]string)
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '|' })
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, ":")
		if !ok {
			log.Warn().Str("component", "Notify").Str("line", part).Msg("invalid header line, expect \"Key: Value\"")
			continue
		}
		headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return headers
}

// checkStatus 检查响应状态码，>= 400 视为失败。
func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}

var urlInErrRegexp = regexp.MustCompile(`https?://[^\s"']+`)

// sanitizeError 把错误文本中的 URL 整体打码：http.Client 的错误串包含完整请求 URL，
// 可能携带渠道凭据（Bark key / ServerChan sendkey），不直接写入日志。
func sanitizeError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", urlInErrRegexp.ReplaceAllString(err.Error(), "<redacted url>"))
}

// postJSON 发送 JSON POST 并检查状态码；expectedCode >= 0 时解析 body 的 code 字段对比。
func postJSON(endpoint string, payload map[string]any, expectedCode int) error {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := httpClient.Post(endpoint, "application/json;charset=utf-8", strings.NewReader(string(jsonBody)))
	if err != nil {
		return sanitizeError(err)
	}
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return err
	}
	if expectedCode >= 0 {
		respBody, err := io.ReadAll(resp.Body)
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

// addIfPresent 非空字段做变量替换后加入 payload（Bark/ServerChan 参数收集共用）。
func addIfPresent(payload map[string]any, vars map[string]string, key, value string) {
	if v := ReplaceVars(strings.TrimSpace(value), vars); v != "" {
		payload[key] = v
	}
}

// pipeSeparated 把用户按逗号输入的多值列表统一为官方接口要求的 | 分隔。
func pipeSeparated(raw string) string {
	return strings.Join(splitList(raw, ',', '，', '、', '|'), "|")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// channelTitleBody 解析渠道级标题/正文：渠道配置优先（支持 {{title}}/{{body}} 引用通知项预填），留空回退 vars 预填内容。
func channelTitleBody(chTitle, chBody string, vars map[string]string) (string, string) {
	return ReplaceVars(firstNonEmpty(chTitle, vars["title"]), vars),
		ReplaceVars(firstNonEmpty(chBody, vars["body"]), vars)
}

// Send 遍历配置中所有已启用的渠道发送通知，任一渠道发送成功即返回 true；
// 全部失败返回 false（不影响调用方流程，仅记录日志）。
// 标题/正文优先级：渠道配置（bark_title 等，支持 {{title}}/{{body}} 引用通知项预填内容）> 通知项模板（vars["title"]/["body"]）> 默认标题。
// Webhook 不读取标题/正文，可在请求体里用 {{title}}/{{body}} 引用本次通知的标题/正文。
func Send(config Config, vars map[string]string) bool {
	if !config.Enabled() {
		log.Debug().Str("component", "Notify").Msg("no enabled channel, skip notify")
		return true
	}

	// 通知项预填内容：此时 vars 尚无 title/body，通知项模板里写 {{title}}/{{body}} 不会被替换（天然无效）
	prefillTitle := ReplaceVars(firstNonEmpty(vars["title"]), vars)
	prefillBody := ReplaceVars(firstNonEmpty(vars["body"]), vars)
	// 写回变量，供渠道标题/正文与 Webhook 请求体 {{title}}/{{body}} 引用
	vars["title"] = firstNonEmpty(prefillTitle, i18n.T("notify.default_title"))
	vars["body"] = firstNonEmpty(prefillBody, i18n.T("notify.default_body"))

	sent := false
	if config.WebhookEnabled {
		if err := sendWebhook(config, vars); err != nil {
			log.Error().Err(err).Str("component", "Notify").Str("channel", "webhook").Msg("webhook notify failed")
		} else {
			sent = true
		}
	}
	if config.BarkEnabled {
		title, body := channelTitleBody(config.BarkTitle, config.BarkBody, vars)
		if err := sendBark(config, title, body, vars); err != nil {
			log.Error().Err(err).Str("component", "Notify").Str("channel", "bark").Msg("bark notify failed")
		} else {
			sent = true
		}
	}
	if config.ServerChanEnabled {
		title, body := channelTitleBody(config.ServerChanTitle, config.ServerChanBody, vars)
		if err := sendServerChan(config, title, body, vars); err != nil {
			log.Error().Err(err).Str("component", "Notify").Str("channel", "serverchan").Msg("serverchan notify failed")
		} else {
			sent = true
		}
	}
	return sent
}

func sendWebhook(config Config, vars map[string]string) error {
	urlStr := ReplaceVars(strings.TrimSpace(config.WebhookURL), vars)
	if urlStr == "" {
		return fmt.Errorf("webhook url is empty")
	}
	method := strings.ToUpper(strings.TrimSpace(config.WebhookMethod))
	if method == "" {
		method = http.MethodPost
	}
	headers := ParseHeaders(ReplaceVars(config.WebhookHeaders, vars))
	body := ReplaceVars(config.WebhookBody, vars)

	req, err := http.NewRequest(method, urlStr, strings.NewReader(body))
	if err != nil {
		return sanitizeError(err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	log.Debug().
		Str("component", "Notify").
		Str("channel", "webhook").
		Str("method", method).
		Msg("sending webhook notify")
	resp, err := httpClient.Do(req)
	if err != nil {
		return sanitizeError(err)
	}
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return err
	}
	return nil
}

// barkEndpointDefault 构造 Bark 推送端点（POST JSON 到 https://api.day.app/{key}）。
func barkEndpointDefault(key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("bark key is empty")
	}
	return fmt.Sprintf("https://api.day.app/%s", url.PathEscape(strings.TrimSpace(key))), nil
}

// sendBark 按 Bark 官方文档发送 POST JSON，支持文档全部参数（非空才携带，均做变量替换）。
func sendBark(config Config, title, body string, vars map[string]string) error {
	endpoint, err := barkEndpoint(config.BarkKey)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"title": title,
		"body":  body,
	}
	for _, p := range []struct{ key, val string }{
		{"subtitle", config.BarkSubtitle},
		{"group", config.BarkGroup},
		{"level", config.BarkLevel},
		{"sound", config.BarkSound},
		{"icon", config.BarkIcon},
		{"image", config.BarkImage},
		{"url", config.BarkURL},
		{"markdown", config.BarkMarkdown},
		{"copy", config.BarkCopy},
		{"isArchive", config.BarkIsArchive},
		{"ttl", config.BarkTTL},
		{"device_keys", config.BarkDeviceKeys},
		{"volume", config.BarkVolume},
		{"call", config.BarkCall},
		{"autoCopy", config.BarkAutoCopy},
		{"ciphertext", config.BarkCiphertext},
		{"action", config.BarkAction},
		{"id", config.BarkID},
		{"delete", config.BarkDelete},
	} {
		addIfPresent(payload, vars, p.key, p.val)
	}
	// badge 为数字参数，纯数字时按整数发送
	if badge := ReplaceVars(strings.TrimSpace(config.BarkBadge), vars); badge != "" {
		if n, err := strconv.Atoi(badge); err == nil {
			payload["badge"] = n
		} else {
			payload["badge"] = badge
		}
	}
	log.Debug().Str("component", "Notify").Str("channel", "bark").Msg("sending bark notify")
	return postJSON(endpoint, payload, 200)
}

// serverChanEndpointDefault 根据 SendKey 前缀选择端点：
// sctp 开头为 ServerChan3（SC3），端点为 https://{uid}.push.ft07.com/send/{sendkey}.send（uid 从 sendkey 提取）；
// 其余为 ServerChan Turbo，端点为 https://sctapi.ftqq.com/{sendkey}.send。
func serverChanEndpointDefault(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("serverchan sendkey is empty")
	}
	if strings.HasPrefix(key, "sctp") {
		uid := sc3UID(key)
		return fmt.Sprintf("https://%s.push.ft07.com/send/%s.send", uid, url.PathEscape(key)), nil
	}
	return fmt.Sprintf("https://sctapi.ftqq.com/%s.send", url.PathEscape(key)), nil
}

// sc3UID 从 sctp{uid}t{token} 中提取 uid；无 t 分隔符时退化整体作 uid。
func sc3UID(key string) string {
	rest := key[4:]
	if idx := strings.Index(rest, "t"); idx >= 0 {
		return rest[:idx]
	}
	return rest
}

// sendServerChan 按官方 SDK 发送 POST JSON，支持 tags/short/noip/channel/openid 参数，
// 响应 code != 0 视为业务失败。
func sendServerChan(config Config, title, body string, vars map[string]string) error {
	endpoint, err := serverChanEndpoint(config.ServerChanKey)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"title": title,
		"desp":  body,
	}
	if tags := pipeSeparated(ReplaceVars(config.ServerChanTags, vars)); tags != "" {
		payload["tags"] = tags
	}
	addIfPresent(payload, vars, "short", config.ServerChanShort)
	if config.ServerChanNoIP {
		payload["noip"] = 1
	}
	if channel := pipeSeparated(ReplaceVars(config.ServerChanChannel, vars)); channel != "" {
		payload["channel"] = channel
	}
	addIfPresent(payload, vars, "openid", config.ServerChanOpenID)
	log.Debug().Str("component", "Notify").Str("channel", "serverchan").Msg("sending serverchan notify")
	return postJSON(endpoint, payload, 0)
}

// NotifySendAction 供 Pipeline 手动触发通知：读取 __NotifyConfig 节点 attach 中的
// 渠道配置并发送。通知失败不会导致 Pipeline 节点失败。
type NotifySendAction struct{}

var _ maa.CustomActionRunner = &NotifySendAction{}

func (a *NotifySendAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	raw, err := ctx.GetNodeJSON(defaultConfigNode)
	if err != nil {
		log.Error().Err(err).Str("component", "NotifySendAction").Str("node", defaultConfigNode).Msg("failed to get node json")
		return true
	}
	config, err := ParseConfig(raw)
	if err != nil {
		log.Error().Err(err).Str("component", "NotifySendAction").Str("node", defaultConfigNode).Msg("failed to parse notify config")
		return true
	}

	applyTaskToggle(&config)

	vars := BuildVars(arg.CurrentTaskName, "", time.Now(), time.Time{})
	vars["title"] = config.TaskTitle
	vars["body"] = config.TaskBody
	Send(config, vars)
	// 通知发送失败不影响游戏流程，始终返回成功
	return true
}

// applyTaskToggle 按任务级渠道开关覆盖本次发送渠道（attach 注入，默认 true=跟随设置启用）。
func applyTaskToggle(config *Config) {
	if !config.SendTaskWebhook {
		config.WebhookEnabled = false
	}
	if !config.SendTaskBark {
		config.BarkEnabled = false
	}
	if !config.SendTaskServerChan {
		config.ServerChanEnabled = false
	}
}
