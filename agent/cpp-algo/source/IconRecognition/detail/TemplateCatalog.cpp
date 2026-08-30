#include "TemplateCatalog.h"

#include <set>
#include <stdexcept>

#include <meojson/json.hpp>

#include "Common/JsoncFile.h"
#include "CompositeIcon.h"
#include "DisabledIcon.h"

namespace iconrecognition::detail
{
namespace
{

// 透明度低于该值的像素不参与模板匹配；调高可抑制半透明边缘噪声，调低可保留更多细节。
constexpr int kTemplateAlphaThreshold = 230;
// 地区不可用横条源素材在 128px 图标基准下的像素宽高；加载时严格校验，避免误用裁剪或缩放后的资源。
constexpr int kRegionUnavailableBandSourceWidth = 120;
constexpr int kRegionUnavailableBandSourceHeight = 28;
// 地区不可用白色标识源素材在 128px 图标基准下的像素宽高。
constexpr int kRegionUnavailableMarkSourceWidth = 24;
constexpr int kRegionUnavailableMarkSourceHeight = 24;

cv::Mat DecodeDisabledOverlay(const std::filesystem::path& path, const cv::Size& expected_size)
{
    if (!std::filesystem::is_regular_file(path)) {
        throw std::runtime_error("disabled overlay not found: " + path.string());
    }
    const cv::Mat overlay = cv::imread(path.string(), cv::IMREAD_UNCHANGED);
    if (overlay.empty()) {
        throw std::runtime_error("unable to decode disabled overlay: " + path.string());
    }
    if (overlay.type() != CV_8UC4 || overlay.size() != expected_size) {
        throw std::runtime_error(
            "disabled overlay must be a " + std::to_string(expected_size.width) + "x" + std::to_string(expected_size.height)
            + " BGRA image: " + path.string());
    }
    return overlay;
}

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
    region_unavailable_cache_.clear();
    region_unavailable_background_.release();
    region_unavailable_mark_.release();
    const auto parsed = common::OpenJsoncFile(data_root_ / "recognition_items.json");
    if (!parsed || !parsed->is_object()) {
        throw std::runtime_error("recognition_items.json must be an object");
    }
    const std::set<std::string> expected { "name", "category", "storageKind", "categoryType", "rarity", "iconId", "fluidIconId" };
    const std::set<std::string> optional_sort { "sortId1", "sortId2" };
    const std::set<std::string> optional_boolean { "regionRestricted" };
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
            if (!expected.contains(field) && !optional_sort.contains(field) && !optional_boolean.contains(field)) {
                throw std::runtime_error("catalog field unknown: " + item_id + "." + field);
            }
            if (optional_sort.contains(field) && !is_integer(field_value)) {
                throw std::runtime_error("catalog sort field invalid: " + item_id + "." + field);
            }
            if (optional_boolean.contains(field) && !field_value.is_boolean()) {
                throw std::runtime_error("catalog boolean field invalid: " + item_id + "." + field);
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
            object.contains("regionRestricted") && object.at("regionRestricted").as_boolean(),
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
    return loadUnlocked(target_size);
}

const std::vector<PreparedTemplate>& TemplateCatalog::loadUnlocked(int target_size)
{
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

const std::vector<PreparedTemplate>& TemplateCatalog::loadRegionUnavailable(int target_size)
{
    const std::lock_guard lock(mutex_);
    if (const auto it = region_unavailable_cache_.find(target_size); it != region_unavailable_cache_.end()) {
        return it->second;
    }
    const auto& base_templates = loadUnlocked(target_size);
    if (region_unavailable_background_.empty() || region_unavailable_mark_.empty()) {
        const auto overlay_root = image_root_ / "Overlay";
        cv::Mat background = DecodeDisabledOverlay(
            overlay_root / "icon_placement_disabled_bg.png",
            cv::Size(kRegionUnavailableBandSourceWidth, kRegionUnavailableBandSourceHeight));
        cv::Mat mark = DecodeDisabledOverlay(
            overlay_root / "icon_placement_disabled.png",
            cv::Size(kRegionUnavailableMarkSourceWidth, kRegionUnavailableMarkSourceHeight));
        region_unavailable_background_ = std::move(background);
        region_unavailable_mark_ = std::move(mark);
    }

    std::vector<PreparedTemplate> result;
    for (const auto& base : base_templates) {
        if (base.record.region_restricted) {
            result.push_back(
                BuildRegionUnavailableTemplate(base, region_unavailable_background_, region_unavailable_mark_, kTemplateAlphaThreshold));
        }
    }
    return region_unavailable_cache_.emplace(target_size, std::move(result)).first->second;
}

} // namespace iconrecognition::detail
