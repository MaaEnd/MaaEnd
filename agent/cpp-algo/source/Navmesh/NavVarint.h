#pragma once

#include <cstdint>

namespace navmesh
{

// 无符号 LEB128, 输入耗尽或超过 64 位时返回 false
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

inline int64_t UnZigzag(uint64_t value)
{
    return static_cast<int64_t>(value >> 1U) ^ -static_cast<int64_t>(value & 1U);
}

} // namespace navmesh
