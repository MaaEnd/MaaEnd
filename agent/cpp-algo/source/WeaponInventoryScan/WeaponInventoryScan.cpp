#include "WeaponInventoryScan.h"

#include "../RecoGrid/GridAlignment.h"
#include "../RecoGrid/GridRecognizer.h"
#include "../utils.h"

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/NoWarningCV.hpp>

#include <meojson/json.hpp>

#include <algorithm>
#include <cstdint>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <map>
#include <memory>
#include <set>
#include <sstream>
#include <stdexcept>
#include <string>
#include <utility>
#include <vector>

#ifndef MAA_TRUE
#define MAA_TRUE 1
#endif
#ifndef MAA_FALSE
#define MAA_FALSE 0
#endif

namespace fs = std::filesystem;

namespace weaponinventoryscan
{
namespace
{

constexpr const char* kDefaultOutputPath = "debug/record/WeaponInventoryScan.json";
constexpr int kDefaultMaxPhashDistance = 10;
constexpr double kDefaultMinScore = 0.80;
constexpr double kDefaultHueWeight = 0.40;
constexpr int kDefaultAlignmentMatchDistance = 12;
constexpr double kDefaultMinAlignmentMatchRatio = 0.60;

struct CellDetail
{
    int cellIndex = 0;
    std::vector<int> screenCell;
    std::string hash;
    bool matched = false;
    std::string weaponId;
    double score = 0.0;
    double templateScore = 0.0;
    double hueScore = 0.0;
    int phashDistance = 0;

    MEO_JSONIZATION(
        MEO_KEY("cell_index") cellIndex,
        MEO_KEY("screen_cell") screenCell,
        hash,
        matched,
        MEO_KEY("weapon_id") weaponId,
        score,
        MEO_KEY("template_score") templateScore,
        MEO_KEY("hue_score") hueScore,
        MEO_KEY("phash_distance") phashDistance)
};

struct RecognitionDetail
{
    int schemaVersion = 2;
    int status = 0;
    std::string message = "ok";
    int rows = 0;
    int cols = 0;
    int cellCount = 0;
    std::vector<std::string> cellHashes;
    std::vector<CellDetail> cells;
    int templatesScanned = 0;
    int candidatesAfterPhash = 0;
    int matchesRanked = 0;
    int matchedCells = 0;
    int unmatchedCells = 0;

    MEO_JSONIZATION(
        MEO_KEY("schema_version") schemaVersion,
        status,
        message,
        rows,
        cols,
        MEO_KEY("cell_count") cellCount,
        MEO_KEY("cell_hashes") cellHashes,
        cells,
        MEO_KEY("templates_scanned") templatesScanned,
        MEO_KEY("candidates_after_phash") candidatesAfterPhash,
        MEO_KEY("matches_ranked") matchesRanked,
        MEO_KEY("matched_cells") matchedCells,
        MEO_KEY("unmatched_cells") unmatchedCells)
};

struct PageOutput
{
    int pageIndex = 0;
    int rows = 0;
    int cols = 0;
    int visibleCells = 0;
    int newCells = 0;
    int recognizedCells = 0;
    int unknownCells = 0;
    int rowOffset = 0;
    int comparedCells = 0;
    int matchedCells = 0;
    double matchRatio = 0.0;
    double averageDistance = 0.0;
    std::string status = "ok";

    MEO_JSONIZATION(
        MEO_KEY("page_index") pageIndex,
        rows,
        cols,
        MEO_KEY("visible_cells") visibleCells,
        MEO_KEY("new_cells") newCells,
        MEO_KEY("recognized_cells") recognizedCells,
        MEO_KEY("unknown_cells") unknownCells,
        MEO_KEY("row_offset") rowOffset,
        MEO_KEY("compared_cells") comparedCells,
        MEO_KEY("matched_cells") matchedCells,
        MEO_KEY("match_ratio") matchRatio,
        MEO_KEY("average_distance") averageDistance,
        status)
};

struct CellOutput
{
    int globalIndex = 0;
    int pageIndex = 0;
    int cellIndex = 0;
    std::vector<int> screenCell;
    std::string hash;
    bool matched = false;
    std::string weaponId;
    double score = 0.0;
    double templateScore = 0.0;
    double hueScore = 0.0;
    int phashDistance = 0;

