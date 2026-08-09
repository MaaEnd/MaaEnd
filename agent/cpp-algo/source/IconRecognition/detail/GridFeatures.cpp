#include "GridFeatures.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <limits>
#include <numeric>
#include <utility>

namespace iconrecognition::detail
{
namespace
{

constexpr double kEpsilon = 1e-8;
// 下列参数按 720p 网格样本标定，集中定义以保持结构响应口径一致。
constexpr double kNormalizationPercentile = 99.0;
constexpr double kProjectionTrimRatio = 0.10;
constexpr std::array kGradientScales { std::pair { 0.0, 0.6 }, std::pair { 0.8, 0.4 } };
constexpr double kDiagonalPenaltyWeight = 0.5;
constexpr int kMinimumSupportLength = 5;
constexpr double kSupportLengthRatio = 0.5;
constexpr double kLongEdgeWeight = 0.8;
constexpr double kLocalEdgeWeight = 0.2;
constexpr double kOccupiedCellBrightness = 8.0;

cv::Mat normalize_percentile(const cv::Mat& values, double selected_percentile = kNormalizationPercentile)
{
    std::vector<float> absolute;
    absolute.reserve(values.total());
    for (int row = 0; row < values.rows; ++row) {
        const float* source = values.ptr<float>(row);
        for (int column = 0; column < values.cols; ++column) {
            absolute.push_back(std::abs(source[column]));
        }
    }
    const double scale = Percentile(std::move(absolute), selected_percentile);
    if (scale <= kEpsilon) {
        return cv::Mat::zeros(values.size(), CV_32F);
    }
    cv::Mat normalized = values / scale;
    cv::min(normalized, 1.0, normalized);
    cv::max(normalized, -1.0, normalized);
    return normalized;
}

std::vector<float> project_trimmed(const cv::Mat& values, bool x_axis)
{
    const int output_size = x_axis ? values.cols : values.rows;
    const int sample_count = x_axis ? values.rows : values.cols;
    const int trim = static_cast<int>(sample_count * kProjectionTrimRatio);
    std::vector<float> result(output_size);
    std::vector<float> samples(sample_count);
    for (int output = 0; output < output_size; ++output) {
        for (int sample = 0; sample < sample_count; ++sample) {
            samples[sample] = x_axis ? values.at<float>(sample, output) : values.at<float>(output, sample);
        }
        std::ranges::sort(samples);
        const int begin = trim > 0 && trim * 2 < sample_count ? trim : 0;
        const int end = trim > 0 && trim * 2 < sample_count ? sample_count - trim : sample_count;
        result[output] = std::accumulate(samples.begin() + begin, samples.begin() + end, 0.0F) / (end - begin);
    }
    return result;
}

} // namespace

double Percentile(std::vector<float> values, double selected_percentile)
{
    if (values.empty()) {
        return 0.0;
    }
    std::ranges::sort(values);
    const double position = (values.size() - 1) * selected_percentile / 100.0;
    const auto left = static_cast<std::size_t>(std::floor(position));
    const auto right = static_cast<std::size_t>(std::ceil(position));
    const double fraction = position - left;
    return values[left] * (1.0 - fraction) + values[right] * fraction;
}

StructureMaps BuildStructureMaps(const cv::Mat& image, int cell_size)
{
    cv::Mat bgr;
    if (image.channels() == 4) {
        cv::cvtColor(image, bgr, cv::COLOR_BGRA2BGR);
    }
    else {
        bgr = image;
    }
    cv::Mat gray;
    cv::cvtColor(bgr, gray, cv::COLOR_BGR2GRAY);
    gray.convertTo(gray, CV_32F, 1.0 / 255.0);
    cv::Mat signed_x = cv::Mat::zeros(gray.size(), CV_32F);
    cv::Mat signed_y = cv::Mat::zeros(gray.size(), CV_32F);
    cv::Mat absolute_x = cv::Mat::zeros(gray.size(), CV_32F);
    cv::Mat absolute_y = cv::Mat::zeros(gray.size(), CV_32F);
    for (const auto& [sigma, weight] : kGradientScales) {
        cv::Mat source;
        if (sigma == 0.0) {
            source = gray;
        }
        else {
            cv::GaussianBlur(gray, source, cv::Size(), sigma);
        }
        cv::Mat dx;
        cv::Mat dy;
        cv::Sobel(source, dx, CV_32F, 1, 0, 3);
        cv::Sobel(source, dy, CV_32F, 0, 1, 3);
        signed_x += weight * dx;
        signed_y += weight * dy;
        absolute_x += weight * cv::abs(dx);
        absolute_y += weight * cv::abs(dy);
    }
    cv::Mat total = absolute_x + absolute_y + std::numeric_limits<float>::epsilon();
    cv::Mat vertical_orientation;
    cv::Mat horizontal_orientation;
    cv::divide(absolute_x, total, vertical_orientation);
    cv::divide(absolute_y, total, horizontal_orientation);
    cv::Mat minimum;
    cv::min(absolute_x, absolute_y, minimum);
    cv::Mat diagonal_penalty;
    cv::divide(2.0 * minimum, total, diagonal_penalty);
    cv::Mat vertical_local = absolute_x.mul(vertical_orientation).mul(1.0 - kDiagonalPenaltyWeight * diagonal_penalty);
    cv::Mat horizontal_local = absolute_y.mul(horizontal_orientation).mul(1.0 - kDiagonalPenaltyWeight * diagonal_penalty);
    const int support_length = std::max(kMinimumSupportLength, cvRound(cell_size * kSupportLengthRatio));
    const cv::Mat vertical_kernel = cv::getStructuringElement(cv::MORPH_RECT, cv::Size(1, support_length));
    const cv::Mat horizontal_kernel = cv::getStructuringElement(cv::MORPH_RECT, cv::Size(support_length, 1));
    cv::Mat vertical_long;
    cv::Mat horizontal_long;
    cv::morphologyEx(vertical_local, vertical_long, cv::MORPH_OPEN, vertical_kernel);
    cv::morphologyEx(horizontal_local, horizontal_long, cv::MORPH_OPEN, horizontal_kernel);
    cv::Mat vertical = normalize_percentile(kLongEdgeWeight * vertical_long + kLocalEdgeWeight * vertical_local);
    cv::Mat horizontal = normalize_percentile(kLongEdgeWeight * horizontal_long + kLocalEdgeWeight * horizontal_local);
    cv::max(vertical, 0.0, vertical);
    cv::max(horizontal, 0.0, horizontal);
    cv::min(diagonal_penalty, 1.0, diagonal_penalty);
    cv::max(diagonal_penalty, 0.0, diagonal_penalty);
    return {
        std::move(vertical),         std::move(horizontal), normalize_percentile(signed_x), normalize_percentile(signed_y),
        std::move(diagonal_penalty),
    };
}

std::vector<float> RobustProjection(const cv::Mat& values, bool x_axis)
{
    auto result = project_trimmed(values, x_axis);
    const float minimum = std::min(0.0F, *std::ranges::min_element(result));
    for (float& value : result) {
        value -= minimum;
    }
    const double scale = Percentile(result, kNormalizationPercentile);
    if (scale <= kEpsilon) {
        return std::vector<float>(result.size(), 0.0F);
    }
    for (float& value : result) {
        value = std::clamp(static_cast<float>(value / scale), 0.0F, 1.0F);
    }
    return result;
}

std::vector<float> AggregateSigned(const cv::Mat& values, bool x_axis)
{
    auto projected = project_trimmed(values, x_axis);
    float scale = 0.0F;
    for (float value : projected) {
        scale = std::max(scale, std::abs(value));
    }
    if (scale <= kEpsilon) {
        return std::vector<float>(projected.size(), 0.0F);
    }
    for (float& value : projected) {
        value /= scale;
    }
    return projected;
}

std::vector<float> MedianProjection(const cv::Mat& values, bool x_axis)
{
    const int output_size = x_axis ? values.cols : values.rows;
    const int sample_count = x_axis ? values.rows : values.cols;
    std::vector<float> result(output_size);
    std::vector<float> samples(sample_count);
    for (int output = 0; output < output_size; ++output) {
        for (int sample = 0; sample < sample_count; ++sample) {
            samples[sample] = x_axis ? values.at<float>(sample, output) : values.at<float>(output, sample);
        }
        std::ranges::sort(samples);
        result[output] =
            sample_count % 2 == 0 ? 0.5F * (samples[sample_count / 2 - 1] + samples[sample_count / 2]) : samples[sample_count / 2];
    }
    return result;
}

double GridOccupancyScore(const cv::Mat& image, const GridLayout& layout)
{
    if (image.empty() || layout.cells.empty()) {
        return 0.0;
    }
    int active = 0;
    for (const auto& cell : layout.cells) {
        const cv::Rect clipped = cell.cell_box & cv::Rect(0, 0, image.cols, image.rows);
        if (clipped.width <= 0 || clipped.height <= 0) {
            continue;
        }
        cv::Mat gray;
        if (image.channels() == 4) {
            cv::cvtColor(image(clipped), gray, cv::COLOR_BGRA2GRAY);
        }
        else if (image.channels() == 3) {
            cv::cvtColor(image(clipped), gray, cv::COLOR_BGR2GRAY);
        }
        else {
            gray = image(clipped);
        }
        if (cv::mean(gray)[0] > kOccupiedCellBrightness) {
            ++active;
        }
    }
    return static_cast<double>(active) / layout.cells.size();
}

} // namespace iconrecognition::detail
