#include "SystemMonitor.h"

#include <chrono>
#include <thread>

#include <MaaUtils/Logger.h>

#ifdef _WIN32

#include <algorithm>
#include <cstdint>
#include <regex>
#include <string>
#include <string_view>
#include <unordered_map>
#include <vector>

#include <MaaUtils/SafeWindows.hpp>

#include <pdh.h>
#include <pdhmsg.h>
#include <psapi.h>

#endif

namespace common
{

#ifdef _WIN32

namespace
{

constexpr auto kSampleInterval = std::chrono::seconds(10);
constexpr uint64_t kBytesPerMb = 1024 * 1024;
constexpr double kGpuIdleEpsilon = 1e-6;

uint64_t FileTimeToU64(const FILETIME& ft)
{
    ULARGE_INTEGER value {};
    value.LowPart = ft.dwLowDateTime;
    value.HighPart = ft.dwHighDateTime;
    return value.QuadPart;
}

double ClampUnitInterval(double value)
{
    if (value < 0.0) {
        return 0.0;
    }
    if (value > 1.0) {
        return 1.0;
    }
    return value;
}

// PDH GPU Engine 计数器给出 0–100，日志与 CPU 对齐为 0.0–1.0。
double PdhPercentToUnit(double pdh_percent)
{
    return ClampUnitInterval(pdh_percent / 100.0);
}

std::string WideToUtf8(std::wstring_view src)
{
    if (src.empty()) {
        return {};
    }

    const int size = WideCharToMultiByte(
        CP_UTF8,
        0,
        src.data(),
        static_cast<int>(src.size()),
        nullptr,
        0,
        nullptr,
        nullptr);
    if (size <= 0) {
        return {};
    }

    std::string out(static_cast<size_t>(size), '\0');
    WideCharToMultiByte(
        CP_UTF8,
        0,
        src.data(),
        static_cast<int>(src.size()),
        out.data(),
        size,
        nullptr,
        nullptr);
    return out;
}

struct MemorySample
{
    bool ok = false;
    uint64_t process_private_mb = 0;
    uint64_t process_working_set_mb = 0;
    uint64_t system_used_mb = 0;
    uint64_t system_total_mb = 0;
};

MemorySample SampleMemory()
{
    MemorySample sample;

    PROCESS_MEMORY_COUNTERS_EX pmc {};
    pmc.cb = sizeof(pmc);
    if (GetProcessMemoryInfo(GetCurrentProcess(), reinterpret_cast<PROCESS_MEMORY_COUNTERS*>(&pmc), sizeof(pmc))) {
        sample.process_private_mb = pmc.PrivateUsage / kBytesPerMb;
        sample.process_working_set_mb = pmc.WorkingSetSize / kBytesPerMb;
    }
    else {
        LogWarn << "SystemMonitor GetProcessMemoryInfo failed." << VAR(GetLastError());
    }

    MEMORYSTATUSEX status {};
    status.dwLength = sizeof(status);
    if (GlobalMemoryStatusEx(&status)) {
        sample.system_total_mb = status.ullTotalPhys / kBytesPerMb;
        sample.system_used_mb = (status.ullTotalPhys - status.ullAvailPhys) / kBytesPerMb;
        sample.ok = true;
    }
    else {
        LogWarn << "SystemMonitor GlobalMemoryStatusEx failed." << VAR(GetLastError());
    }

    return sample;
}

class CpuSampler
{
public:
    bool CaptureBaseline()
    {
        SYSTEM_INFO sys_info {};
        GetSystemInfo(&sys_info);
        logical_processor_count_ = static_cast<int32_t>(sys_info.dwNumberOfProcessors);
        if (logical_processor_count_ <= 0) {
            LogWarn << "SystemMonitor invalid processor count." << VAR(logical_processor_count_);
            return false;
        }
        return CaptureTimes();
    }

