#pragma once

namespace common
{

// 启动进程/系统资源监测。
//
// Windows 上在后台分离线程里每隔 10 秒采样一次：
//   - 进程 PrivateUsage / WorkingSet、系统已用/总量内存
//   - 进程 CPU、系统 CPU（区间占用，范围 0.0–1.0）
//   - GPU Engine 占用（Win10+ PDH；不可用则跳过）
// 采样结果以 LogInfo 写出，前缀为 SystemMonitor。
//
// 非 Windows 为空实现。仅应在 main() 启动阶段调用一次。
void StartSystemMonitor();

} // namespace common
