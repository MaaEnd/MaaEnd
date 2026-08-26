package notify

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// dingtalkConfig 是钉钉群机器人渠道的私有配置（attach 顶层 channel_dingtalk_* 键）。
type dingtalkConfig struct {
	Enabled  bool   `json:"channel_dingtalk_enabled"`
	UseProxy bool   `json:"channel_dingtalk_use_proxy"` // 是否走全局代理（配合全局 use_proxy 主开关）
	URL      string `json:"channel_dingtalk_url"`       // webhook 完整地址（含 access_token，不写入日志）
	Secret   string `json:"channel_dingtalk_secret"`    // 加签密钥（SEC 开头），可选；非空则计算 sign
	Title    string `json:"channel_dingtalk_title"`     // 渠道级标题，支持 {{title}} 引用通知项预填，留空回退通知项
	Body     string `json:"channel_dingtalk_body"`      // 渠道级正文，同标题语义；支持 {{body}} 引用通知项预填
	MsgType  string `json:"channel_dingtalk_msgtype"`   // text（默认）| markdown
	AtAll    bool   `json:"channel_dingtalk_at_all"`    // 是否 @所有人
}

// dingtalkEndpoint 端点构造函数为包级变量，便于测试注入本地服务器。
var dingtalkEndpoint = dingtalkEndpointDefault

// dingtalkChannel 钉钉群机器人渠道：向 webhook 推送消息。
// 文本类消息（text/markdown），标题+正文拼合；可选加签与 @所有人；
// 响应校验 errcode（0=成功），避免业务失败被 HTTP 200 掩盖。
type dingtalkChannel struct {
	cfg dingtalkConfig
}

func init() { RegisterChannel(dingtalkChannel{}) }

var _ ChannelFactory = dingtalkChannel{}
var _ Channel = dingtalkChannel{}

func (dingtalkChannel) Name() string {
	return "dingtalk"
}

func (dingtalkChannel) Create(attach map[string]any) (Channel, error) {
	var cfg dingtalkConfig
	if err := decodeAttach(attach, &cfg); err != nil {
		return nil, err
	}
	return dingtalkChannel{cfg: cfg}, nil
}

func (c dingtalkChannel) Enabled() bool {
	return c.cfg.Enabled
}

func (c dingtalkChannel) UseProxy() bool {
	return c.cfg.UseProxy
}

func (c dingtalkChannel) Send(ctx *SendContext) error {
	config := c.cfg
	vars := ctx.Vars
	endpoint, err := dingtalkEndpoint(config.URL, config.Secret)
	if err != nil {
		return err
	}

	title, body := channelTitleBody(config.Title, config.Body, vars)
	if body == "" {
		return fmt.Errorf("dingtalk content is empty")
	}

	msgType := strings.ToLower(strings.TrimSpace(config.MsgType))
	if msgType == "" {
		msgType = "text"
	}

	payload := map[string]any{"msgtype": msgType}
	if msgType == "markdown" {
		md := map[string]any{"text": body}
		if title != "" {
			md["title"] = title
		}
		payload["markdown"] = md
	} else {
		content := title
		if body != "" {
			if content != "" {
				content += "\n"
			}
			content += body
		}
		payload["text"] = map[string]any{"content": content}
	}
	if config.AtAll {
		payload["at"] = map[string]any{"isAtAll": true}
	}

	log.Debug().Str("component", "Notify").Str("channel", "dingtalk").Str("msgtype", msgType).Msg("sending dingtalk notify")
	return postDingTalk(ctx.Client, endpoint, payload)
}

// postDingTalk 发送钉钉消息并校验响应（HTTP < 400 且 errcode == 0）。
// 钉钉成功响应为 {"errcode":0,"errmsg":"ok"}，失败时 errcode 非 0 但 HTTP 仍 200，故单独实现。
func postDingTalk(client *http.Client, endpoint string, payload map[string]any) error {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := client.Post(endpoint, "application/json;charset=utf-8", bytes.NewReader(jsonBody))
	if err != nil {
		return sanitizeError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	// 限制响应体大小（1MB），防止服务端异常返回超大 body 吃内存
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse response body: %w", err)
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("dingtalk api error: %s (errcode %d)", result.ErrMsg, result.ErrCode)
	}
	return nil
}

// dingtalkEndpointDefault 校验并返回钉钉消息推送端点（完整 URL）。
// secret 非空时按官方加签规则追加 timestamp 与 sign 参数；
// 留空/非法协议直接报错，access_token 不进入日志（sanitizeError 对完整 URL 打码）。
func dingtalkEndpointDefault(webhookURL, secret string) (string, error) {
	webhookURL = ensureHTTPS(webhookURL)
	if webhookURL == "" {
		return "", fmt.Errorf("dingtalk webhook url is empty")
	}
	u, err := url.Parse(webhookURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid dingtalk webhook url")
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return webhookURL, nil
	}
	timestamp := time.Now().UnixMilli()
	sign := dingtalkSign(secret, timestamp)
	sep := "&"
	if !strings.Contains(webhookURL, "?") {
		sep = "?"
	}
	return webhookURL + sep + "timestamp=" + strconv.FormatInt(timestamp, 10) + "&sign=" + sign, nil
}

// dingtalkSign 计算钉钉自定义机器人加签签名：
// sign = urlEncode(Base64(HmacSHA256(secret, timestamp + "\n" + secret)))。
func dingtalkSign(secret string, timestamp int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	return url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}
