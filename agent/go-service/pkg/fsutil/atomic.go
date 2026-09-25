// Package fsutil 提供通用的文件系统工具。
package fsutil

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

// WriteFileAtomic 以「写临时文件 + 重命名」的方式写入文件：
// 成功返回时目标文件不会停留在半截内容。
//
// 替换语义是平台相关的：Unix 上 os.Rename 对同目录替换是原子的；
// Windows 上 Go 标准库走 MoveFileExW(MOVEFILE_REPLACE_EXISTING)，
// 不保证原子性（替换瞬间读者可能观察到缺失状态）。本函数用于 debug
// 记录文件，接受该差异。
//
// 注意：本函数只保证「不出现半截内容」，不做目录 fsync，
// 因此不提供掉电后的持久性保证。
func WriteFileAtomic(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			if rmErr := os.Remove(tmpPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				log.Warn().
					Err(rmErr).
					Str("path", tmpPath).
					Msg("failed to remove temp file during atomic write cleanup")
			}
		}
	}()

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false

	return nil
}
