#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import math
import threading
import time
from collections import OrderedDict

import numpy as np

import recastnav as rc
import recastnav_grid as rg
from recastnav import (CAP, CS, DECK_BAND, LAM, LAM_R, MARGIN, MAX_CELLS,
                       MAXERR, MC_HBAND, R, SLIMEPS, SNAP_RADIUS, TAU)
from recastnav_zone import CleanNav, WallOracle


def _llround(v):
    return int(math.floor(v + 0.5)) if v >= 0 else -int(math.floor(-v + 0.5))


def _last_per_cell(cells, vals, order):
    """按给定次序取每格最后一条,用于同格多记录时后写的赢。"""
    if not len(order):
        return cells[:0], vals[:0]
    c = cells[order]
    keep = np.append(c[1:] != c[:-1], True)
    return c[keep], vals[order][keep]


def _pick_start_rec(gw, cell, h0):
    """起点格里高度离 h0 最近的那条真 span 定类。类选错整条线就落在另一层上。"""
    idx = np.flatnonzero((gw.cell == cell)
                         & ((gw.flags & (rg.FLAG_GHOST | rg.FLAG_FILL)) == 0))
    if not len(idx):
        return -1
    return int(idx[int(np.argmin(np.abs(gw.h[idx].astype(np.float64) - h0)))])


def _pick_deck_rec(gw, nx, ny, gcx, gcy, deck):
    """终点声明了面时改由终点定类:终点格附近带内、能走的那条,先按格距再按高度差挑。
    起点那侧只在这个类里选面,所以起点二维吸附落在屋顶上也不会把线拉到别层去。"""
    rad = int(math.ceil(SNAP_RADIUS / CS))
    x = gw.cell % nx
    y = gw.cell // nx
    hd = np.abs(gw.h.astype(np.float64) - deck)
    ok = (((gw.flags & rg.FLAG_WALK) != 0) & ((gw.flags & rg.FLAG_FILL) == 0)
          & (hd <= DECK_BAND) & (np.abs(x - gcx) <= rad) & (np.abs(y - gcy) <= rad))
    idx = np.flatnonzero(ok)
    if not len(idx):
        return -1
    cd = (x[idx] - gcx) ** 2 + (y[idx] - gcy) ** 2
    return int(idx[int(np.lexsort((idx, x[idx], y[idx], hd[idx], cd))[0])])


def _core_anchor_px(gp, gz, p):
    """点到最近核心格的格距 × CS,与窗口里的 near() 同口径,只是在全区图上量。
    搜索半径取判据的两倍,够不着的点只报这个下界,反正它已经在闸外了。"""
    cs = gp.cell_size
    reach = SNAP_RADIUS * 2.0
    cx = int(math.floor(p[0] / cs))
    cy = int(math.floor(p[1] / cs))
    rad = int(math.ceil(reach / cs))
    best = -1
    for tile in rg.tiles_in_rect(gz, cx - rad, cy - rad, cx + rad, cy + rad):
        if int(tile["rec"]) == 0:
            continue
        t = gp.decode(tile)
        g = tile["g"]
        m = (t.flags & rg.FLAG_CORE) != 0
        ix = t.cell[m] % int(g[2])
        iy = t.cell[m] // int(g[2])
        own = ((ix >= int(g[4])) & (ix <= int(g[5]))
               & (iy >= int(g[6])) & (iy <= int(g[7])))
        if not own.any():
            continue
        d = ((int(g[0]) + ix[own] - cx) ** 2 + (int(g[1]) + iy[own] - cy) ** 2).min()
        if best < 0 or d < best:
            best = int(d)
    return reach if best < 0 else math.sqrt(best) * cs


