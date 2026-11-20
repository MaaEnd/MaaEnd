#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdlib>
#include <optional>
#include <string>
#include <string_view>
#include <thread>
#include <utility>

#include <meojson/json.hpp>

#ifdef _WIN32
#include <MaaUtils/Encoding.h>
#include <MaaUtils/SafeWindows.hpp>
#endif
#include <MaaUtils/Logger.h>

#include "../../control/controller_info_utils.h"
#include "../../navi_config.h"
#include "../Desktop/desktop_input_backend.h"
#include "../backend.h"
#include "cloud_web_input_backend.h"

namespace mapnavigator::backend::cloudweb
{

namespace
{

constexpr std::string_view kCloudWebControllerName = "CloudWeb";
// 外壳把 4 号接触点（侧键 2）未按下时的移动当作视角相对移动，位移为落点减客户区中心
constexpr int32_t kViewDeltaContactId = 4;
constexpr int32_t kViewDeltaCenterX = kWorkWidth / 2;
constexpr int32_t kViewDeltaCenterY = kWorkHeight / 2;
// 云客户端对单次位移有上限（实测约 200 像素），超过的部分到不了游戏，切成小块发
constexpr int kViewDeltaChunkPixels = 128;
constexpr auto kViewDeltaChunkInterval = std::chrono::milliseconds(10);

struct PiController
{
    std::string name;

    MEO_JSONIZATION(MEO_OPT name)
};

std::string read_pi_controller()
{
#ifdef _WIN32
    wchar_t* buffer = nullptr;
    size_t buffer_size = 0;
    if (_wdupenv_s(&buffer, &buffer_size, L"PI_CONTROLLER") != 0 || buffer == nullptr) {
        return {};
    }
    std::string controller = MAA_NS::from_u16(buffer);
    std::free(buffer);
    return controller;
#else
    const char* raw = std::getenv("PI_CONTROLLER");
    if (raw == nullptr) {
        return {};
    }
    return raw;
#endif
}

// PI_CONTROLLER 只描述启动时选中的控制器，切换控制器后可能过期；
// 缺失或对不上时按外壳窗口类名兜底，类名见 CloudShell/README.md 的约定表
#ifdef _WIN32
constexpr std::wstring_view kCloudShellWindowClass = L"MaaEnd.CloudShell";
constexpr int kWindowClassNameLength = 256;

bool is_cloud_shell_window(uintptr_t handle)
{
    wchar_t class_name[kWindowClassNameLength] = {};
    const int length = GetClassNameW(reinterpret_cast<HWND>(handle), class_name, kWindowClassNameLength);
    if (length <= 0) {
        LogWarn << "GetClassNameW failed for CloudWeb detection" << VAR(handle) << VAR(GetLastError());
        return false;
    }
    return std::wstring_view(class_name, static_cast<size_t>(length)) == kCloudShellWindowClass;
}
#endif

// 外壳用落点减客户区中心得到位移，而框架投递前会把 pipeline 坐标按 raw/target 放大，
// 所以 raw 位移要先换回 pipeline 位移，否则实际转角会多出 raw/target 倍
int32_t to_pipeline_delta(int raw_delta, int32_t raw_size, int32_t pipeline_size)
{
    if (raw_delta == 0 || raw_size <= 0 || pipeline_size <= 0) {
        return raw_delta;
    }

    const double scaled = static_cast<double>(raw_delta) * pipeline_size / raw_size;
    const auto rounded = static_cast<int32_t>(std::lround(scaled));
    if (rounded != 0) {
        return rounded;
    }

    // 换算后不能退化成 0，否则发送循环不收敛
    return raw_delta > 0 ? 1 : -1;
}

bool is_cloud_web_by_window_class(MaaController* ctrl)
{
#ifdef _WIN32
    const std::optional<uintptr_t> handle = TryGetWin32WindowHandle(ctrl);
    const bool matched = handle.has_value() && is_cloud_shell_window(*handle);
    LogInfo << "PI_CONTROLLER missing or mismatched, fallback to window class." << VAR(matched);
    return matched;
#else
    (void)ctrl;
    return false;
#endif
}

bool is_cloud_web_by_pi_controller()
{
    const std::optional<json::value> parsed = json::parse(read_pi_controller());
    if (!parsed.has_value()) {
        return false;
    }

    PiController controller;
    if (!controller.from_json(*parsed)) {
        return false;
    }
    return controller.name == kCloudWebControllerName;
}

} // namespace

bool IsCloudWebController(MaaController* ctrl)
{
    // 两个信号取或：PI 过期时窗口类名能纠正，窗口类名判不了时 PI 还能兜住
    return is_cloud_web_by_pi_controller() || is_cloud_web_by_window_class(ctrl);
}

InputBackend OpenCloudWebInputBackend(MaaController* ctrl, std::string controller_type)
{
    LogInfo << "MapNavigator input backend selected." << VAR(controller_type) << " backend=cloud_web";
    return desktop::MakeDesktopInputBackend(BackendKind::CloudWeb, ctrl, std::move(controller_type), "cloud_web");
}

bool SendCloudWebViewDeltaSync(const InputBackend& backend, int dx, int dy)
{
    if (dx == 0 && dy == 0) {
        return true;
    }

    if (backend.ctrl == nullptr) {
        return false;
    }

    int32_t raw_width = 0;
    int32_t raw_height = 0;
    const bool has_resolution =
        MaaControllerGetResolution(backend.ctrl, &raw_width, &raw_height) && raw_width > 0 && raw_height > 0;
    const int target_x = has_resolution ? to_pipeline_delta(dx, raw_width, kWorkWidth) : dx;
    const int target_y = has_resolution ? to_pipeline_delta(dy, raw_height, kWorkHeight) : dy;

    const int span = std::max(std::abs(target_x), std::abs(target_y));
    const int chunks = std::max(1, (span + kViewDeltaChunkPixels - 1) / kViewDeltaChunkPixels);
    LogInfo << "SendCloudWebViewDelta" << VAR(dx) << VAR(dy) << VAR(target_x) << VAR(target_y) << VAR(chunks)
            << VAR(raw_width) << VAR(raw_height);
    int remaining_x = target_x;
    int remaining_y = target_y;
    while (remaining_x != 0 || remaining_y != 0) {
        // 云客户端对单次鼠标位移有上限（实测约 200 像素），超出部分会被远端丢掉，
        // 所以大位移切成小块发，落点始终留在窗口内
        const int step_x = std::clamp(remaining_x, -kViewDeltaChunkPixels, kViewDeltaChunkPixels);
        const int step_y = std::clamp(remaining_y, -kViewDeltaChunkPixels, kViewDeltaChunkPixels);
        const int landing_x = std::clamp(kViewDeltaCenterX + step_x, 1, kWorkWidth - 1);
        const int landing_y = std::clamp(kViewDeltaCenterY + step_y, 1, kWorkHeight - 1);
        const MaaCtrlId ctrl_id = MaaControllerPostTouchMove(backend.ctrl, kViewDeltaContactId, landing_x, landing_y, 0);
        if (MaaControllerWait(backend.ctrl, ctrl_id) != MaaStatus_Succeeded) {
            return false;
        }
        remaining_x -= step_x;
        remaining_y -= step_y;
        if (remaining_x != 0 || remaining_y != 0) {
            std::this_thread::sleep_for(kViewDeltaChunkInterval);
        }
    }
    return true;
}

} // namespace mapnavigator::backend::cloudweb
