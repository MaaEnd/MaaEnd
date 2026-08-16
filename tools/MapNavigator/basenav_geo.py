"""BGEO 段解码:把四块几何还原成定长记录的规范字节。

包里四块几何按定长块存,块内自足解码。这里是预览端的整段解码,一次解完所有块,
返回的字节与旧版包里那四段裸表逐位相同 —— 上层的解析、索引、build hash 都不用动。
"""

from __future__ import annotations

import struct
import zlib

import numpy as np


SECTION_TAG = b"BGEO"
VERSION = 1
HEADER_SIZE = 72
CHUNK_ENTRY_SIZE = 16

VERTEX_SIZE = 12
TRIANGLE_SIZE = 36
LINK_SIZE = 8

# 三角块的流数:v0 差分、v1-v0、v2-v0、邻接位图、邻接残差、分量游程值、分量游程长。
TRIANGLE_STREAMS = 7
VERTEX_STREAMS = 1
LINK_STREAMS = 2

# varint 最长 10 字节(64 位 / 每字节 7 位)。
_VARINT_MAX_BYTES = 10


def _varints(stream: bytes) -> np.ndarray:
    """一条 varint 流全解成 uint64。逐字节位置分层做,不走 Python 循环。"""
    raw = np.frombuffer(stream, dtype=np.uint8)
    if raw.size == 0:
        return np.zeros(0, dtype=np.uint64)
    tail = (raw & 0x80) == 0
    # 第 i 个字节属于第 group[i] 个值:每遇到一个结尾字节,组号加一。
    if not tail[-1]:
        raise ValueError("varint 流末尾缺结尾字节")
    group = np.empty(raw.size, dtype=np.int64)
    group[0] = 0
    np.cumsum(tail[:-1], out=group[1:])
    count = int(group[-1]) + 1
    starts = np.searchsorted(group, np.arange(count))
    position = np.arange(raw.size, dtype=np.int64) - starts[group]
    payload = (raw & 0x7F).astype(np.uint64)
    out = np.zeros(count, dtype=np.uint64)
    for shift in range(_VARINT_MAX_BYTES):
        picked = position == shift
        if not picked.any():
            break
        out[group[picked]] |= payload[picked] << np.uint64(7 * shift)
    return out


def _unzigzag(values: np.ndarray) -> np.ndarray:
    return (values >> np.uint64(1)).astype(np.int64) ^ -(values & np.uint64(1)).astype(np.int64)


def _split_streams(chunk: memoryview, count: int) -> list[bytes]:
    """块载荷 = 若干条「varint 长度 + deflate 流」,条数由表类型定死。"""
    out = []
    at = 0
    for _ in range(count):
        length = 0
        shift = 0
        while True:
            byte = chunk[at]
            at += 1
            length |= (byte & 0x7F) << shift
            if byte & 0x80 == 0:
                break
            shift += 7
        out.append(zlib.decompress(chunk[at:at + length]))
        at += length
    if at != len(chunk):
        raise ValueError("BGEO 块尾有多余字节")
    return out


def _decode_vertex_chunk(chunk: memoryview, count: int) -> np.ndarray:
    """顶点做过字节平面重排:n×12 转置成 12×n,这里转回去。"""
    plane = np.frombuffer(_split_streams(chunk, VERTEX_STREAMS)[0], dtype=np.uint8)
    if plane.size != count * VERTEX_SIZE:
        raise ValueError("BGEO 顶点块长度与记录数对不上")
    return plane.reshape(VERTEX_SIZE, count).T


