#include "RecastNavFieldsIO.h"

#include <algorithm>
#include <cstring>
#include <unordered_map>

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

// 采样与包围盒口径写死在 StampWalls / WallHits / BakeWalls 里, 旁包按同样的值烘。
constexpr double kWallSampleSub = 0.4;
constexpr double kHitSampleSub = 0.2;
constexpr double kWallBBoxPad = 4.0;

uint16_t peekU16(const uint8_t* p)
{
    uint16_t v = 0;
    std::memcpy(&v, p, sizeof v);
    return v;
}

uint32_t peekU32(const uint8_t* p)
{
    uint32_t v = 0;
    std::memcpy(&v, p, sizeof v);
    return v;
}

uint64_t peekU64(const uint8_t* p)
{
    uint64_t v = 0;
    std::memcpy(&v, p, sizeof v);
    return v;
}

// 运行期判据常数排成旁包 FCON 段的样子, 整段逐字节比对。
std::vector<uint8_t> expectedConst()
{
    std::vector<uint8_t> out;
    out.reserve(kConstSize);
    const auto f64 = [&](double v) {
        uint8_t b[8];
        std::memcpy(b, &v, 8);
        out.insert(out.end(), b, b + 8);
    };
    const auto i32 = [&](int32_t v) {
        uint8_t b[4];
        std::memcpy(b, &v, 4);
        out.insert(out.end(), b, b + 4);
    };
    f64(kCS);
    f64(kClimb);
    f64(kSlope);
    f64(kStepUp);
    f64(kBumpUp);
    f64(kWallH);
    f64(kMcHBand);
    f64(kMergeH);
    f64(kEdtCap);
    f64(kWallSampleSub);
    f64(kHitSampleSub);
    f64(kWallBBoxPad);
    f64(kStepTax);
    f64(kClrLambda);
    f64(0.0);
    i32(static_cast<int32_t>(kBumpCells));
    i32(static_cast<int32_t>(kDipCells));
    i32(static_cast<int32_t>(kFieldHalo));
    i32(static_cast<int32_t>(kWalkableFlagsDefault));
    i32(static_cast<int32_t>(kFieldsRulesVersion));
    i32(0);
    return out;
}

// 变长整数与带长度前缀的 zlib 流。越界即失败, 不越界读。
struct Reader
{
    const uint8_t* p = nullptr;
    const uint8_t* end = nullptr;

    bool varint(uint64_t& out)
    {
        out = 0;
        for (int shift = 0; shift < 64; shift += 7) {
            if (p == end) {
                return false;
            }
            const uint8_t c = *p++;
            out |= static_cast<uint64_t>(c & 0x7FU) << shift;
            if ((c & 0x80U) == 0) {
                return true;
            }
        }
        return false;
    }

    bool u32(uint32_t& out)
    {
        uint64_t v = 0;
        if (!varint(v) || v > 0xFFFFFFFFULL) {
            return false;
        }
        out = static_cast<uint32_t>(v);
        return true;
    }

    bool stream(std::vector<uint8_t>& out)
    {
        uint64_t len = 0;
        if (!varint(len) || len > static_cast<uint64_t>(end - p)) {
            return false;
        }
        const bool ok = InflateBytes(p, static_cast<size_t>(len), out);
        p += len;
        return ok;
    }

    bool done() const { return p == end; }
};

int64_t unzig(uint64_t u)
{
    return static_cast<int64_t>(u >> 1) ^ -static_cast<int64_t>(u & 1U);
}

}

std::filesystem::path FieldsSidecarPath(const std::filesystem::path& main_pack)
{
    std::string name = main_pack.filename().string();
    const auto endsWith = [&](const char* suffix) {
        const size_t n = std::strlen(suffix);
        return name.size() >= n && name.compare(name.size() - n, n, suffix) == 0;
    };
    if (endsWith(".nav.gz")) {
        name.insert(name.size() - 7, ".fields");
    }
    else if (endsWith(".nav")) {
        name.insert(name.size() - 4, ".fields");
    }
    else {
        name += ".fields";
    }
    return main_pack.parent_path() / name;
}

