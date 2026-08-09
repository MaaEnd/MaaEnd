#include "manual_runner_executor.h"

#include <algorithm>
#include <atomic>
#include <stdexcept>
#include <thread>

namespace iconrecognition::test
{

std::vector<std::exception_ptr> ExecuteManualRunnerCases(std::size_t case_count, std::size_t jobs, const ManualRunnerCaseTask& task)
{
    if (!task) {
        throw std::invalid_argument("manual runner task must be callable");
    }
    if (case_count == 0) {
        return {};
    }
    if (jobs == 0) {
        throw std::invalid_argument("manual runner jobs must be positive");
    }

    std::atomic<std::size_t> next_index { 0 };
    std::vector<std::exception_ptr> errors(case_count);
    const std::size_t worker_count = std::min(case_count, jobs);
    std::vector<std::jthread> workers;
    workers.reserve(worker_count);
    for (std::size_t worker = 0; worker < worker_count; ++worker) {
        workers.emplace_back([&] {
            while (true) {
                const std::size_t index = next_index.fetch_add(1, std::memory_order_relaxed);
                if (index >= case_count) {
                    return;
                }
                try {
                    task(index);
                }
                catch (...) {
                    errors[index] = std::current_exception();
                }
            }
        });
    }
    workers.clear(); // jthread 析构会在读取 errors 前完成 join。
    return errors;
}

} // namespace iconrecognition::test
