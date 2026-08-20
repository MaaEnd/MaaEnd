#include "ZiplineStore.h"

#include <algorithm>
#include <ctime>
#include <fstream>
#include <system_error>

#include <meojson/json.hpp>

#include <MaaUtils/Logger.h>

#include "../utils.h"

namespace zipline
{

namespace
{

constexpr const char* kRecordFileName = "Ziplines.json";

} // namespace

std::string CurrentTimestamp()
{
    const std::time_t now = std::time(nullptr);
    std::tm tm {};
#ifdef _WIN32
    gmtime_s(&tm, &now);
#else
    gmtime_r(&now, &tm);
#endif

    char buf[32] = {};
    std::strftime(buf, sizeof(buf), "%Y-%m-%dT%H:%M:%SZ", &tm);
    return buf;
}

std::filesystem::path ZiplineStore::DefaultPath()
{
    return get_exe_dir() / ".." / "debug" / "record" / kRecordFileName;
}

bool ZiplineStore::load(const std::filesystem::path& path)
{
    maps_.clear();

    std::error_code ec;
    if (!std::filesystem::exists(path, ec)) {
        LogInfo << "ZiplineStore: no record yet" << VAR(path);
        return true;
    }

    std::ifstream ifs(path, std::ios::binary);
    if (!ifs) {
        LogError << "ZiplineStore: open failed" << VAR(path);
        return false;
    }

    const std::string raw((std::istreambuf_iterator<char>(ifs)), std::istreambuf_iterator<char>());
    if (raw.empty()) {
        LogInfo << "ZiplineStore: empty record" << VAR(path);
        return true;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        LogError << "ZiplineStore: record is not a json object" << VAR(path);
        return false;
    }

    const auto& root = parsed->as_object();
    if (!root.contains("maps") || !root.at("maps").is_array()) {
        LogWarn << "ZiplineStore: record has no maps array, treated as empty" << VAR(path);
        return true;
    }

    for (const auto& entry : root.at("maps").as_array()) {
        if (!entry.is_object()) {
            continue;
        }
        const auto& obj = entry.as_object();

        ZiplineMapRecord record;
        record.map_id = obj.get("map_id", std::string {});
        record.fetched_at = obj.get("fetched_at", std::string {});
        if (record.map_id.empty()) {
            continue;
        }

        if (obj.contains("marks") && obj.at("marks").is_array()) {
            for (const auto& mark_value : obj.at("marks").as_array()) {
                if (!mark_value.is_object()) {
                    continue;
                }
                const auto& mark_obj = mark_value.as_object();
                ZiplineMark mark;
                mark.template_id = mark_obj.get("template_id", std::string {});
                mark.level_id = mark_obj.get("level_id", std::string {});
                mark.x = mark_obj.get("x", 0.0);
                mark.y = mark_obj.get("y", 0.0);
                mark.z = mark_obj.get("z", 0.0);
                record.marks.push_back(std::move(mark));
            }
        }

        maps_.push_back(std::move(record));
    }

    LogInfo << "ZiplineStore: loaded" << VAR(path) << VAR(maps_.size());
    return true;
}

bool ZiplineStore::save(const std::filesystem::path& path) const
{
    std::error_code ec;
    std::filesystem::create_directories(path.parent_path(), ec);
    if (ec) {
        LogError << "ZiplineStore: create record dir failed" << VAR(path) << VAR(ec.message());
        return false;
    }

    json::array maps;
    for (const auto& record : maps_) {
        json::array marks;
        for (const auto& mark : record.marks) {
            json::object mark_obj;
            mark_obj["template_id"] = mark.template_id;
            mark_obj["level_id"] = mark.level_id;
            mark_obj["x"] = mark.x;
            mark_obj["y"] = mark.y;
            mark_obj["z"] = mark.z;
            marks.emplace_back(std::move(mark_obj));
        }

        json::object map_obj;
        map_obj["map_id"] = record.map_id;
        map_obj["fetched_at"] = record.fetched_at;
        map_obj["marks"] = std::move(marks);
        maps.emplace_back(std::move(map_obj));
    }

    json::object root;
    root["updated_at"] = CurrentTimestamp();
    root["maps"] = std::move(maps);

    // 先写临时文件再原子改名：导入中途崩掉不会把已有记录截成半截。
    const std::filesystem::path tmp = path.parent_path() / (path.filename().string() + ".tmp");
    {
        std::ofstream ofs(tmp, std::ios::binary | std::ios::trunc);
        if (!ofs) {
            LogError << "ZiplineStore: open temp for write failed" << VAR(tmp);
            return false;
        }
        ofs << json::value(std::move(root)).format();
        if (!ofs) {
            LogError << "ZiplineStore: write failed" << VAR(tmp);
            return false;
        }
    }

    std::filesystem::rename(tmp, path, ec);
    if (ec) {
        LogError << "ZiplineStore: rename temp into place failed" << VAR(tmp) << VAR(path) << VAR(ec.message());
        return false;
    }

    LogInfo << "ZiplineStore: saved" << VAR(path) << VAR(maps_.size());
    return true;
}

void ZiplineStore::replaceMap(ZiplineMapRecord record)
{
    auto it = std::find_if(maps_.begin(), maps_.end(), [&](const ZiplineMapRecord& e) { return e.map_id == record.map_id; });
    if (it == maps_.end()) {
        maps_.push_back(std::move(record));
        return;
    }
    *it = std::move(record);
}

} // namespace zipline
