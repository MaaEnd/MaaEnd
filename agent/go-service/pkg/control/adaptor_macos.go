// Copyright (c) 2026 MaaEnd Contributors
package control

import (
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// macOSControlAdaptor reuses ordinary desktop pointer input while translating
// the Win32 virtual-key semantics exposed by ControlAdaptor into CGKeyCode.
// Camera rotation and player navigation are intentionally unavailable because
// MaaFramework's MacOS controller does not currently provide RelativeMove.
type macOSControlAdaptor struct {
	*desktopControlAdaptor
}

func newMacOSControlAdaptor(ctx *maa.Context, ctrl *maa.Controller, w, h int) *macOSControlAdaptor {
	return &macOSControlAdaptor{
		desktopControlAdaptor: newDefaultDesktopControlAdaptor(ctx, ctrl, w, h),
	}
}

func (mca *macOSControlAdaptor) KeyDown(keyCode int, delayMillis int) {
	macOSKeyCode, ok := mca.convertKey("KeyDown", keyCode)
	if !ok {
		return
	}
	mca.ctrl.PostKeyDown(int32(macOSKeyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (mca *macOSControlAdaptor) KeyUp(keyCode int, delayMillis int) {
	macOSKeyCode, ok := mca.convertKey("KeyUp", keyCode)
	if !ok {
		return
	}
	mca.ctrl.PostKeyUp(int32(macOSKeyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (mca *macOSControlAdaptor) KeyType(keyCode int, delayMillis int) {
	macOSKeyCode, ok := mca.convertKey("KeyType", keyCode)
	if !ok {
		return
	}
	mca.ctrl.PostClickKey(int32(macOSKeyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

func (mca *macOSControlAdaptor) convertKey(operation string, win32VK int) (int, bool) {
	macOSKeyCode, ok := macOSKeyCodeFromWin32VK(win32VK)
	if !ok {
		log.Error().
			Str("operation", operation).
			Int("win32_vk", win32VK).
			Msg("Unsupported Win32 virtual key for macOS controller")
	}
	return macOSKeyCode, ok
}

func (mca *macOSControlAdaptor) RotateCamera(dx, dy int) {
	log.Error().
		Int("dx", dx).
		Int("dy", dy).
		Msg("Relative camera movement is unavailable on the macOS controller")
}

func (mca *macOSControlAdaptor) SetPlayerMovement(_ PlayerMovement, _ PlayerMovementPolicy) {
	log.Error().Msg("Player navigation is unavailable on the macOS controller")
}

func (mca *macOSControlAdaptor) SetPlayerDirection(_ PlayerDirection) {
	log.Error().Msg("Player navigation is unavailable on the macOS controller")
}

func (mca *macOSControlAdaptor) PlayerPulseMove(_, _ time.Duration, _ PlayerMovement) {
	log.Error().Msg("Player navigation is unavailable on the macOS controller")
}

func (mca *macOSControlAdaptor) PlayerJump() {
	log.Error().Msg("Player navigation is unavailable on the macOS controller")
}

func (mca *macOSControlAdaptor) ResetCursor(_ CursorResetPolicy) {
	log.Error().Msg("Cursor reset for navigation is unavailable on the macOS controller")
}

func (mca *macOSControlAdaptor) AggressivelyResetPlayerMovement() {
	log.Error().Msg("Player navigation is unavailable on the macOS controller")
}

func macOSKeyCodeFromWin32VK(keyCode int) (int, bool) {
	macOSKeyCode, ok := win32VKToMacOSKeyCode[keyCode]
	return macOSKeyCode, ok
}

var win32VKToMacOSKeyCode = map[int]int{
	0x08: 0x33, 0x09: 0x30, 0x0D: 0x24, 0x10: 0x38, 0x11: 0x3B, 0x12: 0x3A,
	0x14: 0x39, 0x1B: 0x35, 0x20: 0x31, 0x21: 0x74, 0x22: 0x79, 0x23: 0x77,
	0x24: 0x73, 0x25: 0x7B, 0x26: 0x7E, 0x27: 0x7C, 0x28: 0x7D, 0x2E: 0x75,
	0x30: 0x1D, 0x31: 0x12, 0x32: 0x13, 0x33: 0x14, 0x34: 0x15, 0x35: 0x17,
	0x36: 0x16, 0x37: 0x1A, 0x38: 0x1C, 0x39: 0x19,
	0x41: 0x00, 0x42: 0x0B, 0x43: 0x08, 0x44: 0x02, 0x45: 0x0E, 0x46: 0x03,
	0x47: 0x05, 0x48: 0x04, 0x49: 0x22, 0x4A: 0x26, 0x4B: 0x28, 0x4C: 0x25,
	0x4D: 0x2E, 0x4E: 0x2D, 0x4F: 0x1F, 0x50: 0x23, 0x51: 0x0C, 0x52: 0x0F,
	0x53: 0x01, 0x54: 0x11, 0x55: 0x20, 0x56: 0x09, 0x57: 0x0D, 0x58: 0x07,
	0x59: 0x10, 0x5A: 0x06, 0x5B: 0x37,
	0x70: 0x7A, 0x71: 0x78, 0x72: 0x63, 0x73: 0x76, 0x74: 0x60, 0x75: 0x61,
	0x76: 0x62, 0x77: 0x64, 0x78: 0x65, 0x79: 0x6D, 0x7A: 0x67, 0x7B: 0x6F,
	0xA0: 0x38, 0xA1: 0x3C, 0xA2: 0x3B, 0xA3: 0x3E, 0xA4: 0x3A, 0xA5: 0x3D,
}
