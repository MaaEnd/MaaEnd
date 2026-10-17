#pragma once

#include <MaaUtils/NoWarningCV.hpp>

namespace maplocator
{

inline constexpr int kOrientationRoiWidth = 118;
inline constexpr int kOrientationRoiHeight = 120;
inline constexpr int kOrientationStripHeight = 42;
inline constexpr int kOrientationStripWidth = 360;

struct OrientationStrips
{
    cv::Mat observed;  // CV_8UC3, 观测条带 BGR
    cv::Mat reference; // CV_8UC4, 参考条带 BGR（白底合成）+ alpha
};

// 按定位结果 (x, y, scale) 把小地图 ROI 与 zone 底图展开成极坐标条带：
// 行 = 半径（内径 12 px 到外径 54 px 均分 42 行），列 = 方位角（北为 0、顺时针 1 度/列，360 列）。
// 观测条带在小地图内双线性采样、越界取边缘；参考条带按资产坐标采样、越界取 0，
// BGR 在资产透明处向白底过渡、alpha 保留原值。
// minimap: CV_8UC3 BGR 观测 ROI (720p 基准下 118x120)。
// asset:   CV_8UC4 BGRA zone 底图，尺寸动态。
// 输入不符合契约时返回 false。
bool BuildOrientationStrips(const cv::Mat& minimap, const cv::Mat& asset, float x, float y, float scale, OrientationStrips& out);

} // namespace maplocator
