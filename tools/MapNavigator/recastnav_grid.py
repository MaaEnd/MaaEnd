#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""包里预烘格图段(BGRD)的解码,与 RecastNavGridIO 同格式同口径。"""
import struct
import zlib

import numpy as np

from recastnav import CS

TAG = b"BGRD"
SECTION_VERSION = 2
HEADER = struct.Struct("<4sIdddII")  # tag + 版本 + 格边长 + 瓦边长 + 裙边 + 区数 + 保留
ZONE_HEAD = struct.Struct("<ddII")  # x0 + y0 + 全区类号数 + 瓦数
TILE = struct.Struct("<8iQII")  # gx0 gy0 nx ny px0 px1 py0 py1 + 偏移 + 长度 + 记录数

FLAG_DEAD = 0x01   # 墙压过的 span
FLAG_WALK = 0x02   # 既在层里又在核心里
FLAG_CORE = 0x04   # 补洞封缝后的核心掩码
FLAG_GHOST = 0x08  # 补出来的格,高度由邻格传播
FLAG_FILL = 0x10   # 补出来的格且无高度可传播

# steps 的位序对应的 4 个规范方向,与烘焙端写入这一列时的顺序一致。
STEP_DX = (1, 0, 1, 1)
STEP_DY = (0, 1, 1, -1)


def grid_clearance(q):
    """净空存的是整数平方格距,还原表达式与 clearance 的返回值逐位相同。"""
    return np.minimum(np.sqrt(np.asarray(q, np.float32)) * np.float32(0.25),
                      np.float32(12.0))


def _varints(buf):
    """一列变长整数一次解完。低 7 位一组小端在前,末字节最高位为 0。"""
    b = np.frombuffer(buf, np.uint8)
    if not len(b):
        return np.zeros(0, np.uint64)
    end = (b & 0x80) == 0
    if not end[-1]:
        raise ValueError("grid varint column is truncated")
    term = np.flatnonzero(end)
    starts = np.concatenate(([0], term[:-1] + 1))
    pos = np.arange(len(b)) - np.repeat(starts, np.diff(np.append(starts, len(b))))
    if pos.max() > 9:
        raise ValueError("grid varint is longer than 64 bits")
    # 各字节的 7 位组互不重叠, 段内按位或等价于段内求和
    contrib = (b & 0x7F).astype(np.uint64) << (7 * pos).astype(np.uint64)
    return np.add.reduceat(contrib, starts)


class GridTile:
    __slots__ = ("regions", "cell", "rid", "h", "clr", "flags", "steps")

    def __init__(self, regions, cell, rid, h, clr, flags, steps):
        self.regions = regions
        self.cell = cell
        self.rid = rid
        self.h = h
        self.clr = clr
        self.flags = flags
        self.steps = steps

    def __len__(self):
        return len(self.cell)


def empty_tile():
    return GridTile(0, np.zeros(0, np.int64), np.zeros(0, np.uint32), np.zeros(0, np.float32),
                    np.zeros(0, np.uint16), np.zeros(0, np.uint8), np.zeros(0, np.uint8))


def decode_tile(raw):
    """解一块瓦的载荷(已解压)。字节走完且自洽才返回。"""
    view = memoryview(raw)
    p = 0

    def varint():
        nonlocal p
        val = shift = 0
        while True:
            if p >= len(view):
                raise ValueError("grid tile header is truncated")
            b = view[p]
            p += 1
            val |= (b & 0x7F) << shift
            if not b & 0x80:
                return val
            shift += 7

    n = varint()
    if n == 0:
        if p != len(view):
            raise ValueError("grid tile has trailing bytes")
        return empty_tile()
    hmin = struct.unpack_from("<f", view, p)[0]
    p += 4
    regions = varint()
    ncell = varint()

    # 六列各自长度前缀,顺序与写入端一致:格、高、类号、标志、断口、净空。
    col = []
    for _ in range(6):
        clen = varint()
        if p + clen > len(view):
            raise ValueError("grid tile column is truncated")
        col.append(view[p:p + clen])
        p += clen
    if p != len(view):
        raise ValueError("grid tile has trailing bytes")

    pair = _varints(col[0])
    if len(pair) != 2 * ncell:
        raise ValueError("grid tile cell column does not match the cell count")
    k = pair[1::2].astype(np.int64)
    if int(k.sum()) != n:
        raise ValueError("grid tile record count does not match the cell column")
    cell = np.repeat(np.cumsum(pair[0::2].astype(np.int64)), k)
    hq = _varints(col[1])
    rid = _varints(col[2])
    clr = _varints(col[5])
    flags = np.frombuffer(col[3], np.uint8)
    steps = np.frombuffer(col[4], np.uint8)
    if len(hq) != n or len(rid) != n or len(clr) != n or len(flags) != n or len(steps) != n:
        raise ValueError("grid tile columns do not match the record count")

    h = (np.float64(hmin) + hq.astype(np.float64) / 64.0).astype(np.float32)
    h[(flags & FLAG_FILL) != 0] = np.float32(0.0)
    return GridTile(int(regions), cell, rid.astype(np.uint32), h,
                    clr.astype(np.uint16), flags, steps)


