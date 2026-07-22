#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import math
import threading
import time

import numpy as np

import recastnav as rc
from recastnav import (CAP, CS, LAM, MARGIN, MAX_CELLS, MAXERR, MC_HBAND, R,
                       SLIMEPS, SNAP_RADIUS, TAU)
from recastnav_zone import CleanNav, WallOracle


def build(zc, wo, s, h0, x0, y0, x1, y1):
    nx = int(np.ceil((x1 - x0) / CS))
    ny = int(np.ceil((y1 - y0) / CS))
    m = zc.mesh

    t0 = time.time()
    cell, hz, ins = rc.rasterize(m.V, m.H, m.T, x0, y0, nx, ny)
    bc, bh = rc.seam_bridge(cell, hz, nx, ny)
    if len(bc):
        cell = np.concatenate([cell, bc])
        hz = np.concatenate([hz, bh])
        ins = np.concatenate([ins, np.zeros(len(bc), bool)])
    sp_cell, sp_h, occ, cstart, ccnt = rc.spans(cell, hz)
    HK, IK, sp_ci = rc.dense_k(sp_h, occ, cstart, ccnt)
    t_vox = time.time() - t0

    widx = wo.walls_in_bbox(x0 - 4, y0 - 4, x0 + nx * CS + 4,
                            y0 + ny * CS + 4)
    dead = rc.stamp_walls(wo.P0[widx], wo.P1[widx], wo.HH[widx], x0, y0,
                          nx, ny, (occ, HK, IK, len(sp_h)))

    gx = int((s[0] - x0) / CS); gy = int((s[1] - y0) / CS)
    j = int(np.searchsorted(occ, gy * nx + gx))
    if j >= len(occ) or occ[j] != gy * nx + gx:
        return None, f"起点格无体素 (gx={gx},gy={gy})"
    cand = IK[j][IK[j] >= 0]
    seed = int(cand[int(np.argmin(np.abs(sp_h[cand] - h0)))])

    t0 = time.time()
    vis = rc.flood(seed, sp_h, occ, HK, IK, sp_ci, nx)
    t_fl = time.time() - t0

    lay = np.zeros(ny * nx, bool)
    lh = np.full(ny * nx, np.nan, np.float32)
    c_ = sp_cell[vis]
    lay[c_] = True
    lh[c_] = sp_h[vis]
    lay = lay.reshape(ny, nx); lh = lh.reshape(ny, nx)
    wallcell = np.zeros(ny * nx, bool)
    wallcell[sp_cell[dead]] = True
    lay = rc.fill_holes(lay, rc.HOLE_MAX, protect=wallcell.reshape(ny, nx))
    sol = np.zeros(ny * nx, bool)
    ci, hi_ = cell[ins], hz[ins]
    lf = lh.ravel()
    okc = ~np.isnan(lf[ci]) & (np.abs(hi_ - lf[ci]) <= rc.MERGE_H)
    sol[ci[okc]] = True
    core = rc.fill_holes(lay & sol.reshape(ny, nx), rc.HOLE_MAX,
                         protect=wallcell.reshape(ny, nx))
    core = rc.close_cracks(core, lay, protect=wallcell.reshape(ny, nx))

    keep = rc.walls_at_layer(wo.P0[widx], wo.P1[widx], wo.HH[widx], lh,
                             x0, y0, nx, ny, hband=MC_HBAND)
    wP0, wP1 = wo.P0[widx][keep], wo.P1[widx][keep]
    wid, wstart = rc.wall_index(wP0, wP1, x0, y0, nx, ny)

    t0 = time.time()
    dist = rc.clearance(core)
    t_edt = time.time() - t0

    info = dict(x0=x0, y0=y0, nx=nx, ny=ny, lay=lay, lh=lh, dist=dist,
                core=core, t_vox=t_vox, t_fl=t_fl, t_edt=t_edt,
                wP0=wP0, wP1=wP1, wid=wid, wstart=wstart)
    return info, None


def local_h(info, S, h0):
    gx = np.clip(((S[:, 0] - info["x0"]) / CS).astype(int), 0, info["nx"] - 1)
    gy = np.clip(((S[:, 1] - info["y0"]) / CS).astype(int), 0, info["ny"] - 1)
    h = info["lh"][gy, gx]
    return np.where(np.isnan(h), h0, h)


