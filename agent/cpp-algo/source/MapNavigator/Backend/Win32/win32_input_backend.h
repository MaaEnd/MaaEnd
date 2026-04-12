#pragma once

#include "../backend.h"

namespace mapnavigator::backend::win32
{

std::unique_ptr<IInputBackend> CreateWin32InputBackend(MaaController* ctrl, std::string controller_type);

} // namespace mapnavigator::backend::win32
