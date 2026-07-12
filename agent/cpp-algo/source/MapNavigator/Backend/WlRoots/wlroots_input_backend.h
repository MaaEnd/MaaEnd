#pragma once

#include "../backend.h"

namespace mapnavigator::backend::wlroots
{

std::unique_ptr<IInputBackend> CreateWlrootsInputBackend(MaaController* ctrl, std::string controller_type);

} // namespace mapnavigator::backend::wlroots
