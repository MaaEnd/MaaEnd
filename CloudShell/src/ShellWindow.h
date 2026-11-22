#pragma once

#include <filesystem>
#include <memory>
#include <string>
#include <string_view>
#include <vector>

#include <windows.h>
#include <wrl/client.h>

#include <WebView2.h>

#include "InputBridge.h"

namespace cloudshell
{

struct ShellOptions
{
    std::wstring url;
    std::wstring title;
    int width = 1280;
    int height = 720;
    std::filesystem::path user_data_dir;
    std::vector<std::wstring> extra_scripts;
    bool devtools = false;
    int debug_port = 0;
};

// 所有逻辑都在主线程上运行：窗口消息、WebView2 回调与前台事件共用一个消息循环
class ShellWindow
{
public:
    static constexpr const wchar_t* kWindowClassName = L"MaaEnd.CloudShell";

    explicit ShellWindow(ShellOptions options);
    ~ShellWindow();

    ShellWindow(const ShellWindow&) = delete;
    ShellWindow& operator=(const ShellWindow&) = delete;

    bool Create();
    int Run();

private:
    static LRESULT CALLBACK windowProc(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam);
    LRESULT handleMessage(UINT msg, WPARAM wparam, LPARAM lparam);

    void applyFixedClientSize(UINT dpi, const POINT* origin);

    // 记录错误并以非零退出码关闭窗口
    void fail(std::string_view what, HRESULT hr);

    void initWebView();
    void onEnvironmentCreated(HRESULT result, ICoreWebView2Environment* environment);
    void onControllerCreated(HRESULT result, ICoreWebView2Controller* controller);
    void applySettings();
    void injectScripts();
    void subscribeEvents();

    static void CALLBACK onForegroundChanged(HWINEVENTHOOK hook, DWORD event, HWND hwnd, LONG id_object, LONG id_child, DWORD thread, DWORD time);
    void updateForeground(HWND foreground);

private:
    ShellOptions options_;
    HWND hwnd_ = nullptr;
    HWINEVENTHOOK foreground_hook_ = nullptr;
    std::unique_ptr<InputBridge> input_;

    Microsoft::WRL::ComPtr<ICoreWebView2Environment> environment_;
    Microsoft::WRL::ComPtr<ICoreWebView2Controller> controller_;
    Microsoft::WRL::ComPtr<ICoreWebView2> webview_;
    UINT32 browser_pid_ = 0;
    int exit_code_ = 0;
};

} // namespace cloudshell
