#include "TemplateCatalog.h"

#include <set>
#include <stdexcept>

#include <meojson/json.hpp>

#include "CompositeIcon.h"

namespace iconrecognition::detail
{
namespace
{

// 透明度低于该值的像素不参与模板匹配；调高可抑制半透明边缘噪声，调低可保留更多细节。
constexpr int kTemplateAlphaThreshold = 230;

} // namespace

TemplateCatalog::TemplateCatalog(std::filesystem::path data_root, std::filesystem::path image_root)
    : data_root_(std::move(data_root))
    , image_root_(image_root.empty() ? data_root_ / "images" : std::move(image_root))
{
}

bool TemplateCatalog::initialize()
{
    const std::lock_guard lock(mutex_);
    return InitializeUnlocked();
}

bool TemplateCatalog::InitializeUnlocked()
{
    records_.clear();
    cache_.clear();
    const auto parsed = json::open((data_root_ / "recognition_items.json").string());
    if (!parsed || !parsed->is_object()) {
        throw std::runtime_error("recognition_items.json must be an object");
    }
    const std::set<std::string> expected { "name", "category", "storageKind", "categoryType", "rarity", "iconId", "fluidIconId" };
    const std::set<std::string> optional { "sortId1", "sortId2" };
    const auto is_integer = [](const json::value& value) {
        return value.is_number() && value.as_double() == static_cast<double>(value.as_integer());
    };
    for (const auto& [item_id, value] : parsed->as_object()) {
        if (item_id.empty() || !value.is_object()) {
            throw std::runtime_error("invalid recognition catalog item: " + item_id);
        }
        const auto& object = value.as_object();
        const bool has_sort_id1 = object.contains("sortId1");
        const bool has_sort_id2 = object.contains("sortId2");
        if (has_sort_id1 != has_sort_id2) {
            throw std::runtime_error("catalog fields mismatch: " + item_id);
        }
        for (const auto& field : expected) {
            if (!object.contains(field)) {
                throw std::runtime_error("catalog field missing: " + item_id + "." + field);
            }
        }
        for (const auto& [field, field_value] : object) {
            if (!expected.contains(field) && !optional.contains(field)) {
                throw std::runtime_error("catalog field unknown: " + item_id + "." + field);
            }
            if (optional.contains(field) && !is_integer(field_value)) {
                throw std::runtime_error("catalog sort field invalid: " + item_id + "." + field);
            }
        }
        const auto get_string = [&](const char* key) {
            if (!object.at(key).is_string() || object.at(key).as_string().empty()) {
                throw std::runtime_error("catalog string invalid: " + item_id + "." + key);
            }
            return object.at(key).as_string();
        };
        const int rarity = object.at("rarity").is_number() ? object.at("rarity").as_integer() : 0;
        if (rarity < 1 || rarity > 6) {
            throw std::runtime_error("catalog rarity invalid: " + item_id);
        }
        const std::string icon_id = get_string("iconId");
        if (icon_id.find_first_of("/\\") != std::string::npos) {
            throw std::runtime_error("catalog icon path invalid: " + item_id);
        }
        const std::string fluid_icon_id = object.at("fluidIconId").is_string()
                                              ? object.at("fluidIconId").as_string()
                                              : throw std::runtime_error("catalog fluidIconId invalid: " + item_id);
        records_.push_back(TemplateRecord {
            item_id,
            "iconRecognition.name." + item_id,
            get_string("category"),
            get_string("storageKind"),
            get_string("categoryType"),
            rarity,
            icon_id,
            fluid_icon_id,
        });
    }
    if (records_.empty()) {
        throw std::runtime_error("recognition catalog is empty");
    }
    initialized_ = true;
    return true;
}

std::filesystem::path ResolveIconPath(const std::filesystem::path& image_root, const std::string& icon_id)
{
    std::filesystem::path found;
    for (const auto& directory : std::filesystem::directory_iterator(image_root)) {
        if (!directory.is_directory()) {
            continue;
        }
        const auto path = directory.path() / (icon_id + ".png");
        if (!std::filesystem::is_regular_file(path)) {
            continue;
        }
        if (!found.empty()) {
            throw std::runtime_error("icon id exists in multiple rarity folders: " + icon_id);
        }
        found = path;
    }
    if (found.empty()) {
        throw std::runtime_error("icon image not found: " + icon_id);
    }
    return found;
}

const std::vector<PreparedTemplate>& TemplateCatalog::load(int target_size)
{
    const std::lock_guard lock(mutex_);
    if (!initialized_) {
        InitializeUnlocked();
    }
    if (target_size <= 0) {
        throw std::invalid_argument("template size must be positive");
    }
    if (const auto it = cache_.find(target_size); it != cache_.end()) {
        return it->second;
    }
    std::vector<PreparedTemplate> result;
    result.reserve(records_.size());
    for (const auto& record : records_) {
        const auto base_path = image_root_ / std::to_string(record.rarity) / (record.icon_id + ".png");
        const cv::Mat base = DecodeBgra(base_path);
        if (record.fluid_icon_id.empty()) {
            result.push_back(PrepareStandardTemplate(record, base, target_size, kTemplateAlphaThreshold));
        }
        else {
            result.push_back(BuildCompositeIcon(
                record,
                base,
                DecodeBgra(ResolveIconPath(image_root_, record.fluid_icon_id)),
                target_size,
                kTemplateAlphaThreshold));
        }
    }
    return cache_.emplace(target_size, std::move(result)).first->second;
}

} // namespace iconrecognition::detail
