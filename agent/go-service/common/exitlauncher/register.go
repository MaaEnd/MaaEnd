//go:build linux

package exitlauncher

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the Hypergryph launcher exit action.
func Register() {
	maa.AgentServerRegisterCustomAction("ExitGameLauncherAction", &ExitGameLauncherAction{})
}