def _decode_triangle_chunk(chunk: memoryview, count: int, first: int) -> np.ndarray:
    streams = _split_streams(chunk, TRIANGLE_STREAMS)
    v0 = np.cumsum(_unzigzag(_varints(streams[0])))
    v1 = v0 + _unzigzag(_varints(streams[1]))
    v2 = v0 + _unzigzag(_varints(streams[2]))

    bits = np.unpackbits(np.frombuffer(streams[3], dtype=np.uint8))[:count * 3].astype(bool)
    self_index = np.repeat(np.arange(first, first + count, dtype=np.int64), 3)
    neighbors = np.full(count * 3, -1, dtype=np.int64)
    neighbors[bits] = self_index[bits] + _unzigzag(_varints(streams[4]))

    run_value = np.cumsum(_unzigzag(_varints(streams[5])))
    run_length = _varints(streams[6]).astype(np.int64)
    components = np.repeat(run_value, run_length)
    if components.size != count or v0.size != count:
        raise ValueError("BGEO 三角块的记录数对不上")

    out = np.empty((count, 9), dtype=np.uint32)
    out[:, 0] = v0
    out[:, 1] = v1
    out[:, 2] = v2
    out[:, 3:6] = neighbors.reshape(count, 3).astype(np.int32).view(np.uint32)
    out[:, 6] = components
    return out


def _decode_link_chunk(chunk: memoryview, count: int) -> np.ndarray:
    streams = _split_streams(chunk, LINK_STREAMS)
    source = np.cumsum(_varints(streams[0]).astype(np.int64))
    target = source + _unzigzag(_varints(streams[1]))
    if source.size != count:
        raise ValueError("BGEO 连接块的记录数对不上")
    out = np.empty((count, 2), dtype=np.uint32)
    out[:, 0] = source
    out[:, 1] = target
    return out


class _ChunkTable:
    __slots__ = ("section", "directory", "chunk", "total")

    def __init__(self, section: memoryview, directory: int, chunk: int, total: int) -> None:
        self.section = section
        self.directory = directory
        self.chunk = chunk
        self.total = total

    def __len__(self) -> int:
        return (self.total + self.chunk - 1) // self.chunk if self.chunk else 0

    def span(self, index: int) -> int:
        return min(self.chunk, self.total - index * self.chunk)

    def bytes(self, index: int) -> memoryview:
        at = self.directory + index * CHUNK_ENTRY_SIZE
        offset, size, _aux = struct.unpack_from("<QII", self.section, at)
        return self.section[offset:offset + size]


def decode_geometry(section: memoryview) -> tuple[memoryview, bytes, bytes, bytes]:
    """整段解码,返回 (区表, 顶点, 三角, 连接) 四块的规范字节。

    重心不在包里,按 ``((a+b)+c)/3`` 重算 —— float32 下与写成乘 1/3 不逐位相同。
    """
    if len(section) < HEADER_SIZE or bytes(section[0:4]) != SECTION_TAG:
        raise ValueError("BGEO 段头不对")
    version, vertex_count, triangle_count, link_count = struct.unpack_from("<4I", section, 4)
    if version != VERSION:
        raise ValueError("unsupported BGEO version")
    vertex_chunk, triangle_chunk, link_chunk, zone_size, _reserved = struct.unpack_from("<5I", section, 20)
    vertex_dir, triangle_dir, link_dir, zone_at = struct.unpack_from("<4Q", section, 40)

    table = _ChunkTable(section, vertex_dir, vertex_chunk, vertex_count)
    vertices = np.concatenate([_decode_vertex_chunk(table.bytes(i), table.span(i)) for i in range(len(table))]).tobytes()
    table = _ChunkTable(section, triangle_dir, triangle_chunk, triangle_count)
    triangles = np.concatenate(
        [_decode_triangle_chunk(table.bytes(i), table.span(i), i * triangle_chunk) for i in range(len(table))]
    )
    table = _ChunkTable(section, link_dir, link_chunk, link_count)
    links = np.concatenate([_decode_link_chunk(table.bytes(i), table.span(i)) for i in range(len(table))])

    coord = np.frombuffer(vertices, dtype=np.float32).reshape(vertex_count, 3)[:, :2]
    corner = coord[triangles[:, 0:3].astype(np.int64)]
    center = ((corner[:, 0] + corner[:, 1]) + corner[:, 2]) / np.float32(3.0)
    triangles[:, 7:9] = center.view(np.uint32)

    return section[zone_at:zone_at + zone_size], vertices, triangles.tobytes(), links.tobytes()
