#include "expected_results.h"

#include <filesystem>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <string>

namespace
{

void Check(bool condition, const std::string& message)
{
    if (!condition) {
        throw std::runtime_error(message);
    }
}

const std::filesystem::path kFixturePath = "expected-results-fixture.csv";

void WriteFixture()
{
    std::ofstream stream(kFixturePath, std::ios::binary | std::ios::trunc);
    stream << "image,roi,item_id,count\n"
              "transfer/fixture.png,\"[10,20,300,200]\",item_a,2\n"
              "transfer/fixture.png,\"[10,20,300,200]\",item_b,1\n"
              "valuables/empty.png,\"[0,0,100,100]\",,0\n";
}

iconrecognition::RecognitionResult MakeMatchResult()
{
    iconrecognition::RecognitionResult result;
    result.grid_type = iconrecognition::GridType::Transfer;
    result.roi = cv::Rect(10, 20, 300, 200);
    result.matched = true;
    result.matches.push_back(iconrecognition::ItemMatch {
        .item = iconrecognition::ItemInfo { .item_id = "item_a" },
        .cell_box = cv::Rect(11, 21, 64, 64),
        .item_box = cv::Rect(12, 22, 64, 64),
        .score = 0.91,
        .row = 0,
        .column = 1,
    });
    result.matches.push_back(iconrecognition::ItemMatch {
        .item = iconrecognition::ItemInfo { .item_id = "item_b" },
        .cell_box = cv::Rect(80, 21, 64, 64),
        .item_box = cv::Rect(81, 22, 64, 64),
        .score = 0.90,
        .row = 0,
        .column = 2,
    });
    result.matches.push_back(iconrecognition::ItemMatch {
        .item = iconrecognition::ItemInfo { .item_id = "item_a" },
        .cell_box = cv::Rect(149, 21, 64, 64),
        .item_box = cv::Rect(150, 22, 64, 64),
        .score = 0.89,
        .row = 0,
        .column = 3,
    });
    return result;
}

void TestExpectedItemMultisetIgnoresGeometry()
{
    WriteFixture();
    const auto expected = iconrecognition::test::LoadExpectedResults(kFixturePath);
    auto result = MakeMatchResult();
    result.matches.front().cell_box.x += 7;
    result.matches.front().item_box.y -= 3;
    result.matches.front().row = 8;
    result.matches.front().column = 9;
    result.matched = false;
    const auto error = iconrecognition::test::CompareExpectedCase(
        expected,
        "transfer/fixture.png",
        iconrecognition::GridType::Transfer,
        "full",
        result,
        false);
    Check(!error, "matching item multiset must ignore boxes, cells, row, column, and matched flag");
}

void TestExpectedZeroMatchIsExplicit()
{
    const auto expected = iconrecognition::test::LoadExpectedResults(kFixturePath);
    iconrecognition::RecognitionResult result;
    result.grid_type = iconrecognition::GridType::Valuables;
    result.roi = cv::Rect(0, 0, 100, 100);
    const auto error = iconrecognition::test::CompareExpectedCase(
        expected,
        "valuables/empty.png",
        iconrecognition::GridType::Valuables,
        "full",
        result,
        false);
    Check(!error, "explicit expected zero matches must pass");
}

void TestWrongItemCountAndUnexpectedCaseFail()
{
    const auto expected = iconrecognition::test::LoadExpectedResults(kFixturePath);
    auto result = MakeMatchResult();
    result.matches.pop_back();
    const auto mismatch = iconrecognition::test::CompareExpectedCase(
        expected,
        "transfer/fixture.png",
        iconrecognition::GridType::Transfer,
        "full",
        result,
        false);
    Check(mismatch.has_value(), "wrong item count must fail expected comparison");

    const auto unexpected = iconrecognition::test::CompareExpectedCase(
        expected,
        "local-only.png",
        iconrecognition::GridType::Transfer,
        "full",
        MakeMatchResult(),
        false);
    Check(unexpected.has_value(), "unexpected tracked case must fail expected comparison");
    const auto allowed = iconrecognition::test::CompareExpectedCase(
        expected,
        "local-only.png",
        iconrecognition::GridType::Transfer,
        "full",
        MakeMatchResult(),
        true);
    Check(!allowed, "explicitly allowed local case must not fail expected comparison");
}

void TestSupplementalLocalImageName()
{
    Check(
        iconrecognition::test::IsSupplementalLocalImage("transfer/57.local1.png"),
        ".localN image must be treated as supplemental local input");
    Check(
        iconrecognition::test::IsSupplementalLocalImage("single_roi/1177-450-54/1.local12.png"),
        "nested .localN image must be treated as supplemental local input");
    Check(!iconrecognition::test::IsSupplementalLocalImage("transfer/57.png"), "tracked fixture must not bypass expected comparison");
    Check(
        !iconrecognition::test::IsSupplementalLocalImage("transfer/example.local.png"),
        "local suffix without an index must remain a regular fixture");
}

} // namespace

int main()
{
    try {
        TestExpectedItemMultisetIgnoresGeometry();
        TestExpectedZeroMatchIsExplicit();
        TestWrongItemCountAndUnexpectedCaseFail();
        TestSupplementalLocalImageName();
        std::filesystem::remove(kFixturePath);
        return 0;
    }
    catch (const std::exception& error) {
        std::filesystem::remove(kFixturePath);
        std::cerr << error.what() << '\n';
        return 1;
    }
}