std::vector<uint8_t> FieldsZone::reachFrom(uint32_t rid, uint32_t s0) const
{
    std::vector<uint8_t> seen;
    if (rid >= regions.size() || s0 == 0 || s0 > regions[rid].n_scc) {
        return seen;
    }
    const Region& r = regions[rid];
    seen.assign(static_cast<size_t>(r.n_scc) + 1, 0);
    std::vector<uint32_t> stack { s0 };
    seen[s0] = 1;
    while (!stack.empty()) {
        const uint32_t s = stack.back();
        stack.pop_back();
        const uint32_t b = adj_start[r.adj_off + s];
        const uint32_t e = adj_start[r.adj_off + s + 1];
        for (uint32_t i = b; i < e; ++i) {
            const uint32_t d = edge_dst[i];
            if (seen[d] == 0) {
                seen[d] = 1;
                stack.push_back(d);
            }
        }
    }
    return seen;
}

bool FieldsZone::wallKeep(int32_t tri, int k, uint32_t rid, bool& known) const
{
    const auto key = static_cast<uint32_t>(tri) * 3U + static_cast<uint32_t>(k);
    const auto it = std::lower_bound(wall_key.begin(), wall_key.end(), key);
    known = it != wall_key.end() && *it == key;
    if (!known) {
        return false;
    }
    const auto w = static_cast<size_t>(it - wall_key.begin());
    const auto b = wall_rid.begin() + wall_rid_start[w];
    const auto e = wall_rid.begin() + wall_rid_start[w + 1];
    return std::binary_search(b, e, rid);
}

