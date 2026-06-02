//go:build !windows

package achievement

import "os"

func replaceFile(src string, dst string) error {
	return os.Rename(src, dst)
}
