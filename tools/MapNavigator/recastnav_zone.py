#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import math
import time
from collections import deque

import numpy as np

import basenav_preview as bp
from recastnav import EPS_PROBE, H_BAND, MC_HBAND

WELD_DH = 3.0      # 顶点焊接同柱高差容差 px
PORTAL_SAMPLES = (0.5, 0.25, 0.75, 0.1, 0.9)  # 共边 hop 的门户采样位
FLOOR_BAND = bp.FLOOR_BAND
SNAP_FALLBACK_RADIUS = bp.SNAP_FALLBACK_RADIUS


class PolyMesh:

    def __init__(self, V, T, H):
        V = np.asarray(V, np.float64)
        T = np.asarray(T, np.int64).copy()
        ab = V[T[:, 1]] - V[T[:, 0]]
        ac = V[T[:, 2]] - V[T[:, 0]]
        cw = (ab[:, 0] * ac[:, 1] - ab[:, 1] * ac[:, 0]) < 0
        T[cw] = T[cw][:, [0, 2, 1]]
        self.V, self.T = V, T
        self.H = np.asarray(H, np.float64)
        self.NB = self._build_nb()
        self._build_grid()

    def _build_nb(self):
        T = self.T
        m = len(T)
        N = np.int64(len(self.V))
        A = T
        B = T[:, [1, 2, 0]]
        kd = (A * N + B).ravel()
        kr = (B * N + A).ravel()
        order = np.argsort(kd, kind="stable")
        skeys = kd[order]
        pos = np.searchsorted(skeys, kr)
        posc = np.clip(pos, 0, 3 * m - 1)
        found = skeys[posc] == kr
        return np.where(found, order[posc] // 3, -1).reshape(m, 3)

    def _build_grid(self, cell=24.0):
        self._cell = cell
        m = len(self.T)
        mn = np.minimum(np.minimum(self.V[self.T[:, 0]], self.V[self.T[:, 1]]),
                        self.V[self.T[:, 2]])
        mx = np.maximum(np.maximum(self.V[self.T[:, 0]], self.V[self.T[:, 1]]),
                        self.V[self.T[:, 2]])
        g0 = np.floor(mn / cell).astype(np.int64)
        g1 = np.floor(mx / cell).astype(np.int64)
        nx = g1[:, 0] - g0[:, 0] + 1
        ny = g1[:, 1] - g0[:, 1] + 1
        cnt = nx * ny
        tri = np.repeat(np.arange(m), cnt)
        off = np.concatenate(([0], np.cumsum(cnt)[:-1]))
        jj = np.arange(cnt.sum()) - np.repeat(off, cnt)
        nxr = np.repeat(nx, cnt)
        gx = np.repeat(g0[:, 0], cnt) + jj % nxr
        gy = np.repeat(g0[:, 1], cnt) + jj // nxr
        stride = np.int64(1 << 24)
        key = gx * stride + gy
        order = np.argsort(key, kind="stable")
        self._gkeys = key[order]
        self._gtris = tri[order]
        self._gstride = stride


class Hop:

    __slots__ = ("exit_pt", "entry_pt", "to_tri")

    def __init__(self, exit_pt, entry_pt, to_tri):
        self.exit_pt = exit_pt
        self.entry_pt = entry_pt
        self.to_tri = to_tri


class ZoneClean:

    def __init__(self, nav, zone_name):
        field = nav.field
        zone = field.zone_by_name[zone_name]
        self.name = zone_name
        self.zone_id = zone.zone_id
        self.lo = zone.first_triangle
        self.hi = zone.first_triangle + zone.triangle_count
        t0 = time.time()

        # 区网格:顶点段连续,f32 坐标回吸 0.05 格点得精确 f64
        TVI = np.array([field.triangles[i].vertices
                        for i in range(self.lo, self.hi)], dtype=np.int64)
        vmin = int(TVI.min())
        vmax = int(TVI.max())
        CT = TVI - vmin
        CV = np.array([(field.vertices[i].u, field.vertices[i].v)
                       for i in range(vmin, vmax + 1)], dtype=np.float64)
        CV = np.rint(CV * 20.0) / 20.0
        CH = np.array([field.vertices[i].height
                       for i in range(vmin, vmax + 1)], dtype=np.float64)

        key = np.round(CV * 1e4).astype(np.int64)
        kk = key[:, 0] * np.int64(1 << 40) + key[:, 1]
        order = np.argsort(kk, kind="stable")
        sk = kk[order]
        starts = np.flatnonzero(np.concatenate(([True], sk[1:] != sk[:-1])))
        ends = np.concatenate((starts[1:], [len(sk)]))
        MAP = np.arange(len(CV))
        n_weld = 0
        for s0, e0 in zip(starts, ends):
            if e0 - s0 < 2:
                continue
            ids = order[s0:e0]
            ho = ids[np.argsort(CH[ids], kind="stable")]
            rep = ho[0]
            for prev, cur in zip(ho[:-1], ho[1:]):
                if CH[cur] - CH[prev] <= WELD_DH:
                    MAP[cur] = rep
                    n_weld += 1
                else:
                    rep = cur
        CT2 = MAP[CT]
        degen = int(((CT2[:, 0] == CT2[:, 1]) | (CT2[:, 1] == CT2[:, 2]) |
                     (CT2[:, 2] == CT2[:, 0])).sum())
        if degen:
            raise RuntimeError(
                f"{zone_name}: weld produced {degen} degenerate tris")

        self.mesh = PolyMesh(CV, CT2, CH)
        mesh = self.mesh
        T, NB = mesh.T, mesh.NB

        Nv = np.int64(len(CV))
        kd = (T * Nv + T[:, [1, 2, 0]]).ravel()
        _, inv, cnt = np.unique(kd, return_inverse=True, return_counts=True)
        dup_slots = np.nonzero(cnt[inv] > 1)[0]
        n_dup = 0
        for slot in dup_slots:
            i, k = divmod(int(slot), 3)
            j = NB[i, k]
            NB[i, k] = -1
            if j >= 0:
                for k2 in np.nonzero(NB[j] == i)[0]:
                    NB[j, k2] = -1
            n_dup += 1
        JJ = NB.ravel()
        II = np.repeat(np.arange(len(T)), 3)
        live = JJ >= 0
        sym = np.zeros(len(JJ), bool)
        sym[live] = (NB[JJ[live]] == II[live][:, None]).any(1)
        NB.ravel()[live & ~sym] = -1

        # NB 掩码:焊接邻接必须在 pack link 表有背书,无背书的缝一律割掉
        la, lb = nav.zone_link_arrays(self)
        la = la - self.lo
        lb = lb - self.lo
        mtri = np.int64(len(T))
        lkey = np.sort(la * mtri + lb)
        II = np.repeat(np.arange(len(T)), 3)
        JJ = NB.ravel()
        live = np.nonzero(JJ >= 0)[0]
        pk = (np.minimum(II[live], JJ[live]) * mtri
              + np.maximum(II[live], JJ[live]))
        n_cut = 0
        for slot in live[~np.isin(pk, lkey)]:
            i, k = divmod(int(slot), 3)
            j = NB[i, k]
            if j < 0:
                continue
            NB[i, k] = -1
            for k2 in np.nonzero(NB[j] == i)[0]:
                NB[j, k2] = -1
            n_cut += 1

        par = np.arange(len(T))

        def find(x):
            while par[x] != x:
                par[x] = par[par[x]]
                x = par[x]
            return x

        for t in range(len(T)):
            for nb in NB[t]:
                if nb >= 0:
                    ra, rb = find(t), find(int(nb))
                    if ra != rb:
                        par[max(ra, rb)] = min(ra, rb)
        self.comp = np.array([find(i) for i in range(len(T))])

        # 岛 = 天然分量(pack n 字段)不超过阈值的三角占多数的 comp
        ncomp = np.asarray(field.natural_component)[self.lo:self.hi]
        ncsz = np.asarray(field.natural_component_size)
        isl = ncsz[ncomp] <= bp.SMALL_BRIDGE_COMPONENT_MAX_TRIANGLES
        n_tot = np.bincount(self.comp, minlength=len(T))
        n_isl = np.bincount(self.comp, weights=isl.astype(np.float64),
                            minlength=len(T))
        self.comp_island = (n_tot == 0) | (n_isl * 2 > n_tot)

        # link 层 hop:NB 已邻接跳过;跨分量共焊边 → edge portal,
        # 否则 touch/bridge;同分量共焊边且非近连通 → srcadj 窄通道
        self.hops = {}
        n_hop = {"edge": 0, "touch": 0, "bridge": 0, "srcadj": 0}
        adj = (NB[la] == lb[:, None]).any(1)
        ia, ib = la[~adj], lb[~adj]
        ca, cb = self.comp[ia], self.comp[ib]
        TA, TB = T[ia], T[ib]
        nsh = (TA[:, :, None] == TB[:, None, :]).any(2).sum(1)
        for r in np.nonzero(ca != cb)[0]:
            i, j = int(ia[r]), int(ib[r])
            c1, c2 = int(ca[r]), int(cb[r])
            if nsh[r] == 2:
                vb = set(TB[r].tolist())
                sh = [int(v) for v in TA[r] if int(v) in vb]
                p0, p1 = mesh.V[sh[0]], mesh.V[sh[1]]
                for t_ in PORTAL_SAMPLES:
                    pt = (float(p0[0] + (p1[0] - p0[0]) * t_),
                          float(p0[1] + (p1[1] - p0[1]) * t_))
                    self.hops.setdefault(c1, []).append(Hop(pt, pt, j))
                    self.hops.setdefault(c2, []).append(Hop(pt, pt, i))
                n_hop["edge"] += 1
            else:
                ex, en = field._closest_edge_bridge_points(
                    self.lo + i, self.lo + j)
                gap = math.hypot(ex[0] - en[0], ex[1] - en[1])
                brk = gap > 1e-7
                self.hops.setdefault(c1, []).append(Hop(ex, en, j))
                self.hops.setdefault(c2, []).append(Hop(en, ex, i))
                n_hop["touch" if not brk else "bridge"] += 1

        scand = np.nonzero(ca == cb)[0]
        if len(scand):
            iaS, ibS = ia[scand], ib[scand]
            nshS = nsh[scand]
            ring1 = NB[iaS]
            ring2 = NB[np.clip(ring1, 0, None)]
            near = ((ring2 == ibS[:, None, None])
                    & (ring1 >= 0)[:, :, None]).any((1, 2))
            LOCAL_R = 12.0
            Ccent = mesh.V[T].mean(1)
            for r in np.nonzero(~near)[0]:
                ta_, tb_ = int(iaS[r]), int(ibS[r])
                mx = (Ccent[ta_] + Ccent[tb_]) * 0.5
                seen = {ta_}
                dq2 = deque([ta_])
                hit = False
                while dq2 and not hit:
                    t2 = dq2.popleft()
                    for nb2 in NB[t2]:
                        nb2 = int(nb2)
                        if nb2 < 0 or nb2 in seen:
                            continue
                        if nb2 == tb_:
                            hit = True
                            break
                        if abs(Ccent[nb2, 0] - mx[0]) > LOCAL_R \
                                or abs(Ccent[nb2, 1] - mx[1]) > LOCAL_R:
                            continue
                        seen.add(nb2)
                        dq2.append(nb2)
                if hit:
                    continue
                c1, c2 = int(self.comp[ta_]), int(self.comp[tb_])
                if nshS[r] == 2:
                    vb = set(T[tb_].tolist())
                    sh = [int(v) for v in T[ta_] if int(v) in vb]
                    p0, p1 = mesh.V[sh[0]], mesh.V[sh[1]]
                    for t_ in PORTAL_SAMPLES:
                        pt = (float(p0[0] + (p1[0] - p0[0]) * t_),
                              float(p0[1] + (p1[1] - p0[1]) * t_))
                        self.hops.setdefault(c1, []).append(Hop(pt, pt, tb_))
                        self.hops.setdefault(c2, []).append(Hop(pt, pt, ta_))
                else:
                    ex, en = field._closest_edge_bridge_points(
                        self.lo + ta_, self.lo + tb_)
                    gap = math.hypot(ex[0] - en[0], ex[1] - en[1])
                    if gap > 8.0:
                        continue
                    self.hops.setdefault(c1, []).append(Hop(ex, en, tb_))
                    self.hops.setdefault(c2, []).append(Hop(en, ex, ta_))
                n_hop["srcadj"] += 1

        print(f"{zone_name}: weld {n_weld}v dup-sever {n_dup}, "
              f"link-mask cut {n_cut}, "
              f"comps {len(np.unique(self.comp))}, "
              f"hops {n_hop} [{time.time()-t0:.0f}s]", flush=True)

    _LOCATE_ROW_CAP = 2_000_000

    def _batch_locate(self, pts, hhints):
        mesh = self.mesh
        cell, stride = mesh._cell, mesh._gstride
        keys = (np.floor(pts[:, 0] / cell).astype(np.int64) * stride +
                np.floor(pts[:, 1] / cell).astype(np.int64))
        lo = np.searchsorted(mesh._gkeys, keys, "left")
        hi = np.searchsorted(mesh._gkeys, keys, "right")
        cnt = hi - lo
        csum = np.cumsum(cnt)
        out = np.full(len(pts), -1, np.int64)
        s = 0
        while s < len(pts):
            base = int(csum[s - 1]) if s else 0
            e = max(int(np.searchsorted(csum, base + self._LOCATE_ROW_CAP,
                                        "right")), s + 1)
            self._locate_chunk(pts, hhints, lo, cnt, s, e, out)
            s = e
        return out

    def _locate_chunk(self, pts, hhints, lo, cnt, s, e, out):
        mesh = self.mesh
        ccnt = cnt[s:e]
        pi = np.repeat(np.arange(s, e), ccnt)
        off = np.concatenate(([0], np.cumsum(ccnt)[:-1]))
        idx = np.repeat(lo[s:e], ccnt) + (np.arange(int(ccnt.sum()))
                                          - np.repeat(off, ccnt))
        cand = mesh._gtris[idx]
        A = mesh.V[mesh.T[cand, 0]]
        B = mesh.V[mesh.T[cand, 1]]
        C = mesh.V[mesh.T[cand, 2]]
        P = pts[pi]
        den = (B[:, 1] - C[:, 1]) * (A[:, 0] - C[:, 0]) \
            + (C[:, 0] - B[:, 0]) * (A[:, 1] - C[:, 1])
        den = np.where(np.abs(den) < 1e-12, np.nan, den)
        wa = ((B[:, 1] - C[:, 1]) * (P[:, 0] - C[:, 0])
              + (C[:, 0] - B[:, 0]) * (P[:, 1] - C[:, 1])) / den
        wb = ((C[:, 1] - A[:, 1]) * (P[:, 0] - C[:, 0])
              + (A[:, 0] - C[:, 0]) * (P[:, 1] - C[:, 1])) / den
        wc = 1.0 - wa - wb
        inside = (wa >= -1e-9) & (wb >= -1e-9) & (wc >= -1e-9)
        hcand = (wa * mesh.H[mesh.T[cand, 0]] + wb * mesh.H[mesh.T[cand, 1]] +
                 wc * mesh.H[mesh.T[cand, 2]])
        score = np.abs(hcand - hhints[pi])
        score[~inside] = np.inf
        good = np.isfinite(score)
        if good.any():
            ordr = np.lexsort((score[good], pi[good]))
            gpi = pi[good][ordr]
            gtri = cand[good][ordr]
            first = np.concatenate(([True], gpi[1:] != gpi[:-1]))
            out[gpi[first]] = gtri[first]

    def snap(self, point, radius, floor_y=None):
        mesh = self.mesh
        r = max(0.0, radius)
        for rr in ((r,) if r >= SNAP_FALLBACK_RADIUS
                   else (r, SNAP_FALLBACK_RADIUS)):
            best_key, best = None, None
            for t in self._tris_near(point, rr):
                a, b, c = (mesh.V[mesh.T[t, 0]], mesh.V[mesh.T[t, 1]],
                           mesh.V[mesh.T[t, 2]])
                sp, dist = _closest_on_tri(point, a, b, c)
                if dist > rr:
                    continue
                isl = 1 if self.comp_island[int(self.comp[t])] else 0
                if floor_y is None:
                    k = (isl, dist, int(t))
                else:
                    h = (mesh.H[mesh.T[t, 0]] + mesh.H[mesh.T[t, 1]]
                         + mesh.H[mesh.T[t, 2]]) / 3
                    delta = abs(h - floor_y)
                    k = (0 if delta <= FLOOR_BAND else 1, isl, dist, delta)
                if best_key is None or k < best_key:
                    best_key, best = k, (int(t), sp, dist)
            if best is not None:
                return best
        return None

    def _tris_near(self, p, r):
        mesh = self.mesh
        cell = mesh._cell
        out = set()
        for gx in range(int(math.floor((p[0] - r) / cell)),
                        int(math.floor((p[0] + r) / cell)) + 1):
            for gy in range(int(math.floor((p[1] - r) / cell)),
                            int(math.floor((p[1] + r) / cell)) + 1):
                key = np.int64(gx) * mesh._gstride + np.int64(gy)
                lo = np.searchsorted(mesh._gkeys, key, "left")
                hi = np.searchsorted(mesh._gkeys, key, "right")
                out.update(int(x) for x in mesh._gtris[lo:hi])
        return out


class CleanNav:

    def __init__(self, field):
        self.field = field
        adj = field.adjacency
        flat = np.frombuffer(adj.flat, dtype=np.int32)
        offs = np.asarray(adj.offsets, dtype=np.int64)
        counts = np.diff(offs)
        self.SRC = np.repeat(np.arange(len(counts), dtype=np.int64), counts)
        self.TGT = flat.astype(np.int64)
        self._zones = {}

    def zone(self, name):
        if name not in self._zones:
            self._zones[name] = ZoneClean(self, name)
        return self._zones[name]

    def zone_link_arrays(self, zc):
        m = (self.SRC >= zc.lo) & (self.SRC < zc.hi) & (self.TGT >= zc.lo) \
            & (self.TGT < zc.hi) & (self.SRC < self.TGT)
        return self.SRC[m], self.TGT[m]


class WallOracle:

    def __init__(self, zc):
        self.zc = zc
        mesh = zc.mesh
        T, NB, V, H = mesh.T, mesh.NB, mesh.V, mesh.H
        rows, ks = np.nonzero(NB < 0)
        a = T[rows, ks]
        b = T[rows, (ks + 1) % 3]
        P0, P1 = V[a], V[b]
        self.P0, self.P1 = P0, P1
        self.M = (P0 + P1) / 2.0
        d = P1 - P0
        ln = np.maximum(np.hypot(d[:, 0], d[:, 1]), 1e-12)
        self.NOBS = np.stack([d[:, 1] / ln, -d[:, 0] / ln], 1)
        self.HH = (H[a] + H[b]) / 2.0
        self.cls = np.full(len(a), -1, np.int8)
        self._lo = np.minimum(P0, P1)
        self._hi = np.maximum(P0, P1)
        self.hop_cell = 4.0
        self.hop_grid = {}
        for lst in zc.hops.values():
            for hp in lst:
                hz = float(H[T[hp.to_tri]].mean())
                for px_, py_ in (hp.exit_pt, hp.entry_pt):
                    key = (int(px_ // self.hop_cell),
                           int(py_ // self.hop_cell))
                    self.hop_grid.setdefault(key, []).append((px_, py_, hz))

    def _hop_near(self, ei, tol=1.5):
        p0, p1 = self.P0[ei], self.P1[ei]
        hh = self.HH[ei]
        cs = self.hop_cell
        x0, x1 = sorted((p0[0], p1[0]))
        y0, y1 = sorted((p0[1], p1[1]))
        d = p1 - p0
        l2 = max(float(d[0] * d[0] + d[1] * d[1]), 1e-18)
        for cx in range(int((x0 - tol) // cs), int((x1 + tol) // cs) + 1):
            for cy in range(int((y0 - tol) // cs), int((y1 + tol) // cs) + 1):
                for hx, hy, hz in self.hop_grid.get((cx, cy), ()):
                    if abs(hz - hh) > MC_HBAND:
                        continue
                    t_ = min(1.0, max(0.0, ((hx - p0[0]) * d[0]
                                            + (hy - p0[1]) * d[1]) / l2))
                    if math.hypot(p0[0] + d[0] * t_ - hx,
                                  p0[1] + d[1] * t_ - hy) <= tol:
                        return True
        return False

    def _probe_free(self, todo, dist):
        P = self.M[todo] + self.NOBS[todo] * dist
        hh = self.HH[todo]
        tri = self.zc._batch_locate(P, hh)
        th = np.where(tri >= 0,
                      self.zc.mesh.H[self.zc.mesh.T[np.clip(tri, 0, None)]]
                      .mean(1),
                      np.inf)
        return (tri >= 0) & (np.abs(th - hh) <= H_BAND)

    def classify(self, idx):
        idx = np.asarray(idx, np.int64)
        todo = idx[self.cls[idx] < 0]
        if len(todo):
            wall = ~self._probe_free(todo, EPS_PROBE)
            for j in np.nonzero(wall)[0]:
                if self._hop_near(int(todo[j])):
                    wall[j] = False
            self.cls[todo] = wall.astype(np.int8)
        return self.cls[idx]

    def walls_in_bbox(self, x0, y0, x1, y1):
        idx = np.nonzero((self._hi[:, 0] >= x0) & (self._lo[:, 0] <= x1)
                         & (self._hi[:, 1] >= y0) & (self._lo[:, 1] <= y1))[0]
        return idx[self.classify(idx) == 1]


def _closest_on_tri(p, a, b, c):
    if bp._point_in_triangle(p, a, b, c):
        return p, 0.0
    q = bp._closest_point_on_triangle(p, (a, b, c))
    return q, math.hypot(q[0] - p[0], q[1] - p[1])
