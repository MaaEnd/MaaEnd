#pragma once

namespace cloud
{
// Process/window lifecycle only. Website login and startup run in Pipeline.
int HostCommand(int argc, char** argv);
} // namespace cloud
