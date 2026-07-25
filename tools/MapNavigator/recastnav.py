#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import heapq
import math

import numpy as np

CS = 0.25          # 体素边长 px
CLIMB = 3.0        # 相邻格可连通最大高差 px
MERGE_H = 1.0      # 同列 span 合并容差 px
EDT_CAP = 12.0     # 距离场截断 px
R = 1.75           # 期望余量上限 px
REL = 0.6          # 期望余量 = min(R, REL×局部净空)
LAM = 1.5          # 满亏欠一步加价倍数
RIDGEF = 0.5       # 脊线保底余量地板 px
MAXERR = 0.5       # 轮廓 DP 容差 px
SLIMEPS = 0.5      # 终线共线剔除容差 px
TAU = 1.0          # 贴墙诊断阈 px
CAP = 12.0         # wall_dist 截断 px
MC_HBAND = 8.0     # 层高度带(墙筛/盖章)px
H_BAND = 6.0       # 真墙探针高度带 px
EPS_PROBE = 0.75   # 真墙探针距离 px
SNAP_RADIUS = 8.0  # 起终点吸附半径 px
MARGIN = 25.0      # 窗口外扩 px
HOLE_MAX = max(1, int(round(2.0 / (CS * CS))))  # 封闭小洞填充上限(格 = 2px²)
MAX_CELLS = 30_000_000

_NB8 = [(1, 0, 1.0), (-1, 0, 1.0), (0, 1, 1.0), (0, -1, 1.0),
        (1, 1, math.sqrt(2)), (1, -1, math.sqrt(2)),
        (-1, 1, math.sqrt(2)), (-1, -1, math.sqrt(2))]