def build(zc, wo, gp, gz, s, s_snap, g, h0, goal_deck, x0, y0, x1, y1):
    nx = int(np.ceil((x1 - x0) / CS))
    ny = int(np.ceil((y1 - y0) / CS))
    # 窗口原点是对齐过的,所以它落在全局格线上,窗口格与烘焙格一一对上
    t0 = time.time()
    try:
        gw = rg.GridWindow(gp, gz, _llround(x0 / CS), _llround(y0 / CS), nx, ny)
    except ValueError:
        return None, "预烘格图解不开"
    t_grid = time.time() - t0

    t0 = time.time()
    gx = int((s[0] - x0) / CS); gy = int((s[1] - y0) / CS)
    inw = 0 <= gx < nx and 0 <= gy < ny
    start_rec = _pick_start_rec(gw, gy * nx + gx, h0) if inw else -1
    if start_rec < 0:
        # 起点离网时其所在格无体素,退用按楼层吸附过的起点定种子
        gx = int((s_snap[0] - x0) / CS); gy = int((s_snap[1] - y0) / CS)
        inw = 0 <= gx < nx and 0 <= gy < ny
        start_rec = _pick_start_rec(gw, gy * nx + gx, h0) if inw else -1
    if start_rec < 0:
        return None, f"起点格无体素 (gx={gx},gy={gy})"
    cell0 = gy * nx + gx
    region = int(gw.rid[start_rec])
    if goal_deck is not None:
        deck_rec = _pick_deck_rec(gw, nx, ny, int((g[0] - x0) / CS),
                                  int((g[1] - y0) / CS), goal_deck)
        if deck_rec < 0:
            return None, f"终点附近没有声明的面 (deck={goal_deck:g})"
        region = int(gw.rid[deck_rec])

    ghost = (gw.flags & rg.FLAG_GHOST) != 0
    fill = (gw.flags & rg.FLAG_FILL) != 0
    same = gw.rid == region
    lay = np.zeros(ny * nx, bool)
    core = np.zeros(ny * nx, bool)
    dist = np.zeros(ny * nx, np.float32)
    lh = np.full(ny * nx, np.nan, np.float32)
    stepbits = np.zeros(ny * nx, np.uint8)
    is_core = (gw.flags & rg.FLAG_CORE) != 0
    lay[gw.cell[same & (((gw.flags & rg.FLAG_WALK) != 0) | ~is_core)]] = True
    core[gw.cell[same & is_core]] = True
    sc, ss_ = gw.cell[same], gw.steps[same]
    c, v = _last_per_cell(sc, rg.grid_clearance(gw.clr[same]),
                          np.argsort(sc, kind="stable"))
    dist[c] = v
    for bit in range(8):
        hit = sc[((ss_ >> bit) & 1) != 0]
        if len(hit):
            stepbits[hit] = stepbits[hit] | np.uint8(1 << bit)
    real = same & ~ghost & ~fill
    c, v = _last_per_cell(gw.cell[real], gw.h[real],
                          np.lexsort((gw.h[real], gw.cell[real])))
    lh[c] = v
    lay = lay.reshape(ny, nx); core = core.reshape(ny, nx)
    lh = lh.reshape(ny, nx); dist = dist.reshape(ny, nx)

    widx = wo.walls_in_bbox(x0 - 4, y0 - 4, x0 + nx * CS + 4, y0 + ny * CS + 4)
    keep = rc.walls_at_layer(wo.P0[widx], wo.P1[widx], wo.HH[widx], lh,
                             x0, y0, nx, ny, hband=MC_HBAND)
    wP0, wP1 = wo.P0[widx][keep], wo.P1[widx][keep]
    wid, wstart = rc.wall_index(wP0, wP1, x0, y0, nx, ny)

    # 禁步面按烘出来的位还原。位序与方向表是写入方定的,方向倒序的那一位对应反向键;
    # 只有正交两向出线段,对角步不挡视线。
    sev = set()
    ea, eb = [], []
    flat = lay.ravel()
    for i in range(4):
        dx, dy = rg.STEP_DX[i], rg.STEP_DY[i]
        bits = (stepbits >> (2 * i)) & 0x03
        cid = np.flatnonzero((bits != 0) & flat)
        ax, ay = cid % nx + dx, cid // nx + dy
        m = (ax >= 0) & (ax < nx) & (ay >= 0) & (ay < ny)
        cid, ax, ay = cid[m], ax[m], ay[m]
        if not len(cid):
            continue
        nb = ay * nx + ax
        b = bits[cid]
        fwd = (b & 0x01) != 0
        rev = (b & 0x02) != 0
        sev.update((cid[fwd] * (ny * nx) + nb[fwd]).tolist())
        sev.update((nb[rev] * (ny * nx) + cid[rev]).tolist())
        if dx and dy:
            continue
        px = x0 + (cid % nx + dx) * CS
        py = y0 + (cid // nx + dy) * CS
        ea.append(np.column_stack([px, py]))
        eb.append(np.column_stack([px + dy * CS, py + dx * CS]))
    sseg = ((np.vstack(ea), np.vstack(eb)) if ea
            else (np.zeros((0, 2)), np.zeros((0, 2))))

    # 表里留着别的类的 span:层判据要看整列,少一层就会从楼板底下穿过去
    inspan = ~(fill | (ghost & ~same))
    o = np.lexsort((gw.h[inspan], gw.cell[inspan]))
    spC, spH, spV = gw.cell[inspan][o], gw.h[inspan][o], same[inspan][o]
    spOcc, cst, cct = np.unique(spC, return_index=True, return_counts=True)
    spHK, spIK, spCi = rc.dense_k(spH, spOcc, cst, cct)
    cidx = np.full(ny * nx, -1, np.int32)
    cidx[spOcc] = np.arange(len(spOcc), dtype=np.int32)
    cd = spIK[int(cidx[cell0])]
    cd = cd[(cd >= 0) & spV[np.maximum(cd, 0)]]
    if not len(cd):
        return None, "起点格没有与终点同类的面"
    seedN = int(cd[int(np.argmin(np.abs(spH[cd] - np.float32(h0))))])
    t_win = time.time() - t0

    t0 = time.time()
    reachN = rc.span_reach(seedN, spH, spOcc, spHK, spIK, spCi, spV, nx, ny)
    t_reach = time.time() - t0

    info = dict(x0=x0, y0=y0, nx=nx, ny=ny, lay=lay, lh=lh, dist=dist,
                core=core, t_grid=t_grid, t_win=t_win, t_reach=t_reach,
                wP0=wP0, wP1=wP1, wid=wid, wstart=wstart,
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


def route(info, s, g, *, goal_deck=None):
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
    # 禁步面是硬的,墙边只罚分:窄处绕不开时宁可贴着走也不判不连通
    BIGP = nx * ny * CS * (1.0 + LAM)
    faces = info["sev"]
    soft = bn
    t0 = time.time()
    on3 = cw3
    qs = None

    # 可走集已经限死在终点那张面所属的类里, 起点这侧只剩层内挑高度
    def search(use, c3, svs, gs):
        if not svs:
            return None
        return rc.span_astar(use, spH, spOcc, spHK, spIK, spCi, cidx, c3,
                             at_seed_layer(svs), set(gs), mult, nx, ny,
                             soft, BIGP, faces)

    if as_ == ag_:
        vs = pick(as_, useW)
        gs = goals_of(vs)
        if goal_deck is None:
            qs = [at_seed_layer(vs)]
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
                    qs = [at_seed_layer(vs)]
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
        section = field.sections.get(rg.TAG)
        if section is None:
            raise ValueError("包里没有预烘格图段,请换用带 GRID 段的包")
        self.grid = rg.GridPack(section)
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
        gz = self.grid.zone(zone_name)
        if gz is None:
            raise ValueError(f"区没有预烘格图 ({zone_name})")
        zc, wo = self._zone(zone_name)
        s = (float(start[0]), float(start[1]))
        g = (float(goal[0]), float(goal[1]))
        ss = zc.snap(s, SNAP_RADIUS, floor_y)
        if ss is None:
            raise ValueError("起点不在网格附近")
        if zc.snap(g, SNAP_RADIUS, floor_y) is None:
            raise ValueError("终点不在网格附近")
        h0 = float(np.mean(zc.mesh.H[zc.mesh.T[ss[0]]]))

        # 端点接不上可走层的腿在全区图上就能判掉。全区核心是任何窗口内核心的超集,
        # 量出来的锚距是窗口里那把尺子的下界,过不了这道闸的腿换多大的窗口也接不上。
        zsa = _core_anchor_px(self.grid, gz, s)
        zga = _core_anchor_px(self.grid, gz, g)
        if zsa > SNAP_RADIUS or zga > SNAP_RADIUS:
            raise ValueError(f"端点接不上可走层 (起 {zsa:.1f}px / 终 {zga:.1f}px,"
                             " 疑似不连通)")

        # 扩窗只留两档。59 条生产腿里 ×100 与 ×200 零成功,只在必败腿上把时间烧掉。
        margins = [MARGIN, MARGIN * 2]
        info = line = dg = None
        last_err = None
        for p, margin in enumerate(margins):
            last_margin = p + 1 == len(margins)
            # 窗口边界对齐到全局网格。原点直接取 min-margin 时, 起点差 0.06px 就换一套体素相位,
            # 同一段路两次规划得到的格子划分不同; 对齐后相位只由世界坐标决定, 与起终点无关。
            x0 = math.floor((min(s[0], g[0]) - margin) / CS) * CS
            y0 = math.floor((min(s[1], g[1]) - margin) / CS) * CS
            x1 = math.ceil((max(s[0], g[0]) + margin) / CS) * CS
            y1 = math.ceil((max(s[1], g[1]) + margin) / CS) * CS
            nx = int(np.ceil((x1 - x0) / CS))
            ny = int(np.ceil((y1 - y0) / CS))
            if nx * ny > MAX_CELLS:
                raise ValueError(f"窗口过大 ({nx}×{ny} 格)")
            info, err = build(zc, wo, self.grid, gz, s, ss[1], g, h0,
                              goal_deck_y, x0, y0, x1, y1)
            if err is None:
                line, dg = route(info, s, g, goal_deck=goal_deck_y)
                if line is not None:
                    P = np.asarray(line, float)
                    pad = 2.0
                    # 锚点远 = 走廊出窗,同触界扩窗,否则末段盲跳穿墙
                    if max(dg["snapd"]) > SNAP_RADIUS:
                        if last_margin:
                            raise ValueError(
                                f"端点接不上可走层 (起 {dg['snapd'][0]:.1f}px"
                                f" / 终 {dg['snapd'][1]:.1f}px, 疑似不连通)")
                        err = "端点锚点过远,扩窗重跑"
                    elif last_margin or (
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
            "timing": {"grid": info["t_grid"], "window": info["t_win"],
                       "reach": info["t_reach"], "astar": dg["t_as"],
                       "pull": dg["t_sp"],
                       "total": time.time() - t_all},
        }
