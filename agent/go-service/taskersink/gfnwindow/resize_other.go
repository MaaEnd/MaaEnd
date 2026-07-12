//go:build !windows

package gfnwindow

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// ensureGFNWindowResolution is unsupported outside Windows: the GFN native
// client only ships for Windows, so the sink degrades to a debug log.
func ensureGFNWindowResolution(_ *maa.Controller) (*resizeResult, error) {
	return nil, errUnsupportedPlatform
}
