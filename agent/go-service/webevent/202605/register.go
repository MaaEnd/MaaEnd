package webevent202605

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers all custom components for webevent202605.
func Register() {
	maa.AgentServerRegisterCustomAction("WebEvent202605Action", &WebEvent202605Action{})
}
