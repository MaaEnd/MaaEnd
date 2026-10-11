#include "CameraOrientationPreprocess.h"

#include <algorithm>
#include <array>
#include <bit>
#include <cmath>
#include <cstdint>
#include <utility>

// 逐字节对齐依赖 float32 的逐步舍入：本文件禁止把 a * b + c 收缩成 FMA，由 pragma 随源码保证。
// GCC 不支持 STDC FP_CONTRACT，用 GCC 构建时须另加 -ffp-contract=off（MaaEnd 的非 MSVC 构建只用 clang）。
#if defined(__clang__)
#pragma STDC FP_CONTRACT OFF
#elif defined(_MSC_VER)
#pragma fp_contract(off)
#endif

namespace maplocator
{

namespace
{

// endfield/preprocess.py 的几何常量。
constexpr float kRoiPoleU = 59.0f;
constexpr float kRoiPoleV = 60.0f;
constexpr float kInnerRadius = 12.0f;
constexpr float kOuterRadius = 54.0f;
constexpr float kRadiusStep = (kOuterRadius - kInnerRadius) / static_cast<float>(kOrientationStripHeight);
// 采样点相对极点的最大偏移 = 最大半径（外径 - 半步长）。
constexpr float kMaxRadius = kInnerRadius + (static_cast<float>(kOrientationStripHeight) - 0.5f) * kRadiusStep;
// 参考窗口在采样范围外再留的边（像素），覆盖双线性的 floor / floor + 1 两个支撑像素。
constexpr float kWindowPad = 2.0f;

// torch.sin / torch.cos(torch.deg2rad(arange(360, float32))) 的 float32 位模式。
// 三角函数是预处理里唯一依赖 libm 的部分，不同平台在最后一位上会有出入，直接取定义的值；
// 半径与 u / v 的乘加仍在这里按定义的 float32 顺序计算。
#include "CameraOrientationAzimuthTable.inc"

enum class Padding
{
    Zeros,
    Border,
};

// align_corners = false 下像素中心坐标 -> grid 值 -> 实际采样坐标；与图内一样走一遍以保留 float32 舍入。
float GridRoundTrip(float coord, int length)
{
    const float len = static_cast<float>(length);
    const float normalized = (2.0f * coord + 1.0f) / len - 1.0f;
    return ((normalized + 1.0f) * len - 1.0f) / 2.0f;
}

// 与 torch F.grid_sample(bilinear) 一致：border 先把坐标钳到图内再取 floor，zeros 在图外读 0；
// 先算四角权重再按 nw、ne、sw、se 顺序求和。
template <int Channels, Padding Mode>
void SampleBilinear(const cv::Mat& image, float x, float y, float* out)
{
    const int width = image.cols;
    const int height = image.rows;
    if constexpr (Mode == Padding::Border) {
        x = std::clamp(x, 0.0f, static_cast<float>(width - 1));
        y = std::clamp(y, 0.0f, static_cast<float>(height - 1));
    }
    else {
        // 坐标不在 [-1, width) × [-1, height) 内时四个角都在图外，结果就是 0；提前返回，
        // 也避免把无穷大或超出整数范围的坐标转成整数（输入有限但极大时会出现）。
        if (!(x >= -1.0f && x < static_cast<float>(width)) || !(y >= -1.0f && y < static_cast<float>(height))) {
            std::fill(out, out + Channels, 0.0f);
            return;
        }
    }
    const int64_t x1 = static_cast<int64_t>(std::floor(x));
    const int64_t y1 = static_cast<int64_t>(std::floor(y));
    const int64_t x2 = x1 + 1;
    const int64_t y2 = y1 + 1;

    const float dx2 = static_cast<float>(x2) - x;
    const float dx1 = x - static_cast<float>(x1);
    const float dy2 = static_cast<float>(y2) - y;
    const float dy1 = y - static_cast<float>(y1);
    const float w11 = dy2 * dx2;
    const float w12 = dy2 * dx1;
    const float w21 = dy1 * dx2;
    const float w22 = dy1 * dx1;

    // 四个角的像素起点：border 把越界的角钳回图内，zeros 让越界的角读一块全 0。
    const auto corner = [&](int64_t row, int64_t col) -> const uint8_t* {
        if constexpr (Mode == Padding::Zeros) {
            static constexpr uint8_t kOutside[Channels] = {};
            if (col < 0 || col >= width || row < 0 || row >= height) {
                return kOutside;
            }
        }
        else {
            col = std::clamp<int64_t>(col, 0, width - 1);
            row = std::clamp<int64_t>(row, 0, height - 1);
        }
        return image.ptr<uint8_t>(static_cast<int>(row)) + col * Channels;
    };
    const uint8_t* p11 = corner(y1, x1);
    const uint8_t* p12 = corner(y1, x2);
    const uint8_t* p21 = corner(y2, x1);
    const uint8_t* p22 = corner(y2, x2);

    for (int channel = 0; channel < Channels; ++channel) {
        out[channel] = w11 * static_cast<float>(p11[channel]) + w12 * static_cast<float>(p12[channel])
                       + w21 * static_cast<float>(p21[channel]) + w22 * static_cast<float>(p22[channel]);
    }
}

// torch.round（半偶）+ clamp + uint8；默认舍入模式下 nearbyint 即半偶。
uint8_t ToUint8(float value)
{
    return static_cast<uint8_t>(std::clamp(std::nearbyint(value), 0.0f, 255.0f));
}

// 单轴采样窗 [start, end)：覆盖 center ± extent 与双线性支撑，裁到资产内且非空。
std::pair<int, int> WindowAxis(float center, float extent, int size)
{
    const float lo = std::clamp(std::floor(center - extent - kWindowPad), 0.0f, static_cast<float>(size - 1));
    const float hi = std::clamp(std::floor(center + extent + kWindowPad) + 2.0f, lo + 1.0f, static_cast<float>(size));
    return { static_cast<int>(lo), static_cast<int>(hi) };
}

struct StripGrid
{
    // ROI 像素中心坐标系（u 右、v 下），行 = 半径、列 = 方位角（北 = 0，顺时针 1 度/列）。
    std::array<float, kOrientationStripHeight * kOrientationStripWidth> u {};
    std::array<float, kOrientationStripHeight * kOrientationStripWidth> v {};

