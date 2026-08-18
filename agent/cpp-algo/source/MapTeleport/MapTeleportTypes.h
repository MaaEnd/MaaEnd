#pragma once

#include <string>

#include <MaaUtils/NoWarningCV.hpp>

namespace mapteleport
{

// 大地图铺满全屏、UI 面板浮在四周。取中心一块参与视口求解，
// 比例边界避开左侧图层列表、右侧详情面板、顶栏与底部按钮。
struct ScreenMapRoi
{
    double left = 0.26;
    double top = 0.12;
    double right = 0.64;
    double bottom = 0.82;
};

struct ViewportConfig
{
    ScreenMapRoi roi {};

    // 粗解在降采样图上按等比阶梯扫全尺度带，细解回到原尺度只扫粗解邻域。
    // 尺度是等比量，线性步长在低档过密、高档过疏，所以阶梯用比例而非增量
    int coarseDownscale = 4;
    double scaleMin = 0.20;
    double scaleMax = 4.00;
    double coarseRatio = 1.10;
    int fineSteps = 15;

    // 模板逼近搜索图尺寸时可落位置太少，归一化相关会给出虚高分。
    // 这里按全分辨率屏幕像素计，粗解阶段自行折算到降采样尺度
    int scanSlack = 24;

    // 求解不出可信视口就不给坐标，宁可让上层重来
    double minScore = 0.50;
    double minDelta = 0.02;
};

struct AnchorConfig
{
    // 确认只在期望位置的小窗内做，屏幕别处的同款图标一律看不见
    int searchRadius = 40;

    // 图标是固定屏幕尺寸的，不随地图缩放变：最佳尺度恒为 1.000，
    // 偏离 5% 分数就掉到 0.6 以下。阶梯必须正好踩到 1.000
    double scaleMin = 0.90;
    double scaleMax = 1.15;
    double scaleStep = 0.025;

    // 实测真图标最低 0.76，地图纹理上的背景峰不到 0.45
    double minScore = 0.55;

    // 命中与期望位置的偏差上限，单位是底图像素。
    // 同区传送点两两最近 23.5，此值留出两倍余量，认错点在几何上就不成立
    double gateBase = 10.0;
};

// 基建核心区的传送图标随玩家在区域里的摆放浮动，坐标只圈得住范围、圈不准点位，
// 所以是在这块范围里搜模板，而不是到定点上确认。两种样式各一张模板，取分高的那张
struct CoreIconConfig
{
    // 浮动区中心到角的距离，单位底图像素。判假由分数闸负责，窗口只管别漏，宁大勿小
    double searchBase = 60.0;

    // 同一个图标在各端界面里画得不一样大，模板因此按各端自己的显示尺寸分层出货，
    // 与锚点模板同一前提，阶梯于是两端都正好踩到 1.000
    double scaleMin = 0.90;
    double scaleMax = 1.15;
    double scaleStep = 0.025;

    // 同一批实拍上量的两端：真图标最低 0.914，没有图标的地方最高 0.654。
    // 同一个图标在不同帧之间抖 0.014，约为余量的七分之一
    double minScore = 0.82;

    // 没解锁的图标只是褪成灰的，形状一模一样，归一化相关对整体明暗不敏感、判不出来。
    // 解锁与否因此单独判：模板自身哪些像素是金的，就去实拍的同一批像素上量饱和度。
    // 已解锁的实测 0.74~0.98，而没有图标的地方也能到 0.54、越得过这道闸，
    // 所以这一项只判解锁、不参与判真假，真假一律由分数闸定。
    // TODO: 这两个阈值只在已解锁的实拍上标定过。手头没有未解锁的账号，拿不到实拍，
    // 未解锁那一侧至今没有验证过。有条件的开发者请补测，确认后删掉本注释
    int saturationFloor = 60;
    double minGoldRatio = 0.50;
};

// 角色标记是压在锚点图标之上的实心白三角，它出现在期望位置本身就是图标认不出来的原因。
// 判据是「近白连通块 + 面积落区间 + 凸实」，三项都与朝向无关。面积按 720p 采集分辨率定，
// 与锚点模板同一前提。实心三角凸实度实测 0.93，同亮度同面积的白色地图装饰只有 0.59~0.66
struct PlayerMarkerConfig
{
    int searchRadius = 32;
    int whiteFloor = 240;
    int minArea = 60;
    int maxArea = 300;
    double minSolidity = 0.80;
};

// 屏幕 <-> 底图的相似变换：base = (screen - roiOrigin) * scale + baseOrigin
struct Viewport
{
    double scale = 0.0;
    cv::Point2d roiOrigin { 0.0, 0.0 };
    cv::Point2d baseOrigin { 0.0, 0.0 };
    cv::Size roiSize { 0, 0 };

    double score = 0.0;
    double delta = 0.0;
    double psr = 0.0;

    cv::Point2d toBase(const cv::Point2d& screen) const
    {
        return { (screen.x - roiOrigin.x) * scale + baseOrigin.x, (screen.y - roiOrigin.y) * scale + baseOrigin.y };
    }

    cv::Point2d toScreen(const cv::Point2d& base) const
    {
        return { (base.x - baseOrigin.x) / scale + roiOrigin.x, (base.y - baseOrigin.y) / scale + roiOrigin.y };
    }
};

struct AnchorHit
{
    cv::Point2d center { 0.0, 0.0 };
    cv::Size size { 0, 0 };
    double score = 0.0;
    double matchScale = 0.0;
    double offsetBase = 0.0;
};

struct PlayerMarkerHit
{
    cv::Point2d center { 0.0, 0.0 };
    int area = 0;
    double solidity = 0.0;
};

struct CoreIconHit
{
    cv::Point2d center { 0.0, 0.0 };
    cv::Size size { 0, 0 };
    double score = 0.0;
    double matchScale = 0.0;
    double goldRatio = 0.0;
    bool unlocked = false;
};

} // namespace mapteleport
