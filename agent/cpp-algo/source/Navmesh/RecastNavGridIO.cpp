#include "RecastNavGridIO.h"

#include <cstring>

#include <zlib.h>

#include <MaaUtils/Logger.h>

#include "NavVarint.h"
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

// 定长小端字段, 越界后 ok 置假, 调用方最后查一次即可
struct Cursor
{
    const uint8_t* p = nullptr;
    const uint8_t* end = nullptr;
    bool ok = true;
};

bool cursor_take(Cursor* cur, size_t n, const uint8_t** at)
{
    if (!cur->ok || static_cast<size_t>(cur->end - cur->p) < n) {
        cur->ok = false;
        return false;
    }
    *at = cur->p;
    cur->p += n;
    return true;
}

template <typename T>
bool cursor_read(Cursor* cur, T* out)
{
    const uint8_t* at = nullptr;
    if (!cursor_take(cur, sizeof(T), &at)) {
        return false;
    }
    std::memcpy(out, at, sizeof(T));
    return true;
}

bool cursor_u32(Cursor* cur, uint32_t* out)
{
    return cursor_read(cur, out);
}

bool cursor_u64(Cursor* cur, uint64_t* out)
{
    return cursor_read(cur, out);
}

bool cursor_i32(Cursor* cur, int32_t* out)
{
    return cursor_read(cur, out);
}

bool cursor_f64(Cursor* cur, double* out)
{
    return cursor_read(cur, out);
}

}

