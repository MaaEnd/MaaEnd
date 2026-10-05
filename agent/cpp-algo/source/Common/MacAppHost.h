#pragma once

#ifdef __APPLE__

#include <string>
#include <vector>

namespace common::macapp
{

void RunMainLoop();

void QuitMainLoop();

bool IsMainLoopRunning();

std::vector<std::string> RunningApplicationIds();

} // namespace common::macapp

#endif // __APPLE__
