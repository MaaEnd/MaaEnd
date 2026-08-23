#include "SubpixelMatcher.h"

#include <algorithm>
#include <array>

namespace iconrecognition::detail
{

std::vector<Phase> PhaseGrid()
{
    // 首轮亚像素相位覆盖 -0.75..0.75px，0.25px 步长平衡精度与 49 次二维组合的成本。
    constexpr std::array<double, 7> axis { -0.75, -0.50, -0.25, 0.0, 0.25, 0.50, 0.75 };
    std::vector<Phase> phases;
    phases.reserve(axis.size() * axis.size());
    for (double x : axis) {
        for (double y : axis) {
            phases.push_back({ x, y });
        }
    }
    return phases;
}

std::vector<Phase> BoundaryExtensionPhases(Phase winning)
{
    std::vector<Phase> phases;
    const auto edge = [](double value) -> double {
        return value == 0.75 ? 1.0 : value == -0.75 ? -1.0 : 2.0;
    };
    const double x_edge = edge(winning.x);
    const double y_edge = edge(winning.y);
    // 边界扩展沿另一轴复用首轮相位，避免把二维搜索扩大到全部 ±1px 组合。
    constexpr std::array<double, 7> axis { -0.75, -0.50, -0.25, 0.0, 0.25, 0.50, 0.75 };
    if (x_edge != 2.0) {
        for (double y : axis) {
            phases.push_back({ x_edge, y });
        }
    }
    if (y_edge != 2.0) {
        for (double x : axis) {
            phases.push_back({ x, y_edge });
        }
    }
    if (x_edge != 2.0 && y_edge != 2.0) {
        phases.push_back({ x_edge, y_edge });
    }
    std::ranges::sort(phases, [](const Phase& left, const Phase& right) {
        return left.x < right.x || (left.x == right.x && left.y < right.y);
    });
    const auto duplicate = [](const Phase& left, const Phase& right) {
        return left.x == right.x && left.y == right.y;
    };
    phases.erase(std::unique(phases.begin(), phases.end(), duplicate), phases.end());
    return phases;
}

PreparedTemplate ShiftTemplate(const PreparedTemplate& source, Phase phase)
{
    if (phase.x == 0.0 && phase.y == 0.0) {
        return source;
    }
    PreparedTemplate shifted = source;
    cv::Mat matrix = (cv::Mat_<double>(2, 3) << 1.0, 0.0, phase.x, 0.0, 1.0, phase.y);
    cv::Mat shifted_image;
    cv::warpAffine(source.image, shifted_image, matrix, source.image.size(), cv::INTER_LINEAR, cv::BORDER_CONSTANT, cv::Scalar(0, 0, 0));
    shifted.image = std::move(shifted_image);
    return shifted;
}

} // namespace iconrecognition::detail
