#include "GridRecognizer.h"

#include "GridGeometry.h"

#include <algorithm>
#include <cmath>
#include <compare>
#include <cstdint>
#include <map>
#include <stdexcept>
#include <utility>

namespace recogrid
{
namespace
{

void ClampOptions(GridClassifyOptions& options)
{
    options.maxPhashDistance = std::max(0, options.maxPhashDistance);
    options.minScore = std::clamp(options.minScore, 0.0, 1.0);
    options.hueWeight = std::clamp(options.hueWeight, 0.0, 1.0);
    options.maxRankedCandidates = std::max(0, options.maxRankedCandidates);
}

struct DirectMaskKey
{
    int leftHeaderWidth = 0;
    int leftHeaderHeight = 0;
    int rightHeaderWidth = 0;
    int rightHeaderHeight = 0;
    int bottomHeight = 0;

    auto operator<=>(const DirectMaskKey&) const = default;
};

struct DirectTemplateKey
{
    std::uintptr_t data = 0;
    int rows = 0;
    int cols = 0;
    int type = 0;
    int width = 0;
    int height = 0;
    DirectMaskKey mask;

    auto operator<=>(const DirectTemplateKey&) const = default;
};

struct DirectTemplateEntry
{
    cv::Mat bgr;
    cv::Mat gray;
    cv::Mat mask;
    Hash hash = 0;
};

struct DirectCellEntry
{
    cv::Rect cell;
    cv::Mat bgr;
    cv::Mat gray;
};

DirectMaskKey MakeDirectMaskKey(const CellMaskRatios& ratios)
{
    const auto encode = [](double value) {
        return static_cast<int>(std::lround(value * 1'000'000.0));
    };
    return {
        encode(ratios.leftHeaderWidth),   encode(ratios.leftHeaderHeight), encode(ratios.rightHeaderWidth),
        encode(ratios.rightHeaderHeight), encode(ratios.bottomHeight),
    };
}

cv::Mat ToDirectBgr(const cv::Mat& image)
{
    if (image.channels() == 3) {
        return image.clone();
    }

    cv::Mat bgr;
    if (image.channels() == 4) {
        cv::cvtColor(image, bgr, cv::COLOR_BGRA2BGR);
    }
    else if (image.channels() == 1) {
        cv::cvtColor(image, bgr, cv::COLOR_GRAY2BGR);
    }
    else {
        throw std::invalid_argument("Unsupported image channel count for direct grid classification");
    }
    return bgr;
}

DirectTemplateEntry BuildDirectTemplateEntry(const cv::Mat& image, cv::Size size, const CellMaskRatios& maskRatios)
{
    DirectTemplateEntry entry;
    cv::Mat resized;
    const int interpolation = image.cols > size.width || image.rows > size.height ? cv::INTER_AREA : cv::INTER_CUBIC;
    cv::resize(image, resized, size, 0, 0, interpolation);

    cv::Mat visibleMask;
    if (resized.channels() == 4) {
        std::vector<cv::Mat> bgra;
        cv::split(resized, bgra);
        cv::threshold(bgra[3], visibleMask, 10, 255, cv::THRESH_BINARY);
    }

    entry.bgr = ToDirectBgr(resized);
    cv::cvtColor(entry.bgr, entry.gray, cv::COLOR_BGR2GRAY);

    entry.mask = BuildIgnoreMask(size, maskRatios);
    if (!visibleMask.empty()) {
        if (entry.mask.empty()) {
            entry.mask = visibleMask;
        }
        else {
            cv::bitwise_and(entry.mask, visibleMask, entry.mask);
        }
    }
    if (!entry.mask.empty()) {
        cv::threshold(entry.mask, entry.mask, 10, 255, cv::THRESH_BINARY);
    }

    entry.hash = ComputeHash(ApplyIgnoreMask(resized, maskRatios));
    return entry;
}

DirectTemplateEntry GetDirectTemplateEntry(const GridClassifyTemplate& entry, cv::Size size, const CellMaskRatios& maskRatios)
{
    static std::map<DirectTemplateKey, DirectTemplateEntry> cache;

    const DirectTemplateKey key {
        reinterpret_cast<std::uintptr_t>(entry.image.data),
        entry.image.rows,
        entry.image.cols,
        entry.image.type(),
        size.width,
        size.height,
        MakeDirectMaskKey(maskRatios),
    };

    const auto iter = cache.find(key);
    if (iter != cache.end()) {
        return iter->second;
    }

    DirectTemplateEntry direct = BuildDirectTemplateEntry(entry.image, size, maskRatios);
    auto [insertedIter, _] = cache.emplace(key, direct);
    return insertedIter->second;
}

double HueHistogramScoreDirect(const cv::Mat& source, const cv::Mat& target, const cv::Mat& mask)
{
    if (source.empty() || target.empty()) {
        return 0.0;
    }

    cv::Mat sourceHsv;
    cv::Mat targetHsv;
    cv::cvtColor(source, sourceHsv, cv::COLOR_BGR2HSV);
    cv::cvtColor(target, targetHsv, cv::COLOR_BGR2HSV);

    const int histSize[] = { 30 };
    const float hueRange[] = { 0.0F, 180.0F };
    const float* ranges[] = { hueRange };
    const int channels[] = { 0 };

    cv::Mat sourceHist;
    cv::Mat targetHist;
    cv::calcHist(&sourceHsv, 1, channels, mask, sourceHist, 1, histSize, ranges);
    cv::calcHist(&targetHsv, 1, channels, mask, targetHist, 1, histSize, ranges);

    if (cv::sum(sourceHist)[0] <= 0.0 || cv::sum(targetHist)[0] <= 0.0) {
        return 0.0;
    }

    cv::normalize(sourceHist, sourceHist, 1.0, 0.0, cv::NORM_L1);
    cv::normalize(targetHist, targetHist, 1.0, 0.0, cv::NORM_L1);
    return std::clamp(cv::compareHist(sourceHist, targetHist, cv::HISTCMP_CORREL), 0.0, 1.0);
}

TemplateMatchResult DirectTemplateMatch(const DirectCellEntry& cell, const DirectTemplateEntry& target, int phashDistance, double hueWeight)
{
    TemplateMatchResult result;
    result.cell = cell.cell;
    result.match = cell.cell;
    result.phashDistance = phashDistance;

    cv::Mat match;
    if (!target.mask.empty() && cv::countNonZero(target.mask) > 0) {
        cv::matchTemplate(cell.gray, target.gray, match, cv::TM_CCORR_NORMED, target.mask);
    }
    else {
        cv::matchTemplate(cell.gray, target.gray, match, cv::TM_CCORR_NORMED);
    }

    double templateScore = 0.0;
    cv::minMaxLoc(match, nullptr, &templateScore);
    if (!std::isfinite(templateScore)) {
        templateScore = 0.0;
    }

    const double clampedHueWeight = std::clamp(hueWeight, 0.0, 1.0);
    const double hueScore = clampedHueWeight > 0.0 ? HueHistogramScoreDirect(cell.bgr, target.bgr, target.mask) : 0.0;
    result.templateScore = std::clamp(templateScore, 0.0, 1.0);
    result.hueScore = hueScore;
    result.score = std::clamp((1.0 - clampedHueWeight) * result.templateScore + clampedHueWeight * hueScore, 0.0, 1.0);
    return result;
}

} // namespace

GridClassificationResult ClassifyGridCells(
    const GridRecognitionResult& result,
    const std::vector<GridClassifyTemplate>& templates,
    const GridRecognitionOptions& gridOptions,
    const GridClassifyOptions& classifyOptions,
    cv::Size imageSize,
    const std::vector<std::size_t>& cellIndices)
{
    GridClassifyOptions classify = classifyOptions;
    ClampOptions(classify);

    GridClassificationResult output;
    if (result.grid.cells.empty()) {
        return output;
    }

    std::vector<std::size_t> selectedIndices;
    if (cellIndices.empty()) {
        selectedIndices.reserve(result.grid.cells.size());
        for (std::size_t i = 0; i < result.grid.cells.size(); ++i) {
            selectedIndices.push_back(i);
        }
    }
    else {
        selectedIndices.reserve(cellIndices.size());
        for (const std::size_t index : cellIndices) {
            if (index < result.grid.cells.size()) {
                selectedIndices.push_back(index);
            }
        }
    }

    std::vector<cv::Rect> selectedCells;
    selectedCells.reserve(selectedIndices.size());
    std::vector<DirectCellEntry> directCells;
    directCells.reserve(selectedIndices.size());
    output.cells.reserve(selectedIndices.size());
    for (const std::size_t index : selectedIndices) {
        const cv::Rect& cell = result.grid.cells[index];
        selectedCells.push_back(cell);
        output.cells.push_back({
            index,
            RoiToScreen(cell, gridOptions.detect, imageSize),
            index < result.cellHashes.size() ? result.cellHashes[index] : Hash {},
        });

        DirectCellEntry directCell;
        directCell.cell = cell;
        const cv::Rect clipped = ClampRect(cell, result.grid.roi.size());
        if (!clipped.empty()) {
            directCell.bgr = ToDirectBgr(result.grid.roi(clipped));
            cv::cvtColor(directCell.bgr, directCell.gray, cv::COLOR_BGR2GRAY);
            directCell.cell = clipped;
        }
        directCells.push_back(std::move(directCell));
    }

    struct RankedTemplateCandidate
    {
        std::size_t templateIndex = 0;
        int distance = 0;
    };

    constexpr int kDefaultTopTemplatesPerCell = 5;
    const int topTemplatesPerCell = classify.maxRankedCandidates > 0 ? classify.maxRankedCandidates : kDefaultTopTemplatesPerCell;
    std::vector<std::vector<Candidate>> candidatesByTemplate(templates.size());
    for (std::size_t localCellIndex = 0; localCellIndex < selectedCells.size(); ++localCellIndex) {
        const cv::Rect clipped = ClampRect(selectedCells[localCellIndex], result.grid.roi.size());
        if (clipped.empty() || localCellIndex >= output.cells.size() || localCellIndex >= directCells.size()
            || directCells[localCellIndex].bgr.empty()) {
            continue;
        }

        std::vector<RankedTemplateCandidate> rankedTemplates;
        rankedTemplates.reserve(templates.size());
        const Hash cellHash = output.cells[localCellIndex].hash;
        for (std::size_t templateIndex = 0; templateIndex < templates.size(); ++templateIndex) {
            const GridClassifyTemplate& entry = templates[templateIndex];
            if (entry.id.empty() || entry.image.empty()) {
                continue;
            }

            const DirectTemplateEntry directTemplate = GetDirectTemplateEntry(entry, clipped.size(), gridOptions.mask);
            const Hash templateHash = directTemplate.hash;
            const int distance = HammingDistance(cellHash, templateHash);
            if (distance <= classify.maxPhashDistance) {
                rankedTemplates.push_back({ templateIndex, distance });
            }
        }

        std::sort(rankedTemplates.begin(), rankedTemplates.end(), [](const auto& lhs, const auto& rhs) {
            if (lhs.distance != rhs.distance) {
                return lhs.distance < rhs.distance;
            }
            return lhs.templateIndex < rhs.templateIndex;
        });
        if (static_cast<int>(rankedTemplates.size()) > topTemplatesPerCell) {
            rankedTemplates.resize(static_cast<std::size_t>(topTemplatesPerCell));
        }

        for (const RankedTemplateCandidate& ranked : rankedTemplates) {
            candidatesByTemplate[ranked.templateIndex].push_back(
                { localCellIndex, selectedCells[localCellIndex], cellHash, ranked.distance });
        }
    }

    for (std::size_t templateIndex = 0; templateIndex < templates.size(); ++templateIndex) {
        const GridClassifyTemplate& entry = templates[templateIndex];
        if (entry.id.empty() || entry.image.empty()) {
            continue;
        }
        ++output.templatesScanned;

        const std::vector<Candidate>& candidates = candidatesByTemplate[templateIndex];
        if (candidates.empty()) {
            continue;
        }
        output.candidatesAfterPhash += static_cast<int>(candidates.size());

        std::vector<TemplateMatchResult> ranked;
        ranked.reserve(candidates.size());
        for (const Candidate& candidate : candidates) {
            if (candidate.cellIndex >= directCells.size() || directCells[candidate.cellIndex].bgr.empty()) {
                continue;
            }
            const DirectTemplateEntry directTemplate =
                GetDirectTemplateEntry(entry, directCells[candidate.cellIndex].bgr.size(), gridOptions.mask);
            TemplateMatchResult match =
                DirectTemplateMatch(directCells[candidate.cellIndex], directTemplate, candidate.distance, classify.hueWeight);
            match.cellIndex = candidate.cellIndex;
            ranked.push_back(std::move(match));
        }
        output.matchesRanked += static_cast<int>(ranked.size());

        for (const TemplateMatchResult& match : ranked) {
            if (match.cellIndex >= output.cells.size() || match.match.empty() || !std::isfinite(match.score)
                || match.score < classify.minScore) {
                continue;
            }

            GridCellClassification& current = output.cells[match.cellIndex];
            const bool replace = !current.matched || match.score > current.score
                                 || (match.score == current.score && match.templateScore > current.templateScore)
                                 || (match.score == current.score && match.templateScore == current.templateScore
                                     && match.phashDistance < current.phashDistance)
                                 || (match.score == current.score && match.templateScore == current.templateScore
                                     && match.phashDistance == current.phashDistance && entry.id < current.templateId);
            if (!replace) {
                continue;
            }

            current.matched = true;
            current.templateId = entry.id;
            current.score = match.score;
            current.templateScore = match.templateScore;
            current.hueScore = match.hueScore;
            current.phashDistance = match.phashDistance;
        }
    }

    for (const GridCellClassification& cell : output.cells) {
        if (cell.matched) {
            ++output.matchedCells;
        }
        else {
            ++output.unmatchedCells;
        }
    }
    return output;
}

} // namespace recogrid
