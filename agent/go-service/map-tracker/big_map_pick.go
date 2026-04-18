// Copyright (c) 2026 Harry Huang
package maptracker

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"math/rand"
	"regexp"
	"sync"
	"time"

	mt "github.com/MaaXYZ/MaaEnd/agent/go-service/map-tracker/internal"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// MapTrackerBigMapPick picks a target map coordinate by panning the big map view.
type MapTrackerBigMapPick struct {
	externalOnce sync.Once
	externalData map[string]mapExternalDataItem
	externalErr  error
}

type mapExternalDataItem struct {
	SceneManagerNode string `json:"scene_manager_node,omitempty"`
}

// MapTrackerBigMapPickParam represents the custom_action_param for MapTrackerBigMapPick.
type MapTrackerBigMapPickParam struct {
	// MapName is the target map name.
	MapName string `json:"map_name"`
	// Target is the target coordinate in the specified map file's original coordinate space.
	Target [2]float64 `json:"target"`
	// OnFind controls behavior when target enters viewport. Valid values: "Click", "Teleport", "DoNothing".
	OnFind string `json:"on_find,omitempty"`
	// AutoOpenMapScene controls whether to automatically open the big map scene before picking.
	AutoOpenMapScene bool `json:"auto_open_map_scene,omitempty"`
	// NoZoom controls whether to disable auto zoom before picking.
	NoZoom bool `json:"no_zoom,omitempty"`
}

const (
	ON_FIND_CLICK      = "Click"
	ON_FIND_TELEPORT   = "Teleport"
	ON_FIND_DO_NOTHING = "DoNothing"
)

var mapTrackerBigMapPickDefaultParam = MapTrackerBigMapPickParam{
	OnFind: ON_FIND_CLICK,
}

var _ maa.CustomActionRunner = &MapTrackerBigMapPick{}

// Run implements maa.CustomActionRunner.
func (a *MapTrackerBigMapPick) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, err := a.parseParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerBigMapPick")
		return false
	}

	if param.AutoOpenMapScene {
		sceneManagerNode, hasSceneMapping, err := a.getSceneManagerNode(param.MapName)
		if err != nil {
			log.Error().Err(err).Str("map", param.MapName).Msg("Failed to resolve scene manager mapping")
			return false
		}
		if hasSceneMapping {
			if _, err := ctx.RunTask(sceneManagerNode); err != nil {
				log.Error().Err(err).Str("map", param.MapName).Str("sceneManagerNode", sceneManagerNode).Msg("Failed to run scene manager node")
				return false
			}
			log.Info().Str("map", param.MapName).Str("sceneManagerNode", sceneManagerNode).Str("onFind", param.OnFind).Msg("Scene manager node completed before big-map pick")
		} else {
			log.Warn().Str("map", param.MapName).Msg("No scene manager mapping found for the map, cannot auto open map scene")
		}

		if _, err := ctx.RunTask("__ScenePrivateMapFilterClear"); err != nil {
			log.Error().Err(err).Str("map", param.MapName).Msg("Failed to clear map filters before pick")
			return false
		}
	}

	ctrl := ctx.GetTasker().GetController()
	ca, err := control.NewControlAdaptor(ctx, ctrl, mt.WORK_W, mt.WORK_H)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create control adaptor")
		return false
	}

	if !param.NoZoom {
		if err := a.doAutoZoom(ctx, ctrl, ca); err != nil {
			log.Warn().Err(err).Msg("Failed to auto adjust big-map zoom")
		}
	}

	panFactor := mt.BIG_MAP_PAN_FACTOR_NUMERATOR / control.GetScreenDiagonalSize(ctrl)
	log.Info().Float64("panFactor", panFactor).Msg("Calculated big-map pan factor")

	for attempt := 1; attempt <= mt.BIG_MAP_PICK_RETRY; attempt++ {
		inferRes, err := doBigMapInferForMap(ctx, ctrl, param.MapName)
		if err != nil {
			log.Error().Err(err).Str("map", param.MapName).Int("attempt", attempt).Msg("Currently not in that map")
			return false
		}

		targetInViewX, targetInViewY := inferRes.ViewPort.GetScreenCoordOf(param.Target[0], param.Target[1])
		if inferRes.ViewPort.IsViewCoordInView(targetInViewX, targetInViewY) {
			switch param.OnFind {
			case ON_FIND_CLICK:
				ca.TouchClick(0, int(math.Round(targetInViewX)), int(math.Round(targetInViewY)), 100, 0)
			case ON_FIND_TELEPORT:
				if err := runBigMapTeleportNode(ctx, ca, targetInViewX, targetInViewY); err != nil {
					log.Error().Err(err).Str("map", param.MapName).Msg("Failed to run teleport sequence on find")
					return false
				}
			}

			log.Info().
				Str("map", param.MapName).
				Int("attempt", attempt).
				Str("onFind", param.OnFind).
				Float64("targetX", param.Target[0]).
				Float64("targetY", param.Target[1]).
				Float64("targetInViewX", targetInViewX).
				Float64("targetInViewY", targetInViewY).
				Msg("Big-map target is in valid viewport")
			return true
		}

		centerX := (inferRes.ViewPort.Left + inferRes.ViewPort.Right) * 0.5
		centerY := (inferRes.ViewPort.Top + inferRes.ViewPort.Bottom) * 0.5
		deltaInViewX := targetInViewX - centerX
		deltaInViewY := targetInViewY - centerY
		log.Info().
			Str("map", param.MapName).
			Int("attempt", attempt).
			Float64("targetInViewX", targetInViewX).
			Float64("targetInViewY", targetInViewY).
			Msg("Big-map target is not in viewport, need to pan")

		segments := rand.Intn(4) + 2
		if !doDragViewport(ca, &inferRes.ViewPort, deltaInViewX, deltaInViewY, panFactor, segments) {
			continue
		}
	}

	log.Error().
		Str("map", param.MapName).
		Float64("targetX", param.Target[0]).
		Float64("targetY", param.Target[1]).
		Msg("Failed to pan map to target")
	return false
}