    MEO_JSONIZATION(
        MEO_KEY("global_index") globalIndex,
        MEO_KEY("page_index") pageIndex,
        MEO_KEY("cell_index") cellIndex,
        MEO_KEY("screen_cell") screenCell,
        hash,
        matched,
        MEO_KEY("weapon_id") weaponId,
        score,
        MEO_KEY("template_score") templateScore,
        MEO_KEY("hue_score") hueScore,
        MEO_KEY("phash_distance") phashDistance)
};

struct ScanOutput
{
    int schemaVersion = 2;
    std::string status = "ok";
    int totalCount = 0;
    int recognizedCount = 0;
    int unknownCount = 0;
    int weaponTypeCount = 0;
    std::map<std::string, int> weaponCounts;
    std::vector<PageOutput> pages;
    std::vector<CellOutput> cells;

    MEO_JSONIZATION(
        MEO_KEY("schema_version") schemaVersion,
        status,
        MEO_KEY("total_count") totalCount,
        MEO_KEY("recognized_count") recognizedCount,
        MEO_KEY("unknown_count") unknownCount,
        MEO_KEY("weapon_type_count") weaponTypeCount,
        MEO_KEY("weapon_counts") weaponCounts,
        pages,
        cells)
};

struct ScanOptions
{
    recogrid::GridRecognitionOptions grid;
    recogrid::GridClassifyOptions classify;
    recogrid::GridDeltaOptions delta;
    std::vector<std::string> templatePaths;
};

struct ScanState
{
    ScanOptions options;
    std::vector<recogrid::GridClassifyTemplate> templates;
    recogrid::GridHashSnapshot previousSnapshot;
    bool hasPreviousSnapshot = false;
    std::map<std::string, int> weaponCounts;
    std::vector<PageOutput> pages;
    std::vector<CellOutput> cells;
    int pageCount = 0;
    int totalCount = 0;
    int recognizedCount = 0;
    int unknownCount = 0;
    std::string status = "ok";
    std::string outputPath = kDefaultOutputPath;
};

struct ServiceParam
{
    std::string outputPath;
    int alignmentMatchDistance = kDefaultAlignmentMatchDistance;
    double minAlignmentMatchRatio = kDefaultMinAlignmentMatchRatio;

