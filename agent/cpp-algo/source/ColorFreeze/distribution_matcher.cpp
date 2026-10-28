#include "ColorFreeze.h"

#include <algorithm>
#include <array>
#include <cstdlib>
#include <limits>
#include <numeric>

#include <boost/container/small_vector.hpp>

namespace colorfreeze
{
namespace
{

// 联合颜色键最多包含四个 8 位通道；索引只用于查询邻近颜色，不对颜色分箱。
constexpr std::size_t kMaxChannels = 4;
constexpr int kChannelMax = 255;
constexpr std::size_t kNone = std::numeric_limits<std::size_t>::max();
using Color = std::array<int, kMaxChannels>;

int hue_period(int method)
{
    switch (method) {
    case cv::COLOR_BGR2HSV:
    case cv::COLOR_RGB2HSV:
    case cv::COLOR_BGR2HLS:
    case cv::COLOR_RGB2HLS:
        return 180;
    case cv::COLOR_BGR2HSV_FULL:
    case cv::COLOR_RGB2HSV_FULL:
    case cv::COLOR_BGR2HLS_FULL:
    case cv::COLOR_RGB2HLS_FULL:
        return 256;
    default:
        return 0;
    }
}

// 直接用相似度公式确定整数边界，避免 floor((1-threshold)*scale) 在临界值向下舍入。
// 普通通道的最大距离为 255，环形色相的最大距离为半圈。
int allowed_difference(double threshold, int scale)
{
    int difference = 0;
    while (difference < scale && 1 - static_cast<double>(difference + 1) / scale >= threshold) {
        ++difference;
    }
    return difference;
}

// 色相的邻域可能跨过 0；拆为最多两个盒，普通通道使用线性上下界。
// 返回 true 时停止查询；调用方可用于寻找空余容量，或遍历增广所需的全部邻居。
template <typename Index, typename Visitor>
bool visit_neighbors(
    Index& index,
    const std::array<std::uint8_t, 4>& color,
    std::size_t channels,
    int period,
    int radius,
    int hue_radius,
    Visitor&& visitor)
{
    Color lower {}, upper {};
    for (std::size_t c = 0; c < channels; ++c) {
        lower[c] = std::max(0, color[c] - radius);
        upper[c] = std::min(kChannelMax, color[c] + radius);
    }
    if (period == 0) {
        return index.visit(lower, upper, visitor);
    }
    const auto hue_low = color[0] - hue_radius;
    const auto hue_high = color[0] + hue_radius;
    lower[0] = std::max(0, hue_low);
    upper[0] = std::min(period - 1, hue_high);
    if (index.visit(lower, upper, visitor)) {
        return true;
    }
    if (hue_low < 0 || hue_high >= period) {
        lower[0] = hue_low < 0 ? period + hue_low : 0;
        upper[0] = hue_low < 0 ? period - 1 : hue_high - period;
        return index.visit(lower, upper, visitor);
    }
    return false;
}

// 右侧不同颜色构成的 KD 树。每个节点仍代表一个完整颜色，不改变统计精度。
// 增广搜索只枚举容差盒中的颜色，避免提前构造可能接近 N*M 条的兼容边。
class ColorIndex
{
public:
    ColorIndex(const std::vector<ColorBin>& points, std::size_t channels)
        : points_(points)
        , channels_(channels)
        , nodes_(points.size())
    {
        std::vector<std::size_t> order(points.size());
        std::iota(order.begin(), order.end(), 0);
        root_ = build(order, 0, order.size(), 0, kNone);
    }

    // 搜索中已访问的右顶点暂时移除，后续左顶点不会重复枚举它及其反向流。
    // 只维护子树有效数量，包围盒无需重建；恢复时沿同一路径加回即可。
    void setActive(std::size_t point, bool active)
    {
        for (auto node = point; node != kNone; node = nodes_[node].parent) {
            if (active) {
                ++nodes_[node].active_count;
            }
            else {
                --nodes_[node].active_count;
            }
        }
        nodes_[point].active = active;
    }

    // visitor 返回 true 时立即停止，表示已经找到有剩余容量的增广终点。
    template <typename Visitor>
    bool visit(const Color& lower, const Color& upper, Visitor&& visitor)
    {
        return visitNode(root_, lower, upper, visitor);
    }

private:
    struct Node
    {
        std::size_t left = kNone;
        std::size_t right = kNone;
        std::size_t parent = kNone;
        Color lower {};
        Color upper {};
        std::size_t active_count = 1;
        bool active = true;
    };