    bool Sample(double& process_cpu, double& system_cpu)
    {
        const uint64_t last_now = last_now_;
        const uint64_t last_proc_sys = last_proc_sys_;
        const uint64_t last_proc_user = last_proc_user_;
        const uint64_t last_idle = last_idle_;
        const uint64_t last_kernel = last_kernel_;
        const uint64_t last_user = last_user_;

        if (!CaptureTimes()) {
            return false;
        }

        const uint64_t delta_wall = last_now_ - last_now;
        const uint64_t delta_proc = (last_proc_sys_ - last_proc_sys) + (last_proc_user_ - last_proc_user);
        if (delta_wall == 0 || logical_processor_count_ <= 0) {
            LogWarn << "SystemMonitor process CPU sample skipped: zero wall delta.";
            return false;
        }
        process_cpu = ClampUnitInterval(
            static_cast<double>(delta_proc) / static_cast<double>(delta_wall) / logical_processor_count_);

        // GetSystemTimes 的 kernel 已含 idle。
        const uint64_t delta_idle = last_idle_ - last_idle;
        const uint64_t delta_kernel_plus_user = (last_kernel_ - last_kernel) + (last_user_ - last_user);
        if (delta_kernel_plus_user == 0) {
            LogWarn << "SystemMonitor system CPU sample skipped: zero kernel+user delta.";
            return false;
        }
        system_cpu = ClampUnitInterval(1.0 - static_cast<double>(delta_idle) / static_cast<double>(delta_kernel_plus_user));
        return true;
    }

private:
    bool CaptureTimes()
    {
        FILETIME now_ft {};
        GetSystemTimeAsFileTime(&now_ft);

        FILETIME create_ft {};
        FILETIME exit_ft {};
        FILETIME proc_sys_ft {};
        FILETIME proc_user_ft {};
        if (!GetProcessTimes(GetCurrentProcess(), &create_ft, &exit_ft, &proc_sys_ft, &proc_user_ft)) {
            LogWarn << "SystemMonitor GetProcessTimes failed." << VAR(GetLastError());
            return false;
        }

        FILETIME idle_ft {};
        FILETIME kernel_ft {};
        FILETIME user_ft {};
        if (!GetSystemTimes(&idle_ft, &kernel_ft, &user_ft)) {
            LogWarn << "SystemMonitor GetSystemTimes failed." << VAR(GetLastError());
            return false;
        }

        last_now_ = FileTimeToU64(now_ft);
        last_proc_sys_ = FileTimeToU64(proc_sys_ft);
        last_proc_user_ = FileTimeToU64(proc_user_ft);
        last_idle_ = FileTimeToU64(idle_ft);
        last_kernel_ = FileTimeToU64(kernel_ft);
        last_user_ = FileTimeToU64(user_ft);
        return true;
    }

    int32_t logical_processor_count_ = 0;
    uint64_t last_now_ = 0;
    uint64_t last_proc_sys_ = 0;
    uint64_t last_proc_user_ = 0;
    uint64_t last_idle_ = 0;
    uint64_t last_kernel_ = 0;
    uint64_t last_user_ = 0;
};

struct GpuAdapterUsage
{
    uint32_t adapter_id = 0;
    double process_gpu = 0.0;
    double system_gpu = 0.0;
};

class GpuSampler
{
public:
    GpuSampler()
    {
        PDH_STATUS status = PdhOpenQueryW(nullptr, 0, &query_);
        if (status != ERROR_SUCCESS) {
            LogWarn << "SystemMonitor PdhOpenQueryW failed." << VAR(status);
            query_ = nullptr;
            return;
        }

        status = PdhAddEnglishCounterW(query_, L"\\GPU Engine(*)\\Utilization Percentage", 0, &counter_);
        if (status != ERROR_SUCCESS) {
            LogWarn << "SystemMonitor GPU Engine counter unavailable; GPU logging disabled." << VAR(status);
            PdhCloseQuery(query_);
            query_ = nullptr;
            counter_ = nullptr;
            return;
        }

        pid_ = GetCurrentProcessId();
        status = PdhCollectQueryData(query_);
        if (status != ERROR_SUCCESS) {
            LogWarn << "SystemMonitor GPU baseline collect failed." << VAR(status);
            PdhCloseQuery(query_);
            query_ = nullptr;
            counter_ = nullptr;
            return;
        }

        available_ = true;
    }

    ~GpuSampler()
    {
        if (query_ != nullptr) {
            PdhCloseQuery(query_);
        }
    }

    GpuSampler(const GpuSampler&) = delete;
    GpuSampler& operator=(const GpuSampler&) = delete;

    bool available() const { return available_; }

