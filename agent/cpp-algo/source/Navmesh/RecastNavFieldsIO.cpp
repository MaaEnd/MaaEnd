#include "RecastNavFieldsIO.h"

#include <algorithm>
#include <cstring>
#include <mutex>
#include <tuple>
#include <unordered_map>

#include <MaaUtils/Platform.h>

#include "BaseNavReader.h"
#include "RecastNavGrid.h"
#include "RecastNavZone.h"

namespace navmesh::recast
{

namespace
{

constexpr size_t kHeaderSize = 48;
constexpr size_t kSectionEntrySize = 24;
constexpr size_t kConstSize = 144;

// 须与 StampWalls 里写死的采样与包围盒取值一致
constexpr double kWallSampleSub = 0.4;
constexpr double kHitSampleSub = 0.2;
constexpr double kWallBBoxPad = 4.0;

uint16_t peek_u16(const uint8_t* p)
{
    uint16_t v = 0;
    std::memcpy(&v, p, sizeof v);
    return v;
}

uint32_t peek_u32(const uint8_t* p)
{
    uint32_t v = 0;
    std::memcpy(&v, p, sizeof v);
    return v;
}

uint64_t peek_u64(const uint8_t* p)
{
    uint64_t v = 0;
    std::memcpy(&v, p, sizeof v);
    return v;
}

int32_t peek_i32(const uint8_t* p)
{
    int32_t v = 0;
    std::memcpy(&v, p, sizeof v);
    return v;
}

float peek_f32(const uint8_t* p)
{
    float v = 0.0F;
    std::memcpy(&v, p, sizeof v);
    return v;
}

// FLNK 段: 头 16 B, 区索引 zone_count × 12 B, 记录 count × 80 B
constexpr size_t kLinkHeaderSize = 16;
constexpr size_t kLinkZoneSize = 12;
constexpr size_t kLinkRecSize = 80;
constexpr uint16_t kLinkRecVersion = 1;

FieldsLinkSide peek_link_side(const uint8_t* p)
{
    FieldsLinkSide s;
    s.gx = peek_i32(p);
    s.gy = peek_i32(p + 4);
    s.h = peek_f32(p + 8);
    s.decl_h = peek_f32(p + 12);
    s.rid = peek_u32(p + 16);
    s.clr = peek_u16(p + 20);
    s.flags = p[22];
    s.why = p[23];
    return s;
}

bool parse_links(std::vector<FieldsLinkRec>* recs, std::vector<FieldsLinkZone>* zones, std::string* err, const uint8_t* p, size_t len)
{
    if (len < kLinkHeaderSize) {
        *err = "旁包离网连接段残缺";
        return false;
    }
    const uint32_t count = peek_u32(p);
    const uint16_t ver = peek_u16(p + 4);
    const uint32_t zone_count = peek_u32(p + 8);
    if (ver != kLinkRecVersion) {
        *err = "旁包离网连接记录版本不符 (" + std::to_string(ver) + " ≠ " + std::to_string(kLinkRecVersion) + ")";
        return false;
    }
    const uint64_t want = kLinkHeaderSize + static_cast<uint64_t>(zone_count) * kLinkZoneSize + static_cast<uint64_t>(count) * kLinkRecSize;
    if (want != len) {
        *err = "旁包离网连接段长度与条数不符";
        return false;
    }
    const uint8_t* zp = p + kLinkHeaderSize;
    const uint8_t* rp = zp + static_cast<size_t>(zone_count) * kLinkZoneSize;
    zones->resize(zone_count);
    for (uint32_t i = 0; i < zone_count; ++i) {
        const uint8_t* e = zp + static_cast<size_t>(i) * kLinkZoneSize;
        FieldsLinkZone& z = (*zones)[i];
        z.zone_id = peek_u16(e);
        z.first = peek_u32(e + 4);
        z.len = peek_u32(e + 8);
        if (z.first > count || z.len > count - z.first) {
            *err = "旁包离网连接区索引越界";
            return false;
        }
    }
    recs->resize(count);
    for (uint32_t i = 0; i < count; ++i) {
        const uint8_t* e = rp + static_cast<size_t>(i) * kLinkRecSize;
        FieldsLinkRec& r = (*recs)[i];
        r.boml_index = peek_u32(e);
        r.zone_id = peek_u16(e + 4);
        r.kind = e[6];
        r.valid = e[7];
        r.is_ext = peek_i32(e + 8);
        r.bidirectional = peek_i32(e + 12);
        r.area = peek_i32(e + 16);
        r.link_type = peek_u16(e + 20);
        r.direction = e[22];
        r.radius = peek_f32(e + 24);
        r.cost_modifier = peek_f32(e + 28);
        r.lo = peek_link_side(e + 32);
        r.hi = peek_link_side(e + 56);
    }
    return true;
}

// FROP 段: 头 16 B, 区索引 zone_count × 12 B, 记录 count × 64 B
constexpr size_t kRopeHeaderSize = 16;
constexpr size_t kRopeZoneSize = 12;
constexpr size_t kRopeRecSize = 64;
constexpr uint16_t kRopeRecVersion = 1;

bool parse_ropes(std::vector<FieldsRopeRec>* recs, std::vector<FieldsLinkZone>* zones, std::string* err, const uint8_t* p, size_t len)
{
    if (len < kRopeHeaderSize) {
        *err = "旁包升降索段残缺";
        return false;
    }
    const uint32_t count = peek_u32(p);
    const uint16_t ver = peek_u16(p + 4);
    const uint32_t zone_count = peek_u32(p + 8);
    if (ver != kRopeRecVersion) {
        *err = "旁包升降索记录版本不符 (" + std::to_string(ver) + " ≠ " + std::to_string(kRopeRecVersion) + ")";
        return false;
    }
    const uint64_t want = kRopeHeaderSize + static_cast<uint64_t>(zone_count) * kRopeZoneSize + static_cast<uint64_t>(count) * kRopeRecSize;
    if (want != len) {
        *err = "旁包升降索段长度与条数不符";
        return false;
    }
    const uint8_t* zp = p + kRopeHeaderSize;
    const uint8_t* rp = zp + static_cast<size_t>(zone_count) * kRopeZoneSize;
    zones->resize(zone_count);
    for (uint32_t i = 0; i < zone_count; ++i) {
        const uint8_t* e = zp + static_cast<size_t>(i) * kRopeZoneSize;
        FieldsLinkZone& z = (*zones)[i];
        z.zone_id = peek_u16(e);
        z.first = peek_u32(e + 4);
        z.len = peek_u32(e + 8);
        if (z.first > count || z.len > count - z.first) {
            *err = "旁包升降索区索引越界";
            return false;
        }
    }
    recs->resize(count);
    for (uint32_t i = 0; i < count; ++i) {
        const uint8_t* e = rp + static_cast<size_t>(i) * kRopeRecSize;
        FieldsRopeRec& r = (*recs)[i];
        r.uid_bottom = peek_u32(e);
        r.uid_top = peek_u32(e + 4);
        r.bottom = peek_link_side(e + 8);
        r.top = peek_link_side(e + 32);
        r.cost_px = peek_f32(e + 56);
        r.ride_s = peek_f32(e + 60);
    }
    return true;
}

// FOPN 段: 头 16 B, 区索引 zone_count × 12 B, 记录 count × 16 B, 区内按 (gy, gx, h 位模式) 升序
constexpr size_t kOpenHeaderSize = 16;
constexpr size_t kOpenZoneSize = 12;
constexpr size_t kOpenRecSize = 16;
constexpr uint16_t kOpenRecVersion = 1;

uint32_t h_bits(float h)
{
    uint32_t b = 0;
    std::memcpy(&b, &h, sizeof b);
    return b;
}

bool open_less(const FieldsOpenRec& a, const FieldsOpenRec& b)
{
    return std::tie(a.gy, a.gx) != std::tie(b.gy, b.gx) ? std::tie(a.gy, a.gx) < std::tie(b.gy, b.gx) : h_bits(a.h) < h_bits(b.h);
}

bool parse_opens(std::vector<FieldsOpenRec>* recs, std::vector<FieldsLinkZone>* zones, std::string* err, const uint8_t* p, size_t len)
{
    if (len < kOpenHeaderSize) {
        *err = "旁包打通表残缺";
        return false;
    }
    const uint32_t count = peek_u32(p);
    const uint16_t ver = peek_u16(p + 4);
    const uint32_t zone_count = peek_u32(p + 8);
    if (ver != kOpenRecVersion) {
        *err = "旁包打通表记录版本不符 (" + std::to_string(ver) + " ≠ " + std::to_string(kOpenRecVersion) + ")";
        return false;
    }
    const uint64_t want = kOpenHeaderSize + static_cast<uint64_t>(zone_count) * kOpenZoneSize + static_cast<uint64_t>(count) * kOpenRecSize;
    if (want != len) {
        *err = "旁包打通表长度与条数不符";
        return false;
    }
    const uint8_t* zp = p + kOpenHeaderSize;
    const uint8_t* rp = zp + static_cast<size_t>(zone_count) * kOpenZoneSize;
    zones->resize(zone_count);
    for (uint32_t i = 0; i < zone_count; ++i) {
        const uint8_t* e = zp + static_cast<size_t>(i) * kOpenZoneSize;
        FieldsLinkZone& z = (*zones)[i];
        z.zone_id = peek_u16(e);
        z.first = peek_u32(e + 4);
        z.len = peek_u32(e + 8);
        if (z.first > count || z.len > count - z.first) {
            *err = "旁包打通表区索引越界";
            return false;
        }
    }
    recs->resize(count);
    for (uint32_t i = 0; i < count; ++i) {
        const uint8_t* e = rp + static_cast<size_t>(i) * kOpenRecSize;
        FieldsOpenRec& r = (*recs)[i];
        r.gx = peek_i32(e);
        r.gy = peek_i32(e + 4);
        r.h = peek_f32(e + 8);
        r.clr = peek_u16(e + 12);
        r.flags = e[14];
        r.src = e[15];
    }
    for (const FieldsLinkZone& z : *zones) {
        if (!std::is_sorted(recs->begin() + z.first, recs->begin() + z.first + z.len, open_less)) {
            *err = "旁包打通表未按格序排列";
            return false;
        }
    }
    return true;
}

void append_f64(std::vector<uint8_t>* out, double v)
{
    uint8_t b[8];
    std::memcpy(b, &v, 8);
    out->insert(out->end(), b, b + 8);
}

void append_i32(std::vector<uint8_t>* out, int32_t v)
{
    uint8_t b[4];
    std::memcpy(b, &v, 4);
    out->insert(out->end(), b, b + 4);
}

// 字段顺序须与 FCON 段布局一致
std::vector<uint8_t> expected_const()
{
    std::vector<uint8_t> out;
    out.reserve(kConstSize);
    append_f64(&out, kCS);
    append_f64(&out, kClimb);
    append_f64(&out, kSlope);
    append_f64(&out, kStepUp);
    append_f64(&out, kBumpUp);
    append_f64(&out, kWallH);
    append_f64(&out, kMcHBand);
    append_f64(&out, kMergeH);
    append_f64(&out, kEdtCap);
    append_f64(&out, kWallSampleSub);
    append_f64(&out, kHitSampleSub);
    append_f64(&out, kWallBBoxPad);
    append_f64(&out, kStepTax);
    append_f64(&out, kClrLambda);
    append_f64(&out, 0.0);
    append_i32(&out, static_cast<int32_t>(kBumpCells));
    append_i32(&out, static_cast<int32_t>(kDipCells));
    append_i32(&out, static_cast<int32_t>(kFieldHalo));
    append_i32(&out, static_cast<int32_t>(kWalkableFlagsDefault));
    append_i32(&out, static_cast<int32_t>(kFieldsRulesVersion));
    append_i32(&out, 0);
    return out;
}

struct Reader
{
    const uint8_t* p = nullptr;
    const uint8_t* end = nullptr;
};

bool reader_varint(Reader* rd, uint64_t* out)
{
    *out = 0;
    for (int shift = 0; shift < 64; shift += 7) {
        if (rd->p == rd->end) {
            return false;
        }
        const uint8_t c = *rd->p++;
        *out |= static_cast<uint64_t>(c & 0x7FU) << shift;
        if ((c & 0x80U) == 0) {
            return true;
        }
    }
    return false;
}

bool reader_u32(Reader* rd, uint32_t* out)
{
    uint64_t v = 0;
    if (!reader_varint(rd, &v) || v > 0xFFFFFFFFULL) {
        return false;
    }
    *out = static_cast<uint32_t>(v);
    return true;
}

bool reader_stream(Reader* rd, std::vector<uint8_t>* out)
{
    uint64_t len = 0;
    if (!reader_varint(rd, &len) || len > static_cast<uint64_t>(rd->end - rd->p)) {
        return false;
    }
    const bool ok = InflateBytes(out, rd->p, static_cast<size_t>(len));
    rd->p += len;
    return ok;
}

bool reader_done(const Reader& rd)
{
    return rd.p == rd.end;
}

int64_t unzig(uint64_t u)
{
    return static_cast<int64_t>(u >> 1) ^ -static_cast<int64_t>(u & 1U);
}

bool ends_with(const std::string& name, const char* suffix)
{
    const size_t n = std::strlen(suffix);
    return name.size() >= n && name.compare(name.size() - n, n, suffix) == 0;
}

}

std::filesystem::path FieldsSidecarPath(const std::filesystem::path& main_pack)
{
    std::string name = MAA_NS::path_to_utf8_string(main_pack.filename());
    if (ends_with(name, ".nav.gz")) {
        name.insert(name.size() - 7, ".fields");
    }
    else if (ends_with(name, ".nav")) {
        name.insert(name.size() - 4, ".fields");
    }
    else {
        name += ".fields";
    }
    return main_pack.parent_path() / MAA_NS::path(name);
}

std::vector<uint8_t> FieldsZoneReachFrom(const FieldsZone& zone, uint32_t rid, uint32_t s0)
{
    std::vector<uint8_t> seen;
    if (rid >= zone.regions.size() || s0 == 0 || s0 > zone.regions[rid].n_scc) {
        return seen;
    }
    const FieldsZone::Region& r = zone.regions[rid];
    seen.assign(static_cast<size_t>(r.n_scc) + 1, 0);
    std::vector<uint32_t> stack { s0 };
    seen[s0] = 1;
    while (!stack.empty()) {
        const uint32_t s = stack.back();
        stack.pop_back();
        const uint32_t b = zone.adj_start[r.adj_off + s];
        const uint32_t e = zone.adj_start[r.adj_off + s + 1];
        for (uint32_t i = b; i < e; ++i) {
            const uint32_t d = zone.edge_dst[i];
            if (seen[d] == 0) {
                seen[d] = 1;
                stack.push_back(d);
            }
        }
    }
    return seen;
}

bool FieldsZoneWallKeep(bool* known, const FieldsZone& zone, int32_t tri, int k, uint32_t rid)
{
    const auto key = static_cast<uint32_t>(tri) * 3U + static_cast<uint32_t>(k);
    const auto it = std::lower_bound(zone.wall_key.begin(), zone.wall_key.end(), key);
    *known = it != zone.wall_key.end() && *it == key;
    if (!*known) {
        return false;
    }
    const auto w = static_cast<size_t>(it - zone.wall_key.begin());
    const auto b = zone.wall_rid.begin() + zone.wall_rid_start[w];
    const auto e = zone.wall_rid.begin() + zone.wall_rid_start[w + 1];
    return std::binary_search(b, e, rid);
}

bool LoadFieldsPack(FieldsPack* pack, std::string* err, const std::filesystem::path& path, const BaseNavPack& main, const GridPack& grid)
{
    pack->loaded = false;
    pack->zones.clear();
    pack->bytes.clear();
    const BaseNavLoadResult read = ReadNavFileBytes(path, &pack->bytes);
    if (read.status != BaseNavLoadStatus::Success) {
        *err = "旁包读不到 (" + MAA_NS::path_to_utf8_string(path) + "): " + read.message;
        return false;
    }
    const size_t size = pack->bytes.size();
    const uint8_t* base = pack->bytes.data();
    if (size < kHeaderSize || std::memcmp(base, "BNVF", 4) != 0) {
        *err = "旁包不是 BNVF";
        return false;
    }
    const uint16_t version = peek_u16(base + 4);
    pack->flags = peek_u16(base + 6);
    const uint64_t build_hash = peek_u64(base + 8);
    const uint64_t fnv = peek_u64(base + 16);
    const uint32_t rules = peek_u32(base + 24);
    const uint32_t section_count = peek_u32(base + 28);
    const uint64_t dir_off = peek_u64(base + 32);
    if (version != kFieldsFormatVersion) {
        *err = "旁包格式版本 " + std::to_string(version) + ", 需要 " + std::to_string(kFieldsFormatVersion);
        return false;
    }
    if (build_hash != main.build_hash || fnv != main.file_fnv) {
        *err = "旁包不是配这份主包的 (build_hash/fnv 不符)";
        return false;
    }
    if (rules != kFieldsRulesVersion) {
        *err = "旁包判据版本不符 (" + std::to_string(rules) + " ≠ " + std::to_string(kFieldsRulesVersion) + ")";
        return false;
    }
    if (dir_off > size || static_cast<uint64_t>(section_count) * kSectionEntrySize > size - dir_off) {
        *err = "旁包段目录越界";
        return false;
    }

    struct Sec
    {
        const uint8_t* p = nullptr;
        size_t len = 0;
    };

    std::unordered_map<std::string, Sec> secs;
    for (uint32_t i = 0; i < section_count; ++i) {
        const uint8_t* e = base + dir_off + static_cast<size_t>(i) * kSectionEntrySize;
        const std::string tag(reinterpret_cast<const char*>(e), 4);
        const uint64_t off = peek_u64(e + 8);
        const uint64_t len = peek_u64(e + 16);
        if (off > size || len > size - off) {
            *err = "旁包段 " + tag + " 越界";
            return false;
        }
        secs[tag] = Sec { base + off, static_cast<size_t>(len) };
    }
    for (const char* tag : { "FCON", "FZON", "FSPN", "FSCC", "FWAL" }) {
        if (secs.find(tag) == secs.end()) {
            *err = std::string("旁包缺段 ") + tag;
            return false;
        }
    }
    pack->links.clear();
    pack->link_zones.clear();
    if (const auto it = secs.find("FLNK"); it != secs.end()) {
        if (!parse_links(&pack->links, &pack->link_zones, err, it->second.p, it->second.len)) {
            return false;
        }
    }
    pack->opens.clear();
    pack->open_zones.clear();
    if (const auto it = secs.find("FOPN"); it != secs.end()) {
        if (!parse_opens(&pack->opens, &pack->open_zones, err, it->second.p, it->second.len)) {
            return false;
        }
    }
    pack->ropes.clear();
    pack->rope_zones.clear();
    if (const auto it = secs.find("FROP"); it != secs.end()) {
        if (!parse_ropes(&pack->ropes, &pack->rope_zones, err, it->second.p, it->second.len)) {
            return false;
        }
    }
    const Sec con = secs["FCON"];
    const std::vector<uint8_t> want = expected_const();
    if (con.len != kConstSize || std::memcmp(con.p, want.data(), kConstSize) != 0) {
        *err = "旁包的判据常数与运行期不符";
        return false;
    }
    const Sec zon = secs["FZON"];
    const Sec spn = secs["FSPN"];
    const Sec scc = secs["FSCC"];
    const Sec wal = secs["FWAL"];
    if (zon.len < 4) {
        *err = "旁包区表残缺";
        return false;
    }
    const uint32_t zone_count = peek_u32(zon.p);
    const uint8_t* p = zon.p + 4;
    const uint8_t* zend = zon.p + zon.len;
    for (uint32_t zi = 0; zi < zone_count; ++zi) {
        if (zend - p < 4) {
            *err = "旁包区表残缺";
            return false;
        }
        const uint32_t name_len = peek_u32(p);
        p += 4;
        const size_t padded = (static_cast<size_t>(name_len) + 3) / 4 * 4;
        if (static_cast<size_t>(zend - p) < padded + 8 + 48) {
            *err = "旁包区表残缺";
            return false;
        }
        FieldsZoneDir z;
        z.name.assign(reinterpret_cast<const char*>(p), name_len);
        p += padded;
        z.global_regions = peek_u32(p);
        const uint32_t tile_count = peek_u32(p + 4);
        const uint64_t spn_off = peek_u64(p + 8), spn_len = peek_u64(p + 16);
        const uint64_t scc_off = peek_u64(p + 24), scc_len = peek_u64(p + 32);
        const uint64_t wal_off = peek_u64(p + 40), wal_len = peek_u64(p + 48);
        p += 56;
        if (spn_off > spn.len || spn_len > spn.len - spn_off || scc_off > scc.len || scc_len > scc.len - scc_off || wal_off > wal.len
            || wal_len > wal.len - wal_off) {
            *err = "旁包区 " + z.name + " 的段范围越界";
            return false;
        }
        z.scc = scc.p + scc_off;
        z.scc_len = static_cast<size_t>(scc_len);
        z.wal = wal.p + wal_off;
        z.wal_len = static_cast<size_t>(wal_len);
        const uint8_t* zb = spn.p + spn_off;
        if (static_cast<uint64_t>(tile_count) * 16 > spn_len) {
            *err = "旁包区 " + z.name + " 的瓦表越界";
            return false;
        }
        z.tiles.resize(tile_count);
        for (uint32_t t = 0; t < tile_count; ++t) {
            const uint8_t* e = zb + static_cast<size_t>(t) * 16;
            FieldsTileRef& r = z.tiles[t];
            r.records = peek_u32(e);
            const uint64_t off = peek_u64(e + 4);
            const uint32_t len = peek_u32(e + 12);
            if (off > spn_len || len > spn_len - off) {
                *err = "旁包区 " + z.name + " 的瓦载荷越界";
                return false;
            }
            r.data = zb + off;
            r.len = len;
        }
        pack->zones.push_back(std::move(z));
    }
    // 主包按区裁剪载入时格图仍是整份, 所以按区名配对
    for (const GridZoneDir& g : grid.zones) {
        const FieldsZoneDir* z = nullptr;
        for (const FieldsZoneDir& c : pack->zones) {
            if (c.name == g.name) {
                z = &c;
                break;
            }
        }
        if (z == nullptr) {
            *err = "旁包没有区 " + g.name;
            return false;
        }
        if (z->global_regions != g.global_regions || z->tiles.size() != g.tiles.size()) {
            *err = "旁包区 " + g.name + " 的类数或瓦数与格图不符";
            return false;
        }
        for (size_t t = 0; t < g.tiles.size(); ++t) {
            if (z->tiles[t].records != g.tiles[t].records) {
                *err = "旁包区 " + g.name + " 第 " + std::to_string(t) + " 瓦记录数与格图不符";
                return false;
            }
        }
    }
    pack->loaded = true;
    return true;
}

bool LoadSharedFieldsPack(
    std::shared_ptr<const FieldsPack>* out,
    std::string* err,
    const std::filesystem::path& path,
    const BaseNavPack& main,
    const GridPack& grid)
{
    std::string key;
    std::error_code ec;
    const std::filesystem::path abs = std::filesystem::absolute(path, ec).lexically_normal();
    if (!ec) {
        const uintmax_t size = std::filesystem::file_size(abs, ec);
        if (!ec) {
            const auto mtime = std::filesystem::last_write_time(abs, ec);
            if (!ec) {
                const auto ticks = mtime.time_since_epoch().count();
                key = MAA_NS::path_to_utf8_string(abs) + '\n' + std::to_string(size) + '\n'
                      + std::string(reinterpret_cast<const char*>(&ticks), sizeof(ticks)) + '\n' + std::to_string(main.build_hash) + '\n'
                      + std::to_string(main.file_fnv);
            }
        }
    }
    static std::mutex mutex;
    static std::unordered_map<std::string, std::weak_ptr<const FieldsPack>> cache;
    const std::lock_guard lock(mutex);
    if (!key.empty()) {
        if (const auto it = cache.find(key); it != cache.end()) {
            if (std::shared_ptr<const FieldsPack> hit = it->second.lock()) {
                *out = std::move(hit);
                return true;
            }
        }
    }
    auto fresh = std::make_shared<FieldsPack>();
    const bool ok = LoadFieldsPack(fresh.get(), err, path, main, grid);
    if (ok && !key.empty()) {
        cache[key] = fresh;
    }
    *out = std::move(fresh);
    return ok;
}

const FieldsZoneDir* FindFieldsZone(const FieldsPack& pack, const std::string& name)
{
    for (const FieldsZoneDir& z : pack.zones) {
        if (z.name == name) {
            return &z;
        }
    }
    return nullptr;
}

std::span<const FieldsLinkRec> FieldsLinksOfZone(const FieldsPack& pack, uint16_t zone_id)
{
    for (const FieldsLinkZone& z : pack.link_zones) {
        if (z.zone_id == zone_id) {
            return z.len == 0 ? std::span<const FieldsLinkRec> {} : std::span<const FieldsLinkRec>(pack.links.data() + z.first, z.len);
        }
    }
    return {};
}

std::span<const FieldsRopeRec> FieldsRopesOfZone(const FieldsPack& pack, uint16_t zone_id)
{
    for (const FieldsLinkZone& z : pack.rope_zones) {
        if (z.zone_id == zone_id) {
            return z.len == 0 ? std::span<const FieldsRopeRec> {} : std::span<const FieldsRopeRec>(pack.ropes.data() + z.first, z.len);
        }
    }
    return {};
}

std::span<const FieldsOpenRec> FieldsOpensOfZone(const FieldsPack& pack, uint16_t zone_id)
{
    for (const FieldsLinkZone& z : pack.open_zones) {
        if (z.zone_id == zone_id) {
            return z.len == 0 ? std::span<const FieldsOpenRec> {} : std::span<const FieldsOpenRec>(pack.opens.data() + z.first, z.len);
        }
    }
    return {};
}

const FieldsOpenRec* FindOpen(const FieldsOpenRec* p, size_t n, int32_t gx, int32_t gy, float h)
{
    FieldsOpenRec key;
    key.gx = gx;
    key.gy = gy;
    key.h = h;
    const FieldsOpenRec* it = std::lower_bound(p, p + n, key, open_less);
    if (it == p + n || it->gx != gx || it->gy != gy || h_bits(it->h) != h_bits(h)) {
        return nullptr;
    }
    return it;
}

bool DecodeFieldsTile(FieldsTile* out, const FieldsTileRef& tile)
{
    out->scc.clear();
    out->steps2x.clear();
    out->seg.clear();
    out->tax.clear();
    out->clr2d.clear();
    out->medial.clear();
    if (tile.records == 0) {
        return true;
    }
    Reader rd { tile.data, tile.data + tile.len };
    uint64_t n = 0;
    if (!reader_varint(&rd, &n) || n != tile.records) {
        return false;
    }
    // 六条流依次: 分量号 (zigzag 差分 varint)、禁步 XOR、段位、税边位、净空差 (varint)、中轴位
    std::vector<uint8_t> s0;
    std::vector<uint8_t> s4;
    if (!reader_stream(&rd, &s0) || !reader_stream(&rd, &out->steps2x) || !reader_stream(&rd, &out->seg) || !reader_stream(&rd, &out->tax)
        || !reader_stream(&rd, &s4) || !reader_stream(&rd, &out->medial) || !reader_done(rd)) {
        return false;
    }
    if (out->steps2x.size() != n || out->seg.size() != n || out->tax.size() != n || out->medial.size() != n) {
        return false;
    }
    out->scc.resize(static_cast<size_t>(n));
    Reader r0 { s0.data(), s0.data() + s0.size() };
    int64_t prev = 0;
    for (size_t i = 0; i < n; ++i) {
        uint64_t u = 0;
        if (!reader_varint(&r0, &u)) {
            return false;
        }
        prev += unzig(u);
        if (prev < 0) {
            return false;
        }
        out->scc[i] = static_cast<uint32_t>(prev);
    }
    out->clr2d.resize(static_cast<size_t>(n));
    Reader r4 { s4.data(), s4.data() + s4.size() };
    for (size_t i = 0; i < n; ++i) {
        uint64_t u = 0;
        if (!reader_varint(&r4, &u) || u > 0xFFFFULL) {
            return false;
        }
        out->clr2d[i] = static_cast<uint16_t>(u);
    }
    return reader_done(r0) && reader_done(r4);
}

bool LoadFieldsZone(FieldsZone* out, std::string* err, const FieldsPack& pack, const FieldsZoneDir& zone)
{
    *out = FieldsZone();
    out->regions.assign(static_cast<size_t>(zone.global_regions) + 1, FieldsZone::Region {});
    // 分量图的边已按 (src, dst) 升序, 计数装填即成 CSR
    {
        Reader rd { zone.scc, zone.scc + zone.scc_len };
        uint32_t rc = 0;
        if (!reader_u32(&rd, &rc)) {
            *err = "旁包区 " + zone.name + " 的分量图残缺";
            return false;
        }
        std::vector<uint8_t> st;
        std::vector<uint32_t> src;
        std::vector<uint32_t> dst;
        for (uint32_t i = 0; i < rc; ++i) {
            uint32_t rid = 0, n_scc = 0, n_edges = 0;
            if (!reader_u32(&rd, &rid) || !reader_u32(&rd, &n_scc) || !reader_u32(&rd, &n_edges) || rid > zone.global_regions
                || n_scc == 0) {
                *err = "旁包区 " + zone.name + " 的分量图残缺";
                return false;
            }
            if ((pack.flags & 1U) != 0) {
                uint64_t skip = 0;
                for (int b = 0; b < 4; ++b) {
                    if (!reader_varint(&rd, &skip)) {
                        *err = "旁包区 " + zone.name + " 的分量图残缺";
                        return false;
                    }
                }
            }
            if (!reader_stream(&rd, &st)) {
                *err = "旁包区 " + zone.name + " 的分量图解不开";
                return false;
            }
            // 每条边至少 2 字节, 先按流长卡住 n_edges 再分配
            if (st.size() < 2ULL * n_edges) {
                *err = "旁包区 " + zone.name + " 的分量图残缺";
                return false;
            }
            Reader es { st.data(), st.data() + st.size() };
            src.assign(n_edges, 0);
            dst.assign(n_edges, 0);
            uint32_t prev = 0;
            for (uint32_t e = 0; e < n_edges; ++e) {
                uint32_t d = 0;
                if (!reader_u32(&es, &d) || !reader_u32(&es, &dst[e])) {
                    *err = "旁包区 " + zone.name + " 的分量图残缺";
                    return false;
                }
                prev += d;
                src[e] = prev;
                if (src[e] == 0 || src[e] > n_scc || dst[e] == 0 || dst[e] > n_scc) {
                    *err = "旁包区 " + zone.name + " 类 " + std::to_string(rid) + " 的边越界";
                    return false;
                }
            }
            if (!reader_done(es)) {
                *err = "旁包区 " + zone.name + " 的分量图残缺";
                return false;
            }
            FieldsZone::Region& r = out->regions[rid];
            r.n_scc = n_scc;
            r.adj_off = static_cast<uint32_t>(out->adj_start.size());
            const auto base_e = static_cast<uint32_t>(out->edge_dst.size());
            out->adj_start.resize(out->adj_start.size() + n_scc + 2, base_e);
            for (uint32_t e = 0; e < n_edges; ++e) {
                out->adj_start[r.adj_off + src[e] + 1] += 1;
            }
            for (uint32_t s = 1; s <= n_scc + 1; ++s) {
                out->adj_start[r.adj_off + s] += out->adj_start[r.adj_off + s - 1] - base_e;
            }
            out->edge_dst.insert(out->edge_dst.end(), dst.begin(), dst.end());
        }
        if (!reader_done(rd)) {
            *err = "旁包区 " + zone.name + " 的分量图有多余字节";
            return false;
        }
    }
    // 留墙表: 键升序差分, 逐墙的类表升序差分
    {
        Reader rd { zone.wal, zone.wal + zone.wal_len };
        uint32_t nw = 0;
        std::vector<uint8_t> a;
        std::vector<uint8_t> b;
        if (!reader_u32(&rd, &nw) || !reader_stream(&rd, &a) || !reader_stream(&rd, &b) || !reader_done(rd)) {
            *err = "旁包区 " + zone.name + " 的留墙表残缺";
            return false;
        }
        Reader ra { a.data(), a.data() + a.size() };
        Reader rb { b.data(), b.data() + b.size() };
        out->wall_key.resize(nw);
        out->wall_rid_start.assign(static_cast<size_t>(nw) + 1, 0);
        uint32_t prev = 0;
        for (uint32_t i = 0; i < nw; ++i) {
            uint32_t d = 0;
            if (!reader_u32(&ra, &d)) {
                *err = "旁包区 " + zone.name + " 的留墙表残缺";
                return false;
            }
            prev += d;
            out->wall_key[i] = prev;
        }
        for (uint32_t i = 0; i < nw; ++i) {
            uint32_t nr = 0;
            if (!reader_u32(&rb, &nr)) {
                *err = "旁包区 " + zone.name + " 的留墙表残缺";
                return false;
            }
            uint32_t rp = 0;
            for (uint32_t k = 0; k < nr; ++k) {
                uint32_t d = 0;
                if (!reader_u32(&rb, &d)) {
                    *err = "旁包区 " + zone.name + " 的留墙表残缺";
                    return false;
                }
                rp += d;
                out->wall_rid.push_back(rp);
            }
            out->wall_rid_start[static_cast<size_t>(i) + 1] = static_cast<uint32_t>(out->wall_rid.size());
        }
        if (!reader_done(ra) || !reader_done(rb)) {
            *err = "旁包区 " + zone.name + " 的留墙表有多余字节";
            return false;
        }
    }
    return true;
}

}
