#include <iostream>

#include <MaaAgentServer/MaaAgentServerAPI.h>
#include <MaaToolkit/MaaToolkitAPI.h>

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

    std::cout << "Hello, cpp-algo!" << std::endl;

    MaaToolkitConfigInitOption("./debug/cpp-algo", "{}");

    MaaAgentServerRegisterCustomRecognition("MyReco1", ChildCustomRecognitionCallback, nullptr);

    const char* identifier = argv[argc - 1];

    MaaAgentServerStartUp(identifier);

    MaaAgentServerJoin();

    MaaAgentServerShutDown();

    return 0;
}