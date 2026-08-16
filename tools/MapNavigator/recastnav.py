#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import heapq
import math

import numpy as np

CS = 0.25          # 体素边长 px
CLIMB = 3.0        # 相邻格可连通最大高差 px
SLOPE = 1.0        # 可攀爬坡度上限 tanθ, 抬升超过水平位移的这个倍数即立面, 只能绕行
UP = SLOPE * CS    # 正交相邻格允许的抬升 px, 斜向按实际水平位移等比放大
QH = 1.0           # 体素取样高差容差 px, 需装下斜面单格起伏与格心取样偏差
EDT_CAP = 12.0     # 距离场截断 px
R = 1.75           # 期望余量上限 px
GEO_R = 3.5        # 几何口径舒适余量上限 px
REL = 0.6          # 期望余量 = min(R, REL×局部净空)
LAM = 4.0          # 按局部通道目标计亏欠的满亏欠一步加价倍数
LAM_R = 28.0       # 按固定余量目标 R 计亏欠的满亏欠一步加价倍数
RIDGEF = 0.5       # 脊线保底余量地板 px
MAXERR = 0.5       # 轮廓 DP 容差 px
SLIMEPS = 0.5      # 终线共线剔除容差 px
CLRTOL = 0.125     # 拉直允许的净空退让 px, 取半格即采样步长
CORNER_R = 1.75    # 过角期望余量 px
CORNER_TURN = 5.0  # 需要留过角余量的最小转角 度
CORNER_SEG = 2.0   # 认定为拐点的最小相邻段长 px
CORNER_MAX = 4.0   # 拐点外挪上限 px
CORNER_STEP = 0.5  # 拐点外挪步长 px
CORNER_DIRS = 32   # 拐点外挪候选方向数
CORNER_ROUNDS = 3  # 拐点外挪迭代轮数
COSTTOL = 1e-9     # 代价判据相对容差, 容纳共线子路径的浮点求和差
TAU = 1.0          # 贴墙诊断阈 px
CAP = 12.0         # wall_dist 截断 px
MC_HBAND = 8.0     # 层高度带(墙筛/盖章)px
H_BAND = 6.0       # 真墙探针高度带 px
EPS_PROBE = 0.75   # 真墙探针距离 px
SNAP_RADIUS = 8.0  # 起终点吸附半径 px
DECK_BAND = 2.0    # 声明面高度匹配容差 px, 需远小于相邻面间距
MARGIN = 25.0      # 窗口外扩 px
MAX_CELLS = 30_000_000

_NB8 = [(1, 0, 1.0), (-1, 0, 1.0), (0, 1, 1.0), (0, -1, 1.0),
        (1, 1, math.sqrt(2)), (1, -1, math.sqrt(2)),
        (-1, 1, math.sqrt(2)), (-1, -1, math.sqrt(2))]


def dense_k(sp_h, occ, cstart, ccnt):
    K = int(ccnt.max())
    n = len(occ)
    HK = np.full((n, K), np.inf, np.float32)
    IK = np.full((n, K), -1, np.int32)
    rank = np.arange(len(sp_h)) - np.repeat(cstart, ccnt)
    ci = np.repeat(np.arange(n, dtype=np.int32), ccnt).astype(np.int32)
    HK[ci, rank] = sp_h
    IK[ci, rank] = np.arange(len(sp_h))
    return HK, IK, ci


