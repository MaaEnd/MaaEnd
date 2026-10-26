#include <filesystem>
#include <fstream>
#include <optional>
#include <sstream>
#include <string>

#include <windows.h>

#include <objbase.h>
#include <shellapi.h>

#include "Log.h"
#include "Resources.h"
#include "ShellWindow.h"

namespace
{

constexpr const wchar_t* kDefaultUrl = L"https://endfield.hypergryph.com/cloud";
constexpr const wchar_t* kDefaultTitle = L"MaaEnd CloudShell";

std::optional<std::wstring> readFile(const std::filesystem::path& path)
{
    std::ifstream file(path, std::ios::binary);
    if (!file) {
        return std::nullopt;
    }
    std::ostringstream buffer;
    buffer << file.rdbuf();
    return cloudshell::Utf8ToWide(buffer.str());
}

bool parseOptions(cloudshell::ShellOptions& options)
{
    int argc = 0;
    LPWSTR* argv = CommandLineToArgvW(GetCommandLineW(), &argc);
    if (!argv) {
        return false;
    }

    bool ok = true;
    for (int i = 1; i < argc && ok; ++i) {
        const std::wstring arg = argv[i];
        const bool has_value = i + 1 < argc;
        if (arg == L"--devtools") {
            options.devtools = true;
        }
        else if (arg == L"--url" && has_value) {
            options.url = argv[++i];
        }
        else if (arg == L"--title" && has_value) {
            options.title = argv[++i];
        }
        else if (arg == L"--width" && has_value) {
            options.width = _wtoi(argv[++i]);
        }
        else if (arg == L"--height" && has_value) {
            options.height = _wtoi(argv[++i]);
        }
        else if (arg == L"--user-data-dir" && has_value) {
            options.user_data_dir = std::filesystem::absolute(argv[++i]);
        }
        else if (arg == L"--debug-port" && has_value) {
            options.debug_port = _wtoi(argv[++i]);
        }
        else if (arg == L"--inject" && has_value) {
            const std::filesystem::path path = argv[++i];
            if (auto script = readFile(path)) {
                options.extra_scripts.emplace_back(std::move(*script));
            }
            else {
                cloudshell::log::Error("failed to read inject script {}", cloudshell::WideToUtf8(path.wstring()));
                ok = false;
            }
        }
        else {
            cloudshell::log::Error("unknown or incomplete argument: {}", cloudshell::WideToUtf8(arg));
            ok = false;
        }
    }

    LocalFree(argv);
    return ok && options.width > 0 && options.height > 0;
}

} // namespace

int WINAPI wWinMain(HINSTANCE, HINSTANCE, PWSTR, int)
{
    const std::filesystem::path working_dir = std::filesystem::current_path();
    cloudshell::log::Init(working_dir / "debug" / "cloud-shell.log");

    cloudshell::ShellOptions options {
        .url = kDefaultUrl,
        .title = kDefaultTitle,
        .user_data_dir = working_dir / "cache" / "cloud-shell" / "WebView2",
    };
    if (!parseOptions(options)) {
        return 1;
    }

    SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);

    if (FAILED(CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED | COINIT_DISABLE_OLE1DDE))) {
        cloudshell::log::Error("CoInitializeEx failed");
        return 1;
    }

    int exit_code = 1;
    {
        cloudshell::ShellWindow window(std::move(options));
        if (window.Create()) {
            cloudshell::log::Info("started");
            exit_code = window.Run();
            cloudshell::log::Info("exited with {}", exit_code);
        }
    }

    CoUninitialize();
    return exit_code;
}
