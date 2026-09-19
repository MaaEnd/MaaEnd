package umbralmonument

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the monument selection and progress components.
func Register() {
	maa.AgentServerRegisterCustomAction("UmbralMonumentState", &StateAction{})
	maa.AgentServerRegisterCustomRecognition("UmbralMonumentSelect", &Selection{})
}