def span_reach(seed, sp_h, occ, HK, IK, sp_ci, ok, nx, ny,
               up=UP, climb=CLIMB):
    vis = np.zeros(len(sp_h), bool)
    if not ok[seed]:
        return vis
    vis[seed] = True
    F = np.array([seed], np.int64)
    while len(F):
        nxt = []
        cid = occ[sp_ci[F]]
        gx, gy = cid % nx, cid // nx
        for dx, dy, w in _NB8:
            ax, ay = gx + dx, gy + dy
            good = (ax >= 0) & (ax < nx) & (ay >= 0) & (ay < ny)
            tgt = np.where(good, ay * nx + ax, 0)
            j = np.searchsorted(occ, tgt)
            good &= j < len(occ)
            jj = np.where(good, np.minimum(j, len(occ) - 1), 0)
            good &= occ[jj] == tgt
            if not good.any():
                continue
            src = F[good]
            jj = jj[good]
            with np.errstate(invalid="ignore"):
                dh = HK[jj] - sp_h[src][:, None]
                m = (dh <= up * w) & (dh >= -climb)
            cand = IK[jj][m]
            cand = cand[cand >= 0]
            cand = cand[ok[cand] & ~vis[cand]]
            if len(cand):
                cand = np.unique(cand)
                vis[cand] = True
                nxt.append(cand)
        F = np.concatenate(nxt) if nxt else np.zeros(0, np.int64)
    return vis


def span_astar(ok, sp_h, occ, HK, IK, sp_ci, cidx, ok2, s, gset, mult, nx, ny,
               banned=None, bnp=None, forbidden=None, up=UP, climb=CLIMB):
    if not ok[s] or not gset:
        return None
    K = HK.shape[1]
    hk = HK.ravel()
    ik = IK.ravel()
    bn = banned or ()
    fb = forbidden or ()
    mf = mult.ravel()
    nb8 = _NB8
    heappop = heapq.heappop
    heappush = heapq.heappush
    hypot = math.hypot
    gc = int(occ[sp_ci[next(iter(gset))]])
    gxx, gyy = gc % nx, gc // nx
    NC = nx * ny
    dist = np.full(len(sp_h), np.inf)
    prev = np.full(len(sp_h), -1, np.int32)
    dist[s] = 0.0
    pq = [(0.0, s)]
    hit = -1
    while pq:
        f, u = heappop(pq)
        d0 = dist[u]
        cu = int(occ[sp_ci[u]])
        x, y = cu % nx, cu // nx
        if f > d0 + hypot(gxx - x, gyy - y) + 1e-9:
            continue
        if u in gset:
            hit = u
            break
        hu = sp_h[u]
        m0 = mf[cu]
        base_eid = cu * NC
        for dx, dy, w in nb8:
            a, b = x + dx, y + dy
            if not (0 <= a < nx and 0 <= b < ny):
                continue
            cv = b * nx + a
            if not ok2[cv]:
                continue
            if dx and dy and not (ok2[y * nx + a] and ok2[b * nx + x]):
                continue
            j = cidx[cv]
            if j < 0:
                continue
            eid = base_eid + cv
            if eid in fb:
                continue
            pen = 0.0
            if eid in bn:
                if bnp is None:
                    continue
                pen = bnp
            nd = d0 + w * 0.5 * (m0 + mf[cv]) + pen
            row = j * K
            for k in range(K):
                v = int(ik[row + k])
                if v < 0 or not ok[v]:
                    continue
                dh = hk[row + k] - hu
                if dh > up * w or dh < -climb:
                    continue
                if nd < dist[v] - 1e-12:
                    dist[v] = nd
                    prev[v] = u
                    heappush(pq, (nd + hypot(gxx - a, gyy - b), v))
    if hit < 0:
        return None
    out = [hit]
    while out[-1] != s:
        out.append(int(prev[out[-1]]))
    return out[::-1]


