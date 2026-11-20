#pragma once

#include <string>

#include <MaaFramework/MaaAPI.h>

namespace mapnavigator
{

struct InputBackend;

} // namespace mapnavigator

namespace mapnavigator::backend::cloudweb
{

bool IsCloudWebController(MaaController* ctrl);
InputBackend OpenCloudWebInputBackend(MaaController* ctrl, std::string controller_type);
bool SendCloudWebViewDeltaSync(const InputBackend& backend, int dx, int dy);

} // namespace mapnavigator::backend::cloudweb
