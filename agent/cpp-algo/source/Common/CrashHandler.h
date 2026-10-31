#pragma once

#include <filesystem>

namespace common
{

// 安装崩溃转储处理器，只在 main() 启动阶段调用一次。
// Windows 下照 Crashpad 挂三个钩子：未处理的结构化异常、SIGABRT（各线程的 terminate 都汇到这里）、
// CRT 非法参数。SIGABRT 仅在有待决异常时写转储；非 Windows 只装 terminate 记日志。
// dump_dir 在此解析成绝对路径并建好，崩溃时只做定长拼接，不分配也不依赖当前工作目录。
void InstallCrashHandler(const std::filesystem::path& dump_dir);

} // namespace common
