package essencefilter

import (
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

// func registerResourcePathSink() {
// 	fmt.Println("[EssenceFilter] Calling registerResourcePathSink")
// 	registerSinkOnce.Do(func() {
// 		maa.AgentServerAddResourceSink(&resourcePathSink{})
// 		fmt.Println("[EssenceFilter] Resource path sink registered")
// 	})
// }

type resourcePathSink struct{}

func (c *resourcePathSink) OnResourceLoading(resource *maa.Resource, status maa.EventStatus, detail maa.ResourceLoadingDetail) {
	// fmt.Println("[EssenceFilter] Resource loading event: status=%s, path=%s\n", status, detail.Path)
	if status != maa.EventStatusSucceeded || detail.Path == "" {
		return
	}
	abs := detail.Path
	if p, err := filepath.Abs(detail.Path); err == nil {
		abs = p
	}
	resourcePath.Store(abs)
	log.Info().Str("resource_path", abs).Msg("[EssenceFilter] resource loaded; cached path")
}

func getResourceBase() string {
	if v := resourcePath.Load(); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}
