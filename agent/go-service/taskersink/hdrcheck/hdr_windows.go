//go:build windows

package hdrcheck

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                          = windows.NewLazySystemDLL("user32.dll")
	procGetDisplayConfigBufferSizes = user32.NewProc("GetDisplayConfigBufferSizes")
	procQueryDisplayConfig          = user32.NewProc("QueryDisplayConfig")
	procDisplayConfigGetDeviceInfo  = user32.NewProc("DisplayConfigGetDeviceInfo")
)

// Display config flags
const (
	QDC_ALL_PATHS                  = 0x00000001
	QDC_ONLY_ACTIVE_PATHS          = 0x00000002
	QDC_DATABASE_CURRENT           = 0x00000004
	QDC_VIRTUAL_MODE_AWARE         = 0x00000010
	QDC_INCLUDE_HMD                = 0x00000020
	QDC_VIRTUAL_REFRESH_RATE_AWARE = 0x00000040
)

// DISPLAYCONFIG_DEVICE_INFO_TYPE
const (
	DISPLAYCONFIG_DEVICE_INFO_GET_SOURCE_NAME                = 1
	DISPLAYCONFIG_DEVICE_INFO_GET_TARGET_NAME                = 2
	DISPLAYCONFIG_DEVICE_INFO_GET_TARGET_PREFERRED_MODE      = 3
	DISPLAYCONFIG_DEVICE_INFO_GET_ADAPTER_NAME               = 4
	DISPLAYCONFIG_DEVICE_INFO_SET_TARGET_PERSISTENCE         = 5
	DISPLAYCONFIG_DEVICE_INFO_GET_TARGET_BASE_TYPE           = 6
	DISPLAYCONFIG_DEVICE_INFO_GET_SUPPORT_VIRTUAL_RESOLUTION = 7
	DISPLAYCONFIG_DEVICE_INFO_SET_SUPPORT_VIRTUAL_RESOLUTION = 8
	DISPLAYCONFIG_DEVICE_INFO_GET_ADVANCED_COLOR_INFO        = 9
	DISPLAYCONFIG_DEVICE_INFO_SET_ADVANCED_COLOR_STATE       = 10
	DISPLAYCONFIG_DEVICE_INFO_GET_SDR_WHITE_LEVEL            = 11
)

// LUID represents a locally unique identifier
type LUID struct {
	LowPart  uint32
	HighPart int32
}

// DISPLAYCONFIG_PATH_SOURCE_INFO contains source information for a path
type DISPLAYCONFIG_PATH_SOURCE_INFO struct {
	AdapterId   LUID
	Id          uint32
	ModeInfoIdx uint32
	StatusFlags uint32
}

// DISPLAYCONFIG_PATH_TARGET_INFO contains target information for a path
type DISPLAYCONFIG_PATH_TARGET_INFO struct {
	AdapterId        LUID
	Id               uint32
	ModeInfoIdx      uint32
	OutputTechnology uint32
	Rotation         uint32
	Scaling          uint32
	RefreshRate      DISPLAYCONFIG_RATIONAL
	ScanLineOrdering uint32
	TargetAvailable  int32
	StatusFlags      uint32
}

// DISPLAYCONFIG_RATIONAL represents a rational number
type DISPLAYCONFIG_RATIONAL struct {
	Numerator   uint32
	Denominator uint32
}

// DISPLAYCONFIG_PATH_INFO contains information about a display path
type DISPLAYCONFIG_PATH_INFO struct {
	SourceInfo DISPLAYCONFIG_PATH_SOURCE_INFO
	TargetInfo DISPLAYCONFIG_PATH_TARGET_INFO
	Flags      uint32
}

// DISPLAYCONFIG_MODE_INFO contains mode information
type DISPLAYCONFIG_MODE_INFO struct {
	InfoType     uint32
	Id           uint32
	AdapterId    LUID
	ModeInfoData [64]byte // Union, we don't need to parse this
}

// DISPLAYCONFIG_DEVICE_INFO_HEADER is the header for device info requests
type DISPLAYCONFIG_DEVICE_INFO_HEADER struct {
	Type      uint32
	Size      uint32
	AdapterId LUID
	Id        uint32
}

// DISPLAYCONFIG_GET_ADVANCED_COLOR_INFO contains advanced color information
type DISPLAYCONFIG_GET_ADVANCED_COLOR_INFO struct {
	Header              DISPLAYCONFIG_DEVICE_INFO_HEADER
	Value               uint32 // Bit flags: advancedColorSupported, advancedColorEnabled, wideColorEnforced, advancedColorForceDisabled
	ColorEncoding       uint32
	BitsPerColorChannel uint32
}

