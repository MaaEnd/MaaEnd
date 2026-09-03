#pragma once

#include "../backend.h"

namespace mapnavigator::backend::Linux
{

std::unique_ptr<IInputBackend> CreateLinuxInputBackend(MaaController* ctrl, std::string controller_type);

} // namespace mapnavigator::backend::Linux
