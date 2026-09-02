#include <algorithm>
#include <array>
#include <cstddef>
#include <cstdint>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <functional>
#include <optional>
#include <set>
#include <string>
#include <string_view>
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
// 每个字段从哪一版开始存在,单独记。用 >= kBaseNavRevision 判会在下一次升版时
// 把老包的该字段静默跳过。
constexpr uint16_t kZoneFloorYSince = 3;
constexpr uint16_t kSectionDirSince = 4;
constexpr uint16_t kGeometrySectionSince = 5;
constexpr size_t kGzipReadChunkSize = 4 << 20;
// BGEO 段:四块几何按定长块存,块内自足解码,按区加载只解用得着的块。
constexpr char kGeometrySectionTag[5] = "BGEO";
constexpr char kOffMeshSectionTag[5] = "BOML";
constexpr char kSurfaceSectionTag[5] = "BSRF";
constexpr char kGridSectionTag[5] = "BGRD";
constexpr uint32_t kGeometrySectionVersion = 1;
constexpr uint16_t kOffMeshSectionVersion = 1;
constexpr uint16_t kOffMeshRecordSize = 80;
constexpr size_t kOffMeshHeaderSize = 16;
constexpr uint16_t kSurfaceSectionVersion = 1;
constexpr uint16_t kSurfaceRecordSize = 8;
constexpr size_t kSurfaceHeaderSize = 16;
constexpr size_t kGeoHeaderSize = 72;
constexpr size_t kGeoChunkEntrySize = 16;
constexpr size_t kInflateChunkSize = 1U << 16U;
constexpr uint64_t kFnvOffset = 14'695'981'039'346'656'037ULL;
constexpr uint64_t kFnvPrime = 1'099'511'628'211ULL;

bool ReadExact(std::istream& input, uint8_t* out, size_t size)
{
    input.read(reinterpret_cast<char*>(out), static_cast<std::streamsize>(size));
    return input.good() || input.gcount() == static_cast<std::streamsize>(size);
}

uint16_t ReadU16(const uint8_t*& cursor)
{
    const uint16_t value = static_cast<uint16_t>(cursor[0]) | (static_cast<uint16_t>(cursor[1]) << 8);
    cursor += 2;
    return value;
}

uint32_t ReadU32(const uint8_t*& cursor)
{
    const uint32_t value = static_cast<uint32_t>(cursor[0]) | (static_cast<uint32_t>(cursor[1]) << 8)
                           | (static_cast<uint32_t>(cursor[2]) << 16) | (static_cast<uint32_t>(cursor[3]) << 24);
    cursor += 4;
    return value;
}

uint32_t PeekU32(const uint8_t* bytes)
{
    return static_cast<uint32_t>(bytes[0]) | (static_cast<uint32_t>(bytes[1]) << 8) | (static_cast<uint32_t>(bytes[2]) << 16)
           | (static_cast<uint32_t>(bytes[3]) << 24);
}

uint64_t ReadU64(const uint8_t*& cursor)
{
    uint64_t value = 0;
    for (int index = 0; index < 8; ++index) {
        value |= static_cast<uint64_t>(cursor[index]) << (index * 8);
    }
    cursor += 8;
    return value;
}

int32_t ReadI32(const uint8_t*& cursor)
{
    return static_cast<int32_t>(ReadU32(cursor));
}

float ReadF32(const uint8_t*& cursor)
{
    const uint32_t bits = ReadU32(cursor);
    float value = 0.0F;
    std::memcpy(&value, &bits, sizeof(value));
    return value;
}

BaseNavTriangle ReadTriangleRecord(const uint8_t*& cursor)
{
    BaseNavTriangle triangle;
    for (uint32_t& value : triangle.vertices) {
        value = ReadU32(cursor);
    }
    for (int32_t& value : triangle.neighbors) {
        value = ReadI32(cursor);
    }
    // 分量号与尾部两个 float 重心都全仓无读者, 跳过不落盘 —— 分量号由 planner 从
    // neighbors 自己重算。哈希仍按整条记录算, 所以按区加载时补重心的那段不能省。
    cursor += 12;
    return triangle;
}

BaseNavLink ReadLinkRecord(const uint8_t*& cursor)
{
    BaseNavLink link;
    link.source = ReadU32(cursor);
    link.target = ReadU32(cursor);
    return link;
}

