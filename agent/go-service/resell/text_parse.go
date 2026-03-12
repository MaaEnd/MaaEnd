package resell

import "strconv"

func extractNumbersFromText(text string) (int, bool) {
	var digitsOnly []byte
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
