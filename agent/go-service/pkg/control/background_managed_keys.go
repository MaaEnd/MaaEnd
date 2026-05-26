package control

import (
	"fmt"
	"unsafe"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type maaCtrlOption int32

const maaCtrlOptionBackgroundManagedKeys maaCtrlOption = 7

//go:linkname maaControllerSetOption github.com/MaaXYZ/maa-framework-go/v4/internal/native.MaaControllerSetOption
var maaControllerSetOption func(ctrl uintptr, key maaCtrlOption, value unsafe.Pointer, valSize uint64) bool

func setBackgroundManagedKeys(ctrl *maa.Controller, keys []int32) error {
	if ctrl == nil {
		return fmt.Errorf("nil controller")
	}
	if maaControllerSetOption == nil {
		return fmt.Errorf("MaaControllerSetOption is not loaded")
	}

	var value unsafe.Pointer
	var valueSize uint64
	if len(keys) > 0 {
		value = unsafe.Pointer(&keys[0])
		valueSize = uint64(unsafe.Sizeof(keys[0])) * uint64(len(keys))
	}
	if !maaControllerSetOption(maaControllerHandle(ctrl), maaCtrlOptionBackgroundManagedKeys, value, valueSize) {
		return fmt.Errorf("failed to set background managed keys")
	}
	return nil
}

func maaControllerHandle(ctrl *maa.Controller) uintptr {
	return *(*uintptr)(unsafe.Pointer(ctrl))
}
