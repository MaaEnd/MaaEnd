#include <filesystem>
#include <iostream>
#include <string>
#include <system_error>

#include <meojson/json.hpp>

#include <MaaAgentServer/MaaAgentServerAPI.h>
#include <MaaFramework/Global/MaaGlobal.h>
#include <MaaUtils/Platform.h>

#include "Common/CrashHandler.h"
#include "Common/ParentProcessWatcher.h"
#include "Common/SystemMonitor.h"
#include "EssenceGridScan/EssenceGridScan.h"
#include "IconRecognition/IconRecognitionRecognition.h"
#include "MapLocator/MapLocateAction.h"
#include "MapNavigator/MapNavigator.h"
#include "MapNavmesh/MapNavmeshQuery.h"
#include "RealTimeTask/RealTimeTaskAction.h"
#include "Test/test.h"
#include "WorldMap/WorldMapFind.h"
#include "Zipline/ZiplineImportAction.h"
#include "my_reco_1/my_reco_1.h"
#include "utils.h"

namespace
{
constexpr const char* kLogDir = "./debug/cpp-algo/debug";

bool initialize_logging()
{
    bool logging = true;
    MaaLoggingLevel stdout_level = MaaLoggingLevel_Error;
    const auto config_path = MAA_NS::path("./config/maa_option.json");
    std::error_code ec;
    const bool config_exists = std::filesystem::exists(config_path, ec);
    if (ec) {
        std::cerr << "Failed to access config/maa_option.json: " << ec.message() << "; using logging defaults" << std::endl;
    }
    else if (config_exists) {
        // 与主进程配置格式保持一致，允许 UTF-8 BOM 和注释；不创建或回写配置。
        const auto config = json::open(config_path, true, true);
        if (!config || !config->is_object()) {
            std::cerr << "Failed to read config/maa_option.json as a JSON object; using logging defaults" << std::endl;
        }
        else {
            if (config->contains("logging")) {
                if (const auto value = config->find<bool>("logging")) {
                    logging = *value;
                }
                else {
                    std::cerr << "Invalid logging in config/maa_option.json; using true" << std::endl;
                }
            }
            if (config->contains("stdout_level")) {
                const auto value = config->find<double>("stdout_level");
                if (value && *value >= static_cast<double>(MaaLoggingLevel_Off) && *value <= static_cast<double>(MaaLoggingLevel_All)
                    && *value == static_cast<MaaLoggingLevel>(*value)) {
                    stdout_level = static_cast<MaaLoggingLevel>(*value);
                }
                else {
                    std::cerr << "Invalid stdout_level in config/maa_option.json; using Error" << std::endl;
                }
            }
        }
    }

    // AgentServer 只支持日志选项，图像保存等配置由主进程负责。
    std::string log_dir = logging ? kLogDir : "";
    if (!MaaGlobalSetOption(MaaGlobalOption_LogDir, log_dir.data(), log_dir.size())) {
        std::cerr << "Failed to set AgentServer LogDir" << std::endl;
        return false;
    }
    if (!MaaGlobalSetOption(MaaGlobalOption_StdoutLevel, &stdout_level, sizeof(stdout_level))) {
        std::cerr << "Failed to set AgentServer StdoutLevel" << std::endl;
        return false;
    }
    return true;
}
} // namespace

int main(int argc, char** argv)
{
#ifdef _WIN32
    if (!setup_dll_directory()) {
        std::cerr << "Warning: Failed to set DLL directory to maafw" << std::endl;
    }
#endif

    if (argc < 2) {
        std::cerr << "Usage: cpp-algo <socket_id>" << std::endl;
        std::cerr << "socket_id is provided by AgentIdentifier." << std::endl;
        return -1;
    }

    if (!initialize_logging()) {
        return -1;
    }

    // 转储落到 maafw.log 同一目录，报 issue 打包日志时会一并带上。
    common::InstallCrashHandler(MAA_NS::path(kLogDir));

    // 父进程一旦退出立刻结束自己，避免 MXU/MFAA 崩溃后 cpp-algo 残留。
    common::StartParentProcessWatcher();

    Test();

    common::StartSystemMonitor();

    essencegridscan::EssenceGrid essence_grid;

    MaaAgentServerRegisterCustomRecognition("MyReco1", ChildCustomRecognitionCallback, nullptr);
    MaaAgentServerRegisterCustomRecognition("MapLocateRecognition", maplocator::MapLocateRecognitionRun, nullptr);
    MaaAgentServerRegisterCustomRecognition("MapLocateAssertLocation", maplocator::MapLocateAssertLocationRun, nullptr);
    MaaAgentServerRegisterCustomRecognition("MapNavmeshQuery", mapnavmesh::MapNavmeshQueryRun, nullptr);
    MaaAgentServerRegisterCustomRecognition(
        "EssenceGridAdvanceRecognition",
        essencegridscan::EssenceGrid::advanceRecognitionRun,
        &essence_grid);
    MaaAgentServerRegisterCustomRecognition(
        "EssenceGridPendingRecognition",
        essencegridscan::EssenceGrid::pendingRecognitionRun,
        &essence_grid);
    MaaAgentServerRegisterCustomRecognition("IconRecognition", iconrecognition::IconRecognitionRun, nullptr);
    MaaAgentServerRegisterCustomRecognition("MapFind", worldmap::MapFindRun, nullptr);
    MaaAgentServerRegisterCustomAction("MapNavigateAction", mapnavigator::MapNavigateActionRun, nullptr);
    MaaAgentServerRegisterCustomAction("RealTimeTaskAction", realtimetask::RealTimeTaskActionRun, nullptr);
#ifdef MAAEND_HAVE_WEBVIEW2
    // 导入要开一个内嵌浏览器让用户自己登录, 而这个控件只有 Windows 有实现,
    // 其余平台把这个动作名留给各自的实现
    MaaAgentServerRegisterCustomAction("ZiplineImport", zipline::ZiplineImportActionRun, nullptr);
#endif

    const char* identifier = argv[argc - 1];

    MaaAgentServerStartUp(identifier);

    MaaAgentServerJoin();

    MaaAgentServerShutDown();

    return 0;
}
