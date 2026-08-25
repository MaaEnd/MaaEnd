package notify

import (
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
)

// 模板变量与渠道内容解析的公共辅助函数，供调度层与各渠道复用。

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

// addIfPresent 非空字段做变量替换后加入 payload（Bark/ServerChan 参数收集共用）。
func addIfPresent(payload map[string]any, vars map[string]string, key, value string) {
	if v := ReplaceVars(strings.TrimSpace(value), vars); v != "" {
		payload[key] = v
	}
}

// firstNonEmpty 返回第一个非空白字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// channelTitleBody 解析渠道级标题/正文：渠道配置优先（支持 {{title}}/{{body}}
// 引用通知项预填），留空回退 vars 预填内容。
func channelTitleBody(chTitle, chBody string, vars map[string]string) (string, string) {
	return ReplaceVars(firstNonEmpty(chTitle, vars["title"]), vars),
		ReplaceVars(firstNonEmpty(chBody, vars["body"]), vars)
}