def wall_dist(wo, S, hs, pad=CAP):
    S = np.asarray(S, float)
    if not len(S):
        return np.zeros(0)
    hs = np.full(len(S), float(hs)) if np.isscalar(hs) else np.asarray(hs)
    idx = wo.walls_in_bbox(*(S.min(0) - pad), *(S.max(0) + pad))
    if not len(idx):
        return np.full(len(S), CAP)
    A, D, WH = wo.P0[idx], wo.P1[idx] - wo.P0[idx], wo.HH[idx]
    L2 = np.maximum((D * D).sum(1), 1e-18)
    out = np.empty(len(S))
    for k in range(len(S)):
        hb = np.abs(WH - hs[k]) <= MC_HBAND
        if not hb.any():
            out[k] = CAP
            continue
        a, d, l2 = A[hb], D[hb], L2[hb]
        t = np.clip(((S[k] - a) * d).sum(1) / l2, 0.0, 1.0)
        C = a + d * t[:, None]
        out[k] = min(CAP, float(np.hypot(*(C - S[k]).T).min()))
    return out


def metrics(wo, P, h0, info, step=0.25):
    P = np.asarray(P, float)
    L = float(np.hypot(*np.diff(P, axis=0).T).sum())
    dv = (wall_dist(wo, P[1:-1], local_h(info, P[1:-1], h0))
          if len(P) > 2 else np.zeros(0))
    hug = tot = 0.0
    for i in range(1, len(P)):
        seg = float(np.hypot(*(P[i] - P[i - 1])))
        if seg <= 1e-9:
            continue
        m = max(int(math.ceil(seg / step)), 1)
        ts = (np.arange(m) + 0.5) / m
        S = P[i - 1] + (P[i] - P[i - 1]) * ts[:, None]
        d = wall_dist(wo, S, local_h(info, S, h0))
        hug += float((d < TAU).sum()) * (seg / m); tot += seg
    return (L, len(P), int((dv < TAU).sum()),
            float(dv.min()) if len(dv) else CAP,
            hug / tot * 100 if tot else 0.0)


