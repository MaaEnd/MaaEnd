#pragma once

#include <atomic>
#include <condition_variable>
#include <cstdint>
#include <deque>
#include <functional>
#include <limits>
#include <memory>
#include <mutex>
#include <thread>
#include <vector>

namespace maplocator
{

enum class ScaleTaskPriority : int
{
    Discarded = 0,
    GlobalPrimary = 2,
    Tracking = 3,
    Coordinator = 4,
};

enum class ScaleTaskClass
{
    Tracking,
    Global,
    Coordinator,
};

struct ScaleTaskControl
{
    std::atomic<ScaleTaskPriority> priority = ScaleTaskPriority::Discarded;
    std::atomic<bool> canceled = false;
    std::atomic<bool> reserve_tracking_capacity = false;
};

// 自己持有工作线程，析构时停线程，保留类的写法
class MapLocatorScaleExecutor
{
public:
    explicit MapLocatorScaleExecutor(size_t workerCount)
        : globalConcurrencyLimit(workerCount > 4 ? std::max<size_t>(1, workerCount / 3) : workerCount)
    {
        workers.reserve(workerCount);
        for (size_t index = 0; index < workerCount; ++index) {
            workers.emplace_back([this]() { workerLoop(); });
        }
    }

    ~MapLocatorScaleExecutor()
    {
        {
            std::lock_guard<std::mutex> lock(mutex);
            stopping = true;
            queue.clear();
        }
        condition.notify_all();
        for (auto& worker : workers) {
            if (worker.joinable()) {
                worker.join();
            }
        }
    }

    MapLocatorScaleExecutor(const MapLocatorScaleExecutor&) = delete;
    MapLocatorScaleExecutor& operator=(const MapLocatorScaleExecutor&) = delete;

    void submit(const std::shared_ptr<ScaleTaskControl>& control, ScaleTaskClass taskClass, std::function<void()> function)
    {
        {
            std::lock_guard<std::mutex> lock(mutex);
            queue.emplace_back(QueuedTask {
                .control = control,
                .taskClass = taskClass,
                .sequence = nextSequence++,
                .function = std::move(function),
            });
        }
        condition.notify_one();
    }

    void reprioritize(const std::shared_ptr<ScaleTaskControl>& control, ScaleTaskPriority priority)
    {
        control->priority.store(priority, std::memory_order_relaxed);
        condition.notify_all();
    }

    void cancel(const std::shared_ptr<ScaleTaskControl>& control)
    {
        control->canceled.store(true, std::memory_order_relaxed);
        reprioritize(control, ScaleTaskPriority::Discarded);
    }

    void releaseTrackingCapacity(const std::shared_ptr<ScaleTaskControl>& control)
    {
        control->reserve_tracking_capacity.store(false, std::memory_order_relaxed);
        condition.notify_all();
    }

    size_t workerCount() const { return workers.size(); }

private:
    struct QueuedTask
    {
        std::shared_ptr<ScaleTaskControl> control;
        ScaleTaskClass taskClass = ScaleTaskClass::Global;
        std::uint64_t sequence = 0;
        std::function<void()> function;
    };

    auto findNextTask()
    {
        auto selected = queue.end();
        int selectedPriority = std::numeric_limits<int>::min();
        std::uint64_t selectedSequence = std::numeric_limits<std::uint64_t>::max();
        for (auto it = queue.begin(); it != queue.end(); ++it) {
            if (it->taskClass == ScaleTaskClass::Global && it->control->reserve_tracking_capacity.load(std::memory_order_relaxed)
                && activeGlobalTasks >= globalConcurrencyLimit) {
                continue;
            }
            if (it->taskClass == ScaleTaskClass::Coordinator && activeCoordinatorTasks >= 1) {
                continue;
            }
            const int priority = static_cast<int>(it->control->priority.load(std::memory_order_relaxed));
            if (priority > selectedPriority || (priority == selectedPriority && it->sequence < selectedSequence)) {
                selected = it;
                selectedPriority = priority;
                selectedSequence = it->sequence;
            }
        }
        return selected;
    }

    void workerLoop()
    {
        while (true) {
            QueuedTask task;
            {
                std::unique_lock<std::mutex> lock(mutex);
                condition.wait(lock, [this]() { return stopping || !queue.empty(); });
                if (stopping) {
                    return;
                }

                auto selected = findNextTask();
                if (selected == queue.end()) {
                    condition.wait(lock);
                    continue;
                }
                task = std::move(*selected);
                queue.erase(selected);
                if (task.taskClass == ScaleTaskClass::Global) {
                    ++activeGlobalTasks;
                }
                else if (task.taskClass == ScaleTaskClass::Tracking) {
                    ++activeTrackingTasks;
                }
                else if (task.taskClass == ScaleTaskClass::Coordinator) {
                    ++activeCoordinatorTasks;
                }
            }

            task.function();

            {
                std::lock_guard<std::mutex> lock(mutex);
                if (task.taskClass == ScaleTaskClass::Global) {
                    --activeGlobalTasks;
                }
                else if (task.taskClass == ScaleTaskClass::Tracking) {
                    --activeTrackingTasks;
                }
                else if (task.taskClass == ScaleTaskClass::Coordinator) {
                    --activeCoordinatorTasks;
                }
            }
            condition.notify_all();
        }
    }

    std::vector<std::thread> workers;
    std::deque<QueuedTask> queue;
    mutable std::mutex mutex;
    std::condition_variable condition;
    size_t activeTrackingTasks = 0;
    size_t activeGlobalTasks = 0;
    size_t activeCoordinatorTasks = 0;
    size_t globalConcurrencyLimit = 1;
    std::uint64_t nextSequence = 0;
    bool stopping = false;
};

} // namespace maplocator
