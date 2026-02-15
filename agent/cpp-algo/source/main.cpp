#include <iostream>

#include "utils.h"

int main()
{
#ifdef _WIN32
    if (!setup_dll_directory()) {
        std::cerr << "Warning: Failed to set DLL directory to maafw" << std::endl;
    }
#endif

    std::cout << "Hello, cpp-algo!" << std::endl;
    return 0;
}