    bool Sample(std::vector<GpuAdapterUsage>& usages)
    {
        usages.clear();
        if (!available_ || query_ == nullptr || counter_ == nullptr) {
            return false;
        }

        PDH_STATUS status = PdhCollectQueryData(query_);
        if (status != ERROR_SUCCESS) {
            LogWarn << "SystemMonitor PdhCollectQueryData failed." << VAR(status);
            return false;
        }

        DWORD buffer_size = static_cast<DWORD>(buffer_.size());
        DWORD item_count = 0;
        status = PdhGetFormattedCounterArrayW(
            counter_,
            PDH_FMT_DOUBLE,
            &buffer_size,
            &item_count,
            reinterpret_cast<PDH_FMT_COUNTERVALUE_ITEM_W*>(buffer_.data()));
        if (status == PDH_MORE_DATA) {
            buffer_.assign(buffer_size, 0);
            status = PdhGetFormattedCounterArrayW(
                counter_,
                PDH_FMT_DOUBLE,
                &buffer_size,
                &item_count,
                reinterpret_cast<PDH_FMT_COUNTERVALUE_ITEM_W*>(buffer_.data()));
        }
        if (status != ERROR_SUCCESS) {
            LogWarn << "SystemMonitor PdhGetFormattedCounterArrayW failed." << VAR(status);
            return false;
        }

        const auto* items = reinterpret_cast<PDH_FMT_COUNTERVALUE_ITEM_W*>(buffer_.data());
        static const std::regex kEnginePattern(
            R"(pid_(\d+)_luid_(0x[0-9A-Fa-f]+)_(0x[0-9A-Fa-f]+)_phys_(\d+)_eng_(\d+)_engtype_)");

        // 进程 GPU：当前 PID 在该 adapter 上各 engine 取 max。
        // 系统 GPU：同一 engine 上所有进程求和，再对 engine 取 max。
        std::unordered_map<std::string, double> process_by_adapter;
        std::unordered_map<std::string, std::unordered_map<std::string, double>> engine_sum_by_adapter;

        for (DWORD i = 0; i < item_count; ++i) {
            if (items[i].szName == nullptr) {
                continue;
            }

            const std::string name = WideToUtf8(items[i].szName);
            std::smatch matches;
            try {
                if (!std::regex_search(name, matches, kEnginePattern) || matches.size() != 6) {
                    continue;
                }
            }
            catch (const std::regex_error& e) {
                LogWarn << "SystemMonitor GPU instance regex failed." << VAR(name) << VAR(e.what());
                continue;
            }

            const std::string& pid_str = matches[1];
            const std::string& adapter_str = matches[3];
            const std::string& engine_str = matches[5];
            const double value = items[i].FmtValue.doubleValue;

            if (pid_str == std::to_string(pid_)) {
                auto& slot = process_by_adapter[adapter_str];
                slot = std::max(slot, value);
            }
            engine_sum_by_adapter[adapter_str][engine_str] += value;
        }

        std::unordered_map<std::string, GpuAdapterUsage> by_adapter;
        auto parse_adapter_id = [](const std::string& adapter_str) -> uint32_t {
            try {
                return static_cast<uint32_t>(std::stoul(adapter_str, nullptr, 16));
            }
            catch (const std::exception&) {
                return 0;
            }
        };
        for (const auto& [adapter_str, proc_percent] : process_by_adapter) {
            auto& usage = by_adapter[adapter_str];
            usage.adapter_id = parse_adapter_id(adapter_str);
            usage.process_gpu = PdhPercentToUnit(proc_percent);
        }
        for (const auto& [adapter_str, engines] : engine_sum_by_adapter) {
            double max_percent = 0.0;
            for (const auto& [engine, percent] : engines) {
                max_percent = std::max(max_percent, percent);
            }
            auto& usage = by_adapter[adapter_str];
            usage.adapter_id = parse_adapter_id(adapter_str);
            usage.system_gpu = PdhPercentToUnit(max_percent);
        }

        for (const auto& [adapter_str, usage] : by_adapter) {
            if (usage.process_gpu <= kGpuIdleEpsilon && usage.system_gpu <= kGpuIdleEpsilon) {
                continue;
            }
            usages.push_back(usage);
        }
        return true;
    }

private:
    PDH_HQUERY query_ = nullptr;
    PDH_HCOUNTER counter_ = nullptr;
    std::vector<unsigned char> buffer_;
    DWORD pid_ = 0;
    bool available_ = false;
};

void RunMonitorLoop()
{
    CpuSampler cpu_sampler;
    if (!cpu_sampler.CaptureBaseline()) {
        LogWarn << "SystemMonitor CPU baseline failed; monitor thread exiting.";
        return;
    }

    GpuSampler gpu_sampler;
    LogInfo << "SystemMonitor started." << VAR(gpu_sampler.available());

    while (true) {
        std::this_thread::sleep_for(kSampleInterval);

        const MemorySample memory = SampleMemory();
        double process_cpu = 0.0;
        double system_cpu = 0.0;
        const bool cpu_ok = cpu_sampler.Sample(process_cpu, system_cpu);

        const auto process_private_mb = memory.process_private_mb;
        const auto process_working_set_mb = memory.process_working_set_mb;
        const auto system_used_mb = memory.system_used_mb;
        const auto system_total_mb = memory.system_total_mb;

        if (cpu_ok) {
            LogInfo << "SystemMonitor"
                    << VAR(process_private_mb)
                    << VAR(process_working_set_mb)
                    << VAR(system_used_mb)
                    << VAR(system_total_mb)
                    << VAR(process_cpu)
                    << VAR(system_cpu);
        }
        else {
            LogInfo << "SystemMonitor"
                    << VAR(process_private_mb)
                    << VAR(process_working_set_mb)
                    << VAR(system_used_mb)
                    << VAR(system_total_mb);
        }

        if (!gpu_sampler.available()) {
            continue;
        }

        std::vector<GpuAdapterUsage> gpu_usages;
        if (!gpu_sampler.Sample(gpu_usages)) {
            continue;
        }
        for (const auto& usage : gpu_usages) {
            const auto adapter_id = usage.adapter_id;
            const auto process_gpu = usage.process_gpu;
            const auto system_gpu = usage.system_gpu;
            LogInfo << "SystemMonitor gpu" << VAR(adapter_id) << VAR(process_gpu) << VAR(system_gpu);
        }
    }
}

} // namespace

void StartSystemMonitor()
{
    LogInfo << "SystemMonitor thread launching.";
    std::thread(RunMonitorLoop).detach();
}

#else

void StartSystemMonitor()
{
}

#endif

} // namespace common
