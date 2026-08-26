#pragma once

#include <filesystem>
#include <map>
#include <mutex>
#include <string>
#include <vector>

#include "TemplateTypes.h"

namespace iconrecognition::detail
{

std::filesystem::path ResolveIconPath(const std::filesystem::path& image_root, const std::string& icon_id);

class TemplateCatalog
{
public:
    TemplateCatalog(std::filesystem::path data_root, std::filesystem::path image_root = {});

    bool initialize();

    const std::vector<TemplateRecord>& records() const { return records_; }

    const std::vector<PreparedTemplate>& load(int target_size);
    const std::vector<PreparedTemplate>& loadRegionUnavailable(int target_size);

private:
    bool InitializeUnlocked();
    const std::vector<PreparedTemplate>& loadUnlocked(int target_size);
    std::filesystem::path data_root_;
    std::filesystem::path image_root_;
    std::vector<TemplateRecord> records_;
    std::map<int, std::vector<PreparedTemplate>> cache_;
    std::map<int, std::vector<PreparedTemplate>> region_unavailable_cache_;
    cv::Mat region_unavailable_background_;
    cv::Mat region_unavailable_mark_;
    std::mutex mutex_;
    bool initialized_ = false;
};

} // namespace iconrecognition::detail
