// Copyright (c) 2026 Harry Huang
package maptracker

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	resourcePath     atomic.Value // string
	registerSinkOnce sync.Once
)

// ensureResourcePathSink ensures the resource path sink is registered
func ensureResourcePathSink() {
	registerSinkOnce.Do(func() {
		maa.AgentServerAddResourceSink(&resourcePathSink{})
		log.Debug().Msg("Resource path sink registered for map-tracker")
	})
}

type resourcePathSink struct{}

// OnResourceLoading captures the resource path when a resource is loaded
func (c *resourcePathSink) OnResourceLoading(resource *maa.Resource, status maa.EventStatus, detail maa.ResourceLoadingDetail) {
	if status != maa.EventStatusSucceeded || detail.Path == "" {
		return
	}
	abs := detail.Path
	if p, err := filepath.Abs(detail.Path); err == nil {
		abs = p
	}
	resourcePath.Store(abs)
	log.Debug().Str("resource_path", abs).Msg("Resource loaded; cached path for map-tracker")
}

// getResourceBase returns the cached resource path or common defaults as fallback
func getResourceBase() string {
	if v := resourcePath.Load(); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// findResource tries to find a file in the cached resource path or standard fallbacks
func findResource(relativePath string) string {
	// 1. Try cached path from sink
	if base := getResourceBase(); base != "" {
		path := filepath.Join(base, relativePath)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// 2. Try standard resource directories relative to CWD
	cwd, _ := os.Getwd()
	fallbacks := []string{
		filepath.Join(cwd, "resource"),
		"resource",
	}

	for _, base := range fallbacks {
		path := filepath.Join(base, relativePath)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}
