#pragma once

#include <string>
#include <vector>

#include <MaaUtils/NoWarningCV.hpp>

namespace worldmap
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

    // 整窗判失败后改用分块投票再解一次：模板切成 voteGrid×voteGrid 块各自匹配，
    // 逐像素取中位数。归一化相关整窗只出一个数，搜索窗里有未探索迷雾这类局部遮挡
    // 就整帧塌掉；分块之后被污染的块只是少数票。置 1 关掉这条回退路径
    int voteGrid = 3;

    // 每块的最小边长。整模板那道 kMinTemplateSide 卡的是别的事，
    // 拿来卡每块会要求尺度不低于 0.60，把低倍率档整档筛掉——实测工作档低到 0.4317
    int voteMinBlockSide = 16;
};

// 地图上一类图标的认法：认它的模板图，加上只对这张图成立的那几个阈值。
// 全部来自图标表，调用方只报图标名。缺省值是新图标没写全时的起点，不是哪一张的标定
struct SpotConfig
{
    // 同一类图标有多种样式时并列，取分最高的一张
    std::vector<std::string> templates;

    // 搜索半径。大于零走浮动搜索，单位底图像素，按视口尺度折算到屏幕；
    // 否则在期望位置开定点小窗，单位屏幕像素
    double radiusBase = 0.0;
    int radiusScreen = 40;

    // 图标不随地图缩放变，但各张模板的出货尺寸不成比例：同一轮实测通用传送点那张踩 1.250、
    // 核心那两张踩 1.000。带宽要罩住这两档且阶梯正好落上去——偏离 5% 分数就掉到 0.6 以下
    double scaleMin = 0.90;
    double scaleMax = 1.35;
    double scaleStep = 0.025;

    // 模板越大背景峰越高：通用传送点那张真图标最低 0.76、背景峰不到 0.45，
    // 核心那两张 69x69 的背景峰能到 0.654。每张模板都得自己标一遍
    double minScore = 0.55;

    // 命中与期望位置的偏差上限，单位底图像素。同区传送点两两最近 23.5，
    // 此值留出两倍余量，认错点在几何上就不成立。浮动搜索时窗口本身就是闸，这一项不参与
    double gateBase = 10.0;

    // 没解锁的图标只是褪成灰的，形状一模一样，归一化相关对整体明暗不敏感、判不出来。
    // 解锁与否因此单独判：模板自身哪些像素是金的，就去实拍的同一批像素上量饱和度。
    // 这一项只判解锁、不参与判真假——没有图标的地方也能到 0.54、越得过这道闸。
    // 缺省不判：只有在未解锁实拍上标定过的图标才配得起这个值，
    // 没标就武装会把认得出的点判成锁着的
    int saturationFloor = 60;
    double minGoldRatio = 0.0;
};

// 图标表里的一条
struct IconSpec
{
    SpotConfig spot {};

    // 角色标记压在这类图标之上：认不出图标时，标记落在期望位置就是认不出的原因。
    // 只对会与角色重合的点位成立，别的图标开了只会放宽误判
    bool occludedByPlayer = false;
};

// 角色标记是压在图标之上的实心白三角。判据是「近白连通块 + 面积落区间 + 凸实」，
// 三项都与朝向无关。面积按 720p 采集分辨率定。实心三角凸实度实测 0.93，
// 同亮度同面积的白色地图装饰只有 0.59~0.66
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

    // 这个解是哪条路径给的：1 是整窗匹配，大于 1 是分块投票用的网格边长
    int voteGrid = 1;

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

    // 该点哪一点：图标本体最厚实处，已折算到屏幕。模板框里图标只占一成多，
    // 交整框出去会被框架在框内随机取点，落到图标旁边的空地上
    cv::Point2d hotspot { 0.0, 0.0 };
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

} // namespace worldmap
