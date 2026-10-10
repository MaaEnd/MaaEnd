package giftoperator

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers gift recipient recognition and per-task bookkeeping.
func Register() {
	maa.AgentServerRegisterCustomAction("GiftOperatorSessionAction", &SessionAction{})
	maa.AgentServerRegisterCustomRecognition("GiftOperatorCandidateRecognition", &CandidateRecognition{})
	maa.AgentServerRegisterCustomRecognition("GiftOperatorDialogueRecognition", &DialogueRecognition{})
	maa.AgentServerRegisterCustomRecognition("GiftOperatorGiftStatusRecognition", &StatusRecognition{})
	maa.AgentServerRegisterCustomRecognition("GiftOperatorGiftSelectionRecognition", &SelectionRecognition{})
}