bool ParseOffMeshSection(const uint8_t* data, size_t size, std::vector<BaseNavOffMeshLink>* links)
{
    links->clear();
    if (size < kOffMeshHeaderSize || std::memcmp(data, kOffMeshSectionTag, 4) != 0) {
        return false;
    }
    const uint8_t* cursor = data + 4;
    const uint16_t version = ReadU16(cursor);
    const uint16_t record_size = ReadU16(cursor);
    const uint32_t count = ReadU32(cursor);
    (void)ReadU32(cursor);
    if (version != kOffMeshSectionVersion || record_size != kOffMeshRecordSize
        || count != (size - kOffMeshHeaderSize) / kOffMeshRecordSize
        || size != kOffMeshHeaderSize + static_cast<size_t>(count) * kOffMeshRecordSize) {
        return false;
    }
    links->reserve(count);
    for (uint32_t index = 0; index < count; ++index) {
        BaseNavOffMeshLink link;
        link.zone_id = ReadU16(cursor);
        link.kind = *cursor++;
        ++cursor;
        link.is_ext = ReadI32(cursor);
        link.bidirectional = ReadI32(cursor);
        link.area = ReadI32(cursor);
        link.link_type = ReadU16(cursor);
        link.direction = *cursor++;
        ++cursor;
        link.radius = ReadF32(cursor);
        link.cost_modifier = ReadF32(cursor);
        for (BaseNavVertex& point : link.points) {
            point.u = ReadF32(cursor);
            point.v = ReadF32(cursor);
            point.height = ReadF32(cursor);
        }
        (void)ReadU32(cursor);
        links->push_back(link);
    }
    return cursor == data + size;
}

bool ParseSurfaceSection(const uint8_t* data, size_t size, std::vector<BaseNavSurface>* surfaces)
{
    surfaces->clear();
    if (size < kSurfaceHeaderSize || std::memcmp(data, kSurfaceSectionTag, 4) != 0) {
        return false;
    }
    const uint8_t* cursor = data + 4;
    const uint16_t version = ReadU16(cursor);
    const uint16_t record_size = ReadU16(cursor);
    const uint32_t count = ReadU32(cursor);
    (void)ReadU32(cursor);
    if (version != kSurfaceSectionVersion || record_size != kSurfaceRecordSize
        || count != (size - kSurfaceHeaderSize) / kSurfaceRecordSize
        || size != kSurfaceHeaderSize + static_cast<size_t>(count) * kSurfaceRecordSize) {
        return false;
    }
    surfaces->reserve(count);
    for (uint32_t index = 0; index < count; ++index) {
        BaseNavSurface surface;
        surface.area = *cursor++;
        surface.poly_type = *cursor++;
        (void)ReadU16(cursor);
        surface.flags = ReadU32(cursor);
        surfaces->push_back(surface);
    }
    return cursor == data + size;
}

uint64_t Fnv64Update(uint64_t hash, const uint8_t* bytes, size_t size)
{
    for (size_t index = 0; index < size; ++index) {
        hash ^= bytes[index];
        hash *= kFnvPrime;
    }
    return hash;
}

bool OffsetRangeValid(uint64_t offset, uint64_t size, uint64_t file_size)
{
    return offset <= file_size && size <= file_size - offset;
}

bool Inflate(const uint8_t* data, size_t size, std::vector<uint8_t>* output)
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

// 块内是若干条「varint 长度 + deflate 流」,条数由表类型定死。
bool SplitChunkStreams(const uint8_t* data, size_t size, size_t count, std::vector<std::vector<uint8_t>>* out)
{
    out->assign(count, {});
    const uint8_t* cursor = data;
    const uint8_t* const end = data + size;
    for (std::vector<uint8_t>& stream : *out) {
        uint64_t packed = 0;
        if (!GetVarint(cursor, end, packed) || static_cast<uint64_t>(end - cursor) < packed
            || !Inflate(cursor, static_cast<size_t>(packed), &stream)) {
            return false;
        }
        cursor += packed;
    }
    return cursor == end;
}

// 一张块目录。块定长,所以第 i 块覆盖 [i * chunk, min((i + 1) * chunk, total))。
struct GeoChunkTable
{
    const uint8_t* directory = nullptr;
    uint32_t chunk = 0;
    uint32_t total = 0;

    uint32_t count() const { return chunk == 0 ? 0 : (total + chunk - 1) / chunk; }

    uint32_t first(uint32_t index) const { return index * chunk; }

    uint32_t span(uint32_t index) const { return std::min(chunk, total - first(index)); }

    uint32_t aux(uint32_t index) const { return PeekU32(directory + index * kGeoChunkEntrySize + 12); }

    void entry(uint32_t index, uint64_t& offset, uint32_t& size) const
    {
        const uint8_t* cursor = directory + static_cast<size_t>(index) * kGeoChunkEntrySize;
        offset = ReadU64(cursor);
        size = ReadU32(cursor);
    }

    const uint8_t* bytes(const uint8_t* base, uint32_t index, uint32_t& size) const
    {
        uint64_t offset = 0;
        entry(index, offset, size);
        return base + offset;
    }
};