func (a *MapTrackerBigMapPick) parseParam(paramStr string) (*MapTrackerBigMapPickParam, error) {
	if paramStr == "" {
		return nil, fmt.Errorf("custom_action_param is required")
	}

	var param MapTrackerBigMapPickParam
	if err := json.Unmarshal([]byte(paramStr), &param); err != nil {
		return nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
	}

	if param.MapName == "" {
		return nil, fmt.Errorf("map_name must be provided")
	}
	if param.OnFind == "" {
		param.OnFind = mapTrackerBigMapPickDefaultParam.OnFind
	}
	if param.OnFind != ON_FIND_CLICK && param.OnFind != ON_FIND_TELEPORT && param.OnFind != ON_FIND_DO_NOTHING {
		return nil, fmt.Errorf("on_find must be one of: %s, %s, %s", ON_FIND_CLICK, ON_FIND_TELEPORT, ON_FIND_DO_NOTHING)
	}
	if math.IsNaN(param.Target[0]) || math.IsInf(param.Target[0], 0) || math.IsNaN(param.Target[1]) || math.IsInf(param.Target[1], 0) {
		return nil, fmt.Errorf("target must contain finite numbers")
	}

	return &param, nil
}

func (a *MapTrackerBigMapPick) getSceneManagerNode(mapName string) (string, bool, error) {
	a.externalOnce.Do(func() {
		a.externalData = map[string]mapExternalDataItem{}
		err := resource.ReadJsonResource(mt.MAP_EXTERNAL_DATA_PATH, &a.externalData)
		if err != nil {
			a.externalErr = fmt.Errorf("failed to load map external data: %w", err)
			return
		}
	})

	if a.externalErr != nil {
		return "", false, a.externalErr
	}

	item, ok := a.externalData[mapName]
	if !ok || item.SceneManagerNode == "" {
		return "", false, nil
	}

	return item.SceneManagerNode, true, nil
}

