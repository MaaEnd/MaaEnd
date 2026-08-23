#pragma once

#include <algorithm>
#include <memory>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

#include <MaaUtils/NoWarningCV.hpp>
#include <meojson/json.hpp>

namespace iconrecognition
{

namespace detail
{
struct RecognitionDiagnostics;
}

enum class GridType
{
    Trade,
    Transfer,
    PortStorager,
    Valuables,
    Shipment,
    CreditTrade,
    Rewards,
    SingleRoi,
};

inline std::string_view GridTypeName(GridType type)
{
    switch (type) {
    case GridType::Trade:
        return "trade";
    case GridType::Transfer:
        return "transfer";
    case GridType::PortStorager:
        return "port_storager";
    case GridType::Valuables:
        return "valuables";
    case GridType::Shipment:
        return "shipment";
    case GridType::CreditTrade:
        return "credit_trade";
    case GridType::Rewards:
        return "rewards";
    case GridType::SingleRoi:
        return "single_roi";
    }
    return "transfer";
}

inline std::optional<GridType> ParseGridType(std::string_view name)
{
    if (name == "trade") {
        return GridType::Trade;
    }
    if (name == "transfer") {
        return GridType::Transfer;
    }
    if (name == "port_storager") {
        return GridType::PortStorager;
    }
    if (name == "valuables") {
        return GridType::Valuables;
    }
    if (name == "shipment") {
        return GridType::Shipment;
    }
    if (name == "credit_trade") {
        return GridType::CreditTrade;
    }
    if (name == "rewards") {
        return GridType::Rewards;
    }
    if (name == "single_roi") {
        return GridType::SingleRoi;
    }
    return std::nullopt;
}

inline json::value RectToJson(const cv::Rect& rect)
{
    return json::array { rect.x, rect.y, rect.width, rect.height };
}

inline bool RectFromJson(const json::value& value, cv::Rect& rect)
{
    if (!value.is_array() || value.as_array().size() != 4) {
        return false;
    }
    const auto& values = value.as_array();
    const auto read = [&values](std::size_t index, int& output) {
        if (!values.at(index).is_number()) {
            return false;
        }
        output = values.at(index).as_integer();
        return true;
    };
    return read(0, rect.x) && read(1, rect.y) && read(2, rect.width) && read(3, rect.height);
}

struct ItemInfo
{
    std::string item_id;
    std::string name;
    std::string category;
    std::string storage_kind;
    std::string category_type;
    int rarity = 0;

    json::value to_json() const
    {
        return json::object {
            { "item_id", item_id },
            { "name", name },
            { "category", category },
            { "storage_kind", storage_kind },
            { "category_type", category_type },
            { "rarity", rarity },
        };
    }
};

struct ItemMatch
{
    ItemInfo item;
    cv::Rect cell_box;
    cv::Rect item_box;
    double score = 0.0;
    std::optional<int> row;
    std::optional<int> column;

    json::value to_json() const
    {
        json::object object {
            { "item_id", item.item_id },
            { "name", item.name },
            { "category", item.category },
            { "storage_kind", item.storage_kind },
            { "category_type", item.category_type },
            { "rarity", item.rarity },
            { "cell_box", RectToJson(cell_box) },
            { "item_box", RectToJson(item_box) },
            { "score", score },
        };
        if (row) {
            object["row"] = *row;
        }
        if (column) {
            object["column"] = *column;
        }
        return object;
    }
};

struct CandidateFilter
{
    std::vector<std::string> item_ids;
    std::vector<std::string> item_filters;
    std::vector<std::string> item_recheck_filters;
};

struct RecognitionRequest
{
    // 默认值仅作为构造占位；调用入口解析成功后必须写入实际 grid_type。
    GridType grid_type = GridType::Transfer;
    cv::Rect roi;
    CandidateFilter candidates;
    // Custom 入口根据 MaaContext 填入的内部控制器比例；空值表示离线调用需从图像推断。
    std::optional<double> grid_scale_hint;
    // 最终接受匹配的默认分数；调高减少误识别，调低提高弱图标召回。
    double threshold = 0.85;
    // 首轮分数达到该值才进行亚像素细化；调高减少计算量，调低增加细化候选。
    double subpixel_threshold = 0.60;
    // 默认保留同一 item 的所有格子命中，由调用方决定是否按 item 去重。
    bool deduplicate = false;
    // 默认不采集内部诊断和耗时，避免正常识别承担额外观测开销。
    bool debug = false;
};

struct RecognitionResult
{
    // 公开 detail JSON 的结构版本，字段发生不兼容变化时递增。
    int detail_version = 2;
    bool matched = false;
    // 结果对象的构造占位值；has_grid_type=false 时不会序列化该字段。
    GridType grid_type = GridType::Transfer;
    // 标记 grid_type 是否已经从请求解析或识别上下文中确定。
    bool has_grid_type = true;
    cv::Rect roi;
    std::vector<ItemMatch> matches;
    std::string error_code;
    std::string message;
    // 仅供 IconRecognition 内部 debug 使用，公开 JSON 不序列化该字段。
    std::shared_ptr<detail::RecognitionDiagnostics> diagnostics;

    json::value to_json() const
    {
        json::object object {
            { "detail_version", detail_version },
            { "matched", matched },
            { "roi", RectToJson(roi) },
            { "matches", json::array {} },
        };
        if (has_grid_type) {
            object["grid_type"] = std::string(GridTypeName(grid_type));
        }
        auto& matches_json = object["matches"].as_array();
        for (const auto& match : matches) {
            matches_json.emplace_back(match.to_json());
        }
        if (!error_code.empty()) {
            object["error"] = json::object {
                { "code", error_code },
                { "message", message },
            };
        }
        return object;
    }
};

struct ItemMatchLess
{
    bool operator()(const ItemMatch& left, const ItemMatch& right) const
    {
        if (left.score != right.score) {
            return left.score > right.score;
        }
        if (left.cell_box.y != right.cell_box.y) {
            return left.cell_box.y < right.cell_box.y;
        }
        if (left.cell_box.x != right.cell_box.x) {
            return left.cell_box.x < right.cell_box.x;
        }
        return left.item.item_id < right.item.item_id;
    }
};

// DeduplicateMatches 保留当前排序中每个 item_id 的首个结果。
inline void DeduplicateMatches(std::vector<ItemMatch>& matches)
{
    std::vector<std::string> seen;
    std::erase_if(matches, [&](const ItemMatch& match) {
        if (std::ranges::find(seen, match.item.item_id) != seen.end()) {
            return true;
        }
        seen.push_back(match.item.item_id);
        return false;
    });
}

} // namespace iconrecognition
