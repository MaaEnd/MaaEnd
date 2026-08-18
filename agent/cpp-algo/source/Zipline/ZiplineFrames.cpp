#include "ZiplineFrames.h"

#include <algorithm>
#include <fstream>
#include <system_error>

#include <meojson/json.hpp>

#include <MaaUtils/Logger.h>

#include "../utils.h"

namespace zipline
{

namespace
{

// 非图像的模块数据统一放 data/<模块>/。标定的消费者是 MapNavigator 的寻路规划，
// 归在它名下；resource/model 是另一个仓库的子模块，本仓库的配置不能落进去。
constexpr const char* kFramesRelativePath = "data/MapNavigator/zipline_frames.json";

} // namespace

ZiplineNode ZiplineFrame::project(const ZiplineMark& mark) const
{
    return ZiplineNode {
        .x = plane[0] * mark.x + plane[1] * mark.z + plane[2],
        .y = plane[3] * mark.x + plane[4] * mark.z + plane[5],
        .height = height_scale * mark.y + height_offset,
        .world_x = mark.x,
        .world_y = mark.y,
        .world_z = mark.z,
        .template_id = mark.template_id,
        .level_id = mark.level_id,
    };
}

bool ZiplineFrame::accepts(const ZiplineMark& mark) const
{
    if (template_ids.empty()) {
        return true;
    }
    return std::find(template_ids.begin(), template_ids.end(), mark.template_id) != template_ids.end();
}

std::filesystem::path ZiplineFrames::DefaultPath()
{
    return get_exe_dir() / ".." / kFramesRelativePath;
}

bool ZiplineFrames::load(const std::filesystem::path& path)
{
    frames_.clear();
    types_.clear();
    power_sources_.clear();
    cost_ = ZiplineCostModel {};

    std::error_code ec;
    if (!std::filesystem::exists(path, ec)) {
        LogInfo << "ZiplineFrames: no calibration, zipline routing stays off" << VAR(path);
        return true;
    }

    std::ifstream ifs(path, std::ios::binary);
    if (!ifs) {
        LogError << "ZiplineFrames: open failed" << VAR(path);
        return false;
    }

    const std::string raw((std::istreambuf_iterator<char>(ifs)), std::istreambuf_iterator<char>());
    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        LogError << "ZiplineFrames: not a json object" << VAR(path);
        return false;
    }

    const auto& root = parsed->as_object();
    if (root.contains("cost") && root.at("cost").is_object()) {
        const auto& cost = root.at("cost").as_object();
        cost_.speed_ratio = cost.get("speed_ratio", cost_.speed_ratio);
        cost_.mount_penalty = cost.get("mount_penalty", cost_.mount_penalty);
        // 换乘开销是链上每一跳的边权，取负会让最短链求解本身失去意义，先夹住再用。
        cost_.transfer_penalty = std::max(0.0, cost.get("transfer_penalty", cost_.transfer_penalty));
        cost_.min_gain = std::max(0.0, cost.get("min_gain", cost_.min_gain));
        cost_.reach_radius = std::max(0.0, cost.get("reach_radius", cost_.reach_radius));
    }

    if (root.contains("types") && root.at("types").is_array()) {
        for (const auto& entry : root.at("types").as_array()) {
            if (!entry.is_object()) {
                continue;
            }
            const auto& obj = entry.as_object();

            ZiplineType type;
            type.template_id = obj.get("template_id", std::string {});
            type.name = obj.get("name", std::string {});
            type.max_span = obj.get("max_span", 0.0);
            // 缺 template_id 就无从对号，跨度非正等于这类索一条都配不出来，两者都没有留着的意义。
            if (type.template_id.empty() || type.max_span <= 0.0) {
                LogWarn << "ZiplineFrames: type without a usable template_id/max_span, skipped" << VAR(type.name);
                continue;
            }
            types_.push_back(std::move(type));
        }
    }

