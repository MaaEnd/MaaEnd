package pienv

import (
	"encoding/json"
	"os"
	"sync"

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

type Win32Config struct {
	ClassRegex  string `json:"class_regex,omitempty"`
	WindowRegex string `json:"window_regex,omitempty"`
	Screencap   string `json:"screencap,omitempty"`
	Mouse       string `json:"mouse,omitempty"`
	Keyboard    string `json:"keyboard,omitempty"`
}

type MacOSConfig struct {
	TitleRegex string `json:"title_regex,omitempty"`
	Screencap  string `json:"screencap,omitempty"`
	Input      string `json:"input,omitempty"`
}

type PlayCoverConfig struct {
	UUID string `json:"uuid,omitempty"`
}

type GamepadConfig struct {
	ClassRegex  string `json:"class_regex,omitempty"`
	WindowRegex string `json:"window_regex,omitempty"`
	GamepadType string `json:"gamepad_type,omitempty"`
	Screencap   string `json:"screencap,omitempty"`
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
	WlRoots            json.RawMessage  `json:"wlroots,omitempty"`
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

// Init reads and parses all PI_* environment variables into the global singleton.
// Call once at startup, before modules that depend on PI context.
func Init() {
	once.Do(func() {
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
				log.Warn().Err(err).Msg("pienv: failed to parse PI_CONTROLLER")
			} else {
				env.Controller = &ctrl
			}
		}

		if env.ResourceRaw != "" {
			var res Resource
			if err := json.Unmarshal([]byte(env.ResourceRaw), &res); err != nil {
				log.Warn().Err(err).Msg("pienv: failed to parse PI_RESOURCE")
			} else {
				env.Resource = &res
			}
		}

		global = env

		le := log.Info().
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
	})
}

// Get returns the global Env. Returns nil if Init has not been called.
func Get() *Env {
	return global
}

// ---- Convenience accessors (nil-safe) ----

func InterfaceVersion() string {
	if global == nil {
		return ""
	}
	return global.InterfaceVersion
}

func ClientName() string {
	if global == nil {
		return ""
	}
	return global.ClientName
}

func ClientVersion() string {
	if global == nil {
		return ""
	}
	return global.ClientVersion
}

func ClientLanguage() string {
	if global == nil {
		return ""
	}
	return global.ClientLanguage
}

func ClientMaaFWVersion() string {
	if global == nil {
		return ""
	}
	return global.ClientMaaFWVersion
}

func ProjectVersion() string {
	if global == nil {
		return ""
	}
	return global.Version
}

func GetController() *Controller {
	if global == nil {
		return nil
	}
	return global.Controller
}

func GetResource() *Resource {
	if global == nil {
		return nil
	}
	return global.Resource
}

func ControllerType() string {
	if c := GetController(); c != nil {
		return c.Type
	}
	return ""
}

func ControllerName() string {
	if c := GetController(); c != nil {
		return c.Name
	}
	return ""
}

func ResourceName() string {
	if r := GetResource(); r != nil {
		return r.Name
	}
	return ""
}
