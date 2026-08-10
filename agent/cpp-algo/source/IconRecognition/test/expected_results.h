#pragma once

#include <cstddef>
#include <filesystem>
#include <map>
#include <optional>
#include <string>
#include <string_view>

#include "../IconRecognitionTypes.h"

namespace iconrecognition::test
{

struct ExpectedCase
{
    std::string image;
    GridType grid_type;
    std::string roi_name;
    cv::Rect roi;
    std::map<std::string, std::size_t, std::less<>> item_counts;
    bool explicit_zero = false;
};

class ExpectedResults
{
public:
    const ExpectedCase* find(std::string_view image, GridType grid_type, std::string_view roi_name) const;

    std::size_t size() const { return cases_.size(); }

private:
    friend ExpectedResults LoadExpectedResults(const std::filesystem::path& path);
    std::map<std::string, ExpectedCase, std::less<>> cases_;
};

ExpectedResults LoadExpectedResults(const std::filesystem::path& path);
bool IsSupplementalLocalImage(std::string_view image);

// 返回空 optional 表示通过；非空字符串是面向 runner 的稳定失败原因。
std::optional<std::string> CompareExpectedCase(
    const ExpectedResults& expected,
    std::string_view image,
    GridType grid_type,
    std::string_view roi_name,
    const RecognitionResult& actual,
    bool allow_unexpected);

} // namespace iconrecognition::test
