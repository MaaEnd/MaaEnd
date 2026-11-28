package resource

import (
	"path/filepath"
	"sync"
	"sync/atomic"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	resourceBase         atomic.Value // string
	registerPathSinkOnce sync.Once
)

// EnsureResourcePathSink registers the sink that records the loaded bundle path.
func EnsureResourcePathSink() {
	registerPathSinkOnce.Do(func() {
		maa.AgentServerAddResourceSink(&resourcePathSink{})
		log.Debug().Msg("Resource path sink registered for Go service")
	})
}

type resourcePathSink struct{}

// OnResourceLoading records the bundle path MaaFramework loaded.
func (c *resourcePathSink) OnResourceLoading(resource *maa.Resource, status maa.EventStatus, detail maa.ResourceLoadingDetail) {
	if status != maa.EventStatusSucceeded || detail.Path == "" {
		return
	}
	absPath := detail.Path
	if p, err := filepath.Abs(detail.Path); err == nil {
		absPath = p
	}
	resourceBase.Store(absPath)
	log.Debug().Str("absPath", absPath).Msg("Resource path sink captured resource path")
}

// BasePath returns the route bundle loaded by MaaFramework, or "" when unknown.
// It is a bundle root, not the project root: project files go through fsutil.InstallPath.
func BasePath() string {
	if v := resourceBase.Load(); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