    std::size_t build(std::vector<std::size_t>& order, std::size_t begin, std::size_t end, std::size_t depth, std::size_t parent)
    {
        if (begin == end) {
            return kNone;
        }
        const auto middle = begin + (end - begin) / 2;
        const auto channel = depth % channels_;
        std::nth_element(order.begin() + begin, order.begin() + middle, order.begin() + end, [&](auto a, auto b) {
            return points_[a].color[channel] < points_[b].color[channel];
        });
        const auto point = order[middle];
        auto& node = nodes_[point];
        node.parent = parent;
        node.left = build(order, begin, middle, depth + 1, point);
        node.right = build(order, middle + 1, end, depth + 1, point);
        for (std::size_t c = 0; c < channels_; ++c) {
            node.lower[c] = node.upper[c] = points_[point].color[c];
        }
        for (const auto child : { node.left, node.right }) {
            if (child == kNone) {
                continue;
            }
            node.active_count += nodes_[child].active_count;
            for (std::size_t c = 0; c < channels_; ++c) {
                node.lower[c] = std::min(node.lower[c], nodes_[child].lower[c]);
                node.upper[c] = std::max(node.upper[c], nodes_[child].upper[c]);
            }
        }
        return point;
    }

    template <typename Visitor>
    bool visitNode(std::size_t point, const Color& lower, const Color& upper, Visitor& visitor)
    {
        if (point == kNone || nodes_[point].active_count == 0) {
            return false;
        }
        const auto& node = nodes_[point];
        bool inside = node.active;
        for (std::size_t c = 0; c < channels_; ++c) {
            if (node.upper[c] < lower[c] || node.lower[c] > upper[c]) {
                return false;
            }
            inside = inside && points_[point].color[c] >= lower[c] && points_[point].color[c] <= upper[c];
        }
        if (inside && visitor(point)) {
            return true;
        }
        return visitNode(node.left, lower, upper, visitor) || visitNode(node.right, lower, upper, visitor);
    }