bool DecodeGridTile(GridTile* out, const uint8_t* data, size_t len)
{
    *out = GridTile {};
    if (data == nullptr) {
        return false;
    }
    const uint8_t* p = data;
    const uint8_t* const end = data + len;

    uint64_t n = 0;
    if (!GetVarint(&n, &p, end)) {
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
    if (!GetVarint(&regions, &p, end) || !GetVarint(&ncell, &p, end)) {
        return false;
    }

    // 六列各带长度前缀, 依次为格、高、类号、标志、断口、净空
    const uint8_t* col[6] = {};
    const uint8_t* col_end[6] = {};
    for (int i = 0; i < 6; ++i) {
        uint64_t clen = 0;
        if (!GetVarint(&clen, &p, end) || static_cast<uint64_t>(end - p) < clen) {
            return false;
        }
        col[i] = p;
        col_end[i] = p + clen;
        p += clen;
    }
    if (p != end) {
        return false;
    }

    out->regions = static_cast<uint32_t>(regions);
    out->rec.reserve(static_cast<size_t>(n));
    int64_t cell = 0;
    for (uint64_t i = 0; i < ncell; ++i) {
        uint64_t delta = 0;
        uint64_t k = 0;
        if (!GetVarint(&delta, &col[0], col_end[0]) || !GetVarint(&k, &col[0], col_end[0])) {
            return false;
        }
        cell += static_cast<int64_t>(delta);
        for (uint64_t j = 0; j < k; ++j) {
            if (out->rec.size() >= static_cast<size_t>(n)) {
                return false;
            }
            uint64_t hq = 0;
            uint64_t rid = 0;
            uint64_t clr = 0;
            if (!GetVarint(&hq, &col[1], col_end[1]) || !GetVarint(&rid, &col[2], col_end[2]) || !GetVarint(&clr, &col[5], col_end[5])) {
                return false;
            }
            if (col[3] == col_end[3] || col[4] == col_end[4]) {
                return false;
            }
            GridSpanRec r {};
            r.cell = static_cast<int32_t>(cell);
            r.rid = static_cast<uint32_t>(rid);
            r.clr = static_cast<uint16_t>(clr);
            r.flags = *col[3]++;
            r.steps = *col[4]++;
            r.h = (r.flags & kGridFlagFill) != 0 ? 0.0F : static_cast<float>(static_cast<double>(hmin) + static_cast<double>(hq) / 64.0);
            out->rec.push_back(r);
        }
    }
    if (out->rec.size() != static_cast<size_t>(n)) {
        return false;
    }
    for (int i = 0; i < 6; ++i) {
        if (col[i] != col_end[i]) {
            return false;
        }
    }
    return true;
}

// 头部须与 DecodeGridTileV3 逐字段相同
bool DecodeGridTileRegionsV3(std::vector<uint32_t>* out, const uint8_t* data, size_t len)
{
    out->clear();
    if (data == nullptr) {
        return false;
    }
    const uint8_t* p = data;
    const uint8_t* const end = data + len;

    uint64_t n = 0;
    if (!GetVarint(&n, &p, end)) {
        return false;
    }
    if (n == 0) {
        return p == end;
    }
    if (static_cast<size_t>(end - p) < sizeof(float)) {
        return false;
    }
    p += sizeof(float);

    uint64_t regions = 0;
    uint64_t ncell = 0;
    uint64_t kmax = 0;
    if (!GetVarint(&regions, &p, end) || !GetVarint(&ncell, &p, end) || !GetVarint(&kmax, &p, end)) {
        return false;
    }
    if (ncell == 0 || ncell > n || kmax == 0 || kmax > n || regions == 0 || regions > n || n > UINT32_MAX) {
        return false;
    }
    const size_t select_size = static_cast<size_t>(kmax) * kGridFieldCount;
    if (static_cast<size_t>(end - p) < select_size) {
        return false;
    }
    p += select_size;

    std::vector<uint8_t> dict;
    for (int i = 0; i < 3; ++i) {
        uint64_t packed = 0;
        if (!GetVarint(&packed, &p, end) || static_cast<uint64_t>(end - p) < packed) {
            return false;
        }
        if (i == 2 && !InflateBytes(&dict, p, static_cast<size_t>(packed))) {
            return false;
        }
        p += packed;
    }
    if (regions > dict.size()) {
        return false;
    }
    out->resize(static_cast<size_t>(regions));
    const uint8_t* pd = dict.data();
    const uint8_t* const pd_end = pd + dict.size();
    for (uint32_t& id : *out) {
        uint64_t value = 0;
        if (!GetVarint(&value, &pd, pd_end) || value > UINT32_MAX) {
            return false;
        }
        id = static_cast<uint32_t>(value);
    }
    return pd == pd_end;
}

bool DecodeGridTileV3(GridTile* out, const uint8_t* data, size_t len, int32_t nx)
{
    *out = GridTile {};
    if (data == nullptr || nx <= 0) {
        return false;
    }
    const uint8_t* p = data;
    const uint8_t* const end = data + len;

    uint64_t n = 0;
    if (!GetVarint(&n, &p, end)) {
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

    // 类号字典的长度即本瓦类号数, 不另存
    uint64_t regions = 0;
    uint64_t ncell = 0;
    uint64_t kmax = 0;
    if (!GetVarint(&regions, &p, end) || !GetVarint(&ncell, &p, end) || !GetVarint(&kmax, &p, end)) {
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
        if (!GetVarint(&packed, &p, end) || static_cast<uint64_t>(end - p) < packed || !InflateBytes(&s, p, static_cast<size_t>(packed))) {
            return false;
        }
        p += packed;
    }
    if (p != end) {
        return false;
    }
    // 每条记录在对应流里至少占一字节, 所以流长是各计数的上界
    if (n > stream[3].size() || ncell > stream[0].size() || ncell > stream[1].size() || regions > stream[2].size()) {
        return false;
    }

    // base 是该格首条记录在 rec 里的下标
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
        if (!GetVarint(&step, &pc, pc_end) || !GetVarint(&depth[i], &pk, pk_end) || depth[i] == 0 || depth[i] > kmax) {
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
        if (!GetVarint(&value, &pd, pd_end) || value > UINT32_MAX) {
            return false;
        }
        id = static_cast<uint32_t>(value);
    }
    if (pd != pd_end) {
        return false;
    }

    out->regions = static_cast<uint32_t>(regions);
    out->rec.assign(static_cast<size_t>(n), GridSpanRec {});
    for (size_t i = 0; i < static_cast<size_t>(ncell); ++i) {
        for (uint64_t j = 0; j < depth[i]; ++j) {
            out->rec[base[i] + static_cast<size_t>(j)].cell = static_cast<int32_t>(cell[i]);
        }
    }

    // 每层活格按格号升序排, 五个字段共用
    std::vector<uint32_t> layer_cell(static_cast<size_t>(n));
    std::vector<size_t> layer_at(static_cast<size_t>(kmax) + 1, 0);
    for (size_t i = 0; i < static_cast<size_t>(ncell); ++i) {
        layer_at[static_cast<size_t>(depth[i])] += 1;
    }
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

    // 高度要等标志位解出后才知道是否为 fill 占位, 所以先落临时列
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
                if (!GetVarint(&packed, &q, q_end)) {
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
                GridSpanRec& r = out->rec[at];
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
        GridSpanRec& r = out->rec[i];
        r.rid = dictionary[static_cast<size_t>(region_index[i])];
        r.h =
            (r.flags & kGridFlagFill) != 0 ? 0.0F : static_cast<float>(static_cast<double>(hmin) + static_cast<double>(height_q[i]) / 64.0);
    }
    return true;
}

bool ParseGridPack(GridPack* pack, const uint8_t* data, size_t len)
{
    pack->base = nullptr;
    pack->len = 0;
    pack->zones.clear();
    if (data == nullptr || len < kGridHeaderSize) {
        LogError << "GRID section is truncated." << VAR(len);
        return false;
    }
    if (std::memcmp(data, kGridSectionTag, 4) != 0) {
        LogError << "GRID section has a bad magic.";
        return false;
    }
    Cursor cur { data + 4, data + len };
    uint32_t version = 0;
    uint32_t zone_count = 0;
    uint32_t reserved = 0;
    if (!cursor_u32(&cur, &version) || !cursor_f64(&cur, &pack->cell_size) || !cursor_f64(&cur, &pack->tile_px)
        || !cursor_f64(&cur, &pack->apron_px) || !cursor_u32(&cur, &zone_count) || !cursor_u32(&cur, &reserved)) {
        LogError << "GRID header is truncated.";
        return false;
    }
    if (version != kGridSectionVersion && version != kGridSectionVersionColumnar) {
        LogError << "GRID section version is not supported." << VAR(version);
        return false;
    }
    pack->version = version;
    if (std::abs(pack->cell_size - kCS) > 1e-12) {
        LogError << "GRID cell size does not match the runtime grid." << VAR(pack->cell_size) << VAR(kCS);
        return false;
    }

    // 区目录项最少 28 字节: 名长、名字、两个原点、类号数、瓦数
    if (static_cast<size_t>(cur.end - cur.p) < static_cast<size_t>(zone_count) * kGridZoneMinSize) {
        LogError << "GRID zone directory is truncated.";
        return false;
    }
    // 瓦边格数上限由段头的瓦边长与围裙定死
    const int64_t max_side = static_cast<int64_t>((pack->tile_px + 2.0 * pack->apron_px) / pack->cell_size) + 2;
    pack->zones.resize(zone_count);
    for (uint32_t i = 0; i < zone_count; ++i) {
        GridZoneDir& z = pack->zones[i];
        uint32_t name_len = 0;
        uint32_t tile_count = 0;
        if (!cursor_u32(&cur, &name_len)) {
            LogError << "GRID zone directory is truncated.";
            return false;
        }
        const size_t padded = (static_cast<size_t>(name_len) + 3) / 4 * 4;
        const uint8_t* name_bytes = nullptr;
        if (!cursor_take(&cur, padded, &name_bytes) || !cursor_f64(&cur, &z.x0) || !cursor_f64(&cur, &z.y0)
            || !cursor_u32(&cur, &z.global_regions) || !cursor_u32(&cur, &tile_count)) {
            LogError << "GRID zone directory is truncated.";
            return false;
        }
        z.name.assign(reinterpret_cast<const char*>(name_bytes), name_len);
        if (static_cast<size_t>(cur.end - cur.p) < static_cast<size_t>(tile_count) * kGridTileEntrySize) {
            LogError << "GRID tile directory is truncated." << VAR(z.name) << VAR(tile_count);
            return false;
        }
        z.tiles.resize(tile_count);
        for (uint32_t t = 0; t < tile_count; ++t) {
            GridTileRef& r = z.tiles[t];
            int32_t* const fields[8] = { &r.gx0, &r.gy0, &r.nx, &r.ny, &r.px0, &r.px1, &r.py0, &r.py1 };
            for (int32_t* f : fields) {
                cursor_i32(&cur, f);
            }
            cursor_u64(&cur, &r.offset);
            cursor_u32(&cur, &r.len);
            cursor_u32(&cur, &r.records);
            if (!cur.ok || r.nx <= 0 || r.ny <= 0 || r.nx > max_side || r.ny > max_side || r.offset > len || r.len > len - r.offset) {
                LogError << "GRID tile points outside the section." << VAR(z.name) << VAR(t);
                return false;
            }
        }
    }
    pack->base = data;
    pack->len = len;
    return true;
}

bool GridPackParsed(const GridPack& pack)
{
    return pack.base != nullptr;
}

const GridZoneDir* FindGridZone(const GridPack& pack, const std::string& name)
{
    for (const GridZoneDir& z : pack.zones) {
        if (z.name == name) {
            return &z;
        }
    }
    return nullptr;
}

bool DecodeGridPackTile(GridTile* out, const GridPack& pack, const GridTileRef& tile)
{
    *out = GridTile {};
    if (pack.base == nullptr) {
        return false;
    }
    if (tile.records == 0) {
        return true;
    }
    if (pack.version == kGridSectionVersion) {
        // v3 载荷逐流压缩, 整块没有外层 deflate
        return DecodeGridTileV3(out, pack.base + tile.offset, tile.len, tile.nx) && out->rec.size() == tile.records;
    }
    std::vector<uint8_t> raw;
    if (!InflateBytes(&raw, pack.base + tile.offset, tile.len)) {
        return false;
    }
    return DecodeGridTile(out, raw.data(), raw.size()) && out->rec.size() == tile.records;
}

bool GridPackTileRegions(std::vector<uint32_t>* out, const GridPack& pack, const GridTileRef& tile)
{
    out->clear();
    if (pack.base == nullptr) {
        return false;
    }
    if (tile.records == 0) {
        return true;
    }
    if (pack.version == kGridSectionVersion) {
        return DecodeGridTileRegionsV3(out, pack.base + tile.offset, tile.len);
    }
    GridTile decoded;
    if (!DecodeGridPackTile(&decoded, pack, tile)) {
        return false;
    }
    for (const GridSpanRec& r : decoded.rec) {
        out->push_back(r.rid);
    }
    std::sort(out->begin(), out->end());
    out->erase(std::unique(out->begin(), out->end()), out->end());
    return true;
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

bool InflateBytes(std::vector<uint8_t>* out, const uint8_t* data, size_t len)
{
    out->clear();
    z_stream zs {};
    if (inflateInit(&zs) != Z_OK) {
        return false;
    }
    zs.next_in = const_cast<Bytef*>(data);
    zs.avail_in = static_cast<uInt>(len);
    int rc = Z_OK;
    do {
        const size_t at = out->size();
        out->resize(at + kInflateChunk);
        zs.next_out = out->data() + at;
        zs.avail_out = static_cast<uInt>(kInflateChunk);
        rc = inflate(&zs, Z_NO_FLUSH);
        if (rc != Z_OK && rc != Z_STREAM_END) {
            inflateEnd(&zs);
            return false;
        }
        out->resize(at + kInflateChunk - zs.avail_out);
    } while (rc != Z_STREAM_END);
    inflateEnd(&zs);
    return true;
}

}
