#include "EssenceReward.h"

#include <algorithm>
#include <cmath>
#include <exception>
#include <string>

#include <meojson/json.hpp>

#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/NoWarningCV.hpp>

#include "../Common/paths.h"
#include "../IconRecognition/IconRecognizer.h"
#include "../IconRecognition/detail/GridDetector.h"
#include "../IconRecognition/detail/RarityClassifier.h"
#include "../utils.h"

namespace essencereward
{
namespace
{
// Same acceptance threshold as the original reward templates; color is independent.
constexpr double kFamilyThreshold = 0.7;
// Ignore the bottom rarity stripe and tolerate the grid detector's pixel rounding.
constexpr double kStripeFraction = 0.08;
constexpr int kPositionTolerance = 2;

double family_score(const cv::Mat& image, const cv::Rect& box, const cv::Mat& original)
{
    cv::Mat templ, green, mask, response;
    cv::resize(original, templ, box.size(), 0, 0, cv::INTER_LINEAR);
    cv::inRange(templ, cv::Scalar(0, 240, 0), cv::Scalar(20, 255, 20), green);
    cv::bitwise_not(green, mask);
    mask.rowRange(mask.rows - std::max(1, cvRound(mask.rows * kStripeFraction)), mask.rows).setTo(0);
    const cv::Rect search =
        (box + cv::Size(2 * kPositionTolerance, 2 * kPositionTolerance) - cv::Point(kPositionTolerance, kPositionTolerance))
        & cv::Rect({}, image.size());
    cv::matchTemplate(image(search), templ, response, cv::TM_CCOEFF_NORMED, mask);
    double score = 0;
    cv::minMaxLoc(response, nullptr, &score);
    return std::isfinite(score) ? score : 0;
}

struct Resources
{
    cv::Mat gold_ = MAA_NS::imread(common::InstallPath("resource/image/EssenceFilter/EssenceGeneral.png"));
    cv::Mat purple_ = MAA_NS::imread(common::InstallPath("resource/image/EssenceFilter/EssenceRewardHighPurity.png"));
    iconrecognition::IconRecognizer ordinary_ { common::InstallPath("data/IconRecognition") };
    bool ready_ = !gold_.empty() && !purple_.empty() && ordinary_.initialize();
};
} // namespace

MaaBool MAA_CALL Recognize(
    MaaContext*,
    MaaTaskId,
    const char*,
    const char*,
    const char*,
    const MaaImageBuffer* buffer,
    const MaaRect* roi,
    void*,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    try {
        if (!buffer || !roi || !out_box || !out_detail || MaaImageBufferIsEmpty(buffer)) {
            return false;
        }
        static Resources resources;
        if (!resources.ready_) {
            LogError << "Essence reward resources unavailable";
            return false;
        }
        const cv::Mat image = to_mat(buffer);
        const cv::Rect area(roi->x, roi->y, roi->width, roi->height);
        const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Rewards, area);
        if (grid.cells.empty() || !grid.failure_message.empty()) {
            return false;
        }
        json::array cells;
        bool complete = true;
        for (const auto& cell : grid.cells) {
            const double score =
                std::max(family_score(image, cell.cell_box, resources.gold_), family_score(image, cell.cell_box, resources.purple_));
            const auto rarity = iconrecognition::detail::ClassifyRarity(image, cell.cell_box, grid.grid_scale);
            std::string quality = "unknown";
            if (score >= kFamilyThreshold) {
                if (rarity.rarity == 5) {
                    quality = "flawless_gold";
                }
                else if (rarity.rarity == 4) {
                    quality = "high_purity_purple";
                }
            }
            else {
                // A template miss is not evidence of absence. Recognize a known ordinary item.
                iconrecognition::RecognitionRequest request;
                request.grid_type = iconrecognition::GridType::SingleRoi;
                request.roi = cell.cell_box;
                request.candidates.item_filters = { "Isolate:*", "ValuableDepot:*" };
                const auto other = resources.ordinary_.recognize(image, request);
                if (other.matched && !other.matches.empty()) {
                    quality = "other";
                }
            }
            complete = complete && quality != "unknown";
            cells.emplace_back(
                json::object {
                    { "box", iconrecognition::RectToJson(cell.cell_box) },
                    { "quality", quality },
                    { "family_score", score },
                });
        }
        const auto detail = json::value(
                                json::object {
                                    { "complete", complete },
                                    { "cells", std::move(cells) },
                                })
                                .dumps();
        *out_box = *roi;
        return MaaStringBufferSet(out_detail, detail.c_str());
    }
    catch (const std::exception& error) {
        LogError << "Essence reward recognition failed" << VAR(error.what());
        return false;
    }
}
} // namespace essencereward
