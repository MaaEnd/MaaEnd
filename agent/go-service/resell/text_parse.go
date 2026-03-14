package resell

import "strconv"

// extractIntegerFromText 从文本中提取所有连续的数字字符，并转换为整数。
// 返回提取到的整数和是否成功转换。
func extractIntegerFromText(text string) (int, bool) {
	digitsOnly := make([]byte, 0, len(text))
	for i := 0; i < len(text); i++ {
		if text[i] >= '0' && text[i] <= '9' {
			digitsOnly = append(digitsOnly, text[i])
		}
	}

	if len(digitsOnly) > 0 {
		if num, err := strconv.Atoi(string(digitsOnly)); err == nil {
			return num, true
		}
	}

	return 0, false
}