def rasterize(V, H, T, ox, oy, nx, ny, cs=CS, chunk=2_000_000):
    A, B, C = V[T[:, 0]], V[T[:, 1]], V[T[:, 2]]
    HA, HB, HC = H[T[:, 0]], H[T[:, 1]], H[T[:, 2]]

    fx = np.stack([A[:, 0], B[:, 0], C[:, 0]], 1)
    fy = np.stack([A[:, 1], B[:, 1], C[:, 1]], 1)
    ix0 = np.floor((fx.min(1) - ox) / cs).astype(np.int64)
    ix1 = np.floor((fx.max(1) - ox) / cs).astype(np.int64)
    iy0 = np.floor((fy.min(1) - oy) / cs).astype(np.int64)
    iy1 = np.floor((fy.max(1) - oy) / cs).astype(np.int64)
    keep = (ix1 >= 0) & (ix0 < nx) & (iy1 >= 0) & (iy0 < ny)
    if not keep.any():
        return (np.zeros(0, np.int64), np.zeros(0, np.float32),
                np.zeros(0, bool))
    ix0 = np.clip(ix0[keep], 0, nx - 1); ix1 = np.clip(ix1[keep], 0, nx - 1)
    iy0 = np.clip(iy0[keep], 0, ny - 1); iy1 = np.clip(iy1[keep], 0, ny - 1)
    A, B, C = A[keep], B[keep], C[keep]
    HA, HB, HC = HA[keep], HB[keep], HC[keep]

    w = (ix1 - ix0 + 1); h = (iy1 - iy0 + 1); cnt = w * h
    cum = np.cumsum(cnt)
    cells, hts, ins = [], [], []

    lo = 0
    while lo < len(cnt):
        hi = int(np.searchsorted(cum, (cum[lo - 1] if lo else 0) + chunk)) + 1
        hi = min(max(hi, lo + 1), len(cnt))
        sl = slice(lo, hi)
        c_ = cnt[sl]; tot = int(c_.sum())
        tid = np.repeat(np.arange(lo, hi), c_)
        base = np.repeat(np.concatenate(([0], np.cumsum(c_)[:-1])), c_)
        k = np.arange(tot) - base
        wr = np.repeat(w[sl], c_)
        gx = np.repeat(ix0[sl], c_) + k % wr
        gy = np.repeat(iy0[sl], c_) + k // wr
        px = ox + (gx + 0.5) * cs
        py = oy + (gy + 0.5) * cs

        a = A[tid]; b = B[tid]; c = C[tid]
        ctr = np.stack([px, py], 1)
        hcs = cs * 0.5
        v = [a - ctr, b - ctr, c - ctr]
        ok = np.ones(tot, bool)
        for ax in (0, 1):
            lo_ = np.minimum(np.minimum(v[0][:, ax], v[1][:, ax]), v[2][:, ax])
            hi_ = np.maximum(np.maximum(v[0][:, ax], v[1][:, ax]), v[2][:, ax])
            ok &= (lo_ <= hcs) & (hi_ >= -hcs)
        for i in range(3):
            e = v[(i + 1) % 3] - v[i]
            n0, n1 = -e[:, 1], e[:, 0]
            p = np.stack([v[j][:, 0] * n0 + v[j][:, 1] * n1
                          for j in range(3)], 1)
            rad = hcs * (np.abs(n0) + np.abs(n1))
            ok &= (p.min(1) <= rad) & (p.max(1) >= -rad)
        if ok.any():
            k2 = np.nonzero(ok)[0]
            e1 = b[k2] - a[k2]; e2 = c[k2] - a[k2]; q = ctr[k2] - a[k2]
            den = e1[:, 0] * e2[:, 1] - e1[:, 1] * e2[:, 0]
            den = np.where(np.abs(den) < 1e-12, 1e-12, den)
            t_ = (q[:, 0] * e2[:, 1] - q[:, 1] * e2[:, 0]) / den
            s_ = (e1[:, 0] * q[:, 1] - e1[:, 1] * q[:, 0]) / den
            inside = (t_ >= -1e-12) & (s_ >= -1e-12) & (t_ + s_ <= 1 + 1e-12)
            t_ = np.clip(t_, 0.0, 1.0); s_ = np.clip(s_, 0.0, 1.0 - t_)
            i2 = tid[k2]
            hz = HA[i2] + t_ * (HB[i2] - HA[i2]) + s_ * (HC[i2] - HA[i2])
            cells.append(gy[k2] * nx + gx[k2])
            hts.append(hz.astype(np.float32))
            ins.append(inside)
        lo = hi

    cx = (A[:, 0] + B[:, 0] + C[:, 0]) / 3.0
    cy = (A[:, 1] + B[:, 1] + C[:, 1]) / 3.0
    gx = np.clip(((cx - ox) / cs).astype(np.int64), 0, nx - 1)
    gy = np.clip(((cy - oy) / cs).astype(np.int64), 0, ny - 1)
    inb = (cx >= ox) & (cx < ox + nx * cs) & (cy >= oy) & (cy < oy + ny * cs)
    cells.append(gy[inb] * nx + gx[inb])
    hts.append(((HA + HB + HC) / 3.0)[inb].astype(np.float32))
    ins.append(np.zeros(int(inb.sum()), bool))

    return np.concatenate(cells), np.concatenate(hts), np.concatenate(ins)


def spans(cell, hz, merge_h=MERGE_H):
    o = np.lexsort((hz, cell))
    cell = cell[o]; hz = hz[o]
    new = np.empty(len(cell), bool); new[0] = True
    new[1:] = (cell[1:] != cell[:-1]) | (hz[1:] - hz[:-1] > merge_h)
    sid = np.cumsum(new) - 1
    n = int(sid[-1]) + 1
    cntv = np.bincount(sid, minlength=n)
    sp_h = (np.bincount(sid, weights=hz.astype(np.float64), minlength=n)
            / cntv).astype(np.float32)
    sp_cell = cell[new]
    occ, cstart, ccnt = np.unique(sp_cell, return_index=True,
                                  return_counts=True)
    return sp_cell, sp_h, occ, cstart, ccnt


def dense_k(sp_h, occ, cstart, ccnt):
    K = int(ccnt.max())
    n = len(occ)
    HK = np.full((n, K), np.inf, np.float32)
    IK = np.full((n, K), -1, np.int64)
    rank = np.arange(len(sp_h)) - np.repeat(cstart, ccnt)
    ci = np.repeat(np.arange(n), ccnt)
    HK[ci, rank] = sp_h
    IK[ci, rank] = np.arange(len(sp_h))
    return HK, IK, ci