func runBigMapTeleportNode(ctx *maa.Context, ca control.ControlAdaptor, targetInViewX, targetInViewY float64) error {
	ca.TouchClick(0, int(math.Round(targetInViewX)), int(math.Round(targetInViewY)), 100, 0)

	teleportNodeName := "__MapTrackerBigMapPickTeleport"
	teleportNodeOverride := map[string]any{
		teleportNodeName: map[string]any{
			"recognition": "DirectHit",
			"next": []string{
				"[JumpBack]__ScenePrivateMapTeleportChoose",
				"__ScenePrivateMapTeleportConfirm",
			},
		},
	}

	if _, err := ctx.RunTask(teleportNodeName, teleportNodeOverride); err != nil {
		return fmt.Errorf("failed to run teleport temporary node: %w", err)
	}

	return nil
}

func (a *MapTrackerBigMapPick) doAutoZoom(ctx *maa.Context, ctrl *maa.Controller, ca control.ControlAdaptor) error {
	zoomInTemplate, err := mt.Resource.ZoomInTemplate.Get()
	if err != nil {
		return fmt.Errorf("failed to load zoom-in template: %w", err)
	}

	zoomOutTemplate, err := mt.Resource.ZoomOutTemplate.Get()
	if err != nil {
		return fmt.Errorf("failed to load zoom-out template: %w", err)
	}

	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		return fmt.Errorf("failed to get cached image for auto zoom: %w", err)
	}
	if img == nil {
		return fmt.Errorf("cached image is nil for auto zoom")
	}

	screen := minicv.ImageConvertRGBA(img)
	searchArea := [4]int{
		int(math.Round(mt.ZOOM_BUTTON_AREA_X)),
		int(math.Round(mt.ZOOM_BUTTON_AREA_Y)),
		int(math.Round(mt.ZOOM_BUTTON_AREA_W)),
		int(math.Round(mt.ZOOM_BUTTON_AREA_H)),
	}
	screenIntegral := minicv.GetIntegralArray(screen)

	zoomOutX, zoomOutY, outVal := minicv.MatchTemplateInArea(
		screen,
		screenIntegral,
		zoomOutTemplate.Image,
		zoomOutTemplate.Stats,
		searchArea,
	)
	zoomInX, zoomInY, inVal := minicv.MatchTemplateInArea(
		screen,
		screenIntegral,
		zoomInTemplate.Image,
		zoomInTemplate.Stats,
		searchArea,
	)

	outMatched := outVal >= mt.ZOOM_BUTTON_THRESHOLD
	inMatched := inVal >= mt.ZOOM_BUTTON_THRESHOLD

	if outMatched && inMatched {
		cx := int(math.Round((zoomOutX + zoomInX) / 2.0))
		cy := int(math.Round(zoomInY + (zoomOutY-zoomInY)*0.7))
		ca.TouchClick(0, cx, cy, 100, 0)
		time.Sleep(333 * time.Millisecond) // Wait for UI response
		log.Info().Float64("outVal", outVal).Float64("inVal", inVal).Msg("Auto zoom adjusted by clicking slider area")
		return nil
	}
	if !outMatched && !inMatched {
		log.Warn().Float64("outVal", outVal).Float64("inVal", inVal).Msg("No zoom button matched for auto zoom")
		return nil
	}

	pressZoomButton := func(matchX, matchY float64, tpl *image.RGBA) {
		cx := int(math.Round(matchX + float64(tpl.Rect.Dx())/2.0))
		cy := int(math.Round(matchY + float64(tpl.Rect.Dy())/2.0))
		ca.TouchClick(0, cx, cy, 200, 0)
		time.Sleep(333 * time.Millisecond) // Wait for UI response
	}

	if outMatched {
		pressZoomButton(zoomOutX, zoomOutY, zoomOutTemplate.Image)
		log.Info().Float64("outVal", outVal).Float64("inVal", inVal).Msg("Auto zoom adjusted by pressing zoom-out button")
	} else {
		pressZoomButton(zoomInX, zoomInY, zoomInTemplate.Image)
		log.Info().Float64("outVal", outVal).Float64("inVal", inVal).Msg("Auto zoom adjusted by pressing zoom-in button")
	}
	return nil
}

