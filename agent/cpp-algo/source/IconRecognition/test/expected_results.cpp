#include "expected_results.h"

#include <algorithm>
#include <array>
#include <cctype>
#include <charconv>
#include <fstream>
#include <sstream>
#include <stdexcept>
#include <vector>

namespace iconrecognition::test
{
namespace
{

constexpr std::array<std::string_view, 4> kExpectedHeader { "image", "roi", "item_id", "count" };

std::string CaseKey(std::string_view image, GridType grid_type, std::string_view roi_name)
{
    return std::string(image) + "|" + std::string(GridTypeName(grid_type)) + "|" + std::string(roi_name);
}

std::vector<std::string> ParseCsvRow(std::string_view line, std::size_t line_number)
{
    std::vector<std::string> fields;
    std::string field;
    bool quoted = false;
    bool quote_closed = false;
    for (std::size_t index = 0; index < line.size(); ++index) {
        const char value = line[index];
        if (quoted) {
            if (value == '"') {
                if (index + 1 < line.size() && line[index + 1] == '"') {
                    field.push_back('"');
                    ++index;
                }
                else {
                    quoted = false;
                    quote_closed = true;
                }
            }
            else {
                field.push_back(value);
            }
            continue;
        }
        if (quote_closed && value != ',') {
            throw std::runtime_error("unexpected character after closing quote at CSV line " + std::to_string(line_number));
        }
        if (value == ',') {
            fields.push_back(std::move(field));
            field.clear();
            quote_closed = false;
        }
        else if (value == '"') {
            if (!field.empty()) {
                throw std::runtime_error("unexpected quote at CSV line " + std::to_string(line_number));
            }
            quoted = true;
        }
        else {
            field.push_back(value);
        }
    }
    if (quoted) {
        throw std::runtime_error("unterminated quote at CSV line " + std::to_string(line_number));
    }
    fields.push_back(std::move(field));
    return fields;
}

int ParseInt(std::string_view value, std::string_view context)
{
    int parsed = 0;
    const auto [end, error] = std::from_chars(value.data(), value.data() + value.size(), parsed);
    if (error != std::errc {} || end != value.data() + value.size()) {
        throw std::runtime_error(std::string(context) + " must be an integer");
    }
    return parsed;
}

std::size_t ParseCount(std::string_view value, std::size_t line_number)
{
    std::size_t parsed = 0;
    const auto [end, error] = std::from_chars(value.data(), value.data() + value.size(), parsed);
    if (error != std::errc {} || end != value.data() + value.size()) {
        throw std::runtime_error("count must be a non-negative integer at CSV line " + std::to_string(line_number));
    }
    return parsed;
}

cv::Rect ParseRoi(std::string_view value, std::size_t line_number)
{
    if (value.size() < 9 || value.front() != '[' || value.back() != ']') {
        throw std::runtime_error("roi must use [x,y,width,height] at CSV line " + std::to_string(line_number));
    }
    value.remove_prefix(1);
    value.remove_suffix(1);
    std::array<int, 4> parts {};
    for (std::size_t index = 0; index < parts.size(); ++index) {
        const std::size_t separator = value.find(',');
        if ((index + 1 < parts.size() && separator == std::string_view::npos)
            || (index + 1 == parts.size() && separator != std::string_view::npos)) {
            throw std::runtime_error("roi must contain four integers at CSV line " + std::to_string(line_number));
        }
        const std::string_view part = index + 1 < parts.size() ? value.substr(0, separator) : value;
        parts[index] = ParseInt(part, "roi at CSV line " + std::to_string(line_number));
        if (index + 1 < parts.size()) {
            value.remove_prefix(separator + 1);
        }
    }
    const cv::Rect roi(parts[0], parts[1], parts[2], parts[3]);
    if (roi.x < 0 || roi.y < 0 || roi.width <= 0 || roi.height <= 0) {
        throw std::runtime_error("roi must be a positive in-frame rectangle at CSV line " + std::to_string(line_number));
    }
    return roi;
}

std::pair<GridType, std::string> ParseCaseLocation(std::string_view image, std::size_t line_number)
{
    std::vector<std::string> components;
    for (const auto& component : std::filesystem::path(image)) {
        components.push_back(component.string());
    }
    if (components.size() < 2) {
        throw std::runtime_error("image must include its grid directory at CSV line " + std::to_string(line_number));
    }
    const auto grid_type = ParseGridType(components.front());
    if (!grid_type) {
        throw std::runtime_error("unknown grid type at CSV line " + std::to_string(line_number) + ": " + components.front());
    }
    if (*grid_type == GridType::SingleRoi) {
        if (components.size() < 3) {
            throw std::runtime_error("single_roi image must include its ROI directory at CSV line " + std::to_string(line_number));
        }
        return { *grid_type, components[1] };
    }
    return { *grid_type, "full" };
}

std::map<std::string, std::size_t, std::less<>> CountActualItems(const RecognitionResult& actual)
{
    std::map<std::string, std::size_t, std::less<>> counts;
    for (const auto& match : actual.matches) {
        ++counts[match.item.item_id];
    }
    return counts;
}

std::string DescribeCounts(const std::map<std::string, std::size_t, std::less<>>& counts)
{
    std::ostringstream output;
    bool first = true;
    for (const auto& [item_id, count] : counts) {
        output << (first ? "" : ",") << item_id << ':' << count;
        first = false;
    }
    return output.str();
}

std::string DescribeCase(std::string_view image, GridType grid_type, std::string_view roi_name)
{
    return std::string(GridTypeName(grid_type)) + "/" + std::string(roi_name) + "/" + std::string(image);
}

} // namespace

const ExpectedCase* ExpectedResults::find(std::string_view image, GridType grid_type, std::string_view roi_name) const
{
    const auto found = cases_.find(CaseKey(image, grid_type, roi_name));
    return found == cases_.end() ? nullptr : &found->second;
}

ExpectedResults LoadExpectedResults(const std::filesystem::path& path)
{
    std::ifstream stream(path, std::ios::binary);
    if (!stream) {
        throw std::runtime_error("unable to read expected results: " + path.string());
    }
    ExpectedResults result;
    std::string line;
    std::size_t line_number = 0;
    while (std::getline(stream, line)) {
        ++line_number;
        if (!line.empty() && line.back() == '\r') {
            line.pop_back();
        }
        if (line.empty()) {
            continue;
        }
        auto fields = ParseCsvRow(line, line_number);
        if (line_number == 1) {
            constexpr std::string_view kUtf8Bom = "\xEF\xBB\xBF";
            if (!fields.empty() && fields.front().starts_with(kUtf8Bom)) {
                fields.front().erase(0, kUtf8Bom.size());
            }
            if (!std::ranges::equal(fields, kExpectedHeader)) {
                throw std::runtime_error("expected CSV header must be image,roi,item_id,count");
            }
            continue;
        }
        if (fields.size() != kExpectedHeader.size()) {
            throw std::runtime_error("expected four CSV fields at line " + std::to_string(line_number));
        }
        const std::string& image = fields[0];
        const cv::Rect roi = ParseRoi(fields[1], line_number);
        const auto [grid_type, roi_name] = ParseCaseLocation(image, line_number);
        const std::string key = CaseKey(image, grid_type, roi_name);
        auto [case_iterator, inserted] =
            result.cases_.try_emplace(key, ExpectedCase { .image = image, .grid_type = grid_type, .roi_name = roi_name, .roi = roi });
        ExpectedCase& expected_case = case_iterator->second;
        if (!inserted && expected_case.roi != roi) {
            throw std::runtime_error("inconsistent ROI for expected case: " + key);
        }
        const std::size_t count = ParseCount(fields[3], line_number);
        if (fields[2].empty()) {
            if (count != 0 || expected_case.explicit_zero || !expected_case.item_counts.empty()) {
                throw std::runtime_error("zero-match case must contain one empty item row: " + key);
            }
            expected_case.explicit_zero = true;
            continue;
        }
        if (count == 0 || expected_case.explicit_zero) {
            throw std::runtime_error("matched item count must be positive: " + key);
        }
        if (!expected_case.item_counts.emplace(fields[2], count).second) {
            throw std::runtime_error("duplicate item_id for expected case: " + key + "/" + fields[2]);
        }
    }
    if (line_number == 0) {
        throw std::runtime_error("expected CSV is empty: " + path.string());
    }
    if (result.cases_.empty()) {
        throw std::runtime_error("expected CSV contains no cases: " + path.string());
    }
    return result;
}

bool IsSupplementalLocalImage(std::string_view image)
{
    const std::filesystem::path path(image);
    std::string extension = path.extension().string();
    std::ranges::transform(extension, extension.begin(), [](unsigned char value) { return static_cast<char>(std::tolower(value)); });
    if (extension != ".png") {
        return false;
    }
    const std::string stem = path.stem().string();
    const std::size_t marker = stem.rfind(".local");
    if (marker == std::string::npos) {
        return false;
    }
    const std::string_view index(stem.data() + marker + std::string_view(".local").size(), stem.size() - marker - 6);
    return !index.empty() && std::ranges::all_of(index, [](unsigned char value) { return std::isdigit(value) != 0; });
}

std::optional<std::string> CompareExpectedCase(
    const ExpectedResults& expected,
    std::string_view image,
    GridType grid_type,
    std::string_view roi_name,
    const RecognitionResult& actual,
    bool allow_unexpected)
{
    const std::string description = DescribeCase(image, grid_type, roi_name);
    const ExpectedCase* expected_case = expected.find(image, grid_type, roi_name);
    if (expected_case == nullptr) {
        return allow_unexpected ? std::nullopt : std::optional("missing expected case: " + description);
    }
    const auto actual_counts = CountActualItems(actual);
    if (expected_case->item_counts != actual_counts) {
        return "item mismatch: " + description + " expected=" + DescribeCounts(expected_case->item_counts)
               + " actual=" + DescribeCounts(actual_counts);
    }
    return std::nullopt;
}

} // namespace iconrecognition::test
