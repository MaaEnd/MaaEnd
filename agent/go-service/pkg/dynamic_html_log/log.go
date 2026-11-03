package dynamic_html_log

import (
	"strconv"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
)

const (
	templateKey = "dynamic_html_log.log"
)

var (
	idPrefix string
	logIndex = 0
	mu       sync.Mutex
)

func init() {
	// 初始化时生成一个id前缀，每个agent的id前缀独立
	idPrefix = time.Now().Format("20060102150405")
}

type DynamicHtmlLog struct {
	// 每个实例中的id一致且实例间隔离，用于实现旧日志隐藏
	// 将id写入到输出的html文本中，再使用css查询器隐藏非最后一条日志
	id string
	// 标记style tag是否已经输出，以实现多次输出日志时，只有第一次才会输出style
	// 避免每次输出日志都写一遍style，那样会很丑
	styleIsInit bool
}

type DynamicHtmlLogData struct {
	log     *DynamicHtmlLog
	class   string
	style   string
	message string
}

// New 生成一个日志实例
func New() *DynamicHtmlLog {
	mu.Lock()
	defer mu.Unlock()
	logIndex += 1
	return &DynamicHtmlLog{
		id: idPrefix + `-` + strconv.Itoa(logIndex), //唯一id
	}
}

func (log *DynamicHtmlLog) T(key string, args ...any) *DynamicHtmlLogData {
	return &DynamicHtmlLogData{
		log:     log,
		message: i18n.T(key, args...), //使用i18n转换key和参数为文本
	}
}

func (data *DynamicHtmlLogData) WithClass(class string) *DynamicHtmlLogData {
	// 给输出添加class，默认无class
	data.class = class
	return data
}

func (data *DynamicHtmlLogData) WithStyle(style string) *DynamicHtmlLogData {
	// 给输出添加style，默认无style
	// 添加style时注意适配暗色主题
	data.style = style
	return data
}

func (data *DynamicHtmlLogData) Print() {
	// 输出日志
	// 如果style tag尚未输出，加个锁
	if !data.log.styleIsInit {
		mu.Lock()
		defer mu.Unlock()
	}
	maafocus.PrintLargeContentTrimNewline(
		i18n.RenderHTML(templateKey, map[string]any{
			"id":          data.log.id,
			"styleIsInit": data.log.styleIsInit,
			"class":       data.class,
			"style":       data.style,
			"message":     data.message,
		}),
	)
	data.log.styleIsInit = true
}
