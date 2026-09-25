//go:build linux

package ziplineimport

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// newTempProfile 创建本次运行专用的临时 profile 目录（运行结束即删，不保留任何登录态）。
func newTempProfile() (string, error) {
	dir, err := os.MkdirTemp("", "zipline-profile-")
	if err != nil {
		return "", fmt.Errorf("create temp profile: %w", err)
	}
	return dir, nil
}

// prepareProfile 往临时 profile 写入「仅目标域名走本机代理」的 PAC 配置、注入一次性自签 CA
// （certutil 写 NSS 证书库），并把其余偏好设好。
func prepareProfile(dir string, pacURL, caPemPath string) error {
	userJS := fmt.Sprintf(`user_pref("network.proxy.type", 2);
user_pref("network.proxy.autoconfig_url", %q);
user_pref("browser.sessionstore.resume_from_crash", false);
`, pacURL)
	if err := os.WriteFile(filepath.Join(dir, "user.js"), []byte(userJS), 0o600); err != nil {
		return fmt.Errorf("write user.js: %w", err)
	}

	// 确保 NSS 证书库存在（空目录需先初始化），再注入一次性 CA。
	initNssDB(dir)
	cmd := exec.Command("certutil", "-A", "-n", "MaaEnd ZiplineImport CA",
		"-t", "C,,", "-d", "sql:"+dir, "-i", caPemPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("certutil -A: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// initNssDB 若证书库尚未创建则先用空密码初始化，避免 certutil -A 找不到库。
func initNssDB(dir string) {
	db := filepath.Join(dir, "cert9.db")
	if _, err := os.Stat(db); err == nil {
		return
	}
	cmd := exec.Command("certutil", "-N", "-d", "sql:"+dir, "--empty-password")
	_ = cmd.Run()
}