# 走查只用来跟住弦所在的层, 立面本身由挡线集与拓扑禁步管住, 故抬升按体素取样
# 容差放宽: 高度取自格心, 斜面与接缝上相邻格的取样差本就能超出一步抬升上限,
# 照拓扑口径卡会把大量直弦判死, 拉直退化成网格锯齿。
class LayerOracle:
    def __init__(self, HK, IK, cidx, nx, ny, x0, y0, cs=CS,
                 slope=SLOPE, climb=CLIMB, qh=QH):
        self.HK = HK
        self.IK = IK
        self.cidx = cidx
        self.nx = nx
        self.ny = ny
        self.x0 = x0
        self.y0 = y0
        self.cs = cs
        self.slope = slope
        self.climb = climb
        self.qh = qh

    # h 取起点高度或一组可达高度
    def walk(self, pts, h):
        cs, nx, ny = self.cs, self.nx, self.ny
        cells = []
        for i in range(1, len(pts)):
            ax = int((pts[i - 1][0] - self.x0) / cs)
            ay = int((pts[i - 1][1] - self.y0) / cs)
            bx = int((pts[i][0] - self.x0) / cs)
            by = int((pts[i][1] - self.y0) / cs)
            n = max(abs(bx - ax), abs(by - ay)) or 1
            for k in range(n + 1):
                c = (ax + round((bx - ax) * k / n),
                     ay + round((by - ay) * k / n))
                if not cells or cells[-1] != c:
                    cells.append(c)
        cells = [c for c in cells if 0 <= c[0] < nx and 0 <= c[1] < ny]
        cur = np.atleast_1d(np.asarray(h, np.float32))
        pc = cells[0] if cells else None
        for c in cells[1:]:
            j = int(self.cidx[c[1] * nx + c[0]])
            if j < 0:
                continue
            nb = self.HK[j][self.IK[j] >= 0]
            if not len(nb):
                continue
            up = (self.slope * math.hypot(c[0] - pc[0], c[1] - pc[1]) * cs
                  + self.qh)
            d = nb[None, :] - cur[:, None]
            m = (d <= up) & (d >= -self.climb)
            if not m.any():
                return None
            cur = nb[m.any(0)]
            pc = c
        return cur

    def ok(self, p, q, h, hq=None):
        cur = self.walk((p, q), h)
        if cur is None:
            return False
        return hq is None or bool((np.abs(cur - hq) <= 1e-3).any())


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
    NC = nx * ny
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
        uid = np.unique(pid[hit])
        out.update((a[uid] * NC + b[uid]).tolist())
        out.update((b[uid] * NC + a[uid]).tolist())
    return out


def _csr_gather(wid, start, cells):
    cnt = start[cells + 1] - start[cells]
    if not cnt.sum():
        return np.zeros(0, np.int64)
    base = np.repeat(start[cells], cnt)
    off = np.arange(cnt.sum()) - np.repeat(
        np.concatenate(([0], np.cumsum(cnt)[:-1])), cnt)
    return wid[base + off]


def _sh(m, dy, dx):
    ny, nx = m.shape
    out = np.zeros_like(m)
    out[max(0, -dy):ny + min(0, -dy), max(0, -dx):nx + min(0, -dx)] = \
        m[max(0, dy):ny + min(0, dy), max(0, dx):nx + min(0, dx)]
    return out


def cost_astar(mask, s, g, mult, banned=None, bnp=None, forbidden=None):
    ny, nx = mask.shape
    if not mask[s[1], s[0]] or not mask[g[1], g[0]]:
        return None
    bn = banned or ()
    fb = forbidden or ()
    NC = nx * ny
    dist = np.full(mask.shape, np.inf)
    prev = np.full(mask.shape, -1, np.int32)
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
            eid = (y * nx + x) * NC + (b * nx + a)
            if eid in fb:
                continue
            pen = 0.0
            if eid in bn:
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
        return pref.astype(np.float32, copy=False)
    rg = np.zeros(dist.shape, bool)
    for dy, dx in ((0, 1), (1, 0), (1, 1), (1, -1)):
        a, b = _shf(dist, dy, dx), _shf(dist, -dy, -dx)
        rg |= (dist >= np.maximum(a, b)) & (dist > np.minimum(a, b))
    rg &= dist >= RIDGEF
    return np.where(rg, np.minimum(pref, dist), pref).astype(
        np.float32, copy=False
    )


