#pragma once

namespace common
{

// 安装崩溃转储处理器。
//
// Windows：在 {exe 目录}/../debug/ 写入 MiniDump（cpp-algo.exe.<pid>.dmp），
// 覆盖未处理 SEH、std::terminate、abort / SIGABRT。
// 非 Windows：空实现。
//
// 仅应在 main() 启动阶段调用一次。
void InstallCrashDumpHandler();

} // namespace common