    const std::vector<ColorBin>& points_;
    std::size_t channels_;
    std::vector<Node> nodes_;
    std::size_t root_ = kNone;
};

struct Allocation
{
    std::size_t left;
    std::size_t count;
};

bool compatible_colors(
    const std::array<std::uint8_t, 4>& left,
    const std::array<std::uint8_t, 4>& right,
    std::size_t channels,
    int period,
    int radius,
    int hue_radius)
{
    for (std::size_t c = 0; c < channels; ++c) {
        auto delta = std::abs(left[c] - right[c]);
        if (c == 0 && period != 0) {
            delta = std::min(delta, period - delta);
        }
        if (delta > (c == 0 && period != 0 ? hue_radius : radius)) {
            return false;
        }
    }
    return true;
}

// 保留其余通道的相对顺序，只把指定通道提为主键；单字节稳定计数排序不需要比较器或颜色分箱。
std::vector<std::size_t> channel_order(const std::vector<ColorBin>& bins, std::size_t channel)
{
    std::array<std::size_t, 256> offsets {};
    for (const auto& bin : bins) {
        ++offsets[bin.color[channel]];
    }
    std::size_t prefix = 0;
    for (auto& offset : offsets) {
        const auto count = offset;
        offset = prefix;
        prefix += count;
    }
    std::vector<std::size_t> result(bins.size());
    for (std::size_t i = 0; i < bins.size(); ++i) {
        result[offsets[bins[i].color[channel]]++] = i;
    }
    return result;
}

// 有容量的二部图最大匹配：左/右颜色的计数是容量，兼容颜色之间才允许流动。
// 精确重合只是初始流，后续可沿反向边重新分配，不能锁死，否则简单贪心会漏掉合法配对。
struct Overlap
{
    std::size_t matched;
    bool exact;
};

Overlap approximate_overlap(
    const Distribution& baseline,
    const Distribution& current,
    std::size_t channels,
    int period,
    int radius,
    int hue_radius,
    std::size_t required,
    bool augment = true)
{
    const auto& left = baseline.colors;
    const auto& right = current.colors;
    std::vector<std::size_t> left_order(left.size()), right_order(right.size());
    std::iota(left_order.begin(), left_order.end(), 0);
    std::iota(right_order.begin(), right_order.end(), 0);
    const auto rank_count = [&](const auto& a, const auto& b) {
        std::size_t result = 0;
        for (std::size_t l = 0, r = 0, used_left = 0, used_right = 0; l < a.size() && r < b.size();) {
            const auto count = std::min(left[a[l]].count - used_left, right[b[r]].count - used_right);
            if (compatible_colors(left[a[l]].color, right[b[r]].color, channels, period, radius, hue_radius)) {
                result += count;
            }
            used_left += count;
            used_right += count;
            if (used_left == left[a[l]].count) {
                ++l;
                used_left = 0;
            }
            if (used_right == right[b[r]].count) {
                ++r;
                used_right = 0;
            }
        }
        return result;
    };
    auto best = rank_count(left_order, right_order);
    // 固定完整颜色排序不一定适合所有动画：例如 HSV 亮度变化按 V 排序可能得到更好的合法流。
    // 各候选独立消费计数，只选最好的一份，不能将不同排序的匹配数相加。
    for (std::size_t channel = 1; best < required && channel < channels; ++channel) {
        auto a = channel_order(left, channel), b = channel_order(right, channel);
        const auto count = rank_count(a, b);
        if (count > best) {
            best = count;
            left_order = std::move(a);
            right_order = std::move(b);
        }
    }
    if (best >= required) {
        return { best, best == std::min(baseline.count, current.count) };
    }
    // 排序候选已达到阈值时无需创建容量图；只有确需直接补配或增广时才分配这些状态。
    std::vector<std::size_t> left_remaining, right_remaining;
    // 排序候选通常只给每个右颜色分配一条来流；内联首条边，避免为全屏数十万颜色逐个分配小堆块。
    // 多条边仍自动扩容，完整保留增广重分配所需的全部来流，不限制图的度数。
    std::vector<boost::container::small_vector<Allocation, 1>> allocations(right.size());
    left_remaining.reserve(left.size());
    right_remaining.reserve(right.size());
    for (const auto& point : left) {
        left_remaining.push_back(point.count);
    }
    for (const auto& point : right) {
        right_remaining.push_back(point.count);
    }
    std::size_t matched = 0;
    // 整体轻微偏色时，两侧排序后的颜色往往可以直接按累计计数配对。
    // 先构造这份合法候选流，避免精确重合抢占计数后引发很长的重新分配链。
    // 不兼容的片段仅跳过，不计入 matched；这不是以排序取代最大匹配。
    for (std::size_t a = 0, b = 0, used_left = 0, used_right = 0; a < left.size() && b < right.size();) {
        const auto l = left_order[a], r = right_order[b];
        const auto count = std::min(left[l].count - used_left, right[r].count - used_right);
        if (compatible_colors(left[l].color, right[r].color, channels, period, radius, hue_radius)) {
            allocations[r].push_back({ l, count });
            left_remaining[l] -= count;
            right_remaining[r] -= count;
            matched += count;
        }
        used_left += count;
        used_right += count;
        if (used_left == left[l].count) {
            ++a;
            used_left = 0;
        }
        if (used_right == right[r].count) {
            ++b;
            used_right = 0;
        }
    }
    if (matched >= required) {
        return { matched, matched == std::min(baseline.count, current.count) };
    }
    // 剩余计数先填精确颜色边；上面的候选流和这些精确流都允许在增广阶段被重新分配。
    for (std::size_t l = 0, r = 0; l < left.size() && r < right.size();) {
        if (left[l].key == right[r].key) {
            const auto count = std::min(left_remaining[l], right_remaining[r]);
            if (count != 0) {
                auto edge = std::ranges::find(allocations[r], l, &Allocation::left);
                if (edge == allocations[r].end()) {
                    allocations[r].push_back({ l, count });
                }
                else {
                    edge->count += count;
                }
            }
            left_remaining[l] -= count;
            right_remaining[r] -= count;
            matched += count;
            ++l;
            ++r;
        }
        else if (left[l].key < right[r].key) {
            ++l;
        }
        else {
            ++r;
        }
    }
    if (matched >= required) {
        return { matched, matched == std::min(baseline.count, current.count) };
    }
    // 单独索引仍有空余容量的右颜色，优先直接配对；无直接去向时仍遍历反向边求最大匹配。
    // 大部分计数常已由候选流消费；空余索引只存仍有容量的颜色，避免为整张图建树再逐一停用。
    // 保留双向下标映射，后续容量变化仍写回完整图；需要反向重分配时才另外构造完整索引。
    std::vector<ColorBin> available_points;
    std::vector<std::size_t> available_ids, available_positions(right.size(), kNone);
    for (std::size_t r = 0; r < right.size(); ++r) {
        if (right_remaining[r] != 0) {
            available_positions[r] = available_points.size();
            available_points.push_back(right[r]);
            available_ids.push_back(r);
        }
    }
    ColorIndex available(available_points, channels);

    // 先让所有剩余左颜色尝试直接使用空余计数，再为未满足的颜色搜索反向边。
    // 若逐个颜色立即增广，较早的困难颜色可能遍历很长的重分配链，而后面的颜色本可直接达到阈值。
    // 这些流仍记录在 allocations 中，不能把贪心结果当作最大匹配；不足时继续完整增广。
    for (std::size_t l = 0; l < left.size(); ++l) {
        while (left_remaining[l] != 0) {
            const auto found = visit_neighbors(available, left[l].color, channels, period, radius, hue_radius, [&](std::size_t point) {
                const auto r = available_ids[point];
                const auto count = std::min(left_remaining[l], right_remaining[r]);
                allocations[r].push_back({ l, count });
                left_remaining[l] -= count;
                right_remaining[r] -= count;
                matched += count;
                if (right_remaining[r] == 0) {
                    available.setActive(point, false);
                }
                return true;
            });
            if (matched >= required) {
                return { matched, matched == std::min(baseline.count, current.count) };
            }
            if (!found) {
                break;
            }
        }
    }

    if (!augment) {
        return { matched, false };
    }
    // 直接配对足以通过时无需构造第二棵树；只有增广阶段才需要搜索已满颜色及其反向流。
    ColorIndex index(right, channels);
    std::vector<std::size_t> parent_left(left.size(), kNone), parent_edge(left.size(), kNone), parent_right(right.size(), kNone);
    std::vector<std::size_t> visited(left.size(), 0), queue, touched;
    std::vector<bool> blocked(left.size(), false);
    std::size_t generation = 0;
    for (std::size_t root = 0; root < left.size(); ++root) {
        while (left_remaining[root] != 0 && !blocked[root]) {
            ++generation;
            queue.assign(1, root);
            touched.clear();
            visited[root] = generation;
            std::size_t terminal = kNone;
            for (std::size_t head = 0; head < queue.size() && terminal == kNone; ++head) {
                const auto l = queue[head];
                if (visit_neighbors(available, left[l].color, channels, period, radius, hue_radius, [&](std::size_t point) {
                        const auto r = available_ids[point];
                        parent_right[r] = l;
                        terminal = r;
                        return true;
                    })) {
                    break;
                }
                const auto visit = [&](std::size_t r) {
                    index.setActive(r, false);
                    touched.push_back(r);
                    parent_right[r] = l;
                    if (right_remaining[r] != 0) {
                        terminal = r;
                        return true;
                    }
                    // 已满的右颜色可把原有计数退回其他左颜色，再为它寻找新的去向。
                    for (std::size_t edge = 0; edge < allocations[r].size(); ++edge) {
                        const auto& allocation = allocations[r][edge];
                        if (allocation.count != 0 && visited[allocation.left] != generation) {
                            visited[allocation.left] = generation;
                            parent_left[allocation.left] = r;
                            parent_edge[allocation.left] = edge;
                            queue.push_back(allocation.left);
                        }
                    }
                    return false;
                };
                visit_neighbors(index, left[l].color, channels, period, radius, hue_radius, visit);
            }
            if (terminal == kNone) {
                // 搜索失败时，可达右集合已满，且其全部正向来流都来自可达左集合。
                // 可达左集合也没有通向外部的兼容边，因此后续增广不可能经过它抵达空余容量。
                // 永久移除这个封闭集合，避免大面积无匹配时对同一批颜色反复搜索。
                for (const auto l : queue) {
                    blocked[l] = true;
                }
                break;
            }
            auto count = std::min(left_remaining[root], right_remaining[terminal]);
            for (auto r = terminal; parent_right[r] != root;) {
                const auto l = parent_right[r];
                const auto previous = parent_left[l];
                count = std::min(count, allocations[previous][parent_edge[l]].count);
                r = previous;
            }
            left_remaining[root] -= count;
            right_remaining[terminal] -= count;
            if (right_remaining[terminal] == 0) {
                available.setActive(available_positions[terminal], false);
            }
            matched += count;
            if (matched >= required) {
                return { matched, matched == std::min(baseline.count, current.count) };
            }
            for (auto r = terminal;;) {
                const auto l = parent_right[r];
                // 下标记录反向边，向量只追加、不 erase，避免后续增广路径持有失效的边下标。
                auto& edges = allocations[r];
                auto edge = std::ranges::find(edges, l, &Allocation::left);
                if (edge == edges.end()) {
                    edges.push_back({ l, count });
                }
                else {
                    edge->count += count;
                }
                if (l == root) {
                    break;
                }
                r = parent_left[l];
                allocations[r][parent_edge[l]].count -= count;
            }
            // 成功路径可能改变计数归属，下次搜索必须重新允许访问本次碰过的全部右颜色。
            for (const auto r : touched) {
                index.setActive(r, true);
            }
        }
    }
    return { matched, true };
}

// 找到第一个在原有浮点求值顺序下达到阈值的整数计数，避免 ceil(threshold*total) 的边界偏差。
std::size_t required_matches(std::size_t total, double threshold)
{
    std::size_t lower = 0, upper = total;
    while (lower < upper) {
        const auto middle = lower + (upper - lower) / 2;
        if (CountSimilarity(middle, total) >= threshold) {
            upper = middle;
        }
        else {
            lower = middle + 1;
        }
    }
    return lower;
}

} // namespace

ColorTolerance GetColorTolerance(const Parameters& parameters)
{
    const auto period = hue_period(parameters.method);
    return { allowed_difference(parameters.color_threshold, kChannelMax),
             period == 0 ? 0 : allowed_difference(parameters.color_threshold, period / 2),
             period };
}

std::size_t FindCompatiblePairs(
    const Distribution& baseline,
    const Distribution& current,
    const Parameters& parameters,
    std::size_t channels,
    std::size_t required)
{
    if (baseline.count == 0 || current.count == 0) {
        return 0;
    }
    const auto tolerance = GetColorTolerance(parameters);
    return approximate_overlap(baseline, current, channels, tolerance.hue_period, tolerance.linear, tolerance.hue, required, false).matched;
}

SimilarityBounds
    CompareDistributions(const Distribution& baseline, const Distribution& current, const Parameters& parameters, std::size_t channels)
{
    const auto total = std::max(baseline.count, current.count);
    const auto possible = std::min(baseline.count, current.count);
    const auto upper = CountSimilarity(possible, total);
    if (total == 0 || parameters.color_threshold == 0) {
        return { upper, upper };
    }
    if (upper < parameters.distribution_threshold) {
        return { 0, upper };
    }
    std::size_t matched = 0;
    const auto& left = baseline.colors;
    const auto& right = current.colors;
    for (std::size_t l = 0, r = 0; l < left.size() && r < right.size();) {
        if (left[l].key == right[r].key) {
            matched += std::min(left[l].count, right[r].count);
            ++l;
            ++r;
        }
        else if (left[l].key < right[r].key) {
            ++l;
        }
        else {
            ++r;
        }
    }
    const auto tolerance = GetColorTolerance(parameters);
    if (matched == possible || (tolerance.linear == 0 && tolerance.hue == 0)) {
        const auto exact = CountSimilarity(matched, total);
        return { exact, exact };
    }
    const auto required = required_matches(total, parameters.distribution_threshold);
    if (matched >= required) {
        return { CountSimilarity(matched, total), upper };
    }
    // 增广到达阈值就已足以证明判稳；失败时继续求到无增广路径，以确定真正的排除上界。
    const auto overlap =
        left.size() <= right.size()
            ? approximate_overlap(baseline, current, channels, tolerance.hue_period, tolerance.linear, tolerance.hue, required)
            : approximate_overlap(current, baseline, channels, tolerance.hue_period, tolerance.linear, tolerance.hue, required);
    const auto lower = CountSimilarity(overlap.matched, total);
    return { lower, overlap.exact ? lower : upper };
}

} // namespace colorfreeze
