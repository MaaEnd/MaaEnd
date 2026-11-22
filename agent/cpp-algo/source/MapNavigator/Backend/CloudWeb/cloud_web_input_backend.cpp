#include <algorithm>
#include <cstdint>
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
// 1280×720 客户区实测线性区约 11.5 px/deg，俯仰换算与桌面端保持同一纵横比与速度比
constexpr double kCloudWebDefaultUnitsPerDegree = 11.5;
constexpr double kCloudWebDefaultPitchRangeUnits =
    kCloudWebDefaultUnitsPerDegree * kTurnDegreesPerCircle * (static_cast<double>(kWorkHeight) / kWorkWidth) / kMouseVerticalDragSpeedRatio;
// 单次位移超过约 145 px 会被云端截断；10.5° ≈ 121 px 留在安全线性区内，且低于最小发射角不发包
constexpr double kCloudWebMaxBatchDeltaDeg = 10.5;
constexpr double kCloudWebMinEmitDeltaDeg = 1.0;
// 每拍限发 1 批并保持最小发送间隔，避免连发在云端/WebView2 队列堆积后合并越界
constexpr int kCloudWebMaxBatchesPerTick = 1;
constexpr int kCloudWebMinSendIntervalMs = 50;
// 实测云端画面转向落地需约 340~380 ms，灵敏度检测需等满该静默期再采样
constexpr int64_t kCloudWebSensitivityMinQuietMs = 400;

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
    InputBackend backend = desktop::MakeDesktopInputBackend(BackendKind::CloudWeb, ctrl, std::move(controller_type), "cloud_web");
    backend.default_turn_units_per_degree = kCloudWebDefaultUnitsPerDegree;
    backend.default_pitch_range_units = kCloudWebDefaultPitchRangeUnits;
    LogInfo << "CloudWeb turn scale applied." << VAR(backend.default_turn_units_per_degree) << VAR(backend.default_pitch_range_units);
    return backend;
}

SteeringTransportProfile CloudWebSteeringProfile()
{
    return SteeringTransportProfile {
        .supports_concurrent_move_and_look = true,
        .min_send_interval_ms = kCloudWebMinSendIntervalMs,
        .min_emit_delta_deg = kCloudWebMinEmitDeltaDeg,
        .max_batch_delta_deg = kCloudWebMaxBatchDeltaDeg,
        .max_batches_per_tick = kCloudWebMaxBatchesPerTick,
        .action_quiet_period_ms = 0,
        .sensitivity_min_quiet_ms = kCloudWebSensitivityMinQuietMs,
        .drops_turn_sends = true,
    };
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
