package creditshopping

import (
	"fmt"
	"image"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func isADBController(ctrl *maa.Controller) bool {
	t, err := control.GetControlType(ctrl)
	return err == nil && t == control.CONTROL_TYPE_ADB
}

func screencap(ctrl *maa.Controller) (image.Image, error) {
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		return nil, err
	}
	if img == nil {
		return nil, fmt.Errorf("cached image is nil")
	}
	return img, nil
}