bool FieldsPack::load(const std::filesystem::path& path, const BaseNavPack& main, const GridPack& grid, std::string& err)
{
    loaded_ = false;
    zones_.clear();
    bytes_.clear();
    const BaseNavLoadResult read = ReadNavFileBytes(path, &bytes_);
    if (read.status != BaseNavLoadStatus::Success) {
        err = "旁包读不到 (" + path.string() + "): " + read.message;
        return false;
    }
    const size_t size = bytes_.size();
    const uint8_t* base = bytes_.data();
    if (size < kHeaderSize || std::memcmp(base, "BNVF", 4) != 0) {
        err = "旁包不是 BNVF";
        return false;
    }
    const uint16_t version = peekU16(base + 4);
    flags_ = peekU16(base + 6);
    const uint64_t build_hash = peekU64(base + 8);
    const uint64_t fnv = peekU64(base + 16);
    const uint32_t rules = peekU32(base + 24);
    const uint32_t section_count = peekU32(base + 28);
    const uint64_t dir_off = peekU64(base + 32);
    if (version != kFieldsFormatVersion) {
        err = "旁包格式版本 " + std::to_string(version) + ", 需要 " + std::to_string(kFieldsFormatVersion);
        return false;
    }
    if (build_hash != main.buildHash() || fnv != main.fileFnv()) {
        err = "旁包不是配这份主包的 (build_hash/fnv 不符)";
        return false;
    }
    if (rules != kFieldsRulesVersion) {
        err = "旁包判据版本不符 (" + std::to_string(rules) + " ≠ " + std::to_string(kFieldsRulesVersion) + ")";
        return false;
    }
    if (dir_off > size || static_cast<uint64_t>(section_count) * kSectionEntrySize > size - dir_off) {
        err = "旁包段目录越界";
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
        const uint64_t off = peekU64(e + 8);
        const uint64_t len = peekU64(e + 16);
        if (off > size || len > size - off) {
            err = "旁包段 " + tag + " 越界";
            return false;
        }
        secs[tag] = Sec { base + off, static_cast<size_t>(len) };
    }
    for (const char* tag : { "FCON", "FZON", "FSPN", "FSCC", "FWAL" }) {
        if (secs.find(tag) == secs.end()) {
            err = std::string("旁包缺段 ") + tag;
            return false;
        }
    }
    const Sec con = secs["FCON"];
    const std::vector<uint8_t> want = expectedConst();
    if (con.len != kConstSize || std::memcmp(con.p, want.data(), kConstSize) != 0) {
        err = "旁包的判据常数与运行期不符";
        return false;
    }
    const Sec zon = secs["FZON"];
    const Sec spn = secs["FSPN"];
    const Sec scc = secs["FSCC"];
    const Sec wal = secs["FWAL"];
    if (zon.len < 4) {
        err = "旁包区表残缺";
        return false;
    }
    const uint32_t zone_count = peekU32(zon.p);
    const uint8_t* p = zon.p + 4;
    const uint8_t* zend = zon.p + zon.len;
    for (uint32_t zi = 0; zi < zone_count; ++zi) {
        if (zend - p < 4) {
            err = "旁包区表残缺";
            return false;
        }
        const uint32_t name_len = peekU32(p);
        p += 4;
        const size_t padded = (static_cast<size_t>(name_len) + 3) / 4 * 4;
        if (static_cast<size_t>(zend - p) < padded + 8 + 48) {
            err = "旁包区表残缺";
            return false;
        }
        FieldsZoneDir z;
        z.name.assign(reinterpret_cast<const char*>(p), name_len);
        p += padded;
        z.global_regions = peekU32(p);
        const uint32_t tile_count = peekU32(p + 4);
        const uint64_t spn_off = peekU64(p + 8), spn_len = peekU64(p + 16);
        const uint64_t scc_off = peekU64(p + 24), scc_len = peekU64(p + 32);
        const uint64_t wal_off = peekU64(p + 40), wal_len = peekU64(p + 48);
        p += 56;
        if (spn_off > spn.len || spn_len > spn.len - spn_off || scc_off > scc.len || scc_len > scc.len - scc_off || wal_off > wal.len
            || wal_len > wal.len - wal_off) {
            err = "旁包区 " + z.name + " 的段范围越界";
            return false;
        }
        z.scc = scc.p + scc_off;
        z.scc_len = static_cast<size_t>(scc_len);
        z.wal = wal.p + wal_off;
        z.wal_len = static_cast<size_t>(wal_len);
        const uint8_t* zb = spn.p + spn_off;
        if (static_cast<uint64_t>(tile_count) * 16 > spn_len) {
            err = "旁包区 " + z.name + " 的瓦表越界";
            return false;
        }
        z.tiles.resize(tile_count);
        for (uint32_t t = 0; t < tile_count; ++t) {
            const uint8_t* e = zb + static_cast<size_t>(t) * 16;
            FieldsTileRef& r = z.tiles[t];
            r.records = peekU32(e);
            const uint64_t off = peekU64(e + 4);
            const uint32_t len = peekU32(e + 12);
            if (off > spn_len || len > spn_len - off) {
                err = "旁包区 " + z.name + " 的瓦载荷越界";
                return false;
            }
            r.data = zb + off;
            r.len = len;
        }
        zones_.push_back(std::move(z));
    }
    // 对着格图目录核区: 主包按区裁剪载入时格图仍是整份, 所以按名字配, 格图里的区都得有。
    for (const GridZoneDir& g : grid.zones()) {
        const FieldsZoneDir* z = nullptr;
        for (const FieldsZoneDir& c : zones_) {
            if (c.name == g.name) {
                z = &c;
                break;
            }
        }
        if (z == nullptr) {
            err = "旁包没有区 " + g.name;
            return false;
        }
        if (z->global_regions != g.global_regions || z->tiles.size() != g.tiles.size()) {
            err = "旁包区 " + g.name + " 的类数或瓦数与格图不符";
            return false;
        }
        for (size_t t = 0; t < g.tiles.size(); ++t) {
            if (z->tiles[t].records != g.tiles[t].records) {
                err = "旁包区 " + g.name + " 第 " + std::to_string(t) + " 瓦记录数与格图不符";
                return false;
            }
        }
    }
    loaded_ = true;
    return true;
}