def route(info, s, g):
    lay, dist, core = info["lay"], info["dist"], info["core"]
    x0, y0, nx, ny = info["x0"], info["y0"], info["nx"], info["ny"]
    walk = core & lay
    bn = rc.banned_steps(lay, info["wid"], info["wstart"],
                         info["wP0"], info["wP1"], x0, y0, nx)
    # 亏欠越多单价越高;脊线保底只进几何口径 prefg,禁入 mult
    pref = rc.pref_field(dist)
    mult = 1.0 + LAM * np.clip((pref - dist) / pref, 0.0, 1.0)
    prefg = rc.pref_field(dist, ridge=True)

    sc = (int((s[0] - x0) / CS), int((s[1] - y0) / CS))
    gc = (int((g[0] - x0) / CS), int((g[1] - y0) / CS))

    def near(mask, p):
        ys, xs = np.nonzero(mask)
        if not len(xs):
            return None, 0.0
        d = (xs - p[0]) ** 2 + (ys - p[1]) ** 2
        i = int(np.argmin(d))
        return (int(xs[i]), int(ys[i])), float(np.sqrt(d[i])) * CS

    warn = []
    as_, dsa = near(walk, sc)
    ag_, dga = near(walk, gc)
    if as_ is None:
        return None, {"err": "walk 掩膜为空"}
    BIGP = nx * ny * CS * (1.0 + LAM)
    t0 = time.time()
    used = walk
    q = rc.cost_astar(walk, as_, ag_, mult, bn, BIGP) if as_ != ag_ else [as_]
    if q is None:
        used = core
        q = rc.cost_astar(core, as_, ag_, mult, bn, BIGP)
        if q is not None:
            warn.append("walk 断开→退回 core")
    if q is None:
        return None, {"err": "不连通", "warn": warn}
    t_as = time.time() - t0
    xw = []
    for k in range(1, len(q)):
        if (q[k - 1][1] * nx + q[k - 1][0],
                q[k][1] * nx + q[k][0]) in bn:
            xw.append((x0 + (q[k][0] + 0.5) * CS,
                       y0 + (q[k][1] + 0.5) * CS))
    if xw:
        warn.append(f"不可避穿墙 {len(xw)} 步")

    def cen(P):
        return [(x0 + (a + 0.5) * CS, y0 + (b + 0.5) * CS) for a, b in P]

    t0 = time.time()
    loops_c = rc.trace_contours(core)

    def w(P):
        return np.column_stack([x0 + P[:, 0] * CS, y0 + P[:, 1] * CS])

    wseg = (info["wP0"], info["wP1"])
    onm = (used, x0, y0, CS)
    blk_gray = rc.Blockers([w(P) for P in loops_c], extra=wseg, on=onm)
    grn = [bool(dist[b, a] >= prefg[b, a] - 1e-9) for a, b in q]
    runs, i = [], 0
    while i < len(q):
        j = i
        while j + 1 < len(q) and grn[j + 1] == grn[i]:
            j += 1
        runs.append([grn[i], i, j])
        i = j + 1

    def merge(rs):
        out = []
        for r_ in rs:
            if out and out[-1][0] == r_[0]:
                out[-1][2] = r_[2]
            else:
                out.append(r_)
        return out

    for k, r_ in enumerate(runs):
        if (not r_[0] and (r_[2] - r_[1]) * CS < 2.0
                and 0 < k < len(runs) - 1):
            r_[0] = True
    runs = merge(runs)
    for r_ in runs:
        if r_[0] and (r_[2] - r_[1]) * CS < 1.5:
            r_[0] = False
    mg = merge(runs)
    ones = np.ones_like(mult)
    taut = []
    for isg, i0, i1 in mg:
        cells = q[i0:min(i1 + 1, len(q) - 1) + 1]
        pp = cen(cells)
        if len(cells) >= 2:
            blk = blk_gray
            if isg:
                # 绿段:er=腐蚀掩膜(脊线保底限路径走廊±R),重寻守卫 l2≤l1×1.2+2px
                pm = np.zeros(dist.shape, bool)
                for a, b in cells:
                    pm[b, a] = True
                pmd = pm.copy()
                kd = int(math.ceil(R / CS))
                for dy, dx in ((0, 1), (1, 0)):
                    acc = pmd.copy()
                    for i_ in range(1, kd + 1):
                        acc |= rc._sh(pmd, i_ * dy, i_ * dx)
                        acc |= rc._sh(pmd, -i_ * dy, -i_ * dx)
                    pmd = acc
                er = (dist >= pref) | ((dist >= prefg) & pmd) | pm
                q2 = rc.cost_astar(er, cells[0], cells[-1], ones, bn, None)
                if q2 is not None:
                    l2 = sum(math.dist(q2[k - 1], q2[k])
                             for k in range(1, len(q2)))
                    l1 = sum(math.dist(cells[k - 1], cells[k])
                             for k in range(1, len(cells)))
                    if l2 <= l1 * 1.2 + 2.0 / CS:
                        pp = cen(q2)
                lp = [rc.simplify_loop(P, MAXERR / CS) for P in
                      rc.trace_contours(er)]
                blk = rc.Blockers(
                    [w(P) for P in lp] + [w(P) for P in loops_c],
                    extra=wseg, on=onm)
            pp = [tuple(p) for p in rc.string_pull(pp, blk)]
        if taut and pp and math.dist(pp[0], taut[-1]) < 1e-9:
            pp = pp[1:]
        taut.extend(pp)
    t_sp = time.time() - t0
    line = [tuple(s)] + taut + [tuple(g)]
    line = [p for i, p in enumerate(line)
            if i in (0, len(line) - 1) or
            (math.dist(p, line[0]) > 0.4 and math.dist(p, line[-1]) > 0.4)]
    ded = [line[0]]
    for p in line[1:]:
        if math.dist(p, ded[-1]) > 1e-9:
            ded.append(p)
    line = rc.drop_loops(ded)
    if SLIMEPS > 0 and len(line) > 2:
        line = rc.slim(line, blk_gray, SLIMEPS)
    return line, {"warn": warn, "snapd": (dsa, dga),
                  "t_as": t_as, "t_sp": t_sp, "xwall": xw}


def offmesh(line, info):
    A = np.asarray(line, float)
    SS = []
    for i in range(1, len(A)):
        L = float(np.hypot(*(A[i] - A[i - 1])))
        m = max(int(math.ceil(L / 0.25)), 1)
        ts = (np.arange(m) + 0.5) / m
        SS.append(A[i - 1] + (A[i] - A[i - 1]) * ts[:, None])
    if not SS:
        return 0.0, 0.0
    SS = np.vstack(SS)
    gx = np.clip(((SS[:, 0] - info["x0"]) / CS).astype(int), 0, info["nx"] - 1)
    gy = np.clip(((SS[:, 1] - info["y0"]) / CS).astype(int), 0, info["ny"] - 1)
    wk = info["core"] & info["lay"]
    return (float((~wk[gy, gx]).sum()) * 0.25,
            float((~info["lay"][gy, gx]).sum()) * 0.25)


