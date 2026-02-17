package autoheadhunting

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type Init struct{}
type AutoHeadhunting struct{}

func (a *Init) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	return true
}

func (a *AutoHeadhunting) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		TargetPulls       int    // 最多消耗的抽数
		TargetOperator    string // 抽到目标干员后停止
		TargetOperatorNum int    // 抽到目标干员达到该数量后停止
		PreferMode        int    // 抽卡偏好 优先使用对应模式进行抽取 不足时回退到单抽直至满足停止条件
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("[AutoHeadhunting] Failed to parse parameters")
		return false
	}

	usedPulls := 0
	targetCount := 0
	const MAX_OCR_RETRIES = 10

	tasker := ctx.GetTasker()
	if tasker == nil {
		log.Error().Msg("[AutoHeadhunting] Tasker is nil")
		return false
	}
	controller := tasker.GetController()
	if controller == nil {
		log.Error().Msg("[AutoHeadhunting] Controller is nil")
		return false
	}

	log.Info().Msgf("[AutoHeadhunting] Starting with parameters: %+v", params)
	for usedPulls < params.TargetPulls && targetCount < params.TargetOperatorNum {
		if tasker.Stopping() {
			log.Info().Msg("[AutoHeadhunting] Stopping task...")
			return true
		}

		mode := params.PreferMode
		if params.TargetPulls-usedPulls < 10 {
			mode = 1
		}

		switch mode {
		case 1:
			usedPulls++
			_, err := ctx.RunTask("AutoHeadhunting:Click1x")
			if err != nil {
				log.Err(err).Msg("[AutoHeadhunting] Failed to perform 1x pull")
				return false
			}
			log.Info().Msg("[AutoHeadhunting] Performed 1x pull")
		case 10:
			usedPulls += 10
			_, err := ctx.RunTask("AutoHeadhunting:Click10x")
			if err != nil {
				log.Err(err).Msg("[AutoHeadhunting] Failed to perform 10x pull")
				return false
			}
			log.Info().Msg("[AutoHeadhunting] Performed 10x pull")
		default:
			log.Error().Msgf("[AutoHeadhunting] Invalid prefer mode: %d", params.PreferMode)
			return false
		}

		// 点击确认后检测是否存在源石图标 如果有停止任务 (抽数不够)
		img, err := controller.CacheImage()
		if err != nil {
			log.Err(err).Msg("[AutoHeadhunting] Failed to cache image")
			return false
		}
		_, err = ctx.RunRecognition("AutoHeadhunting:DetectOrigeometry", img)
		if err == nil {
			log.Info().Msg("[AutoHeadhunting] Found Origeometry, stopping task...")
			return true
		}

		ans := make([]string, 0)
		for range mode {
			if tasker.Stopping() {
				log.Info().Msg("[AutoHeadhunting] Stopping task...")
				return true
			}

			// 通过检测武库配额图标以跳过星级动画
			ctx.RunTask("AutoHeadhunting:SkipStars")

			// OCR 识别干员名称
			for range MAX_OCR_RETRIES {
				img, err := controller.CacheImage()
				if err != nil {
					log.Err(err).Msg("[AutoHeadhunting] Failed to cache image")
					return false
				}
				details, err := ctx.RunRecognition("AutoHeadhunting:DetectOperatorName", img)
				if err != nil {
					log.Err(err).Msg("[AutoHeadhunting] Failed to detect operator name")
					return false
				}

				if len(details.Results.Best) == 0 {
					log.Warn().Msg("[AutoHeadhunting] No OCR result detected. Retrying...")
					time.Sleep(500 * time.Millisecond)
					continue
				}

				ocr, ok := details.Results.Best[0].AsOCR()
				if !ok {
					log.Error().Msg("[AutoHeadhunting] Failed to extract OCR text")
					return false
				}

				log.Info().Msgf("[AutoHeadhunting] Detected operator: %s", ocr.Text)
				ans = append(ans, ocr.Text)
				break
			}

			// 下一个干员
			_, err := ctx.RunTask("AutoHeadhunting:NextOperator")
			if err != nil {
				log.Err(err).Msg("[AutoHeadhunting] Failed to close results")
				return false
			}
		}

		// 退出抽卡界面
		ExitEntry := "AutoHeadhunting:Close"
		if mode == 1 {
			ExitEntry = "AutoHeadhunting:Done"
		}
		_, err = ctx.RunTask(ExitEntry)
		if err != nil {
			log.Err(err).Msg("[AutoHeadhunting] Failed to close results")
			return false
		}

		// 记录抽卡结果
		ans_mp := make(map[string]int)
		for _, name := range ans {
			ans_mp[name]++
			if name == params.TargetOperator {
				targetCount++
				log.Info().Msgf("[AutoHeadhunting] Found target operator: %s (count: %d)", name, targetCount)
			}

		}
		ans_str := make([]string, 0)
		for name, count := range ans_mp {
			ans_str = append(ans_str, fmt.Sprintf("%s: %d", name, count))
		}
		log.Info().Msgf("[AutoHeadhunting] Results: %s", ans_str)
		log.Info().Msgf("[AutoHeadhunting] Used pulls: %d /  %d", usedPulls, params.TargetPulls)
	}

	log.Info().Msgf("[AutoHeadhunting] Finished with %d pulls, found %d target operators", usedPulls, targetCount)
	return true
}