def seam_bridge(cell, hz, nx, ny, cs=CS, climb=CLIMB):
    kb = int(round(0.5 / cs)) - 1
    if kb <= 0 or not len(cell):
        return np.zeros(0, np.int64), np.zeros(0, np.float32)
    sp_cell, sp_h, occ, cstart, ccnt = spans(cell, hz)
    HK, IK, _ = dense_k(sp_h, occ, cstart, ccnt)
    K = HK.shape[1]
    O2 = np.zeros(nx * ny, bool); O2[occ] = True
    O2 = O2.reshape(ny, nx)
    E = ~O2
    idx2 = np.arange(nx * ny).reshape(ny, nx)
    add_c, add_h = [], []
    for dy, dx in ((0, 1), (1, 0)):
        for dl in range(1, kb + 1):
            for dr in range(1, kb + 2 - dl):
                m = E.copy()
                for i in range(1, dl):
                    m &= _sh(E, i * dy, i * dx)
                for i in range(1, dr):
                    m &= _sh(E, -i * dy, -i * dx)
                m &= _sh(O2, dl * dy, dl * dx)
                m &= _sh(O2, -dr * dy, -dr * dx)
                if not m.any():
                    continue
                cid = idx2[m]
                a = cid + dl * (dy * nx + dx)
                b = cid - dr * (dy * nx + dx)
                ja = np.searchsorted(occ, a)
                jb = np.searchsorted(occ, b)
                # 空槽 inf-inf=nan 会毒化 argmin,两侧用相反哨兵
                ha = np.where(np.isfinite(HK[ja]), HK[ja], np.float32(1e9))
                hb = np.where(np.isfinite(HK[jb]), HK[jb], np.float32(-1e9))
                dh = np.abs(ha[:, :, None] - hb[:, None, :])
                fl = dh.reshape(len(cid), -1)
                best = fl.argmin(1)
                ok = fl[np.arange(len(cid)), best] <= climb
                if not ok.any():
                    continue
                p, q = best // K, best % K
                hm = (ha[np.arange(len(cid)), p]
                      + hb[np.arange(len(cid)), q]) * np.float32(0.5)
                add_c.append(cid[ok]); add_h.append(hm[ok])
    if not add_c:
        return np.zeros(0, np.int64), np.zeros(0, np.float32)
    return np.concatenate(add_c), np.concatenate(add_h).astype(np.float32)


def flood(seed, sp_h, occ, HK, IK, sp_ci, nx, climb=CLIMB):
    vis = np.zeros(len(sp_h), bool)
    vis[seed] = True
    F = np.array([seed], np.int64)
    while len(F):
        nxt = []
        cid = occ[sp_ci[F]]
        gx = cid % nx
        for dx, dy in ((1, 0), (-1, 0), (0, 1), (0, -1)):
            tgt = cid + dy * nx + dx
            good = np.ones(len(F), bool)
            if dx:
                good &= (gx + dx >= 0) & (gx + dx < nx)
            j = np.searchsorted(occ, tgt)
            good &= (j < len(occ))
            jj = np.where(good, np.minimum(j, len(occ) - 1), 0)
            good &= occ[jj] == tgt
            if not good.any():
                continue
            src = F[good]; jj = jj[good]
            m = np.abs(HK[jj] - sp_h[src][:, None]) <= climb
            cand = IK[jj][m]
            cand = cand[cand >= 0]
            cand = cand[~vis[cand]]
            if len(cand):
                cand = np.unique(cand)
                vis[cand] = True
                nxt.append(cand)
        F = np.concatenate(nxt) if nxt else np.zeros(0, np.int64)
    return vis