class RecastEngine:

    def __init__(self, field):
        self.nav = CleanNav(field)
        self._oracles = {}
        self.lock = threading.Lock()

    def _zone(self, name):
        zc = self.nav.zone(name)
        wo = self._oracles.get(name)
        if wo is None:
            wo = self._oracles[name] = WallOracle(zc)
        return zc, wo

    def warm(self, zone_name):
        # 提前完成该区的准备(区网格切片 + 墙判据); 构建放锁外, 不阻塞其他区的规划,
        # 与并发 plan 撞同一区最坏重复构建一次, dict 单操作在 GIL 下原子
        zc = self.nav.zone(zone_name)
        wo = WallOracle(zc)
        with self.lock:
            self._oracles.setdefault(zone_name, wo)

    def plan(self, zone_name, start, goal, floor_y=None):
        with self.lock:
            return self._plan(zone_name, start, goal, floor_y)

    def _plan(self, zone_name, start, goal, floor_y):
        t_all = time.time()
        zc, wo = self._zone(zone_name)
        s = (float(start[0]), float(start[1]))
        g = (float(goal[0]), float(goal[1]))
        ss = zc.snap(s, SNAP_RADIUS, floor_y)
        if ss is None:
            raise ValueError("起点不在网格附近")
        if zc.snap(g, SNAP_RADIUS, floor_y) is None:
            raise ValueError("终点不在网格附近")
        h0 = float(np.mean(zc.mesh.H[zc.mesh.T[ss[0]]]))

        margins = [MARGIN, MARGIN * 2, MARGIN * 4, MARGIN * 8]
        info = line = dg = None
        last_err = None
        for i, margin in enumerate(margins):
            x0 = min(s[0], g[0]) - margin; y0 = min(s[1], g[1]) - margin
            x1 = max(s[0], g[0]) + margin; y1 = max(s[1], g[1]) + margin
            nx = int(np.ceil((x1 - x0) / CS))
            ny = int(np.ceil((y1 - y0) / CS))
            if nx * ny > MAX_CELLS:
                raise ValueError(f"窗口过大 ({nx}×{ny} 格)")
            info, err = build(zc, wo, s, h0, x0, y0, x1, y1)
            if err is None:
                line, dg = route(info, s, g)
                if line is not None:
                    P = np.asarray(line, float)
                    pad = 2.0
                    # 锚点远 = 走廊出窗,同触界扩窗,否则末段盲跳穿墙
                    far = max(dg["snapd"]) > SNAP_RADIUS
                    if far:
                        if i == len(margins) - 1:
                            raise ValueError(
                                f"端点接不上可走层 (起 {dg['snapd'][0]:.1f}px"
                                f" / 终 {dg['snapd'][1]:.1f}px, 疑似不连通)")
                        err = "端点锚点过远,扩窗重跑"
                    elif i == len(margins) - 1 or (
                            P[:, 0].min() > x0 + pad
                            and P[:, 0].max() < x1 - pad
                            and P[:, 1].min() > y0 + pad
                            and P[:, 1].max() < y1 - pad):
                        break
                    else:
                        err = "终线触界,扩窗重跑"
                else:
                    err = dg.get("err", "路线失败")
            last_err = err
        else:
            raise ValueError(last_err or "路线失败")

        L, npts, hits, cmin, hug = metrics(wo, line, h0, info)
        ow, ol = offmesh(line, info)
        return {
            "points": [(float(x), float(y)) for x, y in line],
            "length": L,
            "warn": list(dg["warn"]),
            "wall_cross": [(float(x), float(y)) for x, y in dg["xwall"]],
            "offmesh_walk": ow,
            "offmesh_lay": ol,
            "metrics": {"points": npts, "corner_hits": hits,
                        "min_clearance": cmin, "hug_pct": hug},
            "snap": {"start": dg["snapd"][0], "goal": dg["snapd"][1]},
            "window": {"x0": info["x0"], "y0": info["y0"],
                       "nx": info["nx"], "ny": info["ny"], "cs": CS},
            "timing": {"vox": info["t_vox"], "flood": info["t_fl"],
                       "edt": info["t_edt"], "astar": dg["t_as"],
                       "pull": dg["t_sp"],
                       "total": time.time() - t_all},
        }
