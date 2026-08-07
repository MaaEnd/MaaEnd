#include "../IconRecognitionTypes.h"

#include <algorithm>
#include <cassert>
#include <string>

#include <meojson/json.hpp>

namespace iconrecognition::test
{

int RunIconRecognitionTypesTest()
{
    RecognitionRequest request;
    assert(request.grid_type == GridType::Transfer);
    assert(!request.deduplicate);
    assert(ParseGridType("single_roi") == GridType::SingleRoi);
    assert(GridTypeName(GridType::SingleRoi) == "single_roi");

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
    assert(object.contains("detail_version"));
    assert(object.contains("matched"));
    assert(object.contains("roi"));
    assert(object.contains("matches"));
    assert(!object.contains("confidence"));
    assert(!object.contains("baseline_score"));
    assert(!object.contains("best_phase"));
    assert(!object.contains("fallback_used"));
    assert(!object.contains("rejected_reason"));
    assert(!object.contains("best"));

    const json::object match = object.at("matches").as_array().at(0).as_object();
    assert(match.contains("item_id"));
    assert(match.contains("cell_box"));
    assert(match.contains("item_box"));
    assert(match.contains("score"));
    assert(match.contains("row"));
    assert(match.contains("column"));

    result.matches.push_back(ItemMatch {
        .item = ItemInfo { .item_id = "item_a" },
        .cell_box = cv::Rect(30, 40, 64, 64),
        .score = 0.9,
    });
    std::stable_sort(result.matches.begin(), result.matches.end(), ItemMatchLess {});
    assert(result.matches.front().item.item_id == "item_a");

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
    assert(result.matches.size() == 3);
    assert(result.matches.at(0).item.item_id == "item_a");
    assert(result.matches.at(0).score == 0.9);
    assert(result.matches.at(1).item.item_id == "item_b");
    assert(result.matches.at(2).item.item_id == "item_c");
    return 0;
}

} // namespace iconrecognition::test

#ifdef ICON_RECOGNITION_TEST_MAIN
int main()
{
    return iconrecognition::test::RunIconRecognitionTypesTest();
}
#endif
