package mapservice

import (
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	MaxLostTrackingCount = 3
	MinMatchScore        = 0.7
	MobileSearchRadius   = 150.0
)

type MapLocator struct {
	basePath     string
	debugDir     string
	lastKnownPos *MapPosition
	lostTracking int

	// 多区域支持
	zones         map[string]*image.RGBA
	currentZoneID string

	velocityX float64 // px/s
	velocityY float64 // px/s
	lastTime  time.Time

	maskAlpha *image.Alpha
	debug     bool

	// Buffers
	workBuffer   *image.RGBA // 用于遮罩处理的小地图
	searchBuffer *image.RGBA // 用于搜索操作的复用缓冲区
}

func NewMapLocator(zoneConfigs map[string]string, debug bool, debugDir string) (*MapLocator, error) {
	if debugDir == "" {
		debugDir = os.TempDir()
	}

	loc := &MapLocator{
		debugDir:     debugDir,
		zones:        make(map[string]*image.RGBA),
		debug:        debug,
		lostTracking: MaxLostTrackingCount + 1, // 初始判定为丢失
	}

	for id, path := range zoneConfigs {
		log.Info().Str("zone", id).Str("path", path).Msg("Loading map zone...")
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open map zone %s: %w", id, err)
		}

		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to decode map zone %s: %w", id, err)
		}

		// Ensure RGBA
		fullRGBA := EnsureRGBA(img)

		// Tier 地图遮罩
		isTierMap := !strings.HasSuffix(id, "_Base")
		if isTierMap {
			log.Info().Str("zone", id).Msg("Applying spotlight mask to Tier map...")
			ApplySpotlightEffect(fullRGBA, 45) // Tier地图亮度阈值
		} else {
			// 透明化
			log.Info().Str("zone", id).Msg("Applying Void Filter (Transparent to Base map...")
			ApplyVoidFilter(fullRGBA, 30) // Base地图过滤暗部阈值
		}

		loc.zones[id] = fullRGBA
		log.Info().Str("zone", id).Int("w", fullRGBA.Bounds().Dx()).Int("h", fullRGBA.Bounds().Dy()).Msg("Zone loaded")

		// 参考路径
		if loc.basePath == "" {
			loc.basePath = filepath.Dir(path)
		}
	}

	if len(loc.zones) == 0 {
		return nil, fmt.Errorf("no map zones loaded")
	}

	// 预计算圆形遮罩
	loc.maskAlpha = GenerateCircularMask(MinimapROIWidth, MinimapROIHeight)

	return loc, nil
}

func (m *MapLocator) Close() {
	m.zones = nil
}