    if (root.contains("power") && root.at("power").is_object()) {
        const auto& power = root.at("power").as_object();
        if (power.contains("sources") && power.at("sources").is_array()) {
            for (const auto& entry : power.at("sources").as_array()) {
                if (!entry.is_object()) {
                    continue;
                }
                const auto& obj = entry.as_object();

                ZiplinePowerSource source;
                source.template_id = obj.get("template_id", std::string {});
                source.name = obj.get("name", std::string {});
                source.radius = obj.get("radius", 0.0);
                // 缺 template_id 就无从对号，半径非正等于这类结构供不了电，两者都没有留着的意义。
                if (source.template_id.empty() || source.radius <= 0.0) {
                    LogWarn << "ZiplineFrames: power source without a usable template_id/radius, skipped" << VAR(source.name);
                    continue;
                }
                power_sources_.push_back(std::move(source));
            }
        }
    }
    if (power_sources_.empty()) {
        LogWarn << "ZiplineFrames: no power sources listed, the powered-only filter stays off";
    }

    if (!root.contains("frames") || !root.at("frames").is_array()) {
        LogWarn << "ZiplineFrames: no frames array" << VAR(path);
        return true;
    }

    for (const auto& entry : root.at("frames").as_array()) {
        if (!entry.is_object()) {
            continue;
        }
        const auto& obj = entry.as_object();

        ZiplineFrame frame;
        frame.map_id = obj.get("map_id", std::string {});
        frame.zone_name = obj.get("zone_name", std::string {});
        if (frame.zone_name.empty()) {
            LogWarn << "ZiplineFrames: frame without zone_name, skipped";
            continue;
        }

        // 缺 plane 就等于没标定过平面，直接丢掉这条：默认的单位仿射会让滑索落在错误的位置上。
        if (!obj.contains("plane") || !obj.at("plane").is_array() || obj.at("plane").as_array().size() != frame.plane.size()) {
            LogWarn << "ZiplineFrames: frame without a 6-term plane affine, skipped" << VAR(frame.zone_name);
            continue;
        }
        const auto& plane = obj.at("plane").as_array();
        for (size_t i = 0; i < frame.plane.size(); ++i) {
            if (!plane[i].is_number()) {
                LogWarn << "ZiplineFrames: plane affine has a non-number term, skipped" << VAR(frame.zone_name);
                frame.zone_name.clear();
                break;
            }
            frame.plane[i] = plane[i].as_double();
        }
        if (frame.zone_name.empty()) {
            continue;
        }

        frame.height_scale = obj.get("height_scale", frame.height_scale);
        frame.height_offset = obj.get("height_offset", frame.height_offset);

        if (obj.contains("template_ids") && obj.at("template_ids").is_array()) {
            for (const auto& item : obj.at("template_ids").as_array()) {
                if (item.is_string()) {
                    frame.template_ids.push_back(item.as_string());
                }
            }
        }

        frames_.push_back(std::move(frame));
    }

    LogInfo << "ZiplineFrames: loaded" << VAR(path) << VAR(frames_.size()) << VAR(types_.size()) << VAR(power_sources_.size());
    return true;
}

const ZiplineFrame* ZiplineFrames::findByZone(const std::string& zone_name) const
{
    auto it = std::find_if(frames_.begin(), frames_.end(), [&](const ZiplineFrame& f) { return f.zone_name == zone_name; });
    return it == frames_.end() ? nullptr : &*it;
}

double ZiplineFrames::maxSpan(const std::string& template_id) const
{
    auto it = std::find_if(types_.begin(), types_.end(), [&](const ZiplineType& t) { return t.template_id == template_id; });
    return it == types_.end() ? 0.0 : it->max_span;
}

double ZiplineFrames::supplyRadius(const std::string& template_id) const
{
    auto it =
        std::find_if(power_sources_.begin(), power_sources_.end(), [&](const ZiplinePowerSource& s) { return s.template_id == template_id; });
    return it == power_sources_.end() ? 0.0 : it->radius;
}

} // namespace zipline
