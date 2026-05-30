#pragma once

namespace common
{

// 启动父进程存活监视器。
//
// 进程启动时记录父进程 PID 并打开句柄（Windows）/ 保留 PID（POSIX），
// 之后在后台分离线程里每秒检查一次：
//   - 父进程仍在  → 继续监视。
//   - 父进程已退出 → 立即 std::exit(0) 结束当前 cpp-algo 进程，
//                    避免 MXU / MFAA 等宿主崩溃后 cpp-algo 残留为孤儿。
//
// 仅应在 main() 启动阶段调用一次。线程为 detach 设计，进程退出时随之结束。
void StartParentProcessWatcher();

} // namespace common
