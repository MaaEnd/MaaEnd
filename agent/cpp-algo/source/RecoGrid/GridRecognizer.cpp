#include "GridRecognizer.h"

#include "GridGeometry.h"

#include <algorithm>
#include <cmath>
#include <cstring>
#include <stdexcept>
#include <utility>

#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>

namespace fs = std::filesystem;

namespace recogrid
{
namespace
{

template <typename T>
void ReadField(const json::object& object, const char* key, T& out)
{
    if (!object.contains(key)) {
        return;
    }

    const json::value& value = object.at(key);
    if constexpr (std::is_same_v<T, bool>) {
        if (value.is_boolean()) {
            out = value.as_boolean();
        }
    }
    else if constexpr (std::is_same_v<T, int>) {
        if (value.is_number()) {
            out = value.as_integer();
        }
    }
    else if constexpr (std::is_same_v<T, double>) {
        if (value.is_number()) {
            out = value.as_double();
        }
    }
    else if constexpr (std::is_same_v<T, std::string>) {
        if (value.is_string()) {
            out = value.as_string();
        }
    }
}

void ReadMaskField(const json::object& object, const char* key, CellMaskRatios& out)
{
    if (!object.contains(key) || !object.at(key).is_object()) {
        return;
    }

    const json::object& mask = object.at(key).as_object();
    ReadField(mask, "left_header_width", out.leftHeaderWidth);
    ReadField(mask, "left_header_height", out.leftHeaderHeight);
    ReadField(mask, "right_header_width", out.rightHeaderWidth);
    ReadField(mask, "right_header_height", out.rightHeaderHeight);
    ReadField(mask, "bottom_height", out.bottomHeight);
}

std::vector<int> ReadIntArray(const json::object& object, const char* key)
{
    if (!object.contains(key) || !object.at(key).is_array()) {
        return {};
    }

    std::vector<int> values;
    for (const auto& item : object.at(key).as_array()) {
        if (!item.is_number()) {
            return {};
        }
        values.push_back(item.as_integer());
    }
    return values;
}

std::vector<std::string> ReadStringArray(const json::object& object, const char* key)
{
    if (!object.contains(key) || !object.at(key).is_array()) {
        return {};
    }

    std::vector<std::string> values;
    for (const auto& item : object.at(key).as_array()) {
        if (!item.is_string()) {
            return {};
        }
        values.push_back(item.as_string());
    }
    return values;
}

bool ApplyRect(const std::vector<int>& values, cv::Rect& rect)
{
    if (values.size() != 4 || values[2] <= 0 || values[3] <= 0) {
        return false;
    }
    rect = { values[0], values[1], values[2], values[3] };
    return true;
}

bool ApplySize(const std::vector<int>& values, cv::Size& size)
{
    if (values.size() != 2 || values[0] <= 0 || values[1] <= 0) {
        return false;
    }
    size = { values[0], values[1] };
    return true;
}

void ClampOptions(GridRecognitionOptions& options)
{
    options.detect.rowThresholdRatio = std::clamp(options.detect.rowThresholdRatio, 0.0, 1.0);
    options.detect.colThresholdRatio = std::clamp(options.detect.colThresholdRatio, 0.0, 1.0);
    options.detect.minRawSegmentLength = std::max(1, options.detect.minRawSegmentLength);
    options.detect.minKeptSegmentRatio = std::clamp(options.detect.minKeptSegmentRatio, 0.0, 1.0);
    options.detect.lockedRowHeight = std::max(0, options.detect.lockedRowHeight);
    options.detect.lockedColWidth = std::max(0, options.detect.lockedColWidth);
    options.detect.lockedSegmentTolerance = std::clamp(options.detect.lockedSegmentTolerance, 0.0, 1.0);
    options.maxPhashDistance = std::max(0, options.maxPhashDistance);
    options.minScore = std::clamp(options.minScore, 0.0, 1.0);
    options.hueWeight = std::clamp(options.hueWeight, 0.0, 1.0);
    options.maxRankedCandidates = std::max(0, options.maxRankedCandidates);
    options.maxReturnedCells = std::max(0, options.maxReturnedCells);
    options.maxReturnedMatches = std::max(0, options.maxReturnedMatches);
}

void ClampOptions(GridClassifyOptions& options)
{
    options.maxPhashDistance = std::max(0, options.maxPhashDistance);
    options.minScore = std::clamp(options.minScore, 0.0, 1.0);
    options.hueWeight = std::clamp(options.hueWeight, 0.0, 1.0);
    options.maxRankedCandidates = std::max(0, options.maxRankedCandidates);
}

cv::Rect UnionRects(const std::vector<cv::Rect>& rects)
{
    cv::Rect bounds;
    for (const auto& rect : rects) {
        if (rect.empty()) {
            continue;
        }
        bounds = bounds.empty() ? rect : (bounds | rect);
    }
    return bounds;
}

void FillScreenGeometry(GridRecognitionResult& result, const GridRecognitionOptions& options, cv::Size imageSize)
{
    result.screenRoi = ScaleRect(options.detect.roi, options.detect.normalizedSize, imageSize);
    result.screenGrid = RoiToScreen(UnionRects(result.grid.cells), options.detect, imageSize);

    if (!options.collectCells) {
        return;
    }

    const int limit = std::clamp(options.maxReturnedCells, 0, static_cast<int>(result.grid.cells.size()));
    result.screenCells.reserve(static_cast<std::size_t>(limit));
    for (int i = 0; i < limit; ++i) {
        result.screenCells.push_back(RoiToScreen(result.grid.cells[static_cast<std::size_t>(i)], options.detect, imageSize));
    }
}

GridRecognitionResult Detect(const cv::Mat& image, const GridRecognitionOptions& options)
{
    if (image.empty()) {
        throw std::invalid_argument("Cannot recognize grid from an empty image");
    }

    GridRecognitionResult result;
    result.grid = DetectGrid(image, options.detect);
    result.cellHashes = ComputeCellHashes(result.grid.roi, result.grid.cells, options.mask);
    result.cellFeatures = ComputeCellFeatures(result.grid.roi, result.grid.cells, options.mask);
    FillScreenGeometry(result, options, image.size());

    if (result.grid.cells.empty()) {
        result.message = "Grid detected no cells";
        return result;
    }

    result.matched = true;
    result.message = "Grid detected";
    return result;
}

} // namespace

GridRecognitionRequest ParseGridRecognitionRequest(const char* raw)
{
    GridRecognitionRequest request;
    if (raw == nullptr || std::strlen(raw) == 0) {
        return request;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        throw std::invalid_argument("custom_recognition_param must be a JSON object");
    }
    if (!request.from_json(*parsed)) {
        throw std::invalid_argument("custom_recognition_param cannot be converted to GridRecognitionRequest");
    }
    return request;
}

GridRecognitionRequest ApplyRoiOverride(const GridRecognitionRequest& request, const cv::Rect& roi)
{
    GridRecognitionRequest output = request;
    if (roi.width > 0 && roi.height > 0) {
        output.options.detect.roi = roi;
    }
    return output;
}

bool GridRecognitionRequest::from_json(const json::value& value)
{
    if (!value.is_object()) {
        return false;
    }

    const json::object& object = value.as_object();
    if (object.contains("options") && object.at("options").is_object()) {
        GridRecognitionRequest nested;
        nested.options = options;
        nested.classify = classify;
        nested.templatePath = templatePath;
        nested.templatePaths = templatePaths;
        if (nested.from_json(object.at("options"))) {
            options = nested.options;
            classify = nested.classify;
            templatePath = nested.templatePath;
            templatePaths = nested.templatePaths;
        }
    }

    ReadField(object, "template_path", templatePath);
    std::vector<std::string> configuredTemplatePaths = ReadStringArray(object, "template_paths");
    if (!configuredTemplatePaths.empty()) {
        templatePaths = std::move(configuredTemplatePaths);
    }
    if (!templatePath.empty()
        && (templatePaths.empty() || std::find(templatePaths.begin(), templatePaths.end(), templatePath) == templatePaths.end())) {
        templatePaths.insert(templatePaths.begin(), templatePath);
    }
    if (templatePath.empty() && !templatePaths.empty()) {
        templatePath = templatePaths.front();
    }
    ApplyRect(ReadIntArray(object, "roi"), options.detect.roi);
    ApplySize(ReadIntArray(object, "normalized_size"), options.detect.normalizedSize);
    ReadField(object, "row_threshold_ratio", options.detect.rowThresholdRatio);
    ReadField(object, "col_threshold_ratio", options.detect.colThresholdRatio);
    ReadField(object, "min_raw_segment_length", options.detect.minRawSegmentLength);
    ReadField(object, "min_kept_segment_ratio", options.detect.minKeptSegmentRatio);
    ReadField(object, "locked_row_height", options.detect.lockedRowHeight);
    ReadField(object, "locked_col_width", options.detect.lockedColWidth);
    ReadField(object, "locked_segment_tolerance", options.detect.lockedSegmentTolerance);
    ReadMaskField(object, "mask_ratios", options.mask);
    ReadMaskField(object, "mask", options.mask);
    ReadField(object, "max_phash_distance", options.maxPhashDistance);
    ReadField(object, "min_score", options.minScore);
    ReadField(object, "hue_weight", options.hueWeight);
    ReadField(object, "max_ranked_candidates", options.maxRankedCandidates);
    ReadField(object, "return_cells", options.collectCells);
    ReadField(object, "max_returned_cells", options.maxReturnedCells);
    ReadField(object, "max_returned_matches", options.maxReturnedMatches);

    ClampOptions(options);
    classify.maxPhashDistance = options.maxPhashDistance;
    classify.minScore = options.minScore;
    classify.hueWeight = options.hueWeight;
    classify.maxRankedCandidates = options.maxRankedCandidates;
    ClampOptions(classify);
    return true;
}

fs::path ResolveTemplatePath(const std::string& templatePath)
{
    if (templatePath.empty()) {
        return {};
    }

    const fs::path configured(templatePath);
    if (configured.is_absolute() && fs::exists(configured)) {
        return configured;
    }

    std::error_code ec;
    const fs::path cwd = fs::current_path(ec);
    if (!ec && !cwd.empty()) {
        for (const fs::path& base : { cwd, cwd / "assets" / "resource" / "image", cwd / "resource" / "image" }) {
            const fs::path candidate = base / configured;
            if (fs::exists(candidate)) {
                return candidate;
            }
        }
    }
    return configured;
}

cv::Mat LoadTemplate(const std::string& templatePath)
{
    if (templatePath.empty()) {
        return {};
    }

    const fs::path resolved = ResolveTemplatePath(templatePath);
    cv::Mat target = MAA_NS::imread(resolved, cv::IMREAD_UNCHANGED);
    if (target.empty()) {
        LogError << "RecoGrid: failed to load template" << VAR(templatePath) << VAR(resolved.string());
    }
    return target;
}

GridRecognitionResult RecognizeGrid(const cv::Mat& image, const GridRecognitionOptions& options)
{
    return Detect(image, options);
}

GridTemplateMatchResult MatchGridTemplate(
    const GridRecognitionResult& result,
    const cv::Mat& target,
    const GridRecognitionOptions& options,
    cv::Size imageSize,
    int maxRankedCandidates)
{
    if (target.empty()) {
        throw std::invalid_argument("Cannot match an empty template");
    }

    GridTemplateMatchResult output;
    if (result.grid.cells.empty()) {
        return output;
    }

    output.candidates = FilterCandidates(result.grid.roi, result.grid.cells, target, std::max(0, options.maxPhashDistance), options.mask);
    output.candidateCount = static_cast<int>(output.candidates.size());

    std::vector<Candidate> rankedCandidates = output.candidates;
    if (maxRankedCandidates > 0 && static_cast<int>(rankedCandidates.size()) > maxRankedCandidates) {
        rankedCandidates.resize(static_cast<std::size_t>(maxRankedCandidates));
    }

    const std::vector<TemplateMatchResult> ranked = RankTemplateMatches(
        result.grid.roi,
        target,
        rankedCandidates,
        TemplateMatchOptions { options.mask, std::clamp(options.hueWeight, 0.0, 1.0) });
    output.rankedCount = static_cast<int>(ranked.size());

    const int limit = std::clamp(options.maxReturnedMatches, 0, static_cast<int>(ranked.size()));
    output.matches.reserve(static_cast<std::size_t>(limit));
    for (int i = 0; i < limit; ++i) {
        const auto& match = ranked[static_cast<std::size_t>(i)];
        const bool rejected = match.match.empty() || !std::isfinite(match.score) || match.score < options.minScore;
        if (i == 0) {
            output.bestRejected = rejected;
        }
        if (rejected) {
            continue;
        }

        output.matches.push_back({
            match.cellIndex,
            match.cell,
            RoiToScreen(match.cell, options.detect, imageSize),
            match.match,
            RoiToScreen(match.match, options.detect, imageSize),
            match.phashDistance,
            match.score,
            match.templateScore,
            match.hueScore,
        });
    }
    return output;
}

GridRecognitionResult RecognizeGridTemplate(const cv::Mat& image, const cv::Mat& target, const GridRecognitionOptions& options)
{
    if (target.empty()) {
        throw std::invalid_argument("Cannot recognize grid template from an empty target");
    }

    GridRecognitionResult result = Detect(image, options);
    if (result.grid.cells.empty()) {
        return result;
    }

    GridTemplateMatchResult templateResult = MatchGridTemplate(result, target, options, image.size());
    result.candidates = std::move(templateResult.candidates);
    result.matches = std::move(templateResult.matches);

    if (result.matches.empty()) {
        result.matched = false;
        result.message = "Template produced no match";
        return result;
    }

    result.matched = true;
    result.message = "Template matched";
    return result;
}

GridRecognitionResult RecognizeGridRequest(const cv::Mat& image, const GridRecognitionRequest& request)
{
    if (request.templatePath.empty()) {
        return RecognizeGrid(image, request.options);
    }

    const cv::Mat target = LoadTemplate(request.templatePath);
    if (target.empty()) {
        throw std::invalid_argument("Template image is empty or cannot be loaded");
    }
    return RecognizeGridTemplate(image, target, request.options);
}

} // namespace recogrid