    MEO_JSONIZATION(
        MEO_OPT MEO_KEY("output_path") outputPath,
        MEO_OPT MEO_KEY("alignment_match_distance") alignmentMatchDistance,
        MEO_OPT MEO_KEY("min_alignment_match_ratio") minAlignmentMatchRatio)
};

std::shared_ptr<ScanState> g_state;

ServiceParam ParseServiceParam(const char* raw, const ServiceParam& defaults = {})
{
    ServiceParam param = defaults;
    if (raw == nullptr || std::strlen(raw) == 0) {
        return param;
    }

    const auto parsed = json::parse(raw);
    if (parsed && parsed->is_object()) {
        param.from_json(*parsed);
    }
    return param;
}

ScanOptions ParseScanOptions(const char* raw)
{
    ScanOptions options;
    options.grid.collectCells = true;
    options.grid.maxReturnedCells = 512;
    options.grid.maxPhashDistance = kDefaultMaxPhashDistance;
    options.grid.minScore = kDefaultMinScore;
    options.grid.hueWeight = kDefaultHueWeight;
    options.classify.maxPhashDistance = kDefaultMaxPhashDistance;
    options.classify.minScore = kDefaultMinScore;
    options.classify.hueWeight = kDefaultHueWeight;
    options.classify.maxRankedCandidates = 0;
    options.delta.matchDistanceThreshold = kDefaultAlignmentMatchDistance;
    options.delta.minMatchRatio = kDefaultMinAlignmentMatchRatio;
    options.templatePaths = {
        "data/WeaponIcon/iconbig",
        "assets/data/WeaponIcon/iconbig",
    };

    if (raw == nullptr || std::strlen(raw) == 0) {
        return options;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        throw std::invalid_argument("custom param must be a JSON object");
    }

    recogrid::GridRecognitionRequest request;
    request.options = options.grid;
    request.classify = options.classify;
    if (!request.from_json(*parsed)) {
        throw std::invalid_argument("custom param cannot be converted to GridRecognitionRequest");
    }
    options.grid = request.options;
    options.classify = request.classify;
    if (!request.templatePaths.empty()) {
        options.templatePaths = std::move(request.templatePaths);
    }

    const ServiceParam serviceParam = ParseServiceParam(raw);
    options.delta.matchDistanceThreshold = serviceParam.alignmentMatchDistance;
    options.delta.minMatchRatio = serviceParam.minAlignmentMatchRatio;
    options.delta.matchDistanceThreshold = std::max(0, options.delta.matchDistanceThreshold);
    options.delta.minMatchRatio = std::clamp(options.delta.minMatchRatio, 0.0, 1.0);
    return options;
}

void ApplyStateParams(ScanState& state, const char* raw)
{
    ServiceParam defaults;
    defaults.outputPath = state.outputPath;
    defaults.alignmentMatchDistance = state.options.delta.matchDistanceThreshold;
    defaults.minAlignmentMatchRatio = state.options.delta.minMatchRatio;

    const ServiceParam param = ParseServiceParam(raw, defaults);
    state.outputPath = param.outputPath.empty() ? state.outputPath : param.outputPath;
    state.options.delta.matchDistanceThreshold = std::max(0, param.alignmentMatchDistance);
    state.options.delta.minMatchRatio = std::clamp(param.minAlignmentMatchRatio, 0.0, 1.0);
}

fs::path ResolvePath(const std::string& configured)
{
    const fs::path path(configured);
    if (path.is_absolute() && fs::exists(path)) {
        return path;
    }

    std::error_code ec;
    const fs::path cwd = fs::current_path(ec);
    if (ec || cwd.empty()) {
        return path;
    }

    for (const fs::path& base : { cwd, cwd / "assets" }) {
        const fs::path candidate = base / path;
        if (fs::exists(candidate)) {
            return candidate;
        }
    }
    return path;
}

std::vector<recogrid::GridClassifyTemplate> LoadTemplateIndex(const ScanOptions& options)
{
    std::vector<recogrid::GridClassifyTemplate> templates;
    std::set<std::string> seen;

    for (const std::string& configured : options.templatePaths) {
        const fs::path dir = ResolvePath(configured);
        std::error_code ec;
        if (!fs::exists(dir, ec) || !fs::is_directory(dir, ec)) {
            continue;
        }

        for (const fs::directory_entry& entry : fs::directory_iterator(dir, ec)) {
            if (ec || !entry.is_regular_file()) {
                continue;
            }

            const fs::path path = entry.path();
            if (path.extension() != ".png") {
                continue;
            }

            const std::string id = path.stem().string();
            if (id.empty() || seen.contains(id)) {
                continue;
            }

            cv::Mat image = MAA_NS::imread(path, cv::IMREAD_UNCHANGED);
            if (image.empty()) {
                LogWarn << "WeaponInventoryScan: skip unreadable template" << VAR(path.string());
                continue;
            }

            seen.insert(id);
            templates.push_back({ id, std::move(image) });
        }
    }

    std::sort(templates.begin(), templates.end(), [](const auto& lhs, const auto& rhs) {
        return lhs.id < rhs.id;
    });
    return templates;
}

ScanState* GetState()
{
    return g_state.get();
}

void SetState(std::shared_ptr<ScanState> state)
{
    g_state = std::move(state);
}

std::shared_ptr<ScanState> TakeState()
{
    std::shared_ptr<ScanState> state = std::move(g_state);
    g_state.reset();
    return state;
}

std::string HashToHex(recogrid::Hash hash)
{
    std::ostringstream stream;
    stream << std::hex << std::setw(16) << std::setfill('0') << static_cast<std::uint64_t>(hash);
    return stream.str();
}

std::vector<int> RectToArray(const cv::Rect& rect)
{
    return { rect.x, rect.y, rect.width, rect.height };
}

template <typename T>
void WriteJsonDetail(MaaStringBuffer* outDetail, const T& payload)
{
    if (outDetail == nullptr) {
        return;
    }
    const std::string text = json::value(payload).dumps();
    MaaStringBufferSet(outDetail, text.c_str());
}

RecognitionDetail MakeErrorDetail(int status, std::string message)
{
    RecognitionDetail detail;
    detail.status = status;
    detail.message = std::move(message);
    return detail;
}

RecognitionDetail ExtractInnerRecognitionDetail(const std::string& raw)
{
    auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        throw std::invalid_argument("recognition detail is not a JSON object");
    }

