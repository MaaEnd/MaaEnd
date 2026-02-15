package keymap

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("km:Init", &KM_Init{})
	maa.AgentServerRegisterCustomAction("km:ClickKey", &KM_ClickKey{})
	maa.AgentServerRegisterCustomAction("km:LongPressKey", &KM_LongPressKey{})
	maa.AgentServerRegisterCustomAction("km:KeyDown", &KM_KeyDown{})
	maa.AgentServerRegisterCustomAction("km:KeyUp", &KM_KeyUp{})
}
