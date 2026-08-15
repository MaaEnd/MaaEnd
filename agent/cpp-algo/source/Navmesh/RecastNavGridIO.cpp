#include "RecastNavGridIO.h"

#include <cstring>

#include <zlib.h>

#include "RecastNavGrid.h"

namespace navmesh::recast
{

namespace
{

constexpr uint32_t kGridSectionVersion = 2;
constexpr size_t kGridHeaderSize = 40;
constexpr size_t kGridTileEntrySize = 48;
constexpr size_t kInflateChunk = 1U << 16U;

// 定长小端字段。越界就停在原地并记下失败,调用方查一次即可。
struct Cursor
{
    const uint8_t* p = nullptr;
    const uint8_t* end = nullptr;
    bool ok = true;

    bool take(size_t n, const uint8_t*& at)
    {
        if (!ok || static_cast<size_t>(end - p) < n) {
            ok = false;
            return false;
        }
        at = p;
        p += n;
        return true;
    }

    template <typename T>
    bool read(T& out)
    {
        const uint8_t* at = nullptr;
        if (!take(sizeof(T), at)) {
            return false;
        }
        std::memcpy(&out, at, sizeof(T));
        return true;
    }

    bool u32(uint32_t& out) { return read(out); }
    bool u64(uint64_t& out) { return read(out); }
    bool i32(int32_t& out) { return read(out); }
    bool f64(double& out) { return read(out); }
};

// 瓦载荷不带原长,所以按块解到流末尾。
bool Inflate(const uint8_t* data, size_t len, std::vector<uint8_t>& out)
{
    out.clear();
    z_stream zs {};
    if (inflateInit(&zs) != Z_OK) {
        return false;
    }
    zs.next_in = const_cast<Bytef*>(data);
    zs.avail_in = static_cast<uInt>(len);
    int rc = Z_OK;
    do {
        const size_t at = out.size();
        out.resize(at + kInflateChunk);
        zs.next_out = out.data() + at;
        zs.avail_out = static_cast<uInt>(kInflateChunk);
        rc = inflate(&zs, Z_NO_FLUSH);
        if (rc != Z_OK && rc != Z_STREAM_END) {
            inflateEnd(&zs);
            return false;
        }
        out.resize(at + kInflateChunk - zs.avail_out);
    } while (rc != Z_STREAM_END);
    inflateEnd(&zs);
    return true;
}

// 变长整数,低 7 位一组小端在前。走过头就报错,不越界读。
bool GetVarint(const uint8_t*& p, const uint8_t* end, uint64_t& out)
{
    out = 0;
    for (int shift = 0; shift < 64; shift += 7) {
        if (p == end) {
            return false;
        }
        const uint8_t b = *p++;
        out |= static_cast<uint64_t>(b & 0x7FU) << shift;
        if ((b & 0x80U) == 0) {
            return true;
        }
    }
    return false;
}

}

bool DecodeGridTile(const uint8_t* data, size_t len, GridTile& out)
{
    out = GridTile {};
    if (data == nullptr) {
        return false;
    }
    const uint8_t* p = data;
    const uint8_t* const end = data + len;

    uint64_t n = 0;
    if (!GetVarint(p, end, n)) {
        return false;
    }
    if (n == 0) {
        return p == end;
    }
    if (static_cast<size_t>(end - p) < sizeof(float)) {
        return false;
    }
    float hmin = 0.0F;
    std::memcpy(&hmin, p, sizeof(float));
    p += sizeof(float);

    uint64_t regions = 0;
    uint64_t ncell = 0;
    if (!GetVarint(p, end, regions) || !GetVarint(p, end, ncell)) {
        return false;
    }

    // 六列各自长度前缀,顺序与写入端一致:格、高、类号、标志、断口、净空。
    const uint8_t* col[6] = {};
    const uint8_t* col_end[6] = {};
    for (int i = 0; i < 6; ++i) {
        uint64_t clen = 0;
        if (!GetVarint(p, end, clen) || static_cast<uint64_t>(end - p) < clen) {
            return false;
        }
        col[i] = p;
        col_end[i] = p + clen;
        p += clen;
    }
    if (p != end) {
        return false;
    }

    out.regions = static_cast<uint32_t>(regions);
    out.rec.reserve(static_cast<size_t>(n));
    int64_t cell = 0;
    for (uint64_t i = 0; i < ncell; ++i) {
        uint64_t delta = 0;
        uint64_t k = 0;
        if (!GetVarint(col[0], col_end[0], delta) || !GetVarint(col[0], col_end[0], k)) {
            return false;
        }
        cell += static_cast<int64_t>(delta);
        for (uint64_t j = 0; j < k; ++j) {
            if (out.rec.size() >= static_cast<size_t>(n)) {
                return false;
            }
            uint64_t hq = 0;
            uint64_t rid = 0;
            uint64_t clr = 0;
            if (!GetVarint(col[1], col_end[1], hq) || !GetVarint(col[2], col_end[2], rid)
                || !GetVarint(col[5], col_end[5], clr)) {
                return false;
            }
            if (col[3] == col_end[3] || col[4] == col_end[4]) {
                return false;
            }
            GridSpanRec r {};
            r.cell = cell;
            r.rid = static_cast<uint32_t>(rid);
            r.clr = static_cast<uint16_t>(clr);
            r.flags = *col[3]++;
            r.steps = *col[4]++;
            r.h = (r.flags & kGridFlagFill) != 0
                ? 0.0F
                : static_cast<float>(static_cast<double>(hmin) + static_cast<double>(hq) / 64.0);
            out.rec.push_back(r);
        }
    }
    if (out.rec.size() != static_cast<size_t>(n)) {
        return false;
    }
    for (int i = 0; i < 6; ++i) {
        if (col[i] != col_end[i]) {
            return false;
        }
    }
    return true;
}

bool GridPack::parse(const uint8_t* data, size_t len, std::string& err)
{
    base_ = nullptr;
    len_ = 0;
    zones_.clear();
    if (data == nullptr || len < kGridHeaderSize) {
        err = "GRID section is truncated";
        return false;
    }
    if (std::memcmp(data, kGridSectionTag, 4) != 0) {
        err = "GRID section has a bad magic";
        return false;
    }
    Cursor cur { data + 4, data + len };
    uint32_t version = 0;
    uint32_t zone_count = 0;
    uint32_t reserved = 0;
    if (!cur.u32(version) || !cur.f64(cell_size_) || !cur.f64(tile_px_) || !cur.f64(apron_px_)
        || !cur.u32(zone_count) || !cur.u32(reserved)) {
        err = "GRID header is truncated";
        return false;
    }
    if (version != kGridSectionVersion) {
        err = "GRID section version " + std::to_string(version) + " is not supported";
        return false;
    }
    // 格边长是判据常数,包和运行端对不上就整包不认:两边的格心不重合,烘出来的一切都错位。
    if (std::abs(cell_size_ - kCS) > 1e-12) {
        err = "GRID cell size does not match the runtime grid";
        return false;
    }

    zones_.resize(zone_count);
    for (uint32_t i = 0; i < zone_count; ++i) {
        GridZoneDir& z = zones_[i];
        uint32_t name_len = 0;
        uint32_t tile_count = 0;
        if (!cur.u32(name_len)) {
            err = "GRID zone directory is truncated";
            return false;
        }
        const size_t padded = (static_cast<size_t>(name_len) + 3) / 4 * 4;
        const uint8_t* name_bytes = nullptr;
        if (!cur.take(padded, name_bytes) || !cur.f64(z.x0) || !cur.f64(z.y0)
            || !cur.u32(z.global_regions) || !cur.u32(tile_count)) {
            err = "GRID zone directory is truncated";
            return false;
        }
        z.name.assign(reinterpret_cast<const char*>(name_bytes), name_len);
        if (static_cast<size_t>(cur.end - cur.p) < static_cast<size_t>(tile_count) * kGridTileEntrySize) {
            err = "GRID tile directory of zone '" + z.name + "' is truncated";
            return false;
        }
        z.tiles.resize(tile_count);
        for (uint32_t t = 0; t < tile_count; ++t) {
            GridTileRef& r = z.tiles[t];
            int32_t* const fields[8] = { &r.gx0, &r.gy0, &r.nx, &r.ny, &r.px0, &r.px1, &r.py0, &r.py1 };
            for (int32_t* f : fields) {
                cur.i32(*f);
            }
            cur.u64(r.offset);
            cur.u32(r.len);
            cur.u32(r.records);
            if (r.nx <= 0 || r.ny <= 0 || r.offset > len || r.len > len - r.offset) {
                err = "GRID tile of zone '" + z.name + "' points outside the section";
                return false;
            }
        }
    }
    base_ = data;
    len_ = len;
    return true;
}

const GridZoneDir* GridPack::findZone(const std::string& name) const
{
    for (const GridZoneDir& z : zones_) {
        if (z.name == name) {
            return &z;
        }
    }
    return nullptr;
}

bool GridPack::decodeTile(const GridTileRef& t, GridTile& out) const
{
    out = GridTile {};
    if (base_ == nullptr) {
        return false;
    }
    if (t.records == 0) {
        return true;
    }
    std::vector<uint8_t> raw;
    if (!Inflate(base_ + t.offset, t.len, raw)) {
        return false;
    }
    return DecodeGridTile(raw.data(), raw.size(), out) && out.rec.size() == t.records;
}

std::vector<const GridTileRef*> GridTilesInRect(
    const GridZoneDir& zone,
    int64_t gx0,
    int64_t gy0,
    int64_t gx1,
    int64_t gy1)
{
    std::vector<const GridTileRef*> hit;
    for (const GridTileRef& t : zone.tiles) {
        const int64_t ox0 = t.gx0 + t.px0;
        const int64_t ox1 = t.gx0 + t.px1;
        const int64_t oy0 = t.gy0 + t.py0;
        const int64_t oy1 = t.gy0 + t.py1;
        if (ox0 > gx1 || ox1 < gx0 || oy0 > gy1 || oy1 < gy0) {
            continue;
        }
        hit.push_back(&t);
    }
    return hit;
}

}
