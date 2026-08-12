#include "GridDetector.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <limits>
#include <map>
#include <numeric>
#include <optional>
#include <set>
#include <stdexcept>
#include <tuple>
#include <vector>

#include "GridAnchors.h"
#include "GridFeatures.h"
#include "GridGeometry.h"
#include "GridProfiles.h"

namespace iconrecognition::detail
{
namespace
{

// 响应、分母和归一化的近零阈值，仅用于数值稳定性。
constexpr double kEpsilon = 1e-8;
// 信用交易界面固定七列卡片，用于限制晶格横向扩展。
constexpr int kCreditTradeColumns = 7;
// 从信用交易卡片左边缘到 128px 图标 cell 的横向偏移；数值增大时 cell 向右移动。
constexpr int kCreditTradeCellOffsetX = 10;
// 从信用交易卡片顶边到 128px 图标 cell 的纵向偏移；数值增大时 cell 向下移动。
constexpr int kCreditTradeCellOffsetY = 6;
// 顶部被 ROI 裁切时保留 cell 所需的默认可见比例；调高更严格，调低可保留更多残缺首行。
constexpr double kDefaultMinimumTopVisibility = 0.90;
// 底部被 ROI 裁切时保留 cell 所需的默认可见比例；调高更严格，调低可保留更多残缺末行。
constexpr double kDefaultMinimumBottomVisibility = 0.70;
// 左右被 ROI 裁切时保留 cell 所需的最小可见比例；调高减少边缘残格，调低提高边缘召回。
constexpr double kMinimumHorizontalVisibility = 0.70;
// 单网格初始周期估计相对 profile pitch 的搜索半径；调大可适应缩放偏差，但增加误周期候选。
constexpr int kSingleLatticePitchSearchRadius = 8;
// 周期估计后正式轴拟合允许的 pitch 偏差；调大提高容忍度，也会放宽不规则序列。
constexpr int kSingleLatticePitchTolerance = 1;
// 卡片纵向评分避开左右边缘的最小内缩像素数。
constexpr int kCardMinimumInset = 6;
// 卡片纵向评分内缩相对 cell 的比例；调大更避开边框，也减少参与评分的横向范围。
constexpr double kCardInsetRatio = 0.08;
// 卡片上下边界采样带的最小半宽（像素）。
constexpr int kCardMinimumBand = 4;
// 卡片上下边界采样带相对 cell 的比例；调大增强宽边响应，也会混入更多邻域。
constexpr double kCardBandRatio = 0.05;
// 卡片 pitch 细化相对 profile 先验允许的最大偏差；调大提高召回，也会扩大搜索空间。
constexpr double kCardProfilePitchRadius = 8.0;
// 卡片 pitch 细化相对当前估计允许的局部偏差；调小更保守，调大可能跳到相邻周期。
constexpr double kCardCurrentPitchRadius = 1.5;
// 卡片 pitch 细化步长（像素）；调小提高精度但增加评分次数。
constexpr double kPitchRefinementStep = 0.25;
// 卡片 pitch 浮点循环的闭区间容差，仅用于确保搜索包含上界。
constexpr double kPitchLoopEpsilon = 0.001;
// 采用卡片细化结果所需的最低绝对分数增益；调高更保守，调低更容易替换初始相位。
constexpr double kCardMinimumAbsoluteGain = 30.0;
// 采用卡片细化结果所需的最低相对增益倍数；调高要求改善更明显。
constexpr double kCardMinimumRelativeGain = 1.20;
// 默认结构相位校正允许的最大像素平移；调大可修复更大错位，也增加跳格风险。
constexpr int kDefaultStructuralPhaseMaximumShift = 20;
// 结构相位替换当前相位所需的最低分数增益；调高更保守，调低更容易校正。
constexpr double kDefaultStructuralPhaseMinimumGain = 0.08;
// 默认忽略的小相位位移范围；调大减少无意义微调，也可能留下真实小偏差。
constexpr int kIgnoredStructuralPhaseShift = 4;
// 结构相位候选的最低响应；调高减少背景纹理误触发，调低可召回弱格框。
constexpr double kMinimumStructuralPhaseResponse = 0.15;
// 双侧轴拟合分数中每像素残差的惩罚系数；调高更偏好规则轴，调低提高畸变容忍度。
constexpr double kTransferAxisResidualPenalty = 0.05;
// 宽 transfer ROI 二次相位校正的最大平移；相对默认值更小以避免跨面板跳转。
constexpr int kWideTransferPhaseMaximumShift = 12;
// 宽 transfer ROI 二次相位校正所需增益；调高更保守，调低可修复较弱边框。
constexpr double kWideTransferPhaseMinimumGain = 0.25;
// 可信色带候选中结构支持的权重；调高更依赖格框证据。
constexpr double kTrustedStructureWeight = 0.40;
// 可信色带平均置信度的权重；调高更依赖颜色、连续性和背景对比质量。
constexpr double kTrustedConfidenceWeight = 0.35;
// 可信色带横纵轴一致性的权重；调高更偏好两轴都稳定的晶格。
constexpr double kTrustedConsistencyWeight = 0.25;
// 缺少 legacy 对齐时允许可信色带接管晶格所需的最少 cell 数；调高更保守。
constexpr int kMinimumTrustedCellsWithoutLegacySupport = 2;
// 缺少 legacy 对齐时允许可信色带接管晶格的最低结构支持；调高更依赖格框。
constexpr double kMinimumTrustedStructureWithoutLegacySupport = 0.10;
// legacy 候选中格框结构分数的权重；调高更看重结构拟合。
constexpr double kLegacyStructureWeight = 0.65;
// legacy 候选中已对齐 rarity 色带比例的权重；调高更看重颜色锚点。
constexpr double kLegacyRarityWeight = 0.35;
// 补行所需的最低结构支持相对已有行均值比例；调高减少补行，调低可能扩展到空白行。
constexpr double kRowCompletionSupportRatio = 0.04;
// 仅一个直接观测时最多允许补出的行数；调大可覆盖更多行，也会放大单点误差。
constexpr std::size_t kSingleObservationCompletionLimit = 2;
// 允许结构证据补足末行所需的最少直接 rarity 行数；调高更保守，调低更易补行。
constexpr std::size_t kStableRarityMinimumRows = 3;
// 少量卡片时，允许的相位残差占网格 pitch 的比例；调大提高召回，调小可抑制误拟合。
constexpr double kCreditTradeMaximumPhaseResidualRatio = 0.04;
// 边界中心只采纳接近峰顶的平台样本；调高更抗旁瓣，调低可追踪较宽但较弱的边界。
constexpr float kBoundaryCenterPlateauRatio = 0.90F;
// 端口边界校正的最低相对响应；调高会过滤弱证据，调低会增加误校正风险。
constexpr float kPortBoundaryMinimumRelativeScore = 0.15F;
// 多个端口边界校正量允许的最大极差（像素）；调大容忍不一致证据，也增加误校正风险。
constexpr double kPortBoundaryMaximumDelta = 1.0;
// 端口边界最终允许的最大整体平移（像素）；调大可修复更大偏差，也可能移动到邻近边缘。
constexpr int kPortBoundaryMaximumShift = 4;
// 首边界细化允许的整体平移范围（像素）；调大提高修正能力，也可能跨到相邻边界。
constexpr int kFirstBoundaryMaximumShift = 4;
// 双侧轴相位细化的搜索半宽（像素）；调大提高相位召回但增加评分次数。
constexpr double kAxisPhaseRefinementHalfRange = 2.0;
// 未细化相位循环的上界容差；仅用于让浮点起点进入一次循环。
constexpr double kAxisPhaseLoopEpsilon = 0.0001;
// 双侧轴相位细化步长（像素）；调小提高精度但增加评分次数。
constexpr double kAxisPhaseRefinementStep = 0.25;
// 未细化相位只检查起点，步长保持一个像素以避免额外候选。
constexpr double kAxisPhaseCoarseStep = 1.0;
// 信用交易白色卡片的 HSV 下界；提高 V 会漏掉偏暗卡片，放宽 S 会混入彩色区域。
const cv::Scalar kCreditTradeCardHsvLower { 0, 0, 226 };
// 信用交易白色卡片的 HSV 上界；H 覆盖完整色相，S 上限限制为低饱和背景。
const cv::Scalar kCreditTradeCardHsvUpper { 179, 34, 255 };
// 卡片连通域使用八邻域，允许斜向相连的亮色像素形成完整卡片区域。
constexpr int kCreditTradeConnectivity = 8;
// 卡片亮区允许的最小宽度（像素）；调高会漏掉被裁切卡片。
constexpr int kCreditTradeCardMinimumWidth = 140;
// 卡片亮区允许的最大宽度（像素）；调高可能接纳相邻卡片合并区域。
constexpr int kCreditTradeCardMaximumWidth = 155;
// 卡片亮区允许的最小高度（像素）；调高会漏掉被遮挡或裁切卡片。
constexpr int kCreditTradeCardMinimumHeight = 150;
// 卡片亮区允许的最大高度（像素）；调高可能接纳大块界面背景。
constexpr int kCreditTradeCardMaximumHeight = 180;
// 卡片亮区的最小连通面积（像素）；调高抑制噪声，也会拒绝破碎卡片。
constexpr int kCreditTradeCardMinimumArea = 5000;
// 使用卡片相位前所需的最少卡片数；不足时退回通用晶格检测。
constexpr std::size_t kCreditTradeMinimumCardCount = 2;
// 少于该数量时使用相位一致性校验，避免少量卡片直接生成错误晶格。
constexpr std::size_t kCreditTradeSparseCardCount = 5;
// 首行达到该卡片数时补全固定七列，兼容已售罄卡片造成的亮区缺失。
constexpr int kCreditTradeFirstRowCompletionCount = 5;
// 奖励卡片白色底板的 HSV 下界，按 720p 实际奖励截图标定；提高 V 会漏掉暗化卡片。
const cv::Scalar kRewardsCardHsvLower { 0, 0, 185 };
// 奖励卡片白色底板的 HSV 上界，按 720p 实际奖励截图标定；提高 S 上限会混入彩色背景和标题光效。
const cv::Scalar kRewardsCardHsvUpper { 179, 65, 255 };
// 白色底板连通域使用八邻域，允许抗锯齿产生的斜向亮色像素保持连通。
constexpr int kRewardsConnectivity = 8;
// 奖励卡片候选最小边长（720p 像素），按 96px cell 和边框缺损标定；调高会漏掉暗化或破碎卡片。
constexpr int kRewardsMinimumCardSize = 78;
// 奖励卡片候选最大边长（720p 像素），按 96px cell 和边框高光标定；调高会接纳更大的背景亮块。
constexpr int kRewardsMaximumCardSize = 112;
// 奖励卡片候选最小亮色面积（720p 平方像素）；调高抑制零碎高光，调低召回被文字切碎的卡片。
constexpr int kRewardsMinimumCardArea = 2600;
// 奖励卡片最小宽高比，按近方形白色底板标定；调低可容忍横向裁切，也会接纳更多窄背景块。
constexpr double kRewardsMinimumCardAspectRatio = 0.82;
// 奖励卡片最大宽高比，按近方形白色底板标定；调高可容忍纵向裁切，也会接纳更多宽背景块。
constexpr double kRewardsMaximumCardAspectRatio = 1.22;
// 同一行卡片中心允许的纵向差异（720p 像素）；调大可能合并相邻行，调小可能拆散轻微错位的同一行。
constexpr int kRewardsRowCenterTolerance = 24;

bool IsFormal(
    const cv::Rect& cell,
    const cv::Rect& roi,
    double minimum_top_visibility = kDefaultMinimumTopVisibility,
    double minimum_bottom_visibility = kDefaultMinimumBottomVisibility)
{
    const cv::Rect intersection = cell & roi;
    if (intersection.empty()) {
        return false;
    }
    const double visible_x = static_cast<double>(intersection.width) / cell.width;
    const double visible_y = static_cast<double>(intersection.height) / cell.height;
    const bool top_ok = cell.y >= roi.y || visible_y >= minimum_top_visibility;
    const bool bottom_ok = cell.y + cell.height <= roi.y + roi.height || visible_y >= minimum_bottom_visibility;
    return visible_x >= kMinimumHorizontalVisibility && top_ok && bottom_ok;
}

GridLayout DetectSingleLattice(const cv::Mat& image, GridType type, const cv::Rect& roi)
{
    const GridProfile profile = ProfileFor(type);
    const cv::Mat crop = image(roi);
    const StructureMaps maps = BuildStructureMaps(crop, profile.cell_size);
    const auto x_signal = RobustProjection(maps.vertical, true);
    const auto y_signal = RobustProjection(maps.horizontal, false);
    const auto diagonal_x = RobustProjection(maps.diagonal_penalty, true);
    const auto diagonal_y = RobustProjection(maps.diagonal_penalty, false);
    const auto signed_x = AggregateSigned(maps.signed_x, true);
    const auto signed_y = AggregateSigned(maps.signed_y, false);
    cv::Mat bgr;
    if (crop.channels() == 4) {
        cv::cvtColor(crop, bgr, cv::COLOR_BGRA2BGR);
    }
    else {
        bgr = crop;
    }
    cv::Mat gray;
    cv::cvtColor(bgr, gray, cv::COLOR_BGR2GRAY);
    gray.convertTo(gray, CV_32F, 1.0 / 255.0);
    const auto support_x = MedianProjection(gray, true);
    const auto support_y = MedianProjection(gray, false);
    const int pitch_x = EstimatePeriod(
        x_signal,
        static_cast<int>(std::floor(profile.pitch_x)) - kSingleLatticePitchSearchRadius,
        static_cast<int>(std::ceil(profile.pitch_x)) + kSingleLatticePitchSearchRadius);
    const int pitch_y = EstimatePeriod(
        y_signal,
        static_cast<int>(std::floor(profile.pitch_y)) - kSingleLatticePitchSearchRadius,
        static_cast<int>(std::ceil(profile.pitch_y)) + kSingleLatticePitchSearchRadius);
    const auto pitch_range_x = std::pair { pitch_x - kSingleLatticePitchTolerance, pitch_x + kSingleLatticePitchTolerance };
    const auto pitch_range_y = std::pair { pitch_y - kSingleLatticePitchTolerance, pitch_y + kSingleLatticePitchTolerance };
    const int expected_columns = std::max(profile.min_columns, (roi.width - profile.cell_size) / std::max(pitch_x, 1) + 1);
    const int expected_rows = std::max(profile.min_rows, (roi.height - profile.cell_size) / std::max(pitch_y, 1) + 1);
    const AxisSequence x_axis =
        FitSubpixelAxis(x_signal, signed_x, support_x, diagonal_x, profile.cell_size, pitch_x, pitch_range_x, expected_columns);
    const AxisSequence y_axis =
        FitSubpixelAxis(y_signal, signed_y, support_y, diagonal_y, profile.cell_size, pitch_y, pitch_range_y, expected_rows);

    GridLayout layout;
    layout.grid_index = 0;
    layout.cell_size = profile.cell_size;
    layout.pitch_x = x_axis.spacings.empty() ? pitch_x : Median(x_axis.spacings);
    layout.pitch_y = y_axis.spacings.empty() ? pitch_y : Median(y_axis.spacings);
    std::vector<int> kept_x;
    std::vector<int> kept_y;
    for (int local_x : x_axis.integer_starts) {
        const int absolute_x = roi.x + local_x;
        if (IsFormal(cv::Rect(absolute_x, roi.y, profile.cell_size, profile.cell_size), roi)) {
            kept_x.push_back(absolute_x);
        }
    }
    for (int local_y : y_axis.integer_starts) {
        const int absolute_y = roi.y + local_y;
        if (IsFormal(cv::Rect(roi.x, absolute_y, profile.cell_size, profile.cell_size), roi)) {
            kept_y.push_back(absolute_y);
        }
    }
    for (int row = 0; row < static_cast<int>(kept_y.size()); ++row) {
        for (int column = 0; column < static_cast<int>(kept_x.size()); ++column) {
            layout.cells.push_back({ 0, row, column, cv::Rect(kept_x[column], kept_y[row], profile.cell_size, profile.cell_size) });
        }
    }
    if (layout.cells.empty()) {
        throw std::runtime_error("grid ROI contains no formal cells");
    }
    layout.rows = static_cast<int>(kept_y.size());
    layout.columns = static_cast<int>(kept_x.size());
    layout.bounds = cv::Rect(
        kept_x.front(),
        kept_y.front(),
        kept_x.back() + profile.cell_size - kept_x.front(),
        kept_y.back() + profile.cell_size - kept_y.front());
    return layout;
}

GridDetection DetectRewardsGrid(const cv::Mat& image, const cv::Rect& roi)
{
    const GridProfile profile = ProfileFor(GridType::Rewards);
    const cv::Mat crop = image(roi);
    cv::Mat bgr;
    if (crop.channels() == 4) {
        cv::cvtColor(crop, bgr, cv::COLOR_BGRA2BGR);
    }
    else {
        bgr = crop;
    }
    cv::Mat hsv;
    cv::cvtColor(bgr, hsv, cv::COLOR_BGR2HSV);
    cv::Mat bright;
    cv::inRange(hsv, kRewardsCardHsvLower, kRewardsCardHsvUpper, bright);
    cv::Mat labels;
    cv::Mat stats;
    cv::Mat centroids;
    const int component_count = cv::connectedComponentsWithStats(bright, labels, stats, centroids, kRewardsConnectivity);
    struct Candidate
    {
        cv::Rect box;
        double center_y = 0.0;
    };
    std::vector<Candidate> candidates;
    for (int index = 1; index < component_count; ++index) {
        const int x = stats.at<int>(index, cv::CC_STAT_LEFT);
        const int y = stats.at<int>(index, cv::CC_STAT_TOP);
        const int width = stats.at<int>(index, cv::CC_STAT_WIDTH);
        const int height = stats.at<int>(index, cv::CC_STAT_HEIGHT);
        const int area = stats.at<int>(index, cv::CC_STAT_AREA);
        if (width < kRewardsMinimumCardSize || width > kRewardsMaximumCardSize || height < kRewardsMinimumCardSize
            || height > kRewardsMaximumCardSize || area < kRewardsMinimumCardArea) {
            continue;
        }
        const double aspect = static_cast<double>(width) / height;
        if (aspect < kRewardsMinimumCardAspectRatio || aspect > kRewardsMaximumCardAspectRatio) {
            continue;
        }
        candidates.push_back({ cv::Rect(roi.x + x, roi.y + y, width, height), roi.y + y + height * 0.5 });
    }
    if (candidates.empty()) {
        throw std::runtime_error("rewards ROI contains no card candidates");
    }
    std::ranges::sort(candidates, [](const Candidate& left, const Candidate& right) { return left.center_y < right.center_y; });
    std::vector<std::vector<Candidate>> rows;
    for (const Candidate& candidate : candidates) {
        if (rows.empty() || std::abs(candidate.center_y - rows.back().front().center_y) > kRewardsRowCenterTolerance) {
            rows.emplace_back();
        }
        rows.back().push_back(candidate);
    }
    GridDetection result { GridType::Rewards, roi, {}, {} };
    int grid_index = 0;
    for (auto& row : rows) {
        std::ranges::sort(row, [](const Candidate& left, const Candidate& right) { return left.box.x < right.box.x; });
        GridLayout layout;
        layout.grid_index = grid_index++;
        layout.cell_size = profile.cell_size;
        layout.rows = 1;
        std::vector<double> row_tops;
        row_tops.reserve(row.size());
        std::ranges::transform(row, std::back_inserter(row_tops), [](const Candidate& item) {
            return static_cast<double>(item.box.y);
        });
        // 白色连通域不包含底部彩色色条，因此必须从卡片顶边定位完整 cell；按中心反推会把框上移并裁掉色条。
        const int row_top = cvRound(Median(std::move(row_tops)));
        layout.pitch_y = profile.cell_size;
        if (row.size() > 1) {
            std::vector<double> pitches;
            for (std::size_t index = 1; index < row.size(); ++index) {
                pitches.push_back(row[index].box.x - row[index - 1].box.x);
            }
            layout.pitch_x = Median(std::move(pitches));
        }
        else {
            layout.pitch_x = profile.cell_size;
        }
        int x1 = std::numeric_limits<int>::max();
        int x2 = std::numeric_limits<int>::min();
        int y1 = std::numeric_limits<int>::max();
        int y2 = std::numeric_limits<int>::min();
        for (const auto& candidate : row) {
            const int x = candidate.box.x + (candidate.box.width - profile.cell_size) / 2;
            const int y = row_top;
            const cv::Rect cell(x, y, profile.cell_size, profile.cell_size);
            if ((cell & cv::Rect(roi.x, roi.y, roi.width, roi.height)) != cell) {
                continue;
            }
            // 内部每行保留独立 layout 以表达不同横向起点；下游 row 按纵向顺序全局编号，column 每行重新计数。
            const int column = static_cast<int>(layout.cells.size());
            layout.cells.push_back({ layout.grid_index, layout.grid_index, column, cell });
            x1 = std::min(x1, cell.x);
            x2 = std::max(x2, cell.x + cell.width);
            y1 = std::min(y1, cell.y);
            y2 = std::max(y2, cell.y + cell.height);
        }
        if (!layout.cells.empty()) {
            layout.columns = static_cast<int>(layout.cells.size());
            layout.bounds = cv::Rect(x1, y1, x2 - x1, y2 - y1);
            result.cells.insert(result.cells.end(), layout.cells.begin(), layout.cells.end());
            result.grids.push_back(std::move(layout));
        }
    }
    if (result.cells.empty()) {
        throw std::runtime_error("rewards ROI contains no formal cells");
    }
    return result;
}

double CardVerticalPhaseScore(const cv::Mat& gray, int phase_y, double pitch_y, int cell_size, const std::vector<int>& x_starts)
{
    const int inset = std::max(kCardMinimumInset, cvRound(cell_size * kCardInsetRatio));
    const int band = std::max(kCardMinimumBand, cvRound(cell_size * kCardBandRatio));
    std::vector<double> scores;
    for (int row = 0; row < 8; ++row) {
        const int y = cvRound(phase_y + row * pitch_y);
        if (y - band < 0 || y + cell_size + band > gray.rows) {
            continue;
        }
        for (int x : x_starts) {
            const int x1 = std::max(0, x + inset);
            const int x2 = std::min(gray.cols, x + cell_size - inset);
            if (x2 <= x1) {
                continue;
            }
            const auto mean = [&](int top, int bottom) {
                return cv::mean(gray(cv::Rect(x1, top, x2 - x1, bottom - top)))[0];
            };
            const double inside_top = mean(y + 2, y + 2 + band);
            const double outside_top = mean(y - band, y);
            const double inside_bottom = mean(y + cell_size - band - 2, y + cell_size - 2);
            const double outside_bottom = mean(y + cell_size, y + cell_size + band);
            scores.push_back(std::max(inside_top - outside_top, 0.0) + std::max(inside_bottom - outside_bottom, 0.0));
        }
    }
    return Median(std::move(scores));
}

void RefineCardVerticalPhase(const cv::Mat& image, const cv::Rect& roi, GridType type, GridLayout& layout)
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
    std::vector<int> x_starts;
    for (const auto& cell : layout.cells) {
        if (cell.row == 0) {
            x_starts.push_back(cell.cell_box.x);
        }
    }
    const int current_y = layout.cells.front().cell_box.y;
    const double current_pitch = layout.pitch_y;
    const double current_score = CardVerticalPhaseScore(gray, current_y, current_pitch, layout.cell_size, x_starts);
    const GridProfile profile = ProfileFor(type);
    const double pitch_min = std::max(profile.pitch_y - kCardProfilePitchRadius, current_pitch - kCardCurrentPitchRadius);
    const double pitch_max = std::min(profile.pitch_y + kCardProfilePitchRadius, current_pitch + kCardCurrentPitchRadius);
    const int phase_stop = std::min(roi.y + roi.height, roi.y + cvRound(current_pitch));
    std::tuple<double, double, double, int, double> best { -1.0, 0.0, 0.0, current_y, current_pitch };
    for (int phase_y = roi.y; phase_y < phase_stop; ++phase_y) {
        for (double pitch = pitch_min; pitch <= pitch_max + kPitchLoopEpsilon; pitch += kPitchRefinementStep) {
            const auto candidate = std::tuple {
                CardVerticalPhaseScore(gray, phase_y, pitch, layout.cell_size, x_starts),
                -std::abs(pitch - current_pitch),
                -std::abs(phase_y - current_y),
                phase_y,
                pitch,
            };
            if (candidate > best) {
                best = candidate;
            }
        }
    }
    const auto [best_score, ignored_pitch, ignored_phase, best_y, best_pitch] = best;
    if (best_score < current_score + kCardMinimumAbsoluteGain || best_score < current_score * kCardMinimumRelativeGain) {
        return;
    }
    std::vector<int> y_starts;
    for (int row = 0; row < 16; ++row) {
        const int y = cvRound(best_y + row * best_pitch);
        if (y >= roi.y + roi.height) {
            break;
        }
        if (IsFormal(cv::Rect(x_starts.front(), y, layout.cell_size, layout.cell_size), roi)) {
            y_starts.push_back(y);
        }
    }
    if (static_cast<int>(y_starts.size()) < profile.min_rows) {
        return;
    }
    layout.cells.clear();
    for (int row = 0; row < static_cast<int>(y_starts.size()); ++row) {
        for (int column = 0; column < static_cast<int>(x_starts.size()); ++column) {
            layout.cells.push_back({ 0, row, column, cv::Rect(x_starts[column], y_starts[row], layout.cell_size, layout.cell_size) });
        }
    }
    layout.pitch_y = best_pitch;
    layout.rows = static_cast<int>(y_starts.size());
    layout.bounds = cv::Rect(
        x_starts.front(),
        y_starts.front(),
        x_starts.back() + layout.cell_size - x_starts.front(),
        y_starts.back() + layout.cell_size - y_starts.front());
}

GridLayout BuildCreditTradeLattice(const cv::Rect& roi, int x_phase, int y_phase, const GridProfile& profile)
{
    const int pitch_x = cvRound(profile.pitch_x);
    const int pitch_y = cvRound(profile.pitch_y);

    GridLayout layout;
    layout.grid_index = 0;
    layout.cell_size = profile.cell_size;
    layout.pitch_x = profile.pitch_x;
    layout.pitch_y = profile.pitch_y;
    layout.columns = kCreditTradeColumns;
    for (int row = 0; row < 8; ++row) {
        const int y = y_phase + row * pitch_y + kCreditTradeCellOffsetY;
        if (y >= roi.y + roi.height) {
            break;
        }
        bool kept_row = false;
        for (int column = 0; column < kCreditTradeColumns; ++column) {
            const cv::Rect cell(x_phase + column * pitch_x + kCreditTradeCellOffsetX, y, profile.cell_size, profile.cell_size);
            if (IsFormal(cell, roi)) {
                layout.cells.push_back({ 0, layout.rows, column, cell });
                kept_row = true;
            }
        }
        layout.rows += kept_row ? 1 : 0;
    }
    if (layout.cells.empty()) {
        throw std::runtime_error("credit_trade ROI contains no formal cells");
    }

    int x1 = std::numeric_limits<int>::max();
    int y1 = std::numeric_limits<int>::max();
    int x2 = std::numeric_limits<int>::min();
    int y2 = std::numeric_limits<int>::min();
    for (const auto& cell : layout.cells) {
        x1 = std::min(x1, cell.cell_box.x), y1 = std::min(y1, cell.cell_box.y), x2 = std::max(x2, cell.cell_box.x + profile.cell_size),
        y2 = std::max(y2, cell.cell_box.y + profile.cell_size);
    }
    layout.bounds = cv::Rect(x1, y1, x2 - x1, y2 - y1);
    return layout;
}

GridLayout DetectCreditTrade(const cv::Mat& image, const cv::Rect& roi)
{
    const GridProfile profile = ProfileFor(GridType::CreditTrade);
    const int pitch_x = cvRound(profile.pitch_x);
    const int pitch_y = cvRound(profile.pitch_y);
    cv::Mat crop = image(roi);
    cv::Mat bgr;
    if (crop.channels() == 4) {
        cv::cvtColor(crop, bgr, cv::COLOR_BGRA2BGR);
    }
    else {
        bgr = crop;
    }
    cv::Mat hsv;
    cv::cvtColor(bgr, hsv, cv::COLOR_BGR2HSV);
    cv::Mat bright;
    cv::inRange(hsv, kCreditTradeCardHsvLower, kCreditTradeCardHsvUpper, bright);
    cv::Mat labels;
    cv::Mat stats;
    cv::Mat centroids;
    const int count = cv::connectedComponentsWithStats(bright, labels, stats, centroids, kCreditTradeConnectivity);
    std::vector<cv::Point> cards;
    for (int index = 1; index < count; ++index) {
        const int width = stats.at<int>(index, cv::CC_STAT_WIDTH);
        const int height = stats.at<int>(index, cv::CC_STAT_HEIGHT);
        const int area = stats.at<int>(index, cv::CC_STAT_AREA);
        if (width >= kCreditTradeCardMinimumWidth && width <= kCreditTradeCardMaximumWidth && height >= kCreditTradeCardMinimumHeight
            && height <= kCreditTradeCardMaximumHeight && area >= kCreditTradeCardMinimumArea) {
            cards.emplace_back(stats.at<int>(index, cv::CC_STAT_LEFT) + roi.x, stats.at<int>(index, cv::CC_STAT_TOP) + roi.y);
        }
    }
    if (cards.size() < kCreditTradeMinimumCardCount) {
        return DetectSingleLattice(image, GridType::CreditTrade, roi);
    }
    std::vector<double> x_phases;
    std::vector<double> y_phases;
    for (const auto& card : cards) {
        x_phases.push_back(card.x - std::nearbyint(static_cast<double>(card.x - roi.x) / pitch_x) * pitch_x);
        y_phases.push_back(card.y - std::nearbyint(static_cast<double>(card.y - roi.y) / pitch_y) * pitch_y);
    }
    const int x_phase = cvRound(Median(std::move(x_phases)));
    const int y_phase = cvRound(Median(std::move(y_phases)));
    if (cards.size() < kCreditTradeSparseCardCount) {
        const double maximum_residual = kCreditTradeMaximumPhaseResidualRatio * std::min(pitch_x, pitch_y);
        const bool coherent = std::ranges::all_of(cards, [&](const auto& card) {
            const int column = cvRound(static_cast<double>(card.x - x_phase) / pitch_x);
            const int row = cvRound(static_cast<double>(card.y - y_phase) / pitch_y);
            return std::abs(card.x - (x_phase + column * pitch_x)) <= maximum_residual
                   && std::abs(card.y - (y_phase + row * pitch_y)) <= maximum_residual;
        });
        if (coherent) {
            return BuildCreditTradeLattice(roi, x_phase, y_phase, profile);
        }
        return DetectSingleLattice(image, GridType::CreditTrade, roi);
    }
    std::vector<std::pair<int, int>> observed;
    for (const auto& card : cards) {
        const int row = cvRound(static_cast<double>(card.y - y_phase) / pitch_y);
        const int column = cvRound(static_cast<double>(card.x - x_phase) / pitch_x);
        if (row >= 0 && column >= 0 && column < kCreditTradeColumns) {
            observed.emplace_back(row, column);
        }
    }
    std::ranges::sort(observed);
    observed.erase(std::unique(observed.begin(), observed.end()), observed.end());
    if (observed.empty()) {
        throw std::runtime_error("credit_trade cards do not form a lattice");
    }
    const int first_row = observed.front().first;
    const int first_row_count =
        static_cast<int>(std::ranges::count_if(observed, [&](const auto& item) { return item.first == first_row; }));
    if (first_row_count >= kCreditTradeFirstRowCompletionCount) {
        for (int column = 0; column < kCreditTradeColumns; ++column) {
            observed.emplace_back(first_row, column);
        }
    }
    std::ranges::sort(observed);
    observed.erase(std::unique(observed.begin(), observed.end()), observed.end());
    std::vector<int> rows;
    for (const auto& [row, column] : observed) {
        rows.push_back(row);
    }
    std::ranges::sort(rows);
    rows.erase(std::unique(rows.begin(), rows.end()), rows.end());

    GridLayout layout;
    layout.grid_index = 0;
    layout.cell_size = profile.cell_size;
    layout.pitch_x = profile.pitch_x;
    layout.pitch_y = profile.pitch_y;
    layout.columns = kCreditTradeColumns;
    layout.rows = static_cast<int>(rows.size());
    for (const auto& [raw_row, column] : observed) {
        const int row = static_cast<int>(std::ranges::lower_bound(rows, raw_row) - rows.begin());
        const cv::Rect cell(
            x_phase + column * pitch_x + kCreditTradeCellOffsetX,
            y_phase + raw_row * pitch_y + kCreditTradeCellOffsetY,
            profile.cell_size,
            profile.cell_size);
        if (IsFormal(cell, roi)) {
            layout.cells.push_back({ 0, row, column, cell });
        }
    }
    if (layout.cells.empty()) {
        throw std::runtime_error("credit_trade ROI contains no formal cells");
    }
    int x1 = std::numeric_limits<int>::max();
    int y1 = std::numeric_limits<int>::max();
    int x2 = std::numeric_limits<int>::min();
    int y2 = std::numeric_limits<int>::min();
    for (const auto& cell : layout.cells) {
        x1 = std::min(x1, cell.cell_box.x), y1 = std::min(y1, cell.cell_box.y), x2 = std::max(x2, cell.cell_box.x + profile.cell_size),
        y2 = std::max(y2, cell.cell_box.y + profile.cell_size);
    }
    layout.bounds = cv::Rect(x1, y1, x2 - x1, y2 - y1);
    return layout;
}

double SampleSignal(const std::vector<float>& signal, double position)
{
    if (position < 0.0 || position > signal.size() - 1) {
        return 0.0;
    }
    const int left = static_cast<int>(std::floor(position));
    const int right = std::min(left + 1, static_cast<int>(signal.size()) - 1);
    const double fraction = position - left;
    return (1.0 - fraction) * signal[left] + fraction * signal[right];
}

int BoundaryCenter(const std::vector<float>& boundary, int position)
{
    const int left = std::max(0, position - 4);
    const int right = std::min(static_cast<int>(boundary.size()), position + 5);
    if (right <= left) {
        return position;
    }
    float maximum = 0.0F;
    for (int index = left; index < right; ++index) {
        maximum = std::max(maximum, boundary[index]);
    }
    if (maximum <= kEpsilon) {
        return position;
    }
    double sum = 0.0;
    int count = 0;
    for (int index = left; index < right; ++index) {
        if (boundary[index] >= maximum * kBoundaryCenterPlateauRatio) {
            sum += index, ++count;
        }
    }
    return count ? static_cast<int>(std::floor(sum / count + 0.5)) : position;
}

std::vector<int> RefineFirstBoundary(const std::vector<int>& starts, const std::vector<float>& boundary, int offset, int cell_size)
{
    if (starts.empty()) {
        throw std::runtime_error("transfer coarse lattice has no axis start");
    }
    const int first = starts.front() - offset;
    std::vector<double> deltas {
        static_cast<double>(BoundaryCenter(boundary, first) - first),
        static_cast<double>(BoundaryCenter(boundary, first + cell_size) - (first + cell_size)),
    };
    const int shift =
        std::clamp(static_cast<int>(std::floor(Median(std::move(deltas)) + 0.5)), -kFirstBoundaryMaximumShift, kFirstBoundaryMaximumShift);
    std::vector<int> result = starts;
    for (int& value : result) {
        value += shift;
    }
    return result;
}

std::vector<int> RefineStructuralPhase(
    const std::vector<int>& starts,
    const std::vector<float>& boundary,
    int offset,
    int cell_size,
    int maximum_shift = kDefaultStructuralPhaseMaximumShift,
    double minimum_gain = kDefaultStructuralPhaseMinimumGain,
    bool allow_small_shift = false)
{
    if (starts.empty()) {
        throw std::runtime_error("transfer coarse lattice has no vertical start");
    }
    const auto normalized = NormalizeSignal(boundary);
    if (*std::ranges::max_element(normalized) <= kEpsilon) {
        return starts;
    }
    std::vector<int> local_starts;
    for (int value : starts) {
        local_starts.push_back(value - offset);
    }
    const auto score = [&](int shift) {
        std::vector<double> pairs;
        for (int start : local_starts) {
            const int shifted = start + shift;
            const int end = shifted + cell_size;
            if (shifted < 0 || end >= static_cast<int>(normalized.size())) {
                continue;
            }
            pairs.push_back(std::sqrt(std::max(SampleSignal(normalized, shifted), 0.0) * std::max(SampleSignal(normalized, end), 0.0)));
        }
        return pairs.empty() ? 0.0 : std::accumulate(pairs.begin(), pairs.end(), 0.0) / pairs.size();
    };
    const double current = score(0);
    std::tuple<double, int, int> best { -1.0, std::numeric_limits<int>::min(), 0 };
    for (int shift = -maximum_shift; shift <= maximum_shift; ++shift) {
        best = std::max(best, std::tuple { score(shift), -std::abs(shift), shift });
    }
    const auto [best_score, ignored, best_shift] = best;
    if ((!allow_small_shift && std::abs(best_shift) <= kIgnoredStructuralPhaseShift) || best_score < kMinimumStructuralPhaseResponse
        || best_score < current + minimum_gain) {
        return starts;
    }
    std::vector<int> result = starts;
    for (int& value : result) {
        value += best_shift;
    }
    return result;
}

struct TransferAxisFit
{
    std::vector<int> starts;
    double phase = 0.0;
    double pitch = 0.0;
    double score = 0.0;
    double mean_residual = 0.0;
};

TransferAxisFit FitTransferAxis(
    const std::vector<int>& starts,
    const std::vector<float>& boundary,
    int offset,
    int cell_size,
    std::pair<double, double> pitch_range,
    int observed_pitch_tolerance,
    int maximum_count,
    bool refine_phase)
{
    std::vector<int> ordered = starts;
    std::ranges::sort(ordered);
    ordered.erase(std::unique(ordered.begin(), ordered.end()), ordered.end());
    if (ordered.empty() || boundary.empty()) {
        throw std::runtime_error("transfer axis fit has no evidence");
    }
    std::vector<double> observed;
    for (int value : ordered) {
        observed.push_back(value - offset);
    }
    std::vector<double> valid_spacings;
    for (std::size_t index = 1; index < observed.size(); ++index) {
        const double spacing = observed[index] - observed[index - 1];
        if (spacing >= pitch_range.first - observed_pitch_tolerance && spacing <= pitch_range.second + observed_pitch_tolerance) {
            valid_spacings.push_back(spacing);
        }
    }
    double coarse_pitch = valid_spacings.empty() ? 0.5 * (pitch_range.first + pitch_range.second) : Median(valid_spacings);
    coarse_pitch = std::clamp(coarse_pitch, pitch_range.first, pitch_range.second);
    std::vector<int> indices;
    for (double value : observed) {
        indices.push_back(static_cast<int>(std::nearbyint((value - observed.front()) / coarse_pitch)));
    }
    for (std::size_t index = 1; index < indices.size(); ++index) {
        indices[index] = std::max(indices[index], indices[index - 1]);
    }
    if (std::set<int>(indices.begin(), indices.end()).size() != indices.size()) {
        std::iota(indices.begin(), indices.end(), 0);
    }
    const int count = std::min(indices.back() + 1, maximum_count);
    const double mean_index = std::accumulate(indices.begin(), indices.end(), 0.0) / indices.size();
    const double mean_observed = std::accumulate(observed.begin(), observed.end(), 0.0) / observed.size();
    double denominator = 0.0;
    double numerator = 0.0;
    for (std::size_t index = 0; index < indices.size(); ++index) {
        denominator += (indices[index] - mean_index) * (indices[index] - mean_index);
        numerator += (indices[index] - mean_index) * (observed[index] - mean_observed);
    }
    double fitted_pitch = ordered.size() >= 6 || denominator <= kEpsilon ? coarse_pitch : numerator / denominator;
    fitted_pitch = std::clamp(fitted_pitch, pitch_range.first, pitch_range.second);
    double phase_center = 0.0;
    for (std::size_t index = 0; index < observed.size(); ++index) {
        phase_center += observed[index] - indices[index] * fitted_pitch;
    }
    phase_center /= observed.size();
    const auto normalized = NormalizeSignal(boundary);
    std::tuple<double, double, double> best { -std::numeric_limits<double>::infinity(), 0.0, 0.0 };
    double best_phase = phase_center;
    double best_score = 0.0;
    double best_residual = 0.0;
    const double phase_begin = refine_phase ? phase_center - kAxisPhaseRefinementHalfRange : phase_center;
    const double phase_end =
        refine_phase ? phase_center + kAxisPhaseRefinementHalfRange + kAxisPhaseLoopEpsilon : phase_center + kAxisPhaseLoopEpsilon;
    for (double phase = phase_begin; phase <= phase_end; phase += refine_phase ? kAxisPhaseRefinementStep : kAxisPhaseCoarseStep) {
        std::vector<double> pairs;
        for (int index = 0; index < count; ++index) {
            const double position = phase + index * fitted_pitch;
            pairs.push_back(std::sqrt(
                std::max(SampleSignal(normalized, position), 0.0) * std::max(SampleSignal(normalized, position + cell_size), 0.0)));
        }
        std::vector<double> residuals;
        for (std::size_t index = 0; index < observed.size(); ++index) {
            if (indices[index] < count) {
                residuals.push_back(std::abs(phase + indices[index] * fitted_pitch - observed[index]));
            }
        }
        const double residual = residuals.empty() ? 0.0 : std::accumulate(residuals.begin(), residuals.end(), 0.0) / residuals.size();
        const double evidence = pairs.empty() ? 0.0 : std::accumulate(pairs.begin(), pairs.end(), 0.0) / pairs.size();
        const double score = evidence - kTransferAxisResidualPenalty * residual;
        const auto candidate = std::tuple { score, -residual, -std::abs(phase - phase_center) };
        if (candidate > best) {
            best = candidate, best_phase = phase, best_score = score, best_residual = residual;
        }
    }
    TransferAxisFit fit { .phase = best_phase + offset, .pitch = fitted_pitch, .score = best_score, .mean_residual = best_residual };
    for (int index = 0; index < count; ++index) {
        fit.starts.push_back(static_cast<int>(std::floor(best_phase + index * fitted_pitch + 0.5)) + offset);
    }
    return fit;
}

std::vector<int> CompleteAxis(
    const std::vector<int>& starts,
    int maximum_count,
    std::optional<int> fixed_pitch,
    int preferred_pitch,
    int pitch_min,
    int pitch_max,
    int observed_pitch_tolerance,
    bool fit_phase)
{
    std::vector<int> ordered = starts;
    std::ranges::sort(ordered);
    ordered.erase(std::unique(ordered.begin(), ordered.end()), ordered.end());
    if (ordered.empty()) {
        throw std::runtime_error("transfer coarse lattice has no axis start");
    }
    std::vector<double> valid;
    for (std::size_t index = 1; index < ordered.size(); ++index) {
        const int spacing = ordered[index] - ordered[index - 1];
        if (spacing >= pitch_min - observed_pitch_tolerance && spacing <= pitch_max) {
            valid.push_back(spacing);
        }
    }
    const int pitch = std::clamp(
        fixed_pitch.value_or(valid.empty() ? preferred_pitch : static_cast<int>(std::floor(Median(valid) + 0.5))),
        pitch_min,
        pitch_max);
    if (fit_phase && ordered.size() > 1) {
        std::vector<int> indices;
        std::vector<double> phases;
        for (int value : ordered) {
            const int index = cvRound(static_cast<double>(value - ordered.front()) / pitch);
            indices.push_back(index);
            phases.push_back(value - index * pitch);
        }
        const int phase = static_cast<int>(std::floor(Median(std::move(phases)) + 0.5));
        std::vector<int> completed;
        for (int index = 0; index < std::min(indices.back() + 1, maximum_count); ++index) {
            completed.push_back(phase + index * pitch);
        }
        return completed;
    }
    std::vector<int> completed { ordered.front() };
    for (std::size_t index = 1; index < ordered.size(); ++index) {
        const int expected = completed.back() + pitch;
        completed.push_back(std::abs(ordered[index] - expected) <= 2 ? ordered[index] : expected);
    }
    return completed;
}

std::vector<int>
    RefinePortY(const std::vector<int>& starts, const std::vector<float>& boundary, int offset, int column_count, int cell_size)
{
    if (column_count != 4) {
        return RefineFirstBoundary(starts, boundary, offset, cell_size);
    }
    std::vector<std::pair<int, float>> evidence;
    for (int value : starts) {
        const int position = value - offset + cell_size;
        const int center = BoundaryCenter(boundary, position);
        const int left = std::max(0, position - 4);
        const int right = std::min(static_cast<int>(boundary.size()), position + 5);
        float score = 0.0F;
        for (int index = left; index < right; ++index) {
            score = std::max(score, boundary[index]);
        }
        if (score > 0.0F) {
            evidence.emplace_back(center - position, score);
        }
    }
    if (!evidence.empty()) {
        const float maximum = std::ranges::max_element(evidence, {}, &std::pair<int, float>::second)->second;
        std::vector<double> reliable;
        for (const auto& [delta, score] : evidence) {
            if (score >= maximum * kPortBoundaryMinimumRelativeScore) {
                reliable.push_back(delta);
            }
        }
        if (reliable.size() >= 2
            && *std::ranges::max_element(reliable) - *std::ranges::min_element(reliable) <= kPortBoundaryMaximumDelta) {
            const int shift = std::clamp(
                static_cast<int>(std::floor(Median(std::move(reliable)) + 0.5)),
                -kPortBoundaryMaximumShift,
                kPortBoundaryMaximumShift);
            std::vector<int> result = starts;
            for (int& value : result) {
                value += shift;
            }
            return result;
        }
    }
    return RefineFirstBoundary(starts, boundary, offset, cell_size);
}

double CellSupport(const cv::Mat& score, int x, int y)
{
    // cell 起点附近取最大结构响应的搜索半径；调大提高错位容忍度，也更易采到相邻纹理。
    constexpr int kSupportRadius = 2;
    const int x1 = std::max(0, x - kSupportRadius);
    const int x2 = std::min(score.cols, x + kSupportRadius + 1);
    const int y1 = std::max(0, y - kSupportRadius);
    const int y2 = std::min(score.rows, y + kSupportRadius + 1);
    if (x2 <= x1 || y2 <= y1) {
        return 0.0;
    }
    double maximum = 0.0;
    cv::minMaxLoc(score(cv::Rect(x1, y1, x2 - x1, y2 - y1)), nullptr, &maximum);
    return maximum;
}

int AlignedTrustedStrips(
    const TrustedRarityGridFit& fit,
    const std::vector<int>& x_starts,
    const std::vector<int>& y_starts,
    const TransferGridProfile& profile)
{
    const auto nearest = [](int position, const std::vector<int>& starts) {
        int residual = std::numeric_limits<int>::max();
        for (int start : starts) {
            residual = std::min(residual, std::abs(position - start));
        }
        return residual;
    };
    int aligned = 0;
    for (const auto& strip : fit.strips) {
        const int cell_top = strip.box.y + strip.box.height - profile.rarity_anchor_offset;
        if (nearest(strip.box.x, x_starts) <= profile.phase_tolerance && nearest(cell_top, y_starts) <= profile.phase_tolerance) {
            ++aligned;
        }
    }
    return aligned;
}

double NormalizedStructureSupport(const cv::Mat& score, const std::vector<int>& x_starts, const std::vector<int>& y_starts)
{
    if (score.empty() || x_starts.empty() || y_starts.empty()) {
        return 0.0;
    }
    double maximum = 0.0;
    cv::minMaxLoc(score, nullptr, &maximum);
    if (maximum <= kEpsilon) {
        return 0.0;
    }
    double total = 0.0;
    for (int y : y_starts) {
        for (int x : x_starts) {
            total += CellSupport(score, x, y) / maximum;
        }
    }
    return total / static_cast<double>(x_starts.size() * y_starts.size());
}

std::vector<int> DropPortRows(
    const cv::Mat& image,
    const cv::Rect& roi,
    const std::vector<int>& x_starts,
    std::vector<int> y_starts,
    int column_count,
    int cell_size)
{
    // 七列端口面板第二行需达到的最低结构支持，避免用弱第二行判断首行为空。
    constexpr double kSecondRowMinimumSupport = 0.20;
    // 首行结构支持低于第二行该比例时移除首行；调高更容易删除弱首行。
    constexpr double kFirstToSecondSupportRatio = 0.50;
    // 四列端口面板末行被视为空行的最大结构支持；调高更容易删除末行。
    constexpr double kLastRowMaximumSupport = 0.08;
    // cell 内用于比较上下纹理的分割高度比例，45/64 对应 64px cell 的 45px 分界。
    constexpr double kTextureSplitRatio = 45.0 / 64.0;
    // 删除末行所需的上下区域最小灰度落差；调高更保守，调低可能删除暗物品行。
    constexpr double kMinimumTextureDrop = 5.0;
    if (y_starts.size() < 2 || x_starts.empty()) {
        return y_starts;
    }
    const cv::Mat crop = image(roi);
    const cv::Mat score = BuildTransferCellScore(crop, cell_size);
    const auto row_support = [&](int y) {
        double total = 0.0;
        for (int x : x_starts) {
            total += CellSupport(score, x, y);
        }
        return total / x_starts.size();
    };
    if (column_count == 7 && row_support(y_starts[1]) >= kSecondRowMinimumSupport
        && row_support(y_starts[0]) < row_support(y_starts[1]) * kFirstToSecondSupportRatio) {
        y_starts.erase(y_starts.begin());
    }
    if (column_count != 4 || y_starts.size() < 2) {
        return y_starts;
    }
    const int y = y_starts.back();
    const double support = row_support(y);
    const int x1 = *std::ranges::min_element(x_starts);
    const int x2 = std::min(crop.cols, *std::ranges::max_element(x_starts) + cell_size);
    cv::Mat bgr;
    if (crop.channels() == 4) {
        cv::cvtColor(crop, bgr, cv::COLOR_BGRA2BGR);
    }
    else {
        bgr = crop;
    }
    cv::Mat gray;
    cv::cvtColor(bgr, gray, cv::COLOR_BGR2GRAY);
    const auto standard_deviation = [&](int top, int bottom) {
        const int clipped_top = std::clamp(top, 0, gray.rows);
        const int clipped_bottom = std::clamp(bottom, 0, gray.rows);
        if (clipped_bottom <= clipped_top || x2 <= x1) {
            return 0.0;
        }
        cv::Scalar mean;
        cv::Scalar deviation;
        cv::meanStdDev(gray(cv::Rect(x1, clipped_top, x2 - x1, clipped_bottom - clipped_top)), mean, deviation);
        return deviation[0];
    };
    const int texture_split = cvRound(cell_size * kTextureSplitRatio);
    const double texture_drop = standard_deviation(y, y + texture_split) - standard_deviation(y + texture_split, y + cell_size);
    if (support < kLastRowMaximumSupport && texture_drop > kMinimumTextureDrop) {
        y_starts.pop_back();
    }
    return y_starts;
}

GridLayout BuildTransferLayout(const cv::Mat& image, const cv::Rect& roi, const TransferGridHint& hint, int grid_index, GridType type)
{
    const bool transfer = type == GridType::Transfer;
    const int absolute_center = roi.x + hint.rect.x + hint.rect.width / 2;
    const bool left_side = absolute_center < image.cols / 2;
    const TransferGridVariant variant = transfer
                                            ? (left_side ? TransferGridVariant::TransferLeft : TransferGridVariant::TransferRight)
                                            : (left_side ? TransferGridVariant::PortStoragerLeft : TransferGridVariant::PortStoragerRight);
    const TransferGridProfile profile = TransferProfileFor(variant);
    const cv::Rect absolute_region(roi.x + hint.region.x, roi.y + hint.region.y, hint.region.width, hint.region.height);
    const StructureMaps maps = BuildStructureMaps(image(absolute_region), profile.cell_size);
    const auto boundary_x = RobustProjection(maps.vertical, true);
    const auto boundary_y = RobustProjection(maps.horizontal, false);
    const int column_count = static_cast<int>(hint.x_starts.size());
    const auto trusted_fit = FitTrustedRarityGrid(image(roi), hint.region, profile);
    const auto refined_x = RefineFirstBoundary(hint.x_starts, boundary_x, hint.region.x, profile.cell_size);
    const TransferAxisFit x_fit = FitTransferAxis(
        refined_x,
        boundary_x,
        hint.region.x,
        profile.cell_size,
        { static_cast<double>(profile.pitch_min), static_cast<double>(profile.pitch_max) },
        profile.observed_pitch_tolerance,
        static_cast<int>(refined_x.size()),
        !transfer);
    std::vector<int> local_x = x_fit.starts;
    std::vector<int> local_y;
    const auto rarity_fit = FitRarityGrid(image(roi), local_x, hint.y_starts, profile);
    if (rarity_fit) {
        local_x = rarity_fit->x_starts;
        const int count = std::min(profile.maximum_rows, std::max(static_cast<int>(hint.y_starts.size()), rarity_fit->supporting_rows));
        for (int row = 0; row < count; ++row) {
            local_y.push_back(rarity_fit->origin + row * rarity_fit->pitch);
        }
    }
    else {
        auto structural_y = transfer ? RefineStructuralPhase(hint.y_starts, boundary_y, hint.region.y, profile.cell_size) : hint.y_starts;
        auto refined_y = structural_y != hint.y_starts
                             ? structural_y
                             : RefinePortY(hint.y_starts, boundary_y, hint.region.y, column_count, profile.cell_size);
        local_y = CompleteAxis(
            refined_y,
            profile.maximum_rows,
            transfer ? std::optional<int>(static_cast<int>(std::floor(x_fit.pitch + 0.5))) : std::nullopt,
            profile.preferred_pitch,
            profile.pitch_min,
            profile.pitch_max,
            profile.observed_pitch_tolerance,
            column_count == 4 || column_count == 7);
        if (transfer && column_count >= 7) {
            local_y = RefineStructuralPhase(
                local_y,
                boundary_y,
                hint.region.y,
                profile.cell_size,
                kWideTransferPhaseMaximumShift,
                kWideTransferPhaseMinimumGain,
                true);
        }
    }
    const cv::Mat cell_score = BuildTransferCellScore(image(roi), profile.cell_size);
    bool trusted_selected = false;
    double trusted_candidate_score = 0.0;
    const double legacy_structure = NormalizedStructureSupport(cell_score, local_x, local_y);
    double legacy_rarity = 0.0;
    std::vector<std::string> rejected_reasons;
    if (trusted_fit) {
        legacy_rarity = static_cast<double>(AlignedTrustedStrips(*trusted_fit, local_x, local_y, profile)) / trusted_fit->supporting_cells;
        const double trusted_structure = NormalizedStructureSupport(cell_score, trusted_fit->x_starts, trusted_fit->y_starts);
        const double trusted_consistency = 0.5 * (trusted_fit->x_axis.confidence + trusted_fit->y_axis.confidence);
        trusted_candidate_score = kTrustedStructureWeight * trusted_structure + kTrustedConfidenceWeight * trusted_fit->mean_confidence
                                  + kTrustedConsistencyWeight * trusted_consistency;
        if (legacy_rarity == 0.0
            && (trusted_fit->supporting_cells >= kMinimumTrustedCellsWithoutLegacySupport
                || trusted_structure >= kMinimumTrustedStructureWithoutLegacySupport)) {
            local_x = trusted_fit->x_starts;
            local_y = trusted_fit->y_starts;
            trusted_selected = true;
        }
        else if (legacy_rarity > 0.0) {
            rejected_reasons.emplace_back("trusted-evidence-already-explained");
        }
        else {
            rejected_reasons.emplace_back("trusted-candidate-lacks-structure");
        }
    }
    else {
        rejected_reasons.emplace_back("no-trusted-chromatic-strip");
    }
    const double legacy_candidate_score = kLegacyStructureWeight * legacy_structure + kLegacyRarityWeight * legacy_rarity;
    if (local_y.size() < static_cast<std::size_t>(profile.maximum_rows)) {
        const auto row_support = [&](int y) {
            double total = 0.0;
            for (int x : local_x) {
                total += CellSupport(cell_score, x, y);
            }
            return local_x.empty() ? 0.0 : total / local_x.size();
        };
        double existing_support = 0.0;
        for (int y : local_y) {
            existing_support = std::max(existing_support, row_support(y));
        }
        const double minimum_support = existing_support * kRowCompletionSupportRatio;
        std::vector<double> spacings;
        for (std::size_t index = 1; index < local_y.size(); ++index) {
            spacings.push_back(local_y[index] - local_y[index - 1]);
        }
        const int pitch_y = spacings.empty() ? profile.preferred_pitch : static_cast<int>(std::floor(Median(spacings) + 0.5));
        const std::size_t completion_limit = trusted_selected && trusted_fit->y_axis.direct_indices.size() == 1
                                                 ? std::min<std::size_t>(kSingleObservationCompletionLimit, profile.maximum_rows)
                                                 : static_cast<std::size_t>(profile.maximum_rows);
        while (local_y.size() < completion_limit) {
            const int following = local_y.back() + std::clamp(pitch_y, profile.pitch_min, profile.pitch_max);
            if (hint.region.y + hint.region.height - following < profile.minimum_bottom_visibility * profile.cell_size) {
                break;
            }
            const bool stable_rarity_lattice =
                (rarity_fit && rarity_fit->supporting_rows >= static_cast<int>(kStableRarityMinimumRows))
                || (trusted_selected && trusted_fit->y_axis.direct_indices.size() >= kStableRarityMinimumRows);
            if (!stable_rarity_lattice && (minimum_support <= 0.0 || row_support(following) < minimum_support)) {
                break;
            }
            local_y.push_back(following);
        }
    }
    if (!transfer && !rarity_fit && !trusted_selected) {
        local_y = DropPortRows(image, roi, local_x, local_y, column_count, profile.cell_size);
    }

    const auto fit_final_axis = [&](const std::vector<int>& starts, int maximum_count) {
        std::vector<LatticeObservation> observations;
        observations.reserve(starts.size());
        for (int start : starts) {
            observations.push_back({ static_cast<double>(start), 1.0, true });
        }
        return FitRegularAxis(
            observations,
            maximum_count,
            { static_cast<double>(profile.pitch_min), static_cast<double>(profile.pitch_max) },
            profile.preferred_pitch);
    };
    const auto final_x_axis = fit_final_axis(local_x, std::max(1, static_cast<int>(local_x.size())));
    const auto final_y_axis = fit_final_axis(local_y, profile.maximum_rows);
    if (!final_x_axis || !final_y_axis) {
        throw std::runtime_error("transfer final lattice does not fit one global origin and pitch");
    }
    local_x = ProjectRegularAxis(*final_x_axis);
    local_y = ProjectRegularAxis(*final_y_axis);

    GridLayout layout;
    layout.grid_index = grid_index;
    layout.cell_size = profile.cell_size;
    for (int row = 0; row < static_cast<int>(local_y.size()); ++row) {
        for (int column = 0; column < static_cast<int>(local_x.size()); ++column) {
            const cv::Rect cell(roi.x + local_x[column], roi.y + local_y[row], profile.cell_size, profile.cell_size);
            if (IsFormal(cell, roi, profile.minimum_top_visibility, profile.minimum_bottom_visibility)) {
                layout.cells.push_back({ grid_index, row, column, cell });
            }
        }
    }
    if (layout.cells.empty()) {
        throw std::runtime_error("transfer hint contains no formal cells");
    }
    layout.pitch_x = final_x_axis->pitch;
    layout.pitch_y = final_y_axis->pitch;
    layout.columns = static_cast<int>(local_x.size());
    layout.rows = static_cast<int>(local_y.size());
    int x1 = std::numeric_limits<int>::max();
    int y1 = std::numeric_limits<int>::max();
    int x2 = std::numeric_limits<int>::min();
    int y2 = std::numeric_limits<int>::min();
    for (const auto& cell : layout.cells) {
        x1 = std::min(x1, cell.cell_box.x), y1 = std::min(y1, cell.cell_box.y), x2 = std::max(x2, cell.cell_box.x + layout.cell_size),
        y2 = std::max(y2, cell.cell_box.y + layout.cell_size);
    }
    layout.bounds = cv::Rect(x1, y1, x2 - x1, y2 - y1);
    const double final_structure = NormalizedStructureSupport(cell_score, local_x, local_y);
    const double final_rarity = trusted_fit ? static_cast<double>(AlignedTrustedStrips(*trusted_fit, local_x, local_y, profile))
                                                  / trusted_fit->supporting_cells * trusted_fit->mean_confidence
                                            : 0.0;
    const double maximum_residual = std::max(final_x_axis->maximum_residual, final_y_axis->maximum_residual);
    const double consistency = std::clamp(1.0 - maximum_residual / kMaximumRegularAxisResidual, 0.0, 1.0);
    const double selected_score = trusted_selected ? trusted_candidate_score : legacy_candidate_score;
    const double other_score = trusted_selected ? legacy_candidate_score : trusted_candidate_score;
    layout.selection_diagnostics = GridSelectionDiagnostics {
        .origin = cv::Point2d(roi.x + final_x_axis->origin, roi.y + final_y_axis->origin),
        .pitch = cv::Point2d(final_x_axis->pitch, final_y_axis->pitch),
        .rows = layout.rows,
        .columns = layout.columns,
        .best_score = selected_score,
        .second_score = other_score,
        .score_margin = selected_score - other_score,
        .structure_score = final_structure,
        .rarity_score = final_rarity,
        .consistency_score = consistency,
        .maximum_residual = maximum_residual,
        .residual_trend = std::max(std::abs(final_x_axis->residual_trend), std::abs(final_y_axis->residual_trend)),
        .trusted_rarity_cells = trusted_fit ? trusted_fit->rarity_counts : std::array<int, 6> {},
        .fallback_used = !trusted_selected,
        .fallback_reason = trusted_selected ? "" : "legacy-structure-without-conflicting-trusted-rarity",
        .rejected_reasons = std::move(rejected_reasons),
    };
    return layout;
}

void Append(GridDetection& result, GridLayout layout)
{
    result.cells.insert(result.cells.end(), layout.cells.begin(), layout.cells.end());
    result.grids.push_back(std::move(layout));
}

} // namespace

