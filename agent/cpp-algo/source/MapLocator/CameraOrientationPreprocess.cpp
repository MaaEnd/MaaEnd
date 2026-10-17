#include "CameraOrientationPreprocess.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <cstdint>
#include <numbers>

namespace maplocator
{

namespace
{

// 极坐标条带几何：极点取小地图 ROI 中心，半径从内径到外径，方位角北为 0、顺时针。
constexpr float kPoleU = 59.0f;
constexpr float kPoleV = 60.0f;
constexpr float kInnerRadius = 12.0f;
constexpr float kOuterRadius = 54.0f;
constexpr float kRadiusStep = (kOuterRadius - kInnerRadius) / static_cast<float>(kOrientationStripHeight);

struct AzimuthTable
{
    std::array<float, kOrientationStripWidth> sin {};
    std::array<float, kOrientationStripWidth> cos {};
};

const AzimuthTable& GetAzimuthTable()
{
    static const AzimuthTable table = [] {
        AzimuthTable result;
        for (int i = 0; i < kOrientationStripWidth; ++i) {
            const float angle = static_cast<float>(i) * std::numbers::pi_v<float> / 180.0f;
            result.sin[i] = std::sin(angle);
            result.cos[i] = std::cos(angle);
        }
        return result;
    }();
    return table;
}

uint8_t ToByte(float value)
{
    return static_cast<uint8_t>(std::clamp(std::lround(value), 0L, 255L));
}

// 双线性采样一个像素写入 out（长度 image.channels()）。
// clamp_to_edge 为真时越界取边缘（观测条带），否则越界取 0（参考条带）。
void SampleBilinear(const cv::Mat& image, float x, float y, bool clamp_to_edge, float* out)
{
    const int channels = image.channels();
    const int width = image.cols;
    const int height = image.rows;

    const auto at = [&](int row, int col, int channel) -> float {
        if (row < 0 || row >= height || col < 0 || col >= width) {
            if (!clamp_to_edge) {
                return 0.0f;
            }
            row = std::clamp(row, 0, height - 1);
            col = std::clamp(col, 0, width - 1);
        }
        return image.ptr<uint8_t>(row)[col * channels + channel];
    };

    const int x0 = static_cast<int>(std::floor(x));
    const int y0 = static_cast<int>(std::floor(y));
    const float fx = x - static_cast<float>(x0);
    const float fy = y - static_cast<float>(y0);
    for (int channel = 0; channel < channels; ++channel) {
        const float top = at(y0, x0, channel) * (1.0f - fx) + at(y0, x0 + 1, channel) * fx;
        const float bottom = at(y0 + 1, x0, channel) * (1.0f - fx) + at(y0 + 1, x0 + 1, channel) * fx;
        out[channel] = top * (1.0f - fy) + bottom * fy;
    }
}

} // namespace

bool BuildOrientationStrips(const cv::Mat& minimap, const cv::Mat& asset, float x, float y, float scale, OrientationStrips& out)
{
    if (minimap.type() != CV_8UC3 || minimap.cols != kOrientationRoiWidth || minimap.rows != kOrientationRoiHeight) {
        return false;
    }
    if (asset.type() != CV_8UC4 || asset.empty() || !std::isfinite(x) || !std::isfinite(y) || !std::isfinite(scale)) {
        return false;
    }

    const AzimuthTable& azimuth = GetAzimuthTable();
    out.observed.create(kOrientationStripHeight, kOrientationStripWidth, CV_8UC3);
    out.reference.create(kOrientationStripHeight, kOrientationStripWidth, CV_8UC4);

    for (int row = 0; row < kOrientationStripHeight; ++row) {
        const float radius = kInnerRadius + kRadiusStep * (static_cast<float>(row) + 0.5f);
        uint8_t* observed = out.observed.ptr<uint8_t>(row);
        uint8_t* reference = out.reference.ptr<uint8_t>(row);
        for (int col = 0; col < kOrientationStripWidth; ++col) {
            // ROI 像素坐标（u 右、v 下）。
            const float u = kPoleU + radius * azimuth.sin[col];
            const float v = kPoleV - radius * azimuth.cos[col];

            float obs[3];
            SampleBilinear(minimap, u, v, true, obs);
            for (int channel = 0; channel < 3; ++channel) {
                observed[col * 3 + channel] = ToByte(obs[channel]);
            }

            // 底图坐标 = 极点定位 (x, y) + 采样点相对极点的偏移 * scale。
            float ref[4];
            SampleBilinear(asset, x + (u - kPoleU) * scale, y + (v - kPoleV) * scale, false, ref);

            // 参考 BGR 在资产透明处向白底过渡，alpha 保留原值。
            const float alpha = ref[3] / 255.0f;
            for (int channel = 0; channel < 3; ++channel) {
                reference[col * 4 + channel] = ToByte(ref[channel] * alpha + 255.0f * (1.0f - alpha));
            }
            reference[col * 4 + 3] = ToByte(ref[3]);
        }
    }
    return true;
}

} // namespace maplocator
