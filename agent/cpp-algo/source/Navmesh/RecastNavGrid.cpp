#include "RecastNavGrid.h"

#include <algorithm>
#include <cmath>
#include <limits>
#include <numbers>
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

int64_t sampleSteps(double len, double sub)
{
    return std::max<int64_t>(static_cast<int64_t>(std::ceil(len / (kCS * sub))), 1) + 1;
}

int64_t occFind(const SpanTable& st, int64_t cid)
{
    return st.j(cid);
}

bool levelAt(const SpanTable& st, int64_t nx, int64_t ny, int64_t cid, int64_t dx, int64_t dy, int64_t s, float h, double tol)
{
    const int64_t ax = cid % nx + dx * s;
    const int64_t ay = cid / nx + dy * s;
    if (ax < 0 || ax >= nx || ay < 0 || ay >= ny) {
        return false;
    }
    const int64_t j = occFind(st, ay * nx + ax);
    if (j < 0) {
        return false;
    }
    const int64_t b = st.cstart(j);
    const int64_t n = st.ccnt(j);
    for (int64_t k = 0; k < n; ++k) {
        if (std::fabs(static_cast<double>(st.sp_h[static_cast<size_t>(b + k)]) - static_cast<double>(h)) <= tol) {
            return true;
        }
    }
    return false;
}

bool rasterFace(const SpanTable& st, int64_t nx, int64_t ny, int64_t cid, int64_t dx, int64_t dy, int64_t s)
{
    const int64_t ax = cid % nx + dx * s;
    const int64_t ay = cid / nx + dy * s;
    if (ax < 0 || ax >= nx || ay < 0 || ay >= ny) {
        return false;
    }
    const int64_t j = occFind(st, ay * nx + ax);
    return j >= 0 && st.face[static_cast<size_t>(j)] != 0;
}

}

bool RiseOk(const SpanTable& st, int64_t nx, int64_t ny, int64_t cid, int64_t dx, int64_t dy, float h0, float h1)
{
    const double dh = static_cast<double>(h1) - static_cast<double>(h0);
    if (dh < -kDrop) {
        return false;
    }
    // 出发那层几格内又回来说明脚下只是一道缝, 不许掉进去
    if (dh < -kClimb) {
        for (int64_t s = 1; s <= kSeamCells; ++s) {
            if (levelAt(st, nx, ny, cid, dx, dy, s, h0, kClimb)) {
                return false;
            }
        }
        if (st.fall && !st.fall(cid, cid + dy * nx + dx, h0, h1)) {
            return false;
        }
    }
    // 坡度口径内立面与平地结论相同, 所以可以先于立面判据放行
    if (dh <= kSlope * kCS) {
        return true;
    }
    const double w = std::hypot(static_cast<double>(dx), static_cast<double>(dy));
    if (dh <= kSlope * w * kCS) {
        return true;
    }
    // 立面上只按坡度放行, 否则能顺着叠层逐格爬上整堵墙
    if (rasterFace(st, nx, ny, cid, dx, dy, 0) || rasterFace(st, nx, ny, cid, dx, dy, 1)) {
        return false;
    }
    if (dh <= kStepUp) {
        return true;
    }
    if (dh > kBumpUp) {
        return false;
    }
    for (int64_t s = 2; s <= kBumpCells; ++s) {
        // 前方回到出发高度而无目标高度即窄凸起, 两个高度都在是叠层, 不算凸起
        if (levelAt(st, nx, ny, cid, dx, dy, s, h0, kStepUp) && !levelAt(st, nx, ny, cid, dx, dy, s, h1, kStepUp)) {
            return true;
        }
    }
    for (int64_t s = 1; s <= kDipCells; ++s) {
        // 身后有目标高度而无出发高度即浅坑, 两个高度都在是叠层, 不算浅坑
        if (levelAt(st, nx, ny, cid, dx, dy, -s, h1, kStepUp) && !levelAt(st, nx, ny, cid, dx, dy, -s, h0, kStepUp)) {
            return true;
        }
    }
    return false;
}

namespace
{

static_assert(kClimb <= kDrop);

inline bool riseOkFast(const SpanTable& st, int64_t nx, int64_t ny, int64_t cid, int64_t dx, int64_t dy, float h0, float h1)
{
    const double dh = static_cast<double>(h1) - static_cast<double>(h0);
    return (dh >= -kClimb && dh <= kSlope * kCS) || RiseOk(st, nx, ny, cid, dx, dy, h0, h1);
}

class MinHeap4
{
public:
    bool empty() const { return a_.empty(); }

    std::pair<double, int64_t> top() const { return { a_.front().f, a_.front().u }; }

    void push(double f, int64_t u)
    {
        const Node n { f, u };
        size_t i = a_.size();
        a_.push_back(n);
        while (i > 0) {
            const size_t p = (i - 1) / 4;
            if (!less(n, a_[p])) {
                break;
            }
            a_[i] = a_[p];
            i = p;
        }
        a_[i] = n;
    }

    void pop()
    {
        const Node last = a_.back();
        a_.pop_back();
        const size_t n = a_.size();
        if (n == 0) {
            return;
        }
        size_t i = 0;
        while (true) {
            const size_t c = 4 * i + 1;
            if (c >= n) {
                break;
            }
            size_t m = c;
            const size_t e = std::min(c + 4, n);
            for (size_t k = c + 1; k < e; ++k) {
                if (less(a_[k], a_[m])) {
                    m = k;
                }
            }
            if (!less(a_[m], last)) {
                break;
            }
            a_[i] = a_[m];
            i = m;
        }
        a_[i] = last;
    }

private:
    struct Node
    {
        double f;
        int64_t u;
    };

    static bool less(const Node& x, const Node& y) { return x.f < y.f || (!(y.f < x.f) && x.u < y.u); }

    std::vector<Node> a_;
};

}

