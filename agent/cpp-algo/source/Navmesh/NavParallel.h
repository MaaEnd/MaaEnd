#pragma once

#include <algorithm>
#include <atomic>
#include <cstddef>
#include <cstdint>
#include <thread>
#include <vector>

namespace navmesh
{

inline constexpr unsigned kNavWorkerLimit = 4;      // 这几个环是访存绑死的, 线程再多也量不出加速
inline constexpr int64_t kNavSerialFloor = 1 << 16; // 元素数低于此值, 起线程比算还贵

inline std::atomic<unsigned>& NavWorkerOverride()
{
    static std::atomic<unsigned> value { 0 };
    return value;
}

// 只改用几个线程, 不改算什么: 切块按下标静态划分, 每块写自己那段, 1 线程与 N 线程输出逐位相同。
// 台架靠它把同一份代码跑成两种线程数来验这一点, 生产不调, 走硬件并发。
inline void SetNavWorkerLimit(unsigned limit)
{
    NavWorkerOverride().store(limit);
}

inline size_t NavWorkerCount(int64_t n)
{
    const unsigned forced = NavWorkerOverride().load();
    const unsigned avail =
        forced != 0 ? forced : std::min(kNavWorkerLimit, std::max(1U, std::thread::hardware_concurrency()));
    if (n < kNavSerialFloor || avail <= 1) {
        return 1;
    }
    return static_cast<size_t>(avail);
}

// 一个元素就是一整块瓦这种循环, 元素数只有几百, 拿它比 kNavSerialFloor 会一路退回单线程。
// 这里只按块数封顶。
inline size_t NavWorkerCountForBlocks(int64_t n)
{
    const unsigned forced = NavWorkerOverride().load();
    const unsigned avail =
        forced != 0 ? forced : std::min(kNavWorkerLimit, std::max(1U, std::thread::hardware_concurrency()));
    if (n <= 1 || avail <= 1) {
        return 1;
    }
    return static_cast<size_t>(std::min<int64_t>(n, avail));
}

// fn(w, begin, end) 跑第 w 块 [begin, end)。调用线程自己领第 0 块, 因此只多起 workers-1 个。
// 池子用 jthread: 调用线程这块抛出时(分配失败是唯一的来路), 析构自己把工人接回来。裸 thread
// 在那条路上还是 joinable, 析构即 terminate, 连崩溃点都留不下。
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

// 过滤环的合流: 每块只往自己那个桶里推, 收工按块序接起来。桶序即下标序, 于是与线程数无关。
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