def clearance(mask, cs=CS, cap=EDT_CAP):
    ny, nx = mask.shape
    Rw = int(np.ceil(cap / cs)) + 1
    BIG = np.float32(Rw * 4)
    idx = np.arange(ny, dtype=np.float32)[:, None]
    obst = ~mask
    neg = np.where(obst, idx, np.float32(-1e9))
    up = idx - np.maximum.accumulate(neg, 0)
    pos = np.where(obst, idx, np.float32(1e9))
    dn = np.minimum.accumulate(pos[::-1], 0)[::-1] - idx
    g = np.minimum(np.minimum(up, dn), BIG).astype(np.float32)
    g2 = g * g

    best = g2.copy()
    for k in range(1, Rw + 1):
        kk = np.float32(k * k)
        if kk >= BIG * BIG:
            break
        sh = np.full_like(g2, BIG * BIG)
        sh[:, k:] = g2[:, :-k]
        np.minimum(best, sh + kk, out=best)
        sh = np.full_like(g2, BIG * BIG)
        sh[:, :-k] = g2[:, k:]
        np.minimum(best, sh + kk, out=best)
    d = np.sqrt(best) * cs
    return np.minimum(d, cap).astype(np.float32) * mask


def stamp_walls(P0, P1, HH, ox, oy, nx, ny, hgrid, cs=CS, hband=MC_HBAND):
    P0 = np.asarray(P0, float); P1 = np.asarray(P1, float)
    occ, HK, IK, n_span = hgrid
    blocked = np.zeros(n_span, bool)
    if not len(P0):
        return blocked
    L = np.hypot(*(P1 - P0).T)
    steps = np.maximum(np.ceil(L / (cs * 0.4)).astype(np.int64), 1) + 1
    tid = np.repeat(np.arange(len(P0)), steps)
    base = np.repeat(np.concatenate(([0], np.cumsum(steps)[:-1])), steps)
    k = np.arange(int(steps.sum())) - base
    t = k / np.maximum(np.repeat(steps, steps) - 1, 1)
    S = P0[tid] + (P1[tid] - P0[tid]) * t[:, None]
    gx = np.floor((S[:, 0] - ox) / cs).astype(np.int64)
    gy = np.floor((S[:, 1] - oy) / cs).astype(np.int64)
    ok = (gx >= 0) & (gx < nx) & (gy >= 0) & (gy < ny)
    cid = gy[ok] * nx + gx[ok]
    hh = np.asarray(HH, float)[tid[ok]]
    j = np.searchsorted(occ, cid)
    good = (j < len(occ))
    jj = np.where(good, np.minimum(j, len(occ) - 1), 0)
    good &= occ[jj] == cid
    jj = jj[good]; hh = hh[good]
    if not len(jj):
        return blocked
    hit = np.abs(HK[jj] - hh[:, None]) <= hband
    sid = IK[jj][hit]
    blocked[sid[sid >= 0]] = True
    return blocked


def walls_at_layer(P0, P1, HH, lh, ox, oy, nx, ny, cs=CS, hband=MC_HBAND):
    P0 = np.asarray(P0, float); P1 = np.asarray(P1, float)
    keep = np.zeros(len(P0), bool)
    if not len(P0):
        return keep
    L = np.hypot(*(P1 - P0).T)
    steps = np.maximum(np.ceil(L / (cs * 0.4)).astype(np.int64), 1) + 1
    tid = np.repeat(np.arange(len(P0)), steps)
    base = np.repeat(np.concatenate(([0], np.cumsum(steps)[:-1])), steps)
    t = (np.arange(int(steps.sum())) - base) / np.maximum(
        np.repeat(steps, steps) - 1, 1)
    S = P0[tid] + (P1[tid] - P0[tid]) * t[:, None]
    gx = np.floor((S[:, 0] - ox) / cs).astype(np.int64)
    gy = np.floor((S[:, 1] - oy) / cs).astype(np.int64)
    ok = (gx >= 0) & (gx < nx) & (gy >= 0) & (gy < ny)
    h = lh.ravel()[np.where(ok, gy * nx + gx, 0)]
    hit = ok & ~np.isnan(h) & (np.abs(h - np.asarray(HH, float)[tid]) <= hband)
    keep[np.unique(tid[hit])] = True
    return keep


