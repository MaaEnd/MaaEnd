package seizedeliveryjobs

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"sync"

	maptrackerbigmap "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/bigmap"
	maptrackerdefault "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/default"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	seizeDeliveryJobsDepartureComponent       = "SeizeDeliveryJobsDepartureAction"
	seizeDeliveryJobsBlueTaskLocationTemplate = "image/SeizeDeliveryJobs/BlueTaskLocation.png"
)

// SeizeDeliveryJobsDepartureAction navigates from the tracked task marker back in the open world.
type SeizeDeliveryJobsDepartureAction struct{}

type seizeDeliveryJobsDepartureParam struct {
	IsRetry bool `json:"is_retry,omitempty"`
}

type seizeDeliveryJobsCachedDestination struct {
	MapName string
	Target  [2]float64
}

var seizeDeliveryJobsDestinationCache = struct {
	sync.Mutex
	hasValue bool
	value    seizeDeliveryJobsCachedDestination
}{}

var _ maa.CustomActionRunner = &SeizeDeliveryJobsDepartureAction{}

// Run implements maa.CustomActionRunner.
func (a *SeizeDeliveryJobsDepartureAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || ctx.GetTasker() == nil || ctx.GetTasker().GetController() == nil {
		log.Error().
			Str("component", seizeDeliveryJobsDepartureComponent).
			Msg("invalid action context")
		return false
	}

	param, err := a.parseParam(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", seizeDeliveryJobsDepartureComponent).
			Msg("failed to parse parameters")
		return false
	}

	var mapName string
	var target [2]float64
	if param.IsRetry {
		cached, ok := a.loadCachedDestination()
		if !ok {
			log.Error().
				Str("component", seizeDeliveryJobsDepartureComponent).
				Msg("retry requested but destination cache is empty")
			return false
		}
		mapName = cached.MapName
		target = cached.Target
		log.Info().
			Str("component", seizeDeliveryJobsDepartureComponent).
			Str("map", mapName).
			Float64("targetX", target[0]).
			Float64("targetY", target[1]).
			Msg("using cached delivery job destination")
	} else {
		screenTarget, ok := a.findAndCacheTarget(ctx, arg, &mapName, &target)
		if !ok {
			return false
		}
		if !a.clickTracking(ctx, screenTarget) {
			return false
		}
	}

	if detail, err := ctx.RunTask("SceneAnyEnterWorld"); err != nil || detail == nil || !detail.Status.Success() {
		event := log.Error().
			Err(err).
			Str("component", seizeDeliveryJobsDepartureComponent).
			Str("sceneNode", "SceneAnyEnterWorld")
		if detail != nil {
			event = event.Int64("subtaskID", detail.ID).Str("subtaskStatus", detail.Status.String())
		}
		event.Msg("failed to return to open world")
		return false
	}

	if !a.runGoal(ctx, arg, mapName, target) {
		return false
	}

	return a.runSubmitEntry(ctx)
}

func (a *SeizeDeliveryJobsDepartureAction) parseParam(paramStr string) (*seizeDeliveryJobsDepartureParam, error) {
	if paramStr == "" {
		return &seizeDeliveryJobsDepartureParam{}, nil
	}

	var param seizeDeliveryJobsDepartureParam
	if err := json.Unmarshal([]byte(paramStr), &param); err != nil {
		return nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
	}
	return &param, nil
}

func (a *SeizeDeliveryJobsDepartureAction) findAndCacheTarget(ctx *maa.Context, arg *maa.CustomActionArg, mapName *string, target *[2]float64) ([2]int, bool) {
	foundMapName, foundTarget, screenTarget, ok := a.findTarget(ctx, arg)
	if !ok {
		return [2]int{}, false
	}

	*mapName = foundMapName
	*target = foundTarget
	a.saveCachedDestination(foundMapName, foundTarget)

	log.Info().
		Str("component", seizeDeliveryJobsDepartureComponent).
		Str("map", foundMapName).
		Float64("targetX", foundTarget[0]).
		Float64("targetY", foundTarget[1]).
		Int("screenTargetX", screenTarget[0]).
		Int("screenTargetY", screenTarget[1]).
		Msg("recorded delivery job destination")

	return screenTarget, true
}

func (a *SeizeDeliveryJobsDepartureAction) saveCachedDestination(mapName string, target [2]float64) {
	seizeDeliveryJobsDestinationCache.Lock()
	defer seizeDeliveryJobsDestinationCache.Unlock()
	seizeDeliveryJobsDestinationCache.value = seizeDeliveryJobsCachedDestination{
		MapName: mapName,
		Target:  target,
	}
	seizeDeliveryJobsDestinationCache.hasValue = true
}