func (m *MapLocator) Locate(ctx *maa.Context, minimap image.Image) (*MapPosition, error) {
	// 确保小地图为 RGBA 并应用遮罩
	bounds := minimap.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// 检查遮罩尺寸
	if m.maskAlpha == nil || m.maskAlpha.Bounds().Dx() != w || m.maskAlpha.Bounds().Dy() != h {
		m.maskAlpha = GenerateCircularMask(w, h)
	}

	// 缓冲区复用：workBuffer
	if m.workBuffer == nil || m.workBuffer.Bounds().Dx() != w || m.workBuffer.Bounds().Dy() != h {
		m.workBuffer = image.NewRGBA(image.Rect(0, 0, w, h))
	}

	// 应用遮罩
	ApplyMaskFastInto(minimap, m.workBuffer, m.maskAlpha)
	// 过滤 UI 图标
	maskIcons(m.workBuffer)
	maskedMinimap := m.workBuffer // 别名

	now := time.Now()

	if m.currentZoneID != "" && m.lastKnownPos != nil && m.lostTracking <= MaxLostTrackingCount {
		zoneImg, ok := m.zones[m.currentZoneID]
		if ok {
			// 运动预测
			dt := now.Sub(m.lastTime).Seconds()
			if dt > 0.5 {
				dt = 0
				m.velocityX = 0
				m.velocityY = 0
			}
			predX := m.lastKnownPos.X + m.velocityX*dt
			predY := m.lastKnownPos.Y + m.velocityY*dt

			// 定义搜索矩形，稀疏搜索
			cx, cy := int(predX), int(predY)
			r := int(MobileSearchRadius)
			pad := r + (w+h)/2
			fullBounds := zoneImg.Bounds()
			searchRect := image.Rect(cx-pad, cy-pad, cx+pad, cy+pad).Intersect(fullBounds)

			if !searchRect.Empty() {
				// Copy ROI
				searchImg := m.copyToSearchBuffer(zoneImg, searchRect)

				localX, localY, score := MatchTemplateRGBA(searchImg, maskedMinimap, 2, 0, 4)

				if score > 0.85 {
					finalX := searchRect.Min.X + localX
					finalY := searchRect.Min.Y + localY

					// 快速微调（4px 半径）
					// 修正步进带来的误差。
					fineRadius := 4
					fineROI := image.Rect(
						finalX-fineRadius, finalY-fineRadius,
						finalX+w+fineRadius, finalY+h+fineRadius,
					).Intersect(zoneImg.Bounds())
					fineImg := copySubImage(zoneImg, fineROI)
					fx, fy, fScore := MatchTemplateRGBA(fineImg, maskedMinimap, 1, 0, 1)

					// 若微调更佳则使用之
					resultX, resultY, resultScore := float64(finalX), float64(finalY), score
					if fScore > score {
						resultX = float64(fineROI.Min.X + fx)
						resultY = float64(fineROI.Min.Y + fy)
						resultScore = fScore
					}

					pos := &MapPosition{
						ZoneID: m.currentZoneID,
						X:      resultX + float64(w)/2,
						Y:      resultY + float64(h)/2,
						Score:  resultScore,
					}

					// 更新状态并立即返回
					m.updateMotionModel(pos, now)
					m.lastKnownPos = pos
					m.lastTime = now
					m.lostTracking = 0

					return pos, nil
				}
			}
		}
	}

	// --- 步骤 2：全局竞速（回退） ---
	// 仅在追踪失败或分数低时运行

	type raceResult struct {
		ZoneID string
		X, Y   int
		Score  float64
	}

	resultsCh := make(chan raceResult, len(m.zones))
	var wg sync.WaitGroup

	// Global Params: Step=4, Skip=2, SampleRate=20
	coarseStep := 4
	coarseSkip := 2
	coarseSampleRate := 20

	for zID, zImg := range m.zones {
		wg.Add(1)
		go func(id string, img *image.RGBA) {
			defer wg.Done()
			bx, by, score := MatchTemplateRGBA(img, maskedMinimap, coarseStep, coarseSkip, coarseSampleRate)
			resultsCh <- raceResult{ZoneID: id, X: bx, Y: by, Score: score}
		}(zID, zImg)
	}

	wg.Wait()
	close(resultsCh)

	var winner raceResult
	maxScore := -1.0

	for res := range resultsCh {
		if res.Score > maxScore {
			maxScore = res.Score
			winner = res
		}
	}

	var bestResult *MapPosition

	if maxScore > 0.5 {
		// Refine Global Winner
		winnerZone := m.zones[winner.ZoneID]
		coarseX, coarseY := winner.X, winner.Y
		fineRadius := 20
		fineROI := image.Rect(
			coarseX-fineRadius, coarseY-fineRadius,
			coarseX+w+fineRadius, coarseY+h+fineRadius,
		).Intersect(winnerZone.Bounds())

		fineImg := copySubImage(winnerZone, fineROI)
		// Fine Search: Step=1, SampleRate=1
		localX, localY, fineScore := MatchTemplateRGBA(fineImg, maskedMinimap, 1, 0, 1)

		if fineScore > MinMatchScore {
			finalX := fineROI.Min.X + localX
			finalY := fineROI.Min.Y + localY
			cx := float64(finalX) + float64(w)/2
			cy := float64(finalY) + float64(h)/2

			bestResult = &MapPosition{
				ZoneID: winner.ZoneID,
				X:      cx, Y: cy, Score: fineScore,
			}
		}
	}

	// Result
	if bestResult != nil {
		// 若切换区域则重置速度。
		if m.currentZoneID != bestResult.ZoneID {
			m.velocityX = 0
			m.velocityY = 0
		} else {
			m.updateMotionModel(bestResult, now)
		}

		m.currentZoneID = bestResult.ZoneID
		m.lastKnownPos = bestResult
		m.lastTime = now
		m.lostTracking = 0

		log.Info().Str("zone", bestResult.ZoneID).
			Float64("score", bestResult.Score).
			Msg("Global Match Resolved")

		return bestResult, nil
	}

	m.lostTracking++
	if m.lostTracking > MaxLostTrackingCount {
		m.lastKnownPos = nil
	}
	return nil, nil
}

func (m *MapLocator) updateMotionModel(newPos *MapPosition, now time.Time) {
	dt := now.Sub(m.lastTime).Seconds()
	if m.lastKnownPos != nil && dt > 0.016 && dt < 1.0 && m.lostTracking == 0 {
		rawVx := (newPos.X - m.lastKnownPos.X) / dt
		rawVy := (newPos.Y - m.lastKnownPos.Y) / dt
		alpha := 0.5
		m.velocityX = m.velocityX*(1-alpha) + rawVx*alpha
		m.velocityY = m.velocityY*(1-alpha) + rawVy*alpha
	} else if m.lostTracking > 0 {
		m.velocityX = 0
		m.velocityY = 0
	}
}

// copyToSearchBuffer 保留复用缓冲区逻辑（与 MapLocator 状态绑定）
func (m *MapLocator) copyToSearchBuffer(src *image.RGBA, r image.Rectangle) *image.RGBA {
	w, h := r.Dx(), r.Dy()
	needed := w * h * 4

	if m.searchBuffer == nil || cap(m.searchBuffer.Pix) < needed {
		m.searchBuffer = image.NewRGBA(image.Rect(0, 0, w, h))
	} else {
		m.searchBuffer.Pix = m.searchBuffer.Pix[:needed]
		m.searchBuffer.Stride = 4 * w
		m.searchBuffer.Rect = image.Rect(0, 0, w, h)
	}

	draw.Draw(m.searchBuffer, m.searchBuffer.Bounds(), src, r.Min, draw.Src)
	return m.searchBuffer
}

func (m *MapLocator) saveDebugImage(name string, img image.Image) {
	SaveDebugImage(m.debugDir, name, img)
}

func (m *MapLocator) GetLastKnownPos() *MapPosition {
	return m.lastKnownPos
}
