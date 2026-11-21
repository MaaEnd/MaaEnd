#include <algorithm>
#include <cstdlib>
#include <optional>
#include <string>
#include <string_view>
#include <utility>

#include <meojson/json.hpp>

#ifdef _WIN32
#include <MaaUtils/Encoding.h>
#endif
#include <MaaUtils/Logger.h>

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

} // namespace

bool IsCloudWebController()
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

    LogInfo << "SendRelativeMoveNative" << VAR(dx) << VAR(dy);
    int remaining_x = dx;
    int remaining_y = dy;
    while (remaining_x != 0 || remaining_y != 0) {
        const int step_x = std::clamp(remaining_x, 1 - kViewDeltaCenterX, kViewDeltaCenterX - 1);
        const int step_y = std::clamp(remaining_y, 1 - kViewDeltaCenterY, kViewDeltaCenterY - 1);
        const MaaCtrlId ctrl_id =
            MaaControllerPostTouchMove(backend.ctrl, kViewDeltaContactId, kViewDeltaCenterX + step_x, kViewDeltaCenterY + step_y, 0);
        if (MaaControllerWait(backend.ctrl, ctrl_id) != MaaStatus_Succeeded) {
            return false;
        }
        remaining_x -= step_x;
        remaining_y -= step_y;
    }
    return true;
}

} // namespace mapnavigator::backend::cloudweb
