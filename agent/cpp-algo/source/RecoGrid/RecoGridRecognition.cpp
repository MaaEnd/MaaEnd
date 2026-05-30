#include "RecoGridRecognition.h"

#include "GridRecognizer.h"

#include <cstring>
#include <exception>
#include <string>
#include <vector>

#include <meojson/json.hpp>

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/NoWarningCV.hpp>

#include "../utils.h"

#ifndef MAA_TRUE
#define MAA_TRUE 1
#endif
#ifndef MAA_FALSE
#define MAA_FALSE 0
#endif

namespace recogrid
{
namespace
{

struct RectOutput
{
    int x = 0;
    int y = 0;
    int width = 0;
    int height = 0;

    MEO_JSONIZATION(x, y, width, height)
};

struct MatchOutput
{
    int cellIndex = 0;
    RectOutput screenCell;
    RectOutput screenMatch;
    int phashDistance = 0;
    double score = 0.0;

    MEO_JSONIZATION(
        MEO_KEY("cell_index") cellIndex,
        MEO_KEY("screen_cell") screenCell,
        MEO_KEY("screen_match") screenMatch,
        MEO_KEY("phash_distance") phashDistance,
        score)
};

struct RecoGridDetail
{
    int status = 0;
    bool matched = false;
    std::string message;
    RectOutput roi;
    RectOutput box;
    int rows = 0;
    int cols = 0;
    int cells = 0;
    int candidates = 0;
    std::vector<RectOutput> cellBoxes;
    std::vector<MatchOutput> matches;

    MEO_JSONIZATION(status, matched, message, roi, box, rows, cols, cells, candidates, MEO_KEY("cell_boxes") cellBoxes, matches)
};

template <typename T>
void WriteJsonDetail(MaaStringBuffer* outDetail, const T& payload)
{
    if (outDetail == nullptr) {
        return;
    }

    const std::string text = json::value(payload).dumps();
    MaaStringBufferSet(outDetail, text.c_str());
}

RectOutput ToOutputRect(const cv::Rect& rect)
{
    return { rect.x, rect.y, rect.width, rect.height };
}

MaaRect ToMaaRect(const cv::Rect& rect)
{
    return { rect.x, rect.y, rect.width, rect.height };
}

RecoGridDetail MakeErrorDetail(int status, std::string message)
{
    RecoGridDetail detail;
    detail.status = status;
    detail.message = std::move(message);
    return detail;
}

RecoGridDetail ToDetail(int status, const GridRecognitionResult& result)
{
    RecoGridDetail detail;
    detail.status = status;
    detail.matched = result.matched;
    detail.message = result.message;
    detail.roi = ToOutputRect(result.screenRoi);
    detail.box = ToOutputRect(result.matches.empty() ? result.screenGrid : result.matches.front().screenMatch);
    detail.rows = static_cast<int>(result.grid.rows.size());
    detail.cols = static_cast<int>(result.grid.cols.size());
    detail.cells = static_cast<int>(result.grid.cells.size());
    detail.candidates = static_cast<int>(result.candidates.size());

    detail.cellBoxes.reserve(result.screenCells.size());
    for (const auto& cell : result.screenCells) {
        detail.cellBoxes.push_back(ToOutputRect(cell));
    }

    detail.matches.reserve(result.matches.size());
    for (const auto& match : result.matches) {
        detail.matches.push_back({
            static_cast<int>(match.cellIndex),
            ToOutputRect(match.screenCell),
            ToOutputRect(match.screenMatch),
            match.phashDistance,
            match.score,
        });
    }
    return detail;
}

} // namespace

MaaBool MAA_CALL RecoGridRecognitionRun(
    [[maybe_unused]] MaaContext* context,
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
        LogError << "RecoGridRecognition: Image buffer is empty";
        return MAA_FALSE;
    }

    try {
        GridRecognitionRequest request = ParseGridRecognitionRequest(custom_recognition_param);
        if (custom_recognition_param == nullptr || std::strlen(custom_recognition_param) == 0) {
            request = ApplyRoiOverride(request, roi == nullptr ? cv::Rect {} : cv::Rect(roi->x, roi->y, roi->width, roi->height));
        }

        const GridRecognitionResult result = RecognizeGridRequest(to_mat(image), request);
        const int status = result.matched ? 0 : (result.grid.cells.empty() ? 1 : 2);
        WriteJsonDetail(out_detail, ToDetail(status, result));
        if (!result.matched) {
            LogWarn << "RecoGridRecognition miss" << VAR(result.message);
            return MAA_FALSE;
        }

        if (out_box != nullptr) {
            *out_box = ToMaaRect(result.matches.empty() ? result.screenGrid : result.matches.front().screenMatch);
        }
        LogInfo << "RecoGridRecognition matched" << VAR(result.message) << VAR(result.grid.rows.size()) << VAR(result.grid.cols.size());
        return MAA_TRUE;
    }
    catch (const std::exception& e) {
        WriteJsonDetail(out_detail, MakeErrorDetail(-4, e.what()));
        LogError << "RecoGridRecognition failed" << VAR(e.what());
        return MAA_FALSE;
    }
}

} // namespace recogrid
