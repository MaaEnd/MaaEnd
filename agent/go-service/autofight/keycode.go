package autofight

import (
	"fmt"
	"strconv"
	"strings"
)

// VirtualKeyCode 将按键字符串转换为 Win32 Virtual-Key Code。
//
// 支持的输入（大小写不敏感）：
//   - 数字键 "0"-"9"     → 0x30-0x39
//   - 字母键 "A"-"Z"     → 0x41-0x5A
//   - 功能键 "F1"-"F24"  → 0x70-0x87
//
// 返回的 code 与 https://learn.microsoft.com/en-us/windows/win32/inputdev/virtual-key-codes
// 中的取值保持一致，可直接传给 control 适配器的 KeyDown/KeyUp/KeyType。
//
// 输入为空或不在支持范围内时返回 error，code 为 0。
func VirtualKeyCode(key string) (int, error) {
	k := strings.ToUpper(strings.TrimSpace(key))
	if k == "" {
		return 0, fmt.Errorf("autofight: empty key")
	}

	if len(k) == 1 {
		c := k[0]
		switch {
		case c >= '0' && c <= '9':
			return int(c), nil
		case c >= 'A' && c <= 'Z':
			return int(c), nil
		}
	}

	if len(k) >= 2 && k[0] == 'F' {
		n, err := strconv.Atoi(k[1:])
		if err == nil && n >= 1 && n <= 24 {
			return 0x70 + n - 1, nil
		}
	}

	return 0, fmt.Errorf("autofight: unsupported key %q", key)
}
