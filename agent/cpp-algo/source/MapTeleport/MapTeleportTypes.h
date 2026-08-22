#pragma once

#include <string>
#include <vector>

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
    // 进图先缩到最小，此时尺度实测不低于 0.40。下界再往下只会让模板小到
    // 靠极值统计虚高分抢走档位；上界留宽，各区底图尺寸不同不便钉死
    double scaleMin = 0.30;
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

// 地图上的一个传送点位。传送点、基建核心都走这一套：在期望位置附近找出图标、确认、点它。
// 点位之间只有模板图与几个阈值不同，全部由调用方从节点参数喂进来，这里给的缺省是通用传送点那组
struct SpotConfig
{
    // 图标模板名，留空用通用传送点那张。多张时取分最高的一张，用于同一点位有多种样式
    std::vector<std::string> templates;

    // 搜索半径。大于零走浮动搜索，单位底图像素，按视口尺度折算到屏幕；
    // 否则在期望位置开定点小窗，单位屏幕像素
    double radiusBase = 0.0;
    int radiusScreen = 40;

    // 图标不随地图缩放变，但各端界面把它画得不一样大：桌面实测踩 1.000，移动端踩 1.250。
    // 带宽要同时罩住两端，且阶梯得正好落在这两个值上——偏离 5% 分数就掉到 0.6 以下
    double scaleMin = 0.90;
    double scaleMax = 1.35;
    double scaleStep = 0.025;

    // 通用传送点那张模板实测真图标最低 0.76，地图纹理上的背景峰不到 0.45。
    // 换模板必须重标：模板越大背景峰越高，56x56 那两张的背景峰能到 0.654
    double minScore = 0.55;

    // 命中与期望位置的偏差上限，单位底图像素。同区传送点两两最近 23.5，
    // 此值留出两倍余量，认错点在几何上就不成立。浮动搜索时窗口本身就是闸，这一项不参与
    double gateBase = 10.0;

    // 没解锁的图标只是褪成灰的，形状一模一样，归一化相关对整体明暗不敏感、判不出来。
    // 解锁与否因此单独判：模板自身哪些像素是金的，就去实拍的同一批像素上量饱和度。
    // 这一项只判解锁、不参与判真假——没有图标的地方也能到 0.54、越得过这道闸。
    // 缺省不判：通用传送点那张模板从没在未解锁实拍上标定过，贸然武装会把认得出的点判成锁着的。
    // TODO: 核心那两张的 0.50 也只在已解锁的实拍上标定过。手头没有未解锁的账号，拿不到实拍，
    // 未解锁那一侧至今没有验证过。有条件的开发者请补测，确认后删掉本注释
    int saturationFloor = 60;
    double minGoldRatio = 0.0;
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

struct SpotHit
{
    std::string templateName;
    cv::Point2d center { 0.0, 0.0 };
    cv::Size size { 0, 0 };
    double score = 0.0;
    double matchScale = 0.0;
    double offsetBase = 0.0;

    // 只在配了解锁判据时有意义，否则恒为 0 与 true
    double goldRatio = 0.0;
    bool unlocked = true;
};

struct PlayerMarkerHit
{
    cv::Point2d center { 0.0, 0.0 };
    int area = 0;
    double solidity = 0.0;
};

} // namespace mapteleport
