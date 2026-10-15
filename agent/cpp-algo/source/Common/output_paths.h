#pragma once

#include <filesystem>

namespace common
{

// Capture the working directory once. Call at the beginning of main so a failure
// aborts startup before logging or other persistent output is initialized.
inline const std::filesystem::path& StartupDir()
{
    static const std::filesystem::path startup_dir = std::filesystem::current_path();
    return startup_dir;
}

// Resolve persistent output against the startup directory, not the executable.
inline std::filesystem::path OutputPath(const std::filesystem::path& relative_path = {})
{
    return relative_path.empty() ? StartupDir() : StartupDir() / relative_path;
}

} // namespace common
