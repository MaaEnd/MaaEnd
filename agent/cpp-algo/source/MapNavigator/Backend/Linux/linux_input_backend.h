#pragma once

#include <string>

#include <MaaFramework/MaaAPI.h>

#include "../backend_types.h"

namespace mapnavigator
{

struct InputBackend;

} // namespace mapnavigator

namespace mapnavigator::backend::Linux
{

InputBackend OpenLinuxInputBackend(MaaController* ctrl, std::string controller_type);
SteeringTransportProfile LinuxSteeringProfile();
void TriggerLinuxSprintSync(const InputBackend& backend);

} // namespace mapnavigator::backend::Linux
