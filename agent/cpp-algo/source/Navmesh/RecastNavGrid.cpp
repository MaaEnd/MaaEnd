#include "RecastNavGrid.h"

#include <algorithm>
#include <cmath>
#include <limits>
#include <numeric>
#include <queue>
#include <tuple>
#include <unordered_map>

#if defined(__clang__)
#pragma clang fp contract(off)
#endif

namespace navmesh::recast
{

namespace
{

struct Nb8
{
    int64_t dx;
    int64_t dy;
    double w;
};

const Nb8 kNb8[8] = {
    { 1, 0, 1.0 },
    { -1, 0, 1.0 },
    { 0, 1, 1.0 },
    { 0, -1, 1.0 },
    { 1, 1, std::sqrt(2.0) },
    { 1, -1, std::sqrt(2.0) },
    { -1, 1, std::sqrt(2.0) },
    { -1, -1, std::sqrt(2.0) },
};

// t[k] = k/(steps-1),末点强制 1.0(np.linspace 语义)
int64_t sampleSteps(double len, double sub)
{
    return std::max<int64_t>(static_cast<int64_t>(std::ceil(len / (kCS * sub))), 1) + 1;
}

int64_t occFind(const std::vector<int64_t>& occ, int64_t cid)
{
    auto it = std::lower_bound(occ.begin(), occ.end(), cid);
    if (it == occ.end() || *it != cid) {
        return -1;
    }
    return it - occ.begin();
}

}

RasterCells Rasterize(
    const std::vector<WorldPoint>& V,
    const std::vector<double>& H,
    const std::vector<std::array<int32_t, 3>>& T,
    double ox,
    double oy,
    int64_t nx,
    int64_t ny)
{
    RasterCells out;
    const double hcs = kCS * 0.5;
    std::vector<int64_t> kept;
    kept.reserve(T.size());
    for (int64_t ti = 0; ti < static_cast<int64_t>(T.size()); ++ti) {
        const WorldPoint& A = V[T[ti][0]];
        const WorldPoint& B = V[T[ti][1]];
        const WorldPoint& C = V[T[ti][2]];
        const double minx = std::min({ A.x, B.x, C.x });
        const double maxx = std::max({ A.x, B.x, C.x });
        const double miny = std::min({ A.y, B.y, C.y });
        const double maxy = std::max({ A.y, B.y, C.y });
        int64_t ix0 = static_cast<int64_t>(std::floor((minx - ox) / kCS));
        int64_t ix1 = static_cast<int64_t>(std::floor((maxx - ox) / kCS));
        int64_t iy0 = static_cast<int64_t>(std::floor((miny - oy) / kCS));
        int64_t iy1 = static_cast<int64_t>(std::floor((maxy - oy) / kCS));
        if (ix1 < 0 || ix0 >= nx || iy1 < 0 || iy0 >= ny) {
            continue;
        }
        kept.push_back(ti);
        ix0 = std::clamp<int64_t>(ix0, 0, nx - 1);
        ix1 = std::clamp<int64_t>(ix1, 0, nx - 1);
        iy0 = std::clamp<int64_t>(iy0, 0, ny - 1);
        iy1 = std::clamp<int64_t>(iy1, 0, ny - 1);
        const double HA = H[T[ti][0]];
        const double HB = H[T[ti][1]];
        const double HC = H[T[ti][2]];
        for (int64_t gy = iy0; gy <= iy1; ++gy) {
            for (int64_t gx = ix0; gx <= ix1; ++gx) {
                const double px = ox + (static_cast<double>(gx) + 0.5) * kCS;
                const double py = oy + (static_cast<double>(gy) + 0.5) * kCS;
                const double vx[3] = { A.x - px, B.x - px, C.x - px };
                const double vy[3] = { A.y - py, B.y - py, C.y - py };
                bool ok = std::min({ vx[0], vx[1], vx[2] }) <= hcs && std::max({ vx[0], vx[1], vx[2] }) >= -hcs
                          && std::min({ vy[0], vy[1], vy[2] }) <= hcs && std::max({ vy[0], vy[1], vy[2] }) >= -hcs;
                for (int i = 0; ok && i < 3; ++i) {
                    const int i1 = (i + 1) % 3;
                    const double n0 = -(vy[i1] - vy[i]);
                    const double n1 = vx[i1] - vx[i];
                    const double p0 = vx[0] * n0 + vy[0] * n1;
                    const double p1 = vx[1] * n0 + vy[1] * n1;
                    const double p2 = vx[2] * n0 + vy[2] * n1;
                    const double rad = hcs * (std::abs(n0) + std::abs(n1));
                    ok = std::min({ p0, p1, p2 }) <= rad && std::max({ p0, p1, p2 }) >= -rad;
                }
                if (!ok) {
                    continue;
                }
                const double e1x = B.x - A.x, e1y = B.y - A.y;
                const double e2x = C.x - A.x, e2y = C.y - A.y;
                const double qx = px - A.x, qy = py - A.y;
                double den = e1x * e2y - e1y * e2x;
                if (std::abs(den) < 1e-12) {
                    den = 1e-12;
                }
                double t = (qx * e2y - qy * e2x) / den;
                double s = (e1x * qy - e1y * qx) / den;
                const bool inside = t >= -1e-12 && s >= -1e-12 && t + s <= 1 + 1e-12;
                t = std::clamp(t, 0.0, 1.0);
                s = std::clamp(s, 0.0, 1.0 - t);
                out.cell.push_back(gy * nx + gx);
                out.h.push_back(static_cast<float>(HA + t * (HB - HA) + s * (HC - HA)));
                out.ins.push_back(inside ? 1 : 0);
            }
        }
    }
    for (const int64_t ti : kept) {
        const WorldPoint& A = V[T[ti][0]];
        const WorldPoint& B = V[T[ti][1]];
        const WorldPoint& C = V[T[ti][2]];
        const double cx = (A.x + B.x + C.x) / 3.0;
        const double cy = (A.y + B.y + C.y) / 3.0;
        if (!(cx >= ox && cx < ox + static_cast<double>(nx) * kCS && cy >= oy && cy < oy + static_cast<double>(ny) * kCS)) {
            continue;
        }
        const int64_t gx = std::clamp<int64_t>(static_cast<int64_t>((cx - ox) / kCS), 0, nx - 1);
        const int64_t gy = std::clamp<int64_t>(static_cast<int64_t>((cy - oy) / kCS), 0, ny - 1);
        out.cell.push_back(gy * nx + gx);
        out.h.push_back(static_cast<float>((H[T[ti][0]] + H[T[ti][1]] + H[T[ti][2]]) / 3.0));
        out.ins.push_back(0);
    }
    return out;
}

SpanTable BuildSpans(const std::vector<int64_t>& cell, const std::vector<float>& h)
{
    SpanTable st;
    const int64_t n = static_cast<int64_t>(cell.size());
    if (n == 0) {
        return st;
    }
    std::vector<int64_t> ord(n);
    std::iota(ord.begin(), ord.end(), 0);
    std::stable_sort(ord.begin(), ord.end(), [&](int64_t a, int64_t b) {
        return cell[a] < cell[b] || (cell[a] == cell[b] && h[a] < h[b]);
    });
    double acc = 0.0;
    int64_t cnt = 0;
    for (int64_t i = 0; i < n; ++i) {
        const int64_t c = cell[ord[i]];
        const float hv = h[ord[i]];
        const bool fresh = i == 0 || c != cell[ord[i - 1]] || (hv - h[ord[i - 1]]) > 1.0f;
        if (fresh) {
            if (cnt) {
                st.sp_h.push_back(static_cast<float>(acc / static_cast<double>(cnt)));
            }
            st.sp_cell.push_back(c);
            acc = 0.0;
            cnt = 0;
        }
        acc += static_cast<double>(hv);
        ++cnt;
    }
    st.sp_h.push_back(static_cast<float>(acc / static_cast<double>(cnt)));

    const int64_t n_span = static_cast<int64_t>(st.sp_cell.size());
    for (int64_t i = 0; i < n_span; ++i) {
        if (i == 0 || st.sp_cell[i] != st.sp_cell[i - 1]) {
            st.occ.push_back(st.sp_cell[i]);
            st.cstart.push_back(i);
            st.ccnt.push_back(0);
        }
        ++st.ccnt.back();
    }
    st.K = *std::max_element(st.ccnt.begin(), st.ccnt.end());
    const int64_t n_occ = static_cast<int64_t>(st.occ.size());
    st.HK.assign(static_cast<size_t>(n_occ * st.K), std::numeric_limits<float>::infinity());
    st.IK.assign(static_cast<size_t>(n_occ * st.K), -1);
    st.sp_ci.resize(n_span);
    for (int64_t ci = 0, si = 0; ci < n_occ; ++ci) {
        for (int64_t r = 0; r < st.ccnt[ci]; ++r, ++si) {
            st.HK[ci * st.K + r] = st.sp_h[si];
            st.IK[ci * st.K + r] = si;
            st.sp_ci[si] = ci;
        }
    }
    return st;
}

void AppendSeamBridge(RasterCells& rc, int64_t nx, int64_t ny)
{
    const int64_t kb = static_cast<int64_t>(std::nearbyint(0.5 / kCS)) - 1;
    if (kb <= 0 || rc.cell.empty()) {
        return;
    }
    const SpanTable st = BuildSpans(rc.cell, rc.h);
    const int64_t K = st.K;
    std::vector<uint8_t> O2(static_cast<size_t>(nx * ny), 0);
    for (const int64_t c : st.occ) {
        O2[static_cast<size_t>(c)] = 1;
    }
    const auto occAt = [&](int64_t y, int64_t x) {
        return y >= 0 && y < ny && x >= 0 && x < nx && O2[static_cast<size_t>(y * nx + x)] != 0;
    };
    const int64_t dirs[2][2] = { { 0, 1 }, { 1, 0 } }; // (dy,dx)
    for (const auto& d : dirs) {
        const int64_t dy = d[0], dx = d[1];
        for (int64_t dl = 1; dl <= kb; ++dl) {
            for (int64_t dr = 1; dr <= kb + 1 - dl; ++dr) {
                for (int64_t y = 0; y < ny; ++y) {
                    for (int64_t x = 0; x < nx; ++x) {
                        if (O2[static_cast<size_t>(y * nx + x)]) {
                            continue;
                        }
                        bool m = true;
                        for (int64_t i = 1; m && i < dl; ++i) {
                            m = y + i * dy >= 0 && y + i * dy < ny && x + i * dx >= 0 && x + i * dx < nx
                                && !O2[static_cast<size_t>((y + i * dy) * nx + x + i * dx)];
                        }
                        for (int64_t i = 1; m && i < dr; ++i) {
                            m = y - i * dy >= 0 && y - i * dy < ny && x - i * dx >= 0 && x - i * dx < nx
                                && !O2[static_cast<size_t>((y - i * dy) * nx + x - i * dx)];
                        }
                        if (!m || !occAt(y + dl * dy, x + dl * dx) || !occAt(y - dr * dy, x - dr * dx)) {
                            continue;
                        }
                        const int64_t cid = y * nx + x;
                        const int64_t ja = occFind(st.occ, cid + dl * (dy * nx + dx));
                        const int64_t jb = occFind(st.occ, cid - dr * (dy * nx + dx));
                        // 空槽 inf-inf=nan 会毒化 argmin,两侧用相反哨兵
                        float best_dh = std::numeric_limits<float>::infinity();
                        float best_ha = 0.0f, best_hb = 0.0f;
                        for (int64_t p = 0; p < K; ++p) {
                            const float hka = st.HK[ja * K + p];
                            const float ha = std::isfinite(hka) ? hka : 1e9f;
                            for (int64_t q = 0; q < K; ++q) {
                                const float hkb = st.HK[jb * K + q];
                                const float hb = std::isfinite(hkb) ? hkb : -1e9f;
                                const float dh = std::fabs(ha - hb);
                                if (dh < best_dh) {
                                    best_dh = dh;
                                    best_ha = ha;
                                    best_hb = hb;
                                }
                            }
                        }
                        if (best_dh <= 3.0f) {
                            rc.cell.push_back(cid);
                            rc.h.push_back((best_ha + best_hb) * 0.5f);
                            rc.ins.push_back(0);
                        }
                    }
                }
            }
        }
    }
}

std::vector<uint8_t> Flood(int64_t seed, const SpanTable& st, int64_t nx)
{
    std::vector<uint8_t> vis(st.sp_h.size(), 0);
    vis[static_cast<size_t>(seed)] = 1;
    std::vector<int64_t> frontier { seed };
    const int64_t dirs[4][2] = { { 1, 0 }, { -1, 0 }, { 0, 1 }, { 0, -1 } }; // (dx,dy)
    while (!frontier.empty()) {
        std::vector<int64_t> next;
        for (const int64_t f : frontier) {
            const int64_t cid = st.occ[st.sp_ci[f]];
            const int64_t gx = cid % nx;
            for (const auto& d : dirs) {
                const int64_t dx = d[0], dy = d[1];
                if (dx != 0 && (gx + dx < 0 || gx + dx >= nx)) {
                    continue;
                }
                const int64_t j = occFind(st.occ, cid + dy * nx + dx);
                if (j < 0) {
                    continue;
                }
                for (int64_t slot = 0; slot < st.K; ++slot) {
                    if (!(std::fabs(st.HK[j * st.K + slot] - st.sp_h[f]) <= 3.0f)) {
                        continue;
                    }
                    const int64_t cand = st.IK[j * st.K + slot];
                    if (cand >= 0 && !vis[static_cast<size_t>(cand)]) {
                        vis[static_cast<size_t>(cand)] = 1;
                        next.push_back(cand);
                    }
                }
            }
        }
        frontier = std::move(next);
    }
    return vis;
}

Grid<float> Clearance(const Mask& mask)
{
    const int64_t ny = mask.ny, nx = mask.nx;
    const int64_t Rw = static_cast<int64_t>(std::ceil(kEdtCap / kCS)) + 1;
    const float BIG = static_cast<float>(Rw * 4);
    Grid<float> g2(nx, ny, 0.0f);
    std::vector<float> up(static_cast<size_t>(ny));
    for (int64_t x = 0; x < nx; ++x) {
        float runmax = -1e9f;
        for (int64_t y = 0; y < ny; ++y) {
            const float neg = mask.at(y, x) ? -1e9f : static_cast<float>(y);
            runmax = std::max(runmax, neg);
            up[static_cast<size_t>(y)] = static_cast<float>(y) - runmax;
        }
        float runmin = 1e9f;
        for (int64_t y = ny - 1; y >= 0; --y) {
            const float pos = mask.at(y, x) ? 1e9f : static_cast<float>(y);
            runmin = std::min(runmin, pos);
            const float dn = runmin - static_cast<float>(y);
            const float g = std::min(std::min(up[static_cast<size_t>(y)], dn), BIG);
            g2.at(y, x) = g * g;
        }
    }
    Grid<float> best = g2;
    std::vector<float> buf(static_cast<size_t>(nx));
    for (int64_t y = 0; y < ny; ++y) {
        std::copy_n(&g2.at(y, 0), nx, buf.begin());
        float* row = &best.at(y, 0);
        for (int64_t k = 1; k <= Rw; ++k) {
            const float kk = static_cast<float>(k * k);
            if (kk >= BIG * BIG) {
                break;
            }
            for (int64_t x = k; x < nx; ++x) {
                row[x] = std::min(row[x], buf[static_cast<size_t>(x - k)] + kk);
            }
            for (int64_t x = 0; x < nx - k; ++x) {
                row[x] = std::min(row[x], buf[static_cast<size_t>(x + k)] + kk);
            }
        }
    }
    Grid<float> out(nx, ny, 0.0f);
    for (int64_t i = 0; i < nx * ny; ++i) {
        if (mask.v[static_cast<size_t>(i)]) {
            out.v[static_cast<size_t>(i)] = std::min(std::sqrt(best.v[static_cast<size_t>(i)]) * 0.25f, 12.0f);
        }
    }
    return out;
}

std::vector<uint8_t> StampWalls(
    const std::vector<WorldPoint>& p0,
    const std::vector<WorldPoint>& p1,
    const std::vector<double>& hh,
    double ox,
    double oy,
    int64_t nx,
    int64_t ny,
    const SpanTable& st)
{
    std::vector<uint8_t> blocked(st.sp_h.size(), 0);
    for (size_t i = 0; i < p0.size(); ++i) {
        const double L = std::hypot(p1[i].x - p0[i].x, p1[i].y - p0[i].y);
        const int64_t steps = sampleSteps(L, 0.4);
        for (int64_t k = 0; k < steps; ++k) {
            const double t = static_cast<double>(k) / static_cast<double>(steps - 1);
            const double sx = p0[i].x + (p1[i].x - p0[i].x) * t;
            const double sy = p0[i].y + (p1[i].y - p0[i].y) * t;
            const int64_t gx = static_cast<int64_t>(std::floor((sx - ox) / kCS));
            const int64_t gy = static_cast<int64_t>(std::floor((sy - oy) / kCS));
            if (gx < 0 || gx >= nx || gy < 0 || gy >= ny) {
                continue;
            }
            const int64_t j = occFind(st.occ, gy * nx + gx);
            if (j < 0) {
                continue;
            }
            for (int64_t slot = 0; slot < st.K; ++slot) {
                if (std::abs(static_cast<double>(st.HK[j * st.K + slot]) - hh[i]) <= kMcHBand) {
                    const int64_t sid = st.IK[j * st.K + slot];
                    if (sid >= 0) {
                        blocked[static_cast<size_t>(sid)] = 1;
                    }
                }
            }
        }
    }
    return blocked;
}

std::vector<uint8_t> WallsAtLayer(
    const std::vector<WorldPoint>& p0,
    const std::vector<WorldPoint>& p1,
    const std::vector<double>& hh,
    const Grid<float>& lh,
    double ox,
    double oy)
{
    std::vector<uint8_t> keep(p0.size(), 0);
    for (size_t i = 0; i < p0.size(); ++i) {
        const double L = std::hypot(p1[i].x - p0[i].x, p1[i].y - p0[i].y);
        const int64_t steps = sampleSteps(L, 0.4);
        for (int64_t k = 0; k < steps && !keep[i]; ++k) {
            const double t = static_cast<double>(k) / static_cast<double>(steps - 1);
            const double sx = p0[i].x + (p1[i].x - p0[i].x) * t;
            const double sy = p0[i].y + (p1[i].y - p0[i].y) * t;
            const int64_t gx = static_cast<int64_t>(std::floor((sx - ox) / kCS));
            const int64_t gy = static_cast<int64_t>(std::floor((sy - oy) / kCS));
            if (gx < 0 || gx >= lh.nx || gy < 0 || gy >= lh.ny) {
                continue;
            }
            const float h = lh.at(gy, gx);
            if (!std::isnan(h) && std::abs(static_cast<double>(h) - hh[i]) <= kMcHBand) {
                keep[i] = 1;
            }
        }
    }
    return keep;
}

WallCsr BuildWallIndex(const std::vector<WorldPoint>& p0, const std::vector<WorldPoint>& p1, double ox, double oy, int64_t nx, int64_t ny)
{
    WallCsr csr;
    csr.start.assign(static_cast<size_t>(nx * ny + 1), 0);
    std::vector<std::pair<int64_t, int64_t>> entries;
    for (size_t i = 0; i < p0.size(); ++i) {
        const double L = std::hypot(p1[i].x - p0[i].x, p1[i].y - p0[i].y);
        const int64_t steps = sampleSteps(L, 0.2);
        for (int64_t k = 0; k < steps; ++k) {
            const double t = static_cast<double>(k) / static_cast<double>(steps - 1);
            const double sx = p0[i].x + (p1[i].x - p0[i].x) * t;
            const double sy = p0[i].y + (p1[i].y - p0[i].y) * t;
            const int64_t gx = static_cast<int64_t>(std::floor((sx - ox) / kCS));
            const int64_t gy = static_cast<int64_t>(std::floor((sy - oy) / kCS));
            if (gx >= 0 && gx < nx && gy >= 0 && gy < ny) {
                entries.emplace_back(gy * nx + gx, static_cast<int64_t>(i));
            }
        }
    }
    std::sort(entries.begin(), entries.end());
    entries.erase(std::unique(entries.begin(), entries.end()), entries.end());
    for (const auto& [cid, wid] : entries) {
        ++csr.start[static_cast<size_t>(cid + 1)];
        csr.wid.push_back(wid);
    }
    for (size_t i = 1; i < csr.start.size(); ++i) {
        csr.start[i] += csr.start[i - 1];
    }
    return csr;
}

std::unordered_set<int64_t> BannedSteps(
    const Mask& free,
    const WallCsr& csr,
    const std::vector<WorldPoint>& p0,
    const std::vector<WorldPoint>& p1,
    double ox,
    double oy)
{
    std::unordered_set<int64_t> out;
    if (p0.empty()) {
        return out;
    }
    const int64_t nx = free.nx, ny = free.ny;
    const int64_t NC = nx * ny;
    for (int64_t c = 0; c < NC; ++c) {
        if (csr.start[static_cast<size_t>(c + 1)] == csr.start[static_cast<size_t>(c)] || !free.v[static_cast<size_t>(c)]) {
            continue;
        }
        const double cx = (static_cast<double>(c % nx) + 0.5) * kCS + ox;
        const double cy = (static_cast<double>(c / nx) + 0.5) * kCS + oy;
        for (const auto& d : kNb8) {
            const int64_t ax = c % nx + d.dx, ay = c / nx + d.dy;
            if (ax < 0 || ax >= nx || ay < 0 || ay >= ny) {
                continue;
            }
            const int64_t b = ay * nx + ax;
            if (!free.v[static_cast<size_t>(b)]) {
                continue;
            }
            const double qx = cx + static_cast<double>(d.dx) * kCS;
            const double qy = cy + static_cast<double>(d.dy) * kCS;
            bool hit = false;
            for (const int64_t cell : { c, b }) {
                for (int64_t s = csr.start[static_cast<size_t>(cell)]; !hit && s < csr.start[static_cast<size_t>(cell + 1)]; ++s) {
                    const int64_t w = csr.wid[static_cast<size_t>(s)];
                    const double rx = qx - cx, ry = qy - cy;
                    const double sx = p1[w].x - p0[w].x, sy = p1[w].y - p0[w].y;
                    const double ux = p0[w].x - cx, uy = p0[w].y - cy;
                    const double den = rx * sy - ry * sx;
                    if (!(std::abs(den) > 1e-12)) {
                        continue;
                    }
                    const double t = (ux * sy - uy * sx) / den;
                    const double ww = (ux * ry - uy * rx) / den;
                    hit = t > 1e-9 && t < 1 - 1e-9 && ww > -1e-9 && ww < 1 + 1e-9;
                }
                if (hit) {
                    break;
                }
            }
            if (hit) {
                out.insert(c * NC + b);
                out.insert(b * NC + c);
            }
        }
    }
    return out;
}

std::vector<int64_t> Comps4(const Mask& mask)
{
    const int64_t ny = mask.ny, nx = mask.nx, NC = nx * ny;
    // 并查集按最小根合并 == comps4 标签传播的不动点(标签 = 分量最小格 id)
    std::vector<int64_t> par(static_cast<size_t>(NC));
    std::iota(par.begin(), par.end(), 0);
    const auto find = [&](int64_t x) {
        while (par[static_cast<size_t>(x)] != x) {
            par[static_cast<size_t>(x)] = par[static_cast<size_t>(par[static_cast<size_t>(x)])];
            x = par[static_cast<size_t>(x)];
        }
        return x;
    };
    for (int64_t y = 0; y < ny; ++y) {
        for (int64_t x = 0; x < nx; ++x) {
            if (!mask.at(y, x)) {
                continue;
            }
            const int64_t c = y * nx + x;
            if (x + 1 < nx && mask.at(y, x + 1)) {
                const int64_t ra = find(c), rb = find(c + 1);
                if (ra != rb) {
                    par[static_cast<size_t>(std::max(ra, rb))] = std::min(ra, rb);
                }
            }
            if (y + 1 < ny && mask.at(y + 1, x)) {
                const int64_t ra = find(c), rb = find(c + nx);
                if (ra != rb) {
                    par[static_cast<size_t>(std::max(ra, rb))] = std::min(ra, rb);
                }
            }
        }
    }
    std::vector<int64_t> lab(static_cast<size_t>(NC), -1);
    for (int64_t c = 0; c < NC; ++c) {
        if (mask.v[static_cast<size_t>(c)]) {
            lab[static_cast<size_t>(c)] = find(c);
        }
    }
    return lab;
}

Mask FillHoles(const Mask& mask, int64_t max_cells, const Mask* protect)
{
    const int64_t ny = mask.ny, nx = mask.nx;
    Mask inv(nx, ny, 0);
    for (size_t i = 0; i < mask.v.size(); ++i) {
        inv.v[i] = mask.v[i] ? 0 : 1;
    }
    const std::vector<int64_t> lab = Comps4(inv);
    std::unordered_set<int64_t> edge;
    for (int64_t x = 0; x < nx; ++x) {
        edge.insert(lab[static_cast<size_t>(x)]);
        edge.insert(lab[static_cast<size_t>((ny - 1) * nx + x)]);
    }
    for (int64_t y = 0; y < ny; ++y) {
        edge.insert(lab[static_cast<size_t>(y * nx)]);
        edge.insert(lab[static_cast<size_t>(y * nx + nx - 1)]);
    }
    std::unordered_map<int64_t, int64_t> cnt;
    std::unordered_set<int64_t> prot;
    for (int64_t c = 0; c < nx * ny; ++c) {
        const int64_t l = lab[static_cast<size_t>(c)];
        if (l < 0) {
            continue;
        }
        ++cnt[l];
        if (protect != nullptr && protect->v[static_cast<size_t>(c)]) {
            prot.insert(l);
        }
    }
    Mask out = mask;
    for (int64_t c = 0; c < nx * ny; ++c) {
        const int64_t l = lab[static_cast<size_t>(c)];
        if (l >= 0 && !edge.contains(l) && cnt[l] <= max_cells && !prot.contains(l)) {
            out.v[static_cast<size_t>(c)] = 1;
        }
    }
    return out;
}

Mask CloseCracks(const Mask& core, const Mask& lay, const Mask* protect)
{
    const int64_t k = std::max<int64_t>(1, static_cast<int64_t>(std::nearbyint(0.5 / kCS)));
    const int64_t ny = core.ny, nx = core.nx;
    Mask out = core;
    for (int it = 0; it < 4; ++it) {
        const auto shifted = [&](int64_t y, int64_t x) {
            return y >= 0 && y < ny && x >= 0 && x < nx && out.at(y, x) != 0;
        };
        int64_t n = 0;
        std::vector<int64_t> add;
        for (int64_t y = 0; y < ny; ++y) {
            for (int64_t x = 0; x < nx; ++x) {
                if (out.at(y, x) || !lay.at(y, x)) {
                    continue;
                }
                if (protect != nullptr && protect->at(y, x)) {
                    continue;
                }
                bool thin = false;
                for (int d = 0; !thin && d < 2; ++d) {
                    const int64_t dy = d == 0 ? 1 : 0, dx = d == 0 ? 0 : 1;
                    bool a = false, b = false;
                    for (int64_t i = 1; i <= k; ++i) {
                        a = a || shifted(y + i * dy, x + i * dx);
                        b = b || shifted(y - i * dy, x - i * dx);
                    }
                    thin = a && b;
                }
                if (thin) {
                    add.push_back(y * nx + x);
                    ++n;
                }
            }
        }
        if (n == 0) {
            break;
        }
        for (const int64_t c : add) {
            out.v[static_cast<size_t>(c)] = 1;
        }
    }
    return out;
}

std::optional<std::vector<CellPt>>
    CostAstar(const Mask& mask, CellPt s, CellPt g, const Grid<float>& mult, const std::unordered_set<int64_t>* banned, const double* bnp)
{
    const int64_t ny = mask.ny, nx = mask.nx;
    if (!mask.at(s.y, s.x) || !mask.at(g.y, g.x)) {
        return std::nullopt;
    }
    const int64_t NC = nx * ny;
    Grid<double> dist(nx, ny, std::numeric_limits<double>::infinity());
    Grid<int64_t> prev(nx, ny, -1);
    dist.at(s.y, s.x) = 0.0;
    using Node = std::tuple<double, int64_t, int64_t>;
    std::priority_queue<Node, std::vector<Node>, std::greater<Node>> pq;
    pq.emplace(0.0, s.x, s.y);
    while (!pq.empty()) {
        const auto [f, x, y] = pq.top();
        pq.pop();
        const double d0 = dist.at(y, x);
        if (f > d0 + std::hypot(static_cast<double>(g.x - x), static_cast<double>(g.y - y)) + 1e-9) {
            continue;
        }
        if (x == g.x && y == g.y) {
            break;
        }
        const float m0 = mult.at(y, x);
        for (const auto& d : kNb8) {
            const int64_t a = x + d.dx, b = y + d.dy;
            if (a < 0 || a >= nx || b < 0 || b >= ny || !mask.at(b, a)) {
                continue;
            }
            if (d.dx != 0 && d.dy != 0 && !(mask.at(y, a) && mask.at(b, x))) {
                continue;
            }
            double pen = 0.0;
            if (banned != nullptr && banned->contains((y * nx + x) * NC + (b * nx + a))) {
                if (bnp == nullptr) {
                    continue;
                }
                pen = *bnp;
            }
            // numpy 弱标量语义:步价在 f32 里算,再升 f64 与 d0/pen 相加
            const float step = static_cast<float>(d.w * 0.5) * (m0 + mult.at(b, a));
            const double nd = d0 + static_cast<double>(step) + pen;
            if (nd < dist.at(b, a) - 1e-12) {
                dist.at(b, a) = nd;
                prev.at(b, a) = y * nx + x;
                pq.emplace(nd + std::hypot(static_cast<double>(g.x - a), static_cast<double>(g.y - b)), a, b);
            }
        }
    }
    if (!std::isfinite(dist.at(g.y, g.x))) {
        return std::nullopt;
    }
    std::vector<CellPt> out { g };
    int64_t x = g.x, y = g.y;
    while (!(x == s.x && y == s.y)) {
        const int64_t p = prev.at(y, x);
        x = p % nx;
        y = p / nx;
        out.push_back({ x, y });
    }
    std::reverse(out.begin(), out.end());
    return out;
}

namespace
{

Grid<float> LocalMax(const Grid<float>& a, int64_t k)
{
    Grid<float> m = a;
    for (int ax = 0; ax < 2; ++ax) {
        Grid<float> acc = m;
        for (int64_t y = 0; y < m.ny; ++y) {
            for (int64_t x = 0; x < m.nx; ++x) {
                float v = acc.at(y, x);
                for (int64_t s = 1; s <= k; ++s) {
                    for (int sgn = 0; sgn < 2; ++sgn) {
                        const int64_t yy = ax == 0 ? y + (sgn == 0 ? -s : s) : y;
                        const int64_t xx = ax == 1 ? x + (sgn == 0 ? -s : s) : x;
                        if (yy >= 0 && yy < m.ny && xx >= 0 && xx < m.nx) {
                            v = std::max(v, m.at(yy, xx));
                        }
                    }
                }
                acc.at(y, x) = v;
            }
        }
        m = std::move(acc);
    }
    return m;
}

}

Grid<float> PrefField(const Grid<float>& dist, bool ridge)
{
    const Grid<float> locw = LocalMax(dist, static_cast<int64_t>(std::ceil(kR / kCS)));
    const int64_t ny = dist.ny, nx = dist.nx;
    Grid<float> pref(nx, ny, 0.0f);
    for (size_t i = 0; i < pref.v.size(); ++i) {
        pref.v[i] = std::max(std::min(1.75f, 0.6f * locw.v[i]), 0.25f);
    }
    if (!ridge) {
        return pref;
    }
    const float ninf = -std::numeric_limits<float>::infinity();
    const auto at = [&](int64_t y, int64_t x) {
        return y >= 0 && y < ny && x >= 0 && x < nx ? dist.at(y, x) : ninf;
    };
    const int64_t dirs[4][2] = { { 0, 1 }, { 1, 0 }, { 1, 1 }, { 1, -1 } }; // (dy,dx)
    Grid<float> out = pref;
    for (int64_t y = 0; y < ny; ++y) {
        for (int64_t x = 0; x < nx; ++x) {
            const float dv = dist.at(y, x);
            bool rg = false;
            for (const auto& d : dirs) {
                const float a = at(y + d[0], x + d[1]);
                const float b = at(y - d[0], x - d[1]);
                if (dv >= std::max(a, b) && dv > std::min(a, b)) {
                    rg = true;
                    break;
                }
            }
            if (rg && dv >= 0.5f) {
                out.at(y, x) = std::min(pref.at(y, x), dv);
            }
        }
    }
    return out;
}

namespace
{

// 边界侧序(dx,dy,角a,角b):自由区在行进方向左侧
const int64_t kSides[4][6] = {
    { 1, 0, 1, 0, 1, 1 },
    { 0, 1, 1, 1, 0, 1 },
    { -1, 0, 0, 1, 0, 0 },
    { 0, -1, 0, 0, 1, 0 },
};

}

std::vector<std::vector<WorldPoint>> TraceContours(const Mask& mask)
{
    const int64_t ny = mask.ny, nx = mask.nx;
    const int64_t W = nx + 1;
    const int64_t KK = W * (ny + 1);
    std::unordered_map<int64_t, std::vector<int64_t>> nxt;
    std::vector<int64_t> order;
    for (const auto& sd : kSides) {
        const int64_t dx = sd[0], dy = sd[1];
        for (int64_t y = 0; y < ny; ++y) {
            for (int64_t x = 0; x < nx; ++x) {
                if (!mask.at(y, x)) {
                    continue;
                }
                const int64_t yy = y + dy, xx = x + dx;
                if (yy >= 0 && yy < ny && xx >= 0 && xx < nx && mask.at(yy, xx)) {
                    continue;
                }
                const int64_t u = (y + sd[3]) * W + (x + sd[2]);
                const int64_t v = (y + sd[5]) * W + (x + sd[4]);
                auto [it, fresh] = nxt.try_emplace(u);
                if (fresh) {
                    order.push_back(u);
                }
                it->second.push_back(v);
            }
        }
    }
    std::vector<std::vector<WorldPoint>> loops;
    std::unordered_set<int64_t> used;
    for (const int64_t u0 : order) {
        for (const int64_t v0 : nxt[u0]) {
            if (used.contains(u0 * KK + v0)) {
                continue;
            }
            std::vector<int64_t> loop;
            int64_t u = u0, v = v0;
            while (true) {
                used.insert(u * KK + v);
                loop.push_back(u);
                const auto it = nxt.find(v);
                if (it == nxt.end() || it->second.empty()) {
                    break;
                }
                int64_t w = 0;
                if (it->second.size() == 1) {
                    w = it->second[0];
                }
                else {
                    // 岔口优先右转,与 A* 禁切角一致
                    const int64_t d0 = v % W - u % W, d1 = v / W - u / W;
                    int best = 10;
                    for (const int64_t z : it->second) {
                        const int64_t e0 = z % W - v % W, e1 = z / W - v / W;
                        int rank = 9;
                        if (e0 == d1 && e1 == -d0) {
                            rank = 0;
                        }
                        else if (e0 == d0 && e1 == d1) {
                            rank = 1;
                        }
                        else if (e0 == -d1 && e1 == d0) {
                            rank = 2;
                        }
                        else if (e0 == -d0 && e1 == -d1) {
                            rank = 3;
                        }
                        if (rank < best) {
                            best = rank;
                            w = z;
                        }
                    }
                }
                if (used.contains(v * KK + w)) {
                    break;
                }
                u = v;
                v = w;
            }
            if (loop.size() >= 4) {
                std::vector<WorldPoint> pts;
                pts.reserve(loop.size());
                for (const int64_t p : loop) {
                    pts.push_back({ static_cast<double>(p % W), static_cast<double>(p / W) });
                }
                loops.push_back(std::move(pts));
            }
        }
    }
    return loops;
}

namespace
{

int64_t DpSplit(const std::vector<WorldPoint>& P, int64_t i0, int64_t i1, double max_err)
{
    const int64_t n = static_cast<int64_t>(P.size());
    const double dx = P[i1].x - P[i0].x, dy = P[i1].y - P[i0].y;
    const double L2 = dx * dx + dy * dy;
    double best = max_err * max_err;
    int64_t bi = -1;
    for (int64_t i = (i0 + 1) % n; i != i1; i = (i + 1) % n) {
        const double qx = P[i].x - P[i0].x, qy = P[i].y - P[i0].y;
        double ex = qx, ey = qy;
        if (L2 > 1e-12) {
            const double t = std::clamp((qx * dx + qy * dy) / L2, 0.0, 1.0);
            ex = qx - dx * t;
            ey = qy - dy * t;
        }
        const double dd = ex * ex + ey * ey;
        if (dd > best) {
            best = dd;
            bi = i;
        }
    }
    return bi;
}

}

std::vector<WorldPoint> SimplifyLoop(const std::vector<WorldPoint>& P, double max_err)
{
    const int64_t n = static_cast<int64_t>(P.size());
    if (n <= 4) {
        return P;
    }
    int64_t ll = 0, ur = 0;
    for (int64_t i = 1; i < n; ++i) {
        if (P[i].x < P[ll].x || (P[i].x == P[ll].x && P[i].y < P[ll].y)) {
            ll = i;
        }
        if (P[i].x > P[ur].x || (P[i].x == P[ur].x && P[i].y >= P[ur].y)) {
            ur = i;
        }
    }
    if (ll == ur) {
        return P;
    }
    std::vector<int64_t> keep { ll, ur };
    size_t i = 0;
    while (i < keep.size()) {
        const int64_t a = keep[i];
        const int64_t b = keep[(i + 1) % keep.size()];
        const int64_t bi = DpSplit(P, a, b, max_err);
        if (bi >= 0) {
            keep.insert(keep.begin() + static_cast<int64_t>(i) + 1, bi);
        }
        else {
            ++i;
        }
    }
    std::vector<WorldPoint> out;
    out.reserve(keep.size());
    for (const int64_t idx : keep) {
        out.push_back(P[idx]);
    }
    return out;
}

Blockers::Blockers(
    const std::vector<std::vector<WorldPoint>>& loops,
    const std::vector<WorldPoint>* extra_a,
    const std::vector<WorldPoint>* extra_b,
    std::optional<OnMask> on)
    : on_(on)
{
    for (const auto& P : loops) {
        for (size_t i = 0; i < P.size(); ++i) {
            a_.push_back(P[i]);
            b_.push_back(P[(i + 1) % P.size()]);
        }
    }
    if (extra_a != nullptr && !extra_a->empty()) {
        a_.insert(a_.end(), extra_a->begin(), extra_a->end());
        b_.insert(b_.end(), extra_b->begin(), extra_b->end());
    }
    lo_.reserve(a_.size());
    hi_.reserve(a_.size());
    for (size_t i = 0; i < a_.size(); ++i) {
        lo_.push_back({ std::min(a_[i].x, b_[i].x), std::min(a_[i].y, b_[i].y) });
        hi_.push_back({ std::max(a_[i].x, b_[i].x), std::max(a_[i].y, b_[i].y) });
    }
}

bool Blockers::blocked(const WorldPoint& p, const WorldPoint& q) const
{
    constexpr double eps = 1e-7;
    const double lox = std::min(p.x, q.x) - eps, hix = std::max(p.x, q.x) + eps;
    const double loy = std::min(p.y, q.y) - eps, hiy = std::max(p.y, q.y) + eps;
    const double rx = q.x - p.x, ry = q.y - p.y;
    for (size_t i = 0; i < a_.size(); ++i) {
        if (hi_[i].x < lox || lo_[i].x > hix || hi_[i].y < loy || lo_[i].y > hiy) {
            continue;
        }
        const double sx = b_[i].x - a_[i].x, sy = b_[i].y - a_[i].y;
        const double den = rx * sy - ry * sx;
        if (!(std::abs(den) > 1e-12)) {
            continue;
        }
        const double ux = a_[i].x - p.x, uy = a_[i].y - p.y;
        const double t = (ux * sy - uy * sx) / den;
        const double w = (ux * ry - uy * rx) / den;
        if (t > eps && t < 1 - eps && w > eps && w < 1 - eps) {
            return true;
        }
    }
    return offMask(p, q);
}

bool Blockers::offMask(const WorldPoint& p, const WorldPoint& q) const
{
    if (!on_) {
        return false;
    }
    const Mask& msk = *on_->mask;
    const double L = std::hypot(q.x - p.x, q.y - p.y);
    const int64_t n = static_cast<int64_t>(L / (on_->cs * 0.5)) + 2;
    const double step = 1.0 / static_cast<double>(n - 1);
    for (int64_t i = 0; i < n; ++i) {
        const double t = i == n - 1 ? 1.0 : static_cast<double>(i) * step;
        const int64_t gx = static_cast<int64_t>((p.x + (q.x - p.x) * t - on_->x0) / on_->cs);
        const int64_t gy = static_cast<int64_t>((p.y + (q.y - p.y) * t - on_->y0) / on_->cs);
        if (gx < 0 || gy < 0 || gx >= msk.nx || gy >= msk.ny || !msk.at(gy, gx)) {
            return true;
        }
    }
    return false;
}

std::vector<WorldPoint> StringPull(const std::vector<WorldPoint>& pts, const Blockers& blk)
{
    std::vector<WorldPoint> P = pts;
    for (int round = 0; round < 6; ++round) {
        std::vector<WorldPoint> out { P[0] };
        size_t i = 0;
        while (i < P.size() - 1) {
            size_t j = P.size() - 1;
            while (j > i + 1 && blk.blocked(P[i], P[j])) {
                --j;
            }
            out.push_back(P[j]);
            i = j;
        }
        const bool changed = out.size() != P.size();
        P = std::move(out);
        if (!changed) {
            break;
        }
    }
    return P;
}

std::vector<WorldPoint> Slim(const std::vector<WorldPoint>& pts, const Blockers& blk, double eps)
{
    std::vector<WorldPoint> P = pts;
    bool ch = true;
    while (ch) {
        ch = false;
        size_t i = 1;
        while (i + 1 < P.size()) {
            const WorldPoint &a = P[i - 1], &b = P[i], &c = P[i + 1];
            const double ux = c.x - a.x, uy = c.y - a.y;
            const double L2 = ux * ux + uy * uy;
            const double t = L2 == 0.0 ? 0.0 : std::max(0.0, std::min(1.0, ((b.x - a.x) * ux + (b.y - a.y) * uy) / L2));
            const double d = std::hypot(b.x - a.x - t * ux, b.y - a.y - t * uy);
            if (d <= eps && !blk.blocked(a, c)) {
                P.erase(P.begin() + static_cast<int64_t>(i));
                ch = true;
            }
            else {
                ++i;
            }
        }
    }
    return P;
}

std::vector<WorldPoint> DropLoops(const std::vector<WorldPoint>& pts)
{
    constexpr double eps = 1e-9;
    std::vector<WorldPoint> P = pts;
    bool changed = true;
    while (changed && P.size() > 3) {
        changed = false;
        for (size_t i = 0; i + 1 < P.size() && !changed; ++i) {
            for (size_t j = i + 2; j + 1 < P.size(); ++j) {
                const WorldPoint &a = P[i], &b = P[i + 1], &c = P[j], &d = P[j + 1];
                const double rx = b.x - a.x, ry = b.y - a.y;
                const double sx = d.x - c.x, sy = d.y - c.y;
                const double den = rx * sy - ry * sx;
                if (std::abs(den) < eps) {
                    continue;
                }
                const double t = ((c.x - a.x) * sy - (c.y - a.y) * sx) / den;
                const double u = ((c.x - a.x) * ry - (c.y - a.y) * rx) / den;
                if (!(eps < t && t < 1 - eps && eps < u && u < 1 - eps)) {
                    continue;
                }
                const WorldPoint x { a.x + rx * t, a.y + ry * t };
                std::vector<WorldPoint> np(P.begin(), P.begin() + static_cast<int64_t>(i) + 1);
                np.push_back(x);
                np.insert(np.end(), P.begin() + static_cast<int64_t>(j) + 1, P.end());
                P = std::move(np);
                changed = true;
                break;
            }
        }
    }
    return P;
}

}