RasterCells Rasterize(
    const BaseNavVertex* V,
    const std::vector<std::array<int32_t, 3>>& T,
    double ox,
    double oy,
    int64_t nx,
    int64_t ny,
    const std::vector<uint8_t>* walkable)
{
    RasterCells out;
    const double hcs = kCS * 0.5;
    const auto P = [V](int32_t i) {
        return WorldPoint { static_cast<double>(V[i].u), static_cast<double>(V[i].v) };
    };
    const auto H = [V](int32_t i) {
        return static_cast<double>(V[i].height);
    };
    std::vector<int64_t> kept;
    kept.reserve(T.size());
    for (int64_t ti = 0; ti < static_cast<int64_t>(T.size()); ++ti) {
        if (walkable != nullptr && (*walkable)[static_cast<size_t>(ti)] == 0) {
            continue;
        }
        const WorldPoint A = P(T[ti][0]);
        const WorldPoint B = P(T[ti][1]);
        const WorldPoint C = P(T[ti][2]);
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
        const double HA = H(T[ti][0]);
        const double HB = H(T[ti][1]);
        const double HC = H(T[ti][2]);
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
        const WorldPoint A = P(T[ti][0]);
        const WorldPoint B = P(T[ti][1]);
        const WorldPoint C = P(T[ti][2]);
        const double cx = (A.x + B.x + C.x) / 3.0;
        const double cy = (A.y + B.y + C.y) / 3.0;
        if (!(cx >= ox && cx < ox + static_cast<double>(nx) * kCS && cy >= oy && cy < oy + static_cast<double>(ny) * kCS)) {
            continue;
        }
        const int64_t gx = std::clamp<int64_t>(static_cast<int64_t>((cx - ox) / kCS), 0, nx - 1);
        const int64_t gy = std::clamp<int64_t>(static_cast<int64_t>((cy - oy) / kCS), 0, ny - 1);
        out.cell.push_back(gy * nx + gx);
        out.h.push_back(static_cast<float>((H(T[ti][0]) + H(T[ti][1]) + H(T[ti][2])) / 3.0));
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
    float anchor = 0.0F;
    for (int64_t i = 0; i < n; ++i) {
        const int64_t c = cell[ord[i]];
        const float hv = h[ord[i]];
        const bool fresh = i == 0 || c != cell[ord[i - 1]] || (static_cast<double>(hv) - static_cast<double>(anchor)) > kMergeH;
        if (fresh) {
            if (cnt) {
                st.sp_h.push_back(static_cast<float>(acc / static_cast<double>(cnt)));
            }
            st.sp_cell.push_back(static_cast<int32_t>(c));
            acc = 0.0;
            cnt = 0;
            anchor = hv;
        }
        acc += static_cast<double>(hv);
        ++cnt;
    }
    st.sp_h.push_back(static_cast<float>(acc / static_cast<double>(cnt)));
    return PackSpans(std::move(st.sp_cell), std::move(st.sp_h));
}

SpanTable PackSpans(std::vector<int32_t> cell, std::vector<float> h, std::vector<uint8_t>* flags, std::vector<uint32_t>* aux)
{
    SpanTable st;
    const int64_t n_span = static_cast<int64_t>(cell.size());
    if (n_span == 0) {
        return st;
    }
    // 两级计数排序加同格插入排序, 结果须与按 (cell, h) 稳定排序同序
    const size_t n = static_cast<size_t>(n_span);
    const int32_t c_max = *std::max_element(cell.begin(), cell.end());
    const size_t n_hi = (static_cast<size_t>(c_max) >> 16U) + 1;
    std::vector<int32_t> ord(n);
    std::vector<int32_t> hi_start(n_hi + 1, 0);
    for (const int32_t c : cell) {
        ++hi_start[(static_cast<size_t>(c) >> 16U) + 1];
    }
    size_t bucket_max = 0;
    for (size_t k = 1; k <= n_hi; ++k) {
        bucket_max = std::max(bucket_max, static_cast<size_t>(hi_start[k]));
        hi_start[k] += hi_start[k - 1];
    }
    {
        std::vector<int32_t> cursor(hi_start.begin(), hi_start.end() - 1);
        for (size_t i = 0; i < n; ++i) {
            ord[static_cast<size_t>(cursor[static_cast<size_t>(cell[i]) >> 16U]++)] = static_cast<int32_t>(i);
        }
    }
    {
        std::vector<int32_t> lo_start(65537, 0);
        std::vector<int32_t> tmp(bucket_max);
        for (size_t k = 0; k < n_hi; ++k) {
            const size_t b = static_cast<size_t>(hi_start[k]), e = static_cast<size_t>(hi_start[k + 1]);
            if (e - b < 2) {
                continue;
            }
            std::fill(lo_start.begin(), lo_start.end(), 0);
            for (size_t i = b; i < e; ++i) {
                ++lo_start[(static_cast<size_t>(cell[static_cast<size_t>(ord[i])]) & 0xFFFFU) + 1];
            }
            for (size_t d = 1; d < lo_start.size(); ++d) {
                lo_start[d] += lo_start[d - 1];
            }
            for (size_t i = b; i < e; ++i) {
                const int32_t v = ord[i];
                tmp[static_cast<size_t>(lo_start[static_cast<size_t>(cell[static_cast<size_t>(v)]) & 0xFFFFU]++)] = v;
            }
            std::copy(tmp.begin(), tmp.begin() + static_cast<std::ptrdiff_t>(e - b), ord.begin() + static_cast<std::ptrdiff_t>(b));
        }
    }
    // 每搬完一列即释放对应入参, 压低峰值内存
    st.sp_cell.resize(n);
    for (size_t i = 0; i < n; ++i) {
        st.sp_cell[i] = cell[static_cast<size_t>(ord[i])];
    }
    cell = std::vector<int32_t>();
    for (size_t b = 0; b < n;) {
        size_t e = b + 1;
        while (e < n && st.sp_cell[e] == st.sp_cell[b]) {
            ++e;
        }
        for (size_t i = b + 1; i < e; ++i) {
            const int32_t v = ord[i];
            const float hv = h[static_cast<size_t>(v)];
            size_t j = i;
            while (j > b && h[static_cast<size_t>(ord[j - 1])] > hv) {
                ord[j] = ord[j - 1];
                --j;
            }
            ord[j] = v;
        }
        b = e;
    }
    st.sp_h.resize(n);
    for (size_t i = 0; i < n; ++i) {
        st.sp_h[i] = h[static_cast<size_t>(ord[i])];
    }
    h = std::vector<float>();
    if (flags != nullptr) {
        std::vector<uint8_t> fo(n);
        for (size_t i = 0; i < n; ++i) {
            fo[i] = (*flags)[static_cast<size_t>(ord[i])];
        }
        flags->swap(fo);
    }
    if (aux != nullptr) {
        std::vector<uint32_t> ao(n);
        for (size_t i = 0; i < n; ++i) {
            ao[i] = (*aux)[static_cast<size_t>(ord[i])];
        }
        aux->swap(ao);
    }
    ord = std::vector<int32_t>();
    int64_t n_occ = 0;
    for (int64_t i = 0; i < n_span; ++i) {
        if (i == 0 || st.sp_cell[static_cast<size_t>(i)] != st.sp_cell[static_cast<size_t>(i - 1)]) {
            ++n_occ;
        }
    }
    st.cs.resize(static_cast<size_t>(n_occ) + 1);
    st.cs[static_cast<size_t>(n_occ)] = static_cast<int32_t>(n_span);
    for (int64_t i = 0, ci = 0; i < n_span; ++i) {
        if (i == 0 || st.sp_cell[static_cast<size_t>(i)] != st.sp_cell[static_cast<size_t>(i - 1)]) {
            st.cs[static_cast<size_t>(ci++)] = static_cast<int32_t>(i);
        }
    }
    const size_t words = static_cast<size_t>(st.sp_cell.back() >> 6) + 1;
    st.occ_bits.assign(words, 0);
    st.occ_rank.assign(words, 0);
    for (int64_t ci = 0; ci < n_occ; ++ci) {
        const int64_t cid = st.occ(ci);
        st.occ_bits[static_cast<size_t>(cid >> 6)] |= uint64_t { 1 } << (cid & 63);
    }
    for (size_t w = 0, acc = 0; w < words; ++w) {
        st.occ_rank[w] = static_cast<int32_t>(acc);
        acc += static_cast<size_t>(std::popcount(st.occ_bits[w]));
    }
    st.face.assign(static_cast<size_t>(n_occ), 0);
    for (int64_t ci = 0; ci < n_occ; ++ci) {
        // 首末即该列最低最高, 恰好两张是地面加顶盖, 不算立面
        const int64_t b = st.cstart(ci);
        const int64_t c = st.ccnt(ci);
        const double lo = st.sp_h[static_cast<size_t>(b)];
        const double hi = st.sp_h[static_cast<size_t>(b + c - 1)];
        st.face[static_cast<size_t>(ci)] = static_cast<uint8_t>(c != 2 && hi - lo > kClimb);
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
    std::vector<uint8_t> O2(static_cast<size_t>(nx * ny), 0);
    for (int64_t ci = 0, cn = st.nOcc(); ci < cn; ++ci) {
        O2[static_cast<size_t>(st.occ(ci))] = 1;
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
                        const int64_t ja = occFind(st, cid + dl * (dy * nx + dx));
                        const int64_t jb = occFind(st, cid - dr * (dy * nx + dx));
                        float best_dh = std::numeric_limits<float>::infinity();
                        float best_ha = 0.0f, best_hb = 0.0f;
                        const int64_t ba = st.cstart(ja), na = st.ccnt(ja);
                        const int64_t bb = st.cstart(jb), nb = st.ccnt(jb);
                        for (int64_t p = 0; p < na; ++p) {
                            const float ha = st.sp_h[static_cast<size_t>(ba + p)];
                            for (int64_t q = 0; q < nb; ++q) {
                                const float hb = st.sp_h[static_cast<size_t>(bb + q)];
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
            const int64_t cid = st.sp_cell[f];
            const int64_t gx = cid % nx;
            for (const auto& d : dirs) {
                const int64_t dx = d[0], dy = d[1];
                if (dx != 0 && (gx + dx < 0 || gx + dx >= nx)) {
                    continue;
                }
                const int64_t j = occFind(st, cid + dy * nx + dx);
                if (j < 0) {
                    continue;
                }
                const int64_t b = st.cstart(j), n = st.ccnt(j);
                for (int64_t slot = 0; slot < n; ++slot) {
                    const int64_t cand = b + slot;
                    if (!(std::fabs(st.sp_h[static_cast<size_t>(cand)] - st.sp_h[f]) <= 3.0f)) {
                        continue;
                    }
                    if (!vis[static_cast<size_t>(cand)]) {
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

std::vector<int32_t> LabelRegions(const SpanTable& st, int64_t nx)
{
    const int64_t n = static_cast<int64_t>(st.sp_h.size());
    std::vector<int32_t> parent(static_cast<size_t>(n));
    std::iota(parent.begin(), parent.end(), 0);
    const auto find = [&](int32_t a) {
        while (parent[static_cast<size_t>(a)] != a) {
            parent[static_cast<size_t>(a)] = parent[static_cast<size_t>(parent[static_cast<size_t>(a)])];
            a = parent[static_cast<size_t>(a)];
        }
        return a;
    };
    const int64_t dirs[2][2] = { { 1, 0 }, { 0, 1 } };
    for (int64_t ci = 0, cn = st.nOcc(); ci < cn; ++ci) {
        const int64_t cid = st.occ(ci);
        const int64_t gx = cid % nx;
        for (const auto& d : dirs) {
            if (d[0] != 0 && gx + d[0] >= nx) {
                continue;
            }
            const int64_t j = occFind(st, cid + d[1] * nx + d[0]);
            if (j < 0) {
                continue;
            }
            const int64_t jb = st.cstart(j), jn = st.ccnt(j);
            for (int64_t r = 0; r < st.ccnt(ci); ++r) {
                const int64_t u = st.cstart(ci) + r;
                for (int64_t slot = 0; slot < jn; ++slot) {
                    const int64_t v = jb + slot;
                    if (!(std::fabs(st.sp_h[static_cast<size_t>(v)] - st.sp_h[static_cast<size_t>(u)]) <= 3.0f)) {
                        continue;
                    }
                    const int32_t a = find(static_cast<int32_t>(u));
                    const int32_t b = find(static_cast<int32_t>(v));
                    if (a != b) {
                        parent[static_cast<size_t>(a)] = b;
                    }
                }
            }
        }
    }
    for (int64_t i = 0; i < n; ++i) {
        parent[static_cast<size_t>(i)] = find(static_cast<int32_t>(i));
    }
    return parent;
}

std::vector<uint8_t> SpanReach(int64_t seed, const SpanTable& st, const std::vector<uint8_t>& ok, int64_t nx, int64_t ny)
{
    std::vector<uint8_t> vis(st.sp_h.size(), 0);
    if (seed < 0 || ok[static_cast<size_t>(seed)] == 0) {
        return vis;
    }
    vis[static_cast<size_t>(seed)] = 1;
    std::vector<int64_t> frontier { seed };
    while (!frontier.empty()) {
        std::vector<int64_t> next;
        for (const int64_t f : frontier) {
            const int64_t cid = st.sp_cell[f];
            const int64_t gx = cid % nx, gy = cid / nx;
            for (const auto& d : kNb8) {
                const int64_t ax = gx + d.dx, ay = gy + d.dy;
                if (ax < 0 || ax >= nx || ay < 0 || ay >= ny) {
                    continue;
                }
                const int64_t j = occFind(st, ay * nx + ax);
                if (j < 0) {
                    continue;
                }
                const int64_t b = st.cstart(j), n = st.ccnt(j);
                for (int64_t slot = 0; slot < n; ++slot) {
                    const int64_t cand = b + slot;
                    if (ok[static_cast<size_t>(cand)] == 0 || vis[static_cast<size_t>(cand)]) {
                        continue;
                    }
                    if (!RiseOk(st, nx, ny, cid, d.dx, d.dy, st.sp_h[f], st.sp_h[static_cast<size_t>(cand)])) {
                        continue;
                    }
                    vis[static_cast<size_t>(cand)] = 1;
                    next.push_back(cand);
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
            const int64_t j = occFind(st, gy * nx + gx);
            if (j < 0) {
                continue;
            }
            const int64_t b = st.cstart(j), n = st.ccnt(j);
            for (int64_t slot = 0; slot < n; ++slot) {
                const int64_t sid = b + slot;
                if (std::abs(static_cast<double>(st.sp_h[static_cast<size_t>(sid)]) - hh[i]) <= kMcHBand) {
                    blocked[static_cast<size_t>(sid)] = 1;
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

std::vector<int64_t> Comps4(const Mask& mask)
{
    const int64_t ny = mask.ny, nx = mask.nx, NC = nx * ny;
    // 按最小根合并, 标签即分量内最小格号
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

std::optional<std::vector<CellPt>> CostAstar(
    const Mask& mask,
    CellPt s,
    CellPt g,
    const PriceField& mult,
    const EdgeBits* banned,
    const double* bnp,
    const EdgeBits* forbidden,
    double* out_cost,
    const JumpEdges* jumps)
{
    const int64_t ny = mask.ny, nx = mask.nx;
    if (!mask.at(s.y, s.x) || !mask.at(g.y, g.x)) {
        return std::nullopt;
    }
    const bool hj = jumps != nullptr && !jumps->empty();
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
            const int64_t cu = y * nx + x;
            const int64_t cv = b * nx + a;
            if (forbidden != nullptr && forbidden->has(cu, cv)) {
                continue;
            }
            double pen = 0.0;
            if (banned != nullptr && banned->has(cu, cv)) {
                if (bnp == nullptr) {
                    continue;
                }
                pen = *bnp;
            }
            const float step = static_cast<float>(d.w * 0.5) * (m0 + mult.at(b, a));
            const double nd = d0 + static_cast<double>(step) + pen;
            if (nd < dist.at(b, a) - 1e-12) {
                dist.at(b, a) = nd;
                prev.at(b, a) = y * nx + x;
                pq.emplace(nd + std::hypot(static_cast<double>(g.x - a), static_cast<double>(g.y - b)), a, b);
            }
        }
        if (hj) {
            const int64_t cu = y * nx + x;
            auto [ji, jn] = jumps->from(cu);
            for (; ji < jn; ++ji) {
                const JumpEdges::Edge& je = jumps->e[ji];
                const int64_t a = je.dst % nx, b = je.dst / nx;
                if (!mask.at(b, a)) {
                    continue;
                }
                const double nd = d0 + static_cast<double>(je.cost);
                if (nd < dist.at(b, a) - 1e-12) {
                    dist.at(b, a) = nd;
                    prev.at(b, a) = cu;
                    pq.emplace(nd + std::hypot(static_cast<double>(g.x - a), static_cast<double>(g.y - b)), a, b);
                }
            }
        }
    }
    if (!std::isfinite(dist.at(g.y, g.x))) {
        return std::nullopt;
    }
    if (out_cost != nullptr) {
        *out_cost = dist.at(g.y, g.x);
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

bool Visibility::crossesStep(const WorldPoint& p, const WorldPoint& q) const
{
    const bool hs = steps_ != nullptr && !steps_->empty();
    const bool hf = faces_ != nullptr && !faces_->empty();
    if (!hs && !hf) {
        return false;
    }
    // 须与 LayerOracle::walk 用同一套整数插值取格
    const int64_t ax = static_cast<int64_t>((p.x - x0_) / kCS);
    const int64_t ay = static_cast<int64_t>((p.y - y0_) / kCS);
    const int64_t bx = static_cast<int64_t>((q.x - x0_) / kCS);
    const int64_t by = static_cast<int64_t>((q.y - y0_) / kCS);
    const int64_t n = std::max<int64_t>(std::max(std::abs(bx - ax), std::abs(by - ay)), 1);
    int64_t px = ax;
    int64_t py = ay;
    for (int64_t k = 1; k <= n; ++k) {
        const int64_t cx =
            ax + static_cast<int64_t>(std::nearbyint(static_cast<double>(bx - ax) * static_cast<double>(k) / static_cast<double>(n)));
        const int64_t cy =
            ay + static_cast<int64_t>(std::nearbyint(static_cast<double>(by - ay) * static_cast<double>(k) / static_cast<double>(n)));
        if (cx == px && cy == py) {
            continue;
        }
        if (px >= 0 && py >= 0 && px < nx_ && py < ny_ && cx >= 0 && cy >= 0 && cx < nx_ && cy < ny_) {
            const int64_t ca = py * nx_ + px;
            const int64_t cb = cy * nx_ + cx;
            if ((hs && steps_->has(ca, cb)) || (hf && faces_->has(ca, cb))) {
                return true;
            }
        }
        px = cx;
        py = cy;
    }
    return false;
}

bool Visibility::ok(const WorldPoint& p, const WorldPoint& q, float hp, float hq) const
{
    if (blk_ != nullptr && blk_->blocked(p, q)) {
        return false;
    }
    if (crossesStep(p, q)) {
        return false;
    }
    return lyo_ == nullptr || lyo_->ok(p, q, hp, hq);
}

std::optional<std::vector<int64_t>> SpanAstar(
    const SpanTable& st,
    const std::vector<uint8_t>& ok,
    const Mask& ok2,
    int64_t s,
    const std::vector<int64_t>& gset,
    const PriceField& mult,
    const EdgeBits* banned,
    const double* bnp,
    const EdgeBits* forbidden,
    const Visibility* vis,
    std::vector<int64_t>* corners,
    double* out_cost,
    const JumpEdges* jumps,
    double give_up_at)
{
    if (s < 0 || ok[static_cast<size_t>(s)] == 0 || gset.empty()) {
        return std::nullopt;
    }
    const bool hj = jumps != nullptr && !jumps->empty();
    const auto byJump = [&](int64_t p, int64_t u) {
        return hj && p >= 0 && jumps->has(p, u);
    };
    // 台沿下落那一步视线必然被台沿挡住, 与跳边同样豁免
    const auto byFall = [&](int64_t p, int64_t u) {
        return p >= 0
               && static_cast<double>(st.sp_h[static_cast<size_t>(p)]) - static_cast<double>(st.sp_h[static_cast<size_t>(u)]) > kClimb;
    };
    const int64_t nx = ok2.nx, ny = ok2.ny;
    const int64_t gc = st.sp_cell[static_cast<size_t>(gset.front())];
    const int64_t gxx = gc % nx, gyy = gc / nx;
    std::vector<double> dist(st.sp_h.size(), std::numeric_limits<double>::infinity());
    std::vector<int32_t> prev(st.sp_h.size(), -1);
    dist[static_cast<size_t>(s)] = 0.0;
    MinHeap4 pq;
    pq.push(0.0, s);
    int64_t hit = -1;
    // Lazy Theta* 的 SetVertex, 祖父直连验不过时从已关闭的邻格里挑最便宜的当父亲
    std::vector<uint8_t> closed;
    if (vis != nullptr) {
        closed.assign(st.sp_h.size(), 0);
    }
    const auto reparent = [&](int64_t u, int64_t cu) {
        const int64_t x = cu % nx, y = cu / nx;
        const float hu = st.sp_h[static_cast<size_t>(u)];
        double bd = std::numeric_limits<double>::infinity();
        int64_t bp = -1;
        for (const auto& d : kNb8) {
            const int64_t a = x + d.dx, b = y + d.dy;
            if (a < 0 || a >= nx || b < 0 || b >= ny) {
                continue;
            }
            const int64_t cw = b * nx + a;
            if (ok2.v[static_cast<size_t>(cw)] == 0) {
                continue;
            }
            if (d.dx != 0 && d.dy != 0 && !(ok2.at(y, a) && ok2.at(b, x))) {
                continue;
            }
            const int64_t j = st.j(cw);
            if (j < 0) {
                continue;
            }
            // 与扩展同口径, 禁步位不管纯下落的 span 对
            const bool faceblk = forbidden != nullptr && forbidden->hasStep(cw, cu, -d.dx, -d.dy, nx);
            double pen = 0.0;
            if (banned != nullptr && banned->hasStep(cw, cu, -d.dx, -d.dy, nx)) {
                if (bnp == nullptr) {
                    continue;
                }
                pen = *bnp;
            }
            const double stp = d.w * 0.5 * static_cast<double>(mult.v(static_cast<size_t>(cw)) + mult.v(static_cast<size_t>(cu)));
            const int64_t jb = st.cstart(j), jn = st.ccnt(j);
            for (int64_t k = 0; k < jn; ++k) {
                const int64_t w = jb + k;
                if (ok[static_cast<size_t>(w)] == 0 || closed[static_cast<size_t>(w)] == 0) {
                    continue;
                }
                if (faceblk && !byFall(w, u)) {
                    continue;
                }
                if (!riseOkFast(st, nx, ny, cw, -d.dx, -d.dy, st.sp_h[static_cast<size_t>(w)], hu)) {
                    continue;
                }
                const double nd = dist[static_cast<size_t>(w)] + stp + pen;
                if (nd < bd - 1e-12) {
                    bd = nd;
                    bp = w;
                }
            }
        }
        if (bp >= 0) {
            dist[static_cast<size_t>(u)] = bd;
            prev[static_cast<size_t>(u)] = static_cast<int32_t>(bp);
        }
    };
    while (!pq.empty()) {
        const auto [f, u] = pq.top();
        pq.pop();
        double d0 = dist[static_cast<size_t>(u)];
        const int64_t cu = st.sp_cell[static_cast<size_t>(u)];
        const int64_t x = cu % nx, y = cu / nx;
        if (f > d0 + std::hypot(static_cast<double>(gxx - x), static_cast<double>(gyy - y)) + 1e-9) {
            continue;
        }
        if (vis == nullptr && f >= give_up_at) {
            return std::nullopt;
        }
        if (vis != nullptr) {
            if (closed[static_cast<size_t>(u)] != 0) {
                continue;
            }
            const int64_t p = prev[static_cast<size_t>(u)];
            if (p >= 0 && !byJump(p, u) && !byFall(p, u)
                && !vis->ok(
                    vis->at(st.sp_cell[static_cast<size_t>(p)]),
                    vis->at(cu),
                    st.sp_h[static_cast<size_t>(p)],
                    st.sp_h[static_cast<size_t>(u)])) {
                reparent(u, cu);
            }
            closed[static_cast<size_t>(u)] = 1;
            d0 = dist[static_cast<size_t>(u)];
        }
        if (std::find(gset.begin(), gset.end(), u) != gset.end()) {
            hit = u;
            break;
        }
        const float hu = st.sp_h[static_cast<size_t>(u)];
        const float m0 = mult.v(static_cast<size_t>(cu));
        const int64_t pu = vis != nullptr ? prev[static_cast<size_t>(u)] : -1;
        const bool pj = byJump(pu, u) || byFall(pu, u);
        for (const auto& d : kNb8) {
            const int64_t a = x + d.dx, b = y + d.dy;
            if (a < 0 || a >= nx || b < 0 || b >= ny) {
                continue;
            }
            const int64_t cv = b * nx + a;
            if (ok2.v[static_cast<size_t>(cv)] == 0) {
                continue;
            }
            if (d.dx != 0 && d.dy != 0 && !(ok2.at(y, a) && ok2.at(b, x))) {
                continue;
            }
            const int64_t j = st.j(cv);
            if (j < 0) {
                continue;
            }
            // 禁步位不分方向, 纯下落的 span 对不受它管, 留到逐 span 循环里再定
            const bool faceblk = forbidden != nullptr && forbidden->hasStep(cu, cv, d.dx, d.dy, nx);
            double pen = 0.0;
            if (banned != nullptr && banned->hasStep(cu, cv, d.dx, d.dy, nx)) {
                if (bnp == nullptr) {
                    continue;
                }
                pen = *bnp;
            }
            const float step = static_cast<float>(d.w * 0.5) * (m0 + mult.v(static_cast<size_t>(cv)));
            const double nd = d0 + static_cast<double>(step) + pen;
            // 弦按欧氏长度计价, 只在单价为一的实心区才精确, 所以两端都在实心区才许走弦
            int64_t np = u;
            double ndp = nd;
            if (pu >= 0 && !pj && mult.v(static_cast<size_t>(cv)) <= 1.0F) {
                const int64_t cp = st.sp_cell[static_cast<size_t>(pu)];
                if (mult.v(static_cast<size_t>(cp)) <= 1.0F) {
                    const double cd =
                        dist[static_cast<size_t>(pu)] + std::hypot(static_cast<double>(a - cp % nx), static_cast<double>(b - cp / nx));
                    if (cd < ndp - 1e-12) {
                        np = pu;
                        ndp = cd;
                    }
                }
            }
            const int64_t sb = st.cstart(j), sn = st.ccnt(j);
            for (int64_t k = 0; k < sn; ++k) {
                const int64_t v = sb + k;
                if (ok[static_cast<size_t>(v)] == 0) {
                    continue;
                }
                // 已关闭的 span 不许松弛, 否则改写后的弦验不到视线
                if (vis != nullptr && closed[static_cast<size_t>(v)] != 0) {
                    continue;
                }
                const float hv = st.sp_h[static_cast<size_t>(v)];
                const bool fall = static_cast<double>(hu) - static_cast<double>(hv) > kClimb;
                if (faceblk && !fall) {
                    continue;
                }
                if (!riseOkFast(st, nx, ny, cu, d.dx, d.dy, hu, hv)) {
                    continue;
                }
                // 下落不接祖父, 弦会从台沿上方穿空而过
                const int64_t pv = fall ? u : np;
                const double dv = fall ? nd : ndp;
                if (dv < dist[static_cast<size_t>(v)] - 1e-12) {
                    dist[static_cast<size_t>(v)] = dv;
                    prev[static_cast<size_t>(v)] = static_cast<int32_t>(pv);
                    pq.push(dv + std::hypot(static_cast<double>(gxx - a), static_cast<double>(gyy - b)), v);
                }
            }
        }
        if (hj) {
            auto [ji, jn] = jumps->from(u);
            for (; ji < jn; ++ji) {
                const JumpEdges::Edge& je = jumps->e[ji];
                const int64_t v = je.dst;
                const int64_t cv = st.sp_cell[static_cast<size_t>(v)];
                if (ok[static_cast<size_t>(v)] == 0 || ok2.v[static_cast<size_t>(cv)] == 0) {
                    continue;
                }
                if (vis != nullptr && closed[static_cast<size_t>(v)] != 0) {
                    continue;
                }
                const double nd = d0 + static_cast<double>(je.cost);
                if (nd < dist[static_cast<size_t>(v)] - 1e-12) {
                    dist[static_cast<size_t>(v)] = nd;
                    prev[static_cast<size_t>(v)] = static_cast<int32_t>(u);
                    pq.push(nd + std::hypot(static_cast<double>(gxx - cv % nx), static_cast<double>(gyy - cv / nx)), v);
                }
            }
        }
    }
    if (hit < 0) {
        return std::nullopt;
    }
    if (out_cost != nullptr) {
        *out_cost = dist[static_cast<size_t>(hit)];
    }
    std::vector<int64_t> out { hit };
    while (out.back() != s) {
        out.push_back(prev[static_cast<size_t>(out.back())]);
    }
    std::reverse(out.begin(), out.end());
    if (corners != nullptr) {
        *corners = out;
    }
    if (vis == nullptr) {
        return out;
    }
    // 父链是拐点序列, 下游按逐格读, 所以把每条弦铺回格上
    const std::vector<int64_t> corn = out;
    out.assign(1, corn.front());
    for (size_t i = 1; i < corn.size(); ++i) {
        if (byJump(corn[i - 1], corn[i])) {
            out.push_back(corn[i]);
            continue;
        }
        const int64_t ca = st.sp_cell[static_cast<size_t>(corn[i - 1])];
        const int64_t cb = st.sp_cell[static_cast<size_t>(corn[i])];
        const int64_t axx = ca % nx, ayy = ca / nx, bxx = cb % nx, byy = cb / nx;
        const int64_t n = std::max<int64_t>(std::max(std::abs(bxx - axx), std::abs(byy - ayy)), 1);
        const float ha = st.sp_h[static_cast<size_t>(corn[i - 1])];
        const float hb = st.sp_h[static_cast<size_t>(corn[i])];
        for (int64_t k = 1; k < n; ++k) {
            const int64_t cx =
                axx
                + static_cast<int64_t>(std::nearbyint(static_cast<double>(bxx - axx) * static_cast<double>(k) / static_cast<double>(n)));
            const int64_t cy =
                ayy
                + static_cast<int64_t>(std::nearbyint(static_cast<double>(byy - ayy) * static_cast<double>(k) / static_cast<double>(n)));
            const int64_t cc = cy * nx + cx;
            if (cc == st.sp_cell[static_cast<size_t>(out.back())]) {
                continue;
            }
            const int64_t j = st.j(cc);
            if (j < 0) {
                continue;
            }
            const float ht = ha + (hb - ha) * static_cast<float>(k) / static_cast<float>(n);
            int64_t bv = -1;
            float bdh = 0.0F;
            const int64_t sb = st.cstart(j), sn = st.ccnt(j);
            for (int64_t kk = 0; kk < sn; ++kk) {
                const int64_t v = sb + kk;
                if (ok[static_cast<size_t>(v)] == 0) {
                    continue;
                }
                const float dh = std::fabs(st.sp_h[static_cast<size_t>(v)] - ht);
                if (bv < 0 || dh < bdh) {
                    bv = v;
                    bdh = dh;
                }
            }
            if (bv >= 0) {
                out.push_back(bv);
            }
        }
        if (out.back() != corn[i]) {
            out.push_back(corn[i]);
        }
    }
    return out;
}

namespace
{

// 两遍扫描求每格的最近源点, 没有源点的格留 -1
void NearestSource(const std::vector<uint8_t>& src, int64_t nx, int64_t ny, std::vector<int32_t>& fx, std::vector<int32_t>& fy)
{
    const int64_t n = nx * ny;
    fx.assign(static_cast<size_t>(n), -1);
    fy.assign(static_cast<size_t>(n), -1);
    for (int64_t i = 0; i < n; ++i) {
        if (src[static_cast<size_t>(i)] != 0) {
            fx[static_cast<size_t>(i)] = static_cast<int32_t>(i % nx);
            fy[static_cast<size_t>(i)] = static_cast<int32_t>(i / nx);
        }
    }
    const auto take = [&](int64_t c, int64_t x, int64_t y, int64_t o) {
        const int64_t ox = fx[static_cast<size_t>(o)];
        if (ox < 0) {
            return;
        }
        const int64_t oy = fy[static_cast<size_t>(o)];
        const int64_t od = (ox - x) * (ox - x) + (oy - y) * (oy - y);
        const int64_t cx = fx[static_cast<size_t>(c)];
        if (cx >= 0) {
            const int64_t cy = fy[static_cast<size_t>(c)];
            if ((cx - x) * (cx - x) + (cy - y) * (cy - y) <= od) {
                return;
            }
        }
        fx[static_cast<size_t>(c)] = static_cast<int32_t>(ox);
        fy[static_cast<size_t>(c)] = static_cast<int32_t>(oy);
    };
    for (int64_t y = 0; y < ny; ++y) {
        for (int64_t x = 0; x < nx; ++x) {
            const int64_t c = y * nx + x;
            if (y > 0) {
                take(c, x, y, c - nx);
                if (x > 0) {
                    take(c, x, y, c - nx - 1);
                }
                if (x + 1 < nx) {
                    take(c, x, y, c - nx + 1);
                }
            }
            if (x > 0) {
                take(c, x, y, c - 1);
            }
        }
        for (int64_t x = nx - 2; x >= 0; --x) {
            take(y * nx + x, x, y, y * nx + x + 1);
        }
    }
    for (int64_t y = ny - 1; y >= 0; --y) {
        for (int64_t x = nx - 1; x >= 0; --x) {
            const int64_t c = y * nx + x;
            if (y + 1 < ny) {
                take(c, x, y, c + nx);
                if (x > 0) {
                    take(c, x, y, c + nx - 1);
                }
                if (x + 1 < nx) {
                    take(c, x, y, c + nx + 1);
                }
            }
            if (x + 1 < nx) {
                take(c, x, y, c + 1);
            }
        }
        for (int64_t x = 1; x < nx; ++x) {
            take(y * nx + x, x, y, y * nx + x - 1);
        }
    }
}

} // namespace

// 一格与某个邻格的最近障碍点相隔 λ 以上即在中轴上
Mask MedialAxis(const Grid<float>& dist, double lam)
{
    const int64_t ny = dist.ny;
    const int64_t nx = dist.nx;
    const int64_t n = nx * ny;
    std::vector<uint8_t> solid(static_cast<size_t>(n), 0);
    for (int64_t i = 0; i < n; ++i) {
        solid[static_cast<size_t>(i)] = static_cast<uint8_t>(dist.v[static_cast<size_t>(i)] <= 0.0F);
    }
    std::vector<int32_t> fx;
    std::vector<int32_t> fy;
    NearestSource(solid, nx, ny, fx, fy);
    const double step = lam / kCS;
    const int64_t thr = static_cast<int64_t>(std::ceil(step * step));
    Mask out(nx, ny, 0);
    for (int64_t y = 0; y < ny; ++y) {
        for (int64_t x = 0; x < nx; ++x) {
            const int64_t c = y * nx + x;
            const int64_t cx = fx[static_cast<size_t>(c)];
            if (dist.v[static_cast<size_t>(c)] <= 0.0F || cx < 0) {
                continue;
            }
            const int64_t cy = fy[static_cast<size_t>(c)];
            for (const auto& d : kNb8) {
                const int64_t a = x + d.dx;
                const int64_t b = y + d.dy;
                if (a < 0 || a >= nx || b < 0 || b >= ny) {
                    continue;
                }
                const int64_t o = b * nx + a;
                const int64_t ox = fx[static_cast<size_t>(o)];
                if (ox < 0) {
                    continue;
                }
                const int64_t oy = fy[static_cast<size_t>(o)];
                if ((ox - cx) * (ox - cx) + (oy - cy) * (oy - cy) >= thr) {
                    out.at(y, x) = 1;
                    break;
                }
            }
        }
    }
    return out;
}

// 每格取最近种子格的净空作走廊半宽, band 是各种子按自身净空张开的圆盘之并
Grid<float> CorridorWidth(const Grid<float>& dist, const Mask& seed, Mask* band)
{
    const int64_t ny = dist.ny;
    const int64_t nx = dist.nx;
    std::vector<int32_t> fx;
    std::vector<int32_t> fy;
    NearestSource(seed.v, nx, ny, fx, fy);
    Grid<float> out(nx, ny, 0.0F);
    if (band != nullptr) {
        *band = Mask(nx, ny, 0);
    }
    for (int64_t y = 0; y < ny; ++y) {
        for (int64_t x = 0; x < nx; ++x) {
            const int64_t i = y * nx + x;
            const int64_t ax = fx[static_cast<size_t>(i)];
            if (ax < 0) {
                continue;
            }
            const int64_t ay = fy[static_cast<size_t>(i)];
            const float w = dist.v[static_cast<size_t>(ay * nx + ax)];
            out.v[static_cast<size_t>(i)] = w;
            if (band != nullptr) {
                const double dx = static_cast<double>(ax - x) * kCS;
                const double dy = static_cast<double>(ay - y) * kCS;
                const bool in = dx * dx + dy * dy <= static_cast<double>(w) * static_cast<double>(w);
                band->at(y, x) = static_cast<uint8_t>(in || seed.at(y, x) != 0);
            }
        }
    }
    return out;
}

namespace
{

// 边界侧序 (dx,dy,角a,角b), 自由区在行进方向左侧
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
    // 每个格角一字节, 低四位是四个侧的出边, 高四位是已走过标记
    const int64_t kStep[4] = { W, -1, -W, 1 };
    std::vector<uint8_t> bits(static_cast<size_t>(W * (ny + 1)), 0);
    std::vector<int64_t> order;
    for (int s = 0; s < 4; ++s) {
        const int64_t* sd = kSides[s];
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
                uint8_t& b = bits[static_cast<size_t>(u)];
                if ((b & 0x0FU) == 0) {
                    order.push_back(u);
                }
                b |= static_cast<uint8_t>(1U << s);
            }
        }
    }
    std::vector<std::vector<WorldPoint>> loops;
    for (const int64_t u0 : order) {
        for (int s0 = 0; s0 < 4; ++s0) {
            const uint8_t m0 = static_cast<uint8_t>(1U << s0);
            const uint8_t b0 = bits[static_cast<size_t>(u0)];
            if ((b0 & m0) == 0 || (b0 & static_cast<uint8_t>(m0 << 4)) != 0) {
                continue;
            }
            std::vector<int64_t> loop;
            int64_t u = u0, v = u0 + kStep[s0];
            int su = s0;
            while (true) {
                bits[static_cast<size_t>(u)] |= static_cast<uint8_t>(1U << (su + 4));
                loop.push_back(u);
                const uint8_t out = static_cast<uint8_t>(bits[static_cast<size_t>(v)] & 0x0FU);
                if (out == 0) {
                    break;
                }
                int sw = 0;
                if ((out & static_cast<uint8_t>(out - 1)) == 0) {
                    while ((out & static_cast<uint8_t>(1U << sw)) == 0) {
                        ++sw;
                    }
                }
                else {
                    // 岔口优先右转, 与 A* 禁切角一致
                    const int64_t d0 = v % W - u % W, d1 = v / W - u / W;
                    int best = 10;
                    for (int s = 0; s < 4; ++s) {
                        if ((out & static_cast<uint8_t>(1U << s)) == 0) {
                            continue;
                        }
                        const int64_t z = v + kStep[s];
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
                            sw = s;
                        }
                    }
                }
                if ((bits[static_cast<size_t>(v)] & static_cast<uint8_t>(1U << (sw + 4))) != 0) {
                    break;
                }
                const int64_t w = v + kStep[sw];
                u = v;
                v = w;
                su = sw;
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

BlockerSegments::BlockerSegments(
    const std::vector<std::vector<WorldPoint>>& loops,
    const std::vector<WorldPoint>* extra_a,
    const std::vector<WorldPoint>* extra_b)
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
}

void BlockerSegments::buildIndex() const
{
    built_ = true;
    if (a_.empty()) {
        return;
    }
    double mnx = lo(0).x, mny = lo(0).y, mxx = hi(0).x, mxy = hi(0).y;
    for (size_t i = 1; i < a_.size(); ++i) {
        const WorldPoint l = lo(i), h = hi(i);
        mnx = std::min(mnx, l.x);
        mny = std::min(mny, l.y);
        mxx = std::max(mxx, h.x);
        mxy = std::max(mxy, h.y);
    }
    bx0_ = mnx - kBkt;
    by0_ = mny - kBkt;
    bnx_ = static_cast<int64_t>((mxx + kBkt - bx0_) / kBkt) + 1;
    bny_ = static_cast<int64_t>((mxy + kBkt - by0_) / kBkt) + 1;
    bstart_.assign(static_cast<size_t>(bnx_ * bny_ + 1), 0);
    const auto span = [&](size_t i, int64_t* g) {
        const WorldPoint l = lo(i), h = hi(i);
        g[0] = static_cast<int64_t>((l.x - kBktPad - bx0_) / kBkt);
        g[1] = static_cast<int64_t>((h.x + kBktPad - bx0_) / kBkt);
        g[2] = static_cast<int64_t>((l.y - kBktPad - by0_) / kBkt);
        g[3] = static_cast<int64_t>((h.y + kBktPad - by0_) / kBkt);
    };
    int64_t g[4];
    for (size_t i = 0; i < a_.size(); ++i) {
        span(i, g);
        for (int64_t gy = g[2]; gy <= g[3]; ++gy) {
            for (int64_t gx = g[0]; gx <= g[1]; ++gx) {
                ++bstart_[static_cast<size_t>(gy * bnx_ + gx) + 1];
            }
        }
    }
    for (size_t k = 1; k < bstart_.size(); ++k) {
        bstart_[k] += bstart_[k - 1];
    }
    bitem_.resize(static_cast<size_t>(bstart_.back()));
    std::vector<int32_t> fill(bstart_.begin(), bstart_.end() - 1);
    for (size_t i = 0; i < a_.size(); ++i) {
        span(i, g);
        for (int64_t gy = g[2]; gy <= g[3]; ++gy) {
            for (int64_t gx = g[0]; gx <= g[1]; ++gx) {
                bitem_[static_cast<size_t>(fill[static_cast<size_t>(gy * bnx_ + gx)]++)] = static_cast<int32_t>(i);
            }
        }
    }
    seen_.assign(a_.size(), 0);
    bseen_.assign(bstart_.size() - 1, 0);
}

bool Blockers::blocked(const WorldPoint& p, const WorldPoint& q) const
{
    constexpr double eps = 1e-7;
    const double lox = std::min(p.x, q.x) - eps, hix = std::max(p.x, q.x) + eps;
    const double loy = std::min(p.y, q.y) - eps, hiy = std::max(p.y, q.y) + eps;
    const double rx = q.x - p.x, ry = q.y - p.y;
    // 取样间隔半桶并取三乘三邻域, 弦经过的桶才不会漏
    const BlockerSegments& sg = *segs_;
    if (!sg.built_) {
        sg.buildIndex();
    }
    if (++sg.epoch_ == 0) {
        std::fill(sg.seen_.begin(), sg.seen_.end(), 0);
        std::fill(sg.bseen_.begin(), sg.bseen_.end(), 0);
        ++sg.epoch_;
    }
    const int64_t ns = static_cast<int64_t>(std::hypot(rx, ry) / (kBkt * 0.5)) + 1;
    for (int64_t k = 0; k <= ns && sg.bnx_ > 0; ++k) {
        const double t = static_cast<double>(k) / static_cast<double>(ns);
        const int64_t sx0 = static_cast<int64_t>((p.x + rx * t - sg.bx0_) / kBkt);
        const int64_t sy0 = static_cast<int64_t>((p.y + ry * t - sg.by0_) / kBkt);
        for (int64_t gy = std::max<int64_t>(sy0 - 1, 0); gy <= std::min(sy0 + 1, sg.bny_ - 1); ++gy) {
            for (int64_t gx = std::max<int64_t>(sx0 - 1, 0); gx <= std::min(sx0 + 1, sg.bnx_ - 1); ++gx) {
                const size_t bk = static_cast<size_t>(gy * sg.bnx_ + gx);
                if (sg.bseen_[bk] == sg.epoch_) {
                    continue;
                }
                sg.bseen_[bk] = sg.epoch_;
                for (int32_t e = sg.bstart_[bk]; e < sg.bstart_[bk + 1]; ++e) {
                    const int32_t id = sg.bitem_[static_cast<size_t>(e)];
                    if (sg.seen_[static_cast<size_t>(id)] == sg.epoch_) {
                        continue;
                    }
                    sg.seen_[static_cast<size_t>(id)] = sg.epoch_;
                    const size_t i = static_cast<size_t>(id);
                    const WorldPoint sl = sg.lo(i), sh = sg.hi(i);
                    if (sh.x < lox || sl.x > hix || sh.y < loy || sl.y > hiy) {
                        continue;
                    }
                    const double sx = sg.b_[i].x - sg.a_[i].x, sy = sg.b_[i].y - sg.a_[i].y;
                    const double den = rx * sy - ry * sx;
                    if (!(std::abs(den) > 1e-12)) {
                        continue;
                    }
                    const double ux = sg.a_[i].x - p.x, uy = sg.a_[i].y - p.y;
                    const double tt = (ux * sy - uy * sx) / den;
                    const double w = (ux * ry - uy * rx) / den;
                    // 挡线一侧闭区间防 45° 弦从顶点缝溜过, 弦一侧开区间让端点贴墙不算穿墙
                    if (tt > eps && tt < 1 - eps && w > -eps && w < 1 + eps) {
                        return true;
                    }
                }
            }
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

std::optional<std::vector<float>> LayerOracle::walk(const std::vector<WorldPoint>& pts, float h) const
{
    return walk(pts, std::vector<float> { h });
}

std::optional<std::vector<float>> LayerOracle::walk(const std::vector<WorldPoint>& pts, const std::vector<float>& h, bool fall) const
{
    std::vector<CellPt> cells;
    for (size_t i = 1; i < pts.size(); ++i) {
        const int64_t ax = static_cast<int64_t>((pts[i - 1].x - x0_) / kCS);
        const int64_t ay = static_cast<int64_t>((pts[i - 1].y - y0_) / kCS);
        const int64_t bx = static_cast<int64_t>((pts[i].x - x0_) / kCS);
        const int64_t by = static_cast<int64_t>((pts[i].y - y0_) / kCS);
        const int64_t n = std::max<int64_t>(std::max(std::abs(bx - ax), std::abs(by - ay)), 1);
        for (int64_t k = 0; k <= n; ++k) {
            const CellPt c {
                ax + static_cast<int64_t>(std::nearbyint(static_cast<double>(bx - ax) * static_cast<double>(k) / static_cast<double>(n))),
                ay + static_cast<int64_t>(std::nearbyint(static_cast<double>(by - ay) * static_cast<double>(k) / static_cast<double>(n)))
            };
            if (cells.empty() || !(cells.back() == c)) {
                cells.push_back(c);
            }
        }
    }
    std::erase_if(cells, [&](const CellPt& c) { return c.x < 0 || c.x >= nx_ || c.y < 0 || c.y >= ny_; });
    std::vector<float> cur = h;
    std::vector<float> nb;
    std::vector<float> nxt;
    CellPt pc = cells.empty() ? CellPt {} : cells[0];
    for (size_t i = 1; i < cells.size(); ++i) {
        const int64_t j = st_->j(cells[i].y * nx_ + cells[i].x);
        if (j < 0) {
            continue;
        }
        nb.clear();
        const int64_t sb = st_->cstart(j), sn = st_->ccnt(j);
        for (int64_t k = 0; k < sn; ++k) {
            nb.push_back(st_->sp_h[static_cast<size_t>(sb + k)]);
        }
        if (nb.empty()) {
            continue;
        }
        nxt.clear();
        const double up = UpAllow(std::hypot(static_cast<double>(cells[i].x - pc.x), static_cast<double>(cells[i].y - pc.y))) + kQH;
        // 与 RiseOk 同口径, 出发那层几格内又回来即路面上一道缝
        const int64_t sx = (cells[i].x > pc.x) - (cells[i].x < pc.x);
        const int64_t sy = (cells[i].y > pc.y) - (cells[i].y < pc.y);
        const auto seam = [&](float c) {
            for (int64_t s = 1; s <= kSeamCells; ++s) {
                if (levelAt(*st_, nx_, ny_, pc.y * nx_ + pc.x, sx, sy, s, c, kClimb)) {
                    return true;
                }
            }
            return false;
        };
        bool over_seam = false;
        for (const float t : nb) {
            for (const float c : cur) {
                const float dh = t - c;
                if (static_cast<double>(dh) > up) {
                    continue;
                }
                if (dh >= -static_cast<float>(kClimb)) {
                    nxt.push_back(t);
                    break;
                }
                if (fall) {
                    if (seam(c)) {
                        over_seam = true;
                        continue;
                    }
                    nxt.push_back(t);
                    break;
                }
            }
        }
        if (nxt.empty()) {
            if (!fall) {
                return std::nullopt;
            }
            nxt = cur;
            if (!over_seam) {
                nxt.insert(nxt.end(), nb.begin(), nb.end());
            }
        }
        cur = nxt;
        pc = cells[i];
    }
    return cur;
}

bool LayerOracle::ok(const WorldPoint& p, const WorldPoint& q, float h, float hq) const
{
    const auto cur = walk({ p, q }, h);
    if (!cur.has_value()) {
        return false;
    }
    return std::any_of(cur->begin(), cur->end(), [&](float v) { return std::fabs(v - hq) <= 1e-3F; });
}

std::vector<WorldPoint> StringPull(
    const std::vector<WorldPoint>& pts,
    const Blockers& blk,
    const LayerOracle* lyo,
    const std::vector<float>* hs,
    const Visibility* vis)
{
    std::vector<WorldPoint> out { pts[0] };
    size_t i = 0;
    while (i < pts.size() - 1) {
        size_t j = pts.size() - 1;
        while (j > i + 1) {
            if (vis != nullptr) {
                if (vis->ok(pts[i], pts[j], (*hs)[i], (*hs)[j])) {
                    break;
                }
                --j;
                continue;
            }
            if (!blk.blocked(pts[i], pts[j]) && (lyo == nullptr || lyo->ok(pts[i], pts[j], (*hs)[i], (*hs)[j]))) {
                break;
            }
            --j;
        }
        out.push_back(pts[j]);
        i = j;
    }
    return out;
}

} // namespace navmesh::recast
