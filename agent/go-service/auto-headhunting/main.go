package autoheadhunting

import (
	"encoding/json"
	"fmt"
	"strings"
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
	node, err := ctx.GetNode("AutoHeadhunting")
	if err != nil {
		log.Err(err).Msg("[AutoHeadhunting] Failed to get node parameters")
		return false
	}

	attach := node.Attach
	attachJSON, err := json.Marshal(attach)
	if err != nil {
		log.Err(err).Msg("[AutoHeadhunting] Failed to marshal attach")
		return false
	}
	if err := json.Unmarshal(attachJSON, &params); err != nil {
		log.Err(err).Msg("[AutoHeadhunting] Failed to unmarshal attach to params")
		return false
	}

	fmt.Printf("%s: %d\n", t("target_pulls"), params.TargetPulls)
	fmt.Printf("%s: %s\n", t("target_operator"), params.TargetOperator)
	fmt.Printf("%s: %d\n", t("target_num"), params.TargetOperatorNum)
	fmt.Printf("%s: %d\n", t("prefer_mode"), params.PreferMode)

	lang = params.Language

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
			fmt.Printf("%s\n", t("fallback_1x"))
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
		details, err := ctx.RunRecognition("AutoHeadhunting:DetectOrigeometry", img)
		if err == nil && len(details.Results.Best) > 0 {
			log.Info().Msg("[AutoHeadhunting] Found Origeometry, stopping task...")
			return true
		}

		// 跳过拉杆和降落动画
		task_details, err := ctx.RunTask("AutoHeadhunting:Skip1")
		if err != nil {
			log.Err(err).Msg("[AutoHeadhunting] Failed to skip the pull animation")
			return false
		}
		if task_details != nil && !task_details.Status.Done() {
			// 六星动画无法跳过 当未检测到跳过键时 进入等待
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
					time.Sleep(500 * time.Millisecond)
					continue
				}

				ocr, ok := details.Results.Best[0].AsOCR()
				if !ok {
					log.Error().Msg("[AutoHeadhunting] Failed to extract OCR text")
					return false
				}

				fmt.Printf(t("results"+"\n"), ocr.Text)
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
			ans_mp[name]++
			mp[name]++
			// 寻找目标干员
			if t(name) == params.TargetOperator {
				targetCount++
				log.Info().Msgf("[AutoHeadhunting] Found target operator: %s (count: %d)", name, targetCount)
			}

		}
		ans_mp_str := make([]string, 0)
		ans_str := make([]string, 0)
		ans_str = append(ans_str, fmt.Sprintf("%s %d/%d", t("used_pulls"), usedPulls, params.TargetPulls)+"\n")
		for name, count := range ans_mp {
			ans_mp_str = append(ans_mp_str, fmt.Sprintf("%s: %d", name, count))
			ans_str = append(ans_str, fmt.Sprintf("%s: %d", name, count)+"\n")
		}

		fmt.Printf("%s", strings.Join(ans_str, ""))
		log.Info().Msgf("[AutoHeadhunting] Results: %s", ans_mp_str)
		log.Info().Msgf("[AutoHeadhunting] Used pulls: %d /  %d", usedPulls, params.TargetPulls)
	}

	log.Info().Msgf("[AutoHeadhunting] Finished with %d pulls, found %d target operators (%s)", usedPulls, targetCount, params.TargetOperator)
	log.Info().Msgf("[AutoHeadhunting] Final results: %v", mp)

	fmt.Printf(t("done"+"\n"), usedPulls, targetCount, params.TargetOperator)
	fmt.Printf(t("final_results"), mp)

	return true
}
