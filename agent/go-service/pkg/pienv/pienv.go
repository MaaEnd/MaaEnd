package pienv

import (
	"encoding/json"
	"os"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// PI v2.5.0 environment variable keys.
const (
	EnvInterfaceVersion   = "PI_INTERFACE_VERSION"
	EnvClientName         = "PI_CLIENT_NAME"
	EnvClientVersion      = "PI_CLIENT_VERSION"
	EnvClientLanguage     = "PI_CLIENT_LANGUAGE"
	EnvClientMaaFWVersion = "PI_CLIENT_MAAFW_VERSION"
	EnvVersion            = "PI_VERSION"
	EnvController         = "PI_CONTROLLER"
	EnvResource           = "PI_RESOURCE"
)

// ---- Controller sub-object types (per PI protocol) ----

// Win32Config holds Win32 controller-specific fields (class/window regex, screencap and input methods).
type Win32Config struct {
	ClassRegex  string `json:"class_regex,omitempty"`
	WindowRegex string `json:"window_regex,omitempty"`
	Screencap   string `json:"screencap,omitempty"`
	Mouse       string `json:"mouse,omitempty"`
	Keyboard    string `json:"keyboard,omitempty"`
}

// MacOSConfig holds macOS controller-specific fields (title regex, screencap and input methods).
type MacOSConfig struct {
	TitleRegex string `json:"title_regex,omitempty"`
	Screencap  string `json:"screencap,omitempty"`
	Input      string `json:"input,omitempty"`
}

// PlayCoverConfig holds PlayCover (macOS) controller-specific fields.
type PlayCoverConfig struct {
	UUID string `json:"uuid,omitempty"`
}

// GamepadConfig holds Gamepad (Windows, requires ViGEm) controller-specific fields.
type GamepadConfig struct {
	ClassRegex  string `json:"class_regex,omitempty"`
	WindowRegex string `json:"window_regex,omitempty"`
	GamepadType string `json:"gamepad_type,omitempty"`
	Screencap   string `json:"screencap,omitempty"`
}

// LinuxConfig holds Linux controller-specific fields (screencap/input methods,
// keycode handling and PipeWire source).
type LinuxConfig struct {
	Screencap      string `json:"screencap,omitempty"`
	Input          string `json:"input,omitempty"`
	UseWin32VKCode bool   `json:"use_win32_vk_code,omitempty"`
	PipewireSource string `json:"pipewire_source,omitempty"`
}

// Controller is the parsed PI_CONTROLLER single-line JSON.
// i18n-capable fields (label, description, icon) are pre-resolved by the Client.
type Controller struct {
	Name               string           `json:"name"`
	Label              string           `json:"label,omitempty"`
	Description        string           `json:"description,omitempty"`
	Icon               string           `json:"icon,omitempty"`
	Type               string           `json:"type"`
	DisplayShortSide   *int             `json:"display_short_side,omitempty"`
	DisplayLongSide    *int             `json:"display_long_side,omitempty"`
	DisplayRaw         *bool            `json:"display_raw,omitempty"`
	PermissionRequired bool             `json:"permission_required,omitempty"`
	AttachResourcePath []string         `json:"attach_resource_path,omitempty"`
	Option             []string         `json:"option,omitempty"`
	Win32              *Win32Config     `json:"win32,omitempty"`
	Adb                json.RawMessage  `json:"adb,omitempty"`
	MacOS              *MacOSConfig     `json:"macos,omitempty"`
	PlayCover          *PlayCoverConfig `json:"playcover,omitempty"`
	Gamepad            *GamepadConfig   `json:"gamepad,omitempty"`
	Linux              *LinuxConfig     `json:"linux,omitempty"`
}

// Resource is the parsed PI_RESOURCE single-line JSON.
// i18n-capable fields (label, description, icon) are pre-resolved by the Client.
type Resource struct {
	Name        string   `json:"name"`
	Label       string   `json:"label,omitempty"`
	Description string   `json:"description,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	Path        []string `json:"path"`
	Controller  []string `json:"controller,omitempty"`
	Option      []string `json:"option,omitempty"`
}

// Env holds all parsed PI_* environment variables (PI v2.5.0).
type Env struct {
	InterfaceVersion   string
	ClientName         string
	ClientVersion      string
	ClientLanguage     string
	ClientMaaFWVersion string
	Version            string

	Controller    *Controller
	ControllerRaw string
	Resource      *Resource
	ResourceRaw   string
}

var (
	global *Env
	once   sync.Once
)

func doInit() {
	env := &Env{
		InterfaceVersion:   os.Getenv(EnvInterfaceVersion),
		ClientName:         os.Getenv(EnvClientName),
		ClientVersion:      os.Getenv(EnvClientVersion),
		ClientLanguage:     os.Getenv(EnvClientLanguage),
		ClientMaaFWVersion: os.Getenv(EnvClientMaaFWVersion),
		Version:            os.Getenv(EnvVersion),
		ControllerRaw:      os.Getenv(EnvController),
		ResourceRaw:        os.Getenv(EnvResource),
	}

	if env.ControllerRaw != "" {
		var ctrl Controller
		if err := json.Unmarshal([]byte(env.ControllerRaw), &ctrl); err != nil {
			log.Warn().Err(err).
				Str("component", "pienv").
				Str("env_key", EnvController).
				Msg("failed to parse env")
		} else {
			env.Controller = &ctrl
		}
	}

	if env.ResourceRaw != "" {
		var res Resource
		if err := json.Unmarshal([]byte(env.ResourceRaw), &res); err != nil {
			log.Warn().Err(err).
				Str("component", "pienv").
				Str("env_key", EnvResource).
				Msg("failed to parse env")
		} else {
			env.Resource = &res
		}
	}

	global = env

	le := log.Info().
		Str("component", "pienv").
		Str("interface_version", env.InterfaceVersion).
		Str("client_name", env.ClientName).
		Str("client_version", env.ClientVersion).
		Str("client_language", env.ClientLanguage).
		Str("client_maafw_version", env.ClientMaaFWVersion).
		Str("pi_version", env.Version).
		Bool("controller_ok", env.Controller != nil).
		Bool("resource_ok", env.Resource != nil)

	if env.Controller != nil {
		le = le.Str("ctrl_name", env.Controller.Name).
			Str("ctrl_type", env.Controller.Type)
	}
	if env.Resource != nil {
		le = le.Str("res_name", env.Resource.Name)
	}

	le.Msg("PI environment initialized")
}

// Init reads and parses all PI_* environment variables into the global singleton.
// Safe to call multiple times; only the first call performs actual initialization.
func Init() {
	once.Do(doInit)
}

// Get returns the global Env, initializing on first access if needed.
// The underlying sync.Once ensures no data race between Init and Get.
func Get() *Env {
	once.Do(doInit)
	return global
}

// ---- Convenience accessors ----

// InterfaceVersion returns the PI_INTERFACE_VERSION value (e.g. "v2.5.0").
func InterfaceVersion() string { return Get().InterfaceVersion }

// ClientName returns the PI_CLIENT_NAME value (e.g. "MFAA", "MXU").
func ClientName() string { return Get().ClientName }

// ClientVersion returns the PI_CLIENT_VERSION value.
func ClientVersion() string { return Get().ClientVersion }

// ClientLanguage returns the PI_CLIENT_LANGUAGE value (e.g. "zh_cn", "en_us").
func ClientLanguage() string { return Get().ClientLanguage }

// ClientMaaFWVersion returns the PI_CLIENT_MAAFW_VERSION value.
func ClientMaaFWVersion() string { return Get().ClientMaaFWVersion }

// ProjectVersion returns the PI_VERSION value (resource project version from interface.json).
func ProjectVersion() string { return Get().Version }

// GetController returns the parsed Controller, or nil if PI_CONTROLLER was absent or empty.
func GetController() *Controller { return Get().Controller }

// GetResource returns the parsed Resource, or nil if PI_RESOURCE was absent or empty.
func GetResource() *Resource { return Get().Resource }

// ControllerType returns the controller type (e.g. "Win32", "Adb"), or empty if unavailable.
func ControllerType() string {
	if c := GetController(); c != nil {
		return c.Type
	}
	return ""
}

// ControllerName returns the controller name identifier, or empty if unavailable.
func ControllerName() string {
	if c := GetController(); c != nil {
		return c.Name
	}
	return ""
}

/* ******** Controller Kind ******** */

// Normalized controller kinds. Both the raw type injected by the Client
// (PI_CONTROLLER's type, i.e. interface.json's controller.type) and the raw type
// reported by MaaFramework (controller.GetInfo()) are normalized to these values
// before any comparison, so callers never hand-roll case/alias handling.
const (
	// ControllerKindUnknown means the controller type is unknown: the PI environment is
	// absent, or the raw type is not in the known set.
	ControllerKindUnknown = ""
	// ControllerKindAdb / ControllerKindPlayCover / ControllerKindNativeAndroid all use
	// touch input with the mobile UI. Use IsAdbLikeControllerType when they share a branch.
	ControllerKindAdb           = "adb"
	ControllerKindPlayCover     = "playcover"
	ControllerKindNativeAndroid = "native_android"
	ControllerKindWin32         = "win32"
	ControllerKindMacOS         = "macos"
	ControllerKindLinux         = "linux"
	ControllerKindGamepad       = "gamepad"
)

// ControllerKind returns the normalized kind of the current PI environment,
// or ControllerKindUnknown when PI_CONTROLLER is absent or of an unknown type.
func ControllerKind() string {
	return NormalizeControllerKind(ControllerType())
}

// NormalizeControllerKind maps a raw controller type to its normalized kind,
// or ControllerKindUnknown if it cannot be recognized.
func NormalizeControllerKind(controllerType string) string {
	switch strings.ToLower(strings.TrimSpace(controllerType)) {
	case ControllerKindAdb:
		return ControllerKindAdb
	// play_cover is an accepted spelling of PlayCover in some clients.
	case ControllerKindPlayCover, "play_cover":
		return ControllerKindPlayCover
	case ControllerKindNativeAndroid:
		return ControllerKindNativeAndroid
	case ControllerKindWin32:
		return ControllerKindWin32
	case ControllerKindMacOS:
		return ControllerKindMacOS
	case ControllerKindLinux:
		return ControllerKindLinux
	case ControllerKindGamepad:
		return ControllerKindGamepad
	default:
		return ControllerKindUnknown
	}
}

// IsAdbLikeControllerType reports whether the raw controller type uses touch input and
// the mobile UI: ADB, PlayCover, and the Android native controller (MaaFwApp on phone).
func IsAdbLikeControllerType(controllerType string) bool {
	switch NormalizeControllerKind(controllerType) {
	case ControllerKindAdb, ControllerKindPlayCover, ControllerKindNativeAndroid:
		return true
	default:
		return false
	}
}

// ResolveControllerKind returns the controller kind for the current callback: PI_CONTROLLER
// first, then controller.GetInfo() as a fallback. The PI protocol lets a Client omit
// variables it cannot provide, so callers must not assume PI_CONTROLLER exists — without the
// fallback an ADB controller would degrade into an unknown/desktop one.
//
// ctrl must be the controller already obtained and passed down inside the current callback;
// never call GetController() just to feed this function (see the coding standards on handle
// lifetime). A nil ctrl is fine: it simply yields ControllerKindUnknown when PI_CONTROLLER is
// unavailable too.
func ResolveControllerKind(ctrl *maa.Controller) string {
	if kind := ControllerKind(); kind != ControllerKindUnknown {
		return kind
	}
	if ctrl == nil {
		return ControllerKindUnknown
	}
	info, err := ctrl.GetInfo()
	if err != nil {
		return ControllerKindUnknown
	}
	return controllerKindFromInfo(info)
}

// controllerKindFromInfo parses MaaFramework's controller info (the JSON behind
// controller.GetInfo()). It probes the raw string when info is not valid JSON, and returns
// ControllerKindUnknown when neither recognizes the type. Split out from ResolveControllerKind
// so it can be tested without a real controller.
func controllerKindFromInfo(info string) string {
	if info == "" {
		return ControllerKindUnknown
	}

	var dto struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(info), &dto); err != nil {
		// Fallback: probe the raw string when it is not valid JSON.
		lower := strings.ToLower(info)
		for _, candidate := range []string{
			ControllerKindWin32,
			ControllerKindMacOS,
			ControllerKindLinux,
			ControllerKindPlayCover,
			ControllerKindNativeAndroid,
			ControllerKindAdb,
		} {
			if strings.Contains(lower, candidate) {
				return candidate
			}
		}
		return ControllerKindUnknown
	}
	return NormalizeControllerKind(dto.Type)
}

// ResourceName returns the resource name identifier, or empty if unavailable.
func ResourceName() string {
	if r := GetResource(); r != nil {
		return r.Name
	}
	return ""
}
