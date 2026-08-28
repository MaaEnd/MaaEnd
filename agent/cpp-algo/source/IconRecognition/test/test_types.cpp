#include "../IconRecognitionTypes.h"

#include <algorithm>
#include <stdexcept>
#include <string>

#include <meojson/json.hpp>

namespace iconrecognition::test
{

void Check(bool condition, const std::string& message)
{
    if (!condition) {
        throw std::runtime_error(message);
    }
}

void TestRectJsonRoundTripAndInvalidValues()
{
    const cv::Rect expected { 10, 20, 100, 80 };
    cv::Rect decoded;
    Check(RectFromJson(RectToJson(expected), decoded), "RectFromJson must accept RectToJson output");
    Check(decoded == expected, "RectFromJson round-trip must preserve the rectangle");

    const auto check_invalid = [](const json::value& value) {
        cv::Rect output { 1, 2, 3, 4 };
        Check(!RectFromJson(value, output), "RectFromJson must reject invalid input");
    };
    check_invalid(json::value(json::array { 1, 2, 3 }));
    check_invalid(json::value(json::array { 1, 2, 3, 4, 5 }));
    check_invalid(json::value(json::array { 1, std::string("invalid"), 3, 4 }));
    check_invalid(json::value(json::object { { "x", 1 } }));
}

int RunIconRecognitionTypesTest()
{
    TestRectJsonRoundTripAndInvalidValues();
    RecognitionRequest request;
    Check(request.grid_type == GridType::Transfer, "request default grid type mismatch");
    Check(!request.deduplicate, "request deduplicate must default to false");
    Check(!request.recognize_region_unavailable, "recognition of items unavailable in the current region must default to false");
    Check(ParseGridType("single_roi") == GridType::SingleRoi, "single_roi grid type must parse");
    Check(GridTypeName(GridType::SingleRoi) == "single_roi", "single_roi grid type name mismatch");
    const auto rewards = ParseGridType("rewards");
    Check(rewards.has_value(), "rewards grid type must parse");
    Check(GridTypeName(*rewards) == "rewards", "rewards grid type name mismatch");
    Check(
        SupportsRegionUnavailableRecognition(GridType::Transfer) && SupportsRegionUnavailableRecognition(GridType::PortStorager),
        "region-restricted fallback must support transfer and port_storager grids");
    for (const auto grid_type : {
             GridType::Trade,
             GridType::Valuables,
             GridType::Shipment,
             GridType::CreditTrade,
             GridType::Rewards,
             GridType::SingleRoi,
         }) {
        Check(
            !SupportsRegionUnavailableRecognition(grid_type),
            "current-region availability recognition must be disabled for unsupported grids");
    }

    RecognitionResult result;
    result.matched = true;
    result.roi = cv::Rect(10, 20, 100, 80);
    result.matches.push_back(ItemMatch {
        .item =
            ItemInfo {
                .item_id = "item_b",
                .name = "iconRecognition.name.item_b",
                .category = "产物",
                .storage_kind = "Normal",
                .category_type = "Product",
                .rarity = 3,
            },
        .cell_box = cv::Rect(30, 40, 64, 64),
        .item_box = cv::Rect(38, 48, 48, 48),
        .score = 0.9,
        .region_unavailable = false,
        .row = 0,
        .column = 1,
    });

    const json::object object = json::value(result).as_object();
    Check(object.contains("detail_version"), "serialized result must contain detail_version");
    Check(object.at("detail_version").as_integer() == 3, "serialized result must use detail contract version 3");
    Check(object.contains("matched"), "serialized result must contain matched");
    Check(object.contains("roi"), "serialized result must contain roi");
    Check(object.contains("matches"), "serialized result must contain matches");
    Check(object.at("roi").is_array() && object.at("roi").as_array().size() == 4, "serialized roi must use Maa array format");
    Check(!object.contains("confidence"), "serialized result must not expose confidence");
    Check(!object.contains("baseline_score"), "serialized result must not expose baseline_score");
    Check(!object.contains("best_phase"), "serialized result must not expose best_phase");
    Check(!object.contains("fallback_used"), "serialized result must not expose fallback_used");
    Check(!object.contains("rejected_reason"), "serialized result must not expose rejected_reason");
    Check(!object.contains("best"), "serialized result must not expose best");

    const json::object match = object.at("matches").as_array().at(0).as_object();
    Check(match.contains("item_id"), "serialized match must contain item_id");
    Check(match.contains("cell_box"), "serialized match must contain cell_box");
    Check(match.contains("item_box"), "serialized match must contain item_box");
    Check(match.at("cell_box").is_array() && match.at("cell_box").as_array().size() == 4, "serialized cell_box must use Maa array format");
    Check(match.at("item_box").is_array() && match.at("item_box").as_array().size() == 4, "serialized item_box must use Maa array format");
    Check(match.contains("score"), "serialized match must contain score");
    Check(!match.contains("region_unavailable"), "normal serialized match must omit region_unavailable");
    Check(!match.contains("disabled"), "serialized match must not expose the ambiguous disabled field");
    Check(match.contains("row"), "serialized match must contain row");
    Check(match.contains("column"), "serialized match must contain column");

    ItemMatch disabled_match {
        .item = ItemInfo { .item_id = "item_disabled" },
        .region_unavailable = true,
    };
    const json::object disabled_object = json::value(disabled_match).as_object();
    Check(
        disabled_object.contains("region_unavailable") && disabled_object.at("region_unavailable").as_boolean(),
        "region-unavailable match must serialize region_unavailable=true");
    Check(!disabled_object.contains("disabled"), "region-unavailable match must not serialize disabled");

    result.matches.push_back(ItemMatch {
        .item = ItemInfo { .item_id = "item_a" },
        .cell_box = cv::Rect(30, 40, 64, 64),
        .score = 0.9,
    });
    std::stable_sort(result.matches.begin(), result.matches.end(), ItemMatchLess {});
    Check(result.matches.front().item.item_id == "item_a", "stable match ordering must use item_id as a tiebreaker");

    result.matches.push_back(ItemMatch {
        .item = ItemInfo { .item_id = "item_a" },
        .cell_box = cv::Rect(100, 40, 64, 64),
        .score = 0.8,
    });
    result.matches.push_back(ItemMatch {
        .item = ItemInfo { .item_id = "item_c" },
        .cell_box = cv::Rect(170, 40, 64, 64),
        .score = 0.7,
    });
    DeduplicateMatches(result.matches);
    Check(result.matches.size() == 3, "deduplication must keep one result per item_id");
    Check(result.matches.at(0).item.item_id == "item_a", "deduplicated item_a ordering mismatch");
    Check(result.matches.at(0).score == 0.9, "deduplication must keep the highest item_a score");
    Check(result.matches.at(1).item.item_id == "item_b", "deduplicated item_b ordering mismatch");
    Check(result.matches.at(2).item.item_id == "item_c", "deduplicated item_c ordering mismatch");
    return 0;
}

} // namespace iconrecognition::test

#ifdef ICON_RECOGNITION_TEST_MAIN
int main()
{
    return iconrecognition::test::RunIconRecognitionTypesTest();
}
#endif