GridDetection DetectGrid(const cv::Mat& image, GridType type, const cv::Rect& roi)
{
    if (image.empty()) {
        throw std::invalid_argument("cannot detect grid in empty image");
    }
    const cv::Rect bounds(0, 0, image.cols, image.rows);
    if ((roi & bounds) != roi || roi.width <= 0 || roi.height <= 0) {
        throw std::invalid_argument("grid ROI is outside image");
    }
    GridDetection result { type, roi, {}, {} };
    if (type == GridType::Rewards) {
        return DetectRewardsGrid(image, roi);
    }
    if (type == GridType::CreditTrade) {
        Append(result, DetectCreditTrade(image, roi));
    }
    else if (type == GridType::Transfer || type == GridType::PortStorager) {
        const auto hints = DiscoverTransferGridHints(image(roi), type == GridType::Transfer);
        for (int index = 0; index < static_cast<int>(hints.size()); ++index) {
            Append(result, BuildTransferLayout(image, roi, hints[index], index, type));
        }
    }
    else {
        GridLayout layout = DetectSingleLattice(image, type, roi);
        if (type == GridType::Trade || type == GridType::Valuables || type == GridType::Shipment) {
            RefineCardVerticalPhase(image, roi, type, layout);
        }
        Append(result, std::move(layout));
    }
    return result;
}

} // namespace iconrecognition::detail
