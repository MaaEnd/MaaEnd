package datacollect

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers temporary data-collection custom recognitions.
func Register() {
	maa.AgentServerRegisterCustomRecognition("DataCollectOcrTempFullScreenOCR", &DataCollectOcrTempFullScreenOCR{})
}