def wall_index(P0, P1, ox, oy, nx, ny, cs=CS, sub=0.2):
    P0 = np.asarray(P0, float); P1 = np.asarray(P1, float)
    start = np.zeros(nx * ny + 1, np.int64)
    if not len(P0):
        return np.zeros(0, np.int64), start
    L = np.hypot(*(P1 - P0).T)
    steps = np.maximum(np.ceil(L / (cs * sub)).astype(np.int64), 1) + 1
    tid = np.repeat(np.arange(len(P0)), steps)
    base = np.repeat(np.concatenate(([0], np.cumsum(steps)[:-1])), steps)
    t = (np.arange(int(steps.sum())) - base) / np.maximum(
        np.repeat(steps, steps) - 1, 1)
    S = P0[tid] + (P1[tid] - P0[tid]) * t[:, None]
    gx = np.floor((S[:, 0] - ox) / cs).astype(np.int64)
    gy = np.floor((S[:, 1] - oy) / cs).astype(np.int64)
    ok = (gx >= 0) & (gx < nx) & (gy >= 0) & (gy < ny)
    key = np.unique((gy[ok] * nx + gx[ok]) * (len(P0) + 1) + tid[ok])
    cid, wid = key // (len(P0) + 1), key % (len(P0) + 1)
    start[1:] = np.cumsum(np.bincount(cid, minlength=nx * ny))
    return wid, start


