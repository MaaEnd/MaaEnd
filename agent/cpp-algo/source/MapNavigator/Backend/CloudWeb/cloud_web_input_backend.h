#pragma once

#include <string>

#include <MaaFramework/MaaAPI.h>

#include "../backend_types.h"

namespace mapnavigator
{

struct InputBackend;

} // namespace mapnavigator

namespace mapnavigator::backend::cloudweb
{

bool IsCloudWebController();
InputBackend OpenCloudWebInputBackend(MaaController* ctrl, std::string controller_type);
SteeringTransportProfile CloudWebSteeringProfile();
bool SendCloudWebViewDeltaSync(const InputBackend& backend, int dx, int dy);

} // namespace mapnavigator::backend::cloudweb
