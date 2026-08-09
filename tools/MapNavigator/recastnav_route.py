#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import math
import threading
import time
from collections import OrderedDict

import numpy as np

import recastnav as rc
from recastnav import (CAP, CS, DECK_BAND, LAM, LAM_R, MARGIN, MAX_CELLS,
                       MAXERR, MC_HBAND, PLAN_BUDGET_MS, R, SLIMEPS,
                       SNAP_RADIUS, TAU)
from recastnav_zone import CleanNav, WallOracle


def _clip_walls_at_layer_interpolated(P0, P1, H0, H1, lh, ox, oy, nx, ny):
    """只保留墙边沿途与所选层相交的局部片段。"""
    P0 = np.asarray(P0, float)
    P1 = np.asarray(P1, float)
    if not len(P0):
        return np.empty((0, 2), float), np.empty((0, 2), float)
    H0 = np.asarray(H0, float)
    H1 = np.asarray(H1, float)
    out0, out1 = [], []
    for wall in range(len(P0)):
        length = float(np.hypot(*(P1[wall] - P0[wall])))
        steps = max(int(math.ceil(length / (CS * 0.4))), 1) + 1
        open_t = None
        for k in range(steps - 1):
            t0 = k / (steps - 1)
            t1 = (k + 1) / (steps - 1)
            t = (t0 + t1) / 2.0
            sample = P0[wall] + (P1[wall] - P0[wall]) * t
            gx = math.floor((sample[0] - ox) / CS)
            gy = math.floor((sample[1] - oy) / CS)
            keep = False
            if 0 <= gx < nx and 0 <= gy < ny:
                layer_height = float(lh[gy, gx])
                edge_height = H0[wall] + (H1[wall] - H0[wall]) * t
                keep = not math.isnan(layer_height) and abs(
                    layer_height - edge_height) <= rc.QH
            if keep and open_t is None:
                open_t = t0
            if not keep and open_t is not None:
                out0.append(P0[wall] + (P1[wall] - P0[wall]) * open_t)
                out1.append(P0[wall] + (P1[wall] - P0[wall]) * t0)
                open_t = None
        if open_t is not None:
            out0.append(P0[wall] + (P1[wall] - P0[wall]) * open_t)
            out1.append(P1[wall])
    return np.asarray(out0, float).reshape(-1, 2), np.asarray(out1, float).reshape(-1, 2)


