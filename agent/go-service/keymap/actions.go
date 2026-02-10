package keymap

import (
	"encoding/json"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	unsupportedKey = -1
	invalidKey     = -2
)

var keymap map[string]int32 = Win32Keymap

// Transfer key string to key code.
//
// Params:
//   - key: The key string, e.g. "Move_W", "Jump", "Attack", etc.
//
// Returns:
//   - KeyCode(int32): The corresponding key code for the given key string if the key is supported.
//   - -1: If the key string is unsupported.
//   - -2: If the key string is invalid.
func GetKeyCode(key string) int32 {

	var keyCode, ok = keymap[key]
	if !ok {
		log.Error().Msgf("Invalid key: %s", key)
		return invalidKey
	}
	if keyCode == unsupportedKey {
		log.Error().Msgf("Unsupported key: %s", key)
	}

	return keyCode
}

type KM_Init struct{}

func (a *KM_Init) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params map[string]int32
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse CustomActionParam")
		return false
	}

	keymap = params

	log.Info().Msg("Keymap initialized successfully!")
	log.Info().Interface("keymap", keymap).Msg("Current keymap")

	return true
}

type KM_ClickKey struct{}

func (a *KM_ClickKey) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Key string `json:"key"`
	}

	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse CustomActionParam")
		return false
	}

	key := GetKeyCode(params.Key)
	if key < 0 {
		return false
	}

	return ctx.GetTasker().GetController().PostClickKey(key).Wait().Done()
}

type KM_LongPressKey struct{}

func (a *KM_LongPressKey) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Key      string `json:"key"`
		Duration int32  `json:"duration"`
	}

	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse CustomActionParam")
		return false
	}

	key := GetKeyCode(params.Key)
	if key < 0 {
		return false
	}

	var ctrl = ctx.GetTasker().GetController()
	if !ctrl.PostKeyDown(key).Wait().Done() {
		return false
	}

	time.Sleep(time.Duration(params.Duration) * time.Millisecond)

	return ctrl.PostKeyUp(key).Wait().Done()
}

type KM_KeyDown struct{}

func (a *KM_KeyDown) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Key string `json:"key"`
	}

	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse CustomActionParam")
		return false
	}

	key := GetKeyCode(params.Key)
	if key < 0 {
		return false
	}

	return ctx.GetTasker().GetController().PostKeyDown(key).Wait().Done()
}

type KM_KeyUp struct{}

func (a *KM_KeyUp) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Key string `json:"key"`
	}

	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse CustomActionParam")
		return false
	}

	key := GetKeyCode(params.Key)
	if key < 0 {
		return false
	}

	return ctx.GetTasker().GetController().PostKeyUp(key).Wait().Done()
}
