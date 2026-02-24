#pragma once

#include <string>
#include <vector>

#include <meojson/json.hpp>

#include <MaaFramework/MaaAPI.h>

#include <MaaUtils/Logger.h>

struct OcrItem
{
    std::string text;

    MEO_JSONIZATION(MEO_OPT text);
};

struct OcrDetail
{
    OcrItem best;
    std::vector<OcrItem> filtered;
    std::vector<OcrItem> all;

    MEO_JSONIZATION(MEO_OPT best, MEO_OPT filtered, MEO_OPT all);
};

// Extracts the best OCR text from the recognition detail and writes it
// directly to the detail buffer as a plain string.
inline void extract_best_text_for_custom(MaaStringBuffer* detail_buf)
{
    auto raw = MaaStringBufferGet(detail_buf);
    if (!raw) {
        return;
    }

    auto parsed = json::parse(raw);
    if (!parsed.has_value()) {
        return;
    }

    auto detail = parsed->as<OcrDetail>();
    std::string text;

    // Try "best" first (single object), then first item of "filtered", then "all"
    if (!detail.best.text.empty()) {
        text = detail.best.text;
    }
    if (text.empty()) {
        for (const auto& item : detail.filtered) {
            if (!item.text.empty()) {
                text = item.text;
                break;
            }
        }
    }
    if (text.empty()) {
        for (const auto& item : detail.all) {
            if (!item.text.empty()) {
                text = item.text;
                break;
            }
        }
    }

    MaaStringBufferSet(detail_buf, text.c_str());
}

inline json::value build_ocr_params(const json::value& params, const MaaRect* roi)
{
    json::value ocr;

    if (params.contains("expected")) {
        ocr["expected"] = params.at("expected");
    }
    if (params.contains("threshold")) {
        ocr["threshold"] = params.at("threshold");
    }
    if (params.contains("order_by")) {
        ocr["order_by"] = params.at("order_by");
    }
    if (params.contains("replace")) {
        ocr["replace"] = params.at("replace");
    }
    if (params.contains("index")) {
        ocr["index"] = params.at("index");
    }
    if (params.contains("only_rec")) {
        ocr["only_rec"] = params.at("only_rec");
    }
    if (params.contains("model")) {
        ocr["model"] = params.at("model");
    }

    if (roi) {
        int x = MaaRectGetX(roi);
        int y = MaaRectGetY(roi);
        int w = MaaRectGetW(roi);
        int h = MaaRectGetH(roi);
        if (w > 0 && h > 0) {
            ocr["roi"] = json::array { x, y, w, h };
        }
    }

    return ocr;
}
