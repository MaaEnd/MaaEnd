package notify

import "unicode/utf8"

// 内容长度截断辅助：各渠道对正文有官方长度上限，超长时静默截断（不报错、不校验），
// 避免请求被服务端拒绝。按字符数（rune）或字节数截断，字节截断回退到 UTF-8 字符边界。

// truncateRunes 按字符数（rune）截断，超出部分静默丢弃。
func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

// truncateBytes 按字节数截断，回退到最近 UTF-8 字符边界，避免切断多字节字符。
func truncateBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}
