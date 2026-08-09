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

constexpr int kSearchRadius = 8;
constexpr double kLabDistance = 25.0;
constexpr double kMinCoverage = 0.8;
const std::array<cv::Vec3f, 6> kPrototypes {
    cv::Vec3f(163.0F, 128.0F, 128.0F), cv::Vec3f(198.0F, 98.0F, 191.0F),  cv::Vec3f(182.0F, 113.0F, 86.0F),
    cv::Vec3f(129.0F, 189.0F, 55.0F),  cv::Vec3f(204.0F, 136.0F, 202.0F), cv::Vec3f(163.0F, 167.0F, 191.0F),
};

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

RarityRowEvidence MeasureRarityRow(const cv::Mat& lab_row)
{
    if (lab_row.empty() || lab_row.type() != CV_32FC3 || lab_row.rows != 1) {
        return {};
    }
    double maximum_coverage = 0.0;
    double chromatic_coverage = 0.0;
    for (std::size_t index = 0; index < kPrototypes.size(); ++index) {
        const auto& prototype = kPrototypes[index];
        int covered = 0;
        for (int column = 0; column < lab_row.cols; ++column) {
            if (cv::norm(lab_row.at<cv::Vec3f>(0, column) - prototype) <= kLabDistance) {
                ++covered;
            }
        }
        const double coverage = static_cast<double>(covered) / lab_row.cols;
        maximum_coverage = std::max(maximum_coverage, coverage);
        if (index > 0) {
            chromatic_coverage = std::max(chromatic_coverage, coverage);
        }
    }
    return { maximum_coverage, chromatic_coverage };
}

double RarityRowCoverage(const cv::Mat& lab_row)
{
    return MeasureRarityRow(lab_row).coverage;
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
    candidates.reserve(kPrototypes.size());
    for (int index = 0; index < static_cast<int>(kPrototypes.size()); ++index) {
        Candidate best { .coverage = -1.0, .rarity = index + 1 };
        for (int row = 0; row < lab.rows; ++row) {
            int covered = 0;
            for (int column = 0; column < lab.cols; ++column) {
                const cv::Vec3f delta = lab.at<cv::Vec3f>(row, column) - kPrototypes[index];
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