def target_field(dist):
    locw = local_max(dist, int(math.ceil(R / CS)))
    return np.maximum(np.minimum(GEO_R, locw), 0.25).astype(
        np.float32, copy=False
    )


# 层高逐点否决: 弦须从前一点的可达高度集走通, 且走到的高度集覆盖后一点原有的
# 高度集, 后续各点据此仍然走得通。整线走查只能全取或全弃, 一处跨带就把整条线
# 的共线剔除连坐掉, 网格锯齿会原样留在终线上。
# 剔点后自该点起重算高度集: 剔点只会放大可达集, 沿用旧值会把后续弦按更窄的
# 起点集判死。
def slim(pts, blk, eps=SLIMEPS, cfl=None, lyo=None, h=None):
    P = [tuple(p) for p in pts]
    hv = None

    def chain(k):
        for i in range(k, len(P)):
            hv[i] = (None if hv[i - 1] is None
                     else lyo.walk((P[i - 1], P[i]), hv[i - 1]))

    if lyo is not None and h is not None:
        hv = [np.asarray([h], np.float32)] + [None] * (len(P) - 1)
        chain(1)
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
            ok = d <= eps and not blk.blocked(a, c) and (
                cfl is None
                or cfl.seg(a, c) >= min(cfl.seg(a, b), cfl.seg(b, c)))
            if ok and hv is not None:
                nh = (None if hv[i - 1] is None
                      else lyo.walk((a, c), hv[i - 1]))
                ok = (nh is not None and hv[i + 1] is not None
                      and bool(np.isin(hv[i + 1], nh).all()))
            if ok:
                P.pop(i)
                if hv is not None:
                    hv.pop(i)
                    hv[i] = nh
                    chain(i + 1)
                ch = True
            else:
                i += 1
    return P


def _turn(a, b, c):
    ux, uy = b[0] - a[0], b[1] - a[1]
    vx, vy = c[0] - b[0], c[1] - b[1]
    nu, nv = math.hypot(ux, uy), math.hypot(vx, vy)
    if nu < 1e-12 or nv < 1e-12:
        return -1.0
    return (ux * vx + uy * vy) / (nu * nv)


def _at(F, x0, y0, cs, p):
    ny, nx = F.shape
    return float(F[min(max(int((p[1] - y0) / cs), 0), ny - 1),
                   min(max(int((p[0] - x0) / cs), 0), nx - 1)])