def build(
    zc,
    wo,
    s,
    s_snap,
    h0,
    x0,
    y0,
    x1,
    y1,
    *,
    layered_core=False,
    layer_hint=None,
):
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
    dead = (None if layered_core else
            rc.stamp_walls(wo.P0[widx], wo.P1[widx], wo.HH[widx], x0, y0,
                           nx, ny, (occ, HK, IK, len(sp_h))))

    gx = int((s[0] - x0) / CS); gy = int((s[1] - y0) / CS)
    j = int(np.searchsorted(occ, gy * nx + gx))
    if j >= len(occ) or occ[j] != gy * nx + gx:
        # 起点离网时其所在格无体素,退用按楼层吸附过的起点定种子
        gx = int((s_snap[0] - x0) / CS); gy = int((s_snap[1] - y0) / CS)
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
    wall_lh = lh.copy()
    # 声明面只参与墙的局部选层；旧 core 仍沿用原层图，避免改写成功线路。
    if layer_hint is not None:
        # The recovery path is tied to a declared deck. Select that deck per column before
        # filtering walls; otherwise the last reachable upper span can project its walls onto
        # the lower route even though the lower span is the one being searched.
        safe_ids = np.maximum(IK, 0)
        reachable = (IK >= 0) & vis[safe_ids]
        delta = np.where(reachable, np.abs(sp_h[safe_ids] - layer_hint), np.inf)
        have = reachable.any(axis=1)
        best = safe_ids[np.arange(len(occ)), delta.argmin(axis=1)]
        wall_lh.fill(np.nan)
        wall_lh[occ[have]] = sp_h[best[have]]
    if layered_core:
        lh = wall_lh
    lay = lay.reshape(ny, nx); lh = lh.reshape(ny, nx)
    wall_lh = wall_lh.reshape(ny, nx)
    if layer_hint is not None:
        wP0, wP1 = _clip_walls_at_layer_interpolated(
            wo.P0[widx], wo.P1[widx], wo.H0[widx], wo.H1[widx], wall_lh,
            x0, y0, nx, ny)
    else:
        keep = rc.walls_at_layer(wo.P0[widx], wo.P1[widx], wo.HH[widx], lh,
                                 x0, y0, nx, ny, hband=MC_HBAND)
        wP0, wP1 = wo.P0[widx][keep], wo.P1[widx][keep]
    wid, wstart = rc.wall_index(wP0, wP1, x0, y0, nx, ny)
    if layered_core:
        wallcell = np.diff(wstart) > 0
    else:
        wallcell = np.zeros(ny * nx, bool)
        wallcell[sp_cell[dead]] = True
    lay = rc.fill_holes(lay, rc.HOLE_MAX, protect=wallcell.reshape(ny, nx))
    sol = np.zeros(ny * nx, bool)
    ci, hi_ = cell[ins], hz[ins]
    if layered_core:
        # A 2-D height slot is ambiguous when two reachable decks overlap the same cell: assigning
        # ``lh[c] = sp_h`` lets the last span erase the other deck. Match each interior raster sample
        # against the reachable spans in its own column instead, so an upper deck cannot punch holes
        # into the lower deck's core mask (or vice versa).
        rows = np.searchsorted(occ, ci)
        span_ids = IK[rows]
        valid = span_ids >= 0
        safe_ids = np.where(valid, span_ids, 0)
        okc = np.any(
            valid & vis[safe_ids]
            & (np.abs(hi_[:, None] - sp_h[safe_ids]) <= rc.QH), axis=1)
    else:
        lf = lh.ravel()
        okc = ~np.isnan(lf[ci]) & (np.abs(hi_ - lf[ci]) <= rc.QH)
    sol[ci[okc]] = True
    core = rc.fill_holes(lay & sol.reshape(ny, nx), rc.HOLE_MAX,
                         protect=wallcell.reshape(ny, nx))
    core = rc.close_cracks(core, lay, protect=wallcell.reshape(ny, nx))

    sev, sseg = rc.step_breaks(occ, HK, IK, vis, lay, nx, ny, x0, y0)

    spC, spH, spV = sp_cell, sp_h, vis
    spOcc, spHK, spIK, spCi = occ, HK, IK, sp_ci
    have = np.zeros(ny * nx, bool)
    have[sp_cell[vis]] = True
    gh = np.nonzero(lay.ravel() & ~have)[0]
    if len(gh):
        T0 = rc._step_heights(occ, HK, IK, vis, lay, nx, ny)[:, 0]
        gh = gh[np.isfinite(T0[gh])]
    if len(gh):
        spC = np.concatenate([sp_cell, gh])
        spH = np.concatenate([sp_h, T0[gh].astype(np.float32)])
        spV = np.concatenate([vis, np.ones(len(gh), bool)])
        o = np.lexsort((spH, spC))
        spC, spH, spV = spC[o], spH[o], spV[o]
        spOcc, cst, cct = np.unique(spC, return_index=True, return_counts=True)
        spHK, spIK, spCi = rc.dense_k(spH, spOcc, cst, cct)
    cidx = np.full(ny * nx, -1, np.int32)
    cidx[spOcc] = np.arange(len(spOcc), dtype=np.int32)
    sj = int(cidx[gy * nx + gx])
    cd = spIK[sj][spIK[sj] >= 0]
    seedN = int(cd[int(np.argmin(np.abs(spH[cd] - h0)))])
    reachN = rc.span_reach(seedN, spH, spOcc, spHK, spIK, spCi, spV, nx, ny)

    t0 = time.time()
    dist = rc.clearance(core)
    t_edt = time.time() - t0

    info = dict(x0=x0, y0=y0, nx=nx, ny=ny, lay=lay, lh=lh, dist=dist,
                core=core, t_vox=t_vox, t_fl=t_fl, t_edt=t_edt,
                wP0=wP0, wP1=wP1, wid=wid, wstart=wstart,
                sourceWallP0=wo.P0[widx], sourceWallP1=wo.P1[widx],
                sourceWallH0=wo.H0[widx], sourceWallH1=wo.H1[widx],
                sev=sev, sseg=sseg, h0=h0,
                spC=spC, spH=spH, spOcc=spOcc, spHK=spHK, spIK=spIK,
                spCi=spCi, cidx=cidx, reachN=reachN)
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


