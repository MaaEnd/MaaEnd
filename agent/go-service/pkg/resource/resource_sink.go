package resource

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	resourcePath         atomic.Value // string
	registerPathSinkOnce sync.Once
)

// EnsureResourcePathSink ensures the resource path sink is registered.
func EnsureResourcePathSink() {
	registerPathSinkOnce.Do(func() {
		maa.AgentServerAddResourceSink(&resourcePathSink{})
		log.Debug().Msg("Resource path sink registered for Go service")
	})
}

type resourcePathSink struct{}

// OnResourceLoading captures the resource path when a resource is loaded.
func (c *resourcePathSink) OnResourceLoading(resource *maa.Resource, status maa.EventStatus, detail maa.ResourceLoadingDetail) {
	if status != maa.EventStatusSucceeded || detail.Path == "" {
		return
	}
	absPath := detail.Path
	if p, err := filepath.Abs(detail.Path); err == nil {
		absPath = p
	}
	resourcePath.Store(absPath)
	log.Debug().Str("absPath", absPath).Msg("Resource path sink captured resource path")
}

// getResourceBase returns the cached resource base path or an empty string if unavailable.
func getResourceBase() string {
	if v := resourcePath.Load(); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// getStandardResourceBase returns a list of standard base paths to search for resources.
func getStandardResourceBase() []string {
	cwd, _ := os.Getwd()
	wd := filepath.Clean(cwd)
	wdParent := filepath.Dir(wd)
	wdGrandParent := filepath.Dir(wdParent)

	return []string{
		wd,
		filepath.Join(wd, "resource"),
		filepath.Join(wd, "assets"),
		wdParent,
		filepath.Join(wdParent, "resource"),
		filepath.Join(wdParent, "assets"),
		wdGrandParent,
		filepath.Join(wdGrandParent, "resource"),
		filepath.Join(wdGrandParent, "assets"),
	}
}
