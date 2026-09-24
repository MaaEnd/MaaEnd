#pragma once

#include <cstdint>

namespace navmesh
{

// Reads one unsigned LEB128 value and advances cursor. Returns false when the input ends or the value exceeds 64 bits.
// The main pack, the fields sidecar and the occluder pack share this reader for their delta streams.
inline bool GetVarint(const uint8_t*& cursor, const uint8_t* end, uint64_t& out)
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

// Maps a zigzag-encoded value back to its signed form.
inline int64_t UnZigzag(uint64_t value)
{
    return static_cast<int64_t>(value >> 1U) ^ -static_cast<int64_t>(value & 1U);
}

} // namespace navmesh
