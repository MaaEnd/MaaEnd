package puzzle

import "github.com/MaaXYZ/maa-framework-go/v3"

// Register registers all custom recognition and action components for puzzle-solver package
func Register() {
	maa.AgentServerRegisterCustomRecognition("PuzzleRecognition", &Recognition{})
	maa.AgentServerRegisterCustomAction("PuzzleAction", &Action{})
}
