package autoecofarm

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type Rect = maa.Rect

// 初始化常数
const rotate90 = 360 //转一圈需要拖动的像素点
// 2. 定义屏幕的宽高（假设你屏幕分辨率是 1280×720，中心就是 640×360）
const screenW = 1280 // 屏幕宽度（X轴）
const screenH = 720  // 屏幕高度（Y轴）
// 定义干员饭盒的位置,先默认x为屏幕中线，y自定义
const footX = 640

// win32虚拟键位
const (
	KEY_W     = 0x57
	KEY_A     = 0x41
	KEY_S     = 0x53
	KEY_D     = 0x44
	KEY_SHIFT = 0x10
	KEY_CTRL  = 0x11
	KEY_ALT   = 0x12
	KEY_SPACE = 0x20
)

// 拟合参数，用于建立饭盒坐标和角度缩放之间的关系,后续可以在这改
const (
	paramA = 198.2
	paramB = 118.7
)

// 定义获取参数的字段
type MoveToTarget3DParam struct {
	FootY int `json:"FootY"`
}

type RecognitionDetailJson struct {
	Best struct {
		Box maa.Rect `json:"box"`
	} `json:"best"`
}

type MoveToTarget3D struct{}

func (self *MoveToTarget3D) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {

	//初始化结构体默认值
	var params = MoveToTarget3DParam{
		FootY: 500,
	}
	var results = RecognitionDetailJson{}

	//解析 JSON 参数到结构体中
	err := json.Unmarshal([]byte(arg.CustomActionParam), &params)
	if err != nil {
		log.Error().Err(err).Msg("参数解析失败")
		maafocus.NodeActionStarting(ctx, "参数解析失败")
		return false
	}
	msg_foot := fmt.Sprintf("当前角色脚的y坐标是:%d", params.FootY)
	maafocus.NodeActionStarting(ctx, msg_foot)

	err = json.Unmarshal([]byte(arg.RecognitionDetail.DetailJson), &results)
	if err != nil {
		log.Error().Err(err).Msg("识别区域解析失败")
		maafocus.NodeActionStarting(ctx, "识别区域解析失败")
		return false
	}

	msg_json := fmt.Sprintf("Value JSON: %s\n", arg.RecognitionDetail.DetailJson)
	maafocus.NodeActionStarting(ctx, msg_json)

	// 获取目标矩形（arg.Box）的参数（X=左上角X，Y=左上角Y，W=宽度，H=高度）

	targetX := arg.Box.X()      // 目标矩形左上角X
	targetY := arg.Box.Y()      // 目标矩形左上角Y
	targetW := arg.Box.Width()  // 目标矩形宽度（X轴方向）
	targetH := arg.Box.Height() // 目标矩形高度（Y轴方向）

	msg_target := fmt.Sprintf("移动目标在屏幕上的坐标为[%d,%d,%d,%d]", targetX, targetY, targetW, targetH)

	maafocus.NodeActionStarting(ctx, msg_target)
	// 计算目标矩形的中点坐标
	targetCenterX := targetX + targetW/2 // 中点X = 左上角X + 宽度/2
	targetCenterY := targetY + targetH/2 // 中点Y = 左上角Y + 高度/2

	//  计算屏幕中心坐标
	screenCenterX := screenW / 2
	screenCenterY := screenH / 2
	//计算脚的位置

	// 干员脚 指向 目标中点 的向量（目标中点 - 干员脚坐标）
	deltax := float64(targetCenterX - footX)
	deltay := float64(targetCenterY - params.FootY)
	//计算反三角函数，得到x轴到向量的弧度并转换成角度，顺时针为负，逆时针为正（屏幕y轴向下，所以要反一下）
	angleRad := math.Atan2(deltax, -deltay)
	angleDeg := angleRad * 180.0 / math.Pi
	//将屏幕角度通过映射转换成实际角度
	RealangleDeg := Y2Pitch(float64(params.FootY)) / 45 * angleDeg

	msg1 := fmt.Sprintf("需要旋转%.0f°", RealangleDeg)
	maafocus.NodeActionStarting(ctx, msg1)

	swipex := int(angleDeg / 90 * rotate90)
	msg2 := fmt.Sprintf("需要移动%d像素", swipex)
	maafocus.NodeActionStarting(ctx, msg2)

	//转一定角度
	ctx.RunActionDirect("Swipe", maa.NodeSwipeParam{
		Begin:     maa.NewTargetRect(maa.Rect{screenCenterX, screenCenterY, 1, 1}),
		End:       []maa.Target{maa.NewTargetRect(maa.Rect{screenCenterX + swipex, screenCenterY, 1, 1})},
		Duration:  []int64{1000},
		OnlyHover: true,
	}, maa.Rect{0, 0, 0, 0}, nil)
	//转完还得点一下alt加左键解除占用
	ctx.RunActionDirect("KeyDown", maa.NodeKeyDownParam{
		Key: KEY_ALT,
	}, maa.Rect{0, 0, 0, 0}, nil)

	ctx.RunActionDirect("Click", maa.NodeClickParam{}, maa.Rect{0, 0, 0, 0}, nil)

	ctx.RunActionDirect("KeyUp", maa.NodeKeyUpParam{
		Key: KEY_ALT,
	}, maa.Rect{0, 0, 0, 0}, nil)

	return true
}

// ==========================================
// 核心逆算函数：已知屏幕footY坐标，求45°角会变成什么样
// ==========================================
func Y2Pitch(footY float64) float64 {
	// 1. 计算反余弦的输入参数
	denominator := footY - paramB
	if denominator == 0 {
		// 防止除零，返回默认值（比如中间角度）
		return 65.0
	}
	arg := paramA / denominator

	// 2. 【关键保护】限制arg在[-1, 1]之间
	// math.Acos的输入必须在这个区间，否则会返回NaN（非数字）
	if arg > 1.0 {
		arg = 1.0
	}
	if arg < -1.0 {
		arg = -1.0
	}

	// 3. 反余弦计算（得到的是弧度）
	thetaRad := math.Acos(arg)

	// 4. 弧度转角度
	thetaDeg := thetaRad * 180.0 / math.Pi

	// 5. 【可选保护】限制角度在你的标定区间[45°, 85°]内
	// 避免超出标定范围的y值输出不合理的角度
	if thetaDeg < 45.0 {
		thetaDeg = 45.0
	}
	if thetaDeg > 85.0 {
		thetaDeg = 85.0
	}

	return thetaDeg
}
