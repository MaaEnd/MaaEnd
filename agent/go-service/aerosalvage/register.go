package aerosalvage

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the Aerial Salvage custom recognition.
func Register() {
	maa.AgentServerRegisterCustomRecognition("AeroSalvageRecognition", &Recognition{})
}
