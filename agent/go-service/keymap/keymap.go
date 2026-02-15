package keymap

// Win32 Default settings
var Win32Keymap = map[string]int32{
	// General
	"Move_W":            87,  // W (Locked)
	"Move_A":            65,  // A (Locked)
	"Move_S":            83,  // S (Locked)
	"Move_D":            68,  // D (Locked)
	"Dash":              16,  // Shift
	"Jump":              32,  // Space
	"Interact":          70,  // F
	"Walk":              17,  // Ctrl (Locked)
	"Menu":              27,  // Esc (Locked)
	"Backpack":          66,  // B
	"Valuables":         78,  // N
	"Team":              85,  // U
	"Operator":          67,  // C
	"Mission":           74,  // J
	"TrackMission":      86,  // V
	"Map":               77,  // M
	"BackerChat":        72,  // H
	"Mail":              75,  // K
	"OperationalManual": 119, // F8
	"Headhunt":          120, // F9
	"SwitchModes":       9,   // Tab (Locked)
	"UseTools":          82,  // R
	"ExpandToolsWheel":  82,  // R (LongPress Key) (Followed "UseTools")
	// Combat
	"Attack":              1,              // Left Mouse Button (Locked)
	"LockToTarget":        4,              // Middle Mouse Button (Locked)
	"SwitchTarget":        unsupportedKey, // It's unsupported, please use the "Scroll" Action. (Locked)
	"CastCombo":           69,             // E
	"OperatorSkill_1":     49,             // 1
	"OperatorSkill_2":     50,             // 2
	"OperatorSkill_3":     51,             // 3
	"OperatorSkill_4":     52,             // 4
	"OperatorUltimate_1":  49,             // 1 (LongPress Key) (Followed "OperatorSkill_1")
	"OperatorUltimate_2":  50,             // 2 (LongPress Key) (Followed "OperatorSkill_2")
	"OperatorUltimate_3":  51,             // 3 (LongPress Key) (Followed "OperatorSkill_3")
	"OperatorUltimate_4":  52,             // 4 (LongPress Key) (Followed "OperatorSkill_4")
	"SwitchOperator_1":    112,            // F1
	"SwitchOperator_2":    113,            // F2
	"SwitchOperator_3":    114,            // F3
	"SwitchOperator_4":    115,            // F4
	"SwitchOperator_Next": 81,             // Q
	// AIC Factory
	"AICFactoryPlan":        84,  // T
	"TransportBelt":         69,  // E
	"Pipeline":              81,  // Q
	"FacilityList":          90,  // Z
	"TopViewMode":           20,  // CapsLock
	"StashMode":             88,  // X
	"RegionalDeployment":    89,  // Y
	"Blueprints":            112, // F1
	"Show/HideProductIcons": 115, // F4
}
