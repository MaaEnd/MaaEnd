#include "RecastNavNoGoIO.h"

#include <algorithm>
#include <cmath>
#include <cstring>
#include <system_error>
#include <utility>

#include <meojson/json.hpp>

#include <MaaUtils/Platform.h>

namespace navmesh::recast
{

namespace
{

struct PolyJson
{
    std::vector<std::vector<double>> poly;
    std::string id;
    std::string tier; // 空 = 画在底图上。meojson 没有 optional, 用空串表达

    MEO_JSONIZATION(poly, MEO_OPT id, MEO_OPT tier);
};

struct FileJson
{
    int version = 0;
    std::unordered_map<std::string, std::vector<PolyJson>> zones;

    MEO_JSONIZATION(version, MEO_OPT zones);
};

}

bool NoGoPoly::contains(const WorldPoint& p) const
{
    if (p.x < bx0 || p.x > bx1 || p.y < by0 || p.y > by1) {
        return false;
    }
    // 奇偶规则: 手绘出来的自交环也有确定答案。
    bool in = false;
    const size_t n = ring.size();
    for (size_t i = 0, j = n - 1; i < n; j = i++) {
        const WorldPoint& a = ring[j];
        const WorldPoint& b = ring[i];
        if ((a.y > p.y) != (b.y > p.y) && p.x < a.x + (p.y - a.y) * (b.x - a.x) / (b.y - a.y)) {
            in = !in;
        }
    }
    return in;
}

bool NoGoTable::load(const std::filesystem::path& path, const BaseNavPack& pack, std::string& err)
{
    zones_.clear();
    std::error_code ec;
    const bool present = std::filesystem::exists(path, ec);
    if (ec) {
        err = "虚拟禁区表读不到 (" + MAA_NS::path_to_utf8_string(path) + "): " + ec.message();
        return false;
    }
    if (!present) {
        return true;
    }
    const auto doc = json::open(path, true, true);
    if (!doc.has_value() || !doc->is<FileJson>()) {
        err = "虚拟禁区表解不开 (" + MAA_NS::path_to_utf8_string(path) + ")";
        return false;
    }
    const auto file = doc->as<FileJson>();
    if (file.version != 1) {
        err = "虚拟禁区表版本不认识 (" + MAA_NS::path_to_utf8_string(path) + ")";
        return false;
    }
    for (const auto& [name, list] : file.zones) {
        std::vector<NoGoPoly> out;
        for (const PolyJson& src : list) {
            NoGoPoly p;
            p.id = src.id;
            for (const std::vector<double>& q : src.poly) {
                if (q.size() < 2) {
                    err = "区 " + name + " 的禁区里有不是两个数的点";
                    zones_.clear();
                    return false;
                }
                p.ring.push_back({ q[0], q[1] });
            }
            // 三个点才围得出面积。
            if (p.ring.size() < 3) {
                err = "区 " + name + " 的禁区点数不够";
                zones_.clear();
                return false;
            }
            if (!src.tier.empty()) {
                const BaseNavZone* tz = pack.findZoneByName(src.tier);
                if (tz == nullptr) {
                    err = "区 " + name + " 的禁区 " + p.id + " 画在包里没有的 tier " + src.tier + " 上";
                    zones_.clear();
                    return false;
                }
                p.tier = src.tier;
                // tier 没烘出地面高度就没有挑层的依据, 按底图口径盖整列。
                if (tz->floor_y > kBaseNavFloorYValidMin) {
                    p.banded = true;
                    p.y_lo = static_cast<double>(tz->floor_y) - static_cast<double>(kBaseNavFloorBand);
                    p.y_hi = static_cast<double>(tz->floor_y) + static_cast<double>(kBaseNavFloorBand);
                }
            }
            p.bx0 = p.ring[0].x;
            p.bx1 = p.ring[0].x;
            p.by0 = p.ring[0].y;
            p.by1 = p.ring[0].y;
            for (const WorldPoint& q : p.ring) {
                p.bx0 = std::min(p.bx0, q.x);
                p.bx1 = std::max(p.bx1, q.x);
                p.by0 = std::min(p.by0, q.y);
                p.by1 = std::max(p.by1, q.y);
            }
            out.push_back(std::move(p));
        }
        if (!out.empty()) {
            zones_.emplace(name, std::move(out));
        }
    }
    return true;
}

const std::vector<NoGoPoly>* NoGoTable::zone(const std::string& name) const
{
    const auto it = zones_.find(name);
    return it == zones_.end() ? nullptr : &it->second;
}

void StampNoGo(
    const std::vector<NoGoPoly>& polys,
    double x0,
    double y0,
    int64_t nx,
    int64_t ny,
    const Grid<float>& lh,
    Mask& core,
    Mask& lay,
    Grid<float>& dist)
{
    // 落在 [lo, hi] 里的格心对应格号 [ceil(lo/kCS - 0.5), floor(hi/kCS - 0.5)], 再截到窗口内。
    const auto span = [](double lo, double hi, double org, int64_t n, int64_t& c0, int64_t& c1) {
        c0 = static_cast<int64_t>(std::clamp(std::ceil((lo - org) / kCS - 0.5), 0.0, static_cast<double>(n)));
        c1 = static_cast<int64_t>(std::clamp(std::floor((hi - org) / kCS - 0.5), -1.0, static_cast<double>(n - 1)));
        return c0 <= c1;
    };
    for (const NoGoPoly& p : polys) {
        int64_t gx0 = 0;
        int64_t gx1 = 0;
        int64_t gy0 = 0;
        int64_t gy1 = 0;
        if (!span(p.bx0, p.bx1, x0, nx, gx0, gx1) || !span(p.by0, p.by1, y0, ny, gy0, gy1)) {
            continue;
        }
        for (int64_t gy = gy0; gy <= gy1; ++gy) {
            const double cy = y0 + (static_cast<double>(gy) + 0.5) * kCS;
            for (int64_t gx = gx0; gx <= gx1; ++gx) {
                const auto cell = static_cast<size_t>(gy * nx + gx);
                if (p.banded) {
                    const double lf = static_cast<double>(lh.v[cell]);
                    if (std::isnan(lf) || lf < p.y_lo || lf > p.y_hi) {
                        continue;
                    }
                }
                if (!p.contains({ x0 + (static_cast<double>(gx) + 0.5) * kCS, cy })) {
                    continue;
                }
                core.v[cell] = 0;
                lay.v[cell] = 0;
                dist.v[cell] = 0.0F;
            }
        }
    }
}

bool NoGoContains(const std::vector<NoGoPoly>& polys, const WorldPoint& p, double h, std::string& hit_id)
{
    for (const NoGoPoly& z : polys) {
        if (z.banded && (h < z.y_lo || h > z.y_hi)) {
            continue;
        }
        if (z.contains(p)) {
            hit_id = z.id;
            return true;
        }
    }
    return false;
}

}