# 拉直把拐点钉在轮廓角上, 过角即贴角切线, 实机绕不过去。沿转弯外侧扫方向把
# 拐点外挪到留够过角余量; 只挪拐点不插点, 两段仍是直线, 直角不抹圆。
# 判据取拐点自身净空: 用整弦会被两侧远处的窄段钉死, 角上的亏欠被掩盖。
# 相邻段短于 CORNER_SEG 的不算拐点, 亚像素锯齿挪动只会把线推向墙。
# 候选按偏离转弯外侧的角度排序, 达标即停; 绝大多数方向被挡线否决, 少试方向
# 会整体空转。两段弦净空各自允许半格退让, 挡线与层高各自否决。
# 候选不得把转角掰得更尖: 外挪是给转弯让余量, 掰尖等于就地折返。
def widen_corners(P, blk, dist, x0, y0, cs, cfl, want=CORNER_R,
                  lyo=None, h=None, rounds=CORNER_ROUNDS):
    Q = [(float(p[0]), float(p[1])) for p in P]
    if len(Q) < 3:
        return Q
    cosmin = math.cos(math.radians(CORNER_TURN))
    nstep = max(1, int(round(CORNER_MAX / CORNER_STEP)))
    ang = [2.0 * math.pi * t / CORNER_DIRS for t in range(CORNER_DIRS)]
    for _ in range(rounds):
        moved = False
        for k in range(1, len(Q) - 1):
            a, b, c = Q[k - 1], Q[k], Q[k + 1]
            ux, uy = b[0] - a[0], b[1] - a[1]
            vx, vy = c[0] - b[0], c[1] - b[1]
            nu, nv = math.hypot(ux, uy), math.hypot(vx, vy)
            if nu < CORNER_SEG or nv < CORNER_SEG:
                continue
            ux, uy, vx, vy = ux / nu, uy / nu, vx / nv, vy / nv
            if ux * vx + uy * vy > cosmin:
                continue
            best = _at(dist, x0, y0, cs, b)
            if best >= want:
                continue
            out = math.atan2(uy - vy, ux - vx)
            fa = cfl.seg(a, b) - CLRTOL if cfl is not None else -1.0
            fc = cfl.seg(b, c) - CLRTOL if cfl is not None else -1.0
            order = sorted(ang, key=lambda t: abs(
                math.remainder(t - out, 2.0 * math.pi)))
            turn = ux * vx + uy * vy
            pick = None
            for t in order:
                dx, dy = math.cos(t), math.sin(t)
                for i in range(1, nstep + 1):
                    q = (b[0] + dx * i * CORNER_STEP,
                         b[1] + dy * i * CORNER_STEP)
                    if blk.blocked(a, q) or blk.blocked(q, c):
                        break
                    if _turn(a, q, c) < turn - 1e-9:
                        continue
                    v = _at(dist, x0, y0, cs, q)
                    if v > best + 1e-9 and (
                            cfl is None
                            or (cfl.seg(a, q) >= fa and cfl.seg(q, c) >= fc)):
                        best, pick = v, q
                        if best >= want:
                            break
                if best >= want:
                    break
            if pick is None:
                continue
            if lyo is not None and lyo.walk(
                    Q[:k] + [pick] + Q[k + 1:], h) is None:
                continue
            Q[k] = pick
            moved = True
        if not moved:
            break
    return Q


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
    if i1 > i0:
        idx = np.arange(i0 + 1, i1, dtype=np.int64)
    else:
        idx = np.concatenate(
            (np.arange(i0 + 1, n, dtype=np.int64),
             np.arange(0, i1, dtype=np.int64))
        )
    if not len(idx):
        return -1
    q = P[idx] - a
    if L2 > 1e-12:
        t = np.clip((q @ d) / L2, 0.0, 1.0)
        e = q - d * t[:, None]
    else:
        e = q
    dd = e[:, 0] * e[:, 0] + e[:, 1] * e[:, 1]
    best = max_err * max_err
    m = float(dd.max())
    return -1 if m <= best else int(idx[int(np.argmax(dd))])


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
        self.cell = 8.0
        self._grid: dict[tuple[int, int], list[int]] = {}
        for i in range(len(self.A)):
            ax, ay = self.A[i]
            bx, by = self.B[i]
            for cx, cy in self._line_cells(ax, ay, bx, by, 0.0):
                self._grid.setdefault((cx, cy), []).append(i)

    def blocked(self, p, q, eps=1e-7):
        px, py = float(p[0]), float(p[1])
        qx, qy = float(q[0]), float(q[1])
        rx, ry = qx - px, qy - py
        seen = set()
        for cx, cy in self._line_cells(px, py, qx, qy, eps):
            for i in self._grid.get((cx, cy), ()):
                if i in seen:
                    continue
                seen.add(i)
                ax, ay = self.A[i]
                bx, by = self.B[i]
                sx, sy = bx - ax, by - ay
                den = rx * sy - ry * sx
                if abs(den) <= 1e-12:
                    continue
                ux, uy = ax - px, ay - py
                t = (ux * sy - uy * sx) / den
                w = (ux * ry - uy * rx) / den
                if (eps < t < 1 - eps and eps < w < 1 - eps):
                    return True
        return self._off(p, q)

    def _line_cells(self, px, py, qx, qy, eps):
        """枚举查询线段实际经过的网格单元, 避免遍历整张包围盒。"""
        cell = self.cell
        lx, hx = min(px, qx) - eps, max(px, qx) + eps
        ly, hy = min(py, qy) - eps, max(py, qy) + eps
        c0, c1 = int(lx // cell), int(hx // cell)
        r0, r1 = int(ly // cell), int(hy // cell)
        dx, dy = qx - px, qy - py
        if dx == 0.0:
            for cx in range(c0, c1 + 1):
                for cy in range(r0, r1 + 1):
                    yield cx, cy
            return
        for cx in range(c0, c1 + 1):
            xlo = max(cx * cell, min(px, qx))
            xhi = min((cx + 1) * cell, max(px, qx))
            if xlo > xhi:
                continue
            t0 = max(min((xlo - px) / dx, (xhi - px) / dx), 0.0)
            t1 = min(max((xlo - px) / dx, (xhi - px) / dx), 1.0)
            if t1 < t0:
                continue
            y0 = py + dy * t0
            y1 = py + dy * t1
            ylo, yhi = min(y0, y1) - eps, max(y0, y1) + eps
            for cy in range(int(ylo // cell), int(yhi // cell) + 1):
                yield cx, cy

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


# 沿弦按半格步长采样: seg 取 min(净空, 目标余量) 的下确界, cost 取代价泛函积分
class ClearanceFloor:
    def __init__(self, cf, mg, x0, y0, cs=CS):
        self.cf = cf
        self.mg = mg
        self.x0 = x0
        self.y0 = y0
        self.cs = cs

    def _cells(self, p, q, t):
        gx = ((p[0] + (q[0] - p[0]) * t - self.x0) / self.cs).astype(np.int64)
        gy = ((p[1] + (q[1] - p[1]) * t - self.y0) / self.cs).astype(np.int64)
        return (np.clip(gy, 0, self.cf.shape[0] - 1),
                np.clip(gx, 0, self.cf.shape[1] - 1))

    def seg(self, p, q):
        L = float(np.hypot(q[0] - p[0], q[1] - p[1]))
        gy, gx = self._cells(p, q, np.linspace(0.0, 1.0,
                                               int(L / (self.cs * 0.5)) + 2))
        return float(self.cf[gy, gx].min())

    def cost(self, p, q):
        L = float(np.hypot(q[0] - p[0], q[1] - p[1]))
        n = max(int(math.ceil(L / (self.cs * 0.5))), 1)
        gy, gx = self._cells(p, q, (np.arange(n) + 0.5) / n)
        return L * float(self.mg[gy, gx].mean(dtype=np.float64))


def string_pull(pts, blk, rounds=6, cfl=None, lyo=None, hs=None):
    P = [tuple(map(float, p)) for p in pts]
    seg = cst = None
    if cfl is not None and len(P) > 1:
        seg = np.array([cfl.seg(P[k], P[k + 1]) for k in range(len(P) - 1)],
                       dtype=np.float64)
        cst = np.array([cfl.cost(P[k], P[k + 1]) for k in range(len(P) - 1)],
                       dtype=np.float64)
    idx = list(range(len(P)))
    for _ in range(rounds):
        out = [P[0]]
        oid = [idx[0]]
        i = 0
        while i < len(P) - 1:
            acc = None if seg is None else np.minimum.accumulate(seg[idx[i]:])
            ac2 = None if cst is None else np.cumsum(cst[idx[i]:])
            j = len(P) - 1
            while j > i + 1:
                k = idx[j] - idx[i] - 1
                if (not blk.blocked(P[i], P[j])
                        and (acc is None
                             or (cfl.seg(P[i], P[j]) >= float(acc[k]) - CLRTOL
                                 and cfl.cost(P[i], P[j])
                                 <= float(ac2[k]) * (1.0 + COSTTOL)))
                        and (lyo is None
                             or lyo.ok(P[i], P[j], hs[idx[i]], hs[idx[j]]))):
                    break
                j -= 1
            out.append(P[j])
            oid.append(idx[j])
            i = j
        changed = len(out) != len(P)
        P = out
        idx = oid
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
