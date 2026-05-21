#include <array>
#include <cstddef>
#include <cstdint>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <set>
#include <string>
#include <utility>
#include <vector>

#include <zlib.h>

#include <MaaUtils/Logger.h>

#include "BaseNavReader.h"

namespace navmesh
{

namespace
{

constexpr size_t kHeaderSize = 64;
constexpr size_t kZonePrefixSize = 44;
constexpr size_t kVertexSize = 12;
constexpr size_t kTriangleSize = 36;
constexpr size_t kLinkSize = 8;
constexpr uint16_t kBaseNavVersion = 2;
constexpr size_t kGzipReadChunkSize = 1 << 20;
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

BaseNavLoadResult Fail(BaseNavLoadStatus status, std::string message)
{
    BaseNavLoadResult result;
    result.status = status;
    result.message = std::move(message);
    return result;
}

bool HasGzipSuffix(const std::filesystem::path& path)
{
    return path.extension() == ".gz";
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

    std::array<uint8_t, kGzipReadChunkSize> buffer {};
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

BaseNavLoadResult LoadBaseNavPack(const std::filesystem::path& path)
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
    if (version != kBaseNavVersion) {
        return Fail(BaseNavLoadStatus::UnsupportedVersion, "unsupported nav version");
    }
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
    if (zone_table_offset < kHeaderSize || vertex_offset < zone_table_offset || triangle_offset < vertex_offset
        || link_offset < triangle_offset) {
        return Fail(BaseNavLoadStatus::InvalidOffset, "invalid nav offsets");
    }
    const uint64_t vertex_size = static_cast<uint64_t>(vertex_count) * kVertexSize;
    const uint64_t triangle_size = static_cast<uint64_t>(triangle_count) * kTriangleSize;
    const uint64_t link_size = static_cast<uint64_t>(link_count) * kLinkSize;
    if (!OffsetRangeValid(vertex_offset, vertex_size, file_size) || !OffsetRangeValid(triangle_offset, triangle_size, file_size)
        || !OffsetRangeValid(link_offset, link_size, file_size)) {
        return Fail(BaseNavLoadStatus::InvalidOffset, "nav payload is outside file bounds");
    }

    const uint64_t zone_table_size = vertex_offset - zone_table_offset;
    const uint8_t* zone_table = file_bytes.data() + zone_table_offset;
    const uint8_t* zone_end = zone_table + zone_table_size;
    const uint8_t* vertex_bytes = file_bytes.data() + vertex_offset;
    const uint8_t* triangle_bytes = file_bytes.data() + triangle_offset;
    const uint8_t* link_bytes = file_bytes.data() + link_offset;

    uint64_t hash = kFnvOffset;
    hash = Fnv64Update(hash, zone_table, static_cast<size_t>(zone_table_size));
    hash = Fnv64Update(hash, vertex_bytes, static_cast<size_t>(vertex_size));
    hash = Fnv64Update(hash, triangle_bytes, static_cast<size_t>(triangle_size));
    hash = Fnv64Update(hash, link_bytes, static_cast<size_t>(link_size));
    if (hash != build_hash) {
        return Fail(BaseNavLoadStatus::HashMismatch, "nav build hash mismatch");
    }

    std::vector<BaseNavZone> zones;
    zones.reserve(zone_count);
    std::set<uint16_t> zone_ids;
    const uint8_t* zone_cursor = zone_table;
    for (uint32_t index = 0; index < zone_count; ++index) {
        if (static_cast<size_t>(zone_end - zone_cursor) < kZonePrefixSize) {
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

    std::vector<BaseNavVertex> vertices;
    vertices.reserve(vertex_count);
    const uint8_t* vertex_cursor = vertex_bytes;
    for (uint32_t index = 0; index < vertex_count; ++index) {
        BaseNavVertex vertex;
        vertex.u = ReadF32(vertex_cursor);
        vertex.v = ReadF32(vertex_cursor);
        vertex.height = ReadF32(vertex_cursor);
        vertices.push_back(vertex);
    }

    std::vector<BaseNavTriangle> triangles;
    triangles.reserve(triangle_count);
    const uint8_t* triangle_cursor = triangle_bytes;
    for (uint32_t index = 0; index < triangle_count; ++index) {
        BaseNavTriangle triangle;
        for (uint32_t& value : triangle.vertices) {
            value = ReadU32(triangle_cursor);
            if (value >= vertex_count) {
                return Fail(BaseNavLoadStatus::InvalidSize, "triangle vertex index is outside vertex table");
            }
        }
        for (int32_t& value : triangle.neighbors) {
            value = ReadI32(triangle_cursor);
        }
        triangle.component_id = ReadU32(triangle_cursor);
        triangle.center_u = ReadF32(triangle_cursor);
        triangle.center_v = ReadF32(triangle_cursor);
        triangles.push_back(triangle);
    }

    std::vector<BaseNavLink> links;
    links.reserve(link_count);
    const uint8_t* link_cursor = link_bytes;
    for (uint32_t index = 0; index < link_count; ++index) {
        BaseNavLink link;
        link.source = ReadU32(link_cursor);
        link.target = ReadU32(link_cursor);
        if (link.source >= triangle_count || link.target >= triangle_count) {
            LogWarn << "Skipping invalid BaseNav link." << VAR(index) << VAR(link.source) << VAR(link.target)
                    << VAR(triangle_count);
            continue;
        }
        links.push_back(link);
    }

    BaseNavLoadResult result;
    result.pack = detail::MakeBaseNavPack(path, std::move(zones), std::move(vertices), std::move(triangles), std::move(links));
    return result;
}

}