def banned_steps(free, wid, start, P0, P1, ox, oy, nx, cs=CS):
    ny = free.shape[0]
    P0 = np.asarray(P0, float); P1 = np.asarray(P1, float)
    flat = free.ravel()
    has = np.nonzero((start[1:] > start[:-1]) & flat)[0]
    out = set()
    if not len(has) or not len(P0):
        return out
    cx = (has % nx + 0.5) * cs + ox
    cy = (has // nx + 0.5) * cs + oy
    for dx, dy in ((1, 0), (-1, 0), (0, 1), (0, -1),
                   (1, 1), (1, -1), (-1, 1), (-1, -1)):
        ax, ay = has % nx + dx, has // nx + dy
        m = (ax >= 0) & (ax < nx) & (ay >= 0) & (ay < ny)
        m &= flat[np.where(m, ay * nx + ax, 0)]
        if not m.any():
            continue
        a, b = has[m], (ay * nx + ax)[m]
        p = np.stack([cx[m], cy[m]], 1)
        q = p + np.array([dx, dy], float) * cs
        ca, cb = start[a + 1] - start[a], start[b + 1] - start[b]
        pid = np.concatenate([np.repeat(np.arange(len(a)), ca),
                              np.repeat(np.arange(len(a)), cb)])
        gw = np.concatenate([
            _csr_gather(wid, start, a), _csr_gather(wid, start, b)])
        A, B = P0[gw], P1[gw]
        r = (q - p)[pid]; s = B - A; u = A - p[pid]
        den = r[:, 0] * s[:, 1] - r[:, 1] * s[:, 0]
        ok = np.abs(den) > 1e-12
        dd = np.where(ok, den, 1.0)
        t = (u[:, 0] * s[:, 1] - u[:, 1] * s[:, 0]) / dd
        w = (u[:, 0] * r[:, 1] - u[:, 1] * r[:, 0]) / dd
        hit = ok & (t > 1e-9) & (t < 1 - 1e-9) & (w > -1e-9) & (w < 1 + 1e-9)
        for k in np.unique(pid[hit]):
            out.add((int(a[k]), int(b[k]))); out.add((int(b[k]), int(a[k])))
    return out


def _csr_gather(wid, start, cells):
    cnt = start[cells + 1] - start[cells]
    if not cnt.sum():
        return np.zeros(0, np.int64)
    base = np.repeat(start[cells], cnt)
    off = np.arange(cnt.sum()) - np.repeat(
        np.concatenate(([0], np.cumsum(cnt)[:-1])), cnt)
    return wid[base + off]


def comps4(mask):
    ny, nx = mask.shape
    lab = np.where(mask, np.arange(ny * nx).reshape(ny, nx), -1)
    for _ in range(8000):
        prev = lab.copy()
        for sh, ax in ((1, 0), (-1, 0), (1, 1), (-1, 1)):
            n = np.roll(lab, sh, axis=ax)
            if ax == 0:
                n[0 if sh > 0 else -1, :] = -1
            else:
                n[:, 0 if sh > 0 else -1] = -1
            m = mask & (n >= 0) & ((lab > n) | (lab < 0))
            lab[m] = n[m]
        lab[~mask] = -1
        if np.array_equal(lab, prev):
            break
    return lab


def fill_holes(mask, max_cells, protect=None):
    lab = comps4(~mask)
    out = mask.copy()
    edge = set(lab[0, :].tolist()) | set(lab[-1, :].tolist()) \
        | set(lab[:, 0].tolist()) | set(lab[:, -1].tolist())
    u, c = np.unique(lab[lab >= 0], return_counts=True)
    for cid, cnt in zip(u.tolist(), c.tolist()):
        if cid in edge or cnt > max_cells:
            continue
        if protect is not None and protect[lab == cid].any():
            continue
        out[lab == cid] = True
    return out


def _sh(m, dy, dx):
    ny, nx = m.shape
    out = np.zeros_like(m)
    out[max(0, -dy):ny + min(0, -dy), max(0, -dx):nx + min(0, -dx)] = \
        m[max(0, dy):ny + min(0, dy), max(0, dx):nx + min(0, dx)]
    return out


def close_cracks(core, lay, protect=None):
    k = max(1, int(round(0.5 / CS)))
    out = core
    for _ in range(4):
        thin = np.zeros_like(out)
        for dy, dx in ((1, 0), (0, 1)):
            a = np.zeros_like(out); b = np.zeros_like(out)
            for i in range(1, k + 1):
                a |= _sh(out, i * dy, i * dx)
                b |= _sh(out, -i * dy, -i * dx)
            thin |= a & b
        add = ~out & thin & lay
        if protect is not None:
            add &= ~protect
        if not add.any():
            break
        out = out | add
    return out


def cost_astar(mask, s, g, mult, banned=None, bnp=None):
    ny, nx = mask.shape
    if not mask[s[1], s[0]] or not mask[g[1], g[0]]:
        return None
    bn = banned or ()
    dist = np.full(mask.shape, np.inf)
    prev = np.full(mask.shape, -1, np.int64)
    dist[s[1], s[0]] = 0.0
    pq = [(0.0, s[0], s[1])]
    while pq:
        f, x, y = heapq.heappop(pq)
        d0 = dist[y, x]
        if f > d0 + math.hypot(g[0] - x, g[1] - y) + 1e-9:
            continue
        if (x, y) == g:
            break
        m0 = mult[y, x]
        for dx, dy, w in _NB8:
            a, b = x + dx, y + dy
            if not (0 <= a < nx and 0 <= b < ny) or not mask[b, a]:
                continue
            if dx and dy and not (mask[y, a] and mask[b, x]):
                continue
            pen = 0.0
            if (y * nx + x, b * nx + a) in bn:
                if bnp is None:
                    continue
                pen = bnp
            nd = d0 + w * 0.5 * (m0 + mult[b, a]) + pen
            if nd < dist[b, a] - 1e-12:
                dist[b, a] = nd
                prev[b, a] = y * nx + x
                heapq.heappush(pq, (nd + math.hypot(g[0] - a, g[1] - b), a, b))
    if not np.isfinite(dist[g[1], g[0]]):
        return None
    out = [g]
    x, y = g
    while (x, y) != s:
        p = prev[y, x]
        x, y = int(p % nx), int(p // nx)
        out.append((x, y))
    return out[::-1]


def local_max(a, k):
    m = a
    for ax in (0, 1):
        acc = m.copy()
        for s in range(1, k + 1):
            for sgn in (1, -1):
                sh = np.zeros_like(m)
                sl_dst = slice(s, None) if sgn > 0 else slice(None, -s)
                sl_src = slice(None, -s) if sgn > 0 else slice(s, None)
                if ax == 0:
                    sh[sl_dst, :] = m[sl_src, :]
                else:
                    sh[:, sl_dst] = m[:, sl_src]
                acc = np.maximum(acc, sh)
        m = acc
    return m


def _shf(a, dy, dx):
    o = np.full_like(a, -np.inf)
    H, W = a.shape
    ys, yd = (slice(dy, H), slice(0, H - dy)) if dy >= 0 else \
             (slice(0, H + dy), slice(-dy, H))
    xs, xd = (slice(dx, W), slice(0, W - dx)) if dx >= 0 else \
             (slice(0, W + dx), slice(-dx, W))
    o[yd, xd] = a[ys, xs]
    return o


def pref_field(dist, ridge=False):
    locw = local_max(dist, int(math.ceil(R / CS)))
    pref = np.maximum(np.minimum(R, REL * locw) if REL > 0
                      else np.full_like(dist, R), 0.25)
    if not ridge:
        return pref
    rg = np.zeros(dist.shape, bool)
    for dy, dx in ((0, 1), (1, 0), (1, 1), (1, -1)):
        a, b = _shf(dist, dy, dx), _shf(dist, -dy, -dx)
        rg |= (dist >= np.maximum(a, b)) & (dist > np.minimum(a, b))
    rg &= dist >= RIDGEF
    return np.where(rg, np.minimum(pref, dist), pref)


def slim(pts, blk, eps=SLIMEPS):
    P = [tuple(p) for p in pts]
    ch = True
    while ch:
        ch = False
        i = 1
        while i < len(P) - 1:
            a, b, c = P[i - 1], P[i], P[i + 1]
            ux, uy = c[0] - a[0], c[1] - a[1]
            L2 = ux * ux + uy * uy
            t = 0.0 if L2 == 0 else max(0.0, min(1.0, (
                (b[0] - a[0]) * ux + (b[1] - a[1]) * uy) / L2))
            d = math.hypot(b[0] - a[0] - t * ux, b[1] - a[1] - t * uy)
            if d <= eps and not blk.blocked(a, c):
                P.pop(i)
                ch = True
            else:
                i += 1
    return P


_SIDES = (
    (1, 0, (1, 0), (1, 1)),
    (0, 1, (1, 1), (0, 1)),
    (-1, 0, (0, 1), (0, 0)),
    (0, -1, (0, 0), (1, 0)),
)


def trace_contours(mask):
    ny, nx = mask.shape
    W = nx + 1
    nxt = {}
    pad = np.zeros((ny + 2, nx + 2), bool)
    pad[1:-1, 1:-1] = mask
    for dx, dy, a, b in _SIDES:
        blocked = mask & ~pad[1 + dy:ny + 1 + dy, 1 + dx:nx + 1 + dx]
        ys, xs = np.nonzero(blocked)
        for x, y in zip(xs.tolist(), ys.tolist()):
            u = (y + a[1]) * W + (x + a[0])
            v = (y + b[1]) * W + (x + b[0])
            nxt.setdefault(u, []).append(v)

    loops = []
    used = set()
    for u0, vs in nxt.items():
        for v0 in vs:
            if (u0, v0) in used:
                continue
            loop = []
            u, v = u0, v0
            while True:
                used.add((u, v))
                loop.append(u)
                cand = nxt.get(v)
                if not cand:
                    break
                if len(cand) == 1:
                    w = cand[0]
                else:
                    d = (v % W - u % W, v // W - u // W)
                    order = {(d[1], -d[0]): 0, d: 1, (-d[1], d[0]): 2,
                             (-d[0], -d[1]): 3}
                    w = min(cand, key=lambda z: order.get(
                        (z % W - v % W, z // W - v // W), 9))
                if (v, w) in used:
                    break
                u, v = v, w
            if len(loop) >= 4:
                loops.append(np.array([(p % W, p // W) for p in loop],
                                      np.float64))
    return loops


def _dp_split(P, i0, i1, max_err):
    n = len(P)
    a, b = P[i0], P[i1]
    d = b - a
    L2 = float(d @ d)
    best, bi = max_err * max_err, -1
    i = (i0 + 1) % n
    while i != i1:
        q = P[i] - a
        if L2 > 1e-12:
            t = float(np.clip((q @ d) / L2, 0.0, 1.0))
            e = q - d * t
        else:
            e = q
        dd = float(e @ e)
        if dd > best:
            best, bi = dd, i
        i = (i + 1) % n
    return bi


def simplify_loop(P, max_err):
    n = len(P)
    if n <= 4:
        return P
    ll = int(np.lexsort((P[:, 1], P[:, 0]))[0])
    ur = int(np.lexsort((P[:, 1], P[:, 0]))[-1])
    if ll == ur:
        return P
    keep = [ll, ur]
    i = 0
    while i < len(keep):
        a, b = keep[i], keep[(i + 1) % len(keep)]
        bi = _dp_split(P, a, b, max_err)
        if bi >= 0:
            keep.insert(i + 1, bi)
        else:
            i += 1
    return P[keep]


class Blockers:

    def __init__(self, loops, extra=None, on=None):
        self.on = on
        A, B = [], []
        for P in loops:
            A.append(P)
            B.append(np.roll(P, -1, axis=0))
        if extra is not None and len(extra[0]):
            A.append(np.asarray(extra[0], float))
            B.append(np.asarray(extra[1], float))
        self.A = np.vstack(A) if A else np.zeros((0, 2))
        self.B = np.vstack(B) if B else np.zeros((0, 2))
        self.lo = np.minimum(self.A, self.B)
        self.hi = np.maximum(self.A, self.B)

    def blocked(self, p, q, eps=1e-7):
        p = np.asarray(p, float); q = np.asarray(q, float)
        lo = np.minimum(p, q) - eps; hi = np.maximum(p, q) + eps
        m = ((self.hi[:, 0] >= lo[0]) & (self.lo[:, 0] <= hi[0])
             & (self.hi[:, 1] >= lo[1]) & (self.lo[:, 1] <= hi[1]))
        if not m.any():
            return self._off(p, q)
        A, B = self.A[m], self.B[m]
        r = q - p; s = B - A
        den = r[0] * s[:, 1] - r[1] * s[:, 0]
        u = A - p
        ok = np.abs(den) > 1e-12
        t = np.where(ok, (u[:, 0] * s[:, 1] - u[:, 1] * s[:, 0])
                     / np.where(ok, den, 1), -1.0)
        w = np.where(ok, (u[:, 0] * r[1] - u[:, 1] * r[0])
                     / np.where(ok, den, 1), -1.0)
        if bool((ok & (t > eps) & (t < 1 - eps)
                 & (w > eps) & (w < 1 - eps)).any()):
            return True
        return self._off(p, q)

    def _off(self, p, q):
        if self.on is None:
            return False
        msk, x0, y0, cs = self.on
        L = float(np.hypot(q[0] - p[0], q[1] - p[1]))
        n = int(L / (cs * 0.5)) + 2
        t = np.linspace(0.0, 1.0, n)
        gx = ((p[0] + (q[0] - p[0]) * t - x0) / cs).astype(np.int64)
        gy = ((p[1] + (q[1] - p[1]) * t - y0) / cs).astype(np.int64)
        inb = ((gx >= 0) & (gy >= 0)
               & (gx < msk.shape[1]) & (gy < msk.shape[0]))
        if not inb.all():
            return True
        return not bool(msk[gy, gx].all())


def string_pull(pts, blk, rounds=6):
    P = [tuple(map(float, p)) for p in pts]
    for _ in range(rounds):
        out = [P[0]]
        i = 0
        while i < len(P) - 1:
            j = len(P) - 1
            while j > i + 1 and blk.blocked(P[i], P[j]):
                j -= 1
            out.append(P[j])
            i = j
        changed = len(out) != len(P)
        P = out
        if not changed:
            break
    return P


def drop_loops(P, eps=1e-9):
    P = [tuple(map(float, p)) for p in P]
    changed = True
    while changed and len(P) > 3:
        changed = False
        for i in range(len(P) - 1):
            for j in range(i + 2, len(P) - 1):
                a, b, c, d = P[i], P[i + 1], P[j], P[j + 1]
                r = (b[0] - a[0], b[1] - a[1])
                s = (d[0] - c[0], d[1] - c[1])
                den = r[0] * s[1] - r[1] * s[0]
                if abs(den) < eps:
                    continue
                t = ((c[0] - a[0]) * s[1] - (c[1] - a[1]) * s[0]) / den
                u = ((c[0] - a[0]) * r[1] - (c[1] - a[1]) * r[0]) / den
                if not (eps < t < 1 - eps and eps < u < 1 - eps):
                    continue
                x = (a[0] + r[0] * t, a[1] + r[1] * t)
                P = P[:i + 1] + [x] + P[j + 1:]
                changed = True
                break
            if changed:
                break
    return P
