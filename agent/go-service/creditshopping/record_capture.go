package creditshopping

import (
	"fmt"
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func recordScreencap(ctrl *maa.Controller) (image.Image, error) {
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
