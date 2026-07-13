package runtimeimagecache

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type Scope string

const (
	ScopeContext  Scope = "context"
	ScopeResource Scope = "resource"
)

type Source string

const (
	SourceRecognitionBox Source = "recognition_box"
	SourceROI            Source = "roi"
)

type StoreActionParam struct {
	Scope     Scope  `json:"scope,omitempty"`
	Module    string `json:"module"`
	Key       string `json:"key"`
	Source    Source `json:"source,omitempty"`
	ROI       [4]int `json:"roi,omitempty"`
	ROIOffset [4]int `json:"roi_offset,omitempty"`
}

type Entry struct {
	ImageName string
	Rect      image.Rectangle
}

type OverrideFunc func(name string, img image.Image) error

// Store 裁剪源图并通过调用方提供的 OverrideImage 函数写入 MaaFramework 内存图片表。
func Store(
	module string,
	key string,
	src image.Image,
	rect image.Rectangle,
	offset [4]int,
	override OverrideFunc,
) (Entry, error) {
	if override == nil {
		return Entry{}, errors.New("override function is nil")
	}
	imageName, err := BuildImageName(module, key)
	if err != nil {
		return Entry{}, err
	}
	rect, err = ApplyROIOffset(rect, offset)
	if err != nil {
		return Entry{}, err
	}
	cropped, err := Crop(src, rect)
	if err != nil {
		return Entry{}, err
	}
	if err := override(imageName, cropped); err != nil {
		return Entry{}, fmt.Errorf("override image %q: %w", imageName, err)
	}
	return Entry{ImageName: imageName, Rect: rect}, nil
}

func parseStoreActionParam(raw string) (StoreActionParam, error) {
	var param StoreActionParam
	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		return StoreActionParam{}, fmt.Errorf("parse store action param: %w", err)
	}
	if param.Scope == "" {
		param.Scope = ScopeContext
	}
	if param.Source == "" {
		param.Source = SourceRecognitionBox
	}
	if param.Scope != ScopeContext && param.Scope != ScopeResource {
		return StoreActionParam{}, fmt.Errorf("unsupported scope %q", param.Scope)
	}
	if param.Source != SourceRecognitionBox && param.Source != SourceROI {
		return StoreActionParam{}, fmt.Errorf("unsupported source %q", param.Source)
	}
	if _, err := BuildImageName(param.Module, param.Key); err != nil {
		return StoreActionParam{}, err
	}
	if param.Source == SourceROI && (param.ROI[2] <= 0 || param.ROI[3] <= 0) {
		return StoreActionParam{}, errors.New("source roi requires positive width and height")
	}
	return param, nil
}

type StoreAction struct{}

var _ maa.CustomActionRunner = &StoreAction{}

func (a *StoreAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "RuntimeImageCache").Msg("store action received nil context or argument")
		return false
	}
	param, err := parseStoreActionParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Str("component", "RuntimeImageCache").Msg("invalid store action parameters")
		return false
	}

	rect := image.Rect(
		arg.Box.X(),
		arg.Box.Y(),
		arg.Box.X()+arg.Box.Width(),
		arg.Box.Y()+arg.Box.Height(),
	)
	if param.Source == SourceROI {
		rect = image.Rect(param.ROI[0], param.ROI[1], param.ROI[0]+param.ROI[2], param.ROI[1]+param.ROI[3])
	}

	tasker := ctx.GetTasker()
	ctrl := tasker.GetController()
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		log.Error().Err(err).Str("component", "RuntimeImageCache").Msg("failed to get fresh screenshot")
		return false
	}

	override := OverrideFunc(ctx.OverrideImage)
	if param.Scope == ScopeResource {
		override = tasker.GetResource().OverrideImage
	}
	entry, err := Store(param.Module, param.Key, img, rect, param.ROIOffset, override)
	if err != nil {
		log.Error().Err(err).Str("component", "RuntimeImageCache").Msg("failed to store runtime image")
		return false
	}

	log.Info().
		Str("component", "RuntimeImageCache").
		Str("scope", string(param.Scope)).
		Str("image_name", entry.ImageName).
		Ints("roi", []int{entry.Rect.Min.X, entry.Rect.Min.Y, entry.Rect.Dx(), entry.Rect.Dy()}).
		Msg("runtime image stored")
	return true
}