class GridZone:
    __slots__ = ("name", "x0", "y0", "global_regions", "tiles")

    def __init__(self, name, x0, y0, global_regions, tiles):
        self.name = name
        self.x0 = x0
        self.y0 = y0
        self.global_regions = global_regions
        self.tiles = tiles  # (gx0, gy0, nx, ny, px0, px1, py0, py1, offset, len, records)


class GridPack:
    """包里 GRID 段的目录。段字节归调用方持有,这里只记一个视图。"""

    def __init__(self, data):
        if data is None or len(data) < HEADER.size:
            raise ValueError("GRID section is truncated")
        tag, version, cell_size, tile_px, apron_px, zone_count, _reserved = HEADER.unpack_from(data, 0)
        if tag != TAG:
            raise ValueError("GRID section has a bad magic")
        if version != SECTION_VERSION:
            raise ValueError(f"GRID section version {version} is not supported")
        # 格边长是判据常数,包和运行端对不上就整包不认:两边的格心不重合,烘出来的一切都错位。
        if abs(cell_size - CS) > 1e-12:
            raise ValueError("GRID cell size does not match the runtime grid")
        self.data = memoryview(data)
        self.cell_size = cell_size
        self.tile_px = tile_px
        self.apron_px = apron_px
        self.zones = {}
        cursor = HEADER.size
        for _ in range(zone_count):
            (name_len,) = struct.unpack_from("<I", data, cursor)
            cursor += 4
            padded = (name_len + 3) // 4 * 4
            name = bytes(self.data[cursor:cursor + name_len]).decode("utf-8")
            cursor += padded
            x0, y0, global_regions, tile_count = ZONE_HEAD.unpack_from(data, cursor)
            cursor += ZONE_HEAD.size
            tiles = np.frombuffer(
                self.data[cursor:cursor + tile_count * TILE.size],
                np.dtype([("g", "<i4", 8), ("off", "<u8"), ("len", "<u4"), ("rec", "<u4")]),
                count=tile_count,
            )
            cursor += tile_count * TILE.size
            if tile_count and (
                    (tiles["g"][:, 2] <= 0).any() or (tiles["g"][:, 3] <= 0).any()
                    or (tiles["off"] + tiles["len"] > len(data)).any()):
                raise ValueError(f"GRID tile of zone '{name}' points outside the section")
            self.zones[name] = GridZone(name, x0, y0, global_regions, tiles)

    def zone(self, name):
        return self.zones.get(name)

    def decode(self, tile):
        """解一块瓦:按需 inflate 再解码,不缓存。空瓦返回空表。"""
        if int(tile["rec"]) == 0:
            return empty_tile()
        off = int(tile["off"])
        raw = zlib.decompressobj().decompress(bytes(self.data[off:off + int(tile["len"])]))
        out = decode_tile(raw)
        if len(out) != int(tile["rec"]):
            raise ValueError("grid tile record count does not match the directory")
        return out


def tiles_in_rect(zone, gx0, gy0, gx1, gy1):
    """自有矩形与全局格矩形 [gx0,gx1]×[gy0,gy1] 相交的瓦。"""
    g = zone.tiles["g"].astype(np.int64)
    ox0 = g[:, 0] + g[:, 4]
    ox1 = g[:, 0] + g[:, 5]
    oy0 = g[:, 1] + g[:, 6]
    oy1 = g[:, 1] + g[:, 7]
    hit = ~((ox0 > gx1) | (ox1 < gx0) | (oy0 > gy1) | (oy1 < gy0))
    return zone.tiles[hit]


class GridWindow:
    """窗口里从预烘图解出来的记录。格号已换成窗口格号,窗外的与不属于本瓦
    自有矩形的都已剔除;一格只归一块瓦,所以同格的记录必然来自同一块瓦。"""

    __slots__ = ("cell", "rid", "h", "clr", "flags", "steps")

    def __init__(self, gp, zone, wgx0, wgy0, nx, ny):
        cols = [[] for _ in range(6)]
        for tile in tiles_in_rect(zone, wgx0, wgy0, wgx0 + nx - 1, wgy0 + ny - 1):
            if int(tile["rec"]) == 0:
                continue
            t = gp.decode(tile)
            g = tile["g"]
            ix = t.cell % int(g[2])
            iy = t.cell // int(g[2])
            keep = ((ix >= int(g[4])) & (ix <= int(g[5]))
                    & (iy >= int(g[6])) & (iy <= int(g[7])))
            wx = int(g[0]) + ix - wgx0
            wy = int(g[1]) + iy - wgy0
            keep &= (wx >= 0) & (wx < nx) & (wy >= 0) & (wy < ny)
            if not keep.any():
                continue
            cols[0].append(wy[keep] * nx + wx[keep])
            cols[1].append(t.rid[keep])
            cols[2].append(t.h[keep])
            cols[3].append(t.clr[keep])
            cols[4].append(t.flags[keep])
            cols[5].append(t.steps[keep])
        empty = (np.zeros(0, np.int64), np.zeros(0, np.uint32), np.zeros(0, np.float32),
                 np.zeros(0, np.uint16), np.zeros(0, np.uint8), np.zeros(0, np.uint8))
        self.cell, self.rid, self.h, self.clr, self.flags, self.steps = [
            np.concatenate(c) if c else e for c, e in zip(cols, empty)]

    def __len__(self):
        return len(self.cell)