struct GeometrySection
{
    const uint8_t* base = nullptr;
    const uint8_t* zone_table = nullptr;
    uint64_t zone_table_size = 0;
    GeoChunkTable vertices;
    GeoChunkTable triangles;
    GeoChunkTable links;
};

bool ParseGeometrySection(const uint8_t* data, uint64_t size, GeometrySection* out)
{
    if (data == nullptr || size < kGeoHeaderSize || std::memcmp(data, kGeometrySectionTag, 4) != 0) {
        return false;
    }
    const uint8_t* cursor = data + 4;
    if (ReadU32(cursor) != kGeometrySectionVersion) {
        return false;
    }
    out->base = data;
    out->vertices.total = ReadU32(cursor);
    out->triangles.total = ReadU32(cursor);
    out->links.total = ReadU32(cursor);
    out->vertices.chunk = ReadU32(cursor);
    out->triangles.chunk = ReadU32(cursor);
    out->links.chunk = ReadU32(cursor);
    out->zone_table_size = ReadU32(cursor);
    (void)ReadU32(cursor); // reserved
    GeoChunkTable* const tables[3] = { &out->vertices, &out->triangles, &out->links };
    for (GeoChunkTable* table : tables) {
        const uint64_t at = ReadU64(cursor);
        if (table->chunk == 0 || !OffsetRangeValid(at, static_cast<uint64_t>(table->count()) * kGeoChunkEntrySize, size)) {
            return false;
        }
        table->directory = data + at;
    }
    const uint64_t zone_at = ReadU64(cursor);
    if (!OffsetRangeValid(zone_at, out->zone_table_size, size)) {
        return false;
    }
    out->zone_table = data + zone_at;
    // 校验只看偏移本身:先加到指针上再判越界,那个指针已经算不出来了。
    for (const GeoChunkTable* table : tables) {
        for (uint32_t i = 0; i < table->count(); ++i) {
            uint64_t at = 0;
            uint32_t chunk_size = 0;
            table->entry(i, at, chunk_size);
            if (!OffsetRangeValid(at, chunk_size, size)) {
                return false;
            }
        }
    }
    return true;
}

// 顶点是 12 字节记录做的字节平面:n × 12 的字节矩阵转置成 12 × n。
bool DecodeVertexChunk(const uint8_t* data, size_t size, uint32_t count, uint8_t* out)
{
    std::vector<std::vector<uint8_t>> stream;
    if (!SplitChunkStreams(data, size, 1, &stream) || stream[0].size() != static_cast<size_t>(count) * kVertexSize) {
        return false;
    }
    for (size_t i = 0; i < count; ++i) {
        for (size_t j = 0; j < kVertexSize; ++j) {
            out[i * kVertexSize + j] = stream[0][j * count + i];
        }
    }
    return true;
}

