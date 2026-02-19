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
		Language          string // 语言
		TargetPulls       int    // 最多消耗的抽数
		TargetOperator    string // 抽到目标干员后停止
		TargetOperatorNum int    // 抽到目标干员达到该数量后停止
		PreferMode        int    // 抽卡偏好 优先使用对应模式进行抽取 不足时回退到单抽直至满足停止条件
	}
	raw, err := ctx.GetNodeJSON("AutoHeadhunting")
	if err != nil {
		log.Err(err).Msg("[AutoHeadhunting] Failed to get node JSON")
		return false
	}

	var nodeData map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &nodeData); err != nil {
		log.Err(err).Msg("[AutoHeadhunting] Failed to unmarshal node JSON")
		return false
	}

	attachRaw, ok := nodeData["attach"]
	if !ok {
		log.Error().Msg("[AutoHeadhunting] attach field not found in node JSON")
		return false
	}

	if err := json.Unmarshal(attachRaw, &params); err != nil {
		log.Err(err).Msg("[AutoHeadhunting] Failed to unmarshal attach to params")
		return false
	}

	lang = params.Language

	targetLabel, _ := o(params.TargetOperator)
	if targetLabel == "" {
		targetLabel = "None"
	}

	// 输出任务参数配置摘要 HTML
	logTaskParamsHTML(ctx, params.TargetPulls, targetLabel, params.TargetOperatorNum, params.PreferMode)

	stopping := false
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
	mp := make(map[string]int)
	for usedPulls < params.TargetPulls && targetCount < params.TargetOperatorNum {
		if tasker.Stopping() {
			log.Info().Msg("[AutoHeadhunting] Stopping task...")
			stopping = true
			break
		}

		mode := params.PreferMode
		if params.TargetPulls-usedPulls < 10 {
			LogMXUSimpleHTMLWithColor(ctx, t("fallback_1x"), "#ffa500")
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
		controller.PostScreencap().Wait()
		img, err := controller.CacheImage()
		if err != nil {
			log.Err(err).Msg("[AutoHeadhunting] Failed to get cache image")
			return false
		}
		details, err := ctx.RunRecognition("AutoHeadhunting:DetectOrigeometry", img)
		if err == nil && len(details.Results.Best) > 0 {
			log.Info().Msg("[AutoHeadhunting] Found Origeometry, stopping task...")
			LogMXUSimpleHTMLWithColor(ctx, t("unenough_pulls"), "#ffa500")
			stopping = true
			break
		}

		// 跳过拉杆和降落动画
		task_details, err := ctx.RunTask("AutoHeadhunting:Skip1")
		if err != nil {
			log.Err(err).Msg("[AutoHeadhunting] Failed to skip the pull animation")
			return false
		}
		if task_details != nil && !task_details.Status.Done() {
			// 部分六星动画无法跳过 当未检测到跳过键时 进入等待
			log.Info().Msg("[AutoHeadhunting] Skip button is not detected, waiting for animation to finish...")
			ctx.RunTask("AutoHeadhunting:Waiting")
		}

		ans := make([]string, 0)
		for range mode {
			if tasker.Stopping() {
				log.Info().Msg("[AutoHeadhunting] Stopping task...")
				stopping = true
				break
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
					time.Sleep(300 * time.Millisecond)
					continue
				}

				ocr, ok := details.Results.Best[0].AsOCR()
				if !ok {
					log.Error().Msg("[AutoHeadhunting] Failed to extract OCR text")
					return false
				}

				_, ocrStars := o(t(ocr.Text))
				starLabel := ""
				if ocrStars != "0" {
					starLabel = fmt.Sprintf(" ★%s", ocrStars)
				}
				LogMXUSimpleHTMLWithColor(ctx, fmt.Sprintf(t("results"), ocr.Text+starLabel), getColorForStars(ocrStars))
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
		if stopping {
			break
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
			// 寻找目标干员
			_, stars := o(t(name))
			ans_mp[name]++
			mp[name]++
			mp[stars]++
			if name == targetLabel {
				targetCount++
				log.Info().Msgf("[AutoHeadhunting] Found target operator: %s (count: %d)", name, targetCount)
			}

		}
		ans_mp_str := make([]string, 0)
		for name, count := range ans_mp {
			ans_mp_str = append(ans_mp_str, fmt.Sprintf("%s: %d", name, count))
		}
		log.Info().Msgf("[AutoHeadhunting] Results: %s", ans_mp_str)
		log.Info().Msgf("[AutoHeadhunting] Used pulls: %d /  %d", usedPulls, params.TargetPulls)

		// 输出单轮抽卡结果的带颜色 HTML
		logPullResultsHTML(ctx, usedPulls, params.TargetPulls, ans_mp)
	}

	log.Info().Msgf("[AutoHeadhunting] Finished with %d pulls, found %d target operators (%s)", usedPulls, targetCount, params.TargetOperator)
	log.Info().Msgf("[AutoHeadhunting] Final results: %v", mp)

	// 输出最终抽卡结果摘要的带颜色 HTML
	logFinalSummaryHTML(ctx, usedPulls, targetCount, targetLabel, mp)

	return true
}
