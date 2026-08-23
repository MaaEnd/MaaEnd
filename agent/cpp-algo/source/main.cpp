#include <filesystem>
#include <iostream>

#include <MaaAgentServer/MaaAgentServerAPI.h>
#include <MaaToolkit/MaaToolkitAPI.h>

#include "Common/CrashHandler.h"
#include "Common/ParentProcessWatcher.h"
#include "Common/SystemMonitor.h"
#include "EssenceGridScan/EssenceGridScan.h"
#include "IconRecognition/IconRecognitionRecognition.h"
#include "MapLocator/MapLocateAction.h"
#include "MapNavigator/MapNavigator.h"
#include "MapNavigator/MapNavigatorCompatible.h"
#include "MapNavmesh/MapNavmeshQuery.h"
#include "RealTimeTask/RealTimeTaskAction.h"
#include "Test/test.h"
#include "Zipline/ZiplineImportAction.h"
#include "my_reco_1/my_reco_1.h"
#include "utils.h"

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

    // 父进程一旦退出立刻结束自己，避免 MXU/MFAA 崩溃后 cpp-algo 残留。
    common::StartParentProcessWatcher();

    Test();

    // std::cout << "Hello, cpp-algo!" << std::endl;

    constexpr const char* kUserPath = "./debug/cpp-algo";
    MaaToolkitConfigInitOption(kUserPath, "{}");

    // 转储落到 maa.log 同一目录，报 issue 打包日志时会一并带上。
    common::InstallCrashHandler(std::filesystem::path(kUserPath) / "debug");

    common::StartSystemMonitor();

    essencegridscan::EssenceGrid essence_grid;

    MaaAgentServerRegisterCustomRecognition("MyReco1", ChildCustomRecognitionCallback, nullptr);
    MaaAgentServerRegisterCustomRecognition("MapLocateRecognition", maplocator::MapLocateRecognitionRun, nullptr);
    MaaAgentServerRegisterCustomRecognition("MapLocateAssertLocation", maplocator::MapLocateAssertLocationRun, nullptr);
    MaaAgentServerRegisterCustomRecognition(
        "MapNavigatorAssertLocationCompatible",
        mapnavigator::MapNavigatorAssertLocationCompatibleRun,
        nullptr);
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
    MaaAgentServerRegisterCustomAction("MapNavigateAction", mapnavigator::MapNavigateActionRun, nullptr);
    MaaAgentServerRegisterCustomAction("MapNavigatorCompatible", mapnavigator::MapNavigatorCompatibleRun, nullptr);
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
