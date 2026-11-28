#pragma once

#include <cstdlib>
#include <filesystem>
#include <memory>
#include <stdexcept>

#include <MaaUtils/Platform.h>

namespace common
{

// 安装根目录：进程启动时的工作目录，宿主以它作为 agent 的 CWD。
inline const std::filesystem::path& InstallDir()
{
    static const std::filesystem::path install_dir = std::filesystem::current_path();
    return install_dir;
}

inline std::filesystem::path InstallPath(const std::filesystem::path& relative_path = {})
{
    return relative_path.empty() ? InstallDir() : InstallDir() / relative_path;
}

// 用户数据根：PI_USER_DATA_DIR，未注入时回退项目根（PI 默认便携布局）。
inline const std::filesystem::path& UserDataDir()
{
    static const std::filesystem::path user_data_dir = [] {
#ifdef _WIN32
        wchar_t* buffer = nullptr;
        size_t size = 0;
        if (_wdupenv_s(&buffer, &size, L"PI_USER_DATA_DIR") != 0) {
            throw std::runtime_error("Failed to read PI_USER_DATA_DIR");
        }
        const std::unique_ptr<wchar_t, decltype(&std::free)> value(buffer, &std::free);
        const std::filesystem::path dir = buffer && *buffer ? std::filesystem::path(buffer) : InstallDir();
#else
        const char* value = std::getenv("PI_USER_DATA_DIR");
        const std::filesystem::path dir = value && *value ? MAA_NS::path(value) : InstallDir();
#endif
        if (!dir.is_absolute()) {
            throw std::runtime_error("PI_USER_DATA_DIR must be an absolute path");
        }
        return dir.lexically_normal();
    }();
    return user_data_dir;
}

inline std::filesystem::path UserDataPath(const std::filesystem::path& relative_path = {})
{
    return relative_path.empty() ? UserDataDir() : UserDataDir() / relative_path;
}

} // namespace common
