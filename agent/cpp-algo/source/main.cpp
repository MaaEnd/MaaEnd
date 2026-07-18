#include <iostream>

#include <MaaAgentServer/MaaAgentServerAPI.h>
#include <MaaToolkit/MaaToolkitAPI.h>

#include "Common/ParentProcessWatcher.h"
#include "EssenceGridScan/EssenceGridScan.h"
#include "MapLocator/MapLocateAction.h"
#include "MapNavigator/MapNavigator.h"
#include "MapNavigator/MapNavigatorCompatible.h"
#include "RealTimeTask/RealTimeTaskAction.h"
#include "RecoGrid/RecoGridRecognition.h"
#include "Test/test.h"
#include "WeaponInventoryScan/WeaponInventoryScan.h"
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

    MaaToolkitConfigInitOption("./debug/cpp-algo", "{}");

    MaaAgentServerRegisterCustomRecognition("MyReco1", ChildCustomRecognitionCallback, nullptr);
    MaaAgentServerRegisterCustomRecognition("MapLocateRecognition", maplocator::MapLocateRecognitionRun, nullptr);
    MaaAgentServerRegisterCustomRecognition("MapLocateAssertLocation", maplocator::MapLocateAssertLocationRun, nullptr);
    MaaAgentServerRegisterCustomRecognition(
        "MapNavigatorAssertLocationCompatible",
        mapnavigator::MapNavigatorAssertLocationCompatibleRun,
        nullptr);
    MaaAgentServerRegisterCustomRecognition("RecoGridRecognition", recogrid::RecoGridRecognitionRun, nullptr);
    MaaAgentServerRegisterCustomRecognition("EssenceGridAdvanceRecognition", essencegridscan::EssenceGridAdvanceRecognitionRun, nullptr);
    MaaAgentServerRegisterCustomRecognition("EssenceGridPendingRecognition", essencegridscan::EssenceGridPendingRecognitionRun, nullptr);
    MaaAgentServerRegisterCustomRecognition(
        "WeaponInventoryScanRecognition",
        weaponinventoryscan::WeaponInventoryScanRecognitionRun,
        nullptr);
    MaaAgentServerRegisterCustomAction("MapNavigateAction", mapnavigator::MapNavigateActionRun, nullptr);
    MaaAgentServerRegisterCustomAction("MapNavigatorCompatible", mapnavigator::MapNavigatorCompatibleRun, nullptr);
    MaaAgentServerRegisterCustomAction("RealTimeTaskAction", realtimetask::RealTimeTaskActionRun, nullptr);

    const char* identifier = argv[argc - 1];

    MaaAgentServerStartUp(identifier);

    MaaAgentServerJoin();

    MaaAgentServerShutDown();

    return 0;
}
