#include "RecastNavGridIO.h"

#include <cstring>

#include <zlib.h>

#include "RecastNavGrid.h"

namespace navmesh::recast
{

namespace
{

constexpr uint32_t kGridSectionVersion = 3;
constexpr uint32_t kGridSectionVersionColumnar = 2;
constexpr size_t kGridHeaderSize = 40;
constexpr size_t kGridTileEntrySize = 48;
constexpr size_t kGridZoneMinSize = 28;
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

int64_t UnZigzag(uint64_t value)
{
    return static_cast<int64_t>(value >> 1U) ^ -static_cast<int64_t>(value & 1U);
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
            if (!GetVarint(col[1], col_end[1], hq) || !GetVarint(col[2], col_end[2], rid) || !GetVarint(col[5], col_end[5], clr)) {
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
            r.h = (r.flags & kGridFlagFill) != 0 ? 0.0F : static_cast<float>(static_cast<double>(hmin) + static_cast<double>(hq) / 64.0);
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

bool DecodeGridTileV3(const uint8_t* data, size_t len, int32_t nx, GridTile& out)
{
    out = GridTile {};
    if (data == nullptr || nx <= 0) {
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

    // 类号字典的长度就是本瓦的类号数,不另存。
    uint64_t regions = 0;
    uint64_t ncell = 0;
    uint64_t kmax = 0;
    if (!GetVarint(p, end, regions) || !GetVarint(p, end, ncell) || !GetVarint(p, end, kmax)) {
        return false;
    }
    if (ncell == 0 || ncell > n || kmax == 0 || kmax > n || regions == 0 || regions > n || n > UINT32_MAX) {
        return false;
    }
    const size_t select_size = static_cast<size_t>(kmax) * kGridFieldCount;
    if (static_cast<size_t>(end - p) < select_size) {
        return false;
    }
    const uint8_t* const select = p;
    p += select_size;

    std::vector<uint8_t> stream[kGridStreamCount];
    for (std::vector<uint8_t>& s : stream) {
        uint64_t packed = 0;
        if (!GetVarint(p, end, packed) || static_cast<uint64_t>(end - p) < packed || !Inflate(p, static_cast<size_t>(packed), s)) {
            return false;
        }
        p += packed;
    }
    if (p != end) {
        return false;
    }
    // 每条记录给每条残差流至少一个字节,每个格给格号流与层数流各至少一个,类号同理。
    // 流已经解开了,流长就是这几个计数的现成上界:先卡住,再按计数开数组。
    if (n > stream[3].size() || ncell > stream[0].size() || ncell > stream[1].size() || regions > stream[2].size()) {
        return false;
    }

    // 格号、每格层数,以及该格首条记录在 rec 里的下标。
    std::vector<int64_t> cell(static_cast<size_t>(ncell));
    std::vector<uint64_t> depth(static_cast<size_t>(ncell));
    std::vector<size_t> base(static_cast<size_t>(ncell));
    const uint8_t* pc = stream[0].data();
    const uint8_t* pk = stream[1].data();
    const uint8_t* const pc_end = pc + stream[0].size();
    const uint8_t* const pk_end = pk + stream[1].size();
    int64_t running_cell = 0;
    size_t running_base = 0;
    for (size_t i = 0; i < static_cast<size_t>(ncell); ++i) {
        uint64_t step = 0;
        if (!GetVarint(pc, pc_end, step) || !GetVarint(pk, pk_end, depth[i]) || depth[i] == 0 || depth[i] > kmax) {
            return false;
        }
        running_cell += static_cast<int64_t>(step);
        cell[i] = running_cell;
        base[i] = running_base;
        running_base += static_cast<size_t>(depth[i]);
    }
    if (pc != pc_end || pk != pk_end || running_base != static_cast<size_t>(n)) {
        return false;
    }

    std::vector<uint32_t> dictionary(static_cast<size_t>(regions));
    const uint8_t* pd = stream[2].data();
    const uint8_t* const pd_end = pd + stream[2].size();
    for (uint32_t& id : dictionary) {
        uint64_t value = 0;
        if (!GetVarint(pd, pd_end, value) || value > UINT32_MAX) {
            return false;
        }
        id = static_cast<uint32_t>(value);
    }
    if (pd != pd_end) {
        return false;
    }

    out.regions = static_cast<uint32_t>(regions);
    out.rec.assign(static_cast<size_t>(n), GridSpanRec {});
    for (size_t i = 0; i < static_cast<size_t>(ncell); ++i) {
        for (uint64_t j = 0; j < depth[i]; ++j) {
            out.rec[base[i] + static_cast<size_t>(j)].cell = cell[i];
        }
    }

    // 每层的活格表按格号升序建一次,五个字段共用。逐字段逐层扫全格的话,扫到的
    // 格次是记录数的好几倍,而记录本身只有 n 条。列号同理,除法只做 ncell 次。
    std::vector<uint32_t> layer_cell(static_cast<size_t>(n));
    std::vector<size_t> layer_at(static_cast<size_t>(kmax) + 1, 0);
    for (size_t i = 0; i < static_cast<size_t>(ncell); ++i) {
        layer_at[static_cast<size_t>(depth[i])] += 1;
    }
    // 直方图先倒着累成「第 L 层有多少活格」,再正着累成起点。
    for (size_t d = static_cast<size_t>(kmax), live = 0; d >= 1; --d) {
        live += layer_at[d];
        layer_at[d] = live;
    }
    for (size_t layer = 0; layer < static_cast<size_t>(kmax); ++layer) {
        layer_at[layer + 1] += layer_at[layer];
    }
    std::vector<size_t> cursor(layer_at.begin(), layer_at.end() - 1);
    std::vector<int32_t> column_of(static_cast<size_t>(ncell));
    for (size_t i = 0; i < static_cast<size_t>(ncell); ++i) {
        column_of[i] = static_cast<int32_t>(cell[i] % nx);
        for (uint64_t layer = 0; layer < depth[i]; ++layer) {
            layer_cell[cursor[static_cast<size_t>(layer)]++] = static_cast<uint32_t>(i);
        }
    }

    // 高度与类号下标先落在临时列里:高度要等标志位解出来才知道是不是 fill 的占位 0。
    std::vector<int64_t> height_q(static_cast<size_t>(n), 0);
    std::vector<int64_t> region_index(static_cast<size_t>(n), 0);
    std::vector<int64_t> above(static_cast<size_t>(ncell), 0);
    std::vector<int64_t> current(static_cast<size_t>(ncell), 0);
    std::vector<int64_t> column(static_cast<size_t>(nx), 0);
    std::vector<uint64_t> column_layer(static_cast<size_t>(nx), 0);
    uint64_t layer_stamp = 0;

    for (int field = 0; field < kGridFieldCount; ++field) {
        const uint8_t* q = stream[3 + field].data();
        const uint8_t* const q_end = q + stream[3 + field].size();
        for (uint64_t layer = 0; layer < kmax; ++layer) {
            const uint8_t how = select[static_cast<size_t>(field) * kmax + layer];
            if (how != kGridPredUp && (how != kGridPredDown || layer == 0)) {
                return false;
            }
            ++layer_stamp;
            for (size_t k = layer_at[static_cast<size_t>(layer)]; k < layer_at[static_cast<size_t>(layer) + 1]; ++k) {
                const size_t i = layer_cell[k];
                uint64_t packed = 0;
                if (!GetVarint(q, q_end, packed)) {
                    return false;
                }
                const int64_t residual = UnZigzag(packed);
                int64_t value = 0;
                if (how == kGridPredUp) {
                    const auto x = static_cast<size_t>(column_of[i]);
                    value = column_layer[x] == layer_stamp ? column[x] + residual : residual;
                    column[x] = value;
                    column_layer[x] = layer_stamp;
                }
                else {
                    value = above[i] + residual;
                }
                current[i] = value;

                const size_t at = base[i] + static_cast<size_t>(layer);
                GridSpanRec& r = out.rec[at];
                switch (field) {
                case 0:
                    if (value < 0) {
                        return false;
                    }
                    height_q[at] = value;
                    break;
                case 1:
                    if (value < 0 || static_cast<uint64_t>(value) >= regions) {
                        return false;
                    }
                    region_index[at] = value;
                    break;
                case 2:
                    if (value < 0 || value > UINT16_MAX) {
                        return false;
                    }
                    r.clr = static_cast<uint16_t>(value);
                    break;
                case 3:
                    if (value < 0 || value > UINT8_MAX) {
                        return false;
                    }
                    r.flags = static_cast<uint8_t>(value);
                    break;
                default:
                    if (value < 0 || value > UINT8_MAX) {
                        return false;
                    }
                    r.steps = static_cast<uint8_t>(value);
                    break;
                }
            }
            above.swap(current);
        }
        if (q != q_end) {
            return false;
        }
    }

    for (size_t i = 0; i < static_cast<size_t>(n); ++i) {
        GridSpanRec& r = out.rec[i];
        r.rid = dictionary[static_cast<size_t>(region_index[i])];
        r.h =
            (r.flags & kGridFlagFill) != 0 ? 0.0F : static_cast<float>(static_cast<double>(hmin) + static_cast<double>(height_q[i]) / 64.0);
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
    if (!cur.u32(version) || !cur.f64(cell_size_) || !cur.f64(tile_px_) || !cur.f64(apron_px_) || !cur.u32(zone_count)
        || !cur.u32(reserved)) {
        err = "GRID header is truncated";
        return false;
    }
    if (version != kGridSectionVersion && version != kGridSectionVersionColumnar) {
        err = "GRID section version " + std::to_string(version) + " is not supported";
        return false;
    }
    version_ = version;
    // 格边长是判据常数,包和运行端对不上就整包不认:两边的格心不重合,烘出来的一切都错位。
    if (std::abs(cell_size_ - kCS) > 1e-12) {
        err = "GRID cell size does not match the runtime grid";
        return false;
    }

    // 一个区目录项最少 28 字节(名长、名字、两个原点、类号数、瓦数),照它先卡一遍数量。
    if (static_cast<size_t>(cur.end - cur.p) < static_cast<size_t>(zone_count) * kGridZoneMinSize) {
        err = "GRID zone directory is truncated";
        return false;
    }
    // 瓦的两边由段头的瓦边长与围裙定死(格心比格边多一格),后面按 nx 开的数组照它开。
    const int64_t max_side = static_cast<int64_t>((tile_px_ + 2.0 * apron_px_) / cell_size_) + 2;
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
        if (!cur.take(padded, name_bytes) || !cur.f64(z.x0) || !cur.f64(z.y0) || !cur.u32(z.global_regions) || !cur.u32(tile_count)) {
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
            if (!cur.ok || r.nx <= 0 || r.ny <= 0 || r.nx > max_side || r.ny > max_side || r.offset > len || r.len > len - r.offset) {
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
    if (version_ == kGridSectionVersion) {
        // v3 的载荷是逐流压的,整块不再套一层 deflate。
        return DecodeGridTileV3(base_ + t.offset, t.len, t.nx, out) && out.rec.size() == t.records;
    }
    std::vector<uint8_t> raw;
    if (!Inflate(base_ + t.offset, t.len, raw)) {
        return false;
    }
    return DecodeGridTile(raw.data(), raw.size(), out) && out.rec.size() == t.records;
}

std::vector<const GridTileRef*> GridTilesInRect(const GridZoneDir& zone, int64_t gx0, int64_t gy0, int64_t gx1, int64_t gy1)
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