// 三角的重心不存,这里先留空,顶点到位后按 ((a+b)+c)/3.0f 重算。
bool DecodeTriangleChunk(const uint8_t* data, size_t size, uint32_t count, uint32_t first, uint8_t* out)
{
    std::vector<std::vector<uint8_t>> stream;
    if (!SplitChunkStreams(data, size, 7, &stream) || stream[3].size() != (static_cast<size_t>(count) * 3 + 7) / 8) {
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

bool DecodeLinkChunk(const uint8_t* data, size_t size, uint32_t count, uint8_t* out)
{
    std::vector<std::vector<uint8_t>> stream;
    if (!SplitChunkStreams(data, size, 2, &stream)) {
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

// 解第 [low, high] 块, 接在 out 尾上。块定长, 每块的输出长度只由自己的 span 定,
// 所以先按总长开好再让各块写各自那段 —— 与一块块接着写逐字节相同, 于是能并起来解。
bool DecodeChunkSpan(
    const GeoChunkTable& table,
    const uint8_t* base,
    uint32_t low,
    uint32_t high,
    size_t record_size,
    std::vector<uint8_t>* out,
    const std::function<bool(const uint8_t*, size_t, uint32_t, uint32_t, uint8_t*)>& decode)
{
    const uint32_t base_record = table.first(low);
    const size_t records = static_cast<size_t>(table.first(high)) + table.span(high) - base_record;
    const size_t at = out->size();
    out->resize(at + records * record_size);
    const auto chunks = static_cast<int64_t>(high - low + 1);
    std::vector<uint8_t> ok(static_cast<size_t>(chunks), 1);
    ParallelChunks(chunks, NavWorkerCount(static_cast<int64_t>(records)), [&](size_t, int64_t b, int64_t e) {
        for (int64_t c = b; c < e; ++c) {
            const uint32_t i = low + static_cast<uint32_t>(c);
            uint32_t size = 0;
            const uint8_t* src = table.bytes(base, i, size);
            uint8_t* dst = out->data() + at + (static_cast<size_t>(table.first(i)) - base_record) * record_size;
            if (!decode(src, size, table.span(i), table.first(i), dst)) {
                ok[static_cast<size_t>(c)] = 0;
            }
        }
    });
    return std::find(ok.begin(), ok.end(), 0) == ok.end();
}

// 解出覆盖 [from, to) 的整块,返回块首记录的全局下标。
bool DecodeChunkRange(
    const GeoChunkTable& table,
    const uint8_t* base,
    uint32_t from,
    uint32_t to,
    size_t record_size,
    std::vector<uint8_t>* out,
    uint32_t* out_first,
    const std::function<bool(const uint8_t*, size_t, uint32_t, uint32_t, uint8_t*)>& decode)
{
    if (from >= to) {
        *out_first = from;
        return true;
    }
    const uint32_t low = from / table.chunk;
    const uint32_t high = (to - 1) / table.chunk;
    if (high >= table.count()) {
        return false;
    }
    *out_first = table.first(low);
    return DecodeChunkSpan(table, base, low, high, record_size, out, decode);
}

BaseNavLoadResult Fail(BaseNavLoadStatus status, std::string message)
{
    BaseNavLoadResult result;
    result.status = status;
    result.message = std::move(message);
    return result;
}

// 连接块目录里带每块首条的 source。表按 source 非降,所以「下一块首条还 < source」
// 就说明本块整块都在前面,可以跳过。
uint32_t GeoLinkChunkFor(const GeoChunkTable& table, uint32_t source)
{
    uint32_t index = 0;
    while (index + 1 < table.count() && table.aux(index + 1) < source) {
        ++index;
    }
    return index;
}

size_t LowerBoundLinkSource(const uint8_t* link_bytes, uint32_t link_count, uint32_t source)
{
    size_t left = 0;
    size_t right = link_count;
    while (left < right) {
        const size_t middle = left + (right - left) / 2;
        const uint32_t middle_source = PeekU32(link_bytes + middle * kLinkSize);
        if (middle_source < source) {
            left = middle + 1;
        }
        else {
            right = middle;
        }
    }
    return left;
}

bool HasGzipSuffix(const std::filesystem::path& path)
{
    return path.extension() == ".gz";
}

std::optional<size_t> ReadGzipUncompressedSize(const std::filesystem::path& path)
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
    if (!ReadExact(input, size_bytes.data(), size_bytes.size())) {
        return std::nullopt;
    }
    return static_cast<size_t>(PeekU32(size_bytes.data()));
}

BaseNavLoadResult ReadGzipFile(const std::filesystem::path& path, std::vector<uint8_t>* output)
{
#ifdef _WIN32
    gzFile file = gzopen_w(path.c_str(), "rb");
#else
    gzFile file = gzopen(path.string().c_str(), "rb");
#endif
    if (file == nullptr) {
        return Fail(BaseNavLoadStatus::FileOpenFailed, "failed to open gzip nav file");
    }
    gzbuffer(file, static_cast<unsigned int>(kGzipReadChunkSize));
    if (const auto uncompressed_size = ReadGzipUncompressedSize(path); uncompressed_size && *uncompressed_size > 0) {
        output->reserve(*uncompressed_size);
    }

    std::vector<uint8_t> buffer(kGzipReadChunkSize);
    while (true) {
        const int bytes_read = gzread(file, buffer.data(), static_cast<unsigned int>(buffer.size()));
        if (bytes_read < 0) {
            int error_code = Z_OK;
            const char* message = gzerror(file, &error_code);
            gzclose(file);
            return Fail(BaseNavLoadStatus::FileReadFailed, message != nullptr ? message : "failed to decompress gzip nav file");
        }
        if (bytes_read == 0) {
            break;
        }
        output->insert(output->end(), buffer.begin(), buffer.begin() + bytes_read);
    }

    if (gzclose(file) != Z_OK) {
        return Fail(BaseNavLoadStatus::FileReadFailed, "failed to close gzip nav file");
    }
    return {};
}

BaseNavLoadResult ReadNavFileBytes(const std::filesystem::path& path, std::vector<uint8_t>* output)
{
    if (HasGzipSuffix(path)) {
        return ReadGzipFile(path, output);
    }

    std::error_code ec;
    const uint64_t file_size = std::filesystem::file_size(path, ec);
    if (ec) {
        return Fail(BaseNavLoadStatus::FileOpenFailed, "failed to stat nav file");
    }

    std::ifstream input(path, std::ios::binary);
    if (!input) {
        return Fail(BaseNavLoadStatus::FileOpenFailed, "failed to open nav file");
    }
    output->resize(static_cast<size_t>(file_size));
    if (!output->empty() && !ReadExact(input, output->data(), output->size())) {
        return Fail(BaseNavLoadStatus::FileReadFailed, "failed to read nav file");
    }
    return {};
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
        return Fail(BaseNavLoadStatus::InvalidSize, "nav file is smaller than header");
    }

    if (std::memcmp(file_bytes.data(), "BNAV", 4) != 0) {
        return Fail(BaseNavLoadStatus::InvalidMagic, "invalid nav magic");
    }
    const uint8_t* header_cursor = file_bytes.data() + 4;
    const uint16_t version = ReadU16(header_cursor);
    (void)ReadU16(header_cursor); // flags
    if (version == 0 || version > kBaseNavRevision) {
        return Fail(BaseNavLoadStatus::UnsupportedVersion, "unsupported nav version");
    }
    const size_t header_size = version >= kSectionDirSince ? kHeaderSizeV4 : kHeaderSize;
    if (file_size < header_size) {
        return Fail(BaseNavLoadStatus::InvalidSize, "nav file is smaller than header");
    }
    const size_t zone_prefix_size = kZonePrefixSize + (version >= kZoneFloorYSince ? 4 : 0);
    const uint32_t zone_count = ReadU32(header_cursor);
    const uint32_t vertex_count = ReadU32(header_cursor);
    const uint32_t triangle_count = ReadU32(header_cursor);
    const uint32_t link_count = ReadU32(header_cursor);
    const uint64_t zone_table_offset = ReadU64(header_cursor);
    const uint64_t vertex_offset = ReadU64(header_cursor);
    const uint64_t triangle_offset = ReadU64(header_cursor);
    const uint64_t link_offset = ReadU64(header_cursor);
    const uint64_t build_hash = ReadU64(header_cursor);

    if (link_count == 0) {
        return Fail(BaseNavLoadStatus::InvalidSize, "nav link table is empty");
    }
    uint64_t section_dir_offset = 0;
    uint32_t section_count = 0;
    if (version >= kSectionDirSince) {
        section_dir_offset = ReadU64(header_cursor);
        section_count = ReadU32(header_cursor);
        (void)ReadU32(header_cursor); // reserved
        if (section_count != 0
            && !OffsetRangeValid(section_dir_offset, static_cast<uint64_t>(section_count) * kSectionEntrySize, file_size)) {
            return Fail(BaseNavLoadStatus::InvalidOffset, "nav section directory is outside file bounds");
        }
    }

    // 段目录先读:v5 起四块几何连同区表都在 BGEO 段里,头里那四个偏移作废。
    // 只有 BGRD 的字节要活过本函数(规划时才按瓦解码),其余段就地解析,段条目留空壳 ——
    // 下游只拿 section(tag) != nullptr 认包的世代。
    std::vector<BaseNavSection> sections;
    std::vector<std::pair<const uint8_t*, size_t>> section_raw;
    sections.reserve(section_count);
    section_raw.reserve(section_count);
    const uint8_t* dir_cursor = file_bytes.data() + section_dir_offset;
    for (uint32_t index = 0; index < section_count; ++index) {
        BaseNavSection sec;
        std::memcpy(sec.tag.data(), dir_cursor, 4);
        dir_cursor += 4;
        sec.flags = ReadU32(dir_cursor);
        const uint64_t offset = ReadU64(dir_cursor);
        const uint64_t size = ReadU64(dir_cursor);
        if (!OffsetRangeValid(offset, size, file_size)) {
            return Fail(BaseNavLoadStatus::InvalidOffset, "nav section is outside file bounds");
        }
        const uint8_t* at = file_bytes.data() + offset;
        section_raw.emplace_back(at, static_cast<size_t>(size));
        if (std::memcmp(sec.tag.data(), kGridSectionTag, 4) == 0) {
            sec.bytes.assign(at, at + size);
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
        if (found == nullptr || !ParseGeometrySection(found->first, found->second, &geometry)) {
            return Fail(BaseNavLoadStatus::InvalidOffset, "nav geometry section is missing or malformed");
        }
        if (geometry.vertices.total != vertex_count || geometry.triangles.total != triangle_count || geometry.links.total != link_count) {
            return Fail(BaseNavLoadStatus::InvalidSize, "nav geometry section disagrees with the header counts");
        }
        zone_table = geometry.zone_table;
        zone_table_size = geometry.zone_table_size;
    }
    else {
        if (zone_table_offset < header_size || vertex_offset < zone_table_offset || triangle_offset < vertex_offset
            || link_offset < triangle_offset) {
            return Fail(BaseNavLoadStatus::InvalidOffset, "invalid nav offsets");
        }
        if (!OffsetRangeValid(vertex_offset, static_cast<uint64_t>(vertex_count) * kVertexSize, file_size)
            || !OffsetRangeValid(triangle_offset, static_cast<uint64_t>(triangle_count) * kTriangleSize, file_size)
            || !OffsetRangeValid(link_offset, static_cast<uint64_t>(link_count) * kLinkSize, file_size)) {
            return Fail(BaseNavLoadStatus::InvalidOffset, "nav payload is outside file bounds");
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
            return Fail(BaseNavLoadStatus::InvalidSize, "zone table is truncated");
        }
        BaseNavZone zone;
        zone.zone_id = ReadU16(zone_cursor);
        zone.flags = ReadU16(zone_cursor);
        const uint32_t name_size = ReadU32(zone_cursor);
        zone.first_triangle = ReadU32(zone_cursor);
        zone.triangle_count = ReadU32(zone_cursor);
        zone.component_count = ReadU32(zone_cursor);
        zone.width = ReadF32(zone_cursor);
        zone.height = ReadF32(zone_cursor);
        for (float& value : zone.transform) {
            value = ReadF32(zone_cursor);
        }
        if (version >= kZoneFloorYSince) {
            // v3 起每区多一个值;更早的包留在哨兵值上。
            zone.floor_y = ReadF32(zone_cursor);
        }
        if (static_cast<size_t>(zone_end - zone_cursor) < name_size) {
            return Fail(BaseNavLoadStatus::InvalidSize, "zone name is truncated");
        }
        zone.name.assign(reinterpret_cast<const char*>(zone_cursor), name_size);
        zone_cursor += name_size;
        if (!zone_ids.insert(zone.zone_id).second) {
            return Fail(BaseNavLoadStatus::DuplicateZone, "duplicate zone id");
        }
        zones.push_back(std::move(zone));
    }
    std::optional<BaseNavZone> selected_zone;
    for (const BaseNavZone& zone : zones) {
        if (static_cast<uint64_t>(zone.first_triangle) + zone.triangle_count > triangle_count) {
            return Fail(BaseNavLoadStatus::InvalidSize, "zone triangle range is outside triangle table");
        }
        if (!zone_name.empty() && zone.name == zone_name) {
            selected_zone = zone;
        }
    }
    if (!zone_name.empty() && !selected_zone) {
        return Fail(BaseNavLoadStatus::ZoneNotFound, "nav zone not found");
    }
    const uint32_t selected_first_triangle = selected_zone ? selected_zone->first_triangle : 0;
    const uint32_t selected_triangle_count = selected_zone ? selected_zone->triangle_count : triangle_count;
    const uint32_t selected_triangle_end = selected_first_triangle + selected_triangle_count;
    const bool zone_scoped = selected_zone.has_value();

    // v5:只解这次用得着的块。重心不在包里,所以先扫一遍三角拿到顶点下标范围,
    // 解完顶点再把重心按 ((a+b)+c)/3.0f 补回记录里 —— 换成乘 1/3 不逐位相同。
    std::vector<uint8_t> triangle_storage;
    std::vector<uint8_t> vertex_storage;
    std::vector<uint8_t> link_storage;
    uint32_t triangle_base = 0;
    uint32_t vertex_base = 0;
    uint32_t link_base = 0;
    uint32_t link_table_count = link_count;
    if (sectioned) {
        if (!DecodeChunkRange(
                geometry.triangles,
                geometry.base,
                selected_first_triangle,
                selected_triangle_end,
                kTriangleSize,
                &triangle_storage,
                &triangle_base,
                [](const uint8_t* at, size_t size, uint32_t span, uint32_t first, uint8_t* dst) {
                    return DecodeTriangleChunk(at, size, span, first, dst);
                })) {
            return Fail(BaseNavLoadStatus::InvalidSize, "nav triangle chunk is malformed");
        }
        uint32_t low = vertex_count;
        uint32_t high = 0;
        for (size_t at = 0; at + kTriangleSize <= triangle_storage.size(); at += kTriangleSize) {
            for (int i = 0; i < 3; ++i) {
                const uint32_t value = PeekU32(triangle_storage.data() + at + i * 4);
                if (value >= vertex_count) {
                    return Fail(BaseNavLoadStatus::InvalidSize, "triangle vertex index is outside vertex table");
                }
                low = std::min(low, value);
                high = std::max(high, value);
            }
        }
        // 三角为空的区(tier 只有一份仿射)不用解顶点。
        uint32_t want_low = 0;
        uint32_t want_high = vertex_count;
        if (zone_scoped) {
            want_low = low <= high ? low : 0;
            want_high = low <= high ? high + 1 : 0;
        }
        if (!DecodeChunkRange(
                geometry.vertices,
                geometry.base,
                want_low,
                want_high,
                kVertexSize,
                &vertex_storage,
                &vertex_base,
                [](const uint8_t* at, size_t size, uint32_t span, uint32_t, uint8_t* dst) {
                    return DecodeVertexChunk(at, size, span, dst);
                })) {
            return Fail(BaseNavLoadStatus::InvalidSize, "nav vertex chunk is malformed");
        }
        for (size_t at = 0; at + kTriangleSize <= triangle_storage.size(); at += kTriangleSize) {
            uint8_t* const record = triangle_storage.data() + at;
            float u[3] = {};
            float v[3] = {};
            for (int i = 0; i < 3; ++i) {
                const uint8_t* vertex = vertex_storage.data() + static_cast<size_t>(PeekU32(record + i * 4) - vertex_base) * kVertexSize;
                std::memcpy(&u[i], vertex, sizeof(float));
                std::memcpy(&v[i], vertex + 4, sizeof(float));
            }
            const float center_u = ((u[0] + u[1]) + u[2]) / 3.0F;
            const float center_v = ((v[0] + v[1]) + v[2]) / 3.0F;
            std::memcpy(record + 28, &center_u, sizeof(float));
            std::memcpy(record + 32, &center_v, sizeof(float));
        }

        // 连接表可以一条都没有,那时一块都不解,下游按 0 条连接走。
        const uint32_t link_chunks = geometry.links.count();
        const uint32_t low_chunk = zone_scoped ? GeoLinkChunkFor(geometry.links, selected_first_triangle) : 0;
        const uint32_t high_chunk = zone_scoped ? GeoLinkChunkFor(geometry.links, selected_triangle_end) : link_chunks;
        if (low_chunk < link_chunks && low_chunk <= high_chunk) {
            const uint32_t last_chunk = std::min(high_chunk, link_chunks - 1);
            if (!DecodeChunkSpan(
                    geometry.links,
                    geometry.base,
                    low_chunk,
                    last_chunk,
                    kLinkSize,
                    &link_storage,
                    [](const uint8_t* at, size_t size, uint32_t span, uint32_t, uint8_t* dst) {
                        return DecodeLinkChunk(at, size, span, dst);
                    })) {
                return Fail(BaseNavLoadStatus::InvalidSize, "nav link chunk is malformed");
            }
        }
        link_base = geometry.links.first(low_chunk);
        link_table_count = static_cast<uint32_t>(link_storage.size() / kLinkSize);
        vertex_bytes = vertex_storage.data();
        triangle_bytes = triangle_storage.data();
        link_bytes = link_storage.data();
    }

    // 哈希仍定义在四块的规范字节上,与它们怎么存无关。v5 的字节要先解码才有,
    // 所以只在整包加载时算;按区加载的 gzip 流本来就有 CRC 兜底。
    const bool should_verify_hash = zone_name.empty() || (!sectioned && !HasGzipSuffix(path));
    if (should_verify_hash) {
        uint64_t hash = kFnvOffset;
        hash = Fnv64Update(hash, zone_table, static_cast<size_t>(zone_table_size));
        hash = Fnv64Update(hash, vertex_bytes, static_cast<size_t>(vertex_count) * kVertexSize);
        hash = Fnv64Update(hash, triangle_bytes, static_cast<size_t>(triangle_count) * kTriangleSize);
        hash = Fnv64Update(hash, link_bytes, static_cast<size_t>(link_count) * kLinkSize);
        if (hash != build_hash) {
            return Fail(BaseNavLoadStatus::HashMismatch, "nav build hash mismatch");
        }
    }

    std::vector<BaseNavTriangle> triangles;
    triangles.reserve(selected_triangle_count);
    uint32_t first_vertex = zone_scoped ? vertex_count : 0;
    uint32_t last_vertex = 0;
    const uint8_t* triangle_cursor = triangle_bytes + static_cast<uint64_t>(selected_first_triangle - triangle_base) * kTriangleSize;
    for (uint32_t index = 0; index < selected_triangle_count; ++index) {
        BaseNavTriangle triangle = ReadTriangleRecord(triangle_cursor);
        for (uint32_t value : triangle.vertices) {
            if (value >= vertex_count) {
                return Fail(BaseNavLoadStatus::InvalidSize, "triangle vertex index is outside vertex table");
            }
            if (zone_scoped) {
                first_vertex = std::min(first_vertex, value);
                last_vertex = std::max(last_vertex, value);
            }
        }
        if (zone_scoped) {
            for (int32_t& neighbor : triangle.neighbors) {
                if (neighbor < 0 || static_cast<uint32_t>(neighbor) < selected_first_triangle
                    || static_cast<uint32_t>(neighbor) >= selected_triangle_end) {
                    neighbor = -1;
                    continue;
                }
                neighbor -= static_cast<int32_t>(selected_first_triangle);
            }
        }
        triangles.push_back(triangle);
    }

    const uint32_t selected_vertex_count = zone_scoped ? (triangles.empty() ? 0 : last_vertex - first_vertex + 1) : vertex_count;
    std::vector<BaseNavVertex> vertices;
    vertices.reserve(selected_vertex_count);
    const uint64_t vertex_skip = selected_vertex_count == 0 ? 0 : static_cast<uint64_t>(first_vertex - vertex_base);
    const uint8_t* vertex_cursor = vertex_bytes + vertex_skip * kVertexSize;
    for (uint32_t index = 0; index < selected_vertex_count; ++index) {
        BaseNavVertex vertex;
        vertex.u = ReadF32(vertex_cursor);
        vertex.v = ReadF32(vertex_cursor);
        vertex.height = ReadF32(vertex_cursor);
        vertices.push_back(vertex);
    }
    if (zone_scoped) {
        for (BaseNavTriangle& triangle : triangles) {
            for (uint32_t& vertex_index : triangle.vertices) {
                vertex_index -= first_vertex;
            }
        }
        selected_zone->first_triangle = 0;
        selected_zone->triangle_count = static_cast<uint32_t>(triangles.size());
        // Keep the selected geometry zone PLUS its tier children (zones whose component_count points
        // back at it). Tier zones are 0-triangle affine metadata, so retaining them never touches the
        // sliced geometry / snap graph / golden-hash parity (this in-memory vector is not serialized) —
        // it just lets a zone-scoped pack still resolve a tier zone_id to base via its OWN baked affine,
        // mirroring the python loader which holds every zone.
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
    const size_t first_link = zone_scoped ? LowerBoundLinkSource(link_bytes, link_table_count, selected_first_triangle) : 0;
    const size_t last_link = zone_scoped ? LowerBoundLinkSource(link_bytes, link_table_count, selected_triangle_end) : link_table_count;
    links.reserve(last_link - first_link);
    const uint8_t* link_cursor = link_bytes + first_link * kLinkSize;
    for (size_t index = first_link; index < last_link; ++index) {
        BaseNavLink link = ReadLinkRecord(link_cursor);
        if (link.source >= triangle_count || link.target >= triangle_count) {
            LogWarn << "Skipping invalid BaseNav link." << VAR(link_base + index) << VAR(link.source) << VAR(link.target)
                    << VAR(triangle_count);
            continue;
        }
        if (zone_scoped) {
            if (link.source < selected_first_triangle || link.source >= selected_triangle_end || link.target < selected_first_triangle
                || link.target >= selected_triangle_end) {
                continue;
            }
            link.source -= selected_first_triangle;
            link.target -= selected_first_triangle;
        }
        links.push_back(link);
    }

    std::vector<BaseNavOffMeshLink> off_mesh_links;
    for (size_t i = 0; i < sections.size(); ++i) {
        if (std::memcmp(sections[i].tag.data(), kOffMeshSectionTag, 4) == 0
            && !ParseOffMeshSection(section_raw[i].first, section_raw[i].second, &off_mesh_links)) {
            return Fail(BaseNavLoadStatus::InvalidSize, "nav off-mesh section is malformed");
        }
    }
    std::vector<BaseNavSurface> surfaces;
    for (size_t i = 0; i < sections.size(); ++i) {
        if (std::memcmp(sections[i].tag.data(), kSurfaceSectionTag, 4) == 0) {
            if (!ParseSurfaceSection(section_raw[i].first, section_raw[i].second, &surfaces)
                || surfaces.size() != triangle_count) {
                return Fail(BaseNavLoadStatus::InvalidSize, "nav surface section is malformed");
            }
        }
    }
    if (zone_scoped && !surfaces.empty()) {
        std::vector<BaseNavSurface> scoped(
            surfaces.begin() + selected_first_triangle,
            surfaces.begin() + selected_triangle_end);
        surfaces = std::move(scoped);
    }
    if (zone_scoped) {
        std::erase_if(off_mesh_links, [&](const BaseNavOffMeshLink& link) { return link.zone_id != selected_zone->zone_id; });
    }
    for (const BaseNavOffMeshLink& link : off_mesh_links) {
        const bool known_zone = std::any_of(
            zones.begin(), zones.end(), [&](const BaseNavZone& zone) { return zone.zone_id == link.zone_id; });
        if (!known_zone) {
            return Fail(BaseNavLoadStatus::InvalidSize, "nav off-mesh link refers to an unknown zone");
        }
    }

    BaseNavLoadResult result;
    result.pack = detail::MakeBaseNavPack(
        path,
        std::move(zones),
        std::move(vertices),
        std::move(triangles),
        std::move(links),
        std::move(surfaces),
        std::move(off_mesh_links),
        std::move(sections));
    return result;
}

}