func (a *SeizeDeliveryJobsDepartureAction) loadCachedDestination() (seizeDeliveryJobsCachedDestination, bool) {
	seizeDeliveryJobsDestinationCache.Lock()
	defer seizeDeliveryJobsDestinationCache.Unlock()
	return seizeDeliveryJobsDestinationCache.value, seizeDeliveryJobsDestinationCache.hasValue
}

func (a *SeizeDeliveryJobsDepartureAction) findTarget(ctx *maa.Context, arg *maa.CustomActionArg) (string, [2]float64, [2]int, bool) {
	ctrl := ctx.GetTasker().GetController()
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		log.Error().
			Err(err).
			Str("component", seizeDeliveryJobsDepartureComponent).
			Msg("failed to get cached image")
		return "", [2]float64{}, [2]int{}, false
	}
	if img == nil {
		log.Error().
			Str("component", seizeDeliveryJobsDepartureComponent).
			Msg("cached image is nil")
		return "", [2]float64{}, [2]int{}, false
	}

	inferResult, err := a.inferBigMap(ctx, arg, img)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", seizeDeliveryJobsDepartureComponent).
			Msg("failed to infer destination map")
		return "", [2]float64{}, [2]int{}, false
	}

	matches, err := a.findBlueTaskLocation(ctx, arg, img)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", seizeDeliveryJobsDepartureComponent).
			Str("template", seizeDeliveryJobsBlueTaskLocationTemplate).
			Msg("failed to find delivery job marker")
		return "", [2]float64{}, [2]int{}, false
	}
	if len(matches) == 0 {
		log.Warn().
			Str("component", seizeDeliveryJobsDepartureComponent).
			Str("template", seizeDeliveryJobsBlueTaskLocationTemplate).
			Msg("delivery job marker not found")
		return "", [2]float64{}, [2]int{}, false
	}

	best := matches[0]
	screenTarget := [2]int{int(math.Round(best.ScreenX)), int(math.Round(best.ScreenY))}
	return inferResult.MapName, [2]float64{best.MapX, best.MapY}, screenTarget, true
}

func (a *SeizeDeliveryJobsDepartureAction) inferBigMap(ctx *maa.Context, arg *maa.CustomActionArg, img image.Image) (*maptrackerbigmap.MapTrackerBigMapInferResult, error) {
	resultWrapper, hit := maptrackerbigmap.MapTrackerBigMapInferRunner.Run(ctx, &maa.CustomRecognitionArg{
		TaskID:                arg.TaskID,
		CurrentTaskName:       arg.CurrentTaskName,
		CustomRecognitionName: "MapTrackerBigMapInfer",
		Img:                   img,
		Roi:                   maa.Rect{0, 0, img.Bounds().Dx(), img.Bounds().Dy()},
	})
	if !hit {
		return nil, fmt.Errorf("big-map inference not hit")
	}
	if resultWrapper == nil || resultWrapper.Detail == "" {
		return nil, fmt.Errorf("big-map inference result is empty")
	}

	var result maptrackerbigmap.MapTrackerBigMapInferResult
	if err := json.Unmarshal([]byte(resultWrapper.Detail), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal big-map inference result: %w", err)
	}
	if result.MapName == "" {
		return nil, fmt.Errorf("big-map inference returned empty map name")
	}
	if result.ViewPort.Scale <= 0 {
		return nil, fmt.Errorf("invalid inferred scale: %f", result.ViewPort.Scale)
	}
	return &result, nil
}

