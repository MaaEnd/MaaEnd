//go:build linux

package ziplineimport

import (
	"net/url"
	"os"
	"path/filepath"
	"sort"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
)

// Action 是 ZiplineImport 自定义动作的 Linux 实现。
//
// 与 win32 WebView2 版同目标（把滑索写入 debug/record/Ziplines.json），取数方式不同：
// 用本地受限 MITM 代理抄下“浏览器自己发出的带签名 mark/list 响应”，解析 data.saveMarks。
// 落盘不做类型过滤、全量保留（与 win32 的空过滤语义一致）：供电桩等供电结构必须随滑索架
// 一并入库，寻路的通电判定依赖它们。完成判据与 win32 参考对齐（见 runCapture）。
//
// 隐私边界：登录凭据当次使用、当次清理——每次运行全新临时 profile（结束即删）；根 CA 一次性
// 生成、私有 key 不进磁盘；只对白名单主机（zonai.skland.com）解密且只留 mark/list 响应体
// 在内存用于解析；落盘的只有森空岛地图标记本身。
type Action struct{}

func (a *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	p := loadParam(ctx, arg.CurrentTaskName)

	if u, err := url.Parse(p.URL); err == nil && isGlobalRegionHost(u.Hostname()) {
		log.Error().Str("component", componentName).Str("url", p.URL).
			Msg("zipline import: global server (SKPORT) is not supported on Linux")
		maafocus.Print(ctx, i18n.T("ziplineimport.region_unsupported"))
		return false
	}

	profileDir, err := newTempProfile()
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("zipline import: create temp profile failed")
		return false
	}
	defer os.RemoveAll(profileDir) // 不留登录态 / 凭据 / CA 证书

	// 一次性根 CA：证书写进临时 profile（随 profile 一起删），私钥只留内存。
	caCertPath := filepath.Join(profileDir, "ca.crt")
	ca, err := generateRootCA(caCertPath)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("zipline import: generate root CA failed")
		return false
	}

	proxy := newMitmProxy(p.ProxyPort)
	defer proxy.stop()
	if _, err := proxy.start(ca); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("zipline import: start mitm proxy failed")
		return false
	}
	pacURL, err := proxy.startPAC()
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("zipline import: start pac endpoint failed")
		return false
	}
	if err := prepareProfile(profileDir, pacURL, caCertPath); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("zipline import: prepare profile failed")
		return false
	}

	expected := expectedMaps()
	if !runCapture(ctx, p, profileDir, proxy, expected) {
		return false
	}

	// 把本次抓到的响应归集为「唯一账号 + 按地图分组」的标记（与 cpp PersistCaptured 一致：
	// 所有出现过的图都落，空图不动，已抓到的按部分成功处理）。一次导入必须恰好对应一个
	// roleId，否则整批拒绝；不按 template 过滤：供电结构必须随滑索架一并入库。
	responses := proxy.responsesSnapshot()
	accountID, roleID, byMap, err := accountScopedMarks(responses, p.TemplateIDs)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Int("responses", len(responses)).
			Msg("zipline import: cannot attribute captured marks to a single account, refuse to persist")
		return false
	}

	rec, err := loadRecord(defaultRecordPath())
	if err != nil {
		log.Error().Err(err).Str("component", componentName).
			Str("path", defaultRecordPath()).
			Msg("zipline import: existing record is broken, refuse to overwrite")
		return false
	}

	okMaps, totalRacks := 0, 0
	// 提示里的「滑索架」数量只统计标定 types[] 认识的类型；落盘仍全量保留。
	towers := towerTemplateIDs()
	for mapID, marks := range byMap {
		marks = dedupMarks(marks)
		if len(marks) == 0 {
			continue
		}

		// 逐 templateId 报数（排序保证日志稳定），与 cpp PersistCaptured 一致：标记类型只有编号
		// 没有名字，拿这行日志和游戏里数出来的数量对照，就能认出哪个编号是滑索、哪个是别的标记。
		byTemplate := make(map[string]int)
		for _, m := range marks {
			byTemplate[m.TemplateID]++
		}
		templateIDs := make([]string, 0, len(byTemplate))
		for id := range byTemplate {
			templateIDs = append(templateIDs, id)
		}
		sort.Strings(templateIDs)
		for _, id := range templateIDs {
			log.Info().Str("component", componentName).Str("map_id", mapID).
				Str("template_id", id).Int("count", byTemplate[id]).
				Msg("zipline import: template marks captured")
		}

		rec.replaceMap(ziplineMapRecord{AccountID: accountID, MapID: mapID, FetchedAt: currentTimestampUTC(), Marks: marks})
		okMaps++
		for _, m := range marks {
			if towers[m.TemplateID] {
				totalRacks++
			}
		}
		log.Info().Str("component", componentName).Str("map_id", mapID).Int("marks", len(marks)).
			Msg("zipline import: map marks captured")
	}

	if okMaps == 0 {
		// 一条滑索都没有：最常见的原因是自始至终未登录，或抓到的标记里没有滑索架。
		log.Error().Str("component", componentName).
			Msg("zipline import: no zipline racks captured, was the page signed in?")
		return false
	}
	if err := rec.save(defaultRecordPath()); err != nil {
		log.Error().Err(err).Str("component", componentName).
			Str("path", defaultRecordPath()).Msg("zipline import: save record failed")
		return false
	}

	log.Info().Str("component", componentName).Str("account_id", accountID).
		Int("ok_maps", okMaps).Int("total_racks", totalRacks).
		Msg("zipline import: done")
	// 抓到数据后给用户一条焦点提示（原始 UID 仅临时展示，不落盘）。
	maafocus.Print(ctx, i18n.T("ziplineimport.captured_done", roleID, okMaps, totalRacks))
	return true
}