// Windows error codes
const (
	ERROR_INSUFFICIENT_BUFFER = 122
)

// Maximum retry attempts for display config query
const maxRetries = 3

// IsHDREnabled checks if HDR is enabled on any display
func IsHDREnabled() (bool, error) {
	// Check if required APIs are available (may not exist on older Windows)
	if err := procGetDisplayConfigBufferSizes.Find(); err != nil {
		return false, err
	}
	if err := procQueryDisplayConfig.Find(); err != nil {
		return false, err
	}
	if err := procDisplayConfigGetDeviceInfo.Find(); err != nil {
		return false, err
	}

	// Query display config with retry logic for topology changes
	pathArray, err := queryDisplayConfigWithRetry()
	if err != nil {
		return false, err
	}

	if len(pathArray) == 0 {
		return false, nil
	}

	// Check each active path for HDR
	var successCount int
	for i := range pathArray {
		colorInfo := DISPLAYCONFIG_GET_ADVANCED_COLOR_INFO{
			Header: DISPLAYCONFIG_DEVICE_INFO_HEADER{
				Type:      DISPLAYCONFIG_DEVICE_INFO_GET_ADVANCED_COLOR_INFO,
				Size:      uint32(unsafe.Sizeof(DISPLAYCONFIG_GET_ADVANCED_COLOR_INFO{})),
				AdapterId: pathArray[i].TargetInfo.AdapterId,
				Id:        pathArray[i].TargetInfo.Id,
			},
		}

		ret, _, _ := procDisplayConfigGetDeviceInfo.Call(
			uintptr(unsafe.Pointer(&colorInfo)),
		)
		if ret != 0 {
			// Skip this display if we can't get info
			continue
		}
		successCount++

		// Check if HDR is enabled (bit 1 of Value field)
		// Bit 0: advancedColorSupported
		// Bit 1: advancedColorEnabled (HDR is ON)
		// Bit 2: wideColorEnforced
		// Bit 3: advancedColorForceDisabled
		advancedColorEnabled := (colorInfo.Value & 0x2) != 0
		if advancedColorEnabled {
			return true, nil
		}
	}

	// If we couldn't query any display successfully, return error
	if successCount == 0 {
		return false, windows.ERROR_GEN_FAILURE
	}

	return false, nil
}

// queryDisplayConfigWithRetry queries display config with retry on ERROR_INSUFFICIENT_BUFFER
func queryDisplayConfigWithRetry() ([]DISPLAYCONFIG_PATH_INFO, error) {
	for attempt := 0; attempt < maxRetries; attempt++ {
		var numPathArrayElements, numModeInfoArrayElements uint32

		// Get buffer sizes
		ret, _, _ := procGetDisplayConfigBufferSizes.Call(
			uintptr(QDC_ONLY_ACTIVE_PATHS),
			uintptr(unsafe.Pointer(&numPathArrayElements)),
			uintptr(unsafe.Pointer(&numModeInfoArrayElements)),
		)
		if ret != 0 {
			return nil, windows.Errno(ret)
		}

		if numPathArrayElements == 0 {
			return nil, nil
		}

		// Allocate arrays
		pathArray := make([]DISPLAYCONFIG_PATH_INFO, numPathArrayElements)

		// Prepare pointers for QueryDisplayConfig
		// modeInfoArray can be empty, need to handle nil case to avoid panic
		var modeInfoPtr unsafe.Pointer
		if numModeInfoArrayElements > 0 {
			modeInfoArray := make([]DISPLAYCONFIG_MODE_INFO, numModeInfoArrayElements)
			modeInfoPtr = unsafe.Pointer(&modeInfoArray[0])
		}

		// Query display config
		ret, _, _ = procQueryDisplayConfig.Call(
			uintptr(QDC_ONLY_ACTIVE_PATHS),
			uintptr(unsafe.Pointer(&numPathArrayElements)),
			uintptr(unsafe.Pointer(&pathArray[0])),
			uintptr(unsafe.Pointer(&numModeInfoArrayElements)),
			uintptr(modeInfoPtr),
			0,
		)
		if ret == 0 {
			// Trim pathArray to actual count (may be less than allocated)
			return pathArray[:numPathArrayElements], nil
		}
		if ret != ERROR_INSUFFICIENT_BUFFER {
			return nil, windows.Errno(ret)
		}
		// Topology changed, retry with new buffer sizes
	}

	return nil, windows.ERROR_INSUFFICIENT_BUFFER
}
