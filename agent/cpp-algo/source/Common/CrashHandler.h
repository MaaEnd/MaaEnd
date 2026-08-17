#pragma once

#include <filesystem>

namespace common
{

// 安装崩溃转储处理器，只在 main() 启动阶段调用一次。
// Windows 下挂四个钩子覆盖互不相交的死法：结构化异常、未接住的 C++ 异常、SIGABRT、
// CRT 非法参数；非 Windows 只装 terminate 记日志。
// dump_dir 在此解析成绝对路径并建好，崩溃时只做定长拼接，不分配也不依赖当前工作目录。
void InstallCrashHandler(const std::filesystem::path& dump_dir);

} // namespace common