func (a *SeizeDeliveryJobsDepartureAction) findBlueTaskLocation(ctx *maa.Context, arg *maa.CustomActionArg, img image.Image) ([]maptrackerbigmap.MapTrackerBigMapFindImageMatch, error) {
	param := struct {
		Template   string  `json:"template"`
		Expected   bool    `json:"expected"`
		GreenMask  bool    `json:"green_mask,omitempty"`
		ZoomValue  float64 `json:"zoom_value,omitempty"`
		MaxMatches int     `json:"max_matches,omitempty"`
	}{
		Template:   seizeDeliveryJobsBlueTaskLocationTemplate,
		Expected:   true,
		GreenMask:  true,
		ZoomValue:  0.25,
		MaxMatches: 1,
	}
	paramBytes, err := json.Marshal(param)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal find-image parameters: %w", err)
	}

	resultWrapper, hit := (&maptrackerbigmap.MapTrackerBigMapFindImage{}).Run(ctx, &maa.CustomRecognitionArg{
		TaskID:                 arg.TaskID,
		CurrentTaskName:        arg.CurrentTaskName,
		CustomRecognitionName:  "MapTrackerBigMapFindImage",
		CustomRecognitionParam: string(paramBytes),
		Img:                    img,
		Roi:                    maa.Rect{0, 0, img.Bounds().Dx(), img.Bounds().Dy()},
	})
	if resultWrapper == nil || resultWrapper.Detail == "" {
		if !hit {
			return nil, nil
		}
		return nil, fmt.Errorf("find-image result is empty")
	}

	var matches []maptrackerbigmap.MapTrackerBigMapFindImageMatch
	if err := json.Unmarshal([]byte(resultWrapper.Detail), &matches); err != nil {
		return nil, fmt.Errorf("failed to unmarshal find-image result: %w", err)
	}
	if !hit {
		return nil, nil
	}
	return matches, nil
}

func (a *SeizeDeliveryJobsDepartureAction) clickTracking(ctx *maa.Context, screenTarget [2]int) bool {
	if err := ctx.OverridePipeline(map[string]any{
		"SeizeDeliveryJobsClickTracking": map[string]any{
			"target": []int{screenTarget[0], screenTarget[1]},
		},
	}); err != nil {
		log.Error().
			Err(err).
			Str("component", seizeDeliveryJobsDepartureComponent).
			Ints("screenTarget", []int{screenTarget[0], screenTarget[1]}).
			Msg("failed to override tracking click target")
		return false
	}

	if detail, err := ctx.RunTask("SeizeDeliveryJobsClickTracking"); err != nil || detail == nil || !detail.Status.Success() {
		event := log.Error().
			Err(err).
			Str("component", seizeDeliveryJobsDepartureComponent).
			Ints("screenTarget", []int{screenTarget[0], screenTarget[1]}).
			Str("node", "SeizeDeliveryJobsClickTracking")
		if detail != nil {
			event = event.Int64("subtaskID", detail.ID).Str("subtaskStatus", detail.Status.String())
		}
		event.Msg("failed to click and cancel task tracking")
		return false
	}
	return true
}

func (a *SeizeDeliveryJobsDepartureAction) runSubmitEntry(ctx *maa.Context) bool {
	if detail, err := ctx.RunTask("SeizeDeliveryJobsSubmitEntry"); err != nil || detail == nil || !detail.Status.Success() {
		event := log.Error().
			Err(err).
			Str("component", seizeDeliveryJobsDepartureComponent).
			Str("node", "SeizeDeliveryJobsSubmitEntry")
		if detail != nil {
			event = event.Int64("subtaskID", detail.ID).Str("subtaskStatus", detail.Status.String())
		}
		event.Msg("failed to submit delivery job")
		return false
	}
	return true
}

func (a *SeizeDeliveryJobsDepartureAction) runGoal(ctx *maa.Context, arg *maa.CustomActionArg, mapName string, target [2]float64) bool {
	param := struct {
		MapName         string     `json:"map_name"`
		Target          [2]float64 `json:"target"`
		ZiplinePolicy   string     `json:"zipline_policy"`
		StuckMitigators []string   `json:"stuck_mitigators"`
	}{
		MapName:         mapName,
		Target:          target,
		ZiplinePolicy:   maptrackerdefault.ZIPLINE_POLICY_LAZY,
		StuckMitigators: []string{"MoveOrDeleteDevice"},
	}
	paramBytes, err := json.Marshal(param)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", seizeDeliveryJobsDepartureComponent).
			Msg("failed to marshal MapTrackerGoal parameters")
		return false
	}

	ok := (&maptrackerdefault.MapTrackerGoal{}).Run(ctx, &maa.CustomActionArg{
		TaskID:            arg.TaskID,
		CurrentTaskName:   arg.CurrentTaskName,
		CustomActionName:  "MapTrackerGoal",
		CustomActionParam: string(paramBytes),
		RecognitionDetail: arg.RecognitionDetail,
		Box:               arg.Box,
	})
	if !ok {
		log.Error().
			Str("component", seizeDeliveryJobsDepartureComponent).
			Str("map", mapName).
			Float64("targetX", target[0]).
			Float64("targetY", target[1]).
			Msg("MapTrackerGoal failed")
	}
	return ok
}
