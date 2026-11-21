package essencefilter

import (
	"errors"
	"image"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	errNoController    = errors.New("essencefilter: controller is nil")
	errScreencapFailed = errors.New("essencefilter: screencap failed")
)

// Language detect OCR nodes in pipeline EssenceFilter/DetectLanguage.json (CN→TC→EN→JP→KR).
var essenceFilterDetectLangNodes = []struct {
	Node   string
	Locale string
}{
	{Node: "__EssenceFilterDetectLangCN", Locale: matchapi.LocaleCN},
	{Node: "__EssenceFilterDetectLangTC", Locale: matchapi.LocaleTC},
	{Node: "__EssenceFilterDetectLangEN", Locale: matchapi.LocaleEN},
	{Node: "__EssenceFilterDetectLangJP", Locale: matchapi.LocaleJP},
	{Node: "__EssenceFilterDetectLangKR", Locale: matchapi.LocaleKR},
}

// explicitInputLanguage returns a canonical locale when attach.input_language is an explicit
// CN|TC|EN|JP|KR override. Empty / AUTO / unknown values mean auto-detect from screen.
func explicitInputLanguage(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	u := strings.ToUpper(s)
	if u == "AUTO" {
		return "", false
	}
	switch u {
	case matchapi.LocaleCN, matchapi.LocaleTC, matchapi.LocaleEN, matchapi.LocaleJP, matchapi.LocaleKR:
		return u, true
	default:
		return "", false
	}
}

func resolveInitImage(ctrl *maa.Controller) (image.Image, error) {
	if ctrl == nil {
		return nil, errNoController
	}
	job := ctrl.PostScreencap().Wait()
	if job == nil || !job.Success() {
		return nil, errScreencapFailed
	}
	return ctrl.CacheImage()
}

// detectInputLanguageFromScreen runs the five language OCR nodes on one frame.
// Returns the first hit locale, or CN with ok=false when none hit / image unavailable.
func detectInputLanguageFromScreen(ctx *maa.Context, ctrl *maa.Controller) (locale string, ok bool) {
	img, err := resolveInitImage(ctrl)
	if err != nil || img == nil {
		log.Warn().Err(err).Str("component", "EssenceFilter").Msg("language detect: no screenshot")
		return matchapi.LocaleCN, false
	}

	for _, item := range essenceFilterDetectLangNodes {
		detail, recoErr := ctx.RunRecognition(item.Node, img, nil)
		if recoErr != nil {
			log.Debug().Err(recoErr).Str("component", "EssenceFilter").Str("node", item.Node).Msg("language detect reco error")
			continue
		}
		if detail != nil && detail.Hit {
			log.Info().
				Str("component", "EssenceFilter").
				Str("node", item.Node).
				Str("input_language", item.Locale).
				Msg("language detect hit")
			return item.Locale, true
		}
	}

	log.Warn().Str("component", "EssenceFilter").Msg("language detect: no node hit, fallback CN")
	return matchapi.LocaleCN, false
}

// resolveInputLanguage prefers explicit attach.input_language; otherwise OCR-detects from screen.
func resolveInputLanguage(ctx *maa.Context, ctrl *maa.Controller, attachLang string) string {
	if loc, forced := explicitInputLanguage(attachLang); forced {
		log.Info().Str("component", "EssenceFilter").Str("input_language", loc).Msg("language from attach override")
		return loc
	}
	loc, hit := detectInputLanguageFromScreen(ctx, ctrl)
	if !hit {
		reportFocusByKey(ctx, "focus.warn.language_detect_failed")
	}
	return loc
}