const FieldsZoneDir* FieldsPack::findZone(const std::string& name) const
{
    for (const FieldsZoneDir& z : zones_) {
        if (z.name == name) {
            return &z;
        }
    }
    return nullptr;
}

bool FieldsPack::decodeTile(const FieldsTileRef& t, FieldsTile& out) const
{
    out.scc.clear();
    out.steps2x.clear();
    out.seg.clear();
    out.tax.clear();
    out.clr2d.clear();
    out.medial.clear();
    if (t.records == 0) {
        return true;
    }
    Reader rd { t.data, t.data + t.len };
    uint64_t n = 0;
    if (!rd.varint(n) || n != t.records) {
        return false;
    }
    // 六条流依次: 分量号(zigzag 差分 varint)、禁步 XOR、段位、税边位、净空差(varint)、中轴位。
    std::vector<uint8_t> s0;
    std::vector<uint8_t> s4;
    if (!rd.stream(s0) || !rd.stream(out.steps2x) || !rd.stream(out.seg) || !rd.stream(out.tax) || !rd.stream(s4) || !rd.stream(out.medial)
        || !rd.done()) {
        return false;
    }
    if (out.steps2x.size() != n || out.seg.size() != n || out.tax.size() != n || out.medial.size() != n) {
        return false;
    }
    out.scc.resize(static_cast<size_t>(n));
    Reader r0 { s0.data(), s0.data() + s0.size() };
    int64_t prev = 0;
    for (size_t i = 0; i < n; ++i) {
        uint64_t u = 0;
        if (!r0.varint(u)) {
            return false;
        }
        prev += unzig(u);
        if (prev < 0) {
            return false;
        }
        out.scc[i] = static_cast<uint32_t>(prev);
    }
    out.clr2d.resize(static_cast<size_t>(n));
    Reader r4 { s4.data(), s4.data() + s4.size() };
    for (size_t i = 0; i < n; ++i) {
        uint64_t u = 0;
        if (!r4.varint(u) || u > 0xFFFFULL) {
            return false;
        }
        out.clr2d[i] = static_cast<uint16_t>(u);
    }
    return r0.done() && r4.done();
}