func doBigMapInferForMap(ctx *maa.Context, ctrl *maa.Controller, mapName string) (*MapTrackerBigMapInferResult, error) {
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		return nil, fmt.Errorf("failed to get cached image: %w", err)
	}
	if img == nil {
		return nil, fmt.Errorf("cached image is nil")
	}

	inferConfig := map[string]any{
		"map_name_regex": "^" + regexp.QuoteMeta(mapName) + "$",
		"threshold":      mapTrackerBigMapInferDefaultParam.Threshold,
	}
	inferConfigBytes, err := json.Marshal(inferConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal big-map inference config: %w", err)
	}

	taskDetail, err := ctx.GetTaskJob().GetDetail()
	if err != nil {
		return nil, fmt.Errorf("failed to get task detail: %w", err)
	}

	resultWrapper, hit := mapTrackerBigMapInferRunner.Run(ctx, &maa.CustomRecognitionArg{
		TaskID:                 taskDetail.ID,
		CurrentTaskName:        taskDetail.Entry,
		CustomRecognitionName:  "MapTrackerBigMapInfer",
		CustomRecognitionParam: string(inferConfigBytes),
		Img:                    img,
		Roi:                    maa.Rect{0, 0, img.Bounds().Dx(), img.Bounds().Dy()},
	})
	if !hit {
		return nil, fmt.Errorf("big-map inference not hit")
	}
	if resultWrapper == nil || resultWrapper.Detail == "" {
		return nil, fmt.Errorf("big-map inference result is empty")
	}

	var result MapTrackerBigMapInferResult
	if err := json.Unmarshal([]byte(resultWrapper.Detail), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal big-map inference result: %w", err)
	}
	if result.MapName != mapName {
		return nil, fmt.Errorf("inference map mismatch: expect %s, got %s", mapName, result.MapName)
	}
	if result.ViewPort.Scale <= 0 {
		return nil, fmt.Errorf("invalid inferred scale: %f", result.ViewPort.Scale)
	}

	return &result, nil
}

func doDragViewport(ca control.ControlAdaptor, viewport *mt.BigMapViewport, deltaInViewX, deltaInViewY, panFactor float64, segments int) bool {
	rawDragDx := -deltaInViewX * panFactor
	rawDragDy := -deltaInViewY * panFactor

	// Calculate start and end points of the full drag
	minX, minY, maxX, maxY := viewport.GetIntegerRect()

	pickDragStartCorner := func(rawDragDx, rawDragDy float64) (int, int) {
		startX := minX
		if rawDragDx < 0 {
			// Drag toward left, start from right
			startX = maxX
		}

		startY := minY
		if rawDragDy < 0 {
			// Drag toward top, start from bottom
			startY = maxY
		}

		return startX, startY
	}

	startX, startY := pickDragStartCorner(rawDragDx, rawDragDy)

	dragDx := int(math.Ceil(math.Abs(rawDragDx)) * math.Copysign(1, rawDragDx))
	dragDy := int(math.Ceil(math.Abs(rawDragDy)) * math.Copysign(1, rawDragDy))

	endX := max(minX, min(maxX, startX+dragDx))
	endY := max(minY, min(maxY, startY+dragDy))

	dragDx = endX - startX
	dragDy = endY - startY

	if dragDx == 0 && dragDy == 0 {
		return false
	}

	// Calculate and perform segmented drags
	segments = max(1, segments)

	baseSegDx := dragDx / segments
	baseSegDy := dragDy / segments
	remainDx := dragDx - baseSegDx*segments
	remainDy := dragDy - baseSegDy*segments

	log.Info().
		Int("segments", segments).
		Int("startX", startX).
		Int("startY", startY).
		Int("dragDx", dragDx).
		Int("dragDy", dragDy).
		Msg("Panning big-map viewport")

	curX, curY := startX, startY
	for i := 0; i < segments; i++ {
		segDx, segDy := baseSegDx, baseSegDy
		if i == segments-1 {
			segDx += remainDx
			segDy += remainDy
		}

		if segDx == 0 && segDy == 0 {
			continue
		}

		ca.Swipe(0, curX, curY, segDx, segDy, 75, 25)
		curX += segDx
		curY += segDy
	}

	return true
}