    StripGrid()
    {
        for (int row = 0; row < kOrientationStripHeight; ++row) {
            const float radius = kInnerRadius + kRadiusStep * (static_cast<float>(row) + 0.5f);
            for (int col = 0; col < kOrientationStripWidth; ++col) {
                const float sin_theta = std::bit_cast<float>(kAzimuthSinBits[col]);
                const float cos_theta = std::bit_cast<float>(kAzimuthCosBits[col]);
                u[row * kOrientationStripWidth + col] = kRoiPoleU + radius * sin_theta;
                v[row * kOrientationStripWidth + col] = kRoiPoleV - radius * cos_theta;
            }
        }
    }
};

const StripGrid& GetStripGrid()
{
    static const StripGrid grid;
    return grid;
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

    const StripGrid& grid = GetStripGrid();
    out.observed.create(kOrientationStripHeight, kOrientationStripWidth, CV_8UC3);
    out.reference.create(kOrientationStripHeight, kOrientationStripWidth, CV_8UC4);

    const float extent = scale * kMaxRadius;
    const auto [w0, w1] = WindowAxis(x, extent, asset.cols);
    const auto [h0, h1] = WindowAxis(y, extent, asset.rows);
    const cv::Mat window = asset(cv::Rect(w0, h0, w1 - w0, h1 - h0));

    for (int row = 0; row < kOrientationStripHeight; ++row) {
        uint8_t* observed = out.observed.ptr<uint8_t>(row);
        uint8_t* reference = out.reference.ptr<uint8_t>(row);
        for (int col = 0; col < kOrientationStripWidth; ++col) {
            const float u = grid.u[row * kOrientationStripWidth + col];
            const float v = grid.v[row * kOrientationStripWidth + col];

            float obs[3];
            SampleBilinear<3, Padding::Border>(
                minimap,
                GridRoundTrip(u, kOrientationRoiWidth),
                GridRoundTrip(v, kOrientationRoiHeight),
                obs);

            // 参考底图坐标 = (x, y) + (q_roi - pole) * scale，再减去窗口原点。
            const float au = x + (u - kRoiPoleU) * scale - static_cast<float>(w0);
            const float av = y + (v - kRoiPoleV) * scale - static_cast<float>(h0);
            float ref[4];
            SampleBilinear<4, Padding::Zeros>(window, GridRoundTrip(au, w1 - w0), GridRoundTrip(av, h1 - h0), ref);

            // 白底合成：透明（含越界读 0）向白色过渡，alpha 保留原值。
            const float weight = ref[3] / 255.0f;
            for (int channel = 0; channel < 3; ++channel) {
                observed[col * 3 + channel] = ToUint8(obs[channel]);
                reference[col * 4 + channel] = ToUint8(ref[channel] * weight + 255.0f * (1.0f - weight));
            }
            reference[col * 4 + 3] = ToUint8(ref[3]);
        }
    }
    return true;
}

} // namespace maplocator
