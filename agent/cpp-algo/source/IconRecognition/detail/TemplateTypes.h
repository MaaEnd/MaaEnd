#pragma once

#include <filesystem>
#include <optional>
#include <string>
#include <vector>

#include <MaaUtils/NoWarningCV.hpp>

namespace iconrecognition::detail
{

struct TemplateAlias
{
    std::string item_id;
    std::string name_key;
};

struct TemplateRecord
{
    std::string item_id;
    std::string name_key;
    std::string category;
    std::string storage_kind;
    std::string category_type;
    int rarity = 0;
    std::string icon_id;
    std::string fluid_icon_id;
    std::optional<int> sort_id_1;
    std::optional<int> sort_id_2;
    bool region_restricted = false;
    std::vector<TemplateAlias> aliases;
};

struct PreparedTemplate
{
    TemplateRecord record;
    cv::Mat image;
    cv::Mat mask;
    bool composite = false;
    bool region_unavailable = false;
};

cv::Mat DecodeBgra(const std::filesystem::path& path);
cv::Mat ResizeAndCenter(const cv::Mat& source, int target_size);
PreparedTemplate PrepareStandardTemplate(const TemplateRecord& record, const cv::Mat& source, int target_size, int alpha_threshold);
PreparedTemplate PrepareCompositeTemplate(
    const TemplateRecord& record,
    const cv::Mat& base,
    const cv::Mat& content,
    int target_size,
    int alpha_threshold);

} // namespace iconrecognition::detail
