#include <algorithm>
#include <array>
#include <bit>
#include <cstddef>
#include <cstdint>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <memory>
#include <mutex>
#include <optional>
#include <set>
#include <string>
#include <string_view>
#include <unordered_map>
#include <utility>
#include <vector>

#include <zlib.h>

#include <MaaUtils/Logger.h>

#include "BaseNavReader.h"
#include "NavParallel.h"

namespace navmesh
{

namespace
{

constexpr size_t kHeaderSize = 64;
constexpr size_t kHeaderSizeV4 = 80;
constexpr size_t kZonePrefixSize = 44;
constexpr size_t kVertexSize = 12;
constexpr size_t kTriangleSize = 36;
constexpr size_t kLinkSize = 8;
constexpr size_t kSectionEntrySize = 24;
constexpr uint16_t kBaseNavRevision = 5;
// 各字段的起始版本, 判字段存在与否只能用它们, 用 kBaseNavRevision 判会在升版后跳过老包字段
constexpr uint16_t kZoneFloorYSince = 3;
constexpr uint16_t kSectionDirSince = 4;
constexpr uint16_t kGeometrySectionSince = 5;
constexpr size_t kGzipReadChunkSize = 4 << 20;
// BGEO 段: 四块几何按定长块存, 块内自足解码
constexpr char kGeometrySectionTag[5] = "BGEO";
constexpr char kOffMeshSectionTag[5] = "BOML";
constexpr char kSurfaceSectionTag[5] = "BSRF";
constexpr char kGridSectionTag[5] = "BGRD";
constexpr uint32_t kGeometrySectionVersion = 1;
// v1 = 80 B, 尾部 4 B 补零, v2 = 88 B, 从补零处起写方向位与两沿三角
constexpr uint16_t kOffMeshSectionVersion = 2;
constexpr uint16_t kOffMeshRecordSizeV1 = 80;
constexpr uint16_t kOffMeshRecordSizeV2 = 88;
constexpr size_t kOffMeshHeaderSize = 16;
constexpr uint16_t kSurfaceSectionVersion = 1;
constexpr uint16_t kSurfaceRecordSize = 8;
constexpr size_t kSurfaceHeaderSize = 16;
constexpr size_t kGeoHeaderSize = 72;
constexpr size_t kGeoChunkEntrySize = 16;
constexpr size_t kInflateChunkSize = 1U << 16U;
constexpr uint64_t kFnvOffset = 14'695'981'039'346'656'037ULL;
constexpr uint64_t kFnvPrime = 1'099'511'628'211ULL;

bool read_exact(std::istream* input, uint8_t* out, size_t size)
{
    input->read(reinterpret_cast<char*>(out), static_cast<std::streamsize>(size));
    return input->good() || input->gcount() == static_cast<std::streamsize>(size);
}

uint16_t read_u16(const uint8_t** cursor)
{
    const uint16_t value = static_cast<uint16_t>((*cursor)[0]) | (static_cast<uint16_t>((*cursor)[1]) << 8);
    *cursor += 2;
    return value;
}

uint32_t read_u32(const uint8_t** cursor)
{
    const uint32_t value = static_cast<uint32_t>((*cursor)[0]) | (static_cast<uint32_t>((*cursor)[1]) << 8)
                           | (static_cast<uint32_t>((*cursor)[2]) << 16) | (static_cast<uint32_t>((*cursor)[3]) << 24);
    *cursor += 4;
    return value;
}

uint32_t peek_u32(const uint8_t* bytes)
{
    return static_cast<uint32_t>(bytes[0]) | (static_cast<uint32_t>(bytes[1]) << 8) | (static_cast<uint32_t>(bytes[2]) << 16)
           | (static_cast<uint32_t>(bytes[3]) << 24);
}

uint64_t read_u64(const uint8_t** cursor)
{
    uint64_t value = 0;
    for (int index = 0; index < 8; ++index) {
        value |= static_cast<uint64_t>((*cursor)[index]) << (index * 8);
    }
    *cursor += 8;
    return value;
}

int32_t read_i32(const uint8_t** cursor)
{
    return static_cast<int32_t>(read_u32(cursor));
}

float read_f32(const uint8_t** cursor)
{
    const uint32_t bits = read_u32(cursor);
    float value = 0.0F;
    std::memcpy(&value, &bits, sizeof(value));
    return value;
}

BaseNavTriangle read_triangle_record(const uint8_t** cursor)
{
    BaseNavTriangle triangle;
    for (uint32_t& value : triangle.vertices) {
        value = read_u32(cursor);
    }
    for (int32_t& value : triangle.neighbors) {
        value = read_i32(cursor);
    }
    // 跳过分量号与两个 float 重心, 哈希仍按整条 36 字节记录算
    *cursor += 12;
    return triangle;
}

bool parse_off_mesh_section(const uint8_t* data, size_t size, std::vector<BaseNavOffMeshLink>* links)
{
    links->clear();
    if (size < kOffMeshHeaderSize || std::memcmp(data, kOffMeshSectionTag, 4) != 0) {
        return false;
    }
    const uint8_t* cursor = data + 4;
    const uint16_t version = read_u16(&cursor);
    const uint16_t record_size = read_u16(&cursor);
    const uint32_t count = read_u32(&cursor);
    (void)read_u32(&cursor);
    const uint16_t expect_record_size = version == 1 ? kOffMeshRecordSizeV1 : (version == 2 ? kOffMeshRecordSizeV2 : 0);
    if (version > kOffMeshSectionVersion || expect_record_size == 0 || record_size != expect_record_size
        || count != (size - kOffMeshHeaderSize) / record_size || size != kOffMeshHeaderSize + static_cast<size_t>(count) * record_size) {
        return false;
    }
    links->reserve(count);
    for (uint32_t index = 0; index < count; ++index) {
        BaseNavOffMeshLink link;
        link.zone_id = read_u16(&cursor);
        link.kind = *cursor++;
        ++cursor;
        link.is_ext = read_i32(&cursor);
        link.bidirectional = read_i32(&cursor);
        link.area = read_i32(&cursor);
        link.link_type = read_u16(&cursor);
        link.direction = *cursor++;
        ++cursor;
        link.radius = read_f32(&cursor);
        link.cost_modifier = read_f32(&cursor);
        for (BaseNavVertex& point : link.points) {
            point.u = read_f32(&cursor);
            point.v = read_f32(&cursor);
            point.height = read_f32(&cursor);
        }
        if (version >= 2) {
            link.dir_ok = *cursor++;
            cursor += 3; // 对齐填充
            link.start_triangle = read_u32(&cursor);
            link.end_triangle = read_u32(&cursor);
        }
        else {
            (void)read_u32(&cursor);
        }
        links->push_back(link);
    }
    return cursor == data + size;
}

bool parse_surface_section(const uint8_t* data, size_t size, std::vector<BaseNavSurface>* surfaces)
{
    surfaces->clear();
    if (size < kSurfaceHeaderSize || std::memcmp(data, kSurfaceSectionTag, 4) != 0) {
        return false;
    }
    const uint8_t* cursor = data + 4;
    const uint16_t version = read_u16(&cursor);
    const uint16_t record_size = read_u16(&cursor);
    const uint32_t count = read_u32(&cursor);
    (void)read_u32(&cursor);
    if (version != kSurfaceSectionVersion || record_size != kSurfaceRecordSize || count != (size - kSurfaceHeaderSize) / kSurfaceRecordSize
        || size != kSurfaceHeaderSize + static_cast<size_t>(count) * kSurfaceRecordSize) {
        return false;
    }
    surfaces->reserve(count);
    for (uint32_t index = 0; index < count; ++index) {
        BaseNavSurface surface;
        surface.area = *cursor++;
        surface.poly_type = *cursor++;
        (void)read_u16(&cursor);
        surface.flags = read_u32(&cursor);
        surfaces->push_back(surface);
    }
    return cursor == data + size;
}

uint64_t fnv64_update(uint64_t hash, const uint8_t* bytes, size_t size)
{
    for (size_t index = 0; index < size; ++index) {
        hash ^= bytes[index];
        hash *= kFnvPrime;
    }
    return hash;
}

bool offset_range_valid(uint64_t offset, uint64_t size, uint64_t file_size)
{
    return offset <= file_size && size <= file_size - offset;
}

bool inflate_buffer(const uint8_t* data, size_t size, std::vector<uint8_t>* output)
{
    output->clear();
    z_stream stream {};
    if (inflateInit(&stream) != Z_OK) {
        return false;
    }
    stream.next_in = const_cast<Bytef*>(data);
    stream.avail_in = static_cast<uInt>(size);
    int rc = Z_OK;
    do {
        const size_t at = output->size();
        output->resize(at + kInflateChunkSize);
        stream.next_out = output->data() + at;
        stream.avail_out = static_cast<uInt>(kInflateChunkSize);
        rc = inflate(&stream, Z_NO_FLUSH);
        if (rc != Z_OK && rc != Z_STREAM_END) {
            inflateEnd(&stream);
            return false;
        }
        output->resize(at + kInflateChunkSize - stream.avail_out);
    } while (rc != Z_STREAM_END);
    inflateEnd(&stream);
    return true;
}

bool GetVarint(const uint8_t*& cursor, const uint8_t* end, uint64_t& out)
{
    out = 0;
    for (int shift = 0; shift < 64; shift += 7) {
        if (cursor == end) {
            return false;
        }
        const uint8_t byte = *cursor++;
        out |= static_cast<uint64_t>(byte & 0x7FU) << shift;
        if ((byte & 0x80U) == 0) {
            return true;
        }
    }
    return false;
}

int64_t UnZigzag(uint64_t value)
{
    return static_cast<int64_t>(value >> 1U) ^ -static_cast<int64_t>(value & 1U);
}

// 块内是若干条 varint 长度 + deflate 流, 条数由表类型定死
bool split_chunk_streams(const uint8_t* data, size_t size, size_t count, std::vector<std::vector<uint8_t>>* out)
{
    out->assign(count, {});
    const uint8_t* cursor = data;
    const uint8_t* const end = data + size;
    for (std::vector<uint8_t>& stream : *out) {
        uint64_t packed = 0;
        if (!GetVarint(cursor, end, packed) || static_cast<uint64_t>(end - cursor) < packed
            || !inflate_buffer(cursor, static_cast<size_t>(packed), &stream)) {
            return false;
        }
        cursor += packed;
    }
    return cursor == end;
}

// 第 i 块覆盖 [i * chunk, min((i + 1) * chunk, total))
struct GeoChunkTable
{
    const uint8_t* directory = nullptr;
    uint32_t chunk = 0;
    uint32_t total = 0;
};

uint32_t geo_chunk_count(const GeoChunkTable& table)
{
    return table.chunk == 0 ? 0 : (table.total + table.chunk - 1) / table.chunk;
}

uint32_t geo_chunk_first(const GeoChunkTable& table, uint32_t index)
{
    return index * table.chunk;
}

uint32_t geo_chunk_span(const GeoChunkTable& table, uint32_t index)
{
    return std::min(table.chunk, table.total - geo_chunk_first(table, index));
}

uint32_t geo_chunk_aux(const GeoChunkTable& table, uint32_t index)
{
    return peek_u32(table.directory + index * kGeoChunkEntrySize + 12);
}

void geo_chunk_entry(uint64_t* offset, uint32_t* size, const GeoChunkTable& table, uint32_t index)
{
    const uint8_t* cursor = table.directory + static_cast<size_t>(index) * kGeoChunkEntrySize;
    *offset = read_u64(&cursor);
    *size = read_u32(&cursor);
}

const uint8_t* geo_chunk_bytes(uint32_t* size, const GeoChunkTable& table, const uint8_t* base, uint32_t index)
{
    uint64_t offset = 0;
    geo_chunk_entry(&offset, size, table, index);
    return base + offset;
}

struct GeometrySection
{
    const uint8_t* base = nullptr;
    const uint8_t* zone_table = nullptr;
    uint64_t zone_table_size = 0;
    GeoChunkTable vertices;
    GeoChunkTable triangles;
    GeoChunkTable links;
};

bool parse_geometry_section(const uint8_t* data, uint64_t size, GeometrySection* out)
{
    if (data == nullptr || size < kGeoHeaderSize || std::memcmp(data, kGeometrySectionTag, 4) != 0) {
        return false;
    }
    const uint8_t* cursor = data + 4;
    if (read_u32(&cursor) != kGeometrySectionVersion) {
        return false;
    }
    out->base = data;
    out->vertices.total = read_u32(&cursor);
    out->triangles.total = read_u32(&cursor);
    out->links.total = read_u32(&cursor);
    out->vertices.chunk = read_u32(&cursor);
    out->triangles.chunk = read_u32(&cursor);
    out->links.chunk = read_u32(&cursor);
    out->zone_table_size = read_u32(&cursor);
    (void)read_u32(&cursor); // 保留字段
    GeoChunkTable* const tables[3] = { &out->vertices, &out->triangles, &out->links };
    for (GeoChunkTable* table : tables) {
        const uint64_t at = read_u64(&cursor);
        if (table->chunk == 0 || !offset_range_valid(at, static_cast<uint64_t>(geo_chunk_count(*table)) * kGeoChunkEntrySize, size)) {
            return false;
        }
        table->directory = data + at;
    }
    const uint64_t zone_at = read_u64(&cursor);
    if (!offset_range_valid(zone_at, out->zone_table_size, size)) {
        return false;
    }
    out->zone_table = data + zone_at;
    // 校验只看偏移, 先加到指针上再判越界是未定义行为
    for (const GeoChunkTable* table : tables) {
        for (uint32_t i = 0; i < geo_chunk_count(*table); ++i) {
            uint64_t at = 0;
            uint32_t chunk_size = 0;
            geo_chunk_entry(&at, &chunk_size, *table, i);
            if (!offset_range_valid(at, chunk_size, size)) {
                return false;
            }
        }
    }
    return true;
}

// 顶点按字节平面存, n × 12 的字节矩阵转置成 12 × n
bool decode_vertex_chunk(const uint8_t* data, size_t size, uint32_t count, uint8_t* out)
{
    std::vector<std::vector<uint8_t>> stream;
    if (!split_chunk_streams(data, size, 1, &stream) || stream[0].size() != static_cast<size_t>(count) * kVertexSize) {
        return false;
    }
    for (size_t i = 0; i < count; ++i) {
        for (size_t j = 0; j < kVertexSize; ++j) {
            out[i * kVertexSize + j] = stream[0][j * count + i];
        }
    }
    return true;
}

// 重心不存, 先留空, 顶点到位后按 ((a+b)+c)/3.0f 重算
bool decode_triangle_chunk(const uint8_t* data, size_t size, uint32_t count, uint32_t first, uint8_t* out)
{
    std::vector<std::vector<uint8_t>> stream;
    if (!split_chunk_streams(data, size, 7, &stream) || stream[3].size() != (static_cast<size_t>(count) * 3 + 7) / 8) {
        return false;
    }
    const uint8_t* index_cursor[3] = { stream[0].data(), stream[1].data(), stream[2].data() };
    const uint8_t* index_end[3] = {};
    for (int i = 0; i < 3; ++i) {
        index_end[i] = stream[i].data() + stream[i].size();
    }
    const uint8_t* neighbor_cursor = stream[4].data();
    const uint8_t* const neighbor_end = neighbor_cursor + stream[4].size();

    int64_t running = 0;
    for (uint32_t t = 0; t < count; ++t) {
        uint64_t packed[3] = {};
        for (int i = 0; i < 3; ++i) {
            if (!GetVarint(index_cursor[i], index_end[i], packed[i])) {
                return false;
            }
        }
        running += UnZigzag(packed[0]);
        const int64_t vertex[3] = { running, running + UnZigzag(packed[1]), running + UnZigzag(packed[2]) };
        uint8_t* record = out + static_cast<size_t>(t) * kTriangleSize;
        for (int i = 0; i < 3; ++i) {
            if (vertex[i] < 0 || vertex[i] > UINT32_MAX) {
                return false;
            }
            const uint32_t value = static_cast<uint32_t>(vertex[i]);
            std::memcpy(record + i * 4, &value, 4);
        }
        const int64_t self_index = static_cast<int64_t>(first) + t;
        for (int i = 0; i < 3; ++i) {
            int32_t neighbor = -1;
            const size_t bit = static_cast<size_t>(t) * 3 + i;
            if ((stream[3][bit / 8] & (1U << (7 - bit % 8))) != 0) {
                uint64_t delta = 0;
                if (!GetVarint(neighbor_cursor, neighbor_end, delta)) {
                    return false;
                }
                const int64_t value = self_index + UnZigzag(delta);
                if (value < 0 || value > INT32_MAX) {
                    return false;
                }
                neighbor = static_cast<int32_t>(value);
            }
            std::memcpy(record + 12 + i * 4, &neighbor, 4);
        }
    }

    const uint8_t* value_cursor = stream[5].data();
    const uint8_t* const value_end = value_cursor + stream[5].size();
    const uint8_t* length_cursor = stream[6].data();
    const uint8_t* const length_end = length_cursor + stream[6].size();
    int64_t component = 0;
    uint32_t filled = 0;
    while (filled < count) {
        uint64_t delta = 0;
        uint64_t run = 0;
        if (!GetVarint(value_cursor, value_end, delta) || !GetVarint(length_cursor, length_end, run)) {
            return false;
        }
        component += UnZigzag(delta);
        if (run == 0 || run > count - filled || component < 0 || component > UINT32_MAX) {
            return false;
        }
        const uint32_t value = static_cast<uint32_t>(component);
        for (uint64_t i = 0; i < run; ++i) {
            std::memcpy(out + static_cast<size_t>(filled + i) * kTriangleSize + 24, &value, 4);
        }
        filled += static_cast<uint32_t>(run);
    }
    for (int i = 0; i < 3; ++i) {
        if (index_cursor[i] != index_end[i]) {
            return false;
        }
    }
    return neighbor_cursor == neighbor_end && value_cursor == value_end && length_cursor == length_end;
}

bool decode_link_chunk(const uint8_t* data, size_t size, uint32_t count, uint8_t* out)
{
    std::vector<std::vector<uint8_t>> stream;
    if (!split_chunk_streams(data, size, 2, &stream)) {
        return false;
    }
    const uint8_t* source_cursor = stream[0].data();
    const uint8_t* const source_end = source_cursor + stream[0].size();
    const uint8_t* target_cursor = stream[1].data();
    const uint8_t* const target_end = target_cursor + stream[1].size();
    int64_t source = 0;
    for (uint32_t i = 0; i < count; ++i) {
        uint64_t step = 0;
        uint64_t delta = 0;
        if (!GetVarint(source_cursor, source_end, step) || !GetVarint(target_cursor, target_end, delta)) {
            return false;
        }
        source = i == 0 ? static_cast<int64_t>(step) : source + static_cast<int64_t>(step);
        const int64_t target = source + UnZigzag(delta);
        if (source < 0 || source > UINT32_MAX || target < 0 || target > UINT32_MAX) {
            return false;
        }
        const uint32_t pair[2] = { static_cast<uint32_t>(source), static_cast<uint32_t>(target) };
        std::memcpy(out + static_cast<size_t>(i) * kLinkSize, pair, sizeof(pair));
    }
    return source_cursor == source_end && target_cursor == target_end;
}

size_t chunk_span_records(const GeoChunkTable& table, uint32_t low, uint32_t high)
{
    return static_cast<size_t>(geo_chunk_first(table, high)) + geo_chunk_span(table, high) - geo_chunk_first(table, low);
}

enum class GeoChunkKind
{
    Vertex,
    Triangle,
    Link,
};

bool decode_chunk(GeoChunkKind kind, const uint8_t* at, size_t size, uint32_t span, uint32_t first, uint8_t* dst)
{
    switch (kind) {
    case GeoChunkKind::Vertex:
        return decode_vertex_chunk(at, size, span, dst);
    case GeoChunkKind::Triangle:
        return decode_triangle_chunk(at, size, span, first, dst);
    case GeoChunkKind::Link:
        return decode_link_chunk(at, size, span, dst);
    }
    return false;
}

// 解第 [low, high] 块到 dst, dst 须按 chunk_span_records 开好
bool decode_chunk_span(
    const GeoChunkTable& table,
    const uint8_t* base,
    uint32_t low,
    uint32_t high,
    size_t record_size,
    uint8_t* dst,
    GeoChunkKind kind)
{
    const uint32_t base_record = geo_chunk_first(table, low);
    const auto chunks = static_cast<int64_t>(high - low + 1);
    std::vector<uint8_t> ok(static_cast<size_t>(chunks), 1);
    ParallelChunks(chunks, NavWorkerCount(static_cast<int64_t>(chunk_span_records(table, low, high))), [&](size_t, int64_t b, int64_t e) {
        for (int64_t c = b; c < e; ++c) {
            const uint32_t i = low + static_cast<uint32_t>(c);
            uint32_t size = 0;
            const uint8_t* src = geo_chunk_bytes(&size, table, base, i);
            uint8_t* at = dst + (static_cast<size_t>(geo_chunk_first(table, i)) - base_record) * record_size;
            if (!decode_chunk(kind, src, size, geo_chunk_span(table, i), geo_chunk_first(table, i), at)) {
                ok[static_cast<size_t>(c)] = 0;
            }
        }
    });
    return std::find(ok.begin(), ok.end(), 0) == ok.end();
}

// 覆盖记录 [from, to) 的整块范围, from >= to 时为空且 first 取 from
struct ChunkWindow
{
    uint32_t low = 0;
    uint32_t high = 0;
    uint32_t first = 0;
    size_t records = 0;
};

std::optional<ChunkWindow> chunk_window_for(const GeoChunkTable& table, uint32_t from, uint32_t to)
{
    ChunkWindow window;
    if (from >= to) {
        window.first = from;
        return window;
    }
    window.low = from / table.chunk;
    window.high = (to - 1) / table.chunk;
    if (window.high >= geo_chunk_count(table)) {
        return std::nullopt;
    }
    window.first = geo_chunk_first(table, window.low);
    window.records = chunk_span_records(table, window.low, window.high);
    return window;
}

// 三角逐批解码的每批记录数, 用来压低加载峰值内存
constexpr size_t kTriangleBatchRecords = 1U << 20U;

BaseNavLoadResult fail(BaseNavLoadStatus status, std::string message)
{
    BaseNavLoadResult result;
    result.status = status;
    result.message = std::move(message);
    return result;
}

// aux 是每块首条的 source, 表按 source 非降
uint32_t geo_link_chunk_for(const GeoChunkTable& table, uint32_t source)
{
    uint32_t index = 0;
    while (index + 1 < geo_chunk_count(table) && geo_chunk_aux(table, index + 1) < source) {
        ++index;
    }
    return index;
}

size_t lower_bound_link_source(const std::vector<BaseNavLink>& links, uint32_t source)
{
    size_t left = 0;
    size_t right = links.size();
    while (left < right) {
        const size_t middle = left + (right - left) / 2;
        if (links[middle].source < source) {
            left = middle + 1;
        }
        else {
            right = middle;
        }
    }
    return left;
}

bool has_gzip_suffix(const std::filesystem::path& path)
{
    return path.extension() == ".gz";
}

std::optional<size_t> read_gzip_uncompressed_size(const std::filesystem::path& path)
{
    std::ifstream input(path, std::ios::binary | std::ios::ate);
    if (!input) {
        return std::nullopt;
    }
    if (input.tellg() < static_cast<std::streampos>(4)) {
        return std::nullopt;
    }
    input.seekg(-4, std::ios::end);
    std::array<uint8_t, 4> size_bytes {};
    if (!read_exact(&input, size_bytes.data(), size_bytes.size())) {
        return std::nullopt;
    }
    return static_cast<size_t>(peek_u32(size_bytes.data()));
}

BaseNavLoadResult read_gzip_file(const std::filesystem::path& path, std::vector<uint8_t>* output)
{
#ifdef _WIN32
    gzFile file = gzopen_w(path.c_str(), "rb");
#else
    gzFile file = gzopen(path.string().c_str(), "rb");
#endif
    if (file == nullptr) {
        return fail(BaseNavLoadStatus::FileOpenFailed, "failed to open gzip nav file");
    }
    gzbuffer(file, static_cast<unsigned int>(kGzipReadChunkSize));
    if (const auto uncompressed_size = read_gzip_uncompressed_size(path); uncompressed_size && *uncompressed_size > 0) {
        output->reserve(*uncompressed_size);
    }

    std::vector<uint8_t> buffer(kGzipReadChunkSize);
    while (true) {
        const int bytes_read = gzread(file, buffer.data(), static_cast<unsigned int>(buffer.size()));
        if (bytes_read < 0) {
            int error_code = Z_OK;
            const char* message = gzerror(file, &error_code);
            gzclose(file);
            return fail(BaseNavLoadStatus::FileReadFailed, message != nullptr ? message : "failed to decompress gzip nav file");
        }
        if (bytes_read == 0) {
            break;
        }
        output->insert(output->end(), buffer.begin(), buffer.begin() + bytes_read);
    }

    if (gzclose(file) != Z_OK) {
        return fail(BaseNavLoadStatus::FileReadFailed, "failed to close gzip nav file");
    }
    return {};
}

}

BaseNavLoadResult ReadNavFileBytes(const std::filesystem::path& path, std::vector<uint8_t>* output)
{
    if (has_gzip_suffix(path)) {
        return read_gzip_file(path, output);
    }

    std::error_code ec;
    const uint64_t file_size = std::filesystem::file_size(path, ec);
    if (ec) {
        return fail(BaseNavLoadStatus::FileOpenFailed, "failed to stat nav file");
    }

    std::ifstream input(path, std::ios::binary);
    if (!input) {
        return fail(BaseNavLoadStatus::FileOpenFailed, "failed to open nav file");
    }
    output->resize(static_cast<size_t>(file_size));
    if (!output->empty() && !read_exact(&input, output->data(), output->size())) {
        return fail(BaseNavLoadStatus::FileReadFailed, "failed to read nav file");
    }
    return {};
}

namespace
{

std::shared_ptr<const std::vector<uint8_t>> shared_section_bytes(uint64_t file_fnv, const uint8_t* at, size_t size)
{
    static std::mutex mutex;
    static std::unordered_map<uint64_t, std::weak_ptr<const std::vector<uint8_t>>> cache;
    const std::lock_guard lock(mutex);
    if (const auto it = cache.find(file_fnv); it != cache.end()) {
        std::shared_ptr<const std::vector<uint8_t>> hit = it->second.lock();
        if (hit != nullptr && hit->size() == size && (size == 0 || std::memcmp(hit->data(), at, size) == 0)) {
            return hit;
        }
    }
    auto fresh = std::make_shared<const std::vector<uint8_t>>(at, at + size);
    cache[file_fnv] = fresh;
    return fresh;
}

struct TriangleSelection
{
    uint32_t first_triangle = 0;
    uint32_t end_triangle = 0;
    uint32_t vertex_count = 0;
    bool zone_scoped = false;
};

struct TriangleRecordBatch
{
    const uint8_t* records = nullptr;
    uint32_t first = 0;
    size_t count = 0;
};

const char* convert_triangles(
    std::vector<BaseNavTriangle>* triangles,
    uint32_t* first_vertex,
    uint32_t* last_vertex,
    const TriangleSelection& selection,
    const TriangleRecordBatch& batch)
{
    const uint32_t begin = std::max(batch.first, selection.first_triangle);
    const uint32_t end =
        static_cast<uint32_t>(std::min<uint64_t>(static_cast<uint64_t>(batch.first) + batch.count, selection.end_triangle));
    if (begin >= end) {
        return nullptr;
    }
    const uint8_t* cursor = batch.records + static_cast<size_t>(begin - batch.first) * kTriangleSize;
    for (uint32_t index = begin; index < end; ++index) {
        BaseNavTriangle triangle = read_triangle_record(&cursor);
        for (uint32_t value : triangle.vertices) {
            if (value >= selection.vertex_count) {
                return "triangle vertex index is outside vertex table";
            }
            if (selection.zone_scoped) {
                *first_vertex = std::min(*first_vertex, value);
                *last_vertex = std::max(*last_vertex, value);
            }
        }
        if (selection.zone_scoped) {
            for (int32_t& neighbor : triangle.neighbors) {
                if (neighbor < 0 || static_cast<uint32_t>(neighbor) < selection.first_triangle
                    || static_cast<uint32_t>(neighbor) >= selection.end_triangle) {
                    neighbor = -1;
                    continue;
                }
                neighbor -= static_cast<int32_t>(selection.first_triangle);
            }
        }
        triangles->push_back(triangle);
    }
    return nullptr;
}

const char* decode_vertices(std::vector<BaseNavVertex>* vertices, const GeometrySection& geometry, uint32_t from, uint32_t to)
{
    const auto window = chunk_window_for(geometry.vertices, from, to);
    if (!window) {
        return "nav vertex chunk is malformed";
    }
    vertices->resize(window->records);
    if (window->records == 0) {
        return nullptr;
    }
    if (!decode_chunk_span(
            geometry.vertices,
            geometry.base,
            window->low,
            window->high,
            kVertexSize,
            reinterpret_cast<uint8_t*>(vertices->data()),
            GeoChunkKind::Vertex)) {
        return "nav vertex chunk is malformed";
    }
    vertices->erase(vertices->begin() + static_cast<std::ptrdiff_t>(to - window->first), vertices->end());
    vertices->erase(vertices->begin(), vertices->begin() + static_cast<std::ptrdiff_t>(from - window->first));
    return nullptr;
}

bool invalid_link(const std::vector<BaseNavLink>& link_table, uint32_t triangle_count, uint32_t link_base, size_t index)
{
    const BaseNavLink& link = link_table[index];
    if (link.source >= triangle_count || link.target >= triangle_count) {
        LogWarn << "Skipping invalid BaseNav link." << VAR(link_base + index) << VAR(link.source) << VAR(link.target)
                << VAR(triangle_count);
        return true;
    }
    return false;
}

void rebase_triangle(uint32_t* triangle, uint32_t first_triangle, uint32_t end_triangle)
{
    if (*triangle < first_triangle || *triangle >= end_triangle) {
        *triangle = kBaseNavOffMeshTriangleNone;
        return;
    }
    *triangle -= first_triangle;
}

}

BaseNavLoadResult LoadBaseNavPack(const std::filesystem::path& path, std::string_view zone_name)
{
    std::vector<uint8_t> file_bytes;
    const BaseNavLoadResult read_result = ReadNavFileBytes(path, &file_bytes);
    if (!read_result.message.empty() || read_result.status != BaseNavLoadStatus::Success) {
        return read_result;
    }

    const uint64_t file_size = file_bytes.size();
    if (file_size < kHeaderSize) {
        return fail(BaseNavLoadStatus::InvalidSize, "nav file is smaller than header");
    }
    const uint64_t file_fnv = fnv64_update(kFnvOffset, file_bytes.data(), file_bytes.size());

    if (std::memcmp(file_bytes.data(), "BNAV", 4) != 0) {
        return fail(BaseNavLoadStatus::InvalidMagic, "invalid nav magic");
    }
    const uint8_t* header_cursor = file_bytes.data() + 4;
    const uint16_t version = read_u16(&header_cursor);
    (void)read_u16(&header_cursor); // flags
    if (version == 0 || version > kBaseNavRevision) {
        return fail(BaseNavLoadStatus::UnsupportedVersion, "unsupported nav version");
    }
    const size_t header_size = version >= kSectionDirSince ? kHeaderSizeV4 : kHeaderSize;
    if (file_size < header_size) {
        return fail(BaseNavLoadStatus::InvalidSize, "nav file is smaller than header");
    }
    const size_t zone_prefix_size = kZonePrefixSize + (version >= kZoneFloorYSince ? 4 : 0);
    const uint32_t zone_count = read_u32(&header_cursor);
    const uint32_t vertex_count = read_u32(&header_cursor);
    const uint32_t triangle_count = read_u32(&header_cursor);
    const uint32_t link_count = read_u32(&header_cursor);
    const uint64_t zone_table_offset = read_u64(&header_cursor);
    const uint64_t vertex_offset = read_u64(&header_cursor);
    const uint64_t triangle_offset = read_u64(&header_cursor);
    const uint64_t link_offset = read_u64(&header_cursor);
    const uint64_t build_hash = read_u64(&header_cursor);

    if (link_count == 0) {
        return fail(BaseNavLoadStatus::InvalidSize, "nav link table is empty");
    }
    uint64_t section_dir_offset = 0;
    uint32_t section_count = 0;
    if (version >= kSectionDirSince) {
        section_dir_offset = read_u64(&header_cursor);
        section_count = read_u32(&header_cursor);
        (void)read_u32(&header_cursor); // 保留字段
        if (section_count != 0
            && !offset_range_valid(section_dir_offset, static_cast<uint64_t>(section_count) * kSectionEntrySize, file_size)) {
            return fail(BaseNavLoadStatus::InvalidOffset, "nav section directory is outside file bounds");
        }
    }

    // v5 起几何与区表都在 BGEO 段里, 头里那四个偏移作废, 所以先读段目录
    std::vector<BaseNavSection> sections;
    std::vector<std::pair<const uint8_t*, size_t>> section_raw;
    sections.reserve(section_count);
    section_raw.reserve(section_count);
    const uint8_t* dir_cursor = file_bytes.data() + section_dir_offset;
    for (uint32_t index = 0; index < section_count; ++index) {
        BaseNavSection sec;
        std::memcpy(sec.tag.data(), dir_cursor, 4);
        dir_cursor += 4;
        sec.flags = read_u32(&dir_cursor);
        const uint64_t offset = read_u64(&dir_cursor);
        const uint64_t size = read_u64(&dir_cursor);
        if (!offset_range_valid(offset, size, file_size)) {
            return fail(BaseNavLoadStatus::InvalidOffset, "nav section is outside file bounds");
        }
        const uint8_t* at = file_bytes.data() + offset;
        section_raw.emplace_back(at, static_cast<size_t>(size));
        if (std::memcmp(sec.tag.data(), kGridSectionTag, 4) == 0) {
            sec.bytes = shared_section_bytes(file_fnv, at, static_cast<size_t>(size));
        }
        sections.push_back(std::move(sec));
    }

    const bool sectioned = version >= kGeometrySectionSince;
    GeometrySection geometry;
    uint64_t zone_table_size = 0;
    const uint8_t* zone_table = nullptr;
    const uint8_t* vertex_bytes = nullptr;
    const uint8_t* triangle_bytes = nullptr;
    const uint8_t* link_bytes = nullptr;
    if (sectioned) {
        const std::pair<const uint8_t*, size_t>* found = nullptr;
        for (size_t i = 0; i < sections.size(); ++i) {
            if (std::memcmp(sections[i].tag.data(), kGeometrySectionTag, 4) == 0) {
                found = &section_raw[i];
            }
        }
        if (found == nullptr || !parse_geometry_section(found->first, found->second, &geometry)) {
            return fail(BaseNavLoadStatus::InvalidOffset, "nav geometry section is missing or malformed");
        }
        if (geometry.vertices.total != vertex_count || geometry.triangles.total != triangle_count || geometry.links.total != link_count) {
            return fail(BaseNavLoadStatus::InvalidSize, "nav geometry section disagrees with the header counts");
        }
        zone_table = geometry.zone_table;
        zone_table_size = geometry.zone_table_size;
    }
    else {
        if (zone_table_offset < header_size || vertex_offset < zone_table_offset || triangle_offset < vertex_offset
            || link_offset < triangle_offset) {
            return fail(BaseNavLoadStatus::InvalidOffset, "invalid nav offsets");
        }
        if (!offset_range_valid(vertex_offset, static_cast<uint64_t>(vertex_count) * kVertexSize, file_size)
            || !offset_range_valid(triangle_offset, static_cast<uint64_t>(triangle_count) * kTriangleSize, file_size)
            || !offset_range_valid(link_offset, static_cast<uint64_t>(link_count) * kLinkSize, file_size)) {
            return fail(BaseNavLoadStatus::InvalidOffset, "nav payload is outside file bounds");
        }
        zone_table_size = vertex_offset - zone_table_offset;
        zone_table = file_bytes.data() + zone_table_offset;
        vertex_bytes = file_bytes.data() + vertex_offset;
        triangle_bytes = file_bytes.data() + triangle_offset;
        link_bytes = file_bytes.data() + link_offset;
    }
    const uint8_t* zone_end = zone_table + zone_table_size;

    std::vector<BaseNavZone> zones;
    zones.reserve(zone_count);
    std::set<uint16_t> zone_ids;
    const uint8_t* zone_cursor = zone_table;
    for (uint32_t index = 0; index < zone_count; ++index) {
        if (static_cast<size_t>(zone_end - zone_cursor) < zone_prefix_size) {
            return fail(BaseNavLoadStatus::InvalidSize, "zone table is truncated");
        }
        BaseNavZone zone;
        zone.zone_id = read_u16(&zone_cursor);
        zone.flags = read_u16(&zone_cursor);
        const uint32_t name_size = read_u32(&zone_cursor);
        zone.first_triangle = read_u32(&zone_cursor);
        zone.triangle_count = read_u32(&zone_cursor);
        zone.component_count = read_u32(&zone_cursor);
        zone.width = read_f32(&zone_cursor);
        zone.height = read_f32(&zone_cursor);
        for (float& value : zone.transform) {
            value = read_f32(&zone_cursor);
        }
        if (version >= kZoneFloorYSince) {
            zone.floor_y = read_f32(&zone_cursor);
        }
        if (static_cast<size_t>(zone_end - zone_cursor) < name_size) {
            return fail(BaseNavLoadStatus::InvalidSize, "zone name is truncated");
        }
        zone.name.assign(reinterpret_cast<const char*>(zone_cursor), name_size);
        zone_cursor += name_size;
        if (!zone_ids.insert(zone.zone_id).second) {
            return fail(BaseNavLoadStatus::DuplicateZone, "duplicate zone id");
        }
        zones.push_back(std::move(zone));
    }
    std::optional<BaseNavZone> selected_zone;
    for (const BaseNavZone& zone : zones) {
        if (static_cast<uint64_t>(zone.first_triangle) + zone.triangle_count > triangle_count) {
            return fail(BaseNavLoadStatus::InvalidSize, "zone triangle range is outside triangle table");
        }
        if (!zone_name.empty() && zone.name == zone_name) {
            selected_zone = zone;
        }
    }
    if (!zone_name.empty() && !selected_zone) {
        return fail(BaseNavLoadStatus::ZoneNotFound, "nav zone not found");
    }
    const uint32_t selected_first_triangle = selected_zone ? selected_zone->first_triangle : 0;
    const uint32_t selected_triangle_count = selected_zone ? selected_zone->triangle_count : triangle_count;
    const uint32_t selected_triangle_end = selected_first_triangle + selected_triangle_count;
    const bool zone_scoped = selected_zone.has_value();

    // 顶点与连接的记录与结构体逐字节同布局, 三角记录 36 字节与结构体不同
    static_assert(std::endian::native == std::endian::little);
    static_assert(sizeof(BaseNavVertex) == kVertexSize && sizeof(BaseNavLink) == kLinkSize);

    std::vector<BaseNavVertex> vertices;
    std::vector<BaseNavTriangle> triangles;
    triangles.reserve(selected_triangle_count);
    std::vector<BaseNavLink> link_table;
    uint32_t link_base = 0;
    uint32_t first_vertex = zone_scoped ? vertex_count : 0;
    uint32_t last_vertex = 0;

    // 哈希按区表、顶点、三角、连接的顺序算在规范字节上, 三角含重心
    const bool should_verify_hash = zone_name.empty() || (!sectioned && !has_gzip_suffix(path));
    uint64_t hash = kFnvOffset;
    if (should_verify_hash) {
        hash = fnv64_update(hash, zone_table, static_cast<size_t>(zone_table_size));
    }

    const TriangleSelection selection {
        .first_triangle = selected_first_triangle,
        .end_triangle = selected_triangle_end,
        .vertex_count = vertex_count,
        .zone_scoped = zone_scoped,
    };

    if (sectioned) {
        // 整包时重心要靠顶点补所以顶点先解, 按区时顶点范围要扫完三角才知道所以后解
        if (!zone_scoped) {
            if (const char* error = decode_vertices(&vertices, geometry, 0, vertex_count)) {
                return fail(BaseNavLoadStatus::InvalidSize, error);
            }
            if (should_verify_hash) {
                hash = fnv64_update(hash, reinterpret_cast<const uint8_t*>(vertices.data()), vertices.size() * kVertexSize);
            }
        }

        const auto window = chunk_window_for(geometry.triangles, selected_first_triangle, selected_triangle_end);
        if (!window) {
            return fail(BaseNavLoadStatus::InvalidSize, "nav triangle chunk is malformed");
        }
        std::vector<uint8_t> scratch;
        const size_t workers = NavWorkerCount(static_cast<int64_t>(window->records));
        for (uint32_t low = window->low; window->records != 0 && low <= window->high;) {
            uint32_t high = low;
            size_t records = geo_chunk_span(geometry.triangles, low);
            while (high < window->high
                   && (records + geo_chunk_span(geometry.triangles, high + 1) <= kTriangleBatchRecords || high + 1 - low < workers)) {
                ++high;
                records += geo_chunk_span(geometry.triangles, high);
            }
            scratch.resize(records * kTriangleSize);
            if (!decode_chunk_span(geometry.triangles, geometry.base, low, high, kTriangleSize, scratch.data(), GeoChunkKind::Triangle)) {
                return fail(BaseNavLoadStatus::InvalidSize, "nav triangle chunk is malformed");
            }
            if (should_verify_hash) {
                // 重心须按 ((a+b)+c)/3.0f 补回, 乘 1/3 的结果不逐位相同
                for (size_t at = 0; at < scratch.size(); at += kTriangleSize) {
                    uint8_t* const record = scratch.data() + at;
                    float u[3] = {};
                    float v[3] = {};
                    for (int i = 0; i < 3; ++i) {
                        const uint32_t value = peek_u32(record + i * 4);
                        if (value >= vertex_count) {
                            return fail(BaseNavLoadStatus::InvalidSize, "triangle vertex index is outside vertex table");
                        }
                        u[i] = vertices[value].u;
                        v[i] = vertices[value].v;
                    }
                    const float center_u = ((u[0] + u[1]) + u[2]) / 3.0F;
                    const float center_v = ((v[0] + v[1]) + v[2]) / 3.0F;
                    std::memcpy(record + 28, &center_u, sizeof(float));
                    std::memcpy(record + 32, &center_v, sizeof(float));
                }
                hash = fnv64_update(hash, scratch.data(), scratch.size());
            }
            if (const char* error = convert_triangles(
                    &triangles,
                    &first_vertex,
                    &last_vertex,
                    selection,
                    TriangleRecordBatch { .records = scratch.data(),
                                          .first = geo_chunk_first(geometry.triangles, low),
                                          .count = records })) {
                return fail(BaseNavLoadStatus::InvalidSize, error);
            }
            low = high + 1;
        }
        std::vector<uint8_t>().swap(scratch);

        if (zone_scoped) {
            const uint32_t want_low = triangles.empty() ? 0 : first_vertex;
            const uint32_t want_high = triangles.empty() ? 0 : last_vertex + 1;
            if (const char* error = decode_vertices(&vertices, geometry, want_low, want_high)) {
                return fail(BaseNavLoadStatus::InvalidSize, error);
            }
        }

        const uint32_t link_chunks = geo_chunk_count(geometry.links);
        const uint32_t low_chunk = zone_scoped ? geo_link_chunk_for(geometry.links, selected_first_triangle) : 0;
        const uint32_t high_chunk = zone_scoped ? geo_link_chunk_for(geometry.links, selected_triangle_end) : link_chunks;
        if (low_chunk < link_chunks && low_chunk <= high_chunk) {
            const uint32_t last_chunk = std::min(high_chunk, link_chunks - 1);
            link_table.resize(chunk_span_records(geometry.links, low_chunk, last_chunk));
            if (!decode_chunk_span(
                    geometry.links,
                    geometry.base,
                    low_chunk,
                    last_chunk,
                    kLinkSize,
                    reinterpret_cast<uint8_t*>(link_table.data()),
                    GeoChunkKind::Link)) {
                return fail(BaseNavLoadStatus::InvalidSize, "nav link chunk is malformed");
            }
        }
        link_base = geo_chunk_first(geometry.links, low_chunk);
        if (should_verify_hash) {
            hash = fnv64_update(hash, reinterpret_cast<const uint8_t*>(link_table.data()), link_table.size() * kLinkSize);
        }
    }
    else {
        if (should_verify_hash) {
            hash = fnv64_update(hash, vertex_bytes, static_cast<size_t>(vertex_count) * kVertexSize);
            hash = fnv64_update(hash, triangle_bytes, static_cast<size_t>(triangle_count) * kTriangleSize);
            hash = fnv64_update(hash, link_bytes, static_cast<size_t>(link_count) * kLinkSize);
        }
        if (const char* error = convert_triangles(
                &triangles,
                &first_vertex,
                &last_vertex,
                selection,
                TriangleRecordBatch { .records = triangle_bytes, .first = 0, .count = triangle_count })) {
            return fail(BaseNavLoadStatus::InvalidSize, error);
        }
        const uint32_t selected_vertex_count = zone_scoped ? (triangles.empty() ? 0 : last_vertex - first_vertex + 1) : vertex_count;
        vertices.resize(selected_vertex_count);
        if (selected_vertex_count != 0) {
            std::memcpy(
                vertices.data(),
                vertex_bytes + static_cast<size_t>(first_vertex) * kVertexSize,
                selected_vertex_count * kVertexSize);
        }
        link_table.resize(link_count);
        std::memcpy(link_table.data(), link_bytes, static_cast<size_t>(link_count) * kLinkSize);
    }
    if (should_verify_hash && hash != build_hash) {
        return fail(BaseNavLoadStatus::HashMismatch, "nav build hash mismatch");
    }

    if (zone_scoped) {
        for (BaseNavTriangle& triangle : triangles) {
            for (uint32_t& vertex_index : triangle.vertices) {
                vertex_index -= first_vertex;
            }
        }
        selected_zone->first_triangle = 0;
        selected_zone->triangle_count = static_cast<uint32_t>(triangles.size());
        // 除选中区外还留下它的 tier 子区, 让 tier zone_id 仍能投影到底图
        std::vector<BaseNavZone> scoped_zones;
        scoped_zones.reserve(zones.size());
        scoped_zones.push_back(*selected_zone);
        for (const BaseNavZone& candidate : zones) {
            if (IsTierZone(candidate) && candidate.component_count == selected_zone->zone_id) {
                BaseNavZone tier = candidate;
                tier.first_triangle = 0;
                tier.triangle_count = 0;
                scoped_zones.push_back(std::move(tier));
            }
        }
        zones = std::move(scoped_zones);
    }

    std::vector<BaseNavLink> links;
    if (zone_scoped) {
        const size_t first_link = lower_bound_link_source(link_table, selected_first_triangle);
        const size_t last_link = lower_bound_link_source(link_table, selected_triangle_end);
        links.reserve(last_link - first_link);
        for (size_t index = first_link; index < last_link; ++index) {
            if (invalid_link(link_table, triangle_count, link_base, index)) {
                continue;
            }
            BaseNavLink link = link_table[index];
            if (link.source < selected_first_triangle || link.source >= selected_triangle_end || link.target < selected_first_triangle
                || link.target >= selected_triangle_end) {
                continue;
            }
            link.source -= selected_first_triangle;
            link.target -= selected_first_triangle;
            links.push_back(link);
        }
    }
    else {
        size_t kept = 0;
        for (size_t index = 0; index < link_table.size(); ++index) {
            if (!invalid_link(link_table, triangle_count, link_base, index)) {
                link_table[kept++] = link_table[index];
            }
        }
        link_table.resize(kept);
        links = std::move(link_table);
    }

    std::vector<BaseNavOffMeshLink> off_mesh_links;
    for (size_t i = 0; i < sections.size(); ++i) {
        if (std::memcmp(sections[i].tag.data(), kOffMeshSectionTag, 4) == 0
            && !parse_off_mesh_section(section_raw[i].first, section_raw[i].second, &off_mesh_links)) {
            return fail(BaseNavLoadStatus::InvalidSize, "nav off-mesh section is malformed");
        }
    }
    // 两沿三角是整包下标, 按区加载时同样平移, 区外锚点退回哨兵值
    if (zone_scoped) {
        for (BaseNavOffMeshLink& link : off_mesh_links) {
            rebase_triangle(&link.start_triangle, selected_first_triangle, selected_triangle_end);
            rebase_triangle(&link.end_triangle, selected_first_triangle, selected_triangle_end);
        }
    }

    std::vector<BaseNavSurface> surfaces;
    for (size_t i = 0; i < sections.size(); ++i) {
        if (std::memcmp(sections[i].tag.data(), kSurfaceSectionTag, 4) == 0) {
            if (!parse_surface_section(section_raw[i].first, section_raw[i].second, &surfaces) || surfaces.size() != triangle_count) {
                return fail(BaseNavLoadStatus::InvalidSize, "nav surface section is malformed");
            }
        }
    }
    if (zone_scoped && !surfaces.empty()) {
        std::vector<BaseNavSurface> scoped(surfaces.begin() + selected_first_triangle, surfaces.begin() + selected_triangle_end);
        surfaces = std::move(scoped);
    }
    if (zone_scoped) {
        std::erase_if(off_mesh_links, [&](const BaseNavOffMeshLink& link) { return link.zone_id != selected_zone->zone_id; });
    }
    for (const BaseNavOffMeshLink& link : off_mesh_links) {
        const bool known_zone =
            std::any_of(zones.begin(), zones.end(), [&](const BaseNavZone& zone) { return zone.zone_id == link.zone_id; });
        if (!known_zone) {
            return fail(BaseNavLoadStatus::InvalidSize, "nav off-mesh link refers to an unknown zone");
        }
    }

    BaseNavLoadResult result;
    result.pack = BaseNavPack {
        .path = path,
        .build_hash = build_hash,
        .file_fnv = file_fnv,
        .zones = std::move(zones),
        .vertices = std::move(vertices),
        .triangles = std::move(triangles),
        .links = std::move(links),
        .surfaces = std::move(surfaces),
        .off_mesh_links = std::move(off_mesh_links),
        .sections = std::move(sections),
    };
    return result;
}

} // namespace navmesh