    RecognitionDetail detail;
    if (detail.from_json(*parsed)) {
        return detail;
    }

    const json::object& object = parsed->as_object();
    if (object.contains("best") && object.at("best").is_object()) {
        const json::object& best = object.at("best").as_object();
        if (best.contains("detail")) {
            const json::value& wrapped = best.at("detail");
            if (wrapped.is_string()) {
                return ExtractInnerRecognitionDetail(wrapped.as_string());
            }
            if (wrapped.is_object() && detail.from_json(wrapped)) {
                return detail;
            }
        }
    }

    throw std::invalid_argument("recognition detail does not match WeaponInventoryScan schema");
}

std::string GetRecognitionDetailJson(MaaContext* context, MaaRecoId recoId)
{
    if (recoId == MaaInvalidId) {
        throw std::invalid_argument("invalid reco_id");
    }

    MaaTasker* tasker = MaaContextGetTasker(context);
    if (tasker == nullptr) {
        throw std::runtime_error("context has no tasker");
    }

    MaaStringBuffer* nodeName = MaaStringBufferCreate();
    MaaStringBuffer* algorithm = MaaStringBufferCreate();
    MaaStringBuffer* detailJson = MaaStringBufferCreate();
    MaaBool hit = MAA_FALSE;
    MaaRect box {};

    const MaaBool ok =
        MaaTaskerGetRecognitionDetail(tasker, recoId, nodeName, algorithm, &hit, &box, detailJson, nullptr, nullptr);

    std::string text;
    if (ok && hit) {
        const char* raw = MaaStringBufferGet(detailJson);
        if (raw != nullptr) {
            text = raw;
        }
    }

    MaaStringBufferDestroy(nodeName);
    MaaStringBufferDestroy(algorithm);
    MaaStringBufferDestroy(detailJson);

    if (!ok) {
        throw std::runtime_error("MaaTaskerGetRecognitionDetail failed");
    }
    if (!hit) {
        throw std::runtime_error("previous recognition did not hit");
    }
    if (text.empty()) {
        throw std::runtime_error("previous recognition detail is empty");
    }
    return text;
}

std::vector<recogrid::Hash> ParseHashes(const RecognitionDetail& detail)
{
    std::vector<recogrid::Hash> hashes;
    hashes.reserve(detail.cellHashes.size());
    for (const std::string& text : detail.cellHashes) {
        std::uint64_t value = 0;
        std::stringstream stream(text);
        stream >> std::hex >> value;
        hashes.push_back(static_cast<recogrid::Hash>(value));
    }
    return hashes;
}

recogrid::GridHashSnapshot MakeSnapshot(const RecognitionDetail& detail)
{
    return recogrid::MakeGridHashSnapshot(detail.rows, detail.cols, ParseHashes(detail));
}

const CellDetail* FindCell(const RecognitionDetail& detail, std::size_t cellIndex)
{
    if (cellIndex < detail.cells.size() && static_cast<std::size_t>(detail.cells[cellIndex].cellIndex) == cellIndex) {
        return &detail.cells[cellIndex];
    }

    for (const CellDetail& cell : detail.cells) {
        if (cell.cellIndex >= 0 && static_cast<std::size_t>(cell.cellIndex) == cellIndex) {
            return &cell;
        }
    }
    return nullptr;
}

std::vector<std::size_t> AllCellIndices(const RecognitionDetail& detail)
{
    std::vector<std::size_t> indices;
    indices.reserve(detail.cellHashes.size());
    for (std::size_t i = 0; i < detail.cellHashes.size(); ++i) {
        indices.push_back(i);
    }
    return indices;
}

void CountNewCells(ScanState& state, const RecognitionDetail& detail, const std::vector<std::size_t>& indices, PageOutput& page)
{
    for (const std::size_t index : indices) {
        const CellDetail* cell = FindCell(detail, index);

        ++state.totalCount;
        ++page.newCells;

        CellOutput output;
        output.globalIndex = state.totalCount;
        output.pageIndex = page.pageIndex;
        output.cellIndex = static_cast<int>(index);
        if (index < detail.cellHashes.size()) {
            output.hash = detail.cellHashes[index];
        }

        if (cell != nullptr) {
            output.cellIndex = cell->cellIndex;
            output.screenCell = cell->screenCell;
            output.hash = cell->hash;
            output.matched = cell->matched && !cell->weaponId.empty();
            output.weaponId = cell->weaponId;
            output.score = cell->score;
            output.templateScore = cell->templateScore;
            output.hueScore = cell->hueScore;
            output.phashDistance = cell->phashDistance;
        }

        if (output.matched) {
            ++state.recognizedCount;
            ++page.recognizedCells;
            ++state.weaponCounts[output.weaponId];
        }
        else {
            ++state.unknownCount;
            ++page.unknownCells;
        }
        state.cells.push_back(std::move(output));
    }
}

void WriteOutputFile(const ScanState& state)
{
    ScanOutput output;
    output.status = state.status;
    output.totalCount = state.totalCount;
    output.recognizedCount = state.recognizedCount;
    output.unknownCount = state.unknownCount;
    output.weaponCounts = state.weaponCounts;
    output.weaponTypeCount = static_cast<int>(output.weaponCounts.size());
    output.pages = state.pages;
    output.cells = state.cells;

    const fs::path path = ResolvePath(state.outputPath);
    std::error_code ec;
    if (path.has_parent_path()) {
        fs::create_directories(path.parent_path(), ec);
    }
    if (ec) {
        throw std::runtime_error("failed to create output directory: " + ec.message());
    }

    std::ofstream file(path, std::ios::out | std::ios::trunc);
    if (!file) {
        throw std::runtime_error("failed to open output file: " + path.string());
    }
    file << json::value(output).dumps(4) << '\n';
}

} // namespace

