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

int RunIconRecognitionTypesTest()
{
    RecognitionRequest request;
    Check(request.grid_type == GridType::Transfer, "request default grid type mismatch");
    Check(!request.deduplicate, "request deduplicate must default to false");
    Check(ParseGridType("single_roi") == GridType::SingleRoi, "single_roi grid type must parse");
    Check(GridTypeName(GridType::SingleRoi) == "single_roi", "single_roi grid type name mismatch");
    const auto rewards = ParseGridType("rewards");
    Check(rewards.has_value(), "rewards grid type must parse");
    Check(GridTypeName(*rewards) == "rewards", "rewards grid type name mismatch");

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
        .row = 0,
        .column = 1,
    });

    const json::object object = json::value(result).as_object();
    Check(object.contains("detail_version"), "serialized result must contain detail_version");
    Check(object.contains("matched"), "serialized result must contain matched");
    Check(object.contains("roi"), "serialized result must contain roi");
    Check(object.contains("matches"), "serialized result must contain matches");
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
    Check(match.contains("score"), "serialized match must contain score");
    Check(match.contains("row"), "serialized match must contain row");
    Check(match.contains("column"), "serialized match must contain column");

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