def _layer_wall_clear(info, lyo, line, start_height, goal_deck):
    """逐交点验证终线存在一条不穿过同高墙体的连续层。"""
    eps = 1e-7
    heights = np.asarray([start_height], np.float32)
    walls0 = np.asarray(info["sourceWallP0"], float)
    walls1 = np.asarray(info["sourceWallP1"], float)
    wall_h0 = np.asarray(info["sourceWallH0"], float)
    wall_h1 = np.asarray(info["sourceWallH1"], float)
    for segment in range(1, len(line)):
        p = np.asarray(line[segment - 1], float)
        q = np.asarray(line[segment], float)
        route = q - p
        hits = []
        route_lo = np.minimum(p, q)
        route_hi = np.maximum(p, q)
        nearby = np.nonzero(
            (np.maximum(walls0[:, 0], walls1[:, 0]) >= route_lo[0])
            & (np.minimum(walls0[:, 0], walls1[:, 0]) <= route_hi[0])
            & (np.maximum(walls0[:, 1], walls1[:, 1]) >= route_lo[1])
            & (np.minimum(walls0[:, 1], walls1[:, 1]) <= route_hi[1])
        )[0]
        for wall in nearby:
            edge = walls1[wall] - walls0[wall]
            den = route[0] * edge[1] - route[1] * edge[0]
            if abs(den) <= 1e-12:
                continue
            offset = walls0[wall] - p
            route_t = (offset[0] * edge[1] - offset[1] * edge[0]) / den
            wall_t = (offset[0] * route[1] - offset[1] * route[0]) / den
            if not (eps < route_t < 1.0 - eps and eps < wall_t < 1.0 - eps):
                continue
            height = wall_h0[wall] + (wall_h1[wall] - wall_h0[wall]) * wall_t
            hits.append((route_t, height))
        hits.sort(key=lambda hit: hit[0])

        cursor = tuple(p)
        first = 0
        while first < len(hits):
            last = first + 1
            while last < len(hits) and abs(hits[last][0] - hits[first][0]) <= eps:
                last += 1
            crossing = tuple(p + route * hits[first][0])
            at_crossing = lyo.walk((cursor, crossing), heights)
            if at_crossing is None:
                return False
            blocked = np.zeros(len(at_crossing), bool)
            for _, wall_height in hits[first:last]:
                blocked |= np.abs(at_crossing - wall_height) <= rc.QH
            heights = at_crossing[~blocked]
            if not len(heights):
                return False
            cursor = crossing
            first = last
        at_end = lyo.walk((cursor, tuple(q)), heights)
        if at_end is None:
            return False
        heights = at_end
    return goal_deck is None or bool(
        (np.abs(heights - goal_deck) <= DECK_BAND).any())


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


