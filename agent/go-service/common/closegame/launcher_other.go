//go:build !windows

package closegame

func closeLaunchers() bool {
	for _, name := range launcherProcessNames {
		if !killByName(name) {
			return false
		}
	}
	return true
}
