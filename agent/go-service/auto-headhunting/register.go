package autoheadhunting

import "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("AutoHeadhunting", &AutoHeadhunting{})
}