MaaBool MAA_CALL WeaponInventoryScanRecognitionRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    const char* custom_recognition_param,
    const MaaImageBuffer* image,
    const MaaRect* roi,
    [[maybe_unused]] void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    if (image == nullptr || MaaImageBufferIsEmpty(image)) {
        WriteJsonDetail(out_detail, MakeErrorDetail(-2, "Image buffer is empty"));
        LogError << "WeaponInventoryScanRecognition: Image buffer is empty";
        return MAA_FALSE;
    }

    try {
        ScanState* state = GetState();
        ScanOptions options = state == nullptr ? ParseScanOptions(custom_recognition_param) : state->options;
        if ((custom_recognition_param == nullptr || std::strlen(custom_recognition_param) == 0) && roi != nullptr &&
            roi->width > 0 && roi->height > 0) {
            options.grid.detect.roi = cv::Rect(roi->x, roi->y, roi->width, roi->height);
        }

        std::vector<recogrid::GridClassifyTemplate> templates;
        const std::vector<recogrid::GridClassifyTemplate>* templatePtr = nullptr;
        if (state != nullptr && !state->templates.empty()) {
            templatePtr = &state->templates;
        }
        else {
            templates = LoadTemplateIndex(options);
            templatePtr = &templates;
        }
        if (templatePtr == nullptr || templatePtr->empty()) {
            WriteJsonDetail(out_detail, MakeErrorDetail(-3, "No weapon icon templates loaded"));
            LogError << "WeaponInventoryScanRecognition: no templates loaded";
            return MAA_FALSE;
        }

        const cv::Mat screenshot = to_mat(image);
        const recogrid::GridRecognitionResult grid = recogrid::RecognizeGrid(screenshot, options.grid);
        if (grid.grid.cells.empty()) {
            WriteJsonDetail(out_detail, MakeErrorDetail(1, "Grid detected no cells"));
            LogWarn << "WeaponInventoryScanRecognition: grid detected no cells";
            return MAA_FALSE;
        }

        const recogrid::GridClassificationResult classification =
            recogrid::ClassifyGridCells(grid, *templatePtr, options.grid, options.classify, screenshot.size());

        RecognitionDetail detail;
        detail.rows = static_cast<int>(grid.grid.rows.size());
        detail.cols = static_cast<int>(grid.grid.cols.size());
        detail.cellCount = static_cast<int>(grid.grid.cells.size());
        detail.templatesScanned = classification.templatesScanned;
        detail.candidatesAfterPhash = classification.candidatesAfterPhash;
        detail.matchesRanked = classification.matchesRanked;
        detail.matchedCells = classification.matchedCells;
        detail.unmatchedCells = classification.unmatchedCells;

        detail.cellHashes.reserve(grid.cellHashes.size());
        for (const recogrid::Hash hash : grid.cellHashes) {
            detail.cellHashes.push_back(HashToHex(hash));
        }

        detail.cells.reserve(classification.cells.size());
        for (const recogrid::GridCellClassification& cell : classification.cells) {
            detail.cells.push_back({
                static_cast<int>(cell.cellIndex),
                RectToArray(cell.screenCell),
                HashToHex(cell.hash),
                cell.matched,
                cell.templateId,
                cell.score,
                cell.templateScore,
                cell.hueScore,
                cell.phashDistance,
            });
        }

        WriteJsonDetail(out_detail, detail);
        if (out_box != nullptr) {
            cv::Rect firstCell = grid.screenGrid;
            if (!detail.cells.empty() && detail.cells.front().screenCell.size() == 4) {
                firstCell = cv::Rect(
                    detail.cells.front().screenCell[0],
                    detail.cells.front().screenCell[1],
                    detail.cells.front().screenCell[2],
                    detail.cells.front().screenCell[3]);
            }
            *out_box = { firstCell.x, firstCell.y, firstCell.width, firstCell.height };
        }

        LogInfo << "WeaponInventoryScanRecognition matched" << VAR(detail.rows) << VAR(detail.cols)
                << VAR(detail.cellCount) << VAR(detail.matchedCells) << VAR(detail.unmatchedCells)
                << VAR(detail.templatesScanned) << VAR(detail.candidatesAfterPhash) << VAR(detail.matchesRanked);
        return MAA_TRUE;
    }
    catch (const std::exception& e) {
        WriteJsonDetail(out_detail, MakeErrorDetail(-4, e.what()));
        LogError << "WeaponInventoryScanRecognition failed" << VAR(e.what());
        return MAA_FALSE;
    }
}

