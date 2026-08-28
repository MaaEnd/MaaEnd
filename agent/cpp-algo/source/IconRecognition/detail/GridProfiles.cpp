#include "GridProfiles.h"

#include <algorithm>
#include <array>
#include <cctype>
#include <cmath>
#include <map>
#include <optional>
#include <set>
#include <stdexcept>
#include <tuple>
#include <vector>

#include "ForegroundTexture.h"
#include "GridFeatures.h"
#include "GridGeometry.h"

namespace iconrecognition::detail
{
namespace
{

// 结构响应比较的近零阈值，仅用于拒绝无有效边缘的图像。
constexpr double kEpsilon = 1e-8;
// 双侧网格的公共 64px cell、68..70px pitch 和可见性默认值。
constexpr TransferGridProfile kBaseTransferProfile {};
// 粗发现阶段允许的 pitch 范围；扩大可召回畸变网格，也会增加候选和误检。
constexpr std::pair<int, int> kTransferDiscoveryPitchRange { 66, 74 };
// 粗发现候选至少包含的列数；调高减少局部误检，调低可召回窄面板。
constexpr int kTransferDiscoveryMinimumColumns = 3;
// 粗发现候选至少包含的行数；调高减少局部误检，调低可召回浅面板。
constexpr int kTransferDiscoveryMinimumRows = 3;
// 单侧 transfer 候选允许的最大列数，防止把左右面板合成一个网格。
constexpr int kTransferMaximumColumns = 8;
// 右侧面板分区的 720p 参考宽度，用于缺少双候选时构造保守区域。
constexpr int kTransferRightPanelWidth = 398;
// 便捷存取站左面板在归一化 720p full ROI 中的搜索宽度；覆盖 Win32/ADB 左侧四列，并避开中间传送按钮。
constexpr int kPortStoragerLeftPanelWidth = 330;
// 便捷存取站右面板约从归一化 full ROI 的 42% 处开始；调低会混入中间 UI，调高会裁掉首列结构。
constexpr double kPortStoragerRightPanelStartRatio = 0.42;
// 局部峰相对全图最大响应的最低比例；调高抑制弱噪声，调低提高弱格框召回。
constexpr double kLocalPeakMaximumRatio = 0.22;
// 局部峰需达到的正响应百分位；调高只保留尖峰，调低会保留更多纹理峰。
constexpr double kLocalPeakPercentile = 92.0;
// 局部极大值抑制的方形邻域边长；调大合并近邻峰，调小保留更多候选。
constexpr int kLocalPeakNeighborhoodSize = 5;
// 浮点峰值与膨胀结果比较的相等容差，仅用于数值稳定性。
constexpr float kLocalPeakEqualityTolerance = 1e-7F;
// 传输网格候选至少覆盖的槽位比例；调高减少破碎误检，调低可召回遮挡较多的网格。
constexpr double kTransferHypothesisMinimumOccupancy = 0.42;
// 稀疏单行候选中非空物品格的最低纹理覆盖率；调高抑制背景伪网格，调低可召回暗淡物品。
constexpr double kMinimumSparseTransferTextureCoverage = 0.50;
// 已有多行候选达到该覆盖率时，优先保留多行结构，避免单行局部纹理截断正常背包。
constexpr double kMinimumStructuredTransferTextureCoverage = 0.60;
// 宽 ROI 中单侧候选的最大宽度比例；调大可能把左右两侧合并，调小可能截断真实面板。
constexpr double kTransferLocalizedMaximumWidthRatio = 0.70;
// 独立局部候选相对最强候选的最低分数；调高只保留强面板，调低可保留弱面板但增加冲突。
constexpr double kIndependentCandidateMinimumScoreRatio = 0.15;
// 左右面板配对时，较弱一侧相对较强一侧的最低分数；调高抑制伪配对，调低提高弱侧召回。
constexpr double kGridPairMinimumRelativeScore = 0.15;
// 结构响应图的平滑 sigma（像素）；调大可抑制噪声，调小保留窄边界但更易受噪声影响。
constexpr double kTransferStructureBlurSigma = 0.8;
// 局部峰抑制半径（像素）；调大合并相邻响应，调小保留更多候选峰。
constexpr int kClosePeakSuppressionRadius = 6;
// 晶格连通分量的同行/同列邻接容差（索引）；调大可连接缺口，调小可避免跨格连接。
constexpr int kLatticeComponentAdjacencyTolerance = 2;
// 单次局部峰检测最多保留的候选数量；调大提高召回但增加假设组合耗时。
constexpr int kMaximumLocalPeaks = 1000;
// 参与相位假设的最高分峰数量；调大扩大搜索，调小降低组合数量。
constexpr int kMaximumHypothesisSeeds = 160;
// 峰映射到候选晶格的最大坐标误差（像素）；调大提高错位容忍度，也会混入邻近峰。
constexpr int kPeakAssignmentTolerance = 4;
// 晶格列/行索引允许的最大缺口；调大可跨过空槽，调小要求更连续的结构。
constexpr int kMaximumLatticeGap = 2;
// 假设去重时的坐标分桶尺寸（像素）；调大合并更多近似假设，调小保留更多候选。
constexpr int kHypothesisBucketSize = 4;
// 宽 ROI 启用双侧面板发现的最小宽度（像素）；调大只处理更宽画面。
constexpr int kWideTransferRoiMinimumWidth = 700;
// 两个面板候选之间的最小间距（像素）；调大要求更明显的中间空隙。
constexpr int kPanelMinimumGap = 32;
// 判断候选是否已经覆盖左右面板边界时的容差（像素）。
constexpr int kPanelSpanningBoundaryTolerance = 16;
// 从主面板边界向外保留的局部结构上下文偏移（像素）。
constexpr int kPanelContextOffset = 12;
// 主面板与另一侧搜索区域之间的额外分离偏移（像素）。
constexpr int kPanelSeparationOffset = 52;
// 四个边界响应相乘后取四次方根，保持结构分数与单边响应处于相近量级。
constexpr double kTransferStructureGeometricMeanExponent = 0.25;

// 据点交易按 96px cell、310x109px pitch 和至少 3x3 卡片区域标定。
constexpr GridProfile kTradeGridProfile {
    .cell_size = 96,
    .pitch_x = 310.0,
    .pitch_y = 109.0,
    .min_columns = 3,
    .min_rows = 3,
};
// 背包和仓库沿用双侧网格的 64px cell、69px 首选 pitch，至少需要 3x3 结构证据。
constexpr GridProfile kTransferGridProfile {
    .cell_size = kBaseTransferProfile.cell_size,
    .pitch_x = kBaseTransferProfile.preferred_pitch,
    .pitch_y = kBaseTransferProfile.preferred_pitch,
    .min_columns = 3,
    .min_rows = 3,
};
// 便捷存取站与 transfer 使用相同几何基准，但后续选择不同的色带与边界策略。
constexpr GridProfile kPortStoragerGridProfile {
    .cell_size = kBaseTransferProfile.cell_size,
    .pitch_x = kBaseTransferProfile.preferred_pitch,
    .pitch_y = kBaseTransferProfile.preferred_pitch,
    .min_columns = 3,
    .min_rows = 3,
};
// 贵重品库按 96px cell、103.5px 双轴 pitch 标定；ADB 首屏为六列，Win32 宽 ROI 仍由结构证据扩展到七列。
constexpr GridProfile kValuablesGridProfile {
    .cell_size = 96,
    .pitch_x = 103.5,
    .pitch_y = 103.5,
    .min_columns = 6,
    .min_rows = 4,
};
// 送货界面按 64px cell、73.6x112px pitch 标定；底部操作栏可能只留下两行完整卡片。
constexpr GridProfile kShipmentGridProfile {
    .cell_size = 64,
    .pitch_x = 73.6,
    .pitch_y = 112.0,
    .min_columns = 4,
    .min_rows = 2,
};
// 信用交易卡片按 128px cell、161x205px pitch 和单行七列布局标定。
constexpr GridProfile kCreditTradeGridProfile {
    .cell_size = 128,
    .pitch_x = 161.0,
    .pitch_y = 205.0,
    .min_columns = 7,
    .min_rows = 1,
};
// 奖励界面按 96px cell、约 117px 横向 pitch 标定；布局整体居中，换行后共享首行左边界。
constexpr GridProfile kRewardsGridProfile {
    .cell_size = 96,
    .pitch_x = 117.0,
    .pitch_y = 117.0,
    .min_columns = 1,
    .min_rows = 1,
};

struct Peak
{
    int x = 0;
    int y = 0;
    float score = 0.0F;
};

struct TransferHypothesis
{
    cv::Rect rect;
    double score = 0.0;
    double occupancy = 0.0;
    int columns = 0;
    int rows = 0;
    std::vector<int> x_starts;
    std::vector<int> y_starts;
    double foreground_texture_coverage = 0.0;
};

double ForegroundTextureCoverage(const cv::Mat& crop, const TransferHypothesis& hypothesis)
{
    int textured_cells = 0;
    int total_cells = 0;
    for (int y : hypothesis.y_starts) {
        for (int x : hypothesis.x_starts) {
            ++total_cells;
            if (!IsLowTexture(crop, cv::Rect(x, y, kBaseTransferProfile.cell_size, kBaseTransferProfile.cell_size), GridType::Transfer)) {
                ++textured_cells;
            }
        }
    }
    return total_cells == 0 ? 0.0 : static_cast<double>(textured_cells) / total_cells;
}

std::vector<Peak> local_peaks(const cv::Mat& score, int maximum)
{
    if (score.empty()) {
        return {};
    }
    double maximum_score = 0.0;
    cv::minMaxLoc(score, nullptr, &maximum_score);
    if (maximum_score <= kEpsilon) {
        return {};
    }
    std::vector<float> positive;
    for (int y = 0; y < score.rows; ++y) {
        for (int x = 0; x < score.cols; ++x) {
            const float value = score.at<float>(y, x);
            if (value > 0.0F) {
                positive.push_back(value);
            }
        }
    }
    const double threshold = std::max(kLocalPeakMaximumRatio * maximum_score, Percentile(std::move(positive), kLocalPeakPercentile));
    cv::Mat dilated;
    cv::dilate(score, dilated, cv::Mat::ones(kLocalPeakNeighborhoodSize, kLocalPeakNeighborhoodSize, CV_8U));
    std::vector<Peak> peaks;
    for (int y = 0; y < score.rows; ++y) {
        for (int x = 0; x < score.cols; ++x) {
            const float value = score.at<float>(y, x);
            if (value >= threshold && value >= dilated.at<float>(y, x) - kLocalPeakEqualityTolerance) {
                peaks.push_back({ x, y, value });
            }
        }
    }
    std::ranges::sort(peaks, [](const Peak& left, const Peak& right) { return left.score > right.score; });
    if (static_cast<int>(peaks.size()) > maximum) {
        peaks.resize(maximum);
    }
    return peaks;
}

std::vector<Peak> suppress_close_peaks(const std::vector<Peak>& peaks, int radius)
{
    std::vector<Peak> kept;
    for (const Peak& peak : peaks) {
        if (std::ranges::all_of(kept, [&](const Peak& other) {
                return std::abs(peak.x - other.x) > radius || std::abs(peak.y - other.y) > radius;
            })) {
            kept.push_back(peak);
        }
    }
    return kept;
}

std::vector<std::vector<std::pair<int, int>>> lattice_components(const std::map<std::pair<int, int>, Peak>& slots)
{
    std::set<std::pair<int, int>> remaining;
    for (const auto& [key, value] : slots) {
        remaining.insert(key);
    }
    std::vector<std::vector<std::pair<int, int>>> components;
    while (!remaining.empty()) {
        const auto start = *remaining.begin();
        remaining.erase(remaining.begin());
        std::vector<std::pair<int, int>> component { start };
        std::vector<std::pair<int, int>> pending { start };
        while (!pending.empty()) {
            const auto current = pending.back();
            pending.pop_back();
            std::vector<std::pair<int, int>> linked;
            for (const auto& point : remaining) {
                if ((point.first == current.first && std::abs(point.second - current.second) <= kLatticeComponentAdjacencyTolerance)
                    || (point.second == current.second && std::abs(point.first - current.first) <= kLatticeComponentAdjacencyTolerance)) {
                    linked.push_back(point);
                }
            }
            for (const auto& point : linked) {
                remaining.erase(point);
                component.push_back(point);
                pending.push_back(point);
            }
        }
        components.push_back(std::move(component));
    }
    return components;
}

int rounded_median(const std::vector<int>& values)
{
    std::vector<double> converted(values.begin(), values.end());
    return cvRound(Median(std::move(converted)));
}

std::vector<TransferHypothesis>
    phase_hypotheses(const cv::Mat& crop, std::pair<int, int> pitch_range, int minimum_columns, int minimum_rows)
{
    const auto peaks = suppress_close_peaks(
        local_peaks(BuildTransferCellScore(crop, kBaseTransferProfile.cell_size), kMaximumLocalPeaks),
        kClosePeakSuppressionRadius);
    if (peaks.empty()) {
        return {};
    }
    std::map<std::array<int, 4>, TransferHypothesis> hypotheses;
    const int seed_count = std::min(static_cast<int>(peaks.size()), kMaximumHypothesisSeeds);
    for (int seed_index = 0; seed_index < seed_count; ++seed_index) {
        const Peak& seed = peaks[seed_index];
        for (int pitch_x = pitch_range.first; pitch_x <= pitch_range.second; ++pitch_x) {
            for (int pitch_y = pitch_range.first; pitch_y <= pitch_range.second; ++pitch_y) {
                std::map<std::pair<int, int>, Peak> slots;
                for (const Peak& peak : peaks) {
                    const int column = static_cast<int>(std::nearbyint(static_cast<double>(peak.x - seed.x) / pitch_x));
                    const int row = static_cast<int>(std::nearbyint(static_cast<double>(peak.y - seed.y) / pitch_y));
                    if (std::abs(peak.x - (seed.x + column * pitch_x)) > kPeakAssignmentTolerance
                        || std::abs(peak.y - (seed.y + row * pitch_y)) > kPeakAssignmentTolerance) {
                        continue;
                    }
                    const auto key = std::pair { column, row };
                    const auto found = slots.find(key);
                    if (found == slots.end() || peak.score > found->second.score) {
                        slots[key] = peak;
                    }
                }
                for (const auto& component : lattice_components(slots)) {
                    std::vector<int> columns;
                    std::vector<int> rows;
                    for (const auto& [column, row] : component) {
                        columns.push_back(column), rows.push_back(row);
                    }
                    std::ranges::sort(columns);
                    columns.erase(std::unique(columns.begin(), columns.end()), columns.end());
                    std::ranges::sort(rows);
                    rows.erase(std::unique(rows.begin(), rows.end()), rows.end());
                    if (static_cast<int>(columns.size()) < minimum_columns || static_cast<int>(rows.size()) < minimum_rows) {
                        continue;
                    }
                    const auto maximum_gap = [](const std::vector<int>& values) {
                        int maximum = 1;
                        for (std::size_t index = 1; index < values.size(); ++index) {
                            maximum = std::max(maximum, values[index] - values[index - 1]);
                        }
                        return maximum;
                    };
                    if (maximum_gap(columns) > kMaximumLatticeGap || maximum_gap(rows) > kMaximumLatticeGap) {
                        continue;
                    }
                    const double occupancy = static_cast<double>(component.size()) / (columns.size() * rows.size());
                    if (occupancy < kTransferHypothesisMinimumOccupancy) {
                        continue;
                    }
                    std::vector<int> x_starts;
                    std::vector<int> y_starts;
                    for (int column : columns) {
                        std::vector<int> values;
                        for (const auto& key : component) {
                            if (key.first == column) {
                                values.push_back(slots.at(key).x);
                            }
                        }
                        x_starts.push_back(rounded_median(values));
                    }
                    for (int row : rows) {
                        std::vector<int> values;
                        for (const auto& key : component) {
                            if (key.second == row) {
                                values.push_back(slots.at(key).y);
                            }
                        }
                        y_starts.push_back(rounded_median(values));
                    }
                    double mean_score = 0.0;
                    for (const auto& key : component) {
                        mean_score += slots.at(key).score;
                    }
                    mean_score /= component.size();
                    const double score = occupancy * mean_score * std::sqrt(static_cast<double>(columns.size() * rows.size()));
                    TransferHypothesis hypothesis {
                        .rect = cv::Rect(
                            x_starts.front(),
                            y_starts.front(),
                            x_starts.back() + kBaseTransferProfile.cell_size + 1 - x_starts.front(),
                            y_starts.back() + kBaseTransferProfile.cell_size + 1 - y_starts.front()),
                        .score = score,
                        .occupancy = occupancy,
                        .columns = static_cast<int>(columns.size()),
                        .rows = static_cast<int>(rows.size()),
                        .x_starts = std::move(x_starts),
                        .y_starts = std::move(y_starts),
                    };
                    const std::array key { hypothesis.rect.x / kHypothesisBucketSize,
                                           hypothesis.rect.y / kHypothesisBucketSize,
                                           (hypothesis.rect.x + hypothesis.rect.width) / kHypothesisBucketSize,
                                           (hypothesis.rect.y + hypothesis.rect.height) / kHypothesisBucketSize };
                    const auto found = hypotheses.find(key);
                    if (found == hypotheses.end() || hypothesis.score > found->second.score) {
                        hypotheses[key] = std::move(hypothesis);
                    }
                }
            }
        }
    }
    std::vector<TransferHypothesis> result;
    for (auto& [key, hypothesis] : hypotheses) {
        result.push_back(std::move(hypothesis));
    }
    std::ranges::sort(result, [](const auto& left, const auto& right) { return left.score > right.score; });
    return result;
}

std::vector<cv::Rect> discover_transfer_regions(const cv::Mat& crop)
{
    if (crop.cols <= kWideTransferRoiMinimumWidth) {
        return { cv::Rect(0, 0, crop.cols, crop.rows) };
    }
    const auto hypotheses =
        phase_hypotheses(crop, kTransferDiscoveryPitchRange, kTransferDiscoveryMinimumColumns, kTransferDiscoveryMinimumRows);
    std::vector<TransferHypothesis> localized;
    for (const auto& item : hypotheses) {
        if (item.columns <= kTransferMaximumColumns && item.rows <= kBaseTransferProfile.maximum_rows
            && item.rect.width <= crop.cols * kTransferLocalizedMaximumWidthRatio) {
            localized.push_back(item);
        }
    }
    if (localized.empty()) {
        return {};
    }
    std::vector<TransferHypothesis> independent;
    const double threshold = localized.front().score * kIndependentCandidateMinimumScoreRatio;
    for (const auto& hypothesis : localized) {
        if (hypothesis.score < threshold) {
            break;
        }
        if (std::ranges::any_of(independent, [&](const auto& accepted) { return !(hypothesis.rect & accepted.rect).empty(); })) {
            continue;
        }
        independent.push_back(hypothesis);
    }
    std::optional<std::pair<TransferHypothesis, TransferHypothesis>> best_pair;
    double best_pair_score = -1.0;
    for (const auto& left : localized) {
        for (const auto& right : localized) {
            if (left.rect.x >= right.rect.x || right.rect.x - (left.rect.x + left.rect.width) < kPanelMinimumGap) {
                continue;
            }
            const double weaker = std::min(left.score, right.score);
            const double stronger = std::max(left.score, right.score);
            if (weaker < stronger * kGridPairMinimumRelativeScore) {
                continue;
            }
            const bool spanned = std::ranges::any_of(localized, [&](const auto& item) {
                return &item != &left && &item != &right && item.score >= stronger
                       && item.rect.x <= left.rect.x + kPanelSpanningBoundaryTolerance
                       && item.rect.x + item.rect.width >= right.rect.x + right.rect.width - kPanelSpanningBoundaryTolerance;
            });
            if (spanned) {
                continue;
            }
            if (left.score + right.score > best_pair_score) {
                best_pair_score = left.score + right.score, best_pair = std::pair { left, right };
            }
        }
    }
    if (best_pair) {
        const auto& [left, right] = *best_pair;
        return PartitionTransferRegions(crop.size(), left.rect, right.rect);
    }
    if (independent.size() > 2) {
        return {};
    }
    const auto& dominant = localized.front();
    const double center = dominant.rect.x + dominant.rect.width / 2.0;
    if (center >= crop.cols / 2.0) {
        const int left_x2 = std::max(1, dominant.rect.x - kBaseTransferProfile.cell_size);
        const int right_x1 = std::clamp(dominant.rect.x - kPanelContextOffset, left_x2 + 1, crop.cols - 1);
        return { cv::Rect(0, 0, left_x2, crop.rows), cv::Rect(right_x1, 0, crop.cols - right_x1, crop.rows) };
    }
    const int left_x2 = std::min(crop.cols - 1, dominant.rect.x + dominant.rect.width + kPanelContextOffset);
    const int right_x1 = std::clamp(dominant.rect.x + dominant.rect.width + kPanelSeparationOffset, left_x2 + 1, crop.cols - 1);
    return { cv::Rect(0, 0, left_x2, crop.rows), cv::Rect(right_x1, 0, crop.cols - right_x1, crop.rows) };
}

std::optional<TransferHypothesis> select_grid_hypothesis(const cv::Mat& crop, bool require_texture)
{
    const int maximum_columns = std::max(1, (crop.cols - kBaseTransferProfile.cell_size) / kBaseTransferProfile.pitch_min + 1);
    const int maximum_rows = std::max(1, (crop.rows - kBaseTransferProfile.cell_size) / kBaseTransferProfile.pitch_min + 1);
    const auto candidates = [&](int minimum) {
        std::vector<TransferHypothesis> filtered;
        for (const auto& item :
             phase_hypotheses(crop, { kBaseTransferProfile.pitch_min, kBaseTransferProfile.pitch_max }, minimum, minimum)) {
            if (item.columns <= std::min(kTransferMaximumColumns, maximum_columns)
                && item.rows <= std::min(kBaseTransferProfile.maximum_rows, maximum_rows) && item.rect.x + item.rect.width <= crop.cols
                && item.rect.y + item.rect.height <= crop.rows) {
                filtered.push_back(item);
            }
        }
        return filtered;
    };
    auto hypotheses = candidates(3);
    for (auto& hypothesis : hypotheses) {
        hypothesis.foreground_texture_coverage = ForegroundTextureCoverage(crop, hypothesis);
    }
    const bool has_reliable_multirow = std::ranges::any_of(hypotheses, [](const auto& hypothesis) {
        return hypothesis.rows >= 2 && hypothesis.foreground_texture_coverage >= kMinimumStructuredTransferTextureCoverage;
    });
    const auto sparse_candidates = candidates(1);
    if (!has_reliable_multirow) {
        for (auto candidate : sparse_candidates) {
            if (candidate.rows != 1 || candidate.columns < 3) {
                continue;
            }
            candidate.foreground_texture_coverage = ForegroundTextureCoverage(crop, candidate);
            if (candidate.foreground_texture_coverage >= kMinimumSparseTransferTextureCoverage) {
                hypotheses.push_back(std::move(candidate));
            }
        }
    }
    if (hypotheses.empty()) {
        for (auto candidate : sparse_candidates) {
            candidate.foreground_texture_coverage = ForegroundTextureCoverage(crop, candidate);
            if (candidate.foreground_texture_coverage > 0.0) {
                hypotheses.push_back(std::move(candidate));
            }
        }
    }
    if (hypotheses.empty()) {
        return std::nullopt;
    }
    const bool has_reliable_texture_evidence = std::ranges::any_of(hypotheses, [](const TransferHypothesis& hypothesis) {
        return hypothesis.foreground_texture_coverage >= kMinimumSparseTransferTextureCoverage;
    });
    if (require_texture) {
        std::erase_if(hypotheses, [has_reliable_texture_evidence](const TransferHypothesis& hypothesis) {
            return has_reliable_texture_evidence ? hypothesis.foreground_texture_coverage < kMinimumSparseTransferTextureCoverage
                                                 : hypothesis.foreground_texture_coverage <= 0.0;
        });
    }
    if (hypotheses.empty()) {
        return std::nullopt;
    }
    const auto rank = [require_texture](const TransferHypothesis& item) {
        if (require_texture) {
            return std::tuple<double, double, double, double, double, double, double, double> {
                static_cast<double>(item.columns),
                item.foreground_texture_coverage,
                static_cast<double>(item.columns == 7 ? 0 : item.rows),
                static_cast<double>(-item.rect.x),
                item.occupancy,
                item.score,
                static_cast<double>(item.rows),
                static_cast<double>(-item.rect.y),
            };
        }
        return std::tuple<double, double, double, double, double, double, double, double> {
            static_cast<double>(item.columns),
            static_cast<double>(-item.rect.x),
            static_cast<double>(item.columns == 7 ? 0 : item.rows),
            item.occupancy,
            item.score,
            static_cast<double>(item.rows),
            static_cast<double>(-item.rect.y),
            0.0,
        };
    };
    return *std::ranges::max_element(hypotheses, {}, rank);
}

TransferGridHint to_hint(const TransferHypothesis& hypothesis, const cv::Rect& region, int offset_x)
{
    TransferGridHint hint {
        .region = region,
        .rect = cv::Rect(offset_x + hypothesis.rect.x, hypothesis.rect.y, hypothesis.rect.width, hypothesis.rect.height),
        .score = hypothesis.score,
        .occupancy = hypothesis.occupancy,
        .x_starts = hypothesis.x_starts,
        .y_starts = hypothesis.y_starts,
        .foreground_texture_coverage = hypothesis.foreground_texture_coverage,
    };
    for (int& value : hint.x_starts) {
        value += offset_x;
    }
    return hint;
}

} // namespace

std::vector<cv::Rect> PartitionTransferRegions(cv::Size crop_size, const cv::Rect& left, const cv::Rect& right)
{
    if (crop_size.width <= 1 || crop_size.height <= 0) {
        throw std::invalid_argument("transfer crop must have positive dimensions");
    }
    const int left_end = std::clamp(left.x + left.width, 1, crop_size.width - 1);
    const int right_begin = std::clamp(right.x, 1, crop_size.width - 1);
    if (left_end >= right_begin) {
        throw std::invalid_argument("transfer grids must have a separating gap");
    }

    // 分界放在两个已确认网格之间，保留边缘弱纹理单元供后续完整网格拟合。
    const int split = left_end + (right_begin - left_end) / 2;
    // 右侧分区向左保留的结构上下文（像素）；调大有利于边缘 cell 拟合，也会增加区域重叠。
    constexpr int kStructureContext = 12;
    const int right_start = std::clamp(right_begin - kStructureContext, split + 1, crop_size.width - 1);
    return { cv::Rect(0, 0, split, crop_size.height), cv::Rect(right_start, 0, crop_size.width - right_start, crop_size.height) };
}

std::vector<cv::Rect> PartitionPortStoragerRegions(cv::Size crop_size)
{
    const int right_start = cvRound(crop_size.width * kPortStoragerRightPanelStartRatio);
    if (right_start <= 0 || right_start >= crop_size.width || crop_size.height <= 0) {
        throw std::invalid_argument("port_storager full crop is too small for two panels");
    }
    const int left_width = std::min(kPortStoragerLeftPanelWidth, crop_size.width - 1);
    return {
        cv::Rect(0, 0, left_width, crop_size.height),
        cv::Rect(right_start, 0, crop_size.width - right_start, crop_size.height),
    };
}

std::optional<double> GridScaleForControllerType(std::string_view controller_type)
{
    const auto equals_ignore_case = [controller_type](std::string_view candidate) {
        return controller_type.size() == candidate.size() && std::ranges::equal(controller_type, candidate, [](char left, char right) {
                   return std::tolower(static_cast<unsigned char>(left)) == std::tolower(static_cast<unsigned char>(right));
               });
    };
    const auto matches_any = [&](const auto& candidates) {
        return std::ranges::any_of(candidates, equals_ignore_case);
    };
    // Linux 与 MacOS 暂按标准桌面 profile 处理；这些别名尚无独立截图数据验证。
    constexpr std::array<std::string_view, 4> kStandardControllerTypes { "Win32", "Linux", "MacOS" };
    // CloudADB 的 MaaController type 是 Adb，因此放大 profile 会自然覆盖 CloudADB；PlayCover 暂沿用该 profile。
    constexpr std::array<std::string_view, 2> kAdbControllerTypes { "Adb", "PlayCover" };
    if (matches_any(kAdbControllerTypes)) {
        return kAdbControllerGridScale;
    }
    if (matches_any(kStandardControllerTypes)) {
        return kWin32ControllerGridScale;
    }
    return std::nullopt;
}

GridProfile ProfileFor(GridType type)
{
    switch (type) {
    case GridType::Trade:
        return kTradeGridProfile;
    case GridType::Transfer:
        return kTransferGridProfile;
    case GridType::PortStorager:
        return kPortStoragerGridProfile;
    case GridType::Valuables:
        return kValuablesGridProfile;
    case GridType::Shipment:
        return kShipmentGridProfile;
    case GridType::CreditTrade:
        return kCreditTradeGridProfile;
    case GridType::Rewards:
        return kRewardsGridProfile;
    case GridType::SingleRoi:
        throw std::invalid_argument("single_roi does not use a grid profile");
    }
    throw std::invalid_argument("unknown grid type");
}

TransferGridProfile TransferProfileFor(TransferGridVariant variant)
{
    // 背包左侧允许 85% 顶部可见率，兼容 ROI 从首行中部开始的截图。
    constexpr TransferGridProfile kTransferLeft {
        .rarity_anchor_offset = 64,
        .minimum_top_visibility = 0.85,
    };
    // 背包右侧固定 69px pitch，避免五列短轴被物品纹理拉偏 1px。
    constexpr TransferGridProfile kTransferRight {
        .pitch_min = 69,
        .pitch_max = 69,
        .rarity_anchor_offset = 64,
        .minimum_top_visibility = 0.85,
    };
    // 便捷存取站左侧要求 90% 顶部可见率，减少标题区或遮挡造成的伪首行。
    constexpr TransferGridProfile kPortLeft {
        .rarity_anchor_offset = 64,
        .minimum_top_visibility = 0.90,
    };
    // 便捷存取站右侧要求 80% 底部可见率，拒绝 full ROI 中只露出约 75% 的滚动残行。
    constexpr TransferGridProfile kPortRight {
        .rarity_anchor_offset = 64,
        .minimum_top_visibility = 0.90,
        .minimum_bottom_visibility = 0.80,
    };
    switch (variant) {
    case TransferGridVariant::TransferLeft:
        return kTransferLeft;
    case TransferGridVariant::TransferRight:
        return kTransferRight;
    case TransferGridVariant::PortStoragerLeft:
        return kPortLeft;
    case TransferGridVariant::PortStoragerRight:
        return kPortRight;
    }
    throw std::invalid_argument("unknown transfer grid variant");
}

cv::Mat BuildTransferCellScore(const cv::Mat& image, int cell_size)
{
    const StructureMaps maps = BuildStructureMaps(image, cell_size);
    const int extent = cell_size + 1;
    cv::Mat vertical_support;
    cv::Mat horizontal_support;
    cv::boxFilter(maps.vertical, vertical_support, CV_32F, cv::Size(1, extent), cv::Point(0, 0), true, cv::BORDER_CONSTANT);
    cv::boxFilter(maps.horizontal, horizontal_support, CV_32F, cv::Size(extent, 1), cv::Point(0, 0), true, cv::BORDER_CONSTANT);
    const int valid_height = maps.vertical.rows - cell_size;
    const int valid_width = maps.vertical.cols - cell_size;
    if (valid_height <= 0 || valid_width <= 0) {
        return {};
    }
    cv::Mat result(valid_height, valid_width, CV_32F);
    for (int y = 0; y < valid_height; ++y) {
        for (int x = 0; x < valid_width; ++x) {
            const double product = std::max(
                static_cast<double>(vertical_support.at<float>(y, x)) * vertical_support.at<float>(y, x + cell_size)
                    * horizontal_support.at<float>(y, x) * horizontal_support.at<float>(y + cell_size, x),
                0.0);
            result.at<float>(y, x) = static_cast<float>(std::pow(product, kTransferStructureGeometricMeanExponent));
        }
    }
    cv::GaussianBlur(result, result, cv::Size(), kTransferStructureBlurSigma);
    return result;
}

std::vector<TransferGridHint> DiscoverTransferGridHints(const cv::Mat& crop, bool structural_rank)
{
    if (structural_rank && crop.cols > kWideTransferRoiMinimumWidth) {
        // transfer 左右面板之间存在稳定空隙；宽 ROI 先拆成单侧，保证 full 与独立 side 使用同一套候选逻辑。
        const int split = crop.cols - kTransferRightPanelWidth;
        const std::array<cv::Rect, 2> partitions {
            cv::Rect(0, 0, split, crop.rows),
            cv::Rect(split, 0, crop.cols - split, crop.rows),
        };
        std::vector<TransferGridHint> combined;
        for (const cv::Rect& partition : partitions) {
            auto local = DiscoverTransferGridHints(crop(partition), structural_rank);
            if (local.size() != 1) {
                return {};
            }
            TransferGridHint hint = std::move(local.front());
            hint.region.x += partition.x;
            hint.region.y += partition.y;
            hint.rect.x += partition.x;
            hint.rect.y += partition.y;
            for (int& x : hint.x_starts) {
                x += partition.x;
            }
            for (int& y : hint.y_starts) {
                y += partition.y;
            }
            combined.push_back(std::move(hint));
        }
        return combined;
    }
    if (!structural_rank && crop.cols > kWideTransferRoiMinimumWidth) {
        // port_storager 的中间传送 UI 会形成强边缘；宽 full ROI 先按共同 720p 面板范围隔离，再复用单侧候选逻辑。
        std::vector<TransferGridHint> combined;
        for (const cv::Rect& partition : PartitionPortStoragerRegions(crop.size())) {
            auto local = DiscoverTransferGridHints(crop(partition), structural_rank);
            if (local.size() != 1) {
                return {};
            }
            TransferGridHint hint = std::move(local.front());
            hint.region.x += partition.x;
            hint.region.y += partition.y;
            hint.rect.x += partition.x;
            hint.rect.y += partition.y;
            for (int& x : hint.x_starts) {
                x += partition.x;
            }
            for (int& y : hint.y_starts) {
                y += partition.y;
            }
            combined.push_back(std::move(hint));
        }
        return combined;
    }
    const auto regions = discover_transfer_regions(crop);
    auto broad = phase_hypotheses(crop, kTransferDiscoveryPitchRange, kTransferDiscoveryMinimumColumns, kTransferDiscoveryMinimumRows);
    for (auto& hypothesis : broad) {
        hypothesis.foreground_texture_coverage = ForegroundTextureCoverage(crop, hypothesis);
    }
    const bool broad_has_texture_evidence =
        std::ranges::any_of(broad, [](const TransferHypothesis& hypothesis) { return hypothesis.foreground_texture_coverage > 0.0; });
    std::vector<TransferGridHint> hints;
    for (const auto& raw_region : regions) {
        const cv::Rect region(raw_region.x, 0, raw_region.width, crop.rows);
        std::vector<TransferGridHint> candidates;
        if (const auto local = select_grid_hypothesis(crop(region), structural_rank)) {
            candidates.push_back(to_hint(*local, region, region.x));
        }
        for (const auto& hypothesis : broad) {
            if ((!structural_rank || !broad_has_texture_evidence
                 || hypothesis.foreground_texture_coverage >= kMinimumSparseTransferTextureCoverage)
                && hypothesis.rect.x >= region.x && hypothesis.rect.x + hypothesis.rect.width <= region.x + region.width) {
                candidates.push_back(to_hint(hypothesis, region, 0));
            }
        }
        if (candidates.empty()) {
            return {};
        }
        const auto rank = [&](const TransferGridHint& hint) {
            const int columns = static_cast<int>(hint.x_starts.size());
            std::vector<double> spacings;
            for (std::size_t index = 1; index < hint.x_starts.size(); ++index) {
                spacings.push_back(hint.x_starts[index] - hint.x_starts[index - 1]);
            }
            const double pitch = spacings.empty() ? kBaseTransferProfile.preferred_pitch : Median(spacings);
            if (structural_rank) {
                return std::tuple<double, double, double, double, double, double, double, double> {
                    static_cast<double>(columns),
                    hint.foreground_texture_coverage,
                    static_cast<double>(columns == 7 ? 0 : hint.y_starts.size()),
                    hint.occupancy,
                    hint.score,
                    -std::abs(pitch - kBaseTransferProfile.preferred_pitch),
                    static_cast<double>(-(hint.rect.x - hint.region.x)),
                    static_cast<double>(hint.y_starts.size()),
                };
            }
            return std::tuple<double, double, double, double, double, double, double, double> {
                static_cast<double>(columns),
                static_cast<double>(-(hint.rect.x - hint.region.x)),
                static_cast<double>(columns == 7 ? 0 : hint.y_starts.size()),
                hint.occupancy,
                hint.score,
                -std::abs(pitch - kBaseTransferProfile.preferred_pitch),
                static_cast<double>(hint.y_starts.size()),
                static_cast<double>(-hint.rect.y),
            };
        };
        hints.push_back(*std::ranges::max_element(candidates, {}, rank));
    }
    return hints;
}

} // namespace iconrecognition::detail
