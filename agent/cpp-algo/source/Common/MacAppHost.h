#pragma once

#ifdef __APPLE__

namespace common::macapp
{

void RunMainLoop();

void QuitMainLoop();

bool IsMainLoopRunning();

} // namespace common::macapp

#endif // __APPLE__