MaaBool MAA_CALL WeaponInventoryScanInitActionRun(
    [[maybe_unused]] MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    try {
        auto state = std::make_shared<ScanState>();
        state->options = ParseScanOptions(custom_action_param);
        ApplyStateParams(*state, custom_action_param);
        state->templates = LoadTemplateIndex(state->options);
        if (state->templates.empty()) {
            LogError << "WeaponInventoryScanInitAction: no weapon templates loaded";
            return MAA_FALSE;
        }

        LogInfo << "WeaponInventoryScanInitAction initialized" << VAR(state->templates.size())
                << VAR(state->outputPath) << VAR(state->options.classify.hueWeight)
                << VAR(state->options.classify.minScore) << VAR(state->options.delta.matchDistanceThreshold)
                << VAR(state->options.delta.minMatchRatio);
        SetState(std::move(state));
        return MAA_TRUE;
    }
    catch (const std::exception& e) {
        LogError << "WeaponInventoryScanInitAction failed" << VAR(e.what());
        return MAA_FALSE;
    }
}

MaaBool MAA_CALL WeaponInventoryScanRecordActionRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    try {
        ScanState* state = GetState();
        if (state == nullptr) {
            LogError << "WeaponInventoryScanRecordAction: missing scan state";
            return MAA_FALSE;
        }
        ApplyStateParams(*state, custom_action_param);

        const RecognitionDetail detail = ExtractInnerRecognitionDetail(GetRecognitionDetailJson(context, reco_id));
        const recogrid::GridHashSnapshot currentSnapshot = MakeSnapshot(detail);

        PageOutput page;
        page.pageIndex = state->pageCount + 1;
        page.rows = detail.rows;
        page.cols = detail.cols;
        page.visibleCells = detail.cellCount;

        std::vector<std::size_t> newCellIndices;
        bool continueScan = true;
        if (!state->hasPreviousSnapshot) {
            page.rowOffset = detail.rows;
            newCellIndices = AllCellIndices(detail);
        }
        else {
            const recogrid::GridDeltaResult delta =
                recogrid::ComputeGridDelta(state->previousSnapshot, currentSnapshot, state->options.delta);
            page.rowOffset = delta.rowOffset;
            page.comparedCells = delta.comparedCells;
            page.matchedCells = delta.matchedCells;
            page.matchRatio = delta.matchRatio;
            page.averageDistance = delta.averageDistance;

            if (!delta.reliable) {
                state->status = "alignment_failed";
                page.status = "alignment_failed";
                continueScan = false;
            }
            else if (delta.rowOffset < 0) {
                state->status = "reverse_scroll";
                page.status = "reverse_scroll";
                continueScan = false;
            }
            else if (!delta.hasProgress) {
                page.status = "complete";
                continueScan = false;
            }
            else {
                newCellIndices = delta.newCellIndices;
            }
        }

        CountNewCells(*state, detail, newCellIndices, page);
        ++state->pageCount;
        state->pages.push_back(page);

        if (continueScan) {
            state->previousSnapshot = currentSnapshot;
            state->hasPreviousSnapshot = true;
        }

        LogInfo << "WeaponInventoryScanRecordAction page" << VAR(page.pageIndex)
                << VAR(page.visibleCells) << VAR(page.newCells) << VAR(page.recognizedCells)
                << VAR(page.unknownCells) << VAR(page.rowOffset) << VAR(page.matchRatio)
                << VAR(page.status) << VAR(state->totalCount) << VAR(state->recognizedCount)
                << VAR(state->unknownCount);
        return continueScan ? MAA_TRUE : MAA_FALSE;
    }
    catch (const std::exception& e) {
        LogError << "WeaponInventoryScanRecordAction failed" << VAR(e.what());
        return MAA_FALSE;
    }
}

MaaBool MAA_CALL WeaponInventoryScanFinishActionRun(
    [[maybe_unused]] MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    try {
        std::shared_ptr<ScanState> state = TakeState();
        if (state == nullptr) {
            LogError << "WeaponInventoryScanFinishAction: missing scan state";
            return MAA_FALSE;
        }

        ApplyStateParams(*state, custom_action_param);
        WriteOutputFile(*state);
        LogInfo << "WeaponInventoryScanFinishAction finished" << VAR(state->pageCount)
                << VAR(state->totalCount) << VAR(state->recognizedCount) << VAR(state->unknownCount)
                << VAR(state->weaponCounts.size()) << VAR(state->status) << VAR(state->outputPath);
        return MAA_TRUE;
    }
    catch (const std::exception& e) {
        LogError << "WeaponInventoryScanFinishAction failed" << VAR(e.what());
        return MAA_FALSE;
    }
}

} // namespace weaponinventoryscan
