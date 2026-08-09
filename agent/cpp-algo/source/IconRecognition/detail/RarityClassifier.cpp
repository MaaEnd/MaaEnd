#include "RarityClassifier.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <tuple>
#include <vector>

namespace iconrecognition::detail
{

namespace
{

// 在 cell 预期底边附近搜索 rarity 色带的纵向像素半径；调大提高错位容忍度，也更易采到背景。
constexpr int kSearchRadius = 8;
// 像素与 rarity 原型色的最大 Lab 距离；调大提高颜色召回，调小减少相近背景误判。
constexpr double kLabDistance = 25.0;
// 最佳色带行需达到的原型色覆盖率；调高提高可靠性，调低可适应压缩或光照偏色。
constexpr double kMinCoverage = 0.8;

struct Candidate
{
    double coverage = 0.0;
    int rarity = 0;
    int row_offset = 0;
};

bool candidate_less(const Candidate& left, const Candidate& right)
{
    return std::tie(left.coverage, left.rarity, left.row_offset) < std::tie(right.coverage, right.rarity, right.row_offset);
}

} // namespace

const std::array<cv::Vec3f, 6>& RarityLabPrototypes()
{
    // 六个 Lab 原型按数组下标依次对应 rarity 1..6，数值来自 720p 色带回归样本的代表色。
    static const std::array<cv::Vec3f, 6> prototypes {
        // rarity 1 灰色色带的 Lab 原型。
        cv::Vec3f(163.0F, 128.0F, 128.0F),
        // rarity 2 色带的 Lab 原型。
        cv::Vec3f(198.0F, 98.0F, 191.0F),
        // rarity 3 色带的 Lab 原型。
        cv::Vec3f(182.0F, 113.0F, 86.0F),
        // rarity 4 色带的 Lab 原型。
        cv::Vec3f(129.0F, 189.0F, 55.0F),
        // rarity 5 色带的 Lab 原型。
        cv::Vec3f(204.0F, 136.0F, 202.0F),
        // rarity 6 色带的 Lab 原型。
        cv::Vec3f(163.0F, 167.0F, 191.0F),
    };
    return prototypes;
}

double RarityRowEvidence::maximumCoverage() const
{
    return *std::ranges::max_element(coverages);
}

double RarityRowEvidence::maximumChromaticCoverage() const
{
    return *std::ranges::max_element(coverages.begin() + 1, coverages.end());
}

RarityRowEvidence MeasureRarityRow(const cv::Mat& lab_row)
{
    if (lab_row.empty() || lab_row.type() != CV_32FC3 || lab_row.rows != 1) {
        return {};
    }
    RarityRowEvidence evidence;
    const auto& prototypes = RarityLabPrototypes();
    for (std::size_t index = 0; index < prototypes.size(); ++index) {
        const auto& prototype = prototypes[index];
        int covered = 0;
        for (int column = 0; column < lab_row.cols; ++column) {
            if (cv::norm(lab_row.at<cv::Vec3f>(0, column) - prototype) <= kLabDistance) {
                ++covered;
            }
        }
        evidence.coverages[index] = static_cast<double>(covered) / lab_row.cols;
    }
    return evidence;
}

double RarityRowCoverage(const cv::Mat& lab_row)
{
    return MeasureRarityRow(lab_row).maximumCoverage();
}

RarityResult ClassifyRarity(const cv::Mat& image, const cv::Rect& slot)
{
    if (image.empty() || image.channels() < 3) {
        return {};
    }
    const int slot_bottom = slot.y + slot.height;
    const int y1 = std::max(0, slot_bottom - kSearchRadius);
    const int y2 = std::min(image.rows, slot_bottom + kSearchRadius + 1);
    const int x1 = std::max(0, slot.x);
    const int x2 = std::min(image.cols, slot.x + slot.width);
    if (y2 <= y1 || x2 <= x1) {
        return {};
    }

    cv::Mat bgr;
    const cv::Mat crop = image(cv::Rect(x1, y1, x2 - x1, y2 - y1));
    if (image.channels() == 4) {
        cv::cvtColor(crop, bgr, cv::COLOR_BGRA2BGR);
    }
    else {
        bgr = crop;
    }
    cv::Mat lab;
    cv::cvtColor(bgr, lab, cv::COLOR_BGR2Lab);
    lab.convertTo(lab, CV_32FC3);

    std::vector<Candidate> candidates;
    const auto& prototypes = RarityLabPrototypes();
    candidates.reserve(prototypes.size());
    for (int index = 0; index < static_cast<int>(prototypes.size()); ++index) {
        Candidate best { .coverage = -1.0, .rarity = index + 1 };
        for (int row = 0; row < lab.rows; ++row) {
            int covered = 0;
            for (int column = 0; column < lab.cols; ++column) {
                const cv::Vec3f delta = lab.at<cv::Vec3f>(row, column) - prototypes[index];
                if (cv::norm(delta) <= kLabDistance) {
                    ++covered;
                }
            }
            const double coverage = static_cast<double>(covered) / lab.cols;
            const Candidate current {
                .coverage = coverage,
                .rarity = index + 1,
                .row_offset = y1 + row - slot_bottom,
            };
            if (current.coverage > best.coverage) {
                best = current;
            }
        }
        candidates.push_back(best);
    }

    const auto reliable = [](const Candidate& candidate) {
        return candidate.coverage >= kMinCoverage;
    };
    const auto chromatic = [&](const Candidate& candidate) {
        return reliable(candidate) && candidate.rarity != 1;
    };
    auto selected = std::max_element(candidates.begin(), candidates.end(), candidate_less);
    const auto chromatic_selected =
        std::max_element(candidates.begin(), candidates.end(), [&](const Candidate& left, const Candidate& right) {
            if (chromatic(left) != chromatic(right)) {
                return !chromatic(left);
            }
            return candidate_less(left, right);
        });
    if (chromatic_selected != candidates.end() && chromatic(*chromatic_selected)) {
        selected = chromatic_selected;
    }
    else {
        const auto reliable_selected =
            std::max_element(candidates.begin(), candidates.end(), [&](const Candidate& left, const Candidate& right) {
                if (reliable(left) != reliable(right)) {
                    return !reliable(left);
                }
                return candidate_less(left, right);
            });
        if (reliable_selected != candidates.end() && reliable(*reliable_selected)) {
            selected = reliable_selected;
        }
    }
    if (selected == candidates.end()) {
        return {};
    }
    return RarityResult {
        .rarity = reliable(*selected) ? std::optional<int>(selected->rarity) : std::nullopt,
        .coverage = selected->coverage,
        .row_offset = selected->row_offset,
    };
}

} // namespace iconrecognition::detail
