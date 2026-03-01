package autoecofarm

import (
	_ "embed"
	"math"

	"github.com/MaaXYZ/maa-framework-go/v4"
)

// 初始化常数
const rotate90 = 360 //转一圈需要拖动的像素点
// 2. 定义屏幕的宽高（假设你屏幕分辨率是 1280×720，中心就是 640×360）
const screenW = 1280 // 屏幕宽度（X轴）
const screenH = 720  // 屏幕高度（Y轴）

type MoveToTarget3D struct{}

func (self *MoveToTarget3D) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 获取目标矩形（arg.Box）的参数（X=左上角X，Y=左上角Y，W=宽度，H=高度）
	targetX := arg.Box.X()      // 目标矩形左上角X
	targetY := arg.Box.Y()      // 目标矩形左上角Y
	targetW := arg.Box.Width()  // 目标矩形宽度（X轴方向）
	targetH := arg.Box.Height() // 目标矩形高度（Y轴方向）

	// 计算目标矩形的中点坐标
	targetCenterX := targetX + targetW/2 // 中点X = 左上角X + 宽度/2
	targetCenterY := targetY + targetH/2 // 中点Y = 左上角Y + 高度/2

	//  计算屏幕中心坐标
	screenCenterX := screenW / 2
	screenCenterY := screenH / 2

	// 屏幕中心 指向 目标中点 的向量（目标中点 - 屏幕中心）
	deltax := float64(targetCenterX - screenCenterX)
	deltay := float64(targetCenterY - screenCenterY)
	//计算反三角函数，得到x轴到向量的弧度并转换成角度，顺时针为负，逆时针为正（屏幕y轴向下，所以要反一下）
	angleRad := math.Atan2(deltax, -deltay)
	angleDeg := angleRad * 180.0 / math.Pi

	swipex := int(angleDeg / 90 * rotate90)

	//转一定角度
	ctx.RunActionDirect("Swipe", maa.NodeSwipeParam{
		Begin:     maa.NewTargetRect(maa.Rect{screenCenterX, screenCenterY, 1, 1}),
		End:       []maa.Target{maa.NewTargetRect(maa.Rect{screenCenterX + swipex, screenCenterY, 1, 1})},
		OnlyHover: true,
	}, maa.Rect{0, 0, 0, 0}, nil)

	return true
}

func Calculate(roi maa.Rect) (int, int, int, int) {
	centerX := roi.X() + roi.Width()/2
	centerY := roi.Y() + roi.Height()/2
	return centerX, centerY, 0, 0
}
