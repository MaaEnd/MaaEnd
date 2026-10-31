#pragma once

#include <algorithm>
#include <atomic>
#include <cstddef>
#include <cstdint>
#include <thread>
#include <vector>

namespace navmesh
{

inline constexpr unsigned kNavWorkerLimit = 4;
inline constexpr int64_t kNavSerialFloor = 1 << 16; // 元素数低于此值走单线程

inline std::atomic<unsigned>& NavWorkerOverride()
{
    static std::atomic<unsigned> value { 0 };
    return value;
}

// 仅供测试改线程数, 生产代码不调用
inline void SetNavWorkerLimit(unsigned limit)
{
    NavWorkerOverride().store(limit);
}

inline size_t NavWorkerCount(int64_t n)
{
    const unsigned forced = NavWorkerOverride().load();
    const unsigned avail = forced != 0 ? forced : std::min(kNavWorkerLimit, std::max(1U, std::thread::hardware_concurrency()));
    if (n < kNavSerialFloor || avail <= 1) {
        return 1;
    }
    return static_cast<size_t>(avail);
}

// 用于每个元素是一整块瓦的循环, 不受 kNavSerialFloor 限制
inline size_t NavWorkerCountForBlocks(int64_t n)
{
    const unsigned forced = NavWorkerOverride().load();
    const unsigned avail = forced != 0 ? forced : std::min(kNavWorkerLimit, std::max(1U, std::thread::hardware_concurrency()));
    if (n <= 1 || avail <= 1) {
        return 1;
    }
    return static_cast<size_t>(std::min<int64_t>(n, avail));
}

// fn(w, begin, end) 跑第 w 块, 调用线程跑第 0 块, 用 jthread 是为了抛出时自动 join
template <class F>
void ParallelChunks(int64_t n, size_t workers, F&& fn)
{
    if (n <= 0) {
        return;
    }
    const size_t nw = std::max<size_t>(1, workers);
    const int64_t step = (n + static_cast<int64_t>(nw) - 1) / static_cast<int64_t>(nw);
    std::vector<std::jthread> pool;
    pool.reserve(nw - 1);
    for (size_t w = 1; w < nw; ++w) {
        const int64_t b = std::min(n, step * static_cast<int64_t>(w));
        const int64_t e = std::min(n, b + step);
        if (b >= e) {
            break;
        }
        pool.emplace_back([&fn, w, b, e] { fn(w, b, e); });
    }
    fn(size_t { 0 }, int64_t { 0 }, std::min(n, step));
    for (std::jthread& th : pool) {
        th.join();
    }
}

// 按块序拼接各桶, 结果与线程数无关
template <class T>
void ConcatBins(const std::vector<std::vector<T>>& bins, std::vector<T>& out)
{
    size_t total = out.size();
    for (const std::vector<T>& bin : bins) {
        total += bin.size();
    }
    out.reserve(total);
    for (const std::vector<T>& bin : bins) {
        out.insert(out.end(), bin.begin(), bin.end());
    }
}

}