def route(info, s, g, climb_faces=False, *, goal_deck=None, hard_walls=False,
          validate_layer_walls=False):
    # goal_deck: 终点所在面的高度。不声明时终点集是该格全部 span,先够到哪张停哪张。
    lay, dist, core = info["lay"], info["dist"], info["core"]
    x0, y0, nx, ny = info["x0"], info["y0"], info["nx"], info["ny"]
    walk = core & lay
    bn = rc.banned_steps(lay, info["wid"], info["wstart"],
                         info["wP0"], info["wP1"], x0, y0, nx)
    blocked = bn | info["sev"]
    # 掩膜距离场对跨越约束的墙无感, 取真墙距离的下确界补上
    ws = info["wstart"]
    wcell = (ws[1:] > ws[:-1]).reshape(ny, nx)
    dist = np.minimum(dist, rc.clearance(~wcell))
    # 亏欠越多单价越高;脊线保底只进几何口径 prefg,禁入 mult
    # 拓扑口径的余量目标随局部通道半宽下降, 窄通道的余量单价才不会把能走的路挤出选路
    pref = rc.pref_field(dist)
    mult = (1.0 + LAM * np.clip((pref - dist) / pref, 0.0, 1.0)).astype(
        np.float32, copy=False
    )
    prefg = rc.pref_field(dist, ridge=True)
    # 几何口径的余量目标: 通道半宽封顶 GEO_R, 供绿段重寻与拉直判定
    # 通道目标之外再按固定余量目标 R 追加平方亏欠, 使窄通道内部仍向中间收敛
    tgt = rc.target_field(dist)
    cdef = np.clip((R - dist) / R, 0.0, 1.0)
    multg = (1.0 + LAM * np.clip((tgt - dist) / tgt, 0.0, 1.0)
             + LAM_R * cdef * cdef).astype(np.float32, copy=False)
    cfl = rc.ClearanceFloor(np.minimum(dist, tgt), multg, x0, y0, CS)

    spC, spH, spOcc = info["spC"], info["spH"], info["spOcc"]
    spHK, spIK, spCi = info["spHK"], info["spIK"], info["spCi"]
    cidx, reachN = info["cidx"], info["reachN"]
    lyo = rc.LayerOracle(spHK, spIK, cidx, nx, ny, x0, y0)

    def mk(m2):
        u = reachN & m2.ravel()[spC]
        c3 = np.zeros(ny * nx, bool)
        c3[spC[u]] = True
        return u, c3

    useW, cw3 = mk(walk)
    useC, cc3 = mk(core)

    def pick(c, use):
        j = int(cidx[c[1] * nx + c[0]])
        return [int(v) for v in spIK[j] if v >= 0 and use[v]] if j >= 0 else []

    def at_seed_layer(vs):
        return int(min(vs, key=lambda v: abs(spH[v] - info["h0"])))

    # 高度最近的一张; 超出 DECK_BAND 视为该面不在此格
    def at_deck(vs, deck):
        best, bd = -1, 0.0
        for v in vs:
            d = abs(float(spH[v]) - deck)
            if best < 0 or d < bd:
                best, bd = int(v), d
        return best if best >= 0 and bd <= DECK_BAND else -1

    # 起点的面只由起点自己的高度决定, 终点的声明不参与。h0 来自二维吸附,
    # 重叠处可能落在屋顶, 所以终点声明了面时起点层不再当已知, 按 h0 由近到远逐张试。
    def seeds_of(vs):
        if goal_deck is None:
            return [at_seed_layer(vs)]
        return sorted(vs, key=lambda v: abs(float(spH[v]) - info["h0"]))

    # 终点声明是硬的: 收敛到单张 span, 匹配不上交空集让本级失败
    def goals_of(vs):
        if goal_deck is None:
            return vs
        v = at_deck(vs, goal_deck)
        return [v] if v >= 0 else []

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
    as_, dsa = near(cw3.reshape(ny, nx), sc)
    ag_, dga = near(cw3.reshape(ny, nx), gc)
    if as_ is None:
        return None, {"err": "walk 掩膜为空"}
    BIGP = nx * ny * CS * (1.0 + LAM)
    faces = blocked if hard_walls else (None if climb_faces else info["sev"])
    soft = () if hard_walls else (blocked if climb_faces else bn)
    t0 = time.time()
    on3 = cw3
    qs = None

    def search(use, c3, svs, gs):
        for sd in seeds_of(svs):
            r = rc.span_astar(use, spH, spOcc, spHK, spIK, spCi, cidx, c3,
                              sd, set(gs), mult, nx, ny, soft, BIGP, faces)
            if r is not None:
                return r
        return None

    if as_ == ag_:
        vs = pick(as_, useW)
        gs = goals_of(vs)
        if goal_deck is None:
            qs = [seeds_of(vs)[0]]
        elif gs:
            qs = [gs[0]]
    else:
        gs = goals_of(pick(ag_, useW))
        if goal_deck is None or gs:
            qs = search(useW, cw3, pick(as_, useW), gs)
    if qs is None:
        ac_, dc_ = near(cc3.reshape(ny, nx), sc)
        ag2, dg2 = near(cc3.reshape(ny, nx), gc)
        if ac_ is not None and ag2 is not None:
            if ac_ == ag2:
                vs = pick(ac_, useC)
                gs = goals_of(vs)
                if goal_deck is None:
                    qs = [seeds_of(vs)[0]]
                elif gs:
                    qs = [gs[0]]
            else:
                gs = goals_of(pick(ag2, useC))
                if goal_deck is None or gs:
                    qs = search(useC, cc3, pick(ac_, useC), gs)
        if qs is not None:
            on3 = cc3
            as_, dsa, ag_, dga = ac_, dc_, ag2, dg2
            warn.append("walk 断开→退回 core")
    if qs is not None:
        q = [(int(c % nx), int(c // nx)) for c in spC[qs]]
    else:
        # 格级搜索连 span 都不看, 退到这一级等于把选层交回给楼层盲的那一级
        if goal_deck is not None:
            gv = pick(ag_, useW)
            hs = sorted({round(float(spH[v]), 2) for v in gv})
            # 声明的面在表里就是这一跳连不上, 不在表里是这个坐标底下没有那张面
            tail = "这一跳连不上, 拆成多段" if at_deck(gv, goal_deck) >= 0 \
                else "该坐标下没有这张面"
            return None, {
                "err": f"目标面不可达 (声明 {goal_deck:g}, 终点格里的面 "
                       f"{hs if hs else '无'}) — {tail}",
                "warn": warn,
            }
        as_, dsa = near(walk, sc)
        ag_, dga = near(walk, gc)
        on3 = walk.ravel()
        q = (rc.cost_astar(walk, as_, ag_, mult, soft, BIGP, faces)
             if as_ != ag_ else [as_])
        if q is None:
            on3 = core.ravel()
            q = rc.cost_astar(core, as_, ag_, mult, soft, BIGP, faces)
            if q is not None:
                warn.append("walk 断开→退回 core")
        if q is None:
            return None, {"err": "不连通", "warn": warn}
        warn.append("层不连通→退回格级")
    t_as = time.time() - t0
    xw = []
    bad = []
    NC = nx * ny
    for k in range(1, len(q)):
        eid = (q[k - 1][1] * nx + q[k - 1][0]) * NC \
            + (q[k][1] * nx + q[k][0])
        if eid not in blocked:
            continue
        bad.append(k)
        if eid in bn:
            xw.append((x0 + (q[k][0] + 0.5) * CS,
                       y0 + (q[k][1] + 0.5) * CS))
    if xw:
        warn.append(f"不可避穿墙 {len(xw)} 步")
    if len(bad) > len(xw):
        warn.append(f"不可避立面 {len(bad) - len(xw)} 步")

    def cen(P):
        return [(x0 + (a + 0.5) * CS, y0 + (b + 0.5) * CS) for a, b in P]

    t0 = time.time()
    loops_c = rc.trace_contours(core)

    def w(P):
        return np.column_stack([x0 + P[:, 0] * CS, y0 + P[:, 1] * CS])

    wseg = (np.vstack([info["wP0"], info["sseg"][0]]),
            np.vstack([info["wP1"], info["sseg"][1]]))
    onm = (on3.reshape(ny, nx), x0, y0, CS)
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
    taut = []
    for isg, i0, i1 in mg:
        j1 = min(i1 + 1, len(q) - 1) + 1
        cells = q[i0:j1]
        sub = qs[i0:j1] if qs is not None else None
        pp = cen(cells)
        hs = None if sub is None else [float(spH[v]) for v in sub]
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
                # 切角规则要求对角步两个正交伴格都在掩膜里, 原掩膜搜不出时
                # 才放行伴格; 挡线集恒用 er, 伴格进挡线集会把角内侧开口
                ers = er.copy()
                for k2 in range(1, len(cells)):
                    (ax, ay), (bx, by) = cells[k2 - 1], cells[k2]
                    if ax != bx and ay != by:
                        ers[ay, bx] = True
                        ers[by, ax] = True
                # 重寻硬禁穿墙步,不可避穿墙处切开逐子段重寻,原步原样保留
                cuts = [k - i0 for k in bad if i0 < k <= i0 + len(cells) - 1]

                def research(m3):
                    ue, ce = mk(m3)
                    o2, oh, a2 = [], [], 0
                    for c2 in cuts + [len(cells)]:
                        b2 = c2 - 1
                        if a2 == b2:
                            r2 = [cells[a2]]
                            r2h = [hs[a2]] if hs is not None else None
                        elif sub is None:
                            r2 = rc.cost_astar(m3, cells[a2], cells[b2], multg,
                                               blocked, None)
                            r2h = None
                        else:
                            r2s = rc.span_astar(ue, spH, spOcc, spHK, spIK,
                                                spCi, cidx, ce, sub[a2],
                                                {sub[b2]}, multg, nx, ny,
                                                blocked, None)
                            r2 = (None if r2s is None else
                                  [(int(c % nx), int(c // nx))
                                   for c in spC[r2s]])
                            r2h = None if r2s is None else [float(spH[v])
                                                           for v in r2s]
                        if r2 is None:
                            return None, None
                        o2.extend(r2)
                        if r2h is not None:
                            oh.extend(r2h)
                        a2 = c2
                    return o2, oh

                q2, h2 = research(er)
                if q2 is None:
                    q2, h2 = research(ers)
                if q2 is not None:
                    l2 = sum(math.dist(q2[k - 1], q2[k])
                             for k in range(1, len(q2)))
                    l1 = sum(math.dist(cells[k - 1], cells[k])
                             for k in range(1, len(cells)))
                    if l2 <= l1 * 1.2 + 2.0 / CS:
                        pp = cen(q2)
                        hs = h2 if sub is not None else None
                lp = [rc.simplify_loop(P, MAXERR / CS) for P in
                      rc.trace_contours(er)]
                blk = rc.Blockers(
                    [w(P) for P in lp] + [w(P) for P in loops_c],
                    extra=wseg, on=onm)
            pp = [tuple(p) for p in
                  rc.string_pull(pp, blk, cfl=cfl,
                                 lyo=lyo if hs is not None else None, hs=hs)]
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
        line = rc.slim(line, blk_gray, SLIMEPS, cfl,
                       lyo=lyo if qs is not None else None,
                       h=float(spH[qs[0]]) if qs is not None else None)
    if len(line) > 2:
        line = rc.widen_corners(line, blk_gray, dist, x0, y0, CS, cfl,
                                lyo=lyo if qs is not None else None,
                                h=float(spH[qs[0]]) if qs is not None else None)
    if (validate_layer_walls
            and (qs is None or not _layer_wall_clear(
                info, lyo, line, float(spH[qs[0]]), goal_deck))):
        return None, {"err": "终线穿过当前层墙体", "warn": warn}
    clr = []
    for px, py in line:
        a = min(max(int(math.floor((px - x0) / CS)), 0), nx - 1)
        b = min(max(int(math.floor((py - y0) / CS)), 0), ny - 1)
        clr.append(float(dist[b, a]))
    return line, {"warn": warn, "snapd": (dsa, dga), "clr": clr,
                  "t_as": t_as, "t_sp": t_sp, "xwall": xw,
                  "crossed_barrier": bool(bad)}


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

    MAX_CACHED_ZONES = 2

    def __init__(self, field):
        self.nav = CleanNav(field)
        self._oracles: OrderedDict[str, WallOracle] = OrderedDict()
        self.lock = threading.Lock()

    def _zone(self, name):
        # 调用方必须已持有 self.lock（plan 的入口已加锁）。
        zc = self.nav.zone(name)
        wo = self._oracles.get(name)
        if wo is None:
            wo = self._oracles[name] = WallOracle(zc)
        self._oracles.move_to_end(name)
        self._evict_locked()
        return zc, wo

    def _evict_locked(self) -> None:
        while len(self._oracles) > self.MAX_CACHED_ZONES:
            old_name, _ = self._oracles.popitem(last=False)
            self.nav.drop_zone(old_name)

    def warm(self, zone_name):
        # 提前完成该区的准备(区网格切片 + 墙判据); 构建放锁外, 不阻塞其他区的规划。
        with self.lock:
            if zone_name in self._oracles:
                self._oracles.move_to_end(zone_name)
                self._evict_locked()
                return
        zc = self.nav.zone(zone_name)
        wo = WallOracle(zc)
        with self.lock:
            if zone_name in self._oracles:
                return
            self._oracles[zone_name] = wo
            self._oracles.move_to_end(zone_name)
            self._evict_locked()

    # goal_deck_y = 终点所在重叠面的高度,选层用,与吸附用的 floor_y 是两件事
    def plan(self, zone_name, start, goal, floor_y=None, *, goal_deck_y=None):
        with self.lock:
            return self._plan(zone_name, start, goal, floor_y, goal_deck_y)

    def _plan(self, zone_name, start, goal, floor_y, goal_deck_y=None):
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
        # 预算只计扩窗本身,不含首次进区的建区开销(与 RecastNavRoute.cpp 的
        # plan_started_at 同位置);否则冷区第一条线会被建区时间挤爆预算。
        t_budget = time.time()
        prev_cells = prev_ms = 0
        for p in range(len(margins) * 2):
            i, margin = p % len(margins), margins[p % len(margins)]
            x0 = min(s[0], g[0]) - margin; y0 = min(s[1], g[1]) - margin
            x1 = max(s[0], g[0]) + margin; y1 = max(s[1], g[1]) + margin
            nx = int(np.ceil((x1 - x0) / CS))
            ny = int(np.ceil((y1 - y0) / CS))
            if nx * ny > MAX_CELLS:
                raise ValueError(f"窗口过大 ({nx}×{ny} 格)")
            # 与 RecastNavRoute.cpp 同步:本档窗口按上一档实测速度外推,预算装不下就停。
            # 不跟着改的话,预览会算出运行时早已放弃的线。
            if p > 0:
                used_ms = int((time.time() - t_budget) * 1000)
                projected_ms = prev_ms * nx * ny // prev_cells if prev_cells else 0
                if used_ms + projected_ms > PLAN_BUDGET_MS:
                    raise ValueError(
                        f"{last_err or '路线失败'} (扩窗预算耗尽: 已用 {used_ms}ms,"
                        f" 下档 {nx}×{ny} 格约需 {projected_ms}ms)")
            t_pass = time.time()
            info, err = build(zc, wo, s, ss[1], h0, x0, y0, x1, y1)
            if err is None:
                line, dg = route(info, s, g, p >= len(margins),
                                 goal_deck=goal_deck_y)
                same_deck_recovery = (
                    goal_deck_y is not None
                    and abs(h0 - goal_deck_y) <= DECK_BAND
                )
                if same_deck_recovery and line is None:
                    # 成功的旧线路原样保留；只有旧拓扑失败才换稳定相位恢复。
                    recovery_x0 = math.floor((x0 - CS / 2.0) / CS) * CS + CS / 2.0
                    recovery_y0 = math.floor((y0 - CS / 2.0) / CS) * CS + CS / 2.0
                    recovery_x1 = recovery_x0 + math.ceil(
                        (x1 - recovery_x0) / CS) * CS
                    recovery_y1 = recovery_y0 + math.ceil(
                        (y1 - recovery_y0) / CS) * CS
                    recovery_nx = math.ceil((recovery_x1 - recovery_x0) / CS)
                    recovery_ny = math.ceil((recovery_y1 - recovery_y0) / CS)
                    if recovery_nx * recovery_ny > MAX_CELLS:
                        recovery_info = None
                        recovery_err = (
                            f"恢复窗口过大 ({recovery_nx}×{recovery_ny} 格)")
                    else:
                        recovery_info, recovery_err = build(
                            zc,
                            wo,
                            s,
                            ss[1],
                            h0,
                            recovery_x0,
                            recovery_y0,
                            recovery_x1,
                            recovery_y1,
                            layer_hint=goal_deck_y,
                        )
                    if recovery_err is None:
                        recovery_line, recovery_dg = route(
                            recovery_info,
                            s,
                            g,
                            goal_deck=goal_deck_y,
                            validate_layer_walls=True,
                        )
                        if (recovery_line is not None and not recovery_dg.get(
                                "crossed_barrier", False)):
                            info, line, dg = recovery_info, recovery_line, recovery_dg
                        else:
                            dg = recovery_dg
                    else:
                        dg = {"err": recovery_err}
                    if line is None:
                        # 稳定相位仍失败时，退回完整分层且墙体硬约束的保守路径。
                        hard_x0 = math.floor(x0 / CS) * CS
                        hard_y0 = math.floor(y0 / CS) * CS
                        hard_x1 = math.ceil(x1 / CS) * CS
                        hard_y1 = math.ceil(y1 / CS) * CS
                        hard_nx = math.ceil((hard_x1 - hard_x0) / CS)
                        hard_ny = math.ceil((hard_y1 - hard_y0) / CS)
                        if hard_nx * hard_ny > MAX_CELLS:
                            hard_info = None
                            hard_err = f"恢复窗口过大 ({hard_nx}×{hard_ny} 格)"
                        else:
                            hard_info, hard_err = build(
                                zc,
                                wo,
                                s,
                                ss[1],
                                h0,
                                hard_x0,
                                hard_y0,
                                hard_x1,
                                hard_y1,
                                layered_core=True,
                                layer_hint=goal_deck_y,
                            )
                        if hard_err is None:
                            hard_line, hard_dg = route(
                                hard_info,
                                s,
                                g,
                                goal_deck=goal_deck_y,
                                hard_walls=True,
                                validate_layer_walls=True,
                            )
                            if (hard_line is not None and not hard_dg.get(
                                    "crossed_barrier", False)):
                                info, line, dg = hard_info, hard_line, hard_dg
                            else:
                                dg = hard_dg
                        else:
                            dg = {"err": hard_err}
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
            prev_ms = max(int((time.time() - t_pass) * 1000), 1)
            prev_cells = nx * ny
            last_err = err
            info = line = dg = None
        else:
            raise ValueError(last_err or "路线失败")

        L, npts, hits, cmin, hug = metrics(wo, line, h0, info)
        ow, ol = offmesh(line, info)
        return {
            "points": [(float(x), float(y)) for x, y in line],
            "clearance": list(dg["clr"]),
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