bool FieldsPack::loadZone(const FieldsZoneDir& z, FieldsZone& out, std::string& err) const
{
    out = FieldsZone();
    out.regions.assign(static_cast<size_t>(z.global_regions) + 1, FieldsZone::Region {});
    // 分量图: 逐类读边表, 按源分量装成 CSR。边已按 (src, dst) 升序, 所以计数装填即成。
    {
        Reader rd { z.scc, z.scc + z.scc_len };
        uint32_t rc = 0;
        if (!rd.u32(rc)) {
            err = "旁包区 " + z.name + " 的分量图残缺";
            return false;
        }
        std::vector<uint8_t> st;
        std::vector<uint32_t> src;
        std::vector<uint32_t> dst;
        for (uint32_t i = 0; i < rc; ++i) {
            uint32_t rid = 0, n_scc = 0, n_edges = 0;
            if (!rd.u32(rid) || !rd.u32(n_scc) || !rd.u32(n_edges) || rid > z.global_regions || n_scc == 0) {
                err = "旁包区 " + z.name + " 的分量图残缺";
                return false;
            }
            if ((flags_ & 1U) != 0) {
                uint64_t skip = 0;
                for (int b = 0; b < 4; ++b) {
                    if (!rd.varint(skip)) {
                        err = "旁包区 " + z.name + " 的分量图残缺";
                        return false;
                    }
                }
            }
            if (!rd.stream(st)) {
                err = "旁包区 " + z.name + " 的分量图解不开";
                return false;
            }
            // 每条边至少 2 字节 varint, 先按流长卡住 n_edges 再分配
            if (st.size() < 2ULL * n_edges) {
                err = "旁包区 " + z.name + " 的分量图残缺";
                return false;
            }
            Reader es { st.data(), st.data() + st.size() };
            src.assign(n_edges, 0);
            dst.assign(n_edges, 0);
            uint32_t prev = 0;
            for (uint32_t e = 0; e < n_edges; ++e) {
                uint32_t d = 0;
                if (!es.u32(d) || !es.u32(dst[e])) {
                    err = "旁包区 " + z.name + " 的分量图残缺";
                    return false;
                }
                prev += d;
                src[e] = prev;
                if (src[e] == 0 || src[e] > n_scc || dst[e] == 0 || dst[e] > n_scc) {
                    err = "旁包区 " + z.name + " 类 " + std::to_string(rid) + " 的边越界";
                    return false;
                }
            }
            if (!es.done()) {
                err = "旁包区 " + z.name + " 的分量图残缺";
                return false;
            }
            FieldsZone::Region& r = out.regions[rid];
            r.n_scc = n_scc;
            r.adj_off = static_cast<uint32_t>(out.adj_start.size());
            const auto base_e = static_cast<uint32_t>(out.edge_dst.size());
            out.adj_start.resize(out.adj_start.size() + n_scc + 2, base_e);
            for (uint32_t e = 0; e < n_edges; ++e) {
                out.adj_start[r.adj_off + src[e] + 1] += 1;
            }
            for (uint32_t s = 1; s <= n_scc + 1; ++s) {
                out.adj_start[r.adj_off + s] += out.adj_start[r.adj_off + s - 1] - base_e;
            }
            out.edge_dst.insert(out.edge_dst.end(), dst.begin(), dst.end());
        }
        if (!rd.done()) {
            err = "旁包区 " + z.name + " 的分量图有多余字节";
            return false;
        }
    }
    // 留墙表: 键升序差分, 逐墙的类表升序差分。
    {
        Reader rd { z.wal, z.wal + z.wal_len };
        uint32_t nw = 0;
        std::vector<uint8_t> a;
        std::vector<uint8_t> b;
        if (!rd.u32(nw) || !rd.stream(a) || !rd.stream(b) || !rd.done()) {
            err = "旁包区 " + z.name + " 的留墙表残缺";
            return false;
        }
        Reader ra { a.data(), a.data() + a.size() };
        Reader rb { b.data(), b.data() + b.size() };
        out.wall_key.resize(nw);
        out.wall_rid_start.assign(static_cast<size_t>(nw) + 1, 0);
        uint32_t prev = 0;
        for (uint32_t i = 0; i < nw; ++i) {
            uint32_t d = 0;
            if (!ra.u32(d)) {
                err = "旁包区 " + z.name + " 的留墙表残缺";
                return false;
            }
            prev += d;
            out.wall_key[i] = prev;
        }
        for (uint32_t i = 0; i < nw; ++i) {
            uint32_t nr = 0;
            if (!rb.u32(nr)) {
                err = "旁包区 " + z.name + " 的留墙表残缺";
                return false;
            }
            uint32_t rp = 0;
            for (uint32_t k = 0; k < nr; ++k) {
                uint32_t d = 0;
                if (!rb.u32(d)) {
                    err = "旁包区 " + z.name + " 的留墙表残缺";
                    return false;
                }
                rp += d;
                out.wall_rid.push_back(rp);
            }
            out.wall_rid_start[static_cast<size_t>(i) + 1] = static_cast<uint32_t>(out.wall_rid.size());
        }
        if (!ra.done() || !rb.done()) {
            err = "旁包区 " + z.name + " 的留墙表有多余字节";
            return false;
        }
    }
    return true;
}

}
