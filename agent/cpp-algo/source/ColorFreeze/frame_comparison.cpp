#include "ColorFreeze.h"

#include <algorithm>

namespace colorfreeze
{
namespace
{

constexpr int kChannelBits = 8;
constexpr int kChannelMax = 255;
// 位置见证剩余不超过 1/16（6.25%）时才尝试小规模补配；720p 基准下可避免大位置交换重复建图。
// 增大分母会减少补配机会，减小会增加额外搜索成本；只影响搜索顺序，不影响最终判定。
constexpr std::size_t kResidualFractionDenominator = 16;

SimilarityBounds aggregate(const std::vector<SimilarityBounds>& ranges)
{
    SimilarityBounds result { 1, 1 };
    for (const auto& range : ranges) {
        // 所有范围都要达标，因此整体相似度的上下界分别取各范围的最小值。
        result.lower = std::min(result.lower, range.lower);
        result.upper = std::min(result.upper, range.upper);
    }
    return result;
}

const ChannelHistograms& channel_histograms(FrameSample& sample, std::size_t range_index)
{
    auto& range = sample.ranges[range_index];
    if (range.channels) {
        return *range.channels;
    }
    auto& histograms = range.channels.emplace(ChannelHistograms {});
    const auto channels = sample.colors.channels();
    for (int y = 0; y < sample.colors.rows; ++y) {
        const auto* pixels = sample.colors.ptr<std::uint8_t>(y);
        const auto* selected = range.mask.ptr<std::uint8_t>(y);
        for (int x = 0; x < sample.colors.cols; ++x) {
            if (selected[x] == 0) {
                continue;
            }
            for (int c = 0; c < channels; ++c) {
                ++histograms[c][pixels[x * channels + c]];
            }
        }
    }
    return histograms;
}

// 任一合法联合配对都必须落在对方实际颜色包围盒的容差扩张内。
// 单通道投影可能分别有足够计数，但各通道越界像素的并集仍会超限；此处计数并集给出更紧的上界。
std::size_t bounded_count(
    const FrameSample& sample,
    std::size_t range_index,
    const ChannelHistograms& own,
    const ChannelHistograms& other,
    const ColorTolerance& tolerance)
{
    cv::Scalar lower, upper(kChannelMax, kChannelMax, kChannelMax, kChannelMax);
    bool covered = true;
    for (int c = 0; c < sample.colors.channels(); ++c) {
        if (c == 0 && tolerance.hue_period != 0) {
            // 环形色相不能套用单个线性包围盒，忽略 H 约束仍是安全上界。
            continue;
        }
        const auto first = [](const auto& histogram) {
            return static_cast<int>(
                std::find_if(histogram.begin(), histogram.end(), [](auto count) { return count != 0; }) - histogram.begin());
        };
        const auto last = [](const auto& histogram) {
            return kChannelMax
                   - static_cast<int>(
                       std::find_if(histogram.rbegin(), histogram.rend(), [](auto count) { return count != 0; }) - histogram.rbegin());
        };
        lower[c] = std::max(0, first(other[c]) - tolerance.linear);
        upper[c] = std::min(kChannelMax, last(other[c]) + tolerance.linear);
        covered = covered && first(own[c]) >= lower[c] && last(own[c]) <= upper[c];
    }
    if (covered) {
        // 对方包围盒已覆盖本组全部实际颜色时，无需再次遍历像素。
        return sample.ranges[range_index].count;
    }
    cv::Mat possible;
    cv::inRange(sample.colors, lower, upper, possible);
    cv::bitwise_and(possible, sample.ranges[range_index].mask, possible);
    return static_cast<std::size_t>(cv::countNonZero(possible));
}

// 一维距离具有顺序性：从最小颜色开始，优先配对最小的兼容颜色，可得到最大匹配计数。
// 若较小值与另一侧最小剩余值已经相距过大，它也不可能与后面更大的值配对，可直接跳过。
std::size_t linear_overlap(const std::array<std::size_t, 256>& baseline, const std::array<std::size_t, 256>& current, int radius)
{
    std::size_t a = 0, b = 0, left = baseline[0], right = current[0], matched = 0;
    while (a < baseline.size() && b < current.size()) {
        if (left == 0 || static_cast<int>(a) + radius < static_cast<int>(b)) {
            ++a;
            if (a < baseline.size()) {
                left = baseline[a];
            }
        }
        else if (right == 0 || static_cast<int>(b) + radius < static_cast<int>(a)) {
            ++b;
            if (b < current.size()) {
                right = current[b];
            }
        }
        else {
            const auto count = std::min(left, right);
            matched += count;
            left -= count;
            right -= count;
        }
    }
    return matched;
}

// 每个相同位置至多贡献一对计数，双方都在范围内且每个通道满足容差时才可计入。
// 这份合法配对给出联合匹配的下界；达不到阈值不会直接判失败，仍允许颜色跨位置重新分配。
cv::Mat compatible_positions(const FrameSample& baseline, const FrameSample& current, const ColorTolerance& tolerance)
{
    cv::Mat difference, compatible;
    cv::absdiff(baseline.colors, current.colors, difference);
    cv::Scalar maximum(tolerance.linear, tolerance.linear, tolerance.linear, tolerance.linear);
    if (tolerance.hue_period != 0) {
        maximum[0] = tolerance.hue;
    }
    cv::inRange(difference, cv::Scalar(), maximum, compatible);
    if (tolerance.hue_period != 0 && tolerance.hue != 0) {
        // absdiff 给出线性差值，色相跨零时还需接受 period-difference <= hue 的另一段。
        cv::Mat wrapped;
        maximum[0] = kChannelMax;
        cv::inRange(difference, cv::Scalar(tolerance.hue_period - tolerance.hue, 0, 0, 0), maximum, wrapped);
        cv::bitwise_or(compatible, wrapped, compatible);
    }
    return compatible;
}

} // namespace

const Distribution& BuildDistribution(FrameSample& sample, std::size_t range_index)
{
    auto& range = sample.ranges[range_index];
    if (range.distribution) {
        return *range.distribution;
    }
    auto& distribution = range.distribution.emplace();
    distribution.count = range.count;
    std::vector<std::uint32_t> keys;
    keys.reserve(range.count);
    const auto channels = sample.colors.channels();
    for (int y = 0; y < sample.colors.rows; ++y) {
        const auto* pixels = sample.colors.ptr<std::uint8_t>(y);
        const auto* selected = range.mask.ptr<std::uint8_t>(y);
        for (int x = 0; x < sample.colors.cols; ++x) {
            if (selected[x] == 0) {
                continue;
            }
            std::uint32_t color = 0;
            for (int c = 0; c < channels; ++c) {
                color = (color << kChannelBits) | pixels[x * channels + c];
            }
            keys.push_back(color);
        }
    }
    // 对紧凑颜色键做线性基数排序，再合并连续相同值；避免全屏丰富颜色的哈希扩容与随机写入。
    // 排序使用完整键，不量化颜色；结果可直接供精确重合与后续邻近颜色匹配复用。
    std::vector<std::uint32_t> scratch(keys.size());
    for (int shift = 0; shift < channels * kChannelBits; shift += kChannelBits) {
        std::array<std::size_t, 256> offsets {};
        for (const auto key : keys) {
            ++offsets[(key >> shift) & kChannelMax];
        }
        std::size_t prefix = 0;
        for (auto& offset : offsets) {
            const auto count = offset;
            offset = prefix;
            prefix += count;
        }
        for (const auto key : keys) {
            scratch[offsets[(key >> shift) & kChannelMax]++] = key;
        }
        keys.swap(scratch);
    }
    auto& bins = distribution.colors;
    std::size_t distinct = 0;
    for (std::size_t i = 0; i < keys.size(); ++i) {
        distinct += i == 0 || keys[i] != keys[i - 1];
    }
    bins.reserve(distinct);
    for (std::size_t i = 0; i < keys.size();) {
        auto end = i + 1;
        while (end < keys.size() && keys[end] == keys[i]) {
            ++end;
        }
        std::array<std::uint8_t, 4> color {};
        auto remaining = keys[i];
        for (auto channel = channels; channel > 0; --channel) {
            color[channel - 1] = static_cast<std::uint8_t>(remaining & kChannelMax);
            remaining >>= kChannelBits;
        }
        bins.push_back({ keys[i], color, end - i });
        i = end;
    }
    return distribution;
}

SimilarityBounds CompareSamples(FrameSample& baseline, FrameSample& current, const Parameters& parameters)
{
    if (baseline.colors.size() != current.colors.size() || baseline.colors.type() != current.colors.type()
        || baseline.ranges.size() != current.ranges.size() || baseline.ranges.size() != parameters.ranges.size()) {
        return { 0, 0 };
    }
    std::vector<SimilarityBounds> bounds;
    bool complete = true;
    for (std::size_t i = 0; i < baseline.ranges.size(); ++i) {
        const auto a = baseline.ranges[i].count, b = current.ranges[i].count;
        const auto upper = CountSimilarity(std::min(a, b), std::max(a, b));
        // 幅度阈值为零时任何范围内颜色都兼容，只有数量差；双方为空时也精确等于 1。
        const auto lower = parameters.color_threshold == 0 || (a == 0 && b == 0) ? upper : 0;
        bounds.push_back({ lower, upper });
        complete = complete && lower >= parameters.distribution_threshold;
    }
    if (complete || aggregate(bounds).upper < parameters.distribution_threshold) {
        return aggregate(bounds);
    }

    const auto tolerance = GetColorTolerance(parameters);
    const auto compatible = compatible_positions(baseline, current, tolerance);
    std::vector<cv::Mat> paired_masks;
    std::vector<std::size_t> paired_counts;
    for (std::size_t i = 0; i < bounds.size(); ++i) {
        cv::Mat paired;
        cv::bitwise_and(baseline.ranges[i].mask, current.ranges[i].mask, paired);
        cv::bitwise_and(paired, compatible, paired);
        const auto matched = static_cast<std::size_t>(cv::countNonZero(paired));
        bounds[i].lower = CountSimilarity(matched, std::max(baseline.ranges[i].count, current.ranges[i].count));
        paired_masks.push_back(std::move(paired));
        paired_counts.push_back(matched);
    }
    if (aggregate(bounds).lower >= parameters.distribution_threshold) {
        return aggregate(bounds);
    }

    for (std::size_t i = 0; i < bounds.size(); ++i) {
        if (bounds[i].lower >= parameters.distribution_threshold) {
            continue;
        }
        const auto total = std::max(baseline.ranges[i].count, current.ranges[i].count);
        const auto& a = channel_histograms(baseline, i);
        const auto& b = channel_histograms(current, i);
        for (int c = 0; c < current.colors.channels(); ++c) {
            if (c == 0 && tolerance.hue_period != 0) {
                // 线性一维贪心不能用于环形色相；跳过此投影仍是安全上界，S/V/L 继续参与排除。
                continue;
            }
            const auto upper = CountSimilarity(linear_overlap(a[c], b[c], tolerance.linear), total);
            bounds[i].upper = std::min(bounds[i].upper, upper);
        }
        if (current.colors.channels() == 1 && tolerance.hue_period == 0) {
            // 灰度的一维投影就是完整联合分布，因此这里已得到精确结果，无需哈希或匹配图。
            bounds[i].lower = bounds[i].upper;
        }
        else if (bounds[i].upper >= parameters.distribution_threshold) {
            const auto possible = std::min(bounded_count(baseline, i, a, b, tolerance), bounded_count(current, i, b, a, tolerance));
            bounds[i].upper = std::min(bounds[i].upper, CountSimilarity(possible, total));
        }
        if (bounds[i].upper < parameters.distribution_threshold) {
            return aggregate(bounds);
        }
        if (bounds[i].lower >= parameters.distribution_threshold) {
            continue;
        }
        // 仅在位置见证已覆盖绝大多数计数时补配残余；大规模位置交换直接比较联合分布更便宜。
        if (total - paired_counts[i] <= total / kResidualFractionDenominator && current.colors.channels() > 1) {
            // 位置见证已消费的计数不能再次使用。只统计双方尚未配对的像素，生成额外的合法流。
            // 残余样本在本次比较中短暂共享自有颜色图；不修改原始样本、固定基准或它们的缓存。
            FrameSample residual_a { baseline.colors, {} }, residual_b { current.colors, {} };
            for (auto [original, residual] : { std::pair { &baseline, &residual_a }, std::pair { &current, &residual_b } }) {
                RangeSample range;
                cv::subtract(original->ranges[i].mask, paired_masks[i], range.mask);
                range.count = original->ranges[i].count - paired_counts[i];
                residual->ranges.push_back(std::move(range));
            }
            const auto extra = FindCompatiblePairs(
                BuildDistribution(residual_a, 0),
                BuildDistribution(residual_b, 0),
                parameters,
                static_cast<std::size_t>(current.colors.channels()),
                total);
            bounds[i].lower = CountSimilarity(paired_counts[i] + extra, total);
            if (bounds[i].lower >= parameters.distribution_threshold) {
                continue;
            }
        }
        const auto joint = CompareDistributions(
            BuildDistribution(baseline, i),
            BuildDistribution(current, i),
            parameters,
            static_cast<std::size_t>(current.colors.channels()));
        // 不同证明可能使用同一份计数，不能把下界相加；取最大下界、最小上界即可安全收紧区间。
        bounds[i].lower = std::max(bounds[i].lower, joint.lower);
        bounds[i].upper = std::min(bounds[i].upper, joint.upper);
        if (bounds[i].upper < parameters.distribution_threshold) {
            return aggregate(bounds);
        }
    }
    return aggregate(bounds);
}

} // namespace colorfreeze
